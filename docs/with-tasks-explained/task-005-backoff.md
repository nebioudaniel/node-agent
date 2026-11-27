# Task 005 — Backoff with Jitter (Step 1.7)

## 1. Task overview

**Task name and ID:** task-005-backoff (project Step 1.7) — **STATUS: INCOMPLETE / scaffolding only**

**Problem being solved:** A retrying agent needs to wait before retrying; naive immediate/constant retries create retry storms, and `20 agents` retrying in the same second overload a recovering coordinator. We need `func Backoff(attempt int, base, max time.Duration, rnd func() float64) time.Duration` implementing exponential backoff + jitter, with no overflow and no panic.

**Goal:** Provide `internal/agent.Backoff` with `d = base·2^attempt` cap `max`, `attempt` clamped, `result = d/2 + rnd()·d/2 ∈ [d/2,d) ⊆ [0,max)`. `rnd` injected for testability. **No sleeping in `Backoff`.**

**Why necessary:** Step 1.8 agent retry loop + re-register uses it; Step 1.5 showed `StaleAfter=15s/OfflineAfter=45s` liveness pressure. Robust distributed systems must not hammer recovering services.

**Previous task/dependency:** Step 1.6 identity (**not present in this checkout**) / Step 1.5 registry; independent pure function. **Note honestly: Step 1.6 is NOT implemented here.**

**COACH MODE honesty:** I (agent) created `backoff.go` (signature + `// TODO`) + `backoff_test.go`. **You implement `Backoff`.** Therefore this task is documented as **incomplete**; `go test ./internal/agent` currently **FAILS**. With-tasks-explained normally says doc after tests pass; this doc records real state + full learning.

---

## 2. Architecture and workflow

```mermaid
flowchart TD
    A[caller Step 1.8 agent retry loop] -->|attempt++| B[agent.Backoff attempt base max rnd]
    B --> C[clamp attempt 0..30]
    C --> D[d = base*2^attempt; cap d<=max; safe int64]
    D --> E[half=d/2; return half + duration rnd()*half]
    E --> F[result in d/2..d subset of 0..max]
    F --> G[caller sleeps in select ctx.Done vs timer]
```

`Backoff` is a **pure function**; it does not sleep, does not log, does not touch globals, does not call `time.Now`. The **caller** owns randomness (`rand.Float64`, or injected `rnd`) and sleeps cancellably (`select ctx.Done() vs time.Timer`). `Backoff` returns a `time.Duration`; doesn't decide whether to retry. Standalone in `internal/agent` (package `agent`). **Separation of concerns.**

---

## 3. Files changed

| File | Responsibility | What changed |
|---|---|---|
| `internal/agent/backoff.go` | `agent.Backoff` | **Created** `internal/agent` package; signature + doc + `// TODO`; **body NOT implemented** (coach mode). |
| `internal/agent/backoff_test.go` | `agent` tests | **Created**; verified math table; currently **failing** until `Backoff` implemented. |
| `docs/with-tasks-explained/task-005-backoff.md` | This doc | Created. |
| `docs/with-tasks-explained/README.md` | Index | task-005. |

---

## 4. Functions and methods explained

### `func Backoff(attempt int, base, max time.Duration, rnd func() float64) time.Duration`

- **Purpose:** return wait before retry `attempt` (0-based).
- **Inputs:** `attempt` int; `base`,`max` `time.Duration`; `rnd func() float64` returning `[0,1)`.
- **Outputs:** `time.Duration` in `[0,max)`, never negative, never exceeds `max`, never panics.
- **Step-by-step (to implement):**
  1. `if attempt < 0 { attempt = 0 }`
  2. `if attempt > 30 { attempt = 30 }`
  3. `d := base * time.Duration(1<<attempt)` (or `base << attempt`); `if d > max { d = max }`; `if d < 0 { d = max }`
  4. `half := d / 2`; `return half + time.Duration(rnd()*float64(half))`
- **Invariants:** `rnd()=0→d/2`; `rnd()=0.999999→~d (<d)`; `rnd∈[0,1)→[d/2,d)`; `d≤max→≤max`; clamp+cap→never negative/panic; `attempt=-1→0`; `attempt=100→30`.
- **Errors it handles:** none returned; clamps inputs; no panic on negative shift.
- **Who calls it:** Step 1.8 `Run` retry; tests; production passes `rand.Float64`.
- **What calls it next:** caller sleeps cancellably; nothing.

---

## 5. Algorithms and logic

| Technique | Problem | How | Why | Alternative | Complexity |
|---|---|---|---|---|---|
| Exponential backoff | don't hammer recovering service | `d=base·2^attempt` | cheap retry if momentary; slow if persistent | constant/immediate retry | O(1) |
| Cap `max` | bounded wait, still eventually retry | `if d>max d=max` | don't wait forever | uncapped (risk huge) | O(1) |
| Full/equal jitter `d/2 + rnd()·d/2` | thundering herd / synchronization | random in `[d/2,d)` | spread 20 agents; always ≥half & <full | no jitter; `rnd·d` full | O(1) |
| `rnd func() float64` injected | testable, no real randomness/sleep | `rnd()=0`,`0.999999`,`x` | pure fn; deterministic | global `rand.Float64` | O(1) |
| clamp `attempt` `<0→0`, `>30→30` | `1<<attempt` int64 overflow + **negative shift panic** | bound shift | `time.Duration` int64 | uncapped shift (overflow→negative) | O(1) |
| cap `d` + `d<0→max` | int64 overflow wrap | after multiply | never negative | none | O(1) |
| caller cancellable sleep | `Run` cancels fast | `select ctx.Done vs timer` | no leak; ≤100ms | `time.Sleep` (uncancellable) | — |

No formal scheduling scoring; arithmetic + clamps. **bit shift overflow explained below.**

---

## 6. Engineering decisions

| Decision | Why | Alternative | Trade-off |
|---|---|---|---|
| `d/2 + rnd()·d/2` (`[d/2,d)`) | always slow down; randomized spread; ≤max | full `rnd()·d`; `d+rnd` | min wait d/2 |
| cap `d` at `max` | bounded delay + safe | uncapped | never longer than max |
| clamp attempt `<0→0`, `>30→30` | no negative shift panic; no int64 overflow | uncapped `1<<attempt` | attempts >30 same as 30 (d≥max anyway) |
| inject `rnd` | pure/testable, no real randomness/sleep | global `rand` | caller must supply `[0,1)` |
| don't sleep in `Backoff` | separation; caller cancellable | `Backoff` sleeps | caller more code |
| `time.Duration` int64 ns | Go stdlib | float seconds | overflow risk → clamp |
| 0-based `attempt` | `2^0=1` first retry `d=base→d/2` | 1-based | caller increments |
| `base`/`max` caller-set | configurable | constants | caller sanity |
| pure fn, no logging | no lock/log under caller; testable | log inside | caller logs |

---

## 7. Go concepts and patterns learned

**`time.Duration` = int64 nanoseconds** (max ~9.2e18 ≈ 292y). **bit shifts:** `1 << attempt`; shifting left by ≥width(64) drops high bits→`0`/garbage/**negative**; `base·(1<<attempt)` can **overflow int64**→wrap negative; Go **panics on negative shift count** for non-constant shifts (`1<<-1` runtime panic) → `attempt<0` dangerous. **clamp attempt + cap d.** Conversions `time.Duration(float)`, `d/2`, integer truncation. `func() float64` values; DI (`rnd`, like `now`,`collect`,`log`,`ctx`). `math/rand.Rand.Float64()` `[0,1)`. Pure functions. Caller-owned `*rand.Rand` + `sync.Mutex` if shared. No `time.Sleep` in pure fn.

**Thundering herd** + **jitter**; exponential backoff; full/equal jitter. **Goroutine** cancellable sleep `select ctx.Done()`. Honest note: Step1.7 test `TestBackoffNeverExceedsMaxNeverNegative` only trivially passes while `got==0`; not sufficient alone.

---

## 8. Testing explained

**Written (you implement to pass):**
- `TestBackoffJitterRange`: `rnd=0→exactly d/2`; `rnd=0.999999→>d/2 && <d`
- `TestBackoffGrowthTable`: base=1s,max=30s,attempts0–5,rnd=0→**0.5,1,2,4,8,15s** (d cap30s@5: 2^5=32→30)
- `TestBackoffNeverExceedsMaxNeverNegative`: attempts0–100×rnd∈{0,.25,.5,.75,.999999}→`0≤got≤max`
- `TestBackoffNegativeAttempt`: `attempt=-1`→no `recover` panic, `≥0`,`≤max`

**ACTUAL current:** `go vet ./internal/agent` clean; `go test -race ./internal/agent -v` **FAIL** (`got 0s, want ...`; bound tests trivially pass only because `got==0`). Commands: `gofmt`, `go vet`, `go test -race`. **No claim tests pass.** After your `Backoff`: re-run `go test -race ./internal/agent` + `go test -race ./...` + `go vet ./...`. Not yet run green. **Not run real randomness; rnd injected.**

---

## 9. Execution walkthrough

`Backoff(0,1s,30s,rnd0)`→d=1s→**500ms**; `rnd.999999`→~1s(<1s); `attempt=1`→d=2s→**1s**; `attempt=5`→d=32→cap30→**15s**; `attempt=100`→clamp30→d=1s·2^30→cap30→**~15s**(`rnd`-scaled `[15s,30s)`→≤30); `attempt=-1`→clamp0→d=1s→[0.5s,1s); never neg/panic. Caller `attempt++`→`Backoff(...,rand.Float64)`→`select{ctx.Done→return; timer.C→retry}`. **STOPPED (coach); not implemented.**

---

## 10. Important lessons

- Exponential backoff + **jitter** prevent retry storms; **thundering herd** is why 20 agents must not reconnect in the same second.
- `rnd()=0→d/2`, `rnd→1→d`, result `[d/2,d)`, `≤max`, `≥0`.
- **`time.Duration` int64**; **clamp attempt + cap d**; **negative shift panic**; overflow→negative.
- injected `rnd` → pure/testable. **Don't sleep in pure fn**; caller cancellable `select`. 
- honest incomplete: `TODO`, red tests, not claim done.

---

## 11. Questions to test my understanding

1. Why exponential backoff? Why cap `max`?
2. `20 agents` same second: why `d/2 + rnd·d/2` vs `rnd·d`/none? Why `[d/2,d)`?
3. `rnd=0` vs `rnd=0.999999` vs `rnd=0.5` results?
4. `time.Duration` int64; `1<<attempt`; `attempt=100/-1`; **negative shift panic**; cap `d≤max`; result `≤max,≥0`.
5. Why inject `rnd`? `rand.Float64` `[0,1)`; caller-owned `*rand.Rand`+mutex?
6. Why `Backoff` doesn't sleep; caller `select ctx.Done vs timer`; `time.Timer/Stop`.
7. table `1,2,4,8,16,32→cap30`. `d/2`. attempt0 first=`base/2`(rnd0). 
8. Why pure fn/no logging/no time.Now. 

*(Not answering.)*

---

## 12. Future considerations

**Necessary now:** user implements `Backoff`; green tests. **Later:** `time.AfterFunc` vs timer; `rand` `*rand.Rand` mutex; configurable `base`/`max`; `Backoff` reuse Step1.8. **Unnecessary:** full-random-only; sleeping inside `Backoff`; observer; >30 clamp need.

---

**Status:** INCOMPLETE. Step 1.7 scaffolding; `Backoff` TODO; tests red; not Step 1.8. Honestly recorded.