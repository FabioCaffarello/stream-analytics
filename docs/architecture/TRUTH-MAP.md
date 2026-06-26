# TRUTH-MAP — Single Source of Truth

**Status:** Active
**Date:** 2026-06-25
**last_reviewed:** 2026-06-25

---

## Purpose

Canonical map of: single source of truth per critical topic; code and test anchors that validate each claim.

---

## Single Source of Truth by Critical Theme

| Theme | Authoritative doc | Code anchor | Test anchor | State |
|---|---|---|---|---|
| Runtime invariants | `docs/architecture/system-invariants.md` | `scripts/ci/guards/check-domain-isolation.sh:16`, `internal/actors/runtime/guardian.go:273` | `internal/shared/contracts/import_guard_test.go:15`, `internal/actors/runtime/guardian_test.go:57` | Accepted; INV-LAY-01..06 automated |
| Subject taxonomy | `docs/contracts/event-bus.md` | `internal/adapters/jetstream/subject_validation.go:24` | `internal/adapters/jetstream/subject_validation_test.go:5` | Accepted |
| ACK semantics (ACK/NAK/TERM) | `docs/contracts/event-bus.md` | `internal/adapters/jetstream/consumer.go:279`, `internal/adapters/jetstream/ingest_policy.go:59` | `internal/adapters/jetstream/ingest_conformance_test.go:15` | Accepted |
| Replay determinístico | `docs/architecture/sequencing-model.md` | `internal/shared/replay/player.go:45`, `internal/shared/replay/sequencer.go:56`, `internal/shared/replay/canon.go:284` | `internal/shared/replay/golden_test.go:18`, `cmd/consumer/replay_test.go:63` | Accepted |
| Backpressure | `docs/architecture/subsystems.md` | `internal/core/insights/app/vpvr_overload_policy.go:1`, `internal/actors/insights/runtime/vpvr_policy.go:1` | `internal/adapters/storage/vpvr_overload_integration_test.go:TestIntegrationVPVROverload_AckBoundarySafeAndDeterministic`, `internal/actors/insights/runtime/vpvr_soak_test.go:TestVPVROverloadSoakBurstDeterministicBudgets` | Accepted/Done/Production-ready for VPVR overload path |
| Storage hot/cold + federation | `docs/architecture/storage.md` | `internal/core/aggregation/ports/readers.go:1`, `internal/adapters/storage/timescale/candle_reader.go:1`, `internal/adapters/storage/federation/merge.go:1`, `internal/adapters/storage/federation/candle_reader.go:1`, `cmd/server/bootstrap.go:buildStorageOptions` | `internal/adapters/storage/timescale/reader_test.go:1`, `internal/adapters/storage/federation/federation_test.go:1`, `internal/adapters/storage/federation/consistency_test.go:1` | L0 in-memory / L1 TimescaleDB / L2 ClickHouse; federation complete |
| Delivery WS protocol | `docs/contracts/delivery-ws.md` | `internal/actors/delivery/runtime/session_commands.go:handleResync`, `internal/actors/delivery/runtime/session_protocol.go:requireHelloGate` | `internal/actors/delivery/runtime/session_protocol_contract_test.go:TestProtocol_PrevSeqChain_MonotonicAcrossEvents`, `internal/actors/delivery/runtime/session_protocol_contract_test.go:TestProtocol_ResyncAck_CarriesWatermark` | Complete (prev_seq chain, resync, hello gate, clock skew) |
| Orderbook snapshots | `docs/architecture/orderbook.md` | `internal/core/aggregation/app/update_orderbook.go:33`, `internal/actors/delivery/runtime/router.go:167` | `internal/core/aggregation/app/golden_replay_test.go:1`, `internal/actors/delivery/runtime/router_test.go:70` | Runtime implemented |
| Heatmap derivation/persistence | `docs/architecture/heatmap.md` | `internal/core/insights/domain/heatmap_bucket.go:1`, `internal/core/insights/app/build_heatmap.go:1`, `internal/adapters/storage/clickhouse/heatmap_writer.go:1` | `internal/core/insights/app/build_heatmap_test.go:1`, `internal/interfaces/ws/heatmap_delivery_contract_test.go:1` | Implemented; hot/cold + delivery contract |
| Volume profile (VPVR) | `docs/architecture/volume-profiles.md` | `internal/core/insights/domain/volume_profile.go:1`, `internal/core/insights/app/build_volume_profile.go:1` | `internal/core/insights/app/build_volume_profile_test.go:1` | Domain + app implemented |
| Candle aggregation (OHLCV) | `docs/architecture/candle-aggregation.md` | `internal/core/aggregation/domain/candle.go:1`, `internal/core/aggregation/app/build_candle.go:1`, `internal/actors/aggregation/runtime/processor.go:775` | `internal/core/aggregation/app/build_candle_test.go:39`, `internal/actors/aggregation/runtime/processor_e2e_test.go:13`, `internal/interfaces/ws/candle_stats_delivery_contract_test.go:15` | Implemented; runtime + storage + WS contract |
| Stats aggregation | `docs/architecture/stats-aggregation.md` | `internal/core/aggregation/domain/stats.go:1`, `internal/core/aggregation/app/build_stats.go:1` | `internal/core/aggregation/app/build_stats_test.go:136`, `internal/actors/aggregation/runtime/processor_e2e_test.go:36` | Implemented; per-TF + cross-source + delivery |
| Liquidations and mark price | `docs/architecture/liquidations-markprice.md` | `internal/shared/contracts/authority_manifest.go:80`, `internal/shared/contracts/authority_manifest.go:100` | `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_MarkPriceTickV1`, `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_LiquidationTickV1` | Contracts implemented; pipeline production-ready |
| Contract layer | `docs/contracts/event-bus.md` | `internal/shared/contracts/payload_registry.go:19`, `internal/shared/codec/proto_codec.go:25` | `internal/shared/contracts/import_guard_test.go:15`, `internal/shared/contracts/authority_test.go:284` | Accepted; partial protobuf implementation |
| Multi-exchange normalization | `docs/architecture/subsystems.md` | `cmd/consumer/main.go:157`, `scripts/ci/guards/check-domain-isolation.sh:109` | `cmd/consumer/e2e_consumer_integration_test.go:24`, `internal/actors/runtime/guardian_test.go:99` | Runtime implemented; MEX-4 guard wired |
| Analytics pipeline (Kafka→Flink→BI) | `docs/architecture/analytics-pipeline.md` | `internal/adapters/kafka/composite_publisher.go:29`, `flink/sql/02_ohlcv_job.sql:1`, `sql/timescale/migrations/0009_analytics_metabase_views.sql:1` | — | Active; best-effort parallel path; 3 Flink jobs; 11 TimescaleDB views |
| Workspace schema V12 | `docs/architecture/workspace-schema.md` | `internal/core/workspace/domain/workspace.go:21`, `internal/core/workspace/domain/workspace.go:126` | `internal/core/workspace/domain/workspace_test.go:1` | Active; MaxSchemaVersion=12; FNV-1a fingerprint |
| Emulator CLI | `docs/operations/emulator.md` | `cmd/emulator/main.go:17` | — | Active; one-shot synthetic event injector for Kafka pipeline |
| Validator service | `docs/operations/validator.md` | `cmd/validator/main.go:92` | — | Active; JetStream consumer at :8089; durable validator-v1 |
| Metrics catalogue | `docs/architecture/metrics-catalogue.md` | `internal/actors/marketdata/runtime/telemetry.go:14` | — | Active; Prometheus metric inventory per binary |

---

## ADR Inventory

| ADR | File |
|-----|------|
| ADR-0000 Foundation | `docs/adrs/ADR-0000-foundation.md` |
| ADR-0032 Stream Reliability Model | `docs/adrs/ADR-0032-stream-reliability-model.md` |
| ADR-0033 Orderflow Domain Blueprint | `docs/adrs/ADR-0033-orderflow-domain-blueprint.md` |
| ADR-0034 Stream Health Recovery | `docs/adrs/ADR-0034-stream-health-recovery-completion.md` |
| ADR-0035 Orderflow Contract Architecture | `docs/adrs/ADR-0035-orderflow-contract-architecture.md` |

---

## Real Validation Gates

```bash
make docs-check
make invariants-check
make test-workspace
make test-workspace-race
make soak-check
```

Anchor: `Makefile`, `scripts/ci/docs/check-doc-headers.sh`, `scripts/ci/docs/check-doc-links.sh`, `scripts/ci/docs/check-truth-map.sh`, `scripts/ci/guards/check-domain-isolation.sh`, `scripts/test/soak/soak-test.sh`.
