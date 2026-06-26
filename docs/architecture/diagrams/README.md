# Architecture Diagrams

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/README.md`, `docs/architecture/subsystems.md`

---

## Purpose

Visual representation of Stream Analytics's architecture using Mermaid diagrams.
These diagrams complement the textual canonical docs — they show *how* the system
flows, not just what the components are. Text docs govern; diagrams illustrate.

All diagrams render natively on GitHub, GitLab, and most Markdown viewers.

---

## Diagram Index

### C4 Architecture Model

| Diagram | Level | What it shows |
|---------|-------|---------------|
| [C4 System Context](c4-context.md) | L1 — Context | Stream Analytics in its external environment (operators, exchanges, data stores) |
| [C4 Container Map](c4-containers.md) | L2 — Containers | The 7 service binaries, message bus, and storage tiers |
| [C4 Analytics Profile](c4-analytics.md) | L2 — Analytics | Kafka, Flink, TimescaleDB analytics schema, Metabase (analytics profile) |
| [Actor Supervision Tree](actor-supervision-tree.md) | L3 — Runtime | Guardian-managed Hollywood actors per binary |

### Sequence Diagrams — Data Pipeline

| Diagram | Scope |
|---------|-------|
| [Live Data Ingestion](sequence-live-ingestion.md) | Exchange WebSocket → Consumer → NATS → Processor → Delivery + Store → Client |
| [Analytics Pipeline](sequence-analytics-pipeline.md) | Consumer → Kafka → Flink SQL tumbling windows → TimescaleDB → Metabase |
| [Client Session Protocol](sequence-client-session.md) | WebSocket handshake (Terminal_V1), subscription, backfill, live streaming |
| [Storage Federation Write Path](sequence-storage-federation.md) | Aggregation → L0 in-memory → L1 TimescaleDB → L2 ClickHouse |
| [Evidence Detection (LEL)](sequence-evidence-lel.md) | Liquidity Evidence Layer: stateful rule evaluation + multi-replica ownership |

### Sequence Diagrams — Resilience

| Diagram | Scope |
|---------|-------|
| [Exchange Reconnect & Recovery](sequence-exchange-recovery.md) | WebSocket disconnect → exponential backoff → reconnect → replay gap |

---

## Format Convention

- All diagrams use **Mermaid** for source-renderable markup.
- Diagram files follow the same header format as canonical docs (`**Status:**`, `**Last updated:**`).
- Sequence diagram participants use the canonical actor/subsystem names from `subsystems.md`.
- These diagrams are **T2 operational** — updated when behaviour changes but not gating CI.
