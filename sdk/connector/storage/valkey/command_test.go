package valkey

import (
	"context"
	"errors"
	"testing"
	"time"
)

const testConnStr = "redis://localhost:6379"

func newTestImpl(t *testing.T) *Impl {
	t.Helper()
	impl, err := New(&ValkeyOpts{ConnStr: testConnStr, KeyPrefix: "test:"})
	if err != nil {
		if errors.Is(err, ErrCapabilityMissing) {
			t.Fatalf("valkey version too old: %v", err)
		}
		t.Skipf("valkey unreachable: %v", err)
	}
	return impl
}

func TestNew(t *testing.T) {
	t.Run("valid DSN", func(t *testing.T) {
		impl, err := New(&ValkeyOpts{ConnStr: testConnStr, KeyPrefix: "pfx:"})
		if err != nil {
			t.Skipf("valkey unavailable: %v", err)
		}
		if impl == nil {
			t.Fatal("expected non-nil Impl")
		}
	})

	t.Run("invalid DSN", func(t *testing.T) {
		_, err := New(&ValkeyOpts{ConnStr: "not-a-url"})
		if err == nil {
			t.Fatal("expected error for invalid DSN")
		}
	})
}

func TestKey(t *testing.T) {
	t.Run("prepends prefix", func(t *testing.T) {
		impl := &Impl{prefix: "myprefix:"}
		if got := impl.key("foo"); got != "myprefix:foo" {
			t.Fatalf("want %q, got %q", "myprefix:foo", got)
		}
	})

	t.Run("empty prefix", func(t *testing.T) {
		impl := &Impl{prefix: ""}
		if got := impl.key("foo"); got != "foo" {
			t.Fatalf("want %q, got %q", "foo", got)
		}
	})
}

func TestSet(t *testing.T) {
	impl := newTestImpl(t)
	ctx := context.Background()

	t.Run("stores value", func(t *testing.T) {
		if err := impl.set("set_test", "hello"); err != nil {
			t.Fatalf("set: %v", err)
		}

		cmd := impl.client.B().Get().Key(impl.key("set_test")).Build()
		val, err := impl.client.Do(ctx, cmd).ToString()
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if val != "hello" {
			t.Fatalf("want %q, got %q", "hello", val)
		}
	})

	t.Run("overwrites existing value", func(t *testing.T) {
		_ = impl.set("overwrite_test", "first")
		if err := impl.set("overwrite_test", "second"); err != nil {
			t.Fatalf("set: %v", err)
		}

		cmd := impl.client.B().Get().Key(impl.key("overwrite_test")).Build()
		val, err := impl.client.Do(ctx, cmd).ToString()
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if val != "second" {
			t.Fatalf("want %q, got %q", "second", val)
		}
	})
}

func TestGetNumber(t *testing.T) {
	impl := newTestImpl(t)
	ctx := context.Background()

	t.Run("reads existing number", func(t *testing.T) {
		seed := impl.client.B().Set().Key(impl.key("get_number_test")).Value("42").Build()
		if err := impl.client.Do(ctx, seed).Error(); err != nil {
			t.Fatalf("setup: %v", err)
		}

		val, err := impl.GetNumber(ctx, "get_number_test")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if val != 42 {
			t.Fatalf("want 42, got %d", val)
		}
	})

	t.Run("missing key returns error", func(t *testing.T) {
		_, err := impl.GetNumber(ctx, "nonexistent_key_xyz")
		if err == nil {
			t.Fatal("expected error for missing key")
		}
	})
}

func TestIncNumberWithEx(t *testing.T) {
	impl := newTestImpl(t)
	ctx := context.Background()
	expiry := time.Now().Add(10 * time.Second)

	// ponytail: INCREX is non-standard — these subtests document the bug
	t.Run("increments from zero", func(t *testing.T) {
		if err := impl.IncNumberWithEx(ctx, "inc_zero_test", expiry); err != nil {
			t.Fatalf("IncNumberWithEx: %v", err)
		}
		val, err := impl.GetNumber(ctx, "inc_zero_test")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if val != 1 {
			t.Fatalf("want 1, got %d", val)
		}
	})

	t.Run("accumulates on repeated calls", func(t *testing.T) {
		_ = impl.IncNumberWithEx(ctx, "inc_accum_test", expiry)
		if err := impl.IncNumberWithEx(ctx, "inc_accum_test", expiry); err != nil {
			t.Fatalf("IncNumberWithEx: %v", err)
		}
		val, err := impl.GetNumber(ctx, "inc_accum_test")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if val != 2 {
			t.Fatalf("want 2, got %d", val)
		}
	})
}
