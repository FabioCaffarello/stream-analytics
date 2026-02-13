---
type: doc
name: project-overview
description: High-level overview of the project, its purpose, and key components
category: overview
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Project Overview

Market Raccoon is a market intelligence backend that ingests market data, normalizes and sequences events, builds read models, and serves runtime/delivery capabilities through an actor-based architecture. It is designed for deterministic processing, replayability, and low-latency delivery rather than trade execution.

## Codebase Reference
> **Detailed Analysis**: For generated repository structure metadata, see [`codebase-map.json`](./codebase-map.json).

## Quick Facts
- Root: `/Volumes/OWC Express 1M2/Develop/market-raccoon`
- Primary language: Go (workspace with multiple modules in `go.work`)
- Main entrypoints: `cmd/consumer`, `cmd/processor`, `cmd/server`
- Build orchestration: `Makefile`
- Full generated snapshot: [`codebase-map.json`](./codebase-map.json)

## Context Truth Navigation
- Context bridge index: [`truth-pack.md`](./truth-pack.md)
- Canonical authority map: [`docs/architecture/TRUTH-MAP.md`](../../docs/architecture/TRUTH-MAP.md)
- Execution/program baseline: [`docs/rfcs/EXECUTION-SEQUENCE.md`](../../docs/rfcs/EXECUTION-SEQUENCE.md), [`docs/prd/PRD-0001-extreme-runtime.md`](../../docs/prd/PRD-0001-extreme-runtime.md)

## Entry Points
- [`cmd/consumer/main.go`](../../cmd/consumer/main.go#L1) - Market data ingestion runtime (supports fake feed mode for development).
- [`cmd/processor/main.go`](../../cmd/processor/main.go#L1) - Envelope processing and aggregation pipeline wiring.
- [`cmd/server/main.go`](../../cmd/server/main.go#L1) - Runtime supervision and HTTP endpoints (`/healthz`, `/runtime/snapshot`, `/runtime/reload`).

## Key Exports
Primary exported behavior is organized by bounded context modules under `internal/core/*`, plus actor runtime orchestration under `internal/actors/*`. See [`codebase-map.json`](./codebase-map.json) and package-level docs for detailed symbol inventory.

## File Structure & Code Organization
- `cmd/` - Process-level composition and dependency wiring for binaries.
- `internal/core/marketdata` - Ingestion use cases and domain rules for market data.
- `internal/core/aggregation` - Order book aggregation use cases and domain events.
- `internal/core/delivery` - Delivery/session domain logic.
- `internal/core/insights` - Insight domain model.
- `internal/actors/` - Actor subsystem runtime and supervision boundaries.
- `internal/adapters/` - Infrastructure adapter implementations.
- `internal/interfaces/` - HTTP interface and boundary adapters.
- `internal/shared/` - Reusable value objects and utilities used cross-context.
- `docs/` - Architecture notes, ADRs, and domain contracts.

## Technology Stack Summary
The project uses Go workspace modules (`go.work`) with actor runtime patterns (`github.com/anthdm/hollywood/actor`), Make-driven engineering workflows, and GitHub Actions CI. Docker and Docker Compose are available for containerized workflows. Quality controls include `gofmt`, `goimports`, `golangci-lint`, race-enabled tests, and optional/required `govulncheck` depending on environment.

## Development Tools Overview
Key workflow entrypoints:
- `make` targets for formatting, lint, test, vulnerability scan, and build.
- `scripts/*` helpers for module-aware workspace operations.
- pre-commit hooks for fast local guardrails (`tidy-check`, `fmt-check`, `lint`, `test-short`, commit message validation).

## Getting Started Checklist
1. Install Go toolchain compatible with workspace and install tools with `make install-tools`.
2. Run `make modules` to inspect workspace module boundaries.
3. Execute `make test-short` to validate baseline environment quickly.
4. Execute `make ci` to run the same quality gates expected in CI.
5. Run a binary locally, for example `make run APP_CMD=./cmd/server`.
6. Read [`development-workflow.md`](./development-workflow.md) and [`testing-strategy.md`](./testing-strategy.md) before opening PRs.

## Next Steps
For design decisions and future direction, continue with:
- `../../docs/architecture/README.md`
- `../../docs/architecture/moat.md`
- `../../docs/adrs/`
