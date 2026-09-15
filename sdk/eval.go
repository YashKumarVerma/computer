package sdk

import (
	"fmt"
	"strings"
)

// Eval is the narrow waist of the predicate layer: everything downstream — ring
// buffer, store, breaker — sees only this bool. How the bool is produced (typed
// code, CEL-over-JSON, ...) is a pluggable adapter selected per target. See the
// design doc §7.
type Eval func(interface{}) bool

// guard wraps a raw predicate to enforce two design-doc policies:
//
//	§7.3  any panic/nil-deref while producing the bool resolves to SUCCESS — we
//	      never block traffic on our own bug.
//	§7.4  every recovered error fires onErr (increments a separate eval-error
//	      counter) so a broken predicate stays visible instead of silently
//	      reading 100% success.
//
// Adapters funnel their own soft errors (e.g. a CEL eval failure returning
// (val, error)) into a panic, so panic is the single error channel the harness
// has to watch. Compile-time errors (a bad spec) are NOT fail-open — they
// surface at registration, see Adapter.
func guard(raw Eval, onErr func()) Eval {
	return func(v interface{}) (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				onErr()
				ok = true // fail to SUCCESS
			}
		}()
		return raw(v)
	}
}

// Adapter compiles a config-shipped predicate spec into an Eval. One adapter per
// payload family (typed-code targets skip this and pass an Eval directly). The
// compile step runs once at registration, so a bad spec fails loud there rather
// than failing-open on the hot path.
type Adapter func(spec string) (Eval, error)

// fieldEqAdapter compiles specs like "status=success" against a map payload.
// ponytail: single operator, no AND/OR — a stand-in for the real config path.
// Swap the body for cel-go's Compile+Program and nothing else changes; do not
// grow this into a DSL (design doc §7.2 — "do not invent a bespoke language").
func fieldEqAdapter(spec string) (Eval, error) {
	field, want, ok := strings.Cut(spec, "=")
	if !ok {
		return nil, fmt.Errorf("eval spec %q: want field=value", spec)
	}
	return func(v interface{}) bool {
		m, ok := v.(map[string]interface{})
		if !ok {
			panic(fmt.Sprintf("fieldEq: want map payload, got %T", v)) // → guard → SUCCESS + metric
		}
		return fmt.Sprint(m[field]) == want
	}, nil
}
