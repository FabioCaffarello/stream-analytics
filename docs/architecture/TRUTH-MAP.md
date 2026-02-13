# TRUTH-MAP — W11 Doc Inventory + Single Source of Truth

**Status:** Draft
**Date:** 2026-02-13
**Scope:** `docs/prd/PRD-0001-extreme-runtime.md`, `docs/audits/AUDIT-PACK-W11-finalization.md`, `docs/rfcs/EXECUTION-SEQUENCE.md`, `docs/rfcs/ADR-REVISIONS-patch-plan.md`

## Purpose

Create one authoritative map of:
- document inventory (ADR/RFC/architecture/contracts);
- single source of truth per critical topic;
- code/test anchors that validate each critical claim.

## Invariants

- Every critical claim must anchor to at least one of: ADR/RFC/PRD, code file:line, test file:test.
- Taxonomy target: ADR (`Accepted|Proposed|Superseded`), RFC (`Draft|Accepted`), PRD (`Active|Deprecated`).
- When a claim is unresolved in this round, mark as `TODO` or `OPEN QUESTION`.

## Evidence

### Base Docs (Round Input)

| Doc | Summary | Anchor |
|---|---|---|
| PRD-0001 | Normalized active baseline with Implemented/Partially Implemented/Planned matrix and workspace-safe gates. | `docs/prd/PRD-0001-extreme-runtime.md:1`, `docs/prd/PRD-0001-extreme-runtime.md:81` |
| AUDIT-PACK-W11 | Contains strongest evidence matrix linking docs to code/tests. | `docs/audits/AUDIT-PACK-W11-finalization.md:25` |
| EXECUTION-SEQUENCE | Tracks W4..W13 with explicit Implemented/Partially Implemented/Planned matrix and real workspace gates. | `docs/rfcs/EXECUTION-SEQUENCE.md:1`, `docs/rfcs/EXECUTION-SEQUENCE.md:94` |
| ADR-REVISIONS patch plan | Historical patch plan; some amendments already absorbed into ADRs. | `docs/rfcs/ADR-REVISIONS-patch-plan.md:1` |

### Document Inventory

#### ADRs (0000..0018)

- `docs/adrs/ADR-0000-foundation.md` (Accepted)
- `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md` (Accepted)
- `docs/adrs/ADR-0002-event-envelope-and-versioning.md` (Accepted)
- `docs/adrs/ADR-0003-actor-runtime.md` (Accepted)
- `docs/adrs/ADR-0004-bus-nats-jetstream.md` (Accepted)
- `docs/adrs/ADR-0005-sequencing-and-time-normalization.md` (Accepted)
- `docs/adrs/ADR-0006-storage-hot-vs-cold.md` (Accepted)
- `docs/adrs/ADR-0007-delivery-ws-sessions.md` (Accepted)
- `docs/adrs/ADR-0008-insights-decision-support.md` (Accepted)
- `docs/adrs/ADR-0009-config-jsonc-determinism.md` (Accepted)
- `docs/adrs/ADR-0010-config-loading-startup-validation.md` (Accepted)
- `docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md` (Accepted)
- `docs/adrs/ADR-0012-lifecycle-invariants-leak-prevention.md` (Accepted)
- `docs/adrs/ADR-0013-backpressure-overload-policies.md` (Proposed)
- `docs/adrs/ADR-0014-stream-partitioning-strategy.md` (Accepted)
- `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` (Accepted)
- `docs/adrs/ADR-0016-protobuf-contract-layer.md` (Proposed; W6-1 accepted)
- `docs/adrs/ADR-0017-multi-exchange-normalization.md` (Accepted)
- `docs/adrs/ADR-0018-actor-topology-supervision-model.md` (Proposed; runtime implemented, evidence partial)

Status anchors: `docs/adrs/ADR-0000-foundation.md:3`, `docs/adrs/ADR-0010-config-loading-startup-validation.md:3`, `docs/adrs/ADR-0016-protobuf-contract-layer.md:3`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md:3`.

#### RFCs (0001..0010)

- `docs/rfcs/RFC-0001-robustness-roadmap.md` (raw: Accepted, normalized: Accepted)
- `docs/rfcs/RFC-0002-w1-config-shutdown-hardening.md` (raw: Accepted - pronto para implementacao, normalized: Accepted)
- `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md` (raw: Implemented, normalized: Accepted)
- `docs/rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md` (raw: Implemented, normalized: Accepted)
- `docs/rfcs/RFC-0005-W4-observability-profiling.md` (raw: Done, normalized: Accepted)
- `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md` (raw: Done, normalized: Accepted)
- `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md` (raw: Implemented, normalized: Accepted (partial))
- `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md` (raw: Draft + Partially Implemented marker, normalized: Draft)
- `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md` (raw: Done, normalized: Accepted)
- `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md` (raw: Draft + Partially Implemented marker, normalized: Draft)

Status anchors: `docs/rfcs/RFC-0001-robustness-roadmap.md:3`, `docs/rfcs/RFC-0005-W4-observability-profiling.md:3`, `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md:3`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md:3`.

#### Architecture and Contracts

- `docs/architecture/README.md`
- `docs/architecture/ingestion.md`
- `docs/architecture/insights.md`
- `docs/architecture/moat.md`
- `docs/architecture/system-invariants.md`
- `docs/contracts/event-bus.md`

### Single Source of Truth by Critical Theme

| Theme | Authoritative doc | Code anchor | Test anchor | State |
|---|---|---|---|---|
| Runtime invariants | `docs/audits/AUDIT-PACK-W11-finalization.md:25` | `scripts/check-domain-isolation.sh:16`, `internal/actors/runtime/guardian.go:273` | `internal/shared/contracts/import_guard_test.go:15`, `internal/actors/runtime/guardian_test.go:57` | Accepted (operational evidence) |
| Subject taxonomy | `docs/adrs/ADR-0014-stream-partitioning-strategy.md:33` | `internal/adapters/jetstream/subject_validation.go:24` | `internal/adapters/jetstream/subject_validation_test.go:5` | Accepted |
| ACK semantics (ACK/NAK/TERM) | `docs/adrs/ADR-0004-bus-nats-jetstream.md:1`, `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md:1` | `internal/adapters/jetstream/consumer.go:279`, `internal/adapters/jetstream/ingest_policy.go:59` | `internal/adapters/jetstream/ingest_conformance_test.go:15` | Accepted in runtime; RFC remains Draft with explicit partial matrix |
| Replay deterministico | `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md:1`, `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md:1` | `internal/shared/replay/player.go:45`, `internal/shared/replay/sequencer.go:56`, `internal/shared/replay/canon.go:284` | `internal/shared/replay/golden_test.go:18`, `cmd/consumer/replay_test.go:63` | Accepted |
| Backpressure | `docs/adrs/ADR-0013-backpressure-overload-policies.md:1`, `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md:1` | `internal/actors/marketdata/runtime/backpressure_queue.go:56`, `internal/shared/config/loader.go:280` | `internal/actors/marketdata/runtime/backpressure_queue_test.go:1` | Proposed ADR + implemented runtime (`OPEN QUESTION` for ADR acceptance) |
| Storage hot/cold | `docs/adrs/ADR-0006-storage-hot-vs-cold.md:12` | `internal/core/aggregation/ports/ports.go:17`, `internal/core/aggregation/app/update_orderbook.go:141` | `internal/core/aggregation/app/update_orderbook_test.go:33` | Accepted with explicit cold-path deferral |
| Contract layer | `docs/adrs/ADR-0016-protobuf-contract-layer.md:3`, `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md:1` | `internal/shared/contracts/payload_registry.go:19`, `internal/shared/codec/proto_codec.go:25` | `internal/shared/contracts/import_guard_test.go:15`, `internal/shared/contracts/authority_test.go:284` | Proposed ADR + accepted W6 foundation |
| Multi-exchange | `docs/adrs/ADR-0017-multi-exchange-normalization.md:1`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md:1` | `cmd/consumer/main.go:157`, `scripts/check-domain-isolation.sh:109` | `cmd/consumer/e2e_consumer_integration_test.go:24`, `internal/actors/runtime/guardian_test.go:99` | Runtime implemented; MEX-4 guard wired in `invariants-check` |

### Real Validation Gates (Workspace-Safe)

Canonical gates in this round:

```bash
make invariants-check
make test-workspace
make test-workspace-race
make soak-check
```

Anchor: `Makefile:123`, `Makefile:136`, `Makefile:139`, `Makefile:142`.

## Acceptance

- Inventory includes ADR-0000..0018 and RFC-0001..0010.
- All requested topics have single-source mapping to doc + code/test anchors.
- Any unresolved drift is explicitly marked as `TODO` or `OPEN QUESTION`.

## Changelog

- 2026-02-13:
  - created W11 truth map with full ADR/RFC/architecture/contracts inventory;
  - mapped single source of truth for runtime invariants, taxonomy, ACK semantics, replay, backpressure, storage, contract layer and multi-exchange;
  - added workspace-safe gate commands used by PREVC validation.
  - reconciled PRD/RFC W7/W9 summaries after governance normalization wave 2.
  - added MEX-4 guard anchor (`scripts/check-domain-isolation.sh`) in multi-exchange authority row.
