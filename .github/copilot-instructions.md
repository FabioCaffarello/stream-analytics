# Project Rules and Guidelines

> Source of truth: `.context/docs/` — this file is a curated summary, not auto-generated.

## Hard Constraints

- **`./zip/` is READ-ONLY reference.** Never create, edit, or delete files under `zip/`. It contains MarketMonkey source as architectural reference only. Read for analysis; write nothing.
- **Errors are `*problem.Problem`**, never plain `error` in domain/app layers.
- **Results are `result.Result[T]`** for use case returns.
- **`replace` directives are required** in every go.mod, even with go.work.
- **No implementation outside bounded context boundaries** — domain logic lives in `internal/core/*/domain`, orchestration in `internal/core/*/app`.

## Repository Snapshot

- `cmd/` — Binary entrypoints (`consumer`, `processor`, `server`, `store`, `backfill`).
- `internal/core/` — Domain and application use cases by bounded context.
- `internal/actors/` — Actor runtime and subsystem orchestration.
- `internal/adapters/` — Infrastructure adapters (bus, exchange parsers, storage drivers).
- `internal/interfaces/` — HTTP and WebSocket boundary interfaces.
- `internal/shared/` — Shared primitives (`problem`, `result`, `envelope`, `codec`, `config`, `naming`, `ids`).
- `proto/` — Protobuf definitions (envelope, marketdata, aggregation, insights).
- `scripts/` — Workspace utility scripts used by Make targets.
- `docs/` — ADRs, RFCs, PRDs, architecture, contracts.
- `sql/` — DDL migrations (TimescaleDB + ClickHouse).

## Core Guides

- [Start Here](.context/docs/00-START-HERE.md)
- [Project Overview](.context/docs/project-overview.md)
- [Development Workflow](.context/docs/development-workflow.md)
- [Testing Strategy](.context/docs/testing-strategy.md)
- [Tooling](.context/docs/tooling.md)

## Architecture Sources

- [Architecture Overview](docs/architecture/README.md)
- [System Invariants](docs/architecture/system-invariants.md)
- [Event Bus Contract](docs/contracts/event-bus.md)
- [TRUTH-MAP](docs/architecture/TRUTH-MAP.md)
- [ADRs](docs/adrs/) (19 decisions, ADR-0000 to ADR-0018)
- [RFCs](docs/rfcs/) (12 proposals, RFC-0001 to RFC-0011)
- [PRDs](docs/prds/)

## Context Engineering

- **Skills** (16 total): `.context/skills/` — 10 built-in + 6 custom (pareto-analysis, swot-analysis, write-prd, write-adr, write-rfc, milestone-plan)
- **Agents** (15 total): `.context/agents/` — 14 built-in + 1 custom (strategic-planner)
- **Codebase Map**: `.context/docs/codebase-map.json` — Go-first semantic map, 429 files, 13 modules, 4 BCs
- **Workflow**: PREVC (Plan → Review → Execute → Validate → Complete)

## Makefile Quick Reference

- `make test` — all modules
- `make test MODULE=./internal/shared` — single module
- `make fmt` — format all
- `make lint` — golangci-lint
- `make ci` — full pipeline
- `make tidy` — go mod tidy all modules
- `make up-infra` — docker-compose (NATS, TimescaleDB, ClickHouse)
