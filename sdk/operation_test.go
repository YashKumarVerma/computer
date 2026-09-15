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
		dimensions: map[string]struct{}{"bank": {}},
	}
}

func TestSuccessRate(t *testing.T) {
	ctx := context.Background()

	t.Run("basic ratio", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		now := time.Now()
		for range 3 {
			if err := op.RecordSuccess(ctx, now, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := op.RecordFailure(ctx, now, nil); err != nil {
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
		if err := op.RecordSuccess(ctx, now, nil); err != nil {
			t.Fatal(err)
		}
		if err := op.record(ctx, now.Add(-2*op.EvalWindow), OutcomeFailure, nil); err != nil {
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

	// Marginal slices are isolated per dimension value and don't affect the
	// overall rate's ratio.
	t.Run("per-dimension marginal rate", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		now := time.Now()
		// bankA: 3 success / 1 failure = 0.75. bankB: 1 success / 1 failure = 0.5.
		for range 3 {
			if err := op.RecordSuccess(ctx, now, Dims{"bank": "bankA"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := op.RecordFailure(ctx, now, Dims{"bank": "bankA"}); err != nil {
			t.Fatal(err)
		}
		if err := op.RecordSuccess(ctx, now, Dims{"bank": "bankB"}); err != nil {
			t.Fatal(err)
		}
		if err := op.RecordFailure(ctx, now, Dims{"bank": "bankB"}); err != nil {
			t.Fatal(err)
		}

		a, err := op.SuccessRateBy(ctx, "bank", "bankA")
		if err != nil {
			t.Fatal(err)
		}
		if a != 0.75 {
			t.Fatalf("bankA: want 0.75, got %v", a)
		}
		b, err := op.SuccessRateBy(ctx, "bank", "bankB")
		if err != nil {
			t.Fatal(err)
		}
		if b != 0.5 {
			t.Fatalf("bankB: want 0.5, got %v", b)
		}
		// Overall is the union: 4 success / 6 total.
		overall, err := op.SuccessRate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if overall != 4.0/6.0 {
			t.Fatalf("overall: want %v, got %v", 4.0/6.0, overall)
		}
	})

	// FailureRates spreads over the values seen per dimension with no caller-
	// supplied value list, and each rate is 1 - the per-value success rate.
	t.Run("failure rates spread across observed dimension values", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		now := time.Now()
		// bankA: 3 success / 1 failure -> 0.25 failure. bankB: 1/1 -> 0.5.
		for range 3 {
			if err := op.RecordSuccess(ctx, now, Dims{"bank": "bankA"}); err != nil {
				t.Fatal(err)
			}
		}
		for _, d := range []Dims{{"bank": "bankA"}, {"bank": "bankB"}} {
			if err := op.RecordFailure(ctx, now, d); err != nil {
				t.Fatal(err)
			}
		}
		if err := op.RecordSuccess(ctx, now, Dims{"bank": "bankB"}); err != nil {
			t.Fatal(err)
		}

		rates, err := op.FailureRates(ctx)
		if err != nil {
			t.Fatal(err)
		}
		bank := rates["bank"]
		if len(bank) != 2 {
			t.Fatalf("want 2 observed bank values, got %v", bank)
		}
		if bank["bankA"] != 0.25 {
			t.Fatalf("bankA failure: want 0.25, got %v", bank["bankA"])
		}
		if bank["bankB"] != 0.5 {
			t.Fatalf("bankB failure: want 0.5, got %v", bank["bankB"])
		}
	})

	// No traffic reads as zero failure (SuccessRate is 1.0 on silence) and an
	// operation nobody has tagged yet spreads to nothing, not a panic.
	t.Run("failure rate on silence is zero and empty spread", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		fr, err := op.FailureRate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if fr != 0 {
			t.Fatalf("silent failure rate: want 0, got %v", fr)
		}
		rates, err := op.FailureRates(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rates) != 0 {
			t.Fatalf("want empty spread with no traffic, got %v", rates)
		}
	})

	// Undeclared dimensions are rejected on both write and read so cardinality
	// stays bounded to what was registered.
	t.Run("undeclared dimension is rejected", func(t *testing.T) {
		op := newOp(&fakeStore{m: map[string]int64{}})
		err := op.RecordSuccess(ctx, time.Now(), Dims{"user_id": "u123"})
		if !errors.Is(err, ErrUnknownDimension) {
			t.Fatalf("record: want ErrUnknownDimension, got %v", err)
		}
		if _, err := op.SuccessRateBy(ctx, "user_id", "u123"); !errors.Is(err, ErrUnknownDimension) {
			t.Fatalf("read: want ErrUnknownDimension, got %v", err)
		}
	})
}
