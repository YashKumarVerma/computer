package sdk

import (
	"context"
	"fmt"
	"sync"
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
	Dimensions []string
	Eval       func(interface{}) bool
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
	eval       func(interface{}) bool

	// seen is the set of values observed per dimension, so FailureRates can
	// spread over the values actually in play without the caller enumerating
	// them. Populated on write.
	// ponytail: in-memory, per-instance, never pruned — assumes bounded value
	// cardinality (banks, psps). If values are unbounded, prune values whose
	// buckets have all expired.
	mu   sync.Mutex
	seen map[string]map[string]struct{}
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
	o.observe(dims)
	return nil
}

// observe remembers which values have been seen per dimension so FailureRates
// can iterate them later. Lazy-inits so struct-literal Operations (tests) work.
func (o *Operation) observe(dims Dims) {
	if len(dims) == 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen == nil {
		o.seen = make(map[string]map[string]struct{})
	}
	for dim, val := range dims {
		if o.seen[dim] == nil {
			o.seen[dim] = make(map[string]struct{})
		}
		o.seen[dim][val] = struct{}{}
	}
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

// FailureRate is 1 - SuccessRate over the whole window. No traffic reads as
// 0.0 failure (SuccessRate returns 1.0 on silence), so a breaker doesn't trip
// on an idle operation.
func (o *Operation) FailureRate(ctx context.Context) (float64, error) {
	s, err := o.SuccessRate(ctx)
	return 1 - s, err
}

// FailureRates returns the current eval-window failure rate for every value
// observed on every registered dimension, keyed dimension -> value -> rate.
// This is the "spread across dimensions" view the caller would otherwise have
// to assemble by guessing values and calling SuccessRateBy per value.
func (o *Operation) FailureRates(ctx context.Context) (map[string]map[string]float64, error) {
	// Snapshot the seen-set under the lock, then do the store reads without it.
	o.mu.Lock()
	snapshot := make(map[string][]string, len(o.seen))
	for dim, vals := range o.seen {
		for val := range vals {
			snapshot[dim] = append(snapshot[dim], val)
		}
	}
	o.mu.Unlock()

	out := make(map[string]map[string]float64, len(snapshot))
	for dim, vals := range snapshot {
		out[dim] = make(map[string]float64, len(vals))
		for _, val := range vals {
			s, err := o.SuccessRateBy(ctx, dim, val)
			if err != nil {
				return nil, err
			}
			out[dim][val] = 1 - s
		}
	}
	return out, nil
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
	// Wrap the caller's predicate once at registration so the fail-to-success
	// + eval-error policy (design doc §7.3/§7.4) is enforced for every Record,
	// not left to each predicate to remember. Nil Eval => Record is unusable;
	// such operations use RecordSuccess/RecordFailure with a verdict in hand.
	if opts.Eval != nil {
		ops.eval = guard(opts.Eval, func() {
			c.metricEvalErr.WithLabelValues(opts.NameSpace.Name, opts.Name).Inc()
		})
	}
	c.state.Operations[opts.NameSpace][opts.Name] = ops
	return ops
}

// Record runs the operation's predicate on payload and records the outcome. The
// predicate is guarded, so this never returns a predicate error — only a store
// error. Coexists with RecordSuccess/RecordFailure for callers that already
// hold the verdict.
func (o *Operation) Record(ctx context.Context, ts time.Time, payload interface{}, dims Dims) error {
	if time.Since(ts) > o.EvalWindow {
		return nil
	}
	if o.eval == nil {
		return fmt.Errorf("%w: %q has no Eval; use RecordSuccess/RecordFailure", ErrNoEval, o.Name)
	}
	outcome := OutcomeFailure
	if o.eval(payload) {
		outcome = OutcomeSuccess
	}
	return o.record(ctx, ts, outcome, dims)
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

// Success and Fail are the integration surface: an external caller records the
// outcome of a just-completed call, no timestamp to supply. Pass nil dims when
// the operation has no dimensions.
func (o *Operation) Success(ctx context.Context, dims Dims) error {
	return o.record(ctx, time.Now(), OutcomeSuccess, dims)
}

func (o *Operation) Fail(ctx context.Context, dims Dims) error {
	return o.record(ctx, time.Now(), OutcomeFailure, dims)
}
