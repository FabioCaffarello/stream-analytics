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
