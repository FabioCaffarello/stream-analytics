---
type: doc
name: tooling
description: Developer tools, make targets, and local environment setup
category: tools
generated: 2026-03-05
status: filled
scaffoldVersion: "2.0.0"
---

# Tooling & Operations

Market Raccoon leverages strict tooling for invariant guarantees.

## Prerequisite Binaries (macOS / Linux)
- `go 1.23+`
- `golangci-lint` (via `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh`)
- `govulncheck` (via `go install golang.org/x/vuln/cmd/govulncheck@latest`)
- `protoc` (via `brew install protobuf`)
- `protoc-gen-go` (via `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`)
- Docker and Docker-Compose (OrbStack recommended for macOS testing performance)

## Core Targets (`make help`)
```bash
make install-tools        # Installs Go toolchains
make tidy-check-changed   # go mod tidy constraint checking
make fmt-check            # Enforces deterministic gofmt standards
make lint-changed         # Runs golangci-lint on delta lines
make test-workspace-race  # Full tests with race detector enabled (vital for Actors)
```

## Go Workspace (`go.work`)
The system separates `client/` (WASM/Odin/JS) and `.` (Go Backend Modules) physically or conceptually.
Dependencies inside the `make` file traverse `go.work` paths to strictly test isolated bounded contexts. Do not bypass `make` targets; manual `go build ./...` outside workspace configurations will drift from the CI.

## Deployment Simulation
`make local-dev` stands up PostgreSQL (TimescaleDB), ClickHouse, NATS JetStream, and the Guardian orchestration stack for full cold-path runbook testing. See `docs/operations/local-dev.md`.
