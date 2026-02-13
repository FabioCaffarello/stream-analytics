# Storage Architecture (Hot/Cold per BC)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md`

## Purpose

Define doc-first storage architecture for parity v1 with MarketMonkey, separating:
- hot operational path per bounded context (Timescale);
- cold analytical path (ClickHouse);
- current in-memory hot path for ultra-low latency delivery.

## Data Planes

### Plane A: Event Plane (input)

Canonical input events (current authority in `proto/registry.json`):
- `marketdata.trade.v1.{venue}.{instrument}`
- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`

Existing derived events (registry/runtime):
- `insights.crossvenue.trade_snapshot.v1.global.{instrument}`
- `insights.crossvenue.spread_signal.v1.global.{instrument}`

Planned derived events for storage fanout (no implementation in this cycle):
- `aggregation.snapshot.v1.{venue}.{instrument}` (orderbook snapshot)
- `insights.heatmap.bucket.v1.{venue}.{instrument}`
- `insights.volume_profile.snapshot.v1.{venue}.{instrument}`

### Plane B: Hot Storage Plane (Timescale)

Goal: fast API/range reads, short to medium retention, idempotent upsert.

Proposed tables per BC:
- `timescale.marketdata_ticks_hot`
- `timescale.aggregation_orderbook_snapshot_hot`
- `timescale.insights_heatmap_bucket_hot`
- `timescale.insights_volume_profile_hot`
- `timescale.marketdata_markprice_hot`
- `timescale.marketdata_liquidation_hot`

Standard idempotency key:
- `(event_type, version, venue, instrument, seq, idempotency_key)`

### Plane C: Cold Storage Plane (ClickHouse)

Goal: long-term history, analytics, backfill, auditable replay.

Proposed tables per BC:
- `clickhouse.marketdata_ticks_cold`
- `clickhouse.aggregation_orderbook_snapshot_cold`
- `clickhouse.insights_heatmap_bucket_cold`
- `clickhouse.insights_volume_profile_cold`
- `clickhouse.marketdata_markprice_cold`
- `clickhouse.marketdata_liquidation_cold`

Recommended partitioning:
- `toDate(ts_ingest)` + bucketing by `(venue, instrument)`.

## Contracts

- Mandatory envelope fields: `type`, `version`, `venue`, `instrument`, `seq`, `idempotency_key`, `payload`.
- Canonical subject: `{event}.v{version}.{venue_lower}.{instrument_alnum_upper}`.
- Ack semantics for persistence:
1. `ACK` only after commit on required destination.
2. `NAK` for transient failure (timeout/network).
3. `TERM` for poison/invalid contract and route to `quarantine.v1.*`.

## Invariants

- `STO-1`: single writer per `(venue, instrument, event_type)` in each persistence stage.
- `STO-2`: bounded queues in all writers (no unbounded path).
- `STO-3`: strong idempotency by `idempotency_key` + `seq`.
- `STO-4`: deterministic ordering preserved by `(venue, instrument)` partition.
- `STO-5`: replay over same input must produce same hot/cold artifacts.

## Backpressure

- Bounded writer queues per BC (`queue_depth_max` per worker).
- Policy:
1. throttle per partition when `queue_depth > 80%`;
2. NAK with exponential jitter when destination is unavailable;
3. TERM + quarantine for poison payload.
- Never ACK on enqueue; ACK only on persisted commit.

## Storage Strategy

- Three-layer strategy:
1. L0 (memory): ultra-low latency read model for live delivery.
2. L1 (Timescale): durable hot layer for short/medium window queries.
3. L2 (ClickHouse): durable cold layer for long analytics and replay.
- L1 -> L2 promotion is asynchronous and idempotent.

## Replay Strategy

- Primary source: existing fixture/JetStream replay stack (`internal/shared/replay`).
- Secondary source: ClickHouse reprocessing by time partition.
- Deterministic rule:
1. rehydrate by `ts_ingest, seq`;
2. rerun aggregators;
3. validate artifact checksum per window.

## Observability

Minimum required:
- `storage_writer_queue_depth{bc,plane}`
- `storage_write_latency_ms{bc,plane}`
- `storage_commit_total{bc,plane,status}`
- `storage_drop_total{bc,plane,reason}`
- `storage_replay_lag_ms{bc}`

Initial SLO:
- p95 hot write < 150ms
- p99 cold write < 2s
- drop rate < 0.01% per 5m window

## Acceptance Tests

Planned test names:
- `TestStorageHotIdempotencyByIdempotencyKey`
- `TestStorageColdUpsertDeterministicReplay`
- `TestStorageAckOnCommit_NotOnEnqueue`
- `TestStorageBackpressureNAKAndJitterPolicy`
- `TestStoragePoisonRoutesToQuarantine`
- `TestStorageSingleWriterPerInstrument`

Minimum scenarios:
- duplicate `idempotency_key` does not create duplicate rows;
- transient failure triggers `NAK` and jittered retry;
- poison payload triggers `TERM` + quarantine;
- replay twice yields same output table checksum.

## Evidence Hooks

Current evidence:
- `internal/core/aggregation/ports/ports.go`
- `internal/shared/replay/player.go`
- `internal/adapters/jetstream/consumer.go`
- `internal/adapters/jetstream/ingest_conformance_test.go`

TODO hooks (skeleton only, no implementation in this cycle):
- `internal/core/storage/ports/ports.go` (TODO)
- `internal/core/storage/app/persist_hot_path.go` (TODO)
- `internal/core/storage/app/persist_cold_path.go` (TODO)
- `internal/adapters/storage/timescale/writer.go` (TODO)
- `internal/adapters/storage/clickhouse/writer.go` (TODO)
- `internal/adapters/storage/replay/rebuilder.go` (TODO)
- `internal/adapters/storage/storage_integration_test.go` (TODO)

## Failure Modes

- Hot DB unavailable: lag growth risk.
  - Mitigation: NAK + retry budget + queue depth alert.
- Cold DB unavailable: long-history loss risk.
  - Mitigation: bounded temporary buffer + idempotent batch redrive.
- Poison payload: infinite loop risk.
  - Mitigation: TERM + `quarantine.v1.*` + operational DLQ.
- Premature ack (enqueue): silent loss risk.
  - Mitigation: explicit `ack-on-commit` policy.
- Partition skew: hotspot risk.
  - Mitigation: sharding by `(venue,instrument)` + per-writer limits.
