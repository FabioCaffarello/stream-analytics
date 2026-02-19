# TRUTH-MAP — W11/W12 Doc Inventory + Single Source of Truth

**Status:** Active
**Date:** 2026-02-13
**last_reviewed:** 2026-02-18
**Scope:** `docs/prd/PRD-0001-extreme-runtime.md`, `docs/audits/AUDIT-PACK-W11-finalization.md`, `docs/rfcs/EXECUTION-SEQUENCE.md`, `docs/rfcs/archive/ADR-REVISIONS-patch-plan.md`, `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`

## Purpose

Create one authoritative map of:
- document inventory (ADR/RFC/architecture/contracts);
- single source of truth per critical topic;
- code/test anchors that validate each critical claim.

## Parity Source Map (W12 Patch)

| Theme | Source doc | ADR/RFC authority | Test anchors | Status |
|---|---|---|---|---|
| Storage planes and persistence boundaries | `docs/architecture/storage.md` | `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` | `internal/adapters/jetstream/ingest_conformance_test.go:TestIngestConformance_AckNakTermGoldenTable`, `internal/shared/replay/golden_test.go:TestGoldenReplay` | Draft doc, runtime baseline existing, L1/L2 TODO |
| Orderbook snapshot and consistency flow | `docs/architecture/orderbook.md` | `docs/adrs/ADR-0005-sequencing-and-time-normalization.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` | `internal/core/aggregation/domain/orderbook_test.go:TestOrderBook_crossedBook`, `internal/core/aggregation/app/golden_replay_test.go:TestAggregationGoldenReplayFromFixture` | Draft doc, runtime partial existing |
| Heatmap derivation and payload budget | `docs/architecture/heatmap.md` | `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` | `internal/core/insights/app/build_heatmap_test.go:1` | Draft doc; domain+builder Existing; writers/delivery TODO |
| Volume profile (VPVR) | `docs/architecture/volume-profiles.md` | `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0017-multi-exchange-normalization.md` | `internal/adapters/storage/vpvr_overload_integration_test.go:TestIntegrationVPVROverload_AckBoundarySafeAndDeterministic`, `internal/actors/insights/runtime/vpvr_soak_test.go:TestVPVROverloadSoakBurstDeterministicBudgets` | Accepted/Done/Production-ready (overload policy VPVR) |
| Liquidations and markprice parity path | `docs/architecture/liquidations-markprice.md` | `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0017-multi-exchange-normalization.md` | `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_MarkPriceTickV1`, `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_LiquidationTickV1` | Draft doc, contracts existing |
| Delivery WS wire contract and lifecycle | `docs/contracts/delivery-ws.md` | `docs/adrs/ADR-0007-delivery-ws-sessions.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md` | `internal/actors/delivery/runtime/session_test.go:TestSession_parseSubscribeUnsubscribeGetRange`, `internal/actors/delivery/runtime/router_test.go:TestRouter_subscribeUnsubscribeAndBroadcast` | Draft doc, backpressure gap explicit |
| Parity roadmap and acceptance gates | `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` | `docs/prd/PRD-0001-extreme-runtime.md`, `docs/audits/AUDIT-PACK-W11-finalization.md` | `make docs-check`, `make invariants-check`, `make test-workspace`, `make test-workspace-race` | Draft RFC, active planning authority |

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
| ADR-REVISIONS patch plan | **(ARCHIVED)** Historical patch plan; amendments absorbed into ADRs. | `docs/rfcs/archive/ADR-REVISIONS-patch-plan.md:1` |
| W4-W5 Post-Merge Audit | **(ARCHIVED)** Superado por AUDIT-PACK-W11 e DRIFT-REPORT-W11. | `docs/audits/W4-W5-AUDIT.md:1` |
| W5.1 Sweep Throttling | **(ARCHIVED)** Superado por RFC-0006-W5-memory-lifecycle-hardening. | `docs/rfcs/archive/W5.1-SWEEP-THROTTLING.md:1` |

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
- `docs/adrs/ADR-0013-backpressure-overload-policies.md` (Accepted)
- `docs/adrs/ADR-0014-stream-partitioning-strategy.md` (Accepted)
- `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` (Accepted)
- `docs/adrs/ADR-0016-protobuf-contract-layer.md` (Accepted; partial implementation)
- `docs/adrs/ADR-0017-multi-exchange-normalization.md` (Accepted)
- `docs/adrs/ADR-0018-actor-topology-supervision-model.md` (Accepted; partial implementation)

Status anchors: `docs/adrs/ADR-0000-foundation.md:3`, `docs/adrs/ADR-0010-config-loading-startup-validation.md:3`, `docs/adrs/ADR-0016-protobuf-contract-layer.md:3`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md:3`.

#### RFCs (0001..0010)

- `docs/rfcs/RFC-0001-robustness-roadmap.md` (raw: Accepted, normalized: Accepted)
- `docs/rfcs/RFC-0002-w1-config-shutdown-hardening.md` (raw: Accepted - pronto para implementacao, normalized: Accepted)
- `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md` (raw: Implemented, normalized: Accepted)
- `docs/rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md` (raw: Implemented, normalized: Accepted)
- `docs/rfcs/RFC-0005-W4-observability-profiling.md` (raw: Done, normalized: Accepted)
- `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md` (raw: Done, normalized: Accepted)
- `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md` (raw: Implemented, normalized: Accepted (partial))
- `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md` (raw: Draft + Partially Implemented marker, normalized: Accepted (partial))
- `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md` (raw: Done, normalized: Accepted)
- `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md` (raw: Draft + Partially Implemented marker, normalized: Accepted (partial))

Status anchors: `docs/rfcs/RFC-0001-robustness-roadmap.md:3`, `docs/rfcs/RFC-0005-W4-observability-profiling.md:3`, `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md:3`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md:3`.

#### RFCs (0011+)

- `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` (Draft)

#### Architecture and Contracts

- `docs/architecture/README.md`
- `docs/architecture/ingestion.md`
- `docs/architecture/insights.md`
- `docs/prd/moat.md`
- `docs/architecture/system-invariants.md`
- `docs/architecture/storage.md`
- `docs/architecture/orderbook.md`
- `docs/architecture/heatmap.md`
- `docs/architecture/volume-profiles.md`
- `docs/architecture/liquidations-markprice.md`
- `docs/contracts/event-bus.md`
- `docs/contracts/delivery-ws.md`

### Single Source of Truth by Critical Theme

| Theme | Authoritative doc | Code anchor | Test anchor | State |
|---|---|---|---|---|
| Runtime invariants | `docs/audits/AUDIT-PACK-W11-finalization.md:25` | `scripts/check-domain-isolation.sh:16`, `internal/actors/runtime/guardian.go:273` | `internal/shared/contracts/import_guard_test.go:15`, `internal/actors/runtime/guardian_test.go:57` | Accepted (operational evidence); INV-LAY-01..06 automated |
| Subject taxonomy | `docs/adrs/ADR-0014-stream-partitioning-strategy.md:33` | `internal/adapters/jetstream/subject_validation.go:24` | `internal/adapters/jetstream/subject_validation_test.go:5` | Accepted |
| ACK semantics (ACK/NAK/TERM) | `docs/adrs/ADR-0004-bus-nats-jetstream.md:1`, `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md:1` | `internal/adapters/jetstream/consumer.go:279`, `internal/adapters/jetstream/ingest_policy.go:59` | `internal/adapters/jetstream/ingest_conformance_test.go:15` | Accepted in runtime; RFC remains Draft with explicit partial matrix |
| Replay deterministico | `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md:1`, `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md:1` | `internal/shared/replay/player.go:45`, `internal/shared/replay/sequencer.go:56`, `internal/shared/replay/canon.go:284` | `internal/shared/replay/golden_test.go:18`, `cmd/consumer/replay_test.go:63` | Accepted |
| Backpressure | `docs/adrs/ADR-0013-backpressure-overload-policies.md:1`, `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md:1` | `internal/core/insights/app/vpvr_overload_policy.go:1`, `internal/actors/insights/runtime/vpvr_policy.go:1`, `internal/shared/config/loader.go:280` | `internal/adapters/storage/vpvr_overload_integration_test.go:TestIntegrationVPVROverload_AckBoundarySafeAndDeterministic`, `internal/actors/insights/runtime/vpvr_soak_test.go:TestVPVROverloadSoakBurstDeterministicBudgets` | Accepted/Done/Production-ready for VPVR overload path |
| Storage hot/cold | `docs/adrs/ADR-0006-storage-hot-vs-cold.md:12` | `internal/core/aggregation/ports/ports.go:17`, `internal/core/aggregation/app/update_orderbook.go:141` | `internal/core/aggregation/app/update_orderbook_test.go:33` | Accepted with explicit cold-path deferral |
| Product parity roadmap | `docs/rfcs/RFC-0011-product-parity-marketmonkey.md:1` | `internal/shared/contracts/authority_manifest.go:37`, `internal/adapters/jetstream/subject_validation.go:13` | `internal/shared/contracts/marketdata_registry_test.go:17`, `internal/adapters/jetstream/subject_validation_test.go:5` | Draft (doc-first planning) |
| Orderbook snapshots and delivery contract | `docs/architecture/orderbook.md:1`, `docs/contracts/delivery-ws.md:1` | `internal/core/aggregation/app/update_orderbook.go:33`, `internal/actors/delivery/runtime/router.go:167` | `internal/core/aggregation/app/golden_replay_test.go:1`, `internal/actors/delivery/runtime/router_test.go:70` | Draft docs; runtime partial |
| Heatmap derivation/persistence | `docs/architecture/heatmap.md:1` | `internal/core/insights/domain/heatmap_bucket.go:1`, `internal/core/insights/app/build_heatmap.go:1` | `internal/core/insights/app/build_heatmap_test.go:1` | Draft doc; domain + app use cases Existing; writers/delivery TODO |
| Volume profile (VPVR) | `docs/architecture/volume-profiles.md:1` | `internal/core/insights/domain/volume_profile.go:1`, `internal/core/insights/app/build_volume_profile.go:1` | `internal/core/insights/app/build_volume_profile_test.go:1` | Draft doc; domain + app use cases Existing; writers/delivery TODO |
| Candle aggregation (OHLCV) | `docs/architecture/candle-aggregation.md:1` | — | — | Not started; doc-first |
| Stats aggregation (liq/funding/markprice per TF) | `docs/architecture/stats-aggregation.md:1` | — | — | Not started; doc-first |
| Liquidations and mark price e2e | `docs/architecture/liquidations-markprice.md:1` | `internal/shared/contracts/authority_manifest.go:80`, `internal/shared/contracts/authority_manifest.go:100` | `internal/shared/contracts/marketdata_registry_test.go:17`, `internal/shared/codec/payload_codec_test.go:28` | Draft (contracts exist, pipeline planned) |
| Contract layer | `docs/adrs/ADR-0016-protobuf-contract-layer.md:3`, `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md:1` | `internal/shared/contracts/payload_registry.go:19`, `internal/shared/codec/proto_codec.go:25` | `internal/shared/contracts/import_guard_test.go:15`, `internal/shared/contracts/authority_test.go:284` | Accepted ADR + accepted W6 foundation |
| Multi-exchange | `docs/adrs/ADR-0017-multi-exchange-normalization.md:1`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md:1` | `cmd/consumer/main.go:157`, `scripts/check-domain-isolation.sh:109` | `cmd/consumer/e2e_consumer_integration_test.go:24`, `internal/actors/runtime/guardian_test.go:99` | Runtime implemented; MEX-4 guard wired in `invariants-check` |

### Real Validation Gates (Workspace-Safe)

Canonical gates in this round:

```bash
make docs-check
make invariants-check
make test-workspace
make test-workspace-race
make soak-check
```

Anchor: `Makefile`, `scripts/check-doc-headers.sh`, `scripts/check-doc-links.sh`, `scripts/check-truth-map.sh`, `scripts/check-feature-pack-links.sh`, `scripts/check-pack-subjects-vs-event-bus.sh`, `scripts/check-domain-isolation.sh`, `scripts/soak-test.sh`.

## Acceptance

- Inventory includes ADR-0000..0018 and RFC-0001..0011.
- All requested topics have single-source mapping to doc + code/test anchors.
- Any unresolved drift is explicitly marked as `TODO` or `OPEN QUESTION`.

## Changelog

- 2026-02-18:
  - fixed ADR-0013 inventory status: `Proposed` → `Accepted` (matches actual ADR file);
  - fixed ADR-0016 inventory status: `Proposed; W6-1 accepted` → `Accepted; partial implementation`;
  - fixed ADR-0018 inventory status: `Proposed; runtime implemented` → `Accepted; partial implementation`;
  - promoted RFC-0008 normalized status: `Draft` → `Accepted (partial)`;
  - promoted RFC-0010 normalized status: `Draft` → `Accepted (partial)`;
  - fixed Heatmap SSoT row: domain+builder exist (not TODO); updated code anchors;
  - fixed Volume Profile SSoT row: domain+builder exist (not TODO); updated code anchors;
  - fixed Heatmap parity row: domain+builder Existing; writers/delivery TODO;
  - fixed Contract layer SSoT row: `Proposed ADR` → `Accepted ADR`;
  - added SSoT rows for Candle aggregation (OHLCV) and Stats aggregation (not started; doc-first).
- 2026-02-17:
  - updated runtime invariants row: INV-LAY-01..06 automated guards;
  - added BC facade files: `marketdata/app/service.go`, `aggregation/app/service.go`, `insights/app/service.go`;
  - actors rewired to use facade services (MarketDataService, AggregationService);
  - updated VPVR/backpressure rows: soak test moved from `core/insights/app` to `actors/insights/runtime`;
  - added `vpvr_policy.go` code anchor for backpressure (policykit binding now in actors layer).
- 2026-02-13:
  - created W11 truth map with full ADR/RFC/architecture/contracts inventory;
  - mapped single source of truth for runtime invariants, taxonomy, ACK semantics, replay, backpressure, storage, contract layer and multi-exchange;
  - added workspace-safe gate commands used by PREVC validation.
  - aligned gate set to include `make docs-check` + `check-pack-subjects-vs-event-bus` guard.
  - reconciled PRD/RFC W7/W9 summaries after governance normalization wave 2.
  - added MEX-4 guard anchor (`scripts/check-domain-isolation.sh`) in multi-exchange authority row.
  - added parity v1 document authority set (`storage`, `orderbook`, `heatmap`, `volume-profiles`, `liquidations-markprice`, `delivery-ws`, `RFC-0011`).
