package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/yashkumarverma/computer/sdk/connector/storage"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

type NewOpsOpts struct {
	NameSpace  Namespace
	Name       string
	EvalWindow time.Duration
	Resolution time.Duration
	// Dimensions is the closed set of slice names this operation may be tagged
	// with (e.g. "bank", "psp", "router"). Declaring them up front caps key
	// cardinality: only these names ever become counters, so a stray high-
	// cardinality tag (userID, txnID) can't explode the keyspace.
	Dimensions []string
}

// Dims tags a single event with dimension values, e.g.
// {"bank": "bankA", "psp": "psp1"}. Every key must be a dimension the operation
// declared at registration; an undeclared key fails the record.
type Dims map[string]string

type Operation struct {
	Namespace  Namespace
	Name       string
	EvalWindow time.Duration
	Resolution time.Duration
	store      storage.Storage // todo: find a better way, feels counter intuitive
	metric     *prometheus.CounterVec
	dimensions map[string]struct{}
}

func (o *Operation) bucket(ts time.Time) int64 {
	// Nanosecond division so sub-second resolutions don't truncate to a
	// divide-by-zero. Same divisor is used on read and write.
	return ts.UnixNano() / o.Resolution.Nanoseconds()
}

func (o *Operation) key(outcome Outcome, seq int64) string {
	return fmt.Sprintf("%s:%s:%s:%d", o.Namespace.Name, o.Name, outcome, seq)
}

// dimKey is the marginal counter for a single dimension value. The "d" segment
// keeps this namespace disjoint from the overall key above.
func (o *Operation) dimKey(dim, val string, outcome Outcome, seq int64) string {
	return fmt.Sprintf("%s:%s:d:%s:%s:%s:%d", o.Namespace.Name, o.Name, dim, val, outcome, seq)
}

func (o *Operation) record(ctx context.Context, ts time.Time, outcome Outcome, dims Dims) error {
	// Validate the whole tag set before any write so a bad tag can't leave a
	// partial record behind.
	for dim := range dims {
		if _, ok := o.dimensions[dim]; !ok {
			return fmt.Errorf("%w: %q", ErrUnknownDimension, dim)
		}
	}

	seq := o.bucket(ts)
	exp := time.Now().Add(o.EvalWindow)

	o.metric.WithLabelValues(o.Namespace.Name, o.Name, string(outcome)).Inc()
	if err := o.store.IncNumberWithEx(ctx, o.key(outcome, seq), exp); err != nil {
		return err
	}
	// One marginal counter per dimension value.
	for dim, val := range dims {
		if err := o.store.IncNumberWithEx(ctx, o.dimKey(dim, val, outcome, seq), exp); err != nil {
			return err
		}
	}
	return nil
}

// rate scans the eval window's buckets using key to build each lookup, and
// returns success/(success+failure). Both the overall and per-dimension rates
// share this loop.
func (o *Operation) rate(ctx context.Context, key func(Outcome, int64) string) (float64, error) {
	buckets := int(o.EvalWindow / o.Resolution)
	currentSeq := o.bucket(time.Now())

	var success, failure int64
	// i <= buckets: the current bucket is partial, so read one extra older
	// bucket to cover the full EvalWindow instead of 55-60s of it.
	// ponytail: slight tail over-coverage, fine for a health ratio.
	for i := 0; i <= buckets; i++ {
		seq := currentSeq - int64(i)
		s, err := o.store.GetNumber(ctx, key(OutcomeSuccess, seq))
		if err != nil {
			return 0, err
		}
		f, err := o.store.GetNumber(ctx, key(OutcomeFailure, seq))
		if err != nil {
			return 0, err
		}
		success += s
		failure += f
	}

	total := success + failure
	if total == 0 {
		// No traffic in the window is reported as healthy (1.0) by design, so a
		// breaker built on this doesn't trip on silence. A real store outage
		// now surfaces as an error above instead of being masked as 1.0.
		return 1, nil
	}
	return float64(success) / float64(total), nil
}

// SuccessRate is the overall rate across every event in the window.
func (o *Operation) SuccessRate(ctx context.Context) (float64, error) {
	return o.rate(ctx, o.key)
}

// SuccessRateBy is the rate restricted to events tagged dim=val, e.g.
// SuccessRateBy(ctx, "bank", "bankA"). dim must have been declared at
// registration.
func (o *Operation) SuccessRateBy(ctx context.Context, dim, val string) (float64, error) {
	if _, ok := o.dimensions[dim]; !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownDimension, dim)
	}
	return o.rate(ctx, func(outcome Outcome, seq int64) string {
		return o.dimKey(dim, val, outcome, seq)
	})
}

func (c *Client) RegisterOperation(opts *NewOpsOpts) *Operation {
	dims := make(map[string]struct{}, len(opts.Dimensions))
	for _, d := range opts.Dimensions {
		dims[d] = struct{}{}
	}
	ops := &Operation{
		Name:       opts.Name,
		Namespace:  opts.NameSpace,
		EvalWindow: opts.EvalWindow,
		Resolution: opts.Resolution,
		store:      c.store,
		metric:     c.metricOps,
		dimensions: dims,
	}
	c.state.Operations[opts.NameSpace][opts.Name] = ops
	return ops
}

func (o *Operation) RecordSuccess(ctx context.Context, ts time.Time, dims Dims) error {
	if time.Since(ts) > o.EvalWindow {
		return nil
	}
	return o.record(ctx, ts, OutcomeSuccess, dims)
}

func (o *Operation) RecordFailure(ctx context.Context, ts time.Time, dims Dims) error {
	if time.Since(ts) > o.EvalWindow {
		return nil
	}
	return o.record(ctx, ts, OutcomeFailure, dims)
}
