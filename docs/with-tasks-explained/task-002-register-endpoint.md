# Task 002 — Register Endpoint (Step 1.3)

## 1. Task overview

**Task name and ID:** task-002-register-endpoint (project Step 1.3)

**Problem being solved:** The coordinator could validate heartbeats but had no way for a new worker to introduce itself and receive an identity (worker ID + secret). Without a register endpoint, there was no mechanism to onboard workers, store their hardware specs, or establish shared secrets for future authentication.

**Goal:** Implement `POST /v1/workers/register` that:
- Decodes `api.RegisterRequest` (join_token + specs) from the request body
- Generates a worker ID (`"wrk_"` + 6 random bytes hex) and a secret (32 random bytes hex)
- Stores the worker with hostname, OS, arch, RAM total from specs, and the SHA-256 hash of the secret
- Returns `api.RegisterResponse` (worker_id, secret, heartbeat_seconds=5) with HTTP 201
- The plain secret appears in the response **only** — never stored, never logged
- Limits request body to 1 MB via `http.MaxBytesReader`
- Returns 400 on bad JSON, 405 on wrong method

**Why this task is necessary in the overall project:** The register endpoint is the front door of the Team Compute Platform. It is how a new agent joins the coordinator, receives its identity, and gets a secret that will later be used for heartbeat authentication (Step 1.4). Without registration, workers cannot be individually identified or authenticated.

**Previous task or dependency:** Step 1.2 — In-Memory Worker Registry (`internal/coordinator/registry`). The register endpoint depends on `Registry.Register`, `Registry.Get`, `Registry.CheckSecret`, and `registry.HashSecret`, all built in Step 1.2.

---

## 2. Architecture and workflow

The register endpoint extends the coordinator HTTP server:

```mermaid
flowchart TD
    A[Agent POST /v1/workers/register with specs] --> B[Server.handleRegister]
    B --> C[Decode RegisterRequest JSON]
    C -->|bad JSON| D[Return 400]
    C -->|valid| E[newWorkerID() - crypto/rand]
    E --> F[newSecret() - crypto/rand]
    F --> G[HashSecret(secret) - sha256]
    G --> H[Registry.Register(id, specs, hash)]
    H --> I[Return 201 with worker_id + secret + heartbeat_seconds]
    I --> J[Plain secret in response body ONLY]
```

The `Server` struct groups handler dependencies (`reg` for the registry, `log` for logging). `NewHandler(reg)` builds the mux with both routes:
- `POST /v1/workers/{id}/heartbeat` → `handlerHeartbeat` (unchanged from Step 1.2)
- `POST /v1/workers/register` → `s.handleRegister`

The coordinator `main.go` creates a `Registry` via `registry.New(nil)` and passes it to `coordinator.NewHandler(reg)`.

---

## 3. Files changed

| File | Responsibility | Why it exists | What changed |
|---|---|---|---|
| `internal/coordinator/server.go` | Coordinator HTTP handlers: heartbeat + register | Defines how the coordinator serves HTTP requests | **Rewrote.** Added `Server` struct (`reg`, `log`), `newWorkerID()`, `newSecret()`, `handleRegister()` method. Updated `NewHandler()` to accept `*registry.Registry` and register both routes. |
| `cmd/coordinator/main.go` | Coordinator entry point: flags, logger, HTTP server, graceful shutdown | Starts the coordinator binary | **Modified.** Imports `registry`, creates `registry.New(nil)`, passes it to `NewHandler(reg)`, added `flag.Parse()`. |
| `internal/api/types.go` | Shared JSON wire types + validation | Types used by both agent and coordinator | **Modified.** Fixed JSON tag typos: `joson:"specs"` → `json:"specs"`, `josn:"worker_id"` → `json:"worker_id"`. |
| `internal/coordinator/registry/registry.go` | In-memory worker store | Worker registry built in Step 1.2 | **Modified.** Added `Register()` method to store a worker with specs fields + secret hash, using the injected clock. |
| `internal/coordinator/server_test.go` | Tests for coordinator HTTP handlers | Verifies handler behavior | **Modified.** Added `TestHandleRegister`, `TestHandleRegisterCreatesWorkerInRegistry`, `TestHandleRegisterBodyTooLarge`, `TestNewWorkerID`, `TestNewSecret`. Updated `TestHeartbeatHandler` to pass registry to `NewHandler`. |
| `internal/coordinator/registry/registry_test.go` | Tests for registry package | Verifies registry behavior | **Modified.** Added `TestRegister` unit test. |
| `docs/with-tasks-explained/README.md` | Task documentation index | Lists all completed tasks | **Modified.** Added task-002 entry. |
| `docs/with-tasks-explained/task-002-register-endpoint.md` | Task 002 documentation | Explains the register endpoint implementation | **Created.** This document. |

No unrelated files were modified.

---

## 4. Functions and methods explained

### `func newWorkerID() string`

- **Purpose:** Generate a unique, unpredictable worker ID.
- **Inputs:** None.
- **Outputs:** `string` — `"wrk_"` + 6 random bytes hex-encoded (16 chars total).
- **Step-by-step:**
  1. Allocate 6 bytes.
  2. Read cryptographically random bytes via `crypto/rand.Read`.
  3. If `rand.Read` fails (extremely unlikely), panic — crypto/rand failure indicates a broken system RNG.
  4. Hex-encode the 6 bytes (12 hex chars).
  5. Prefix with `"wrk_"`.
- **Errors it handles:** `crypto/rand.Read` error → panic (no graceful recovery; a broken RNG is a fatal condition).
- **Why designed this way:** Worker IDs must be unpredictable to prevent ID guessing attacks. `crypto/rand` provides cryptographic randomness.
- **Who calls it:** `Server.handleRegister`.
- **What calls it next:** Nothing — the ID is used directly as the worker key.

### `func newSecret() string`

- **Purpose:** Generate a high-entropy shared secret for a worker.
- **Inputs:** None.
- **Outputs:** `string` — 32 random bytes hex-encoded (64 chars).
- **Step-by-step:**
  1. Allocate 32 bytes.
  2. Read cryptographically random bytes via `crypto/rand.Read`.
  3. Panic on error (same reasoning as `newWorkerID`).
  4. Hex-encode the 32 bytes (64 hex chars).
- **Errors it handles:** `crypto/rand.Read` error → panic.
- **Why designed this way:** Secrets must be unguessable. 32 bytes = 256 bits of entropy, which is cryptographically strong.
- **Who calls it:** `Server.handleRegister`.
- **What calls it next:** The secret is hashed via `HashSecret` for storage, and returned plaintext in the response.

### `func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request)`

- **Purpose:** Handle `POST /v1/workers/register` — create a worker and return its identity.
- **Inputs:** `w http.ResponseWriter`, `r *http.Request` (standard HTTP handler signature).
- **Outputs:** None (writes HTTP response directly).
- **Step-by-step:**
  1. Wrap `r.Body` with `http.MaxBytesReader(w, r.Body, 1<<20)` — limits body to 1 MB.
  2. Decode JSON body into `api.RegisterRequest` using `json.NewDecoder`.
  3. On decode error → return 400 with readable error message.
  4. Call `newWorkerID()` and `newSecret()` to generate identity material.
  5. Call `registry.HashSecret(secret)` to compute SHA-256 hash.
  6. Call `s.reg.Register(id, hostname, os, arch, cpuModel, ramTotalBytes, secretHash)` to store the worker.
  7. Build `api.RegisterResponse{WorkerID: id, Secret: secret, HeartbeatSeconds: 5}`.
  8. Set `Content-Type: application/json`, write 201, encode response.
  9. On encode error → log via `slog` (never exposes the secret).
- **Important conditions:**
  - Body is limited to 1 MB to prevent memory exhaustion DoS.
  - The plain secret is written to the response body exactly once — never stored, never logged.
  - `cpuModel` is passed as 0 because `specs.Specs.CPU` is a string while `Worker.CPUModel` is int.
  - The join token is decoded but not validated (Step 1.10 will add enforcement).
- **Errors it handles:** JSON decode errors → 400; JSON encode errors → logged (500 not explicitly returned; Go's `json.NewEncoder` rarely fails on a valid response).
- **Why designed this way:** Keeps HTTP concerns in the handler, storage concerns in the registry. Secret is returned once so the agent can authenticate later.
- **Who calls it:** The HTTP mux routes `POST /v1/workers/register` to this method.
- **What calls it next:** Nothing — terminal handler operation.

### `func (r *Registry) Register(id, hostname, os, arch string, cpuModel int, ramTotalBytes uint64, secretHash [32]byte)`

- **Purpose:** Store a newly registered worker in the registry.
- **Inputs:** Worker ID, hostname, OS, arch, CPU model, total RAM, SHA-256 secret hash.
- **Outputs:** None.
- **Step-by-step:**
  1. Acquire `r.mu.Lock()` (write lock).
  2. Create `Worker` with all fields, `LastSeen = r.now()` (injected clock), `Status = StatusFor(r.now(), r.now())`.
  3. Store in `r.workers[id]`.
  4. Release lock.
- **Errors it handles:** None.
- **Why designed this way:** Separates registration storage from heartbeat upsert (`RecordHeartbeat`). Uses the injected clock for consistent time handling.
- **Who calls it:** `Server.handleRegister`.
- **What calls it next:** Nothing.

---

## 5. Algorithms and logic

| Algorithm/technique | Problem it solves | How it works | Why chosen | Alternatives | Complexity | Limitations |
|---|---|---|---|---|---|---|
| `crypto/rand.Read` | Generate unpredictable random bytes | Reads from the OS CSPRNG (`/dev/urandom` on macOS/Linux) | Cryptographic security for IDs and secrets | `math/rand` — faster but predictable; unsuitable for security | O(n) where n = bytes read | Slightly slower than math/rand; failure is fatal (panic) |
| `hex.EncodeToString` | Encode random bytes as safe string | Converts each byte to 2 hex characters | URL-safe, fixed-length output, no special characters | Base64 — more compact but has `+`, `/`, `=` chars | O(n) | Output is 2x input length |
| SHA-256 (`sha256.Sum256`) | One-way secret hashing | Computes 32-byte digest of the secret | Only hash stored; plaintext never persisted | bcrypt/scrypt — better for passwords but overkill for machine secrets | O(n) where n = secret length | Hash is irreversible — lost secret = must re-register |
| `http.MaxBytesReader` | Limit request body size | Wraps `r.Body` to reject reads beyond N bytes | Prevents memory exhaustion from large payloads | No limit — simpler but vulnerable to DoS | O(1) overhead | Returns 413 or 400 depending on Go version |
| `json.NewDecoder.Decode` | Decode JSON from request body | Reads JSON stream directly from `io.Reader` | Works with `http.Request.Body` without buffering entire body | `json.Unmarshal` — requires `[]byte`, needs full read first | O(n) where n = body size | Decoder is stateful; can't reuse after partial read |

No formal scheduling, scoring, or queueing algorithms. Straightforward procedural logic with standard cryptographic primitives.

---

## 6. Engineering decisions

| Decision | Why we chose it | Alternative | Trade-off |
|---|---|---|---|
| `Server` struct with `reg` + `log` | Groups handler dependencies; `log` ready for future logging; follows DI pattern | Standalone package-level functions | More boilerplate, but cleaner and more testable |
| `NewHandler(reg *registry.Registry)` | Registry injected, not created inside | Create registry inside `NewHandler` | Less flexible for testing; `main.go` must change to wire it |
| `crypto/rand` for ID/secret | Unpredictable identifiers prevent guessing attacks | `math/rand` | `math/rand` is predictable — unacceptable for security |
| Secret returned once in response | Like real token systems; agent needs secret to authenticate | Store plain text, email it, etc. | If the response is lost, the secret is unrecoverable (must re-register) |
| Store SHA-256 hash, not plain secret | If registry is dumped/inspected, secrets aren't exposed | Store plain text | Secret unrecoverable if response lost |
| `http.MaxBytesReader` 1 MB | Prevents memory exhaustion DoS | No body limit | DoS risk from huge payloads |
| `cpuModel = 0` in Register | `specs.Specs.CPU` is a string; `Worker.CPUModel` is int | Parse CPU string for a number | Unnecessary complexity; CPU model string isn't needed yet |
| Join token decoded but not validated | Step 1.10 will add token enforcement | Validate token now | Premature — token infrastructure doesn't exist yet |
| Panic on `crypto/rand.Read` failure | Broken RNG is a fatal condition; no graceful fallback | Return error and fail registration | Panic is appropriate for CSPRNG failure; registration can't be secure without randomness |

---

## 7. Go concepts and patterns learned

### Concepts used

**`crypto/rand` vs `math/rand`** — `crypto/rand` reads from the operating system's cryptographically secure pseudorandom number generator (CSPRNG). `math/rand` is a deterministic PRNG suitable for simulations but not security. We use `crypto/rand` because worker IDs and secrets must be unguessable.

**`hex.EncodeToString`** — Encodes raw bytes as hexadecimal characters (0-9, a-f). Each byte becomes 2 hex chars. Used to turn random binary data into safe string identifiers.

**`http.MaxBytesReader`** — Wraps an `http.Request.Body` to limit how many bytes can be read. If the client sends more than the limit, the handler gets an error. Used here to cap registration bodies at 1 MB.

**`json.NewDecoder.Decode` vs `json.Unmarshal`** — `json.Decoder` reads JSON directly from an `io.Reader` (like `http.Request.Body`) without buffering the whole body into memory first. `json.Unmarshal` requires a `[]byte`, so you'd need to read the entire body into memory before decoding. We use `Decoder` because it streams from the request body.

**Methods on a struct** — `handleRegister` has a `*Server` receiver, giving it access to `s.reg` and `s.log`. This is Go's version of dependency injection: the server's dependencies are provided at construction time.

**Functions as values / DI** — `NewHandler(reg *registry.Registry)` accepts the registry as a parameter rather than creating it internally. Same pattern as `server.NewHandler(collect func() (specs.Specs, error))` in the agent.

**JSON tags** — `json:"worker_id"` controls the wire format. We fixed typos (`joson`, `josn`) that would have silently dropped or renamed fields. Tags are not checked at compile time.

**`slog.Logger`** — Structured logging. The `Server` holds a `*slog.Logger` for future logging needs. Currently used only for encode errors.

### Design patterns present

| Pattern | Where it appears | Problem it solves | Why useful | Simpler alternative |
|---|---|---|---|---|
| Dependency Injection | `NewHandler(reg *registry.Registry)`, `Server{reg, log}` | Production and test wiring differ only in what's passed in | Registry can be mocked/faked in tests | Package-level globals — simpler but hides dependencies |
| Copy-on-read / Read model | `registry.View` (from Step 1.2) | What you return is not what you store — secret hash omitted | Prevents secret leakage | Return `*Worker` directly — cheaper but leaks internals |

---

## 8. Testing explained

### Tests created/modified

**`internal/coordinator/server_test.go`** — modified, 6 test functions:

| Test | What it proves | Why important | Edge cases |
|---|---|---|---|
| `TestHeartbeatHandler` | Existing heartbeat behavior unchanged | Regression check — Step 1.3 must not break Step 1.2's endpoint | Valid body, ram_available > total, empty worker_id, invalid JSON |
| `TestNewWorkerID` | ID starts with `"wrk_"`, is 16 chars, unique, valid hex | Identity generation must be correct and unique | Two calls differ; suffix is valid hex |
| `TestNewSecret` | Secret is 64 hex chars, unique, valid hex | Secret must be long enough and unique | Two calls differ; valid hex |
| `TestHandleRegister` | Valid → 201 with `"wrk_"` prefix; bad JSON → 400; empty body → 400; wrong method → 405 | Core endpoint behavior | Table-driven: valid, invalid JSON, empty body, wrong method |
| `TestHandleRegisterCreatesWorkerInRegistry` | After 201, `reg.Get(id)` finds worker; `CheckSecret(id, secret)` is true; `CheckSecret(id, wrong)` is false; specs fields copied correctly | Integration test — verifies end-to-end registration | Hostname, OS, arch, RAM all verified; secret verification both ways |
| `TestHandleRegisterBodyTooLarge` | Body > 1 MB → 400 or 413 | DoS protection works | 1 MB + 1 byte body |

**`internal/coordinator/registry/registry_test.go`** — modified, 1 new test:

| Test | What it proves | Why important |
|---|---|---|
| `TestRegister` | `Register` stores worker with correct fields; `Get` returns them; `CheckSecret` works; `LastSeen` uses injected clock | Unit test for the new `Register` method |

### Tests actually executed

All tests executed with `go test -race ./...` — all packages pass:
- `internal/coordinator` — 6 tests PASS (including new register tests)
- `internal/coordinator/registry` — 10 tests PASS (including new `TestRegister`)
- `internal/api` — PASS
- `internal/server` — PASS

### Commands executed
- `go vet ./...` — clean (exit 0)
- `go test -race ./...` — all packages PASS (exit 0)
- Manual end-to-end test — 201 response received, 400 for invalid JSON

No tests were merely written without running — all were executed and passed.

---

## 9. Execution walkthrough

**Scenario: A new agent registers with the coordinator.**

1. **Agent collects specs:** `curl http://127.0.0.1:8080/specs` returns `{"hostname":"Nebas-MacBook-Pro.local","os":"darwin","arch":"amd64","cpu":"Intel(R) Core(TM) i5-7360U CPU @ 2.30GHz","cores":4,"total_ram":8589934592}`.

2. **Agent sends register request:**
   ```http
   POST /v1/workers/register HTTP/1.1
   Content-Type: application/json
   
   {"join_token":"dev","specs":{"hostname":"Nebas-MacBook-Pro.local","os":"darwin","arch":"amd64","cpu":"...","cores":4,"total_ram":8589934592}}
   ```

3. **`handleRegister` executes:**
   - Body limited to 1 MB (request is well under).
   - JSON decoded into `api.RegisterRequest` — `join_token="dev"`, `specs` populated.
   - `newWorkerID()` → `wrk_479f1507af40` (6 random bytes hex).
   - `newSecret()` → `cf01f6797e07...87e43` (32 random bytes hex).
   - `registry.HashSecret(secret)` → SHA-256 digest `[32]byte`.
   - `reg.Register("wrk_479f1507af40", "Nebas-MacBook-Pro.local", "darwin", "amd64", 0, 8589934592, hash)` stores the worker.
   - Response: `{"worker_id":"wrk_479f1507af40","secret":"cf01f679...","heartbeat_seconds":5}` with HTTP 201.

4. **Agent receives 201** and saves the `worker_id` and `secret`. The secret was in the response body only — it is not in the registry, not in logs, not anywhere else.

**Failure scenario: Invalid JSON.**
```http
POST /v1/workers/register HTTP/1.1
Content-Type: application/json

not json
```
- `json.NewDecoder.Decode` returns an error.
- Handler returns 400 with `invalid JSON: invalid character 'o' in literal null (expecting 'u')`.
- No worker is created. No secret is generated.

**Failure scenario: Body too large (>1 MB).**
- `http.MaxBytesReader` rejects the body.
- Handler returns 400 or 413.
- No worker is created.

---

## 10. Important lessons

- **`crypto/rand` for security-critical randomness.** Never use `math/rand` for IDs, secrets, or tokens. `crypto/rand` reads from the OS CSPRNG.
- **Return the secret once, store the hash.** The plain secret goes in the HTTP response body and nowhere else. The registry stores only `sha256(secret)`. This is how real token systems work.
- **JSON tags are not compile-time checked.** `joson:"specs"` silently broke JSON marshaling for `RegisterRequest`. Always verify tags match the intended wire format.
- **`json.Decoder` streams from `io.Reader`.** It's the right tool for decoding HTTP request bodies without buffering.
- **`http.MaxBytesReader` is cheap DoS protection.** One line wraps the body and prevents memory exhaustion from huge payloads.
- **Dependency injection makes testing easy.** `NewHandler(reg)` lets tests inject a fake registry. `Server{reg, log}` groups dependencies cleanly.

What I should be able to explain to another developer:
- Why `crypto/rand` is used instead of `math/rand`
- Why the secret is returned in the response but only the hash is stored
- Why `NewHandler` now takes a `*registry.Registry` parameter
- What `http.MaxBytesReader` protects against
- Why the join token is accepted but not validated yet

---

## 11. Questions to test my understanding

1. Why does `handleRegister` use `crypto/rand.Read` instead of `math/rand.Int64()` for generating worker IDs and secrets? What would go wrong with `math/rand`?

2. The register endpoint returns the plain secret in the HTTP 201 response but stores only `sha256(secret)` in the registry. What happens if the agent loses the response? What happens if an attacker gains access to the registry's memory?

3. Why does `NewHandler` now accept a `*registry.Registry` parameter instead of creating one internally? How does this affect testing?

4. What does `http.MaxBytesReader(w, r.Body, 1<<20)` protect against? What status code does the client receive when the body exceeds 1 MB?

5. The join token is decoded from the request but not validated. The spec says this is intentional (Step 1.10 will add enforcement). What security risk does this create in the meantime, and why is it acceptable for Phase 0?

*(Answers not provided — ask me to quiz you when ready.)*

---

## 12. Future considerations

| Category | Details |
|---|---|
| **Necessary now** | None — Step 1.3 is complete and verified. The register endpoint works standalone. |
| **Potential improvements later** | Wire secret verification into the heartbeat handler (Step 1.4). Add `flag.Parse()` validation (already added). Validate the join token (Step 1.10). Add `GET /v1/workers` listing endpoint. |
| **Unnecessary complexity at this stage** | Persistence (database) — in-memory is correct for Phase 0. Emailing/separately delivering the secret — returning it in the response is simpler. Token expiration and max_uses — Step 1.10. |

---

**Documentation status:** Complete for Step 1.3. Task document created at `docs/with-tasks-explained/task-002-register-endpoint.md`. Index updated in `docs/with-tasks-explained/README.md`.