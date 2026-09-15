package sdk

import "testing"

func TestGuard(t *testing.T) {
	calls := 0

	// A clean predicate passes through untouched, no eval-error.
	g := guard(func(interface{}) bool { return false }, func() { calls++ })
	if g(nil) {
		t.Fatal("guard altered a clean false")
	}
	if calls != 0 {
		t.Fatalf("onErr fired without an error: %d", calls)
	}

	// A panicking predicate fails to SUCCESS and fires exactly one eval-error
	// (design doc §7.3 / §7.4).
	g = guard(func(interface{}) bool { panic("boom") }, func() { calls++ })
	if !g(nil) {
		t.Fatal("panic must fail to SUCCESS")
	}
	if calls != 1 {
		t.Fatalf("want 1 eval-error, got %d", calls)
	}
}

func TestFieldEqAdapter(t *testing.T) {
	ev, err := fieldEqAdapter("status=success")
	if err != nil {
		t.Fatal(err)
	}
	if !ev(map[string]interface{}{"status": "success"}) {
		t.Fatal("status=success should be true")
	}
	if ev(map[string]interface{}{"status": "failed"}) {
		t.Fatal("status=failed should be false")
	}

	// Bad spec must fail at compile, not silently fail-open at eval time.
	if _, err := fieldEqAdapter("garbage"); err == nil {
		t.Fatal("bad spec must error at compile")
	}
}
