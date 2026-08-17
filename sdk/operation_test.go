package sdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// fakeStore is an in-process Storage. err, when set, is returned by every
// GetNumber to simulate a backend outage.
type fakeStore struct {
	m   map[string]int64
	err error
}

func (f *fakeStore) IncNumberWithEx(_ context.Context, key string, _ time.Time) error {
	f.m[key]++
	return nil
}

func (f *fakeStore) GetNumber(_ context.Context, key string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	// Contract matches the valkey layer: a missing bucket is 0, not an error.
	return f.m[key], nil
}

func newOp(store *fakeStore) *Operation {
	return &Operation{
		Namespace:  Namespace{Name: "ns"},
		Name:       "op",
		EvalWindow: time.Minute,
		Resolution: 5 * time.Second,
		store:      store,
		metric: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "test_invocation"},
			[]string{"namespace", "operation", "outcome"},
		),
	}
}

func TestSuccessRate(t *testing.T) {
	ctx := context.Background()

	t.Run("basic ratio", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		now := time.Now()
		for range 3 {
			if err := op.RecordSuccess(ctx, now); err != nil {
				t.Fatal(err)
			}
		}
		if err := op.RecordFailure(ctx, now); err != nil {
			t.Fatal(err)
		}
		got, err := op.SuccessRate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got != 0.75 {
			t.Fatalf("want 0.75, got %v", got)
		}
	})

	t.Run("no data defaults to 1", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		got, err := op.SuccessRate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got != 1 {
			t.Fatalf("want 1, got %v", got)
		}
	})

	t.Run("events outside window are excluded from the read", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		now := time.Now()
		// One recent success, one failure two windows in the past written
		// straight through record() (RecordFailure would have dropped it).
		if err := op.RecordSuccess(ctx, now); err != nil {
			t.Fatal(err)
		}
		if err := op.record(ctx, now.Add(-2*op.EvalWindow), OutcomeFailure); err != nil {
			t.Fatal(err)
		}
		got, err := op.SuccessRate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got != 1 {
			t.Fatalf("stale failure leaked into window: want 1, got %v", got)
		}
	})

	// A store outage must surface as an error, not a masked 100% healthy.
	t.Run("storage outage propagates as an error", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}, err: errors.New("connection refused")})
		if _, err := op.SuccessRate(ctx); err == nil {
			t.Fatal("want error when store is down, got nil (outage masked as healthy)")
		}
	})
}
