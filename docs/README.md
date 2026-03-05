# Market Raccoon — Documentation Index

**Status:** Active
**Owner:** Core Engineering
**Last updated:** 2026-03-05

Welcome to the Market Raccoon core knowledge base. This directory is the single source of truth for all human-readable and agent-readable architectural decisions, boundaries, and runbooks.

> **Market Raccoon** is a market intelligence backend. Built heavily on Go, an actor-based runtime, deterministic event processing, and NATS JetStream. It normalizes exchange streams and serves low-latency multi-timeframe insights.

---

## 🧭 Find Anything in < 2 Minutes

### 1. Architecture & Subsystems
- **[Architecture Overview](architecture/README.md)** — Core principles and system flow.
- **[Subsystem Responsibilities](architecture/subsystems.md)** — Boundary, I/O, limits, and runtime anchors for all subsystems.
- **[Sequencing Model](architecture/sequencing-model.md)** — Ordering guarantees, predictability, replay, and monotonicity invariants.
- **[IQ Loop Invariants](architecture/iq-loop-invariants.md)** — The top 10 execution properties guarded by the runtime pipeline.
- **[System Invariants](architecture/system-invariants.md)** — Core domain invariants (`INV-DOM`, `INV-DET`, etc).
- **[Truth Map](architecture/TRUTH-MAP.md)** — Map of every product theme and its source of truth.

### 2. Product Requirements & Decisions
- **[PRDs](prds/)** — Product Requirement Documents detailing what the system must accomplish.
- **[RFCs](rfcs/)** — Request for Comments detailing how a specific epic or feature will be built.
- **[ADRs](adrs/)** — Architecture Decision Records detailing immutable rules.
  - *Tip: See [Architectural Decisions Index](architecture/decisions.md) for a summary.*

### 3. Contracts
- **[Contracts Index](contracts/)** — Schemas for exactly how systems communicate.
- **[Event Bus](contracts/event-bus.md)** — Canonical topic taxonomy and deterministic envelope behavior.
- **[Delivery WS](contracts/delivery-ws.md)** — External WebSocket protocol (frames, backpressure, limits).

### 4. Operations & Runbooks
- **[Operations](operations/)** — Guides for sharding, cold path recovery, testing degradation, and backups.
- **[Runbooks](runbooks/)** — Hard procedures for when things drift or break (e.g., `DRIFT-RUNBOOK.md`).
- **[Observability](observability/)** — SLO definitions and operational telemetry definitions.

### 5. Day-to-Day Development
- **[Development Workflow](development-workflow.md)** — Day 1 rules: Makefile targets, GitHub actions, branching.
- **[Local Dev](local-dev.md)** — How to stand up the stack using Compose.
- **[Testing Strategy](testing-strategy.md)** — Expectations on unit, domain, and contract boundaries.
- **[Tooling](tooling.md)** — Instructions for `golangci-lint`, the workspace setup, and pre-commits.
- **[Client App Rules](client/)** — Memory ownership, runtime, and the front-end roadmap.

---

## 🛡 Validation & Guidelines

Every PR must pass validation gates before merging. Our `Makefile` embeds compliance checks:
```bash
make docs-check        # Validates headers, internal links, truth-map integrity
make invariants-check  # Scans core code for domain isolation leaks
make test-workspace    # Executes the full module-based test suite
```

*(Note: Never update the runtime logic to drift away from these docs. If decisions change, amend the ADR and update this folder).*
