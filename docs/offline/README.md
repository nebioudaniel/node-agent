# Offline Design Packet — Steps 1.6, 1.7, 1.8

This directory contains design documents and skeleton (TODO) files for
implementing Steps 1.6, 1.7, and 1.8 while offline. Each design doc has
9 sections: goal, Go concepts, exact spec, algorithm, tests, commands,
common mistakes, self-check questions, and a done checklist.

## Order of work

1. **Step 1.7 — Backoff** (already scaffolded; implement `Backoff`)
   - `internal/agent/backoff.go` — skeleton with TODO
   - `internal/agent/backoff_test.go` — tests (will fail until implemented)
   - `docs/offline/step-1.7-backoff.md` — design doc

2. **Step 1.6 — Identity** (implement `LoadIdentity`, `SaveIdentity`, `DeleteIdentity`)
   - `internal/agent/identity.go` — skeleton with TODO
   - `internal/agent/identity_test.go` — tests (will fail until implemented)
   - `docs/offline/step-1.6-identity.md` — design doc

3. **Step 1.8 — Agent client** (implement `NewClient`, `Register`, `sendHeartbeat`, `Run`)
   - `internal/agent/client.go` — skeleton with TODO
   - `internal/agent/client_test.go` — tests (will fail until 1.6+1.7 done)
   - `docs/offline/step-1.8-client.md` — design doc

## Commands to run after each step

```bash
cd ~/Desktop/node-agent
go vet ./...
go test -race ./internal/agent -run TestBackoff -v      # step 1.7
go test -race ./internal/agent -run TestIdentity -v     # step 1.6
go test -race ./internal/agent -run TestClient -v       # step 1.8
go test -race ./...                                     # full suite
```

## Expected output when done

All `TestBackoff*`, `TestIdentity*`, `TestClient*` PASS. `go vet ./...` clean.
`go test -race ./...` all packages ok, no FAIL. (Tests for 1.8 will fail
until 1.6 and 1.7 are implemented.)

## Checklist

- [ ] Implement `Backoff` (step 1.7) — all TestBackoff* pass
- [ ] Implement `LoadIdentity` / `SaveIdentity` / `DeleteIdentity` (step 1.6) — all TestIdentity* pass
- [ ] Implement `NewClient` / `Register` / `sendHeartbeat` / `Run` (step 1.8) — all TestClient* pass
- [ ] `go vet ./...` clean
- [ ] `go test -race ./...` all packages PASS
- [ ] No tight loop on 400
- [ ] Cancel returns within 100ms
- [ ] Identity file mode 0600, dir mode 0700
- [ ] Atomic write (temp + rename), no leftover .tmp