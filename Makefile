
.PHONY: build test vet run-agent run-coordinator
# this will make the names represent tasks,(not actual files)
build:
	go build -o bin/ ./cmd/...

test:
	go test -race ./...

vet:
	go vet ./...

run-agent:
	go run ./cmd/agent

run-coordinator:
	go run ./cmd/coordinator
