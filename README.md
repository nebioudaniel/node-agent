# node-agent

Part of a team compute platform. Two small Go programs talk to each other over HTTP:

- **agent**: runs on a machine, collects its hardware specs, and serves them over HTTP.
- **coordinator**: the central service. Workers send it heartbeats (CPU, RAM, active tasks) and it validates them.

Status: early development (Phase 0).

## Requirements

- Go 1.26 or newer (the exact version is in `go.mod`)
- `make` (optional, but the commands below use it)

## Run the agent

```
make run-agent
```

By default the agent listens on `127.0.0.1:8080`, so only your own machine can reach it.

```
curl http://localhost:8080/health
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-listen` | `127.0.0.1:8080` | Address to listen on (`host:port`) |
| `-name` | (empty) | Override the reported hostname |

To expose the agent to your network, opt in explicitly:

```
go run ./cmd/agent -listen 0.0.0.0:8080
```

The agent serves machine specs, so only do this on a network you trust.

## Run the coordinator

```
make run-coordinator
```

It listens on `:9090` by default (flag `-addr`). Send it a heartbeat:

```
curl -i -X POST localhost:9090/v1/workers/wrk_test/heartbeat \
  -H 'Content-Type: application/json' \
  -d '{"worker_id":"wrk_test","seq":1,"agent_version":"0.1.0","cpu_percent":10,"ram_total_bytes":8000000000,"ram_available_bytes":4000000000,"active_tasks":0,"loaded_models":[]}'
```

A valid heartbeat returns `200`. An invalid one (for example `ram_available_bytes` larger than `ram_total_bytes`) returns `400` with a readable error.

## Development

```
make build   # compile both programs into bin/
make test    # go test -race ./...
make vet     # go vet ./...
```

CI (GitHub Actions) runs `go vet` and `go test -race` on every push and pull request.

Logs use `log/slog` (text format, written to stderr).

## Folder layout

```
cmd/
  agent/          agent entry point (flags, server, graceful shutdown)
  coordinator/    coordinator entry point (flags, server, graceful shutdown)
internal/
  api/            JSON types shared by agent and coordinator (Heartbeat, etc.)
  coordinator/    coordinator HTTP handler and tests
  server/         agent HTTP handler (/health, /specs)
  specs/          machine spec collection
.github/workflows/ci.yml   CI pipeline
Makefile          build, test, vet, run-agent, run-coordinator
```