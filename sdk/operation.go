package sdk

import (
	"context"
	"fmt"
	"time"

	"github.com/yashkumarverma/computer/sdk/connector/storage"
)

type OpsType string

const (
	SuccessOps OpsType = "success"
	FailureOps OpsType = "failure"
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
}

func (o *Operation) record(ctx context.Context, ts time.Time, outcome OpsType) error {
	sequence := ts.Unix() / int64(o.Resolution.Seconds())
	key := fmt.Sprintf("%s:%s:%s:%d", o.Namespace, o.Name, outcome, sequence)
	return o.store.IncNumberWithEx(ctx, key, time.Now().Add(o.EvalWindow))
}

func (o *Operation) SuccessRate(ctx context.Context) (float64, error) {
	buckets := int(o.EvalWindow / o.Resolution)
	currentSeq := time.Now().Unix() / int64(o.Resolution.Seconds())

	var success, failure int64
	for i := range buckets {
		seq := currentSeq - int64(i)
		s, err := o.store.GetNumber(ctx, fmt.Sprintf("%s:%s:%s:%d", o.Namespace, o.Name, SuccessOps, seq))
		if err == nil {
			success += s
		}
		f, err := o.store.GetNumber(ctx, fmt.Sprintf("%s:%s:%s:%d", o.Namespace, o.Name, FailureOps, seq))
		if err == nil {
			failure += f
		}
	}

	total := success + failure
	if total == 0 {
		return 1, nil
	}
	return float64(success) / float64(total), nil
}

func (c *Client) RegisterOperation(opts *NewOpsOpts) *Operation {
	n := int(opts.EvalWindow / opts.Resolution)
	ops := &Operation{
		Name:       opts.Name,
		Namespace:  opts.NameSpace,
		EvalWindow: opts.EvalWindow,
		Resolution: opts.Resolution,
	}
	_ = n
	c.state.Operations[opts.NameSpace][opts.Name] = ops
	return ops
}

func (o *Operation) RecordSuccess(ctx context.Context, ts time.Time) error {
	if time.Since(ts) > o.EvalWindow {
		return nil
	}
	return o.record(ctx, ts, SuccessOps)
}

func (o *Operation) RecordFailure(ctx context.Context, ts time.Time) error {
	if time.Since(ts) > o.EvalWindow {
		return nil
	}
	return o.record(ctx, ts, FailureOps)
}
