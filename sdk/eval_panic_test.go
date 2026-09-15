package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
)

// counterValue reads one counter out of a CounterVec.
// ponytail: hand-rolled instead of prometheus/testutil — testutil drags in a
// new module (kylelemons/godebug); this uses deps already in the graph.
func counterValue(t *testing.T, vec *prometheus.CounterVec, labels ...string) float64 {
	t.Helper()
	var m dto.Metric
	if err := vec.WithLabelValues(labels...).Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetCounter().GetValue()
}

// newTestClient is Init() minus the network: same metric wiring, fake store.
// ponytail: struct literal, not a testable Init() seam — one caller.
func newTestClient(store *fakeStore) *Client {
	reg := prometheus.NewRegistry()
	return &Client{
		state:          state{Operations: make(map[Namespace]map[string]*Operation)},
		store:          store,
		metricRegistry: reg,
		metricOps: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "internal", Subsystem: "operation", Name: "invocation",
		}, []string{"namespace", "operation", "outcome"}),
		metricEvalErr: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "internal", Subsystem: "operation", Name: "eval_error",
		}, []string{"namespace", "operation"}),
	}
}

// The guard is per-call, not sticky: one panicking payload must not poison the
// predicate for the payloads that follow it (design doc §7.3).
func TestGuardFieldEqPanicPath(t *testing.T) {
	ev, err := fieldEqAdapter("status=success")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	g := guard(ev, func() { calls++ })

	// Non-map payload → fieldEq panics → SUCCESS + one eval-error (§7.4).
	if !g(42) {
		t.Fatal("map-mismatch panic must fail to SUCCESS")
	}
	if calls != 1 {
		t.Fatalf("want 1 eval-error, got %d", calls)
	}

	// The next payload evaluates normally.
	if g(map[string]interface{}{"status": "failed"}) {
		t.Fatal("guard stayed open after recovering a panic")
	}
	if calls != 1 {
		t.Fatalf("onErr fired on a clean call: %d", calls)
	}
}

// End-to-end: a predicate that panics must not break Record for the caller —
// no panic escapes, no error is returned, the event is still recorded (as
// SUCCESS), and the breakage is visible on the eval_error counter.
func TestRecordSurvivesPanickingEval(t *testing.T) {
	ctx := context.Background()
	store := &fakeStore{m: map[string]int64{}}
	c := newTestClient(store)
	ns := c.RegisterNamespace("ns")

	ev, err := fieldEqAdapter("status=success")
	if err != nil {
		t.Fatal(err)
	}
	op := c.RegisterOperation(&NewOpsOpts{
		NameSpace:  ns,
		Name:       "op",
		EvalWindow: time.Minute,
		Resolution: 5 * time.Second,
		Dimensions: []string{"bank"},
		Eval:       ev,
	})

	now := time.Now()
	// A non-map payload is the realistic trigger: config says "status=success",
	// the caller ships a string.
	if err := op.Record(ctx, now, "not-a-map", Dims{"bank": "bankA"}); err != nil {
		t.Fatalf("Record surfaced the predicate panic: %v", err)
	}

	seq := op.bucket(now)
	if got := store.m[op.key(OutcomeSuccess, seq)]; got != 1 {
		t.Fatalf("fail-open write missing: want 1 success, got %d", got)
	}
	if got := store.m[op.key(OutcomeFailure, seq)]; got != 0 {
		t.Fatalf("panic recorded as failure: %d", got)
	}
	if got := store.m[op.dimKey("bank", "bankA", OutcomeSuccess, seq)]; got != 1 {
		t.Fatalf("dimension counter missing: want 1, got %d", got)
	}
	if got := counterValue(t, c.metricEvalErr, "ns", "op"); got != 1 {
		t.Fatalf("want 1 eval_error, got %v", got)
	}

	// The rest of the code keeps working: reads succeed and report healthy,
	// which is precisely why the eval_error counter above has to be alarmed on.
	rate, err := op.SuccessRate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rate != 1 {
		t.Fatalf("want 1 after fail-open, got %v", rate)
	}

	// A clean payload on the same op still evaluates for real.
	if err := op.Record(ctx, now, map[string]interface{}{"status": "failed"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.m[op.key(OutcomeFailure, seq)]; got != 1 {
		t.Fatalf("op broken after a panicking eval: want 1 failure, got %d", got)
	}
	if got := counterValue(t, c.metricEvalErr, "ns", "op"); got != 1 {
		t.Fatalf("eval_error incremented on a clean eval: %v", got)
	}
}
