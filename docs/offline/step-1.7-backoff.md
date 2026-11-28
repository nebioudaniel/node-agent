# Step 1.7 — Backoff with Jitter

## 1. Goal and why it exists in the system (3-5 sentences)

A retrying agent must wait before retrying a failed coordinator call. If every
agent retries immediately or after a fixed delay, a recovering coordinator
gets hit by a synchronized wave of requests (thundering herd), making recovery
harder. Exponential backoff grows the wait so transient blips retry fast but
persistent failures slow down. Jitter randomizes the wait so 20 agents never
reconnect in the same second. `Backoff` is a pure function that computes
this wait; it does not sleep, does not log, and does not touch globals — the
caller owns randomness and the cancellable sleep.

## 2. Go concepts I need first

**`time.Duration` is int64 nanoseconds.**
`time.Duration` is a named type over `int64`, representing nanoseconds. The
maximum value is about 9.2e18 ns (~292 years). Any arithmetic can overflow.

```go
var d time.Duration = 1 * time.Second // 1_000_000_000 ns
```

**Bit shifts (`<<`) and overflow.**
Shifting left multiplies by powers of two. `1 << 63` overflows `int64` and
Go panics at runtime for negative shift counts. `time.Duration(1) << attempt`
with `attempt = 100` produces garbage or a negative number.

```go
x := 1 << 3  // 8
// 1 << 64 -> compile error for constant; runtime for variable
```

**Pure functions and dependency injection.**
A function with no side effects, no global state, and all dependencies passed
as parameters. `rnd func() float64` is injected so tests control randomness
without real `rand.Rand` or sleeping.

```go
func double(x int, mul func() int) int { return x * mul() }
```

**`math/rand` vs `crypto/rand`.**
`math/rand.Float64()` returns `[0,1)` — fast, deterministic with seed, fine
for backoff jitter. `crypto/rand` is for secrets. Backoff uses `math/rand`.

```go
r := rand.New(rand.NewSource(time.Now().UnixNano()))
r.Float64() // [0, 1)
```

**Clamp / cap pattern.**
Bound a value to a range by checking bounds and replacing with the limit.

```go
if v < 0 { v = 0 }
if v > max { v = max }
```

## 3. Exact spec: types, signatures, rules

```go
func Backoff(attempt int, base, max time.Duration, rnd func() float64) time.Duration
```

Package: `agent` (directory `internal/agent/`).

Rules:
- `d = base * 2^attempt`, capped at `max`.
- Clamp `attempt`: negative → 0; huge (e.g. >30) → 30, so `1<<attempt`
  never overflows `int64`.
- Treat `d <= 0` as overflow → use `max`.
- `result = d/2 + rnd()*d/2` → always in `[d/2, d)` ⊆ `[0, max)`.
- `rnd` returns `[0,1)` (like `rand.Float64`); inject it for testability.
- Never panic, never negative, never exceed `max`.
- Do NOT sleep inside `Backoff`. Return the duration; caller sleeps.

## 4. Algorithm in plain-English numbered steps (no Go code)

1. If `attempt` is negative, treat it as 0.
2. If `attempt` is too large (greater than 30), clamp it to 30.
3. If `base` is negative, treat it as 0.
4. If `max` is negative, treat it as 0.
5. Compute `d = base * 2^attempt` using a bit shift.
6. If the shift overflowed (result is negative or less than base), set `d = max`.
7. If `d` is negative, set `d = max`.
8. If `d` exceeds `max`, set `d = max`.
9. Compute `half = d / 2` using integer division.
10. Get `r = rnd()`; if `rnd` is nil, use 0.
11. If `r` is negative, set `r = 0`.
12. If `r` is 1 or greater, set `r = 0.9999999999999999` (keep it strictly below 1).
13. Compute `jitter = time.Duration(r * float64(half))`.
14. `result = half + jitter`.
15. If `result < 0`, return 0. If `result > max`, return `max`.
16. Return `result`.

## 5. List of tests

| Test name | What it asserts | Why it matters |
|---|---|---|
| `TestBackoffJitterRange` | `rnd=0` → exactly `d/2`; `rnd=0.999999` → `> d/2` and `< d` | Verifies jitter bounds; if result is `d` or `0`, jitter is broken |
| `TestBackoffGrowthTable` | base=1s, max=30s, attempts 0–5, rnd=0 → 0.5s,1s,2s,4s,8s,15s | Verifies exponential growth + cap at attempt 5 (2^5=32→30) |
| `TestBackoffNeverExceedsMaxNeverNegative` | attempts 0–100 × rnd∈{0,.25,.5,.75,.999999}: `0 ≤ got ≤ max` | No overflow/negative for any attempt or jitter value |
| `TestBackoffNegativeAttempt` | attempt=-1: no panic, `≥0`, `≤max` | Negative shift count panics in Go; must clamp |

Tests must NOT pass trivially when `Backoff` returns 0: add `result > 0` assertion for `base > 0`.

## 6. Commands to run and exact output when done

```bash
cd ~/Desktop/node-agent
go vet ./internal/agent
go test -race ./internal/agent -run TestBackoff -v
go test -race ./...
```

Expected output:
```
=== RUN   TestBackoffJitterRange
--- PASS: TestBackoffJitterRange (0.00s)
=== RUN   TestBackoffGrowthTable
--- PASS: TestBackoffGrowthTable (0.00s)
=== RUN   TestBackoffNeverExceedsMaxNeverNegative
--- PASS: TestBackoffNeverExceedsMaxNeverNegative (0.00s)
=== RUN   TestBackoffNegativeAttempt
--- PASS: TestBackoffNegativeAttempt (0.00s)
PASS
ok  	node-agent/internal/agent	0.xx s
```

All packages ok, no FAIL.

## 7. Common mistakes and how the tests reveal them

| Mistake | Symptom | Which test catches it |
|---|---|---|
| `1 << attempt` with negative attempt | Runtime panic | `TestBackoffNegativeAttempt` |
| No cap on attempt → overflow | Negative result or huge value | `TestBackoffNeverExceedsMaxNeverNegative` |
| No cap on d → exceeds max | `got > max` | `TestBackoffNeverExceedsMaxNeverNegative` |
| `d <= 0` not handled | Negative/zero result | `TestBackoffNeverExceedsMaxNeverNegative` |
| Result = `rnd()*d` (full) | At rnd=0.999999, result ≈ d, not "just under d" | `TestBackoffJitterRange` |
| Result = `d` (no jitter) | rnd=0 gives d, not d/2 | `TestBackoffJitterRange` |
| Result = `d/2` (no jitter) | rnd=0.999999 gives d/2, not ~d | `TestBackoffJitterRange` |
| Wrong growth exponent | Table mismatch | `TestBackoffGrowthTable` |
| `rnd()=1.0` not clamped | result = d (exceeds d/2+d/2) | `TestBackoffNeverExceedsMaxNeverNegative` |

## 8. 5 self-check questions (answers at the very end)

1. Why must `attempt` be clamped before `1 << attempt`?
2. Why is the result `[d/2, d)` and not `[0, d)` or `[0, d]`?
3. What happens if `base` is 0? Is that a bug or acceptable?
4. Why does the test use `rnd=0` and `rnd=0.999999` specifically?
5. Why is `Backoff` a pure function with injected `rnd` instead of calling `rand.Float64()` inside?

**Answers:**
1. Go panics on negative shift counts at runtime; shifting ≥64 drops bits producing 0 or negative. Clamping to [0,30] keeps the shift safe since 2^30 fits in int64.
2. `[d/2, d)` guarantees minimum wait (d/2) so retries aren't instant, and maximum wait (just under d) so retries aren't longer than the cap. Full jitter `[0,d)` could retry almost instantly.
3. Acceptable: if base=0, d=0, result=0 (immediate retry). The caller should pass a sensible base; Backoff doesn't validate base.
4. rnd=0 tests the lower bound (d/2); rnd=0.999999 tests the upper bound (just under d). Together they pin both ends of the jitter range.
5. Injection makes the function pure and deterministic — tests can assert exact values without sleeping or real randomness, and production passes rand.Float64.

## 9. Done checklist

- [ ] `internal/agent/backoff.go` implements `Backoff` per spec
- [ ] `go vet ./internal/agent` clean
- [ ] `go test -race ./internal/agent -run TestBackoff -v` all 4 PASS
- [ ] `go test -race ./...` all packages PASS
- [ ] No panic on negative attempt
- [ ] No result exceeds max, no negative result
- [ ] Growth table matches 0.5s,1s,2s,4s,8s,15s