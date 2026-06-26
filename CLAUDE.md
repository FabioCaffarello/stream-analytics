# CLAUDE.md — Stream Analytics

> This file is the authoritative guide for Claude Code working in this repository.
> Read it in full before taking any action. When in doubt, the canonical docs govern.

---

## What This Repository Is

**Stream Analytics** is a real-time, multi-exchange cryptocurrency market data platform with an
integrated operational cockpit (~161K LOC: ~131K Go + ~30K Odin).

- **Backend (Go):** Actor-supervised pipeline — Consumer → NATS JetStream → Processor → Server/Store.
  7 active binaries, Hollywood actor model, TimescaleDB (hot) + ClickHouse (cold), strict
  hexagonal architecture enforced by automated guards. Best-effort analytics path:
  Consumer → Kafka → Flink SQL → TimescaleDB analytics schema → Metabase.
- **Client (Odin):** Cross-platform cockpit (WASM + native). 13 widget types, 8 indicators,
  5-layer stream health pipeline, workspace split-tree.

This is **decision infrastructure, not a trading platform**. The execution pipeline was retired in S9.

---

## Critical Rules — Read Before Any Edit

### 1. Layer Isolation Is Automated and Enforced

The dependency graph is strictly one-directional:

```
cmd/* → interfaces/ → actors/ → adapters/ → core/* → shared/
```

**Never import a layer above you.** This is checked by `make invariants-check` (11 automated guards).
If your edit would violate a layer boundary, do NOT bypass the guard — fix the architecture instead.

Key rules (from `docs/architecture/system-invariants.md`):
- `core/*` must NEVER import `actors/`, `adapters/`, or `interfaces/`
- `interfaces/` must NEVER import `adapters/` directly
- `actors/` must NEVER import `interfaces/`
- Time: always use `clock.Clock`, NEVER `time.Now()` directly
- Hot-path string formatting: use `FieldHasher`, NEVER `fmt.Sprintf`

### 2. Bounded Context Boundaries

12 bounded contexts own their domain types. Cross-context communication uses **versioned event
contracts only** — never share types directly across context boundaries.

Bounded contexts: `marketdata`, `aggregation`, `delivery`, `insights`, `evidence`, `storage`,
`marketmodel`, `workspace` (+ `application`, `contracts`, `interfaces` cross-cutting).

### 3. Documentation Is a First-Class Citizen

Every non-trivial change requires updating the relevant canonical doc:
- `docs/architecture/TRUTH-MAP.md` — if you change a code anchor
- `docs/architecture/subsystems.md` — if you change a subsystem's boundary, I/O, or bounds
- `docs/contracts/*.md` — if you change an event subject, payload, or ACK semantics

`make docs-check` validates doc headers and internal links. Never leave docs stale.

### 4. No Direct `time.Now()` Calls

All time-dependent code must use `internal/shared/clock.Clock`. This enables deterministic replay
and test control. The guard `check-domain-isolation.sh` bans direct `time.Now()` in core and actors.

### 5. Envelope Sequencing Invariants

Every NATS envelope carries `seq` + `prev_seq`. Receivers detect gaps via `prev_seq != last_seq + 1`.
Never publish an envelope without correctly threading the sequence chain.

---

## Module Structure

This is a **Go workspace** (`go.work`) with 26 modules:

```
go.work
├── cmd/consumer, processor, server, store, migrate, emulator, validator
├── internal/
│   ├── shared/              ← foundation, zero internal imports
│   ├── core/
│   │   ├── marketdata, aggregation, delivery, insights, evidence
│   │   ├── marketmodel, workspace
│   │   └── (aggregation owns storage ports)
│   ├── adapters/            ← exchange connectors, storage, JetStream
│   ├── actors/              ← Hollywood actor subsystems + Guardian runtime
│   ├── interfaces/          ← HTTP/WS handlers
│   ├── application/         ← cross-cutting app wiring
│   └── contracts/           ← versioned event contracts + proto
└── client/                  ← Odin UI (separate build system)
```

Each module has its own `go.mod`. Always run `make tidy` after adding a dependency.

---

## Essential Make Targets

```bash
# Build
make build                    # All binaries
make build-consumer           # Single binary

# Test
make test                     # Full test suite (all modules)
make test-short-changed       # Fast subset of changed modules
make test-workspace-race      # Race condition detection
make soak-check               # 8 soak harnesses (takes ~5 min)

# Quality Gates
make invariants-check         # Layer isolation guards (MUST pass before PR)
make docs-check               # Documentation header + link validation
make lint-changed             # golangci-lint v2.6.0 on changed files
make legacy-check             # Banned strings scan

# Local Stack
make up                       # Full infra + services via Docker Compose
make up-infra                 # Only NATS + TimescaleDB + ClickHouse + observability
make up-analytics             # Full stack + Flink + Metabase (analytics profile)
make ps                       # Service health status
make logs service=consumer    # Tail logs for a specific service

# Database
make migrate                  # Apply schema migrations

# CI equivalent (run before pushing)
make ci                       # Fast: invariants + lint + docs + unit tests
make ci-full                  # Full: + soak tests + race detection
```

---

## Pre-PR Checklist

Before opening a PR, all of these must pass:

```bash
make invariants-check    # Layer isolation (automated, non-negotiable)
make docs-check          # Doc headers and links
make test-workspace      # Full module test suite
make legacy-check        # Banned string scan
make lint-changed        # Lint
```

If changing event contracts:
```bash
make proto-check         # Protobuf breaking-change detection
make contract-gates      # Subject registry consistency
```

---

## Where to Find Things

| I need to understand... | Go to |
|------------------------|-------|
| Overall architecture | `docs/architecture/README.md` |
| Visual diagrams (C4, sequence) | `docs/architecture/diagrams/` |
| Subsystem boundaries and I/O | `docs/architecture/subsystems.md` |
| Layer invariants | `docs/architecture/system-invariants.md` |
| Event subjects and envelope format | `docs/contracts/event-bus.md` |
| WebSocket protocol (Terminal_V1) | `docs/contracts/delivery-ws.md` |
| Single source of truth per topic | `docs/architecture/TRUTH-MAP.md` |
| Document authority tiers | `docs/architecture/AUTHORITY-MAP.md` |
| Running locally | `docs/local-dev.md` |
| Test strategy | `docs/testing-strategy.md` |
| Exchange adapters | `internal/adapters/exchange/{name}/` |
| Analytics pipeline (Kafka→Flink→Metabase) | `docs/architecture/analytics-pipeline.md` |
| Metrics catalogue (Prometheus) | `docs/architecture/metrics-catalogue.md` |
| Actor runtime / Guardian | `internal/actors/runtime/guardian.go` |
| Storage federation | `internal/adapters/storage/federation/` |
| Canonical envelope type | `internal/shared/envelope/` |
| Event contracts / protobuf | `internal/contracts/` + `proto/` |
| Client architecture | `docs/client/client-architecture.md` |

---

## Architecture Diagrams Quick Reference

All diagrams are in `docs/architecture/diagrams/`:

| Diagram | What it shows |
|---------|---------------|
| `c4-context.md` | System context — exchanges, storage, operator |
| `c4-containers.md` | 7 service binaries and data stores |
| `c4-analytics.md` | Analytics profile — Kafka, Flink, TimescaleDB analytics schema, Metabase |
| `actor-supervision-tree.md` | Hollywood Guardian actor trees per binary |
| `sequence-live-ingestion.md` | **Full data pipeline**: Exchange → Consumer → NATS → Processor → Delivery + Store → Client |
| `sequence-analytics-pipeline.md` | Best-effort analytics: Consumer → Kafka → Flink → TimescaleDB → Metabase |
| `sequence-client-session.md` | WebSocket Terminal_V1 protocol: Hello, Subscribe, Backfill, Resync |
| `sequence-storage-federation.md` | L0/L1/L2 write fan-out and federated range read |
| `sequence-evidence-lel.md` | LEL evidence detection + shard ownership |
| `sequence-exchange-recovery.md` | Exchange disconnect → backoff → reconnect → gap fill |

---

## Tech Stack at a Glance

| Layer | Technology |
|-------|-----------|
| Language (backend) | Go 1.25.6, multi-module workspace |
| Language (client) | Odin (WASM + native via GLFW/SDL2) |
| Actor framework | Hollywood v1.0.5 |
| Message bus | NATS JetStream 2.10.18 |
| Analytics bus | Kafka (Redpanda v24.2.13) |
| Analytics engine | Apache Flink SQL 1.19 |
| BI dashboards | Metabase v0.52.2 (analytics profile) |
| Hot storage | TimescaleDB 2.25.1 (PG16) |
| Cold storage | ClickHouse 24.8.8 |
| Observability | Prometheus + Grafana (5 dashboards, 100+ metrics) |
| Serialization | Protocol Buffers (proto3) + JSON |
| Test tooling | testcontainers, soak harnesses, deterministic replay |
| CI | GitHub Actions: ci-fast / ci-full / ci-nightly |

---

## Common Pitfalls

1. **Adding `time.Now()` in core/actors** — use `clock.Clock` instead. Breaks deterministic replay.
2. **Sharing domain types across bounded contexts** — use versioned event contracts in `internal/contracts/`.
3. **Importing `adapters/` from `interfaces/`** — violates INV-LAY-03. Wire through actors or ports.
4. **Publishing an envelope without threading `prev_seq`** — breaks gap detection on client.
5. **Adding a new event subject without updating `docs/contracts/event-bus.md`** — fails `docs-check`.
6. **Running `go test ./...` from root** — this is a workspace; use `make test` instead.
7. **Changing a subsystem's resource cap without updating `docs/contracts/boundedness-matrix.md`** — stale docs block PR.
8. **Editing Odin client with Go assumptions** — the client has a separate strict DAG: `ports → services → layers → app`. No Go tooling applies there.

---

## Exchanges Supported

| Exchange | Market type | Adapter path |
|----------|-------------|--------------|
| Binance Spot | SPOT | `internal/adapters/exchange/binance/` |
| Binance Futures | USD_M_FUTURES | `internal/adapters/exchange/binancef/` |
| Bybit | USD_M_FUTURES | `internal/adapters/exchange/bybit/` |
| Coinbase | SPOT | `internal/adapters/exchange/coinbase/` |
| HyperLiquid | USD_M_FUTURES | `internal/adapters/exchange/hyperliquid/` |
| Kraken Spot | SPOT | `internal/adapters/exchange/kraken/` |
| Kraken Futures | USD_M_FUTURES | `internal/adapters/exchange/krakenf/` |

Adding a new exchange: implement the `exchange.Adapter` port in `internal/adapters/exchange/{name}/`,
register in `cmd/consumer/exchanges.go`, add to `docs/architecture/subsystems.md`.

---

## Performance Baseline (C4 Soak)

```
10M events, 4 exchanges, ~85s
Throughput: 117,697 evt/sec
Latency:    p50=7µs  p95=13µs  p99=56µs
```

Do not merge changes that regress soak throughput by >5% without explicit discussion.
Soak results are in `artifacts/` and validated by `make soak-check`.
