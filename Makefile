.PHONY: build test vet check run smoke

build:
	go build ./cmd/server

test:
	go test ./...

vet:
	go vet ./...

check: test vet

run:
	go run ./cmd/server

smoke:
	go run ./cmd/smoke -endpoint "$${MCP_ENDPOINT:-http://127.0.0.1:8090/mcp}" -token "$${MCP_TOKEN:-}"
