# Market Raccoon — Documentation

**Last updated:** 2026-03-10

---

## What Is Market Raccoon

Market Raccoon is a real-time, multi-exchange cryptocurrency market data platform with an integrated operational cockpit. It ingests, normalizes, aggregates, and visualizes live market data across 6 exchanges with sub-millisecond latency.

The system has two halves:

- **Backend (Go, ~131K LOC):** Actor-supervised pipeline that consumes exchange WebSocket feeds, normalizes into canonical envelopes, builds aggregated read models (candles, orderbook, stats, tape, heatmaps, volume profiles), and delivers them over WebSocket and HTTP. 12 bounded contexts, 10 actor subsystems, NATS JetStream event bus, TimescaleDB + ClickHouse storage. Execution framework behind a fail-closed governance boundary.

- **Client (Odin, ~30K LOC):** Cross-platform operational cockpit (WASM + native). 13 widget types, 8 indicators, 3 subplot analytics, orderflow visualization (DOM, footprint, trades, orderbook), workspace split-tree with compare mode, and a 5-layer stream health pipeline with operator-visible reliability signals.

This is decision infrastructure, not a trading platform. Venue execution exists but defaults to simulation behind a 5-gate governance boundary.

For the full canonical product definition, see [product-definition.md](product-definition.md).

---

## Architecture at a Glance

```
Exchange WS (6 venues)
    |
    v
[Consumer / MarketData] --> NATS JetStream --> [Processor / Aggregation]
                                                    |
                                              [Insights / Evidence]
                                                    |
                                        [Signal -> Strategy -> Execution -> Portfolio]
                                                    |
                                              [Delivery / Router]
                                                 |        |
                                              [Store]  [WS Session]
                                                          |
                                                    [Client]
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
- [TRUTH-MAP](architecture/TRUTH-MAP.md) — Single source of truth per critical theme
- [AUTHORITY-MAP](architecture/AUTHORITY-MAP.md) — Governance domain to authoritative document

### Product Requirements & Decisions
- [PRDs](prds/) — Product Requirement Documents (0001-0006)
- [ADRs](adrs/) — Architecture Decision Records (0000-0035); recent: [0032 Stream Reliability](adrs/ADR-0032-stream-reliability-model.md), [0033 Orderflow Blueprint](adrs/ADR-0033-orderflow-domain-blueprint.md), [0034 Health Recovery](adrs/ADR-0034-stream-health-recovery-completion.md), [0035 Orderflow Contracts](adrs/ADR-0035-orderflow-contract-architecture.md)
- [RFCs](rfcs/) — Request for Comments (0001-0011); W-series completed and superseded by stage system

### Contracts
- [Event Bus Contract](contracts/event-bus.md) — Topic taxonomy, envelope behavior
- [Delivery WS Contract](contracts/delivery-ws.md) — WebSocket protocol, backpressure, limits
- [Strategy/Execution/Portfolio Contracts](contracts/strategy-execution-portfolio-contracts.md) — Decision pipeline contracts

### Operations
- [Local Dev](local-dev.md) — Standing up the stack with Compose
- [Development Workflow](development-workflow.md) — Makefile targets, branching, CI
- [Testing Strategy](testing-strategy.md) — Unit, domain, and contract test expectations
- [Tooling](tooling.md) — Linter, workspace, pre-commits
- [Runbooks](runbooks/) — Incident procedures
- [Observability](observability/) — SLO definitions and telemetry

### Client
- [Client Documentation](client/) — Memory ownership, runtime, Odin conventions

### History
- [Stage Reports](stages/) — 158 incremental delivery reports (read for provenance, not as current spec)
- [Product Definition](product-definition.md) — Canonical "what is Market Raccoon" with quantitative snapshot

---

## Validation Gates

Every PR must pass:

```bash
make docs-check        # Header format, internal links, truth-map integrity
make invariants-check  # Domain isolation leak scan
make test-workspace    # Full module-based test suite
```

If a decision changes, amend the relevant ADR and update the canonical documents. Do not create parallel definitions.
