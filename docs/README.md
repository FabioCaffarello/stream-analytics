# Stream Analytics — Documentation

**Last updated:** 2026-06-25

---

## What Is Stream Analytics

Stream Analytics is a real-time, multi-exchange cryptocurrency market data platform with an integrated operational cockpit. It ingests, normalizes, aggregates, and visualizes live market data across 6 exchanges with sub-millisecond latency.

The system has two halves:

- **Backend (Go, ~131K LOC):** Actor-supervised pipeline that consumes exchange WebSocket feeds, normalizes into canonical envelopes, builds aggregated read models (candles, orderbook, stats, tape, heatmaps, volume profiles), and delivers them over WebSocket and HTTP. 7 active service binaries, NATS JetStream event bus, TimescaleDB + ClickHouse storage + Kafka analytics path. Hexagonal architecture, DDD bounded contexts, Hollywood actor model.

- **Client (Odin, ~30K LOC):** Cross-platform operational cockpit (WASM + native). 13 widget types, 8 indicators, 3 subplot analytics, orderflow visualization (DOM, footprint, trades, orderbook), workspace split-tree with compare mode, and a 5-layer stream health pipeline with operator-visible reliability signals.

This is decision infrastructure, not a trading platform. Venue execution exists but defaults to simulation behind a 5-gate governance boundary.

For the full canonical product definition, see [product-definition.md](product-definition.md).

---

## Architecture at a Glance

```
Exchange WS (6 venues)
    |
    v
[Consumer / MarketData] --> NATS JetStream --> [Processor / Aggregation + Insights + Evidence]
       |                                                    |
       +--> Kafka (best-effort) --> Flink SQL          [Delivery / Router]
                 |                       |              |           |
       TimescaleDB analytics         [Store]       [WS Session]  [Store]
            |                                           |
         Metabase                                   [Client]
```

**Backend:** Hexagonal architecture, DDD bounded contexts, actor model (Hollywood), event-driven. All state transitions via versioned envelopes. Deterministic and replay-safe.

**Client:** Strict DAG — `ports -> services -> layers -> app`. Pure derivation of health, reliability, and visual state per frame. Zero cyclic dependencies.

Full details in [architecture/README.md](architecture/README.md).

---

## How to Navigate This Documentation

Documents fall into three tiers:

| Tier | What it means | Examples |
|---|---|---|
| **Canonical** | Authoritative, actively maintained, reflects current codebase | Architecture README, ADRs, product-definition, contracts, system invariants |
| **Operational** | Day-to-day guides, correct but updated as needed | local-dev, development-workflow, testing-strategy, runbooks |
| **Historical** | Completed or superseded; kept for provenance, not for decision-making | Stage reports, completed RFCs (W1-W10), retired plans |

When in doubt, canonical documents win. The [AUTHORITY-MAP](architecture/AUTHORITY-MAP.md) maps each governance domain to its authoritative document.

---

## Main Documents

### Architecture & Boundaries
- [Architecture Overview](architecture/README.md) — Bounded contexts, data flow, runtime model, invariants, client architecture
- [Subsystem Responsibilities](architecture/subsystems.md) — Per-subsystem boundary, I/O, and caps
- [System Invariants](architecture/system-invariants.md) — Domain and layer isolation rules
- [Sequencing Model](architecture/sequencing-model.md) — Ordering guarantees and replay invariants
- [TRUTH-MAP](architecture/TRUTH-MAP.md) — Single source of truth per critical theme with code anchors
- [AUTHORITY-MAP](architecture/AUTHORITY-MAP.md) — Document tier classification (T1–T4)

### Architecture Diagrams (Mermaid)
- [Diagrams Index](architecture/diagrams/README.md) — All visual diagrams
- [C4 System Context](architecture/diagrams/c4-context.md) — Stream Analytics in its external environment
- [C4 Container Map](architecture/diagrams/c4-containers.md) — 7 service binaries, message bus, storage tiers
- [Actor Supervision Tree](architecture/diagrams/actor-supervision-tree.md) — Hollywood Guardian trees per binary
- [Sequence: Live Data Ingestion](architecture/diagrams/sequence-live-ingestion.md) — End-to-end pipeline flow
- [Sequence: Client Session Protocol](architecture/diagrams/sequence-client-session.md) — Terminal_V1 handshake, subscribe, backfill, resync
- [Sequence: Storage Federation](architecture/diagrams/sequence-storage-federation.md) — L0/L1/L2 write + federated read
- [Sequence: Evidence / LEL](architecture/diagrams/sequence-evidence-lel.md) — Liquidity Evidence Layer detection
- [Sequence: Exchange Recovery](architecture/diagrams/sequence-exchange-recovery.md) — Disconnect, backoff, reconnect, gap fill
- [C4 Analytics Profile](architecture/diagrams/c4-analytics.md) — Kafka, Flink, TimescaleDB analytics schema, Metabase
- [Sequence: Analytics Pipeline](architecture/diagrams/sequence-analytics-pipeline.md) — Consumer → Kafka → Flink → TimescaleDB → Metabase

### Contracts
- [Event Bus Contract](contracts/event-bus.md) — Topic taxonomy, envelope behavior, subject versioning
- [Delivery WS Contract](contracts/delivery-ws.md) — WebSocket protocol, backpressure, limits
- [Boundedness Matrix](contracts/boundedness-matrix.md) — Resource limits per subsystem
- [Canonical Market Model](contracts/canonical-market-model.md) — CMM field definitions
- [Liquidity Evidence Layer](contracts/liquidity-evidence-layer.md) — LEL rule contracts
- [Signal Engine](contracts/signal-engine.md) — Signal engine contract (**Retired S9**)

### Operations
- [Local Dev](local-dev.md) — Standing up the stack with Compose
- [Development Workflow](development-workflow.md) — Makefile targets, branching, CI
- [Testing Strategy](testing-strategy.md) — Unit, domain, and contract test expectations
- [Tooling](tooling.md) — Linter, workspace, pre-commits
- [Operations Runbooks](operations/sharding.md) — Sharding, cold-path, backup, degradation
- [Emulator](operations/emulator.md) — CLI tool for injecting synthetic events
- [Validator](operations/validator.md) — JetStream schema validation service (:8089)

### Client
- [Client Documentation](client/client-architecture.md) — Memory ownership, runtime, Odin conventions

### Product
- [Product Definition](product-definition.md) — Canonical "what is Stream Analytics" with quantitative snapshot

---

## Validation Gates

Every PR must pass:

```bash
make docs-check        # Header format, internal links, truth-map integrity
make invariants-check  # Domain isolation leak scan
make test-workspace    # Full module-based test suite
```

If a decision changes, amend the relevant ADR and update the canonical documents. Do not create parallel definitions.
