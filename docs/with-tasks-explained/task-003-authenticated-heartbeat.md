# Task 003 — Authenticated Heartbeat + Worker List (Step 1.4)

## 1. Task overview

**Task name and ID:** task-003-authenticated-heartbeat (project Step 1.4)

**Problem being solved:** In Step 1.3 any registered worker could send heartbeats, but heartbeats were not authenticated — anyone could forge a heartbeat for any worker id, and there was no way to list registered workers. Worse, I discovered `Registry.RecordHeartbeat` (Step 1.2) *replaced* the whole worker record, zeroing out the `SecretHash` and erasing registration fields (hostname, OS, arch, labels) after the first heartbeat, which would have broken `CheckSecret`. Separately, `api.HeartbeatResponse` had broken JSON tags (`next_heartbeat_second`, `mini_agent_version`).

**Goal:**
- `POST /v1/workers/{id}/heartbeat` wrapped in `requireWorker` auth middleware with a clear status contract: 404 unknown id, 401 missing/wrong `Bearer`, 400 invalid body, 400 `WorkerID != path id`, 200 + `api.HeartbeatResponse{NextHeartbeatSeconds:5, MinAgentVersion:"0.1.0"}`.
- `GET /v1/workers` returns `registry.List()` as JSON (`[]` not `null` when empty).
- `registry.View` snake_case keys, **no** `secret`/`hash`.
- `last_seen` stamped with coordinator clock; never use `hb.SentAt`.
- Body limited to 1 MB; oversize → 413; bad JSON → 400.

**Why this task is necessary in the overall project:** Authenticated heartbeats are essential for a trustworthy distributed system — only a registered worker that possesses its secret can update its own liveness, preventing spoofing. The worker list enables observability (and the future `GET /v1/workers` admin view) so the coordinator can show who is alive. Fixing `RecordHeartbeat` ensures the secret and registration data survive the constant stream of heartbeats. This is the security/contract layer that Step 1.8's agent will rely on.

**Previous task or dependency:** Step 1.3 (Register Endpoint) + Step 1.2 (registry `CheckSecret`, `RecordHeartbeat`, `List`, `View`, `HashSecret`).

---

## 2. Architecture and workflow

```mermaid
flowchart TD
    A[Agent POST /v1/workers/{id}/heartbeat + Authorization: Bearer secret] --> B[requireWorker middleware]
    B --> C{worker id in registry?}
    C -->|no| D[404 unknown worker]
    C -->|yes| E{Bearer token valid + CheckSecret constant-time?}
    E -->|no| F[401 unauthorized]
    E -->|yes| G[handleHeartbeat]
    G --> H[decode JSON; reject oversize -> 413]
    H --> I[hb.Validate -> 400]
    I --> J[WorkerID == path id -> 400 mismatch]
    J --> K[Registry.RecordHeartbeat id hb stamped with coordinator now]
    K --> L[200 + HeartbeatResponse next_heartbeat_seconds=5 min_agent_version=0.1.0]
    M[GET /v1/workers] --> N[handleListWorkers]
    N --> O[Registry.List sorted Views, no secrets, [] not null]
```

**Middleware pattern:** `requireWorker(next http.HandlerFunc) http.HandlerFunc`. `requireWorker` returns a closure `func(w,r){...}` that does auth checks then calls `next(w,r)`. Registered as `mux.HandleFunc("POST /v1/workers/{id}/heartbeat", s.requireWorker(s.handleHeartbeat))`. `GET /v1/workers` is NOT wrapped (open for now; admin login in Step 2.3).

**Critical ordering — 404 checked BEFORE 401:** Unknown worker returns 404 even without auth. Checking 404 first prevents revealing whether a worker id exists (auth would otherwise leak id enumeration).

**RecordHeartbeat now updates in place:** preserves `SecretHash`, `Hostname`, `OS`, `Arch`, `CPUModel`, `CPUCores`, `Labels`. Only refreshes `LastSeen`, `Last`, `RAMTotalBytes`, `Status`. Previously it replaced the record and zeroed `SecretHash`.

---

## 3. Files changed

| File | Responsibility | What changed |
|---|---|---|
| `internal/coordinator/server.go` | Coordinator HTTP handlers | Added `requireWorker` middleware, `bearerToken`, `handleHeartbeat`, `handleListWorkers`. Replaced standalone `handlerHeartbeat` with `(*Server).handleHeartbeat`. Heartbeat now wrapped by `requireWorker`, gets `r.PathValue("id")`, 1MB body limit (413), `WorkerID==path` check, `RecordHeartbeat`, returns `api.HeartbeatResponse`. `GET /v1/workers` route added. `handleRegister` now reads body via `io.ReadAll`, returns **413** on `*http.MaxBytesError`, and passes `specs.Cores`. |
| `internal/api/types.go` | Shared wire types | Fixed `HeartbeatResponse` JSON tags: `next_heartbeat_second`→`next_heartbeat_seconds`, `mini_agent_version`→`min_agent_version`. |
| `internal/api/types_test.go` | API tests | Strengthened round-trip test with exact key list; added `TestHeartbeatResponseJSONKeys` asserting exact `{"next_heartbeat_seconds":5,"min_agent_version":"0.1.0"}`. |
| `internal/coordinator/registry/registry.go` | Registry | **Fixed `RecordHeartbeat`** to update in place (no longer resets `SecretHash`/erases fields). Added `CPUCores` field (`cpu_cores`). `View.Hostname` tag `host_name`→`hostname`. Applied defensive copy to `View.Last.LoadedModels`. Extended `Register` with `cpuCores`. |
| `internal/coordinator/server_test.go` | Coordinator tests | Added `TestRequireWorkerStatusContract`, `TestRequireWorkerChecks404Before401`, `TestHandleHeartbeatFullFlow`, `TestHandleListWorkersSecurity`, `TestHandleListWorkersEmptyReturnsNotNull`, `TestWorkerStatusWithFakeClock`, `TestHandleRegisterBodyTooLargeIntegration`. Updated `TestHeartbeatHandler`, `TestHandleRegisterBodyTooLarge`. Pinned 413. |
| `docs/with-tasks-explained/README.md` | Task index | Added task-003 entry. |
| `docs/with-tasks-explained/task-003-authenticated-heartbeat.md` | This document | Created. |

---

## 4. Functions and methods explained

### `func (s *Server) requireWorker(next http.HandlerFunc) http.HandlerFunc`

- **Purpose:** HTTP middleware that authenticates a worker before calling `next`.
- **Signature:** takes a `next` handler, returns a `http.HandlerFunc` closure.
- **Step-by-step:**
  1. `id := r.PathValue("id")`.
  2. `s.reg.Get(id)`; if `ErrUnknownWorker` → **404**; other error → 500. **Checked first.**
  3. `bearerToken(r.Header.Get("Authorization"))`; `ok==false`, empty token, or `!s.reg.CheckSecret(id, secret)` → **401**. Never logs secret/header.
  4. `next(w, r)`.
- **Why designed this way:** Middleware composes around a handler; ordering 404-before-401 prevents id-enumeration. `CheckSecret` is constant-time.
- **Who calls it:** `NewHandler` wraps `s.handleHeartbeat`.
- **What calls it next:** `s.handleHeartbeat` on success.

### `func bearerToken(header string) (token string, ok bool)`

- Parses `Authorization`. `""`→not ok; `strings.SplitN(header," ",2)`; requires scheme `Bearer` (case-insensitive `EqualFold`); `TrimSpace`; empty token→not ok. Called by `requireWorker`.

### `func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request)`

- **Purpose:** Process an authenticated heartbeat. (Auth already done by middleware.)
- **Step-by-step:** `id := r.PathValue("id")`; 1MB `http.MaxBytesReader`; `json.NewDecoder.Decode(&hb)`; on `*http.MaxBytesError`→413, else→400; `hb.Validate()`→400; `hb.WorkerID != id`→400; `s.reg.RecordHeartbeat(id, hb)`; `api.HeartbeatResponse{5,"0.1.0"}`; 200 JSON.
- **Never uses `hb.SentAt`** for `LastSeen`.
- **Who calls it:** `requireWorker`. **What calls it next:** nothing.

### `func (s *Server) handleListWorkers(w http.ResponseWriter, r *http.Request)`

- **Purpose:** `GET /v1/workers`. `s.reg.List()` (non-nil empty slice); JSON 200. No secrets.
- **Who calls it:** mux route. Not wrapped by `requireWorker`.

### `func (r *Registry) RecordHeartbeat(id string, hb api.Heartbeat)` [fixed]

- **Before:** created a brand-new `Worker`, resetting `SecretHash` to zero and erasing registration fields.
- **After:** `w, ok := r.workers[id]`; if !ok create; set `RAMTotalBytes`, `LastSeen = r.now()`, `Last = hb`, `Status`. **Preserves `SecretHash`, `Hostname`, `OS`, `Arch`, `CPUModel`, `CPUCores`, `Labels`.** Write lock.

### `func (r *Registry) CheckSecret(id, secret string) bool` [used]

- `subtle.ConstantTimeCompare(w.SecretHash[:], sha256(secret)[:])`. Unknown id→false.

### `func (r *Registry) List() []*View` [used]

- Non-nil empty slice; defensive `toView` copies; sorted by Hostname then ID. No `SecretHash`.

---

## 5. Algorithms and logic

| Technique | Problem | How | Why | Complexity |
|---|---|---|---|---|
| Middleware `func(HandlerFunc) HandlerFunc` | Reusable auth wrapper | Returns closure doing checks then `next` | Compose auth once, apply to any route | O(1) + `next` |
| Constant-time compare (`subtle.ConstantTimeCompare`) | Timing attacks on secret | `==1` on SHA-256 `[32]byte` slices | No early-exit timing leak | O(32) |
| 404-before-401 | id enumeration | check `Get` then `CheckSecret` | Don't reveal existence | — |
| `r.PathValue("id")` (Go 1.22) | extract path param | routing sets it | no manual parsing | O(1) |
| Bearer parsing | parse `Authorization` | `SplitN`, `EqualFold`, `TrimSpace` | robust to scheme case/spacing | O(n) |
| Update-in-place upsert | don't lose secret | reuse existing `*Worker`, set fields | preserve registration + secret across heartbeats | O(1) |
| Compute-on-read status | never stale status | `StatusFor(LastSeen, now())` in `toView` | no background job | O(1) |
| Defensive copy | prevent caller mutation | copy `Labels` + `Last.LoadedModels` | maps/slices are references | O(m+k) |
| `http.MaxBytesReader` + `io.ReadAll` + `*http.MaxBytesError` | body size / 413 | `io.ReadAll`, `errors.As`→413 | stream-safe limit | O(n) |
| Non-nil empty slice | `[]` not `null` | `make(...,0,len)` | JSON correctness | — |

---

## 6. Engineering decisions

| Decision | Why | Alternative | Trade-off |
|---|---|---|---|
| Middleware `requireWorker` | Reusable auth; route registered `s.requireWorker(s.handleHeartbeat)` | Inline checks in handler | One layer of indirection |
| 404 checked before 401 | Don't reveal whether worker id exists | 401 first | Unknown id→404 even without auth |
| `Bearer` scheme | Industry standard (`Authorization: Bearer`) | Custom header | Client must send exactly Bearer |
| Constant-time compare | Timing-attack resistance | `==` on bytes | Always full-length compare |
| Update-in-place `RecordHeartbeat` | Don't erase `SecretHash`/registration | Replace worker (old bug) | Must read-then-update under lock |
| Compute-on-read status | never stale | store status + sweeper | recompute per read (trivial) |
| 413 oversize | clear contract + DoS protection | 400 only | detect `*http.MaxBytesError` |
| View excludes secret | never leak secret/hash | return `*Worker` | copy cost |
| `snake_case` JSON tags incl `hostname`, `cpu_cores`, `next_heartbeat_seconds`, `min_agent_version` | wire contract | various | tags not compile-checked (now tested) |
| Heartbeat response `NextHeartbeatSeconds=5` | agent scheduling | hardcode elsewhere | centralize |

---

## 7. Go concepts and patterns learned

**Middleware as `func(http.HandlerFunc) http.HandlerFunc`:** `requireWorker` takes `next` and returns a closure `func(w,r)`. Closures capture `s`. Registered as `mux.HandleFunc(pattern, s.requireWorker(s.handleHeartbeat))`. Go has no built-in middleware type; this signature is the idiom.

**Go 1.22 routing + `r.PathValue`:** `"POST /v1/workers/{id}/heartbeat"` + `r.PathValue("id")`. Method matching built in (`GET`, `POST`); unknown method → 405 automatically.

**Why 404 before 401:** check `registry.Get(id)` first. Unknown id→404 without requiring auth. Prevents probing whether a worker id exists. Auth only runs for existing workers.

**`Authorization` parsing + `subtle.ConstantTimeCompare`:** `strings.SplitN`, `EqualFold`, `TrimSpace`. Constant-time compare prevents timing attacks. Secret never logged.

**`errors.As` for `*http.MaxBytesError`:** `io.ReadAll(r.Body)`, `errors.As(err,&maxErr)`. Return 413. (`json.NewDecoder` wraps the error, hence `io.ReadAll` first.)

**Struct tags:** `json:"snake_case"`. Typos (`josn`,`joson`,`next_heartbeat_second`,`mini_agent_version`,`host_name`) silently break JSON. Now pinned by tests.

**Maps/slices are reference types:** defensive-copy `Labels` and `Last.LoadedModels`. Struct value copy still shares slice backing.

**Sentinel error:** `registry.ErrUnknownWorker` + `errors.Is`. Handler maps→404.

**Why `View` instead of `*Worker`:** omit `SecretHash`, return copies, read model. No secret leakage.

**Patterns present:** Middleware (not named GoF), Dependency Injection (`NewHandler(reg)`), Copy-on-read read model, Pure function (`StatusFor`), Sentinel error. Not claimed otherwise.

---

## 8. Testing explained

**Executed** (`go test -race ./...`, all pass; `go vet ./...` clean):

`TestRequireWorkerStatusContract` (table): unknown→404; missing header→401; `Basic`→401; wrong secret→401; `"Bearer "`→401; `"Bearer"`→401; correct→200; asserts `next` called iff success. `TestRequireWorkerChecks404Before401`. `TestHandleHeartbeatFullFlow`: register→heartbeat→200+HeartbeatResponse→GET list online hostname + last.cpu_percent. `TestHandleListWorkersSecurity`: no `"secret"`/`"hash"`, snake_case keys. `TestHandleListWorkersEmptyReturnsNotNull`: `[]`. `TestWorkerStatusWithFakeClock`: online→(+20s)stale→(+30s)offline. `TestHandleRegisterBodyTooLarge(Integration)`: 413. `TestHeartbeatHandler`: valid/ram/cpu/worker_id/invalid-json/worker_id-mismatch + 200 response. Pre-work `TestHeartbeatResponseJSONKeys`. Registry `TestRegister`, `TestRecordHeartbeatAndGet`, `TestCheckSecret`, `TestRegistryConcurrentAccess` (50×100 race).

**Manual (real server, verified):** register→201; heartbeat Bearer→`{"next_heartbeat_seconds":5,"min_agent_version":"0.1.0"}`; no auth→401; wrong secret→401; unknown→404; ONLINE + OFFLINE checkpoints; no `secret`/`hash`; `cpu_cores:4`.

---

## 9. Execution walkthrough

**Registered worker `wrk_8a979bcd112f` ("lab-a", secret=...).**
1. `POST /v1/workers/register` → `Register(...SecretHash=sha256(secret), cpuCores=4...)`. Worker created. 201.
2. `POST /v1/workers/wrk_8a979bcd112f/heartbeat`, `Authorization: Bearer <secret>`, body `{...worker_id:"wrk_8a979bcd112f",cpu_percent:5...}`.
3. `requireWorker`: `Get(id)` exists→; `bearerToken("Bearer <secret>")`→token; `CheckSecret` sha256 match→; `next`.
4. `handleHeartbeat`: decode; `Validate()`; `WorkerID==id`; `RecordHeartbeat(id,hb)` stamps `LastSeen=coordinator now`, stores `Last`, **keeps `SecretHash`**; 200 `{"next_heartbeat_seconds":5,"min_agent_version":"0.1.0"}`.
5. `GET /v1/workers` → `List()` → online View (no secret). Status decays to `stale` (+15s) / `offline` (+45s) without new heartbeats.

**Failure:** unknown id→404; no/bad Bearer→401; invalid JSON→400; `Validate`→400; `WorkerID!=path`→400; body>1MB→413.

---

## 10. Important lessons

- **Middleware `func(HandlerFunc) HandlerFunc` + closures** is Go's idiom for wrapping handlers.
- **Check 404 before 401** to avoid leaking which ids exist.
- **`Authorization: Bearer <token>`**: parse scheme case-insensitively, trim space, reject empty.
- **Constant-time secret compare** (`subtle.ConstantTimeCompare` on hashes).
- **`r.PathValue`** (Go 1.22).
- **JSON tags aren't checked at compile time** — `josn`/`joson`/`next_heartbeat_second` silently broke; pin keys in tests.
- **Update in place** — don't rebuild a record and lose `SecretHash`/registration. `RecordHeartbeat` was a real bug; now preserves everything but liveness.
- **`io.ReadAll` + `errors.As(*http.MaxBytesError)`→413**; `json.NewDecoder` wraps the error.
- **`[]` not `null`** for empty JSON arrays.
- **`View` read-model** prevents secret leakage + caller mutation; **defensive-copy maps/slices**.
- **Compute-on-read status** never stale; `LastSeen` uses coordinator clock, not `hb.SentAt`.

---

## 11. Questions to test my understanding

1. Why is `requireWorker` `func(http.HandlerFunc) http.HandlerFunc`, and how does the returned closure get `s` and `next`? Why must `next` only be called on success?
2. Why is unknown-worker (404) checked before the secret (401)? Give the security reason.
3. Why does `subtle.ConstantTimeCompare` exist, and what would `==` on the secret bytes leak?
4. Explain `r.PathValue("id")` + `"POST /v1/workers/{id}/heartbeat"`. Where does `hb.WorkerID != id` fit?
5. Why must `RecordHeartbeat` preserve `SecretHash`? What happened before, and why would subsequent `requireWorker` calls fail?
6. `http.MaxBytesReader` + `io.ReadAll` + `errors.As(*http.MaxBytesError)`→413. Why `io.ReadAll` first? Why was a tag `next_heartbeat_second` a bug?

*(Answers not provided — ask to be quizzed.)*

---

## 12. Future considerations

**Necessary now:** None. **Later:** admin auth for `GET /v1/workers` (Step 2.3); `Seq` ordering (currently unused); log heartbeat rejects; metrics. **Unnecessary now:** persistence; background sweeper; token expiry; `CPUModel` from string; repository interface.

---

**Status:** Step 1.4 complete + verified (`go vet` clean, `go test -race` all pass, manual checks, ONLINE+OFFLINE checkpoint).