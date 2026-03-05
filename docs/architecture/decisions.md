# Architectural Decision Index

**Status:** Active
**Date:** 2026-03-05
**Owner:** Governance Doc-First Maintainer
**Relates to:** `docs/architecture/TRUTH-MAP.md`, `docs/architecture/AUTHORITY-MAP.md`

---

## Purpose

Provide a single-page reference for all ADRs: their status, scope, and the primary runtime evidence
that validates each decision. For the full text of any ADR, follow the path link.

---

## ADR Summary Table

| ADR | Title | Status | Implementation | Key Invariant / Evidence |
|---|---|---|---|---|
| [ADR-0000](../adrs/ADR-0000-foundation.md) | Foundation principles | Accepted | Full | Baseline for all bounded-context rules |
| [ADR-0001](../adrs/ADR-0001-bounded-contexts-and-boundaries.md) | Bounded contexts & boundaries | Accepted | Full | INV-LAY-01…06 (domain isolation guards) |
| [ADR-0002](../adrs/ADR-0002-event-envelope-and-versioning.md) | Event envelope & versioning | Accepted | Full | Canonical envelope schema; `idempotency_key` required |
| [ADR-0003](../adrs/ADR-0003-actor-runtime.md) | Actor runtime as execution model | Accepted | Full | `Guardian`; `SupervisorPolicy`; domain in `core/*` |
| [ADR-0004](../adrs/ADR-0004-bus-nats-jetstream.md) | Bus — NATS JetStream | Accepted | Full | INV-ACK-01 (`ingest_conformance_test.go`) |
| [ADR-0005](../adrs/ADR-0005-sequencing-and-time-normalization.md) | Sequencing & time normalization | Accepted | Full | `ts_exchange`/`ts_ingest`/`ts_server` model |
| [ADR-0006](../adrs/ADR-0006-storage-hot-vs-cold.md) | Storage hot vs. cold | Accepted | Full (cold-path deferred for new subjects) | L0 in-memory / L1 TimescaleDB / L2 ClickHouse split |
| [ADR-0007](../adrs/ADR-0007-delivery-ws-sessions.md) | Delivery — WS sessions | Accepted | Full | WS frame protocol; `prev_seq` chain contract |
| [ADR-0008](../adrs/ADR-0008-insights-decision-support.md) | Insights — decision support | Accepted | Full | Evidence-based, probabilistic, never instructive |
| [ADR-0009](../adrs/ADR-0009-config-jsonc-determinism.md) | Config — JSONC determinism | Accepted | Full | All runtime config in JSONC; no env-var silent defaults |
| [ADR-0010](../adrs/ADR-0010-config-loading-startup-validation.md) | Config loading & startup validation | Accepted | Full | Strict validation at startup; unknown fields rejected |
| [ADR-0011](../adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md) | MarketData — Binance canonical mapping | Accepted | Full | CMM normalization; instrument identity rules |
| [ADR-0012](../adrs/ADR-0012-lifecycle-invariants-leak-prevention.md) | Lifecycle invariants & leak prevention | Accepted | Full | `actor.Stopped` must release all resources deterministically |
| [ADR-0013](../adrs/ADR-0013-backpressure-overload-policies.md) | Backpressure & overload policies | Accepted | Full | Drop policies; VPVR overload; `policykit` in actors layer only |
| [ADR-0014](../adrs/ADR-0014-stream-partitioning-strategy.md) | Stream partitioning strategy | Accepted | Full | INV-BUS-01; subject taxonomy; `subject_validation.go` |
| [ADR-0015](../adrs/ADR-0015-deterministic-replay-time-invariants.md) | Deterministic replay & time invariants | Accepted | Full | INV-DET-01 + INV-REP-01; `replay/player.go:44` |
| [ADR-0016](../adrs/ADR-0016-protobuf-contract-layer.md) | Protobuf contract layer | Accepted | Partial | INV-DOM-01 (proto-free core); `payload_registry.go` |
| [ADR-0017](../adrs/ADR-0017-multi-exchange-normalization.md) | Multi-exchange normalization | Accepted | Full (MEX-4 guard) | INV-MEX-01; `venue+instrument+market_type` identity |
| [ADR-0018](../adrs/ADR-0018-actor-topology-supervision-model.md) | Actor topology & supervision model | Accepted | Partial | TOP-1…5; Guardian readiness; multi-replica wiring |
| [ADR-0019](../adrs/ADR-0019-dual-database-operational-strategy.md) | Dual-database operational strategy | Accepted | Full | TimescaleDB (hot reads) + ClickHouse (analytics/cold) |
| [ADR-0020](../adrs/ADR-0020-gitops-secrets-management.md) | GitOps secrets management | Accepted | In progress | SOPS-encrypted config; least-privilege DB users |
| [ADR-0021](../adrs/ADR-0021-signals-strategist-dedicated-topology-cutover.md) | Signals/Strategist dedicated topology cutover | Accepted | Partial | Dedicated services primary; embedded paths behind explicit flags |

---

## Architectural Decisions — Thematic Groups

### Core Architecture

| Theme | Primary ADR | Key rule |
|---|---|---|
| Domain isolation | ADR-0001 | `core/*` ← never imports `actors/*`, `adapters/*`, `interfaces/*`, or protobuf. |
| Actor model | ADR-0003, ADR-0018 | One actor per major unit of concurrency; domain decisions in `core/*`. |
| Event envelopes | ADR-0002 | Versioned, immutable, idempotency-keyed envelopes only. |
| Config / startup | ADR-0009, ADR-0010 | JSONC; strict validation; no silent defaults. |

### Sequencing & Time

| Theme | Primary ADR | Key rule |
|---|---|---|
| Time normalization | ADR-0005 | `ts_exchange` untrusted; `ts_ingest` monotonic per shard; `ts_server` mandatory. |
| Deterministic replay | ADR-0015 | `core/*` must not call `time.Now()`; replay offline (no NATS). |
| Stream partitioning | ADR-0014 | Subject = `{family}.v{n}.{venue}.{instrument}`; validated at publish/ingest. |
| Multi-exchange identity | ADR-0017 | Stream key = `venue | instrument | market_type`; enforced by MEX-4 guard. |

### Data Pipeline

| Theme | Primary ADR | Key rule |
|---|---|---|
| Bus | ADR-0004 | NATS JetStream; ACK/NAK/TERM semantics mandatory; dedup via `Nats-Msg-Id`. |
| Backpressure | ADR-0013 | Explicit drop policies; `policykit` only in actors layer; VPVR overload gate. |
| Storage planes | ADR-0006 | L0 in-memory (hot); L1 TimescaleDB (hot queries); L2 ClickHouse (analytics/cold). |
| Protobuf contracts | ADR-0016 | `proto/` is contract boundary; `core/*` protobuf-free; registry as authority. |
| MarketData mapping | ADR-0011 | Per-exchange adapter produces CMM envelopes; instrument normalization rules. |

### Operations

| Theme | Primary ADR | Key rule |
|---|---|---|
| Lifecycle / leaks | ADR-0012 | `actor.Stopped` = deterministic, idempotent teardown. |
| Delivery / WS | ADR-0007 | `prev_seq` chain mandatory; `snapshot` resets seq; `getrange`/`getlast` via store. |
| Insights | ADR-0008 | Probabilistic and evidence-based; never instructive; auditable. |
| Dual DB | ADR-0019 | TimescaleDB for operational queries; ClickHouse for analytics. |
| Secrets | ADR-0020 | SOPS encryption; zero plaintext secrets in VCS; per-service DB users. |

---

## RFC Summary Table

| RFC | Title | Status | Scope |
|---|---|---|---|
| [RFC-0001](../rfcs/RFC-0001-robustness-roadmap.md) | Robustness roadmap | Accepted | Overall hardening program |
| [RFC-0002](../rfcs/RFC-0002-w1-config-shutdown-hardening.md) | W1 Config & shutdown hardening | Accepted | Config validation, graceful shutdown |
| [RFC-0003](../rfcs/RFC-0003-W2-DELIVERY-BC.md) | W2 Delivery BC | Accepted | Delivery bounded context |
| [RFC-0004](../rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md) | W3 Sources — MarketData Binance | Accepted | Binance ingest adapter |
| [RFC-0005](../rfcs/RFC-0005-W4-observability-profiling.md) | W4 Observability & profiling | Accepted | Metrics, pprof, SLOs |
| [RFC-0006](../rfcs/RFC-0006-W5-memory-lifecycle-hardening.md) | W5 Memory & lifecycle hardening | Accepted | Goroutine/mem caps, VPVR overload |
| [RFC-0007](../rfcs/RFC-0007-W6-protobuf-contract-layer.md) | W6 Protobuf contract layer | Accepted (partial) | Proto registry, codec |
| [RFC-0008](../rfcs/RFC-0008-W7-nats-jetstream-integration.md) | W7 NATS JetStream integration | Accepted (partial) | JetStream streams, ACK semantics |
| [RFC-0009](../rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md) | W8 Deterministic replay & golden tests | Accepted | Offline replay, SHA fixtures |
| [RFC-0010](../rfcs/RFC-0010-W9-multi-exchange-readiness.md) | W9 Multi-exchange readiness | Accepted (partial) | MEX-4 guard, per-exchange actors |
| [RFC-0011](../rfcs/RFC-0011-product-parity-marketmonkey.md) | Product parity — MarketMonkey | Draft | Feature gap/drift checklist |
| [RFC-0012](../rfcs/RFC-0012-client-multi-exchange-evolution.md) | Client multi-exchange evolution | Draft | Client stream picker / multi-venue |
| [RFC-0013](../rfcs/RFC-0013-client-hardening-blueprint-marketmonkey-parity.md) | Client hardening blueprint | Draft | Client robustness goals |
| [RFC-0014](../rfcs/RFC-0014-client-ui-interaction-architecture-marketmonkey-reference.md) | Client UI interaction architecture | Draft | UI interaction model |
| [RFC-0015](../rfcs/RFC-0015-backend-subminute-hardening-rollout.md) | Backend sub-minute hardening rollout | Draft | 1s/5s timeframe pipeline |

---

## Decision Status Definitions

| Status | Meaning |
|---|---|
| **Accepted** | Decision is finalized and fully reflected in runtime. |
| **Accepted (partial)** | Decision is accepted; implementation is in progress. Implementation matrix in the ADR tracks gaps. |
| **Draft** | Decision is under discussion; not yet authoritative for runtime. |
| **Superseded** | Decision has been replaced by a newer ADR. |

---

## Governance Rules

1. PRDs define **what** must be true. RFCs/ADRs define **how**. PRDs win in conflict.
2. When runtime diverges from docs, **docs must be patched** (not the runtime).
3. Every claim in this index must anchor to ≥ 1 ADR/RFC/PRD, ≥ 1 code file:line, and ≥ 1 test or gate.
4. New architectural decisions must produce an ADR **before** merging implementation.

Authority: `docs/architecture/AUTHORITY-MAP.md`, `docs/architecture/TRUTH-MAP.md`.

---

## Changelog

- 2026-03-05: Initial creation. Synthesized from ADR directory, TRUTH-MAP, AUTHORITY-MAP, and
  ARCHITECTURE-DOSSIER-S16-S17.
