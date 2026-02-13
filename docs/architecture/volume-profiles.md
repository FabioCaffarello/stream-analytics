# Volume Profiles Architecture (VPVR + Price Ranges)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0017-multi-exchange-normalization.md`

## Purpose

Define minimum-scope VPVR/footprint for v1, including price-range aggregation, cardinality risk controls, and hot/cold persistence plan.

## Terminology (canonical)

- `instrument`: canonical envelope identity key.
- `subject`: bus routing key with version segment.
- `stream`: subscription pattern (JetStream/WS).
- `timeframe`: aggregation window boundary.
- `envelope`: ordering/idempotency carrier.
- `payload`: volume-distribution snapshot or delta body.

## Data Planes

### Inputs

- `marketdata.trade.v1.{venue}.{instrument}` (primary)
- `marketdata.bookdelta.v1.{venue}.{instrument}` (optional context enrichment)

### Outputs

- Planned derived event: `insights.<volume_profile_snapshot>.v1.{venue}.{instrument}` (TBD registry key)
- Planned derived event: `insights.<volume_profile_delta>.v1.{venue}.{instrument}` (TBD registry key)
- Planned WS stream: `insights.volume_profile/{venue}/{symbol}/{timeframe}`

### Storage

Hot:
- `timescale.insights_volume_profile_hot`

Cold:
- `clickhouse.insights_volume_profile_cold`

Keys/idempotency:
- `(venue, instrument, timeframe, price_bucket, window_start_ts)`
- `idempotency_key` per window + bucket + aggregation version

## Contracts

Planned VPVR snapshot payload v1:
- `venue`, `instrument`, `timeframe`
- `window_start_ts`, `window_end_ts`
- `buckets[]` with `price_low`, `price_high`, `buy_volume`, `sell_volume`, `total_volume`
- `poc_price` (point of control)
- `value_area_low`, `value_area_high`
- `seq_min`, `seq_max`

Minimum v1 scope:
- VPVR by instrument and fixed timeframe
- no micro-tick aggressor footprint yet

## Invariants

- `VP-1`: aggregated volume is additive and non-negative.
- `VP-2`: price bucket assignment is deterministic for same `tick_size`.
- `VP-3`: `poc_price` must belong to highest-volume bucket.
- `VP-4`: bounded bucket count per window (cardinality cap).
- `VP-5`: replay of same window yields identical snapshot.

## Backpressure

- Bounded queue per instrument/timeframe.
- Containment strategy:
1. increase bucket size;
2. reduce snapshot cadence;
3. prioritize window close over incremental updates.
- ACK only after hot commit.

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Deterministic insights join foundation | Existing | `internal/core/insights/app/join_crossvenue_trades.go` | `internal/core/insights/app/join_crossvenue_trades_test.go:TestJoinCrossVenueTrades_GoldenDeterministicSnapshotAndSignalBytes_50Runs` |
| Input subject and partition validation | Existing | `internal/adapters/jetstream/subject_validation.go`, `internal/shared/config/loader.go` | `internal/adapters/jetstream/subject_validation_test.go`, `internal/shared/config/loader_test.go` |
| VPVR domain model and aggregation use case | TODO | `internal/core/insights/domain/volume_profile.go` (TODO), `internal/core/insights/app/build_volume_profile.go` (TODO) | `internal/core/insights/app/build_volume_profile_test.go` (TODO) |
| VPVR durable storage adapters | TODO | `internal/adapters/storage/timescale/volume_profile_writer.go` (TODO), `internal/adapters/storage/clickhouse/volume_profile_writer.go` (TODO) | `internal/adapters/storage/volume_profile_writer_test.go` (TODO) |
| VPVR WS delivery contract | TODO | `internal/interfaces/ws/volume_profile_delivery.go` (TODO) | `internal/interfaces/ws/volume_profile_delivery_test.go` (TODO) |

## Storage Strategy

- Timescale: recent snapshots/deltas for low-latency query.
- ClickHouse: long-term distribution history.
- Compact closed window before moving to cold.

## Replay Strategy

- Rebuild from `marketdata.trade` in deterministic order.
- Golden per window compares:
1. bucket distribution;
2. `poc_price`;
3. value area bounds.
- Incremental reprocess allowed only for open windows.

## Observability

- `volume_profile_build_latency_ms{venue,instrument,timeframe}`
- `volume_profile_bucket_count{venue,instrument,timeframe}`
- `volume_profile_drop_total{reason}`
- `volume_profile_queue_depth{venue,instrument}`
- `volume_profile_replay_lag_ms{venue,instrument}`

Minimum:
- lag
- drop
- queue depth

## Acceptance Tests

Existing tests used as baseline:
- `internal/core/insights/app/join_crossvenue_trades_test.go:TestJoinCrossVenueTrades_GoldenDeterministicSnapshotAndSignalBytes_50Runs`
- `internal/shared/replay/golden_test.go:TestGoldenReplay`
- `internal/shared/config/loader_test.go:TestJoinEnabled_SubjectsPresent_Passes`
- `internal/shared/config/loader_test.go:TestJoinEnabled_MissingSubjects_Fails`

Tests to create for VPVR parity:
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRBucketDeterminism` (TODO)
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRCardinalityCap` (TODO)
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRPointOfControlConsistency` (TODO)
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRReplayGoldenWindow` (TODO)
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRBackpressureGracefulDegrade` (TODO)
- `internal/interfaces/ws/volume_profile_delivery_test.go:TestVPVRPayloadBudgetAndPagination` (TODO)

## Evidence Hooks

Current related evidence:
- `internal/core/insights/app/join_crossvenue_trades.go`
- `internal/core/insights/app/join_crossvenue_trades_test.go`
- `internal/shared/replay/golden_test.go`
- `internal/shared/config/loader.go`

TODO hooks (skeleton):
- `internal/core/insights/domain/volume_profile.go` (TODO)
- `internal/core/insights/app/build_volume_profile.go` (TODO)
- `internal/adapters/storage/timescale/volume_profile_writer.go` (TODO)
- `internal/adapters/storage/clickhouse/volume_profile_writer.go` (TODO)
- `internal/interfaces/ws/volume_profile_delivery_test.go` (TODO)

## Failure Modes

- Cardinality explosion from price regime shift:
  - Mitigation: bucket cap + larger bucket fallback.
- POC inconsistency from out-of-order data:
  - Mitigation: enforce `(ts_ingest,seq)` ordering + reject out-of-order.
- Peak-hour lag accumulation:
  - Mitigation: lower cadence + window flush policy.
- Oversized WS payload:
  - Mitigation: payload budget + temporal pagination.
