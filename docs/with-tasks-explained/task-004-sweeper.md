# Task 004 — Sweeper: log status transitions (Step 1.5)

## 1. Task overview

**Task name and ID:** task-004-sweeper (project Step 1.5)

**Problem being solved:** Since Step 1.1, status is computed on read (`StatusFor(LastSeen, now)`). `GET /v1/workers` and `registry.Get/List` always reflect current status, but nothing actively *notices* when a worker crosses a threshold — nobody is told that a worker went `online→stale→offline`, or that it came back. There is no observability of status *changes*.

**Goal:** Add a background sweeper that recomputes every worker's status on an interval, compares it to each worker's last **reported** status, emits a `Transition` for each change **exactly once**, and logs it. Critical constraint from the guide: status is computed on read, so **the sweeper does NOT set status**. Its only job is to notice transitions and report each once.

**Why this task is necessary in the overall project:** Operators/monitoring and future components need to know *when* workers change liveness, not just query it. The sweeper produces structured `online→stale→offline` / `offline→online` events. It completes the liveness story started in Step 1.1.

**Previous task or dependency:** Step 1.1 (`StatusFor`, `StaleAfter=15s`, `OfflineAfter=45s`) + Step 1.2/1.4 registry. Reuses `StatusFor`; does not alter `List`/`Get`. (Note: pre-work fixed `cpu_model` being `0`, making it a string.)

---

## 2. Architecture and workflow

```mermaid
flowchart TD
    A[Workers heartbeat -> LastSeen updates] --> B[time.NewTicker every=5s in main]
    B --> C[RunSweeper select ctx.Done / ticker.C]
    C --> D[Registry.Sweep]
    D --> E{for each worker: current = StatusFor LastSeen now}
    E --> F{current != lastReported?}
    F -->|no| G[no output]
    F -->|yes| H[append Transition + set lastReported=current]
    H --> I[sort by ID; non-nil]
    I --> J[Sweep returns []Transition - no logging under lock]
    J --> K[RunSweeper logs id from to via slog]
    L[List / Get] --> M[still compute Status on read - UNCHANGED]
```

**Key separation:** `Sweep` mutates only `lastReported` bookkeeping; `Status` is never written by the sweeper and `toView` still computes `StatusFor` on read. `List`/`Get` output is unchanged.

**Last reported tracking:** `Registry.reported map[string]string` (id → last reported). A newly added worker (`Register` or `RecordHeartbeat` upsert) starts `lastReported="pending"`. `Sweep` defaults missing to `"pending"`, emits on `cur != prev`, then sets `reported[id]=cur`. Result: each change reported **once**; no re-emission on repeated sweeps with no time change.

**Goroutine:** `RunSweeper` runs in its own goroutine. `time.NewTicker`, `defer ticker.Stop()`, `select { case <-ctx.Done(): return; case <-ticker.C: ... }`. `main` starts it `go registry.RunSweeper(ctx, reg, 5*time.Second, slog.Default())` with the **root** `signal.NotifyContext`; on shutdown ctx is cancelled, sweeper returns, no leak. Clean shutdown preserved. Logging happens **outside** the registry lock.

---

## 3. Files changed

| File | Responsibility | What changed |
|---|---|---|
| `internal/coordinator/registry/sweep.go` | **New.** `Transition`, `Sweep`, `RunSweeper` | Created. Sweep takes `Lock`, computes `StatusFor`, diffs `reported`, sorts by ID, no logging under lock. RunSweeper ticker+select+cancel. |
| `internal/coordinator/registry/sweep_test.go` | **New.** Sweeper tests | `TestSweep*` + `TestSweepRunSweeper`, fake clock, syncBuffer slog. |
| `internal/coordinator/registry/registry.go` | Registry | `reported` map now used. `Register` sets `LastSeen=time.Time{}` + `Status="pending"` + `reported[id]="pending"`. `RecordHeartbeat` marks `reported[id]="pending"` for newly created. |
| `cmd/coordinator/main.go` | Coordinator entry | `go registry.RunSweeper(ctx, reg, 5*time.Second, slog.Default())`. |
| `internal/coordinator/registry/registry_test.go` | Registry tests | `TestRegister` updated: pending + zero `LastSeen`. |
| `docs/with-tasks-explained/README.md` | Index | task-004. |
| `docs/with-tasks-explained/task-004-sweeper.md` | This doc | Created. |

**Pre-work (separate commit, Step 1.4→1.5 bug): `cpu_model` string.** Bug: `GET /v1/workers` printed `"cpu_model":0`. Cause: `Worker.CPUModel`/`View.CPUModel` were `int`, and `handleRegister` passed `0`, ignoring `req.Specs.CPU` (string). Fix: `CPUModel int`→`string`, `Register` `cpuModel int`→`string`, wire `req.Specs.CPU`, call sites. Added `TestCPUModelIsString` asserting `"cpu_model":"Test CPU"` + JSON string type. (`cpu_cores` is separate int.)

---

## 4. Functions and methods explained

### `type Transition struct { ID, From, To string }`

- **Purpose:** value describing one worker status change. `From`=last reported, `To`=newly computed. JSON `id,from,to`. **No secrets.** Value type, safe to pass/log/copy.

### `func (r *Registry) Sweep() []Transition`

- **Signature:** no args; returns `[]Transition`.
- **Step-by-step:**
  1. `r.mu.Lock(); defer r.mu.Unlock()` — **write** lock (mutates `reported`).
  2. `now := r.now()` (injected clock).
  3. `out := make([]Transition, 0, len(...))` (**non-nil**).
  4. For each `id, w`: `cur := StatusFor(w.LastSeen, now)`; `prev, ok := r.reported[id]; if !ok prev="pending"`; if `cur != prev` append `Transition{id,prev,cur}` + `r.reported[id]=cur`.
  5. `sort.Slice` by `ID`. return `out`.
- **Does NOT set `w.Status`**, does NOT log under lock.
- **Outputs:** sorted, non-nil, empty when nothing changed; `pending→online`, `online→stale`, `stale→offline`, `offline→online`, etc.
- **Who calls it:** `RunSweeper`; tests.
- **What calls it next:** `RunSweeper` logs each.

### `func RunSweeper(ctx context.Context, r *Registry, every time.Duration, log *slog.Logger)`

- **Purpose:** run `Sweep` every `every`, log transitions, stop on cancel.
- **Step-by-step:** `log=slog.Default()` if nil; `every=1s` if <=0; `ticker := time.NewTicker(every); defer ticker.Stop()`; `for { select { case <-ctx.Done(): return; case <-ticker.C: for _,tr := range r.Sweep() { log.Info(...,"id",tr.ID,"from",tr.From,"to",tr.To) } } }`.
- **Errors it handles:** none (non-blocking select).
- **Why:** `NewTicker` not `Sleep`; `Stop` prevents resource leak; return on `ctx.Done` prevents goroutine leak.
- **Who calls it:** `main` (`go registry.RunSweeper(...)`).
- **What calls it next:** nothing.

---

## 5. Algorithms and logic

| Technique | Problem | How | Why | Complexity |
|---|---|---|---|---|
| Transition detection / dedup | report each change once | `cur != lastReported`; update `reported[id]` | no re-emission on repeated sweeps | O(1) per worker |
| `StatusFor` (Step 1.1) | classify | age<15 online; <45 stale; else offline; zero→pending | reuse pure fn | O(1) |
| sort by ID | deterministic output | `sort.Slice(out, ID<)` | stable order; testable | O(n log n) |
| non-nil slice | `[]` not null | `make(...,0,n)` | safe ranging/JSON | O(1) |
| `time.NewTicker` + `select` + `ctx.Done` | periodic + cancel | `defer Stop`, `return` on `<-ctx.Done()` | no busy-wait; no goroutine leak | — |
| injected `now` | testable time | `reg.New(fakeClock)` | no sleeping in sweeper tests | — |
| **write Lock** (not RLock) | safe `reported` mutation | `r.mu.Lock()` | Sweep mutates last-reported | O(1) |
| no logging under lock | avoid slow-lock holding | `Sweep` returns data; `RunSweeper` logs | shorter critical section | — |

No exponential backoff / scheduling scoring / queueing here.

---

## 6. Engineering decisions

| Decision | Why | Alternative | Trade-off |
|---|---|---|---|
| Sweeper does NOT set status | status computed on read (Step 1.1); sweeper only observes | store status + update | no dual source of truth |
| lastReported separate from computed `w.Status` | `toView` computes live; reported tracks emission | derive reported from `w.Status` | extra map; precise "report once" |
| `reported` starts `pending` | aligns `StatusFor(zero)="pending"`; `pending→online` transition | start `online` | first sweep may emit pending→online |
| `Register` zero `LastSeen` + pending | honest "no heartbeat yet"; `StatusFor→pending` | `LastSeen=now` | freshly registered shows pending until heartbeat |
| `time.NewTicker` | periodic without blocking sleep | `time.Sleep` loop | `Stop()` needed |
| `select ctx.Done + ticker.C` | cancellation-aware | block on ticker only | shutdown stops sweeper |
| return on ctx.Done; Stop | no goroutine/ticker leak | `for range ticker.C` | — |
| no logging under lock | short critical section | log inside Sweep | `Sweep` returns data |
| sort by ID; non-nil empty | deterministic; safe | unsorted map; nil | small sort cost |
| separate `sweep.go` | SRP; keep registry.go focused | append to registry.go | one more file |
| `slog.Logger` injected | testable output (syncBuffer) | global slog only | nil handling |
| every=5s in main | reasonable cadence vs 15/45s thresholds | 1s | slight delay in emission |

---

## 7. Go concepts and patterns learned

**time.Ticker vs time.Sleep:** `time.NewTicker(every)` delivers on `C`; doesn't block; must `Stop()`. `time.Sleep` blocks and can't be cancelled. `defer ticker.Stop()`. `time.Sleep` used in my test only to poll buffer (not prod). Prefer Ticker + select.

**`select` with `ctx.Done()`:** `select { case <-ctx.Done(): return; case <-ticker.C: ... }`. `ctx` cancellation (`signal.NotifyContext`, `defer stop()`, `srv.Shutdown`) makes `Done()` close → `<-ctx.Done()` ready → return. Cancellable.

**`context.Context`:** first arg; never store; `context.Background()` root in main; `WithCancel` in test. Cancel → `Done()` closed. Doesn't block ticker send.

**Why Sweep returns data instead of logging:** separation; shorter lock; testable without parsing logs; `RunSweeper` single log point; no logging while holding mutex (avoid blocking other readers/writers).

**Why WRITE Lock not RLock:** Sweep **mutates** `r.reported`. `RLock` allows concurrent readers + would be a data race on `reported`. `sync.RWMutex` read-lock only safe when no mutation. `go test -race` would catch.

**Goroutine lifecycle + leaks:** `go RunSweeper(...)`; leaks if never returns. `ctx.Done→return`, `ticker.Stop`, `defer`. Shutdown cancels root ctx. `RunSweeper` test asserts return within 100ms after cancel. No `wg` needed since main waits for ctx.

**"each change reported once":** `lastReported` persisted; emit only `cur != prev` then update. Repeated sweeps no time change → zero. Not one-per-sweep; exactly 2 for online→stale→offline.

**Struct value `Transition`:** returned by value in slice; no secrets; printable.

**Patterns present:** Dependency Injection (ctx, clock, logger); separation of concerns; non-nil slices; injected `now`. Not claiming GoF (observer-ish notify is a future possibility; this is simple event return + log, NOT wired observer).

---

## 8. Testing explained

**Fake clock, no sleeping** except `RunSweeper` test. **Executed** `go test -race ./internal/coordinator/registry -run TestSweep -v` + `go test -race ./...` + `go vet ./...` — all pass.

- `TestSweepOnlineStaleOfflineTwoTransitions`: baseline pending→online, repeated-no-change zero, +20s→online→stale, +30s→stale→offline, repeated zero, total==2.
- `TestSweepPendingToOnlineOnFirstHeartbeat`.
- `TestSweepOfflineToOnlineAfterNewHeartbeat`.
- `TestSweepTwoWorkersSortedByID`: both pending→online, sorted wrk-a,wrk-b.
- `TestSweepEmptyRegistry`: non-nil empty.
- `TestSweepRunSweeper`: every=10ms; `slog.NewTextHandler(syncBuffer)`; assert `worker status transition` + id; cancel; return within 100ms. `syncBuffer` mutex-guarded.
- Pre-work `TestCPUModelIsString`. `TestRegister`→pending/zero LastSeen. Existing race test still passes.

**CHECKPOINT — actual verbose output** (`go test -race ./internal/coordinator/registry -run TestSweep -v`): see end-of-message paste. **Manual real-run:** coordinator + register + 1 heartbeat + wait: `pending→online`, `online→stale` ~15s, `stale→offline` ~45s (every=5s).

---

## 9. Execution walkthrough

`wrk-1` heartbeats at t0 (lastSeen=t0, reported=pending→...). Sweeper ticks every 5s:
- tick@t0+5: cur=online; prev=pending→**pending→online**; reported=online.
- no change: online==online→nothing.
- t0+15 (StaleAfter): age=15 not <15→cur=stale; online≠stale→**online→stale**; reported=stale.
- t0+45 (OfflineAfter): age=45 not <45→cur=offline; stale≠offline→**stale→offline**; reported=offline.
- new heartbeat→lastSeen=t'; cur=online; offline≠online→**offline→online**.
Each emitted once; sorted by ID. Simultaneously `GET /v1/workers` independently returns correct `status` on read; sweeper doesn't alter it. Empty registry→`[]`. On shutdown ctx cancelled→sweeper returns→no leak.

---

## 10. Important lessons

- **Status computed on read; sweeper doesn't set it.** Dual tracking would create two truths. Sweeper = observation + event emission.
- **"Reported once" needs persisted last-reported.** Compare `cur != prev`, update `prev`. Repeated no-change→nothing.
- **Sort + non-nil for determinism.** `make(0,n)`.
- **RWMutex read-lock only when no mutation.** Sweep mutates `reported`→**Lock**. `go test -race`.
- **No logging under lock.** Return data; log outside.
- **`time.Ticker`+`defer Stop`+`select ctx.Done`.** Cancellation + no leak. Root ctx for shutdown.
- **`pending` for never-heartbeated.** `StatusFor(zero)="pending"`.
- **JSON typo (`cpu_model` `int 0`→string) silently broke output; pin types + exact strings in tests.**
- **DI:** inject `now`, `ctx`, `log`.

---

## 11. Questions to test my understanding

1. Why does the sweeper NOT set `Status`, and `List`/`Get` still compute `StatusFor` on read? What would break if Sweep wrote status?
2. `reported[id]=cur` only set when `cur!=prev`. Explain how repeated sweeps with no time change yield zero, and why online→stale→offline yields exactly 2.
3. Why WRITE `Lock` not `RLock` in `Sweep`, even though it "only reads workers"? `go test -race`.
4. `time.Ticker` vs `time.Sleep`? `defer ticker.Stop()`? `select ctx.Done + ticker.C` + `defer stop()` + `srv.Shutdown` → no goroutine leak?
5. Why `Sweep` returns `[]Transition` instead of logging; why no log under lock? Why `make(0,n)` (non-nil `[]` not `null`) + sort by ID?
6. A newly added worker: `lastReported="pending"`, `LastSeen=zero`→`StatusFor→pending`. `pending→online` on first heartbeat? `offline→online` on return?
7. Pre-work: why `cpu_model` printed `0` (int + `handleRegister` passed `0`, ignored `req.Specs.CPU`)? Why string vs `cpu_cores` int?
8. `RunSweeper(ctx,...)` in `main` with root ctx + `every=5s`: why cancellation keeps shutdown clean? `slog.Logger` injected?

*(Not answering; ask to be quizzed.)*

---

## 12. Future considerations

**Necessary now:** none. **Later:** Step 1.5's unused `reported`→now used; future Observer pattern (notify subscribers vs return data); `GET /v1/workers` unaffected; metrics; persistence; make `every`/thresholds configurable; `Reap` offline removal; `Seq`. **Unnecessary now:** changing `StatusFor`; storing status on `Worker`; background reaper; channels vs returned slice; big refactor.

---

**Status:** Step 1.5 complete + verified. `go vet ./...` clean, `go test -race ./...` all pass, `go test -race ./internal/coordinator/registry -run TestSweep -v` all PASS, real-run sweeper logs verified. **cpu_model pre-work** fixed + tested. **Not starting Step 1.6.**