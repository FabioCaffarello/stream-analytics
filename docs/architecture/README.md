# Architecture Overview

**Status:** Active | **Last updated:** 2026-06-25

---

## What Stream Analytics Is

Stream Analytics is a real-time, multi-exchange cryptocurrency market data platform with an integrated operational cockpit. It ingests, normalizes, aggregates, and visualizes live market data across 6 exchanges with sub-millisecond latency.

The system has two halves:

- **Backend (Go, ~131K LOC):** Actor-supervised pipeline that consumes exchange WebSocket feeds, normalizes events into canonical envelopes, builds aggregated read models, and delivers them over WebSocket and HTTP. 7 active service binaries, NATS JetStream event bus, TimescaleDB + ClickHouse storage. A parallel best-effort analytics path (Kafka → Flink SQL → TimescaleDB analytics schema → Metabase) provides BI dashboards without touching the primary NATS path.

- **Client (Odin, ~30K LOC):** Cross-platform operational cockpit (WASM + native). 13 widget types, 8 indicators, 3 subplot analytics, orderflow visualization, workspace split-tree with compare mode, and a 5-layer stream health pipeline with operator-visible reliability signals.

This is decision infrastructure, not a trading platform. Venue execution exists but defaults to simulation behind a 5-gate governance boundary.

---

## Architectural Principles

### 1. Clean Architecture — Dependency Inversion

Dependencies point inward. Domain logic (`internal/core`) defines ports (interfaces); infrastructure (`internal/adapters`) implements them. No core module imports actors, adapters, or transport handlers.

### 2. DDD Bounded Contexts

12 bounded contexts with explicit ownership boundaries. Each context owns its domain types, use cases, and port definitions. Cross-context communication uses versioned event contracts — no direct type sharing.

### 3. Hexagonal Ports & Adapters

Each bounded context exposes primary ports (use cases) and secondary ports (infrastructure contracts). Adapters implement secondary ports. Core knows exchange parsers, storage drivers, and message brokers only as interfaces.

### 4. Event-Driven Architecture

All state transitions originate from versioned envelopes on NATS JetStream. Events are immutable, sequenced, and replay-safe. Idempotency keys and monotonic sequencing prevent duplicate processing.

### 5. Actor Model

Runtime orchestration uses Hollywood actors. A Guardian supervision tree manages 10 subsystem actors with exponential backoff, restart limits, and circuit-breaking. Actors coordinate — use cases decide — domain enforces invariants.

### 6. SOLID

- **S:** Each bounded context has a single responsibility. Each actor manages one subsystem.
- **O:** New exchange adapters and widget types are added without modifying core domain logic.
- **L:** All port implementations are substitutable (InMemoryBus for tests, JetStream for production).
- **I:** Ports are narrow and context-specific (`EventPublisher`, `RangeStore`, `SnapshotReader`).
- **D:** Core depends on abstractions (ports); adapters depend on concrete implementations. Never reversed.

---

## Architecture Diagrams

Visual diagrams complement the text below. See [`diagrams/`](diagrams/README.md) for the full index.

| Diagram | Quick link |
|---------|-----------|
| C4 System Context | [c4-context.md](diagrams/c4-context.md) |
| C4 Container Map | [c4-containers.md](diagrams/c4-containers.md) |
| C4 Analytics Profile | [c4-analytics.md](diagrams/c4-analytics.md) |
| Actor Supervision Tree | [actor-supervision-tree.md](diagrams/actor-supervision-tree.md) |
| Sequence: Live Data Ingestion | [sequence-live-ingestion.md](diagrams/sequence-live-ingestion.md) |
| Sequence: Analytics Pipeline | [sequence-analytics-pipeline.md](diagrams/sequence-analytics-pipeline.md) |
| Sequence: Client Session Protocol | [sequence-client-session.md](diagrams/sequence-client-session.md) |
| Sequence: Storage Federation | [sequence-storage-federation.md](diagrams/sequence-storage-federation.md) |
| Sequence: Evidence / LEL | [sequence-evidence-lel.md](diagrams/sequence-evidence-lel.md) |
| Sequence: Exchange Recovery | [sequence-exchange-recovery.md](diagrams/sequence-exchange-recovery.md) |

---

## Backend Architecture

### Layer Hierarchy

Dependencies flow strictly downward. No layer imports a layer above it.

```
cmd/*            ← entry points (main.go)
  ↓
interfaces/      ← HTTP/WS handlers
  ↓
actors/          ← Hollywood actor subsystems
  ↓
adapters/        ← exchange connectors, storage, bus
  ↓
core/*           ← domain + use cases + ports
  ↓
shared/          ← foundation (zero internal imports)
```

Layer isolation is enforced by `make invariants-check`.

### Bounded Contexts

#### Data Pipeline (reactive, event-driven)

| Context | Module | Responsibility |
|---|---|---|
| **MarketData** | `core/marketdata` | Consume exchange WebSocket streams, canonicalize into CMM, dedup, sequence, publish envelopes |
| **Aggregation** | `core/aggregation` | Build read models: orderbook snapshots, candles (9 timeframes), stats, tape, heatmap, volume profiles |
| **Delivery** | `core/delivery` | Route envelopes to WS sessions. Backpressure, backfill, per-stream coherence |
| **Insights** | `core/insights` | VPVR, heatmap, TPO profiles; Liquidity Evidence Layer with 5 stateful rules |
| **Storage** | `core/storage` (via adapters) | Persist aggregated events; serve historical queries from TimescaleDB (hot) and ClickHouse (cold) |
| **MarketModel** | `core/marketmodel` | Instrument metadata and market type definitions |

#### Evidence

| Context | Module | Responsibility |
|---|---|---|
| **Evidence** | `core/evidence` | Stateful liquidity evidence detection with multi-replica ownership by stream hash |

#### Cross-Cutting

| Module | Purpose |
|---|---|
| **Workspace** | `core/workspace` — schema management for client workspace persistence |

### Shared Foundation

`internal/shared` provides 24 cross-cutting packages with zero business logic:

| Package | Purpose |
|---|---|
| `problem` | Typed errors (`*problem.Problem`) |
| `result` | Generic result type (`result.Result[T]`) |
| `validation` | Input validation framework |
| `ids` | ID generation |
| `clock` | Time abstraction (`FakeClock` in tests, `SystemClock` in prod) |
| `envelope` | Canonical event wrapper with seq, timestamps, idempotency key |
| `codec` | Serialization (JSON now, CBOR-ready) |
| `hash` | FNV-1a zero-alloc hashing (`FieldHasher` fluent API) |
| `naming` | Canonical normalization (`CanonicalVenue`, `CanonicalInstrument`) |
| `contracts` | Versioned event contracts (TradeTickV1, CandleV1, etc.) |
| `metrics` | Prometheus metric definitions |
| `observability` | Structured logging and telemetry |
| `policykit` | Actor-runtime supervision policies |
| `replay` | Deterministic event replay (offline-only, no NATS) |
| `ds` | Data structures |
| `ticksize` | Tick size tables per exchange |
| `bootstrap` | Service bootstrap helpers |
| `config` | Configuration schema |
| `proto` | Protobuf generated types |
| `ownership` | Stream ownership assignment |
| `shardregistry` | Shard allocation |
| `slo` | SLO definitions |

**Boundary rule:** If a type in `shared/` is used by only one bounded context, it belongs in that context's `domain/` package.

### Data Flow

```
Exchange WS (6 venues)
    │
    ▼
[Consumer / MarketData] ──(marketdata.*)──────────────────────────────────►
                                                                            │
[Processor / Aggregation] ◄──────────────────────────────────────────────────┘
    │
    ├──(aggregation.snapshot / candle / stats / tape)───────────────────────┐
    ├──(insights.heatmap_snapshot / volume_profile_snapshot)────────────────┤
    └──(trades+bookdelta)──► [Evidence / LEL]                               │
                                    │                                       │
                                    └──(liquidity.evidence)─────────────────┤
                                                                            │
                                                                            ▼
                                                                   [Delivery / Router]
                                                                        │        │
                                                                     [Store]  [WS Session]
                                                                                  │
                                                                            [Client]
```

### Runtime Model

#### Guardian Supervision Tree

The Guardian (`internal/actors/runtime/guardian.go`) orchestrates subsystems per binary:

```
cmd/consumer:
  Engine → Guardian
    └── MarketData  (+ dynamic per-exchange children)

cmd/processor:
  Engine → Guardian
    ├── Aggregation
    ├── Insights
    └── Evidence

cmd/server:
  Engine → Guardian
    └── Delivery
```

**Supervision policy:** BaseBackoff 250ms, MaxBackoff 5s, RestartWindow 30s, RestartLimit 5/window, Cooldown 30s. Global restart limit: 5 per minute.

**Actor protocol:**

```
Shutdown: e.Send(pid, Stop{}) → <-e.Poison(pid).Done()
Request:  engine.Request(pid, Query{}) with ReplyTo fallback to c.Sender()
```

**Key invariants:**
- Failure in one subsystem does not kill siblings (INV-TOPO-01)
- Global restart rate limit prevents restart storms
- Session actors clean-close and de-register from router
- `Msg-ID` dedup on JetStream prevents double-delivery

### Service Entrypoints

7 binaries in `cmd/`:

| Binary | Role |
|---|---|
| `consumer` | Exchange WebSocket → NATS JetStream ingester |
| `processor` | NATS → Aggregation pipeline (candles, orderbook, stats, tape, heatmaps, VPVR, evidence) |
| `server` | HTTP + WS gateway |
| `store` | Storage lifecycle manager (TimescaleDB + ClickHouse) |
| `migrate` | Database migrations (Goose) |
| `emulator` | Test event emitter for Kafka/NATS scenarios |
| `validator` | JetStream event validator with HTTP healthcheck endpoint |

### Infrastructure

| Component | Technology | Purpose |
|---|---|---|
| Event Bus | NATS JetStream | Versioned envelope transport, at-least-once delivery |
| Analytics Bus | Kafka (Redpanda v24.2.13) | Best-effort analytics path; topics: market.trades, market.orderbook |
| Flink Pipeline | Apache Flink 1.19 | Tumbling window SQL jobs (1m/5m/15m/1h OHLCV; 5m volume stats; trade tape) |
| Hot Storage | TimescaleDB (PG16) | Recent data (7 days), range queries, idempotent upserts + analytics schema |
| Cold Storage | ClickHouse 24.8.8 | Historical archive, analytical queries (90-day aggregation_*_cold tables) |
| BI Dashboards | Metabase v0.52.2 | Analytics profile; 11 views over TimescaleDB analytics schema |
| Actor Runtime | Hollywood v1.0.5 | Supervision, concurrency, message passing |
| Observability | Prometheus (100+ metrics), Grafana (5 dashboards), 13 alerts | Monitoring |
| Migrations | Goose | Schema evolution for TimescaleDB + ClickHouse |

### Exchange Parity (6 venues)

Binance (spot + futures), Bybit, Coinbase, HyperLiquid, Kraken (spot), KrakenF (futures).

Each adapter in `internal/adapters/exchange/{name}/` implements: endpoint, parser, backfill.

### Server Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness probe (all expected subsystems running) |
| `GET /runtime/snapshot` | Guardian state for observability |
| `GET /ws` | WebSocket upgrade |
| `GET /api/v1/candles` | Historical candle query |
| `GET /api/v1/stats` | Stats query |
| `GET /api/v1/snapshots` | Artifact snapshots |
| `GET /api/v1/markets` | Market discovery |
| `GET /api/v1/session` | Session metadata |
| `POST /runtime/reload` | Config reload |

### Backend ↔ Client Protocol

1. Client connects: `GET /ws` → HTTP upgrade → SessionActor spawned
2. Optional ClientHello handshake with capability negotiation
3. Client subscribes: `{"op":"subscribe","subject":"...","venue":"...","symbol":"...","channel":"...","aggregation":"..."}`
4. Server sends historical backfill (RangeStore → TimescaleDB/ClickHouse → envelopes)
5. Server streams live envelopes as they arrive
6. Backpressure: configurable drop policy (oldest/newest), slow-client disconnect

Wire format: Terminal_V1 protocol with versioned envelopes. Subject taxonomy: `<family>.<type>.v<version>`. Envelope carries `seq`, `prev_seq`, `ts_exchange`, `ts_ingest`, `idempotency_key`.

---

## Client Architecture (Odin)

### Layer Hierarchy

```
app/             ← orchestration, routing, state management
  ↓
layers/          ← visualization strategies (stateless)
  ↓
services/        ← domain stores, session health
  ↓         ↖ md_common (bridge: services ↔ layers)
ports/           ← adapter interfaces (input, fonts, marketdata)
  ↓
ui/ + math/      ← foundation (zero internal imports)
```

Zero cyclic dependencies. Each layer is a strict DAG:

| Package | Responsibility | May Import |
|---|---|---|
| **ui/, math/** | Primitive types, layout helpers, math utilities | Standard library only |
| **ports/** | Dependency injection interfaces (Marketdata_Port, Input_Port, Font_Port) | ui/ |
| **services/** | Fixed-capacity, zero-allocation-after-init stores per stream | ports/, util/ |
| **md_common/** | Protocol bridge — shared types for services ↔ layers | ports/, services/, util/ |
| **layers/** | Visual rendering strategies (stateless) | ports/, services/, md_common/, ui/, util/ |
| **app/** | Orchestration, workspace, state machine, frame loop | All lower tiers |

Additional packages: `streams/` (stream identity), `util/` (common utilities), `widgets/` (widget definitions).

### Dependency Rules

- **services/** never imports layers/ or app/
- **layers/** never imports app/
- **Layer_Context** is read-only; strategies are stateless
- Widgets receive all data through **Widget_Data_Context** — no globals

### State Pipeline

```
Stream_Apply_State (protocol-derived, per-stream)
    → Cell_Surface_View (10 fields: composition, health_level, reliability, ...)
        → Data_Readiness (6 variants: Absent, Pending, Degraded, Stale, Live, Recovering)
            → Pane_Visual_State (render decision)
```

All derivation is pure. No cached health or reliability state. Values are derived every frame from protocol state.

### Health Pipeline (5 layers)

Defined by ADR-0032 and ADR-0034:

| Layer | What It Tracks | Key Types |
|---|---|---|
| 1. Transport | Stream liveness | Stream_State (Offline / Live / Lag / Desync) |
| 2. Delivery | Composition progress, per-artifact staleness | Composition_Stage (Empty → Composed) |
| 3. Snapshot | Snapshot validity and freshness | Snapshot_Lifecycle (Absent → Live) |
| 4. Health & Recovery | Degradation level, recovery orchestration | System_Health_Level, Recovery_Status (3 backoff attempts) |
| 5. Reliability | Operator-visible trust classification | Stream_Reliability (7 states) |

**Render policy:** Allows render for Reliable, Degraded_Aging, Stale_Recovering. Blocks for Stale_Unrecoverable, Desync, Offline, Manual_Resync.

### Workspace Model

- **Split_Tree:** Binary layout tree (31 nodes max, 16 panes)
- **Pane:** Widget host with role (Primary_Chart, Auxiliary, Context)
- **Schema:** V12 with CRC checksum + artifact fingerprint
- **Widget kinds (13):** Candle, Stats, Counter, Heatmap, VPVR, Trades, Orderbook, DOM, Empty, Analytics, Session_VPVR, TPO, Footprint

### Orderflow

Orderflow is a cross-cutting concern across 4 tiers, not a separate bounded context (ADR-0033, ADR-0035):

| Tier | Owner | Examples |
|---|---|---|
| T0 Raw | marketdata | TradeTickV1, BookDeltaV1 |
| T1 Aggregates | aggregation | TapeWindowV1, OrderBookSnapshotV2, DeltaVolumeWindowV1 |
| T2 Derived | insights | VolumeProfileV1, HeatmapCellV1, FootprintCandleV1 |
| T3 Evidence | evidence | LiquidityEvidence (5 rule types) |

Client-side per-stream stores: DOM_Store (512 levels), Footprint_Store (200 candles × 50 levels), Trades_Store (256 ticks), Orderbook_Store (50/side).

### Platforms

| Platform | Rendering | WebSocket | Threading |
|---|---|---|---|
| **Web (WASM)** | Canvas2D via JS bridge | JS WebSocket bridge | Single-threaded |
| **Native** | GLFW/SDL2 + imgui | Native WebSocket | Background reader thread + mutex |

---

## Module Structure

```
stream-analytics/
├── cmd/                          # 7 service entrypoints
│   ├── consumer/                 # Exchange → Event Bus ingester
│   ├── processor/                # Event Bus → Aggregation + Evidence pipeline
│   ├── server/                   # HTTP + WS gateway
│   ├── store/                    # Storage lifecycle
│   ├── migrate/                  # Database migrations
│   ├── emulator/                 # Test event emitter (Kafka/NATS)
│   └── validator/                # JetStream event validator
├── internal/
│   ├── shared/                   # Foundation (24 packages)
│   ├── core/                     # Bounded contexts (hexagonal)
│   │   ├── marketdata/           #   domain/ + app/ + ports/
│   │   ├── marketmodel/          #   instrument metadata
│   │   ├── aggregation/          #   domain/ + app/ + ports/
│   │   ├── delivery/             #   domain/ + app/ + ports/
│   │   ├── insights/             #   domain/ + app/ + ports/
│   │   ├── evidence/             #   domain/ + app/ + ports/
│   │   └── workspace/            #   schema management
│   ├── adapters/                 # Infrastructure implementations
│   │   ├── exchange/             #   6 exchange adapters + common
│   │   ├── bus/                  #   InMemoryBus
│   │   ├── jetstream/            #   NATS JetStream
│   │   ├── kafka/                #   Kafka adapter
│   │   └── storage/              #   TimescaleDB + ClickHouse
│   ├── actors/                   # Hollywood actor subsystems
│   └── interfaces/               # HTTP server + WS server
├── client/                       # Odin UI (WASM + native)
│   └── src/core/
│       ├── app/                  #   orchestration, workspace, frame loop
│       ├── layers/               #   visualization strategies
│       ├── services/             #   per-stream stores
│       ├── md_common/            #   protocol bridge
│       ├── ports/                #   adapter interfaces
│       ├── streams/              #   stream identity
│       ├── widgets/              #   widget definitions
│       ├── ui/                   #   layout primitives
│       ├── math/                 #   math utilities
│       └── util/                 #   common utilities
├── proto/                        # Protobuf contracts
├── sql/                          # Goose migrations
└── deploy/                       # Docker + deployment
```

Each backend bounded context has its own `go.mod`. The workspace (`go.work`) connects them. Replace directives are required even in workspace mode.

---

## Invariants

### Layer Isolation (enforced by `make invariants-check`)

| ID | Rule |
|---|---|
| INV-LAY-01 | `internal/core/*` cannot import `internal/actors` |
| INV-LAY-02 | `internal/interfaces/*` cannot import `internal/adapters` |
| INV-LAY-03 | `internal/core/*` cannot import `internal/shared/policykit` |
| INV-LAY-04 | `internal/core/*` cannot import `internal/adapters` |
| INV-LAY-05 | `internal/core/*` cannot import `internal/interfaces` |
| INV-LAY-06 | `internal/actors/*` cannot import `internal/interfaces` |

### Domain Invariants

| ID | Rule |
|---|---|
| INV-DOM-01 | Core and actors must be protobuf-free |
| INV-DET-01 | Core cannot call `time.Now()` directly — use `clock.Clock` |
| INV-REP-01 | Replay module must remain offline (no NATS) |
| INV-BUS-01 | Subject taxonomy must maintain valid family/versioning |
| INV-ACK-01 | JetStream ingest must maintain ACK/NAK/TERM semantics |
| INV-TOPO-01 | Guardian must enforce readiness by expected subsystems + restart budget |
| INV-MEX-01 | Stream identity must include `venue + instrument + market_type` |

### Client Guard Rails

| Rule | Ceiling |
|---|---|
| Cell_Surface_View | 10 fields max |
| Data_Readiness | 6 variants max (adding requires ADR) |
| Per-stream store isolation | DOM, Footprint on Market_Stream |
| Workspace schema bump | Only on persistence format change |
| Pure derivation | No cached health/reliability state |

---

## Permitted and Prohibited Dependencies

### Permitted

- Adapters implement core ports
- Actors import core use cases and adapters
- Interfaces import actors (for HTTP/WS handlers)
- Client services import md_common
- Client layers import services and md_common
- Client app imports layers, services, ports

### Prohibited

- Core importing actors, adapters, or interfaces
- Interfaces importing adapters directly
- Actors importing interfaces
- Cross-context domain type imports without ADR (use contracts)
- Client services importing layers or app
- Client layers importing app
- `time.Now()` in core (use `clock.Clock`)
- `fmt.Sprintf` in hot path (use `FieldHasher`)
- Cached health/reliability state in client (derive per-frame)

Full boundary rules with code examples: [boundary-rules.md](boundary-rules.md).
Canonical naming conventions: [naming-rules.md](naming-rules.md).

---

## Performance Baseline (C4 Soak)

10M events, 4 exchanges, 85s → 117,697 evt/sec. p50=7µs, p95=13µs, p99=56µs.

---

## Test Coverage

- **Backend:** ~1,666 tests across all modules
- **Client:** 1,317 tests (512 md_common + 472 app + 246 services + 57 layers + 16 streams + 14 util)
- **CI:** 3-tier pipeline, 8 soak harnesses, testcontainers

---

## Docs Index

### Core Reference

- [Architecture Overview](README.md) — this file
- [Subsystem Responsibilities](subsystems.md) — per-subsystem boundary, I/O, capabilities
- [Sequencing Model](sequencing-model.md) — ordering guarantees, DecideMonotonic, prev_seq, replay
- [System Invariants](system-invariants.md) — live invariant index with gates
- [IQ Loop Invariants](iq-loop-invariants.md) — top-10 properties, guardrail metrics
- [TRUTH-MAP](TRUTH-MAP.md) — single source of truth per critical theme
- [Authority Map](AUTHORITY-MAP.md) — governance domain → authoritative document
- [Architectural Decisions](decisions.md) — ADR + RFC index
- [Boundary Rules](boundary-rules.md) — layer isolation, dependency direction, ownership
- [Naming Rules](naming-rules.md) — canonical ubiquitous language

### Domain Architecture

- [Orderbook](orderbook.md) | [Candle Aggregation](candle-aggregation.md) | [Stats Aggregation](stats-aggregation.md)
- [Heatmap](heatmap.md) | [Volume Profiles](volume-profiles.md) | [Liquidations and MarkPrice](liquidations-markprice.md)
- [Insights](insights.md) | [Storage](storage.md)

### ADR Index

- [ADR-0000](../adrs/ADR-0000-foundation.md) through [ADR-0035](../adrs/ADR-0035-orderflow-contract-architecture.md)
- Key: [0032 Stream Reliability](../adrs/ADR-0032-stream-reliability-model.md) | [0033 Orderflow Blueprint](../adrs/ADR-0033-orderflow-domain-blueprint.md) | [0034 Health Recovery](../adrs/ADR-0034-stream-health-recovery-completion.md) | [0035 Orderflow Contracts](../adrs/ADR-0035-orderflow-contract-architecture.md)

### Contracts

- [Event Bus Contract](../contracts/event-bus.md) | [Delivery WS Contract](../contracts/delivery-ws.md)

### Planning

- [Product Definition](../product-definition.md) | [Moat](../prds/moat.md)
