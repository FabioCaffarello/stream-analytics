# Odin M8 Evidence - Heatmap Delivery Production

Date: 2026-02-19
Milestone: M8 - Heatmap Delivery Production

## Scope Closed

- Heatmap snapshot pipeline is active end-to-end (runtime -> delivery -> cold-path storage).
- Store consumer now routes `insights.heatmap_snapshot.v1` with payload decoding by registry contract.
- ClickHouse cold writer for heatmap is production-aligned (batch insert + deterministic idempotency key).
- WS delivery contract accepts and routes heatmap snapshot subjects to subscribed sessions.
- Docs/contracts/registry/PRD/truth-map moved from planned/TODO state to implemented evidence for snapshot stream.

## Code Changes

- Added heatmap cold writer for ClickHouse batch path:
  - `internal/adapters/storage/clickhouse/heatmap_writer.go`
  - `internal/adapters/storage/clickhouse/heatmap_writer_test.go`
- Added store routing + handler for heatmap snapshots:
  - `cmd/store/bootstrap.go`
  - `cmd/store/heatmap_handler_test.go`
- Added delivery policy support + WS contract test:
  - `internal/core/delivery/domain/envelope_policy.go`
  - `internal/core/delivery/domain/envelope_policy_test.go`
  - `internal/interfaces/ws/heatmap_delivery_contract_test.go`
- Added ClickHouse migration for cold heatmap table:
  - `sql/clickhouse/migrations/0006_m8_heatmap_cold.sql`
  - `cmd/store/schema_check.go`

## Validation Commands

### Focused M8 test suites

```bash
go test ./internal/adapters/storage/clickhouse
go test ./cmd/store -run 'Heatmap|Schema|HandleStoreEnvelope|HandleAggregationSnapshot' -count=1
go test ./internal/core/delivery/domain
go test ./internal/interfaces/ws
```

Result: PASS

### Governance gates

```bash
make docs-check
make invariants-check
```

Result: PASS

## M8 Exit Criteria Mapping

- Pipeline heatmap ativo: PASS (`ProcessorSubsystemActor` publishes + `cmd/store` persists `insights.heatmap_snapshot`).
- Contrato WS verde: PASS (`TestWSDelivery_HeatmapSnapshot_RoutedToSubscriber`).
- Storage/writer robusto: PASS (batch writer + migration + schema contract guard).
- Evidência operacional/documental: PASS (contracts + architecture + PRD + truth-map synchronized).

## Contract and Documentation Sync

- `docs/contracts/event-bus.md`: `insights.heatmap_snapshot.v1` promoted to stable.
- `docs/contracts/subject-registry.yaml`: heatmap snapshot status set to stable and producer/schema authority aligned with aggregation runtime.
- `docs/contracts/delivery-ws.md`: heatmap snapshot added to input plane and WS examples.
- `docs/architecture/heatmap.md`: implementation matrix and acceptance tests updated to existing coverage.
- `.context/docs/feature-packs/heatmap.md`: outputs/WS/evidence hooks switched to implemented snapshot stream.
- `docs/architecture/TRUTH-MAP.md`: heatmap rows updated to implemented (M8).
- `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`: heatmap moved from non-goal to implemented current-state capability.
