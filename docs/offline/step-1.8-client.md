# Step 1.8 — Agent client and heartbeat loop

## 1. Goal and why it exists in the system (3-5 sentences)

The agent's job is to stay registered and keep reporting liveness to the
coordinator. `Run` orchestrates: load (or create) an identity, then heartbeat
in a loop, reacting to auth failures by re-registering. It must never exit
just because the coordinator is down — only `ctx` cancellation stops it.
Every request is cancellable via `http.NewRequestWithContext` and the client
has a 5s timeout so a hung coordinator can't stall the agent. Until Phase 2,
metrics are placeholders (`CPUPercent=0`, `RAMAvailableBytes=RAMTotalBytes`).

## 2. Go concepts I need first

**`http.NewRequestWithContext(ctx, method, url, body)`.**
Builds a request bound to a context. If ctx is cancelled, in-flight requests
abort and return `ctx.Err()`. No dangling goroutines.

```go
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
resp, err := client.Do(req)
```

**`http.Client{Timeout: ...}`.**
Caps total request duration (connect + headers + body). Independent of ctx;
defense in depth. Without it, a hung server blocks forever.

```go
client := &http.Client{Timeout: 5 * time.Second}
```

**`select` with multiple channels.**
Waits for the first ready channel. Used for cancellable timers.

```go
select {
case <-ctx.Done():
    return
case <-timer.C:
    doWork()
}
```

**`time.NewTimer` / `time.AfterFunc`.**
One-shot timer (vs `time.NewTicker`). `defer timer.Stop()` prevents leaks.
For backoff waits, use `time.NewTimer` + select ctx.Done.

```go
timer := time.NewTimer(d)
defer timer.Stop()
select { case <-ctx.Done(): case <-timer.C: }
```

**`errors.Is` and sentinel errors.**
Package-level `var ErrX = errors.New(...)`. Callers use `errors.Is` to
classify without string matching.

```go
if errors.Is(err, ErrUnauthorized) { /* forget identity */ }
```

**`slog.Logger`.**
Structured logging. `log.Warn(...)`, `log.Error(...)`. Injected for testability.

```go
log.Warn("heartbeat failed", "err", err, "attempt", n)
```

**`context.Context` as first parameter.**
Convention: ctx is always first. Never store in struct. Pass through.

```go
func do(ctx context.Context, x int) error
```

**Pointers and nil checks.**
`*Identity` allows nil; `c.id` is `*Identity`. Check `c.id == nil`.

```go
if c.id == nil { /* register */ }
```

## 3. Exact spec: types, signatures, rules

```go
var ErrUnauthorized, ErrUnknownWorker error // returned for 401 and 404
type Client struct {
    base              string
    http              *http.Client // 5s timeout
    id                *Identity
    dir               string
    log               *slog.Logger
    heartbeatInterval time.Duration // unexported; tests set it; default 5s
}
func NewClient(base, dataDir string, log *slog.Logger) *Client
func (c *Client) Register(ctx context.Context, token string, s specs.Specs) error
func (c *Client) sendHeartbeat(ctx context.Context, hb api.Heartbeat) (api.HeartbeatResponse, error)
func (c *Client) Run(ctx context.Context, token string, s specs.Specs)
```

Rules:
- `NewClient`: set `http` with `Timeout: 5*time.Second`; `heartbeatInterval` default 5s; `log` default if nil.
- `Register`: POST `/v1/workers/register` with `{join_token, specs}`; on 201 decode `RegisterResponse`, set `c.id = &Identity{WorkerID, Secret}`, `SaveIdentity(c.dir, *c.id)`; on error return err (no identity saved).
- `sendHeartbeat`: POST `/v1/workers/{id}/heartbeat` with `Authorization: Bearer <secret>`, body `api.Heartbeat`. Return `ErrUnauthorized` on 401, `ErrUnknownWorker` on 404. Other non-2xx → wrapped error. Decode `HeartbeatResponse` on 200.
- `Run`: load identity via `LoadIdentity(c.dir)`; if `ErrNoIdentity` → `Register` (retry with `Backoff` until success or ctx ends). Loop: `seq++`, build heartbeat (`CPUPercent=0`, `RAMAvailableBytes=RAMTotalBytes`, `SentAt=now`, `LoadedModels=[]`), `sendHeartbeat`. On success: reset failures, adopt `NextHeartbeatSeconds` as interval. On `ErrUnauthorized`/`ErrUnknownWorker`: forget identity (`c.id=nil`), `DeleteIdentity`, register again. On other error: `log.Warn`, `attempt++`, wait `Backoff(...)`. Return on `ctx.Done()`.
- HTTP 400: log at **Error** level ("bug in agent payload"), keep normal interval (do not backoff).
- Always `http.NewRequestWithContext`. Both waits (heartbeat timer, backoff) use `select` with `ctx.Done()`.
- `heartbeatInterval` unexported field tests set (e.g. 10ms) for short-interval testing.

## 4. Algorithm in plain-English numbered steps (no Go code)

**Run(ctx, token, s):**
1. Load identity from disk. If missing → go to Register (step 3).
2. If loaded → set `c.id`.
3. If no identity: call `Register`. On success → set `c.id`, save identity, go to loop. On error → log, `attempt++`, wait `Backoff(attempt, base, max, rand.Float64)` with `select ctx.Done`; if ctx done → return; retry step 3.
4. Enter heartbeat loop. Reset `attempt=0`, `seq=0`.
5. Wait for next heartbeat timer (`heartbeatInterval`) OR `ctx.Done()`. If ctx done → return.
6. `seq++`. Build heartbeat with `CPUPercent=0`, `RAMAvailableBytes=RAMTotalBytes`, `SentAt=now`, `LoadedModels=[]`.
7. Call `sendHeartbeat`. On success → adopt `NextHeartbeatSeconds` as new interval, `attempt=0`, go to 5.
8. On `ErrUnauthorized` or `ErrUnknownWorker` → forget identity (`c.id=nil`), `DeleteIdentity`, go to 3 (re-register).
9. On HTTP 400 → `log.Error("agent payload bug")`, keep interval, go to 5 (no backoff).
10. On other error → `log.Warn`, `attempt++`, wait `Backoff(attempt, base, max, rand.Float64)` with `select ctx.Done`; if ctx done → return; go to 5.

**Register(ctx, token, s):**
1. Build request `POST /v1/workers/register` with `{join_token: token, specs: s}`.
2. Send with `http.NewRequestWithContext`. On network error → return err.
3. If 201 → decode `RegisterResponse`, set `c.id`, `SaveIdentity(c.dir, *c.id)`, return nil.
4. If non-201 → return wrapped error.
5. On ctx cancel → return ctx.Err().

**sendHeartbeat(ctx, hb):**
1. Build request `POST /v1/workers/{id}/heartbeat` with `Authorization: Bearer <secret>`, body `hb`.
2. Send with `http.NewRequestWithContext`. On network error → return zero, err.
3. If 200 → decode `HeartbeatResponse`, return it, nil.
4. If 401 → return zero, `ErrUnauthorized`.
5. If 404 → return zero, `ErrUnknownWorker`.
6. If 400 → return zero, wrapped error (agent payload bug).
7. Other → return zero, wrapped error.

## 5. List of tests

| Test name | What it asserts | Why it matters |
|---|---|---|
| `TestClientRunNormalSeq` | seq goes 1,2,3; Authorization header "Bearer <secret>" | Basic heartbeat loop works |
| `TestClientRun404Reregisters` | fake returns 404 once → agent re-registers, keeps going | Coordinator restart recovery |
| `TestClientRun500Then200` | 500 three times then 200 → loop recovers | Transient failure recovery |
| `TestClientRunCancelWithin100ms` | cancelling ctx → Run returns within 100ms | No goroutine leak; clean shutdown |
| `TestClientNoIdentityRegistersAndSaves` | no identity at start → registers and saves identity | First-run onboarding |
| `TestClientCoordinatorDownAtStart` | coordinator down → Run doesn't panic, stops on cancel | Resilience to initial outage |
| `TestClient400NoTightLoop` | 400 → no tight loop (waits, not spin) | Avoids hammering on bad payload |

Tests use `httptest` fake coordinator + very short interval (set via unexported field). Tests for 1.8 are expected to FAIL until identity (1.6) is implemented.

## 6. Commands to run and exact output when done

```bash
cd ~/Desktop/node-agent
go vet ./internal/agent
go test -race ./internal/agent -run TestClient -v
go test -race ./...
```

Expected when 1.6+1.8 implemented: all `TestClient*` PASS, all packages ok. Until then: FAIL (identity missing).

## 7. Common mistakes and how tests reveal them

| Mistake | Symptom | Test |
|---|---|---|
| No `http.NewRequestWithContext` | Cancel doesn't abort in-flight requests | `TestClientRunCancelWithin100ms` |
| No 5s timeout | Hung server blocks forever | (manual) |
| `time.Sleep` instead of select | Cancel doesn't interrupt sleep | `TestClientRunCancelWithin100ms` |
| Treating 404 as fatal | Agent exits on coordinator restart | `TestClientRun404Reregisters` |
| Treating 500 as fatal | Agent exits on transient error | `TestClientRun500Then200` |
| Not saving identity | Re-registers every restart | `TestClientNoIdentityRegistersAndSaves` |
| Tight loop on 400 | 100% CPU | `TestClient400NoTightLoop` |
| Not resetting attempt on success | Backoff grows forever | `TestClientRun500Then200` (recovery) |
| Not adopting NextHeartbeatSeconds | Ignores server hint | `TestClientRunNormalSeq` (seq timing) |

## 8. 5 self-check questions (answers at the very end)

1. Why does `Run` never exit on coordinator-down, and what stops it?
2. Why `http.NewRequestWithContext` instead of `http.NewRequest`?
3. Why check `ErrUnauthorized` and `ErrUnknownWorker` separately from other errors?
4. Why `CPUPercent=0` and `RAMAvailableBytes=RAMTotalBytes` for now?
5. Why does 400 log at Error level and not backoff?

**Answers:**
1. The agent must stay alive on edge machines; coordinator outages are expected. Only `ctx.Done()` stops Run — on cancel, all in-flight requests abort and Run returns.
2. `NewRequestWithContext` binds the request to ctx, so canceling ctx aborts in-flight HTTP calls immediately. Without it, requests could hang until the 5s timeout.
3. Both mean "forget identity and re-register" (404 = coordinator lost us, 401 = secret revoked). Other errors are transient → backoff retry. Distinct sentinels let Run branch precisely.
4. Phase 1 has no real metrics; placeholders keep the wire format valid. Phase 2 replaces with real CPU/RAM.
5. 400 means our payload is buggy (we sent bad data); retrying faster won't fix it. Log at Error so the bug is visible; keep normal interval to avoid spamming.

## 9. Done checklist

- [ ] `internal/agent/client.go` implements all functions
- [ ] `go vet ./internal/agent` clean
- [ ] `go test -race ./internal/agent -run TestClient -v` all PASS (after 1.6+1.7 done)
- [ ] `go test -race ./...` all packages PASS
- [ ] seq goes 1,2,3 with Authorization header
- [ ] 404 → re-register and continue
- [ ] 500×3 then 200 → recovers
- [ ] cancel → return ≤100ms
- [ ] no identity → register + save
- [ ] coordinator down → no panic, stops on cancel
- [ ] 400 → no tight loop