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
}

type Operation struct {
	Namespace  Namespace
	Name       string
	EvalWindow time.Duration
	Resolution time.Duration
	store      storage.Storage // todo: find a better way, feels counter intuitive
	metric     *prometheus.CounterVec
}

func (o *Operation) bucket(ts time.Time) int64 {
	// Nanosecond division so sub-second resolutions don't truncate to a
	// divide-by-zero. Same divisor is used on read and write.
	return ts.UnixNano() / o.Resolution.Nanoseconds()
}

func (o *Operation) key(outcome Outcome, seq int64) string {
	return fmt.Sprintf("%s:%s:%s:%d", o.Namespace.Name, o.Name, outcome, seq)
}

func (o *Operation) record(ctx context.Context, ts time.Time, outcome Outcome) error {
	o.metric.WithLabelValues(o.Namespace.Name, o.Name, string(outcome)).Inc()
	return o.store.IncNumberWithEx(ctx, o.key(outcome, o.bucket(ts)), time.Now().Add(o.EvalWindow))
}

func (o *Operation) SuccessRate(ctx context.Context) (float64, error) {
	buckets := int(o.EvalWindow / o.Resolution)
	currentSeq := o.bucket(time.Now())

	var success, failure int64
	// i <= buckets: the current bucket is partial, so read one extra older
	// bucket to cover the full EvalWindow instead of 55-60s of it.
	// ponytail: slight tail over-coverage, fine for a health ratio.
	for i := 0; i <= buckets; i++ {
		seq := currentSeq - int64(i)
		s, err := o.store.GetNumber(ctx, o.key(OutcomeSuccess, seq))
		if err != nil {
			return 0, err
		}
		f, err := o.store.GetNumber(ctx, o.key(OutcomeFailure, seq))
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

func (c *Client) RegisterOperation(opts *NewOpsOpts) *Operation {
	ops := &Operation{
		Name:       opts.Name,
		Namespace:  opts.NameSpace,
		EvalWindow: opts.EvalWindow,
		Resolution: opts.Resolution,
		store:      c.store,
		metric:     c.metricOps,
	}
	c.state.Operations[opts.NameSpace][opts.Name] = ops
	return ops
}

func (o *Operation) RecordSuccess(ctx context.Context, ts time.Time) error {
	if time.Since(ts) > o.EvalWindow {
		return nil
	}
	return o.record(ctx, ts, OutcomeSuccess)
}

func (o *Operation) RecordFailure(ctx context.Context, ts time.Time) error {
	if time.Since(ts) > o.EvalWindow {
		return nil
	}
	return o.record(ctx, ts, OutcomeFailure)
}
