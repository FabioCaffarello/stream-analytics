# Architecture Overview

## Purpose

This system is a high-performance market intelligence platform designed to:

- Aggregate multi-venue market data
- Normalize and sequence events deterministically
- Build real-time read models
- Deliver low-latency streams to clients
- Generate evidence-based insights
- Maintain full auditability and replay capability

The architecture prioritizes:

- Determinism
- Fault isolation
- Replayability
- Observability
- Low cognitive latency for users

---

## System Philosophy

This is NOT a trading platform.

This is decision infrastructure.

The system helps professionals:

- detect risk earlier
- identify liquidity shifts
- understand positioning
- gain market clarity

Execution is explicitly out of scope.

---

## High-Level Flow

```text
Exchange → Ingestion Actors → Event Bus → Aggregation Actors
→ Hot Read Models → Delivery (WS/API)
                ↘
                 Cold Storage
```

---

## Core Principles

### 1. Domain First

Business invariants live in `internal/core`.

Actors coordinate execution — they do not own rules.

---

### 2. Event-Driven Everything

All state transitions originate from versioned events.

No hidden mutations.

Replay must be possible.

---

### 3. Deterministic Pipelines

Given the same event stream, the system must reproduce identical artifacts.

---

### 4. Bounded Context Isolation

Contexts:

- MarketData
- Aggregation
- Storage
- Delivery
- Insights

No cross-context leakage.

Communication happens through contracts.

---

### 5. Thin Infrastructure

Adapters must be replaceable.

The core must not know about:

- NATS
- ClickHouse
- Supabase
- Exchanges

Only ports.

---

## Runtime Model

We adopt an actor model to guarantee:

- concurrency safety
- supervision
- restartability
- lifecycle clarity

### Actor Responsibilities

Actors coordinate.

Use cases decide.

Domain enforces invariants.

---

## Hot vs Cold Path

### Hot Path

Used for:

- UI streaming
- agent consumption
- real-time detection

Characteristics:

- in-memory
- ultra-low latency
- disposable

### Cold Path

Used for:

- analytics
- backtesting
- audits
- historical queries

Characteristics:

- durable
- partitioned
- replayable

---

## Insights Philosophy

Insights are:

✔ probabilistic
✔ evidence-based
✔ auditable

They are NOT signals.

The system never instructs users to buy or sell.

---

## What Makes This Architecture Defensible

The moat is operational:

- deterministic event pipelines
- actor supervision
- audit trails
- evidence-backed insights
- venue divergence detection

Most competitors optimize UI.

We optimize cognition.

---

## Future Extensions

The architecture is intentionally prepared for:

- additional venues
- new asset classes
- agent expansion
- macro data
- on-chain analytics

without requiring rewrites.

---

## Docs Index (Official)

### Observability

- [Metrics Budget & Label Policy](./metrics-budget-label-policy.md)

### ADR Index

- [ADR-0000](../adrs/ADR-0000-foundation.md)
- [ADR-0001](../adrs/ADR-0001-bounded-contexts-and-boundaries.md)
- [ADR-0002](../adrs/ADR-0002-event-envelope-and-versioning.md)
- [ADR-0003](../adrs/ADR-0003-actor-runtime.md)
- [ADR-0004](../adrs/ADR-0004-bus-nats-jetstream.md)
- [ADR-0005](../adrs/ADR-0005-sequencing-and-time-normalization.md)
- [ADR-0006](../adrs/ADR-0006-storage-hot-vs-cold.md)
- [ADR-0007](../adrs/ADR-0007-delivery-ws-sessions.md)
- [ADR-0008](../adrs/ADR-0008-insights-decision-support.md)
- [ADR-0009](../adrs/ADR-0009-config-jsonc-determinism.md)
- [ADR-0010](../adrs/ADR-0010-config-loading-startup-validation.md)
- [ADR-0011](../adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md)
- [ADR-0012](../adrs/ADR-0012-lifecycle-invariants-leak-prevention.md)
- [ADR-0013](../adrs/ADR-0013-backpressure-overload-policies.md)
- [ADR-0014](../adrs/ADR-0014-stream-partitioning-strategy.md)
- [ADR-0015](../adrs/ADR-0015-deterministic-replay-time-invariants.md)
- [ADR-0016](../adrs/ADR-0016-protobuf-contract-layer.md)
- [ADR-0017](../adrs/ADR-0017-multi-exchange-normalization.md)
- [ADR-0018](../adrs/ADR-0018-actor-topology-supervision-model.md)

### RFC Index

- [RFC-0001](../rfcs/RFC-0001-robustness-roadmap.md)
- [RFC-0002](../rfcs/RFC-0002-w1-config-shutdown-hardening.md)
- [RFC-0003](../rfcs/RFC-0003-W2-DELIVERY-BC.md)
- [RFC-0004](../rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md)
- [RFC-0005](../rfcs/RFC-0005-W4-observability-profiling.md)
- [RFC-0006](../rfcs/RFC-0006-W5-memory-lifecycle-hardening.md)
- [RFC-0007](../rfcs/RFC-0007-W6-protobuf-contract-layer.md)
- [RFC-0008](../rfcs/RFC-0008-W7-nats-jetstream-integration.md)
- [RFC-0009](../rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md)
- [RFC-0010](../rfcs/RFC-0010-W9-multi-exchange-readiness.md)
- [RFC-0011](../rfcs/RFC-0011-product-parity-marketmonkey.md)
- [EXECUTION-SEQUENCE](../rfcs/EXECUTION-SEQUENCE.md)
- [ADR-REVISIONS patch plan](../rfcs/ADR-REVISIONS-patch-plan.md)
- [W4-W5 Audit](../rfcs/W4-W5-AUDIT.md)
- [W5.1 Sweep Throttling](../rfcs/W5.1-SWEEP-THROTTLING.md)

### Architecture Docs Index

- [Architecture Overview](README.md)
- [Doc Contract Template](doc-contract-template.md)
- [System Invariants](system-invariants.md)
- [TRUTH-MAP](TRUTH-MAP.md)
- [Ingestion](ingestion.md)
- [Insights](insights.md)
- [Moat](moat.md)
- [Storage](storage.md)
- [Orderbook](orderbook.md)
- [Heatmap](heatmap.md)
- [Volume Profiles](volume-profiles.md)
- [Liquidations and MarkPrice](liquidations-markprice.md)

### Contracts Index

- [Event Bus Contract](../contracts/event-bus.md)
- [Delivery WS Contract](../contracts/delivery-ws.md)
