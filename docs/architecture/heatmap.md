# Heatmap Architecture (Derivation + Persistence)

**Status:** Active
**Owner:** Product Architect
**Last updated:** 2026-02-19
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`

## Purpose

Define liquidity/activity heatmap modeling by price-time buckets with deterministic bucketing, hot/cold persistence, and strict WS payload budget.

## Terminology (canonical)

- `instrument`: canonical envelope key for partitioning.
- `subject`: bus taxonomy (`{event}.v{version}.{venue}.{instrument}`).
- `stream`: JetStream subject filter / WS stream key.
- `timeframe`: delivery/query window key.
- `envelope`: ADR-0002 metadata and ordering contract.
- `payload`: bucketized matrix body emitted by insights pipeline.

## Data Planes

### Inputs

- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.trade.v1.{venue}.{instrument}`

### Outputs

- Derived event: `insights.heatmap_snapshot.v1.{venue}.{instrument}`
- Planned derived event: `insights.heatmap_delta.v1.{venue}.{instrument}`
- WS stream: `insights.heatmap_snapshot/{venue}/{symbol}/{timeframe}`

### Storage

Hot:
- `timescale.insights_heatmap_bucket_hot`

Cold:
- `clickhouse.insights_heatmap_bucket_cold`

Key/idempotency:
- `(venue, instrument, timeframe, price_bucket, window_start_ts, seq_max)`
- `idempotency_key` per window + bucket

## Contracts

Planned bucket payload v1 (no runtime writer in this cycle):
- `venue`, `instrument`, `timeframe`
- `window_start_ts`, `window_end_ts`
- `price_bucket_low`, `price_bucket_high`
- `bid_liquidity`, `ask_liquidity`, `trade_volume`
- `seq_min`, `seq_max`, `samples`

WS payload budget rules:
- `max_cells_per_frame` (example: 2500)
- `max_payload_bytes` (example: 256KB)
- deterministic trim by priority (highest-intensity cells first)

## Invariants

- `HM-1`: price bucketing must be deterministic for same `tick_size` and same input.
- `HM-2`: closed time window cannot be mutated after commit.
- `HM-3`: no negative cells (`bid_liquidity`, `ask_liquidity`, `trade_volume` >= 0).
- `HM-4`: bounded cardinality per `(venue,instrument,timeframe)`.
- `HM-5`: replay of same fixture yields same matrix values and ordering.

## Backpressure

- Bounded pipeline queues per instrument.
- Progressive degradation under overload:
1. increase `price_bucket_size`;
2. reduce update cadence;
3. keep only top-N cells by intensity.
- Explicit drop policy for stale WS frames (keep-latest per window).

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Input event taxonomy and subject validation | Existing | `internal/adapters/jetstream/subject_validation.go` | `internal/adapters/jetstream/subject_validation_test.go` |
| Deterministic replay foundation (`ts_ingest`, `seq`) | Existing | `internal/shared/replay/player.go`, `internal/shared/replay/sequencer.go` | `internal/shared/replay/golden_test.go:TestGoldenReplay` |
| Bounded state/backpressure primitives | Existing | `internal/shared/ds/boundedmap.go`, `internal/actors/marketdata/runtime/backpressure_queue.go` | `internal/actors/marketdata/runtime/subsystem_test.go:TestSubsystem_WsMessage_nilParseFn_dropsMessage` |
| Heatmap domain model and builder use case | Existing | `internal/core/insights/domain/heatmap_bucket.go`, `internal/core/insights/app/build_heatmap.go` | `internal/core/insights/app/build_heatmap_test.go` |
| Heatmap hot/cold writers | Existing | `internal/adapters/storage/timescale/heatmap_writer.go`, `internal/adapters/storage/clickhouse/heatmap_writer.go`, `sql/clickhouse/migrations/0006_m8_heatmap_cold.sql` | `internal/adapters/storage/heatmap_writer_test.go`, `internal/adapters/storage/clickhouse/heatmap_writer_test.go` |
| Heatmap WS delivery contract | Existing | `internal/core/delivery/domain/envelope_policy.go`, `internal/actors/delivery/runtime/router.go` | `internal/interfaces/ws/heatmap_delivery_contract_test.go` |

## Storage Strategy

- Timescale: recent windows for low-latency query and delivery.
- ClickHouse: long-term historical matrix for analytics/recompute.
- Suggested retention:
- hot: 7-30 days
- cold: 180+ days

## Replay Strategy

- Reprocess `bookdelta+trade` per `(venue,instrument)` ordered by `(ts_ingest,seq)`.
- Compare per-window matrix hash (`window_hash`).
- Partial replay recomputes only affected windows.

## Observability

- `heatmap_build_latency_seconds{venue,instrument,timeframe}`
- `heatmap_cells_total{venue,instrument,timeframe}`
- `heatmap_payload_bytes{venue,instrument,timeframe}`
- `heatmap_drop_total{reason}`
- `heatmap_queue_depth{venue,instrument}`

Minimum:
- derivation lag
- drop rate
- queue depth

## Acceptance Tests

Existing tests used as invariants baseline:
- `internal/adapters/jetstream/subject_validation_test.go:TestValidateSubjectTaxonomy_Valid`
- `internal/adapters/jetstream/subject_validation_test.go:TestValidateSubjectPattern_InvalidRootFailsFast`
- `internal/shared/replay/golden_test.go:TestGoldenReplay`
- `internal/actors/marketdata/runtime/subsystem_test.go:TestSubsystem_WsMessage_nilParseFn_dropsMessage`

Tests to create for heatmap feature parity:
- `internal/core/insights/app/build_heatmap_test.go:TestHeatmapBucketizationDeterministic`
- `internal/core/insights/app/build_heatmap_test.go:TestHeatmapPayloadBudgetHardCap`
- `internal/core/insights/app/build_heatmap_test.go:TestHeatmapReplayGoldenMatrixHash`
- `internal/adapters/storage/heatmap_writer_test.go:TestHeatmapStorageHotColdIdempotent`
- `internal/adapters/storage/clickhouse/heatmap_writer_test.go:TestChHeatmapWriter_Save_Success`
- `internal/interfaces/ws/heatmap_delivery_contract_test.go:TestWSDelivery_HeatmapSnapshot_RoutedToSubscriber`

## Evidence Hooks

Current related evidence:
- `internal/core/aggregation/app/update_orderbook.go`
- `internal/shared/replay/player.go`
- `internal/adapters/jetstream/consumer.go`
- `internal/actors/marketdata/runtime/backpressure_queue.go`

Existing hooks:
- `internal/core/insights/domain/heatmap_bucket.go` (Existing)
- `internal/core/insights/app/build_heatmap.go` (Existing)
- `internal/core/insights/app/service.go` (Existing — InsightsService facade)

TODO hooks (skeleton):
- `internal/adapters/storage/timescale/heatmap_writer.go`
- `internal/adapters/storage/clickhouse/heatmap_writer.go`
- `internal/interfaces/ws/heatmap_delivery_contract_test.go`

## Failure Modes

- Bucket cardinality explosion:
  - Mitigation: hard cap budget + dynamic coarsening.
- Hot/cold drift:
  - Mitigation: reconcile job by per-window checksum.
- Poison payload in heatmap builder:
  - Mitigation: DLQ/quarantine + instrument isolation.
- Slow WS clients:
  - Mitigation: keep-latest + stale-frame drop + controlled disconnect.
