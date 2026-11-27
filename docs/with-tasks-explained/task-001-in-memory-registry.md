# Task 001 — In-Memory Worker Registry (Step 1.2)

## 1. Task overview

**Task name and ID:** task-001-in-memory-registry (project Step 1.2)

**Problem being solved:** The `internal/coordinator/registry/` package was broken. `registry.go` declared `package regiist` (typo) while `status.go` in the same directory declared `package registry` — Go requires one package per directory, so the whole package failed to compile (`go test ./...` failed with "found packages regiist and registry"). Beyond the typo, `registry.go` had duplicate `Worker` struct definitions, a nonsensical `map[string]map[string]*Worker` field, and was truncated mid-file at `func `. No working `Registry`, `Worker`, `View`, `RecordHeartbeat`, `CheckSecret`, `Get`, or `List` existed.

**Goal:** Implement a working, concurrency-safe in-memory worker registry that:
- Stores `Worker` records keyed by worker id.
- Hands out read-only `View` copies (no `SecretHash`, no live references).
- Supports `RecordHeartbeat` (upsert + stamp `LastSeen` with coordinator clock), `CheckSecret` (constant-time hash compare), `Get` (single worker, `ErrUnknownWorker` if missing), and `List` (all workers, sorted).
- Uses an injectable clock (`now func() time.Time`) so time-dependent behavior is deterministically testable.
- Stores only `sha256` secret hashes, never plain secrets.

**Why this task is necessary in the overall project:** The coordinator is the central service of the Team Compute Platform. It needs one authoritative, concurrency-safe place to track which workers exist and whether they are alive. The registry is the storage layer behind the heartbeat endpoint and the future register endpoint and worker-listing endpoint. Without a working registry, the coordinator cannot answer "who is alive" — a core distributed-systems question.

**Previous task or dependency:** Step 1.1 — Worker status function (`StatusFor` in `internal/coordinator/registry/status.go`). `StatusFor(lastSeen, now)` is a pure function that classifies a worker as `pending`, `online`, `stale`, or `offline`. Step 1.2 depends on it: the registry computes `Status` on read by calling `StatusFor`.

---

## 2. Architecture and workflow

The registry sits inside the coordinator binary, between the HTTP handler and the workers:

```mermaid
flowchart TD
    A[Worker sends POST /v1/workers/{id}/heartbeat] --> B[Coordinator HTTP handler]
    B --> C[Heartbeat.Validate() in internal/api]
    C -->|valid| D[Registry.RecordHeartbeat(id, hb)]
    D --> E[Store Worker record, stamp LastSeen = now()]
    E -->|on read| F[Registry.Get(id) or List()]
    F --> G[Build View copy - no SecretHash, Status computed via StatusFor]
    G --> H[Return to handler / API consumer]
```

The registry package (`internal/coordinator/registry`) knows nothing about HTTP. It is a pure in-memory store. The HTTP handler (`internal/coordinator/server.go`) will call into it in a later step; for Step 1.2 the registry is tested standalone.

Data flow for a heartbeat write:
1. Handler receives JSON `api.Heartbeat`.
2. Handler calls `hb.Validate()` (rejects impossible values: `ram_available > ram_total`, `cpu_percent` outside 0–100, etc.).
3. Handler (later step) calls `reg.RecordHeartbeat(id, hb)`.
4. `RecordHeartbeat` takes the write lock, creates/overwrites the `Worker`, stamps `LastSeen = r.now()` (coordinator's clock, **not** the agent's `SentAt`).

Data flow for a read (`Get`/`List`):
1. Caller requests worker(s).
2. Registry takes the read lock (`RLock`).
3. `toView` builds a defensive copy: `Labels` map copied, `LoadedModels` slice copied, `SecretHash` omitted, `Status` computed via `StatusFor(w.LastSeen, r.now())`.
4. Returns `*View` (or `[]*View`) — caller cannot mutate internal state.

---

## 3. Files changed

| File | Responsibility | Why it exists | What changed |
|---|---|---|---|
| `internal/coordinator/registry/registry.go` | Worker registry: `Worker`, `View`, `Registry`, `New`, `RecordHeartbeat`, `CheckSecret`, `Get`, `List`, `HashSecret`, `ErrUnknownWorker` | The coordinator's authoritative, concurrency-safe worker store. Replaces the corrupted file (wrong package name, duplicate structs, truncated). | **Rewritten.** Fixed `package registry`; removed duplicate `Worker` struct; replaced `map[string]map[string]*Worker` with `map[string]*Worker`; added `View` type; completed `Registry` struct with `mu sync.RWMutex`, `workers map[string]*Worker`, `now func() time.Time`, `reported map[string]string` placeholder; implemented `New`, `RecordHeartbeat`, `CheckSecret`, `Get`, `List`, `toView`, `HashSecret`; added `ErrUnknownWorker` sentinel. |
| `internal/coordinator/registry/registry_test.go` | Tests for the registry package | Verifies the registry behaves correctly: fake-clock lifecycle, unknown worker, secret check, defensive copies, sort order, concurrency safety under race detector. | **Created.** 9 test functions covering `RecordHeartbeat`/`Get`, `ErrUnknownWorker`, `CheckSecret` (right/wrong/unknown/empty secret), full `pending`→`online`→`stale`→`offline` lifecycle, coordinator-clock `LastSeen` (ignores agent `SentAt`), defensive copy semantics, sorted `List`, empty list, and a 50-goroutine × 100-iteration concurrent access race test. |
| `internal/coordinator/registry/status.go` | `StatusFor(lastSeen, now)` pure function | Classifies worker liveness (Step 1.1). | **Unchanged.** Dependency only. |
| `internal/coordinator/registry/status_test.go` | Tests for `StatusFor` | Boundary tests at 0s, 14.9s, 15s, 44.9s, 45s, 10min. | **Unchanged.** Dependency only. |
| `internal/api/types.go` | `api.Heartbeat` wire type + `Validate()` | Shared wire types used by the registry's `RecordHeartbeat`. | **Unchanged.** Dependency only. |

No unrelated files were modified.

---

## 4. Functions and methods explained

### `var ErrUnknownWorker = errors.New("unknown worker")`

- **Purpose:** Sentinel error returned by `Get` when a worker id is not in the registry.
- **Inputs:** None (package-level variable).
- **Outputs:** An `error` value.
- **Step-by-step:** Constructed once at package init with `errors.New`. Callers use `errors.Is(err, ErrUnknownWorker)` to detect it.
- **Errors it handles:** None itself; it *is* an error.
- **Why designed this way:** Sentinel errors let the HTTP handler (a later step) decide the mapping: `ErrUnknownWorker` → HTTP 404, while other errors → HTTP 500. This keeps the registry free of HTTP concerns.
- **Who calls it:** `Registry.Get`.
- **What calls it next:** The caller of `Get` (future HTTP handler) uses `errors.Is` to branch.

### `func HashSecret(secret string) [32]byte`

- **Purpose:** Compute the SHA-256 digest of a plaintext secret.
- **Inputs:** `secret string` — the plaintext secret.
- **Outputs:** `[32]byte` — the fixed-size digest.
- **Step-by-step:** `sha256.Sum256([]byte(secret))` returns `[32]byte`.
- **Errors it handles:** None — `sha256.Sum256` never fails.
- **Why designed this way:** Only the hash is stored; the plaintext secret is never persisted. Fixed-size array is a value type (comparable, copy-safe). Returning `[32]byte` (not `[]byte`) prevents accidental mutation of the digest.
- **Who calls it:** `Registry.CheckSecret` (and future registration code in Step 1.3).
- **What calls it next:** `CheckSecret` compares the result against the stored `SecretHash`.

### `func New(now func() time.Time) *Registry`

- **Purpose:** Construct a `Registry` with an injected clock.
- **Inputs:** `now func() time.Time` — a clock function. `nil` falls back to `time.Now`.
- **Outputs:** `*Registry` initialized with empty `workers` map and empty `reported` map.
- **Step-by-step:** If `now == nil`, set `now = time.Now`. Allocate `workers = make(map[string]*Worker)` and `reported = make(map[string]string)`. Return pointer.
- **Errors it handles:** None.
- **Why designed this way:** Dependency injection of the clock. Production passes `nil` (real time); tests pass a fake clock for deterministic timing without sleeping. Follows the same pattern as `server.NewHandler(collect func() (specs.Specs, error))`.
- **Who calls it:** Future coordinator main (wire-up), tests.
- **What calls it next:** Nothing — `New` returns the registry for callers to use.

### `func (r *Registry) RecordHeartbeat(id string, hb api.Heartbeat)`

- **Purpose:** Upsert a worker record from a validated heartbeat.
- **Inputs:** `id string` (worker id from URL path), `hb api.Heartbeat` (validated heartbeat body).
- **Outputs:** None.
- **Step-by-step:**
  1. Acquire `r.mu.Lock()` (write lock).
  2. Build `&Worker{ID: id, RAMTotalBytes: hb.RAMTotalBytes, LastSeen: r.now(), Last: hb, Status: StatusFor(r.now(), r.now())}`.
  3. Store in `r.workers[id] = w` (overwrites if exists).
  4. Release lock via `defer r.mu.Unlock()`.
- **Important conditions:** `LastSeen` is stamped with the **coordinator's** `r.now()`, never `hb.SentAt` (agent clocks can be wrong or malicious — clock skew). `SecretHash` is zero (populated later at registration, Step 1.3). Fields like `Hostname`, `OS`, `Arch`, `CPUModel`, `Labels` remain zero-valued until registration populates them.
- **Errors it handles:** None.
- **Why designed this way:** Upsert semantics mean the first heartbeat creates the worker; subsequent heartbeats refresh `LastSeen`. Write lock ensures mutual exclusion with concurrent reads/writes.
- **Who calls it:** Future heartbeat handler (after validation).
- **What calls it next:** Nothing — it's a terminal storage operation for this step.

### `func (r *Registry) CheckSecret(id string, secret string) bool`

- **Purpose:** Verify a worker's secret without revealing whether the id exists (constant-time compare).
- **Inputs:** `id string`, `secret string` (plaintext candidate).
- **Outputs:** `bool` — `true` if id exists and hash matches; `false` otherwise (including unknown id).
- **Step-by-step:**
  1. Acquire `r.mu.RLock()` (read lock).
  2. Look up `r.workers[id]`. If not found, return `false`.
  3. Compute `got := HashSecret(secret)`.
  4. `subtle.ConstantTimeCompare(w.SecretHash[:], got[:]) == 1` — compare in constant time to resist timing attacks.
  5. Release lock, return result.
- **Important conditions:** Returns `false` for unknown workers (does not distinguish "wrong secret" from "unknown worker" — prevents id enumeration). Uses `subtle.ConstantTimeCompare` on `[]byte` views of `[32]byte` arrays.
- **Errors it handles:** None (returns bool, not error).
- **Why designed this way:** Constant-time comparison prevents timing attacks that could reveal the secret byte-by-byte. Boolean return (not error) is appropriate for a yes/no auth check.
- **Who calls it:** Future heartbeat handler / register endpoint.
- **What calls it next:** Nothing — boolean result is used directly by caller.

### `func (r *Registry) Get(id string) (*View, error)`

- **Purpose:** Return a defensive read-only copy of a worker as a `View`.
- **Inputs:** `id string`.
- **Outputs:** `*View` (copy) or `nil` + `ErrUnknownWorker`.
- **Step-by-step:**
  1. Acquire `r.mu.RLock()`.
  2. Look up `r.workers[id]`. If not found, return `nil, ErrUnknownWorker`.
  3. Call `r.toView(w)` (builds defensive copy: copies `Labels` map, copies `LoadedModels` slice, omits `SecretHash`, computes `Status` via `StatusFor`).
  4. Release lock, return `*View, nil`.
- **Important conditions:** `Status` is computed on read from `LastSeen` and `r.now()` — it is never stored, so it cannot go stale in the record.
- **Errors it handles:** `ErrUnknownWorker` for missing ids.
- **Why designed this way:** Returns `*View` not `*Worker` — `View` excludes `SecretHash` and is a fresh copy, so callers cannot read secrets or mutate storage. `RLock` allows concurrent reads.
- **Who calls it:** Future HTTP handler (worker lookup by id).
- **What calls it next:** The caller uses the `View` fields for display/API response.

### `func (r *Registry) List() []*View`

- **Purpose:** Return defensive copies of all workers as `View`s, sorted by Hostname then ID.
- **Inputs:** None.
- **Outputs:** `[]*View` (empty non-nil slice when no workers).
- **Step-by-step:**
  1. Acquire `r.mu.RLock()`.
  2. Iterate `r.workers`, append `r.toView(w)` for each.
  3. `sort.Slice` by `Hostname` then `ID` for deterministic order.
  4. Release lock, return slice.
- **Important conditions:** Returns an empty (non-nil) slice for an empty registry — callers can range over it safely. Sorting happens while holding the lock to ensure a consistent snapshot.
- **Errors it handles:** None.
- **Why designed this way:** Sorted output makes tests and API consumers deterministic. Defensive copies (via `toView`) prevent caller mutation. Empty non-nil slice avoids nil-slice surprises in JSON marshaling.
- **Who calls it:** Future `GET /v1/workers` endpoint.
- **What calls it next:** The caller ranges over the slice or encodes it to JSON.

### `func (r *Registry) toView(w *Worker) *View`

- **Purpose:** Build a defensive `View` copy from a `Worker`, omitting `SecretHash` and computing `Status`.
- **Inputs:** `w *Worker` (pointer to stored record).
- **Outputs:** `*View`.
- **Step-by-step:**
  1. Copy `w.Labels` map into a new map (iterate + assign).
  2. Copy `w.Last.LoadedModels` slice via `copy`.
  3. Construct `&View{...}` with all fields except `SecretHash`; `Status` = `StatusFor(w.LastSeen, r.now())`.
  4. Return pointer.
- **Important conditions:** Caller must hold the registry lock (RLock or Lock) — `toView` does not lock itself. `LoadedModels` in `api.Heartbeat` is copied so the returned view's slice is independent.
- **Errors it handles:** None.
- **Why designed this way:** Centralizes the copy logic so `Get` and `List` both produce safe, independent views. `SecretHash` is deliberately omitted — secrets never leave the registry.
- **Who calls it:** `Get`, `List`.
- **What calls it next:** Nothing — terminal copy operation.

---

## 5. Algorithms and logic

| Algorithm/technique | Problem it solves | How it works | Why chosen | Alternatives | Complexity | Limitations |
|---|---|---|---|---|---|---|
| `sync.RWMutex` | Concurrent-safe access to the worker map | `RLock` for reads (`Get`, `List`), `Lock` for writes (`RecordHeartbeat`). Multiple readers can proceed; only one writer at a time. | Read-heavy workload (many status checks, few heartbeats per worker). | Plain `sync.Mutex` (simpler but serializes all reads) or `sync.Map` (optimized for insert-if-absent / read-heavy, but less idiomatic here). | O(1) lock/unlock; contention under heavy write load. | Writer blocks all readers; long-held writes stall reads. |
| SHA-256 (`sha256.Sum256`) | One-way secret hashing for storage | `HashSecret` computes 32-byte digest. Only digest stored; plaintext never persisted. | Cryptographically strong, standard library, fixed output size. | bcrypt/scrypt (better for passwords, but overkill for machine-generated secrets; slower). | O(n) where n = secret length; output always 32 bytes. | Hash is irreversible — lost secret means re-register. |
| `subtle.ConstantTimeCompare` | Timing-attack-resistant secret comparison | Compares two `[]byte` slices in constant time; returns 1 if equal, 0 otherwise. | Prevents attackers from learning the secret byte-by-byte via response-time differences. | `==` on slices (fast but variable-time — leaks info). | O(n) where n = 32 (constant). | Both slices must be same length; returns 0 if lengths differ. |
| Defensive copying (`toView`) | Prevent callers from mutating internal state | Copies `Labels` map and `LoadedModels` slice when returning a `View`. | Maps and slices are reference types in Go; returning them directly lets callers mutate the registry's internals. | Return live references (faster but unsafe). | O(m + k) where m = label count, k = loaded model count. | Per-read allocation cost. |
| `sort.Slice` | Deterministic ordering of `List` output | Sorts views by `Hostname`, then `ID`, using a less func. | Map iteration order is random in Go; sorting makes output stable for tests and API consumers. | Return unsorted (faster but non-deterministic). | O(n log n). | Sort cost per read; trivial for small registries. |
| Compute-on-read (`StatusFor` in `toView`) | Worker status never stored/stale | `Status` computed from `LastSeen` + `now()` at read time. | Status can never become stale in the record; always reflects current time. | Background goroutine updating stored status periodically (extra concurrency, complexity). | O(1) per worker. | Recomputed on every read (trivial cost here). |

No exotic algorithms (exponential backoff, hashing beyond SHA-256, scheduling, etc.) are used. The task uses straightforward procedural logic with standard concurrency control.

---

## 6. Engineering decisions

| Decision | Why we chose it | Alternative | Trade-off |
|---|---|---|---|
| `sync.RWMutex` + `map[string]*Worker` | Read-heavy registry: many reads (status checks), few writes (heartbeats). Map of pointers avoids copying large structs on write. | `sync.Map`, plain `Mutex`, or map of values. | RWMutex: readers block only on writers. Map-of-pointers: less copying on write, but pointer indirection. |
| Inject clock `now func() time.Time` | Tests control time without sleeping; deterministic lifecycle tests. Follows existing DI pattern (`server.NewHandler(collect)`). | Global `var now = time.Now`. | Slightly longer constructor signature; tests are deterministic and fast. |
| `View` excludes `SecretHash` | Secrets never leave the registry; defense in depth if registry contents are logged/inspected. | Return `*Worker` directly. | Allocation + `toView` function per read; but prevents secret leakage and caller mutation. |
| Defensive copies on `Get`/`List` | Maps/slices are reference types; callers can't mutate stored state. | Return live references. | Per-read allocation; safe boundary. |
| `LastSeen` = coordinator `now()`, never `hb.SentAt` | Distributed clocks disagree (skew); agent clock can be wrong or malicious. | Use agent's `SentAt`. | Trusts coordinator clock; single source of truth for liveness. |
| Store `sha256` hash, not plain secret | If registry is dumped/inspected, secrets aren't exposed. | Store plain secret. | Secret is unrecoverable if registration response is lost (must re-register) — acceptable for this design. |
| `ErrUnknownWorker` sentinel + bool from `CheckSecret` | Handler decides HTTP status mapping (`Get` error → 404; `CheckSecret` bool → 401/403). | Custom error types for everything. | Sentinel + `errors.Is` is simple and idiomatic Go; `CheckSecret` returns bool to avoid id-enumeration via error messages. |
| `List` sorted by Hostname then ID | Deterministic output for tests and API consumers. | Unsorted map iteration. | Small sort cost per read; stable output worth it. |
| Registry knows nothing about HTTP | Separation of concerns: storage is independent of transport. | Put HTTP logic in registry. | Registry is reusable and testable without `net/http`; handler wires it later. |

---

## 7. Go concepts and patterns learned

### Concepts used

**Structs** — `Worker`, `View`, `Registry` define the data model. `Worker` is the stored record; `View` is the read model (without `SecretHash`).

**Methods** — `RecordHeartbeat`, `CheckSecret`, `Get`, `List`, `toView` all have `*Registry` receiver, operating on the registry's state.

**Pointers** — `Registry` methods use pointer receivers (`*Registry`) because they mutate state (or need to share the mutex). `toView` takes `*Worker` to avoid copying the full struct just to read it.

**Errors** — `ErrUnknownWorker` sentinel; `Get` returns `(*View, error)`; callers use `errors.Is`. `RecordHeartbeat` and `CheckSecret` don't return errors (void/bool are sufficient).

**`sync.RWMutex`** — `mu sync.RWMutex` field. `RLock`/`RUnlock` for concurrent reads; `Lock`/`Unlock` for writes. Held while copying data out of the map.

**Maps and slices are reference types** — `Labels map[string]string` and `LoadedModels []string` must be copied when stored or returned, or callers can mutate internal state. Demonstrated in `toView` and the defensive-copy tests.

**Fixed-size arrays** — `SecretHash [32]byte` is a value type; comparable, copied by value. Sliced to `[]byte` only when passed to `subtle.ConstantTimeCompare`.

**Functions as values (lightweight DI)** — `now func() time.Time` injected into `New`. Same pattern as `server.NewHandler(collect func() (specs.Specs, error))`.

**`time.Time`** — `LastSeen time.Time`; zero value means never seen (`StatusFor` returns `"pending"`). `IsZero()` checks for zero time.

**`crypto/subtle`** — `ConstantTimeCompare` for secret verification.

**`crypto/sha256`** — `Sum256` for one-way hashing.

**`sort.Slice`** — Deterministic ordering in `List`.

### Design patterns present

| Pattern | Where it appears | Problem it solves | Why useful | Simpler alternative |
|---|---|---|---|---|
| Dependency Injection (explicit) | `New(now func() time.Time)` | Production and test wiring differ only in the clock passed in. | Fake clock makes lifecycle testable without sleeping. | Global clock variable — simpler but hides dependencies, makes tests flaky. |
| Copy-on-read / Read model (View vs Worker) | `View` type; `toView` method | What you return is not what you store — secrets omitted, copies returned. | Prevents secret leakage and caller mutation of internal state. | Return `*Worker` directly — cheaper but leaks internals. |
| Pure function | `StatusFor(lastSeen, now)` (status.go, Step 1.1) | Status computation separated from storage. | Each piece is testable alone; no hidden state. | Compute status on write and store it — risks stale status. |

None of these are formal GoF patterns; they are idiomatic Go practices. The registry does not implement Repository (no interface yet), middleware, or observer — those are future candidates noted in EVERYTHING_EXPLAINED.md.

---

## 8. Testing explained

### Tests created

**`internal/coordinator/registry/registry_test.go`** (new file, 9 tests):

| Test | What it proves | Why important | Edge cases covered |
|---|---|---|---|
| `TestRecordHeartbeatAndGet` | Heartbeat upserts a worker; `Get` returns correct fields; `LastSeen` = coordinator clock. | Core write/read path works. | Verifies `LastSeen` equals `clock.Now()`, not agent `SentAt`. |
| `TestGetUnknownWorkerReturnsErrUnknownWorker` | Missing id returns `ErrUnknownWorker`. | Callers need to distinguish "not found" from other errors. | Uses `errors.Is` for sentinel check. |
| `TestCheckSecret` | Correct secret → true; wrong/unknown/empty → false. | Secret verification works; no id enumeration (unknown id → false, not error). | Wrong secret, unknown worker id, empty secret. |
| `TestStatusLifecycleWithFakeClock` | `pending` → `online` → `stale` → `offline` via clock advancement. | Lifecycle matches `StatusFor` boundaries exactly (strict `<` at 15s/45s). | Exactly 15s (stale), exactly 45s (offline), just-under boundaries. |
| `TestLastSeenUsesCoordinatorClockNotAgentClock` | `LastSeen` = coordinator time even when agent `SentAt` is 2020. | Clock skew can't corrupt liveness. | Agent clock far in the past. |
| `TestGetReturnsDefensiveCopies` | Mutating returned `View.Labels`/`LoadedModels` doesn't affect storage. | Callers can't corrupt registry internals via returned references. | Deleting/adding labels; appending to models slice. |
| `TestListIsSortedAndReturnsCopies` | `List` returns workers sorted by Hostname then ID; copies are independent. | Deterministic output; no shared state between calls. | Three workers in non-sorted insertion order. |
| `TestListEmpty` | Empty registry returns empty non-nil slice. | Callers can range safely; JSON marshals as `[]` not `null`. | Zero workers. |
| `TestRegistryConcurrentAccess` | 50 writers + 50 readers × 100 iterations each with no data race. | Concurrency safety under load; `RWMutex` protects the map. | Detected by `go test -race`. |

Plus the pre-existing `TestStatusFor` in `status_test.go` (7 boundary cases).

### Tests actually executed

All tests were executed with `go test -race ./internal/coordinator/registry/...` — all 16 test functions passed (9 new registry tests + 7 `TestStatusFor` sub-tests). Full project `go test -race ./...` also passed (all packages: `api`, `coordinator`, `registry`, `server`).

### Commands executed
- `go vet ./internal/coordinator/registry/...` — clean (exit 0)
- `go test -race -v ./internal/coordinator/registry/...` — all PASS
- `go vet ./...` — clean (exit 0)
- `go test -race ./...` — all packages PASS

### No tests were merely written without running — all were executed and passed.

---

## 9. Execution walkthrough

**Scenario: A worker sends its first heartbeat, then we check its status over time.**

1. **Setup:** `clock := newFakeClock(2026-11-11 12:00:00 UTC)`; `reg := New(clock.Now)`.

2. **Heartbeat arrives:** Handler (future step) validates `api.Heartbeat{WorkerID: "wrk-1", RAMTotalBytes: 8_000_000_000, ...}` and calls `reg.RecordHeartbeat("wrk-1", hb)`.
   - `RecordHeartbeat` takes write lock, creates `Worker{ID: "wrk-1", RAMTotalBytes: 8_000_000_000, LastSeen: 12:00:00, Last: hb, Status: "online"}`, stores in map, releases lock.

3. **Immediate read:** `reg.Get("wrk-1")` → read lock, `toView` → `View{ID: "wrk-1", Status: "online", LastSeen: 12:00:00, ...}`. Caller sees `status: "online"`.

4. **14.999 seconds later:** Clock advanced to 12:00:14.999. `reg.Get("wrk-1")` → `StatusFor(12:00:00, 12:00:14.999)` → age 14.999s < 15s → `"online"`.

5. **15.000 seconds later:** Clock advanced to 12:00:15.000. `reg.Get("wrk-1")` → age 15s, not < 15s → `"stale"`. Strict `<` means exactly 15s is stale.

6. **45.000 seconds later:** Clock at 12:00:45.000 → age 45s, not < 45s → `"offline"`.

**Failure scenario: Unknown worker.**
- `reg.Get("nope")` → not in map → `nil, ErrUnknownWorker`. Caller maps to HTTP 404.

**Failure scenario: Wrong secret.**
- `reg.CheckSecret("wrk-1", "wrong")` → hash mismatch → `false`. Caller maps to HTTP 401/403. Unknown id also → `false` (no id enumeration).

---

## 10. Important lessons

- **Store the hash, not the secret.** `SecretHash [32]byte` is the only secret-related field, and `View` omits it entirely. Never log or return plaintext secrets.
- **Never trust the other machine's clock.** `LastSeen` uses the coordinator's `now()`; agent `SentAt` is recorded but not used for liveness. Clock skew is a real distributed-systems problem.
- **If you hand out a map or slice, hand out a copy.** Go maps and slices are reference types. `toView` copies `Labels` and `LoadedModels` so callers can't mutate storage. The defensive-copy test proves this.
- **Compute status on read, not on write.** `Status` is derived from `LastSeen` + `now()` at read time via `StatusFor`. It can never go stale in the record.
- **Use `sync.RWMutex` for read-heavy state.** Reads (`Get`, `List`) use `RLock` (concurrent); writes (`RecordHeartbeat`) use `Lock` (exclusive).
- **Inject the clock for testability.** `now func() time.Time` lets tests advance time deterministically instead of sleeping for 45 seconds.
- **Return `*View`, not `*Worker`.** The read model omits `SecretHash` and is a defensive copy — the caller gets what they need, nothing more.

What I should be able to explain to another developer:
- Why `LastSeen` uses the coordinator's clock and not the agent's.
- Why `View` has no `SecretHash` field.
- Why `Get` returns `ErrUnknownWorker` (sentinel) rather than a custom error.
- Why `List` returns a sorted, empty-non-nil slice.
- Why defensive copies matter for maps and slices in Go.

---

## 11. Questions to test my understanding

1. Why does `RecordHeartbeat` stamp `LastSeen` with `r.now()` (coordinator time) instead of `hb.SentAt` (agent time)? What would break if we used `SentAt`?

2. Why does `View` omit `SecretHash`, and why does `toView` copy the `Labels` map and `LoadedModels` slice instead of returning references to the stored `Worker`'s fields?

3. Why does `CheckSecret` return `false` for an unknown worker instead of returning an error? What security property does this protect?

4. What does `sync.RWMutex` buy us over a plain `sync.Mutex` in this registry? When would a writer starve readers?

5. The `Status` field is computed in `toView` via `StatusFor(w.LastSeen, r.now())`, not stored on the `Worker`. What problem does compute-on-read prevent, and what is the cost?

*(Answers not provided — ask me to quiz you when ready.)*

---

## 12. Future considerations

| Category | Details |
|---|---|
| **Necessary now** | None — Step 1.2 is complete and verified. The registry is a standalone building block. |
| **Potential improvements later** | Wire `RecordHeartbeat` into the heartbeat handler (so heartbeats actually persist). Add `flag.Parse()` to `cmd/coordinator/main.go` (currently defined but ignored). Wire secret verification into the heartbeat handler. Add `GET /v1/workers` endpoint using `List()`. |
| **Unnecessary complexity at this stage** | Persistence (database) — in-memory is correct for Phase 0; restart loses state, which is acceptable while registering is re-doable. Sweep/reaper for offline workers — the `reported` map placeholder exists but Sweep is Step 1.5. Background goroutine for status updates — compute-on-read is simpler and correct. |

---

**Documentation status:** Complete for Step 1.2. Task document created at `docs/with-tasks-explained/task-001-in-memory-registry.md`. Index updated in `docs/with-tasks-explained/README.md`.