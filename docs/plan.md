# Distributed Failure-Rate Circuit Breaker — Design Doc

**Status:** Draft
**Scope:** Sliding-window failure-rate calculation and policy-driven circuit breaking across a multi-pod fleet, with a config control plane.

---

## 1. Problem

Multiple service pods (order of 20, but unbounded) call shared downstream dependencies. We need to:

- Track success/failure counts per protected target over a configurable trailing window (single-digit seconds to single-digit minutes).
- Expose exact success count, failure count, and computed failure rate at any time.
- Drive a circuit breaker off the aggregated failure rate, so the fleet reacts as a whole, not per-pod-in-isolation.
- Let operators (and later an ML tuner) adjust policy — window size, thresholds, actions, and the definition of "failure" itself — without redeploying the fleet.
- Keep working when dependencies of *this* system fail. Neither Redis nor the control plane going down may take the protected traffic down with it.

Non-goals for v1: coordinated probe-based recovery, PID-style continuous control, and the full anti-oscillation ramp (deferred — see §11).

---

## 2. Architecture Overview

Three planes, deliberately decoupled by availability requirement.

- **Control plane** — a deployment plus a config database. Owns *policy*: eval window, bucket granularity, thresholds, minimum-volume floors, the success/failure predicate, and action mapping. It is **not** in the runtime decision path and must be killable without affecting traffic.
- **Data plane** — Redis. Holds the per-target bucket counters. The only shared runtime state. Chosen for atomic `INCR`, which is what actually makes fleet-wide aggregation work without pods gossiping.
- **SDK** — embedded in each pod. Records events, reads aggregated counts, evaluates policy locally, and enforces the breaker locally. Owns a warm local fallback buffer.

The guiding principle: **the SDK makes every runtime decision locally, off shared inputs.** Neither shared service is on the critical path of a per-request breaker decision.

```
   ┌─────────────────┐        polls config         ┌──────────────────┐
   │  Control Plane  │◄────────(relaxed)───────────│                  │
   │  (deploy + DB)  │────────ships config────────►│      SDK         │
   └─────────────────┘                             │  (in each pod)   │
                                                    │                  │
   ┌─────────────────┐   INCR / MGET (per event)   │  - predicate     │
   │      Redis      │◄────────────────────────────│  - ring buffer   │
   │ (bucket counts) │────────counts──────────────►│  - local buffer  │
   └─────────────────┘                             │  - breaker       │
                                                    └──────────────────┘
```

---

## 3. Failure-Rate Data Structure

### 3.1 Ring buffer of time-bucketed counters

The window is divided into **N sub-buckets**. Each bucket holds a success count, a failure count, and a **sequence number** identifying which slice of wall-clock time it currently represents.

- `bucketDuration = windowSize / N`
- `seq = eventTimestamp / bucketDuration` — a strictly increasing integer identifying the global time-slice. Never resets.
- `bucketIndex = seq % N` — which physical array slot the slice maps to.

Because the array has N slots but `seq` grows forever, slot indices are reused every time the ring wraps (`seq` and `seq + N` land in the same slot). A slot from a previous lap is stale and must be reset before reuse.

This gives **exact counts** (no interpolation/weighting), accurate to one bucket's granularity. Higher N → finer resolution → more memory. Default N = 10.

### 3.2 Out-of-order events

Events carry their own timestamp; bucketing uses that timestamp, **not** wall-clock-at-record-time. This makes recording order-independent within the window. The stale-slot reset condition must therefore compare sequence numbers, not just check inequality:

- `event.seq > slot.seq` → slot belongs to an older lap; reset it, take over, increment.
- `event.seq == slot.seq` → same slice; increment.
- `event.seq < slot.seq` → the incoming event is older than the slot's current occupant; its slice has already aged out. **Drop it.**

Events older than the window cutoff (`event.seq < currentSeq - N + 1`) are dropped. This is correct and unavoidable — you cannot retroactively add to a window that has already slid past.

### 3.3 Dropped-event metric

Every drop (too-old, or the `event.seq < slot.seq` case) increments a **separate dropped-event counter**. This is cheap insurance against silent clock-skew problems across pods — a rise in drops signals drift or misconfiguration that would otherwise silently distort rates.

### 3.4 Read path

To compute stats over the trailing window, sum only buckets whose stored `seq` falls in `[currentSeq - N + 1, currentSeq]`. Buckets with older `seq` are logically expired and excluded even if not yet overwritten. Failure rate = `failure / (success + failure)`, defined as 0 when total is 0.

---

## 4. Data Plane (Redis)

### 4.1 Model — one key per bucket, TTL-managed

Do **not** serialize a ring-buffer object into Redis. Store each bucket as its own key and let Redis expiry handle recycling:

```
succ:{target}:{seq}   → integer counter
fail:{target}:{seq}   → integer counter
```

Write path per event:

1. `seq = eventTimestamp / bucketDuration`
2. If within cutoff: `INCR {succ|fail}:{target}:{seq}` and set `EXPIRE` on the key.
3. If outside cutoff: drop, increment local dropped-event metric.

This sidesteps the manual stale-slot reset entirely: each `seq` is its own key, `INCR` is commutative (order-independent), and expired buckets vanish via TTL.

**TTL must exceed the window** by at least one bucket-width of margin. A TTL shorter than the window would delete buckets still needed for the sum.

### 4.2 Read path

- `currentSeq = now / bucketDuration`
- Construct the N keys for `[currentSeq - N + 1, currentSeq]`, one `MGET` per counter type.
- Sum, divide.

`MGET` across buckets is **not** an atomic snapshot — a counter may increment mid-read. Acceptable: the rate is a moving target anyway. If an atomic snapshot is ever required, use a Lua `EVAL` reading all buckets server-side. Not needed for v1.

### 4.3 Why Redis

The real reason is **atomic distributed `INCR`**, not storage. 20 pods hammering one counter is exactly Redis's strength — single-threaded, no lock contention in our code, no gossip. Storage is incidental.

### 4.4 Known edge — hot keys

A dominant-traffic target concentrates all writes on one key per bucket. Redis tolerates high single-key `INCR` throughput well. If it ever saturates, shard the counter (`fail:{target}:{seq}:{podId % K}`) and sum shards on read. **Do not build this until measured.**

### 4.5 Clock skew

Each pod derives `seq` from its own clock. Skew beyond a bucket-width scatters events into neighbouring buckets. With single-digit-minute windows (and correspondingly wide buckets), NTP-level skew is negligible. Accepted for v1.

---

## 5. Local Fallback Buffer

The SDK **always** maintains an in-memory ring buffer (§3) in parallel with writing to Redis — it is cheap (just counters) and kept warm continuously. It is only *read from* when Redis is unreachable or unresponsive.

- Always-warm means failover has **no cold-start blind window**.
- Local data reflects only *one pod's* traffic slice: low-volume, high-variance. Its failure rate is inherently noisier than the aggregated Redis view.

### 5.1 Two config sets

The control plane ships **two policy sets**:

- **Redis-backed (strict)** — used when the aggregated view is available.
- **Local-fallback (relaxed)** — used during Redis outage.

The relaxed set is *not* merely a looser threshold. Because local data is low-volume/high-variance, the relaxed set primarily raises the **minimum-volume floor relative to per-pod traffic** and biases the action toward **availability** (lean toward *not* tripping on thin evidence). A false trip during a Redis outage stacks a second failure on top of the first.

---

## 6. Control Plane

### 6.1 Responsibility

Ships configuration to SDKs. **Never** makes runtime decisions. Killable without traffic impact. Its purpose is config distribution without redeploys — toggling actions, retuning thresholds, changing the failure definition.

### 6.2 Polling and fail-open

- SDK polls on a **relaxed interval** (policy changes propagate slowly by design; this is acceptable and also bounds how fast an ML tuner can thrash configs).
- SDK caches the last good config locally.
- On control-plane unreachability *after* first fetch: keep using cached config.
- On unreachability *at startup* (no cached config): fall back to a **safe default baked into the SDK** — fail-open (allow all traffic, no breaking). A monitoring system must never be the reason a fresh pod stops traffic.

### 6.3 Config versioning

Every config carries a **timestamped version/generation**. Purposes:

- Debugging — identify which pods run which policy ("why is pod 7 different").
- ML attribution — the version is the **join key** between "config vN was active over interval T" and the observed rates/traffic outcomes. It must be a first-class timestamped record, not just a counter, so impact can be attributed.

### 6.4 Config schema (v1)

- Eval window size, bucket count (N).
- Failure-rate threshold(s).
- **Minimum-volume floor** — required. A 50% rate off 2 requests is noise; do not act until total ≥ floor. Without it, the breaker flaps at low traffic.
- Success/failure predicate (§7).
- Predicate eval-error outcome (§7.3 — fixed to SUCCESS for v1, but modelled as config).
- Action mapping (what a trip does — reject / degrade / shed, and the allowed fraction at each rate level).
- The two config sets (strict / relaxed) of the above.

---

## 7. Success / Failure Definition (Predicate Layer)

### 7.1 The narrow waist

Everything downstream of the predicate — ring buffer, Redis, breaker — sees **only a boolean**. *How* a service produces that boolean is a pluggable front-end adapter, selected per-target by config. This boundary keeps the counting/breaking logic untouched regardless of transport.

### 7.2 Adapters

- **JSON / HTTP targets → CEL-over-JSON.** The predicate (e.g. `status == "success"`) is shipped from the control plane and evaluated against the self-describing JSON payload. Full retune-without-deploy. CEL is chosen because it is constrained, sandboxed, non-Turing-complete, fast, and battle-tested — **do not invent a bespoke expression language**.
- **Proto / gRPC targets → SDK ships the verdict (default).** The RPC caller already holds the typed response struct, so the predicate is written in code against the concrete type (`resp.Status != FAILED`). The SDK records a bare success/fail. Fastest, type-safe, no reflection, no descriptor management. Trade-off: changing the failure definition for a proto target requires an SDK redeploy — the config-driven-predicate superpower is lost *for proto targets specifically*.
- **Proto targets wanting config-driven predicates → CEL-over-proto (opt-in).** `cel-go` evaluates over `proto.Message` natively (no JSON round-trip) if the SDK has the message descriptor registered. Offered for teams willing to manage descriptor distribution.

Rejected: proto→JSON marshal on every event (most expensive, on the hot path).

### 7.3 Internal-error handling — fail to SUCCESS

**Any** internal error in producing the boolean — CEL eval failure, missing field, type mismatch, a panic/nil-pointer in a hand-written proto predicate — is treated as **SUCCESS**. We must never block traffic on our own bug. This is enforced by the **framework wrapping the adapter** (including a `recover()` around user-supplied predicates), not left to each adapter to remember.

### 7.4 Eval-error metric — mandatory companion

"Fail-open on error" silently becomes "fail-silent on error": a broken predicate makes every event read as success, failure rate reads 0%, and the breaker sleeps exactly when it might be needed. Therefore a **separate eval-error counter** is emitted independently of the success/fail stream. The breaker acts on success/fail as designed; a spike in eval-errors is independently visible and alertable. Small cost, essential visibility.

---

## 8. Circuit Breaker Behaviour

### 8.1 Deterministic, continuous partial-open

Not the classic closed/open/half-open state machine. Instead a continuous function: the config maps failure rate to an **allowed-traffic fraction**. Example: at 50% failure rate, allow 10% of traffic; the rest receive an immediate error.

- Every pod reads the same aggregated rate from Redis and applies the same function → pods converge automatically. **Breaker state is per-pod; no global coordination needed.**
- Recovery is *not* probe-based. As the shared rate drifts down, the allowed fraction rises smoothly and simultaneously across the fleet. The allowed fraction *is* the continuous probe, spread proportionally across all pods.
- This avoids the thundering-herd-on-half-open problem entirely: there is no discrete probe moment for 20 pods to pounce on a fragile downstream.

### 8.2 Per-request admission

When the allowed fraction is < 100%, each pod admits requests by **random sampling per request** to the allowed fraction. Random (not deterministic-per-request) so the same requests aren't always the ones let through, and so aggregate admitted traffic ≈ the target fraction.

### 8.3 Minimum-volume floor

The breaker does not act until total volume ≥ the configured floor, regardless of rate. Prevents flapping on statistically meaningless samples.

---

## 9. Failure Modes Summary

| Failure | Behaviour |
|---|---|
| Control plane down (after first fetch) | SDK runs on cached config. No traffic impact. |
| Control plane down (at pod startup) | SDK runs baked-in safe default; fail-open. |
| Redis down | SDK reads warm local buffer; switches to relaxed config set; biases toward availability. |
| Predicate eval error / panic | Treated as SUCCESS; separate eval-error metric emitted. |
| Event older than window | Dropped; dropped-event metric incremented. |
| Clock skew < bucket-width | Negligible; accepted. |
| Hot Redis key | Tolerated; shard-on-read available if measured to saturate (not built v1). |

---

## 10. Observability

Emit as independent signals:

- Success / failure counts and computed rate per target.
- **Allowed-traffic fraction** as a time series — required to *see* breaker behaviour and oscillation; invisible if only the rate is logged. Also the primary ML training signal.
- Dropped-event counter (clock-skew / late-event visibility).
- Eval-error counter (broken-predicate visibility).
- Active config version per pod.

---

## 11. Deferred — Oscillation Control

The rate→fraction loop is a feedback control loop and will oscillate (sawtooth between wide-open and clamped) without damping, due to measurement lag (trailing window), actuation lag (downstream recovers slowly), and lack of damping. Mitigations identified but **deferred to a future iteration**:

- **Hysteresis** — separate thresholds for tightening vs loosening (a dead band).
- **Slew-rate limiting** — cap how fast the allowed fraction may rise; ramp up and watch each step (AIMD / TCP-congestion-control model: additive-increase, multiplicative-decrease; fast to clamp, slow to release).
- **EWMA input smoothing** — damp noise, but sparingly (smoothing adds lag).
- **Minimum dwell time** — bound oscillation frequency.

These damping parameters are the natural knobs for the ML config loop to own and tune per-target, attributed via config version. Explicitly avoid a full PID controller for v1 — the derivative term on a noisy windowed rate misbehaves and the integral term winds up during sustained outages. Start discrete + hysteresis + slew-limit; reach for continuous control only if that proves insufficient.

**Two invariants to preserve when this is built:** asymmetry (fast to clamp, slow to release) and observability of the allowed fraction (§10).

---

## 12. Open Items

- Consumer read pattern: a per-request breaker check should cache the aggregated rate locally for ~1 bucket-width rather than hitting Redis per request; a dashboard poll is different. Caching strategy to be specified.
- Predicate adapter interface — concrete signature and registration mechanism.
- Exact ML config-loop contract (how impact is measured and fed back).
- Oscillation control (§11).
