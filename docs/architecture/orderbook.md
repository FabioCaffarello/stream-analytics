# Orderbook Architecture (Snapshots + Delivery)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0005-sequencing-and-time-normalization.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`

## Purpose

Define the orderbook state contract, snapshot generation, and downstream delivery while preserving per-instrument determinism and bounded behavior under burst.

## Terminology (canonical)

- `instrument`: canonical envelope key (`BTCUSDT`).
- `symbol`: delivery-side token on WS subjects.
- `subject`: bus key (`marketdata.bookdelta.v1.binance.BTCUSDT`).
- `stream`: JetStream filter/publish patterns validated by taxonomy guards.
- `envelope`: ADR-0002 wrapper carrying sequencing and idempotency.
- `payload`: typed body (`marketdata.BookDeltaV1` for inputs).

## Data Planes

### Inputs

- `marketdata.bookdelta.v1.{venue}.{instrument}` (primary)
- `marketdata.trade.v1.{venue}.{instrument}` (optional microstructure context)

### Outputs

- Planned derived event: `aggregation.snapshot.v1.{venue}.{instrument}` (tracked in ADR revision NOTE-001 for subject-root alignment)
- Planned derived event: `aggregation.<orderbook_inconsistency>.v1.{venue}.{instrument}` (TBD registry key, same NOTE-001)
- WS delivery stream: `<stream_type>/<venue>/<symbol>/<timeframe>` with `stream_type=aggregation.snapshot`

### Storage

Hot:
- `timescale.aggregation_orderbook_snapshot_hot`

Cold:
- `clickhouse.aggregation_orderbook_snapshot_cold`

Keys/idempotency:
- Logical PK: `(venue, instrument, seq, snapshot_version)`
- Additional dedup: `idempotency_key`

## Contracts

Current runtime domain contracts:
- `SnapshotProduced` (`internal/core/aggregation/domain/events.go`)
- `OrderBookInconsistentDetected` (`internal/core/aggregation/domain/events.go`)

Planned bus contract (v1 minimum):
- `venue`, `instrument`, `seq`, `ts_ingest`
- `best_bid`, `best_ask`, `spread`
- `bids[]`, `asks[]` truncated by `max_levels`
- deterministic orderbook `checksum`

Planned inconsistency contract:
- mandatory `reason` (`crossed_book`, `sequence_gap`, `invalid_level`)
- `needs_resync=true`

## Invariants

- `OB-1`: single writer per partition (`venue`, `instrument`, and `market_type` when present in upstream stream identity).
- `OB-2`: strict seq monotonicity per stream.
- `OB-3`: book cannot be committed with bid > ask; if it happens, emit inconsistency and block snapshot.
- `OB-4`: `max_levels` bounded in memory and payload.
- `OB-5`: snapshot output must be byte-stable for same replay input.

## Backpressure

- Bounded delta queue per instrument.
- Under saturation:
1. preserve ordering integrity over level completeness;
2. request snapshot resync on detected gaps;
3. throttle publish rate per window.
- ACK policy: `ACK` only after `ApplyDelta + hot persist` is complete.

## Storage Strategy

- Hot (Timescale): latest snapshot + short range for `getrange`.
- Cold (ClickHouse): compacted snapshot history by time bucket.
- Current runtime: in-memory hot read model (`HotReadModelStore`) is the only implemented store.
- Write strategy:
1. hot upsert by `(venue,instrument,seq)`;
2. cold append ordered by `(ts_ingest, seq)`.

## Replay Strategy

- Rebuild from `marketdata.bookdelta` replay with deterministic empty seed.
- On `OrderBookInconsistentDetected`, mark window as resync-required.
- Golden replay compares:
1. top-of-book;
2. checksum;
3. level counts.

## Observability

- `orderbook_apply_latency_ms{venue,instrument}`
- `orderbook_crossed_total{venue,instrument}`
- `orderbook_resync_required_total{venue,instrument}`
- `orderbook_snapshot_publish_total{status}`
- `orderbook_queue_depth{venue,instrument}`

Operational minimum:
- lag per partition
- drops
- queue depth

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Deterministic `ApplyDelta` and crossed-book detection | Existing | `internal/core/aggregation/domain/orderbook.go` | `internal/core/aggregation/domain/orderbook_test.go` |
| Orderbook use case publishes snapshot + inconsistency | Existing | `internal/core/aggregation/app/update_orderbook.go` | `internal/core/aggregation/app/update_orderbook_test.go` |
| Replay golden for aggregation snapshots | Existing | `internal/core/aggregation/app/golden_replay_test.go` | `internal/core/aggregation/app/golden_replay_test.go:TestAggregationGoldenReplayFromFixture` |
| Processor routing `marketdata.bookdelta` -> aggregation | Existing | `internal/actors/aggregation/runtime/processor.go` | `internal/actors/aggregation/runtime/processor_test.go:TestProcessor_BookDelta_callsUpdateOrderBook` |
| Durable hot/cold writers for orderbook | TODO | `internal/adapters/storage/timescale/` and `internal/adapters/storage/clickhouse/` (TODO) | `internal/adapters/storage/orderbook_snapshot_writer_test.go` (TODO) |
| Bus contract for `aggregation.snapshot` | Planned | `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` | `internal/adapters/jetstream/subject_validation_test.go` (root-rule alignment required) |

## Acceptance Tests

Existing tests:
- `internal/core/aggregation/domain/orderbook_test.go:TestOrderBook_applyDelta_seqMonotonic`
- `internal/core/aggregation/domain/orderbook_test.go:TestOrderBook_crossedBook`
- `internal/core/aggregation/domain/orderbook_test.go:TestOrderBook_maxLevelsBoundedPerSide`
- `internal/core/aggregation/app/update_orderbook_test.go:TestUpdateOrderBook_crossedBook`
- `internal/core/aggregation/app/update_orderbook_test.go:TestUpdateOrderBook_boundedBooksEvictionDeterministicVictim`
- `internal/core/aggregation/app/golden_replay_test.go:TestAggregationGoldenReplayFromFixture`
- `internal/actors/aggregation/runtime/processor_test.go:TestProcessor_BookDelta_callsUpdateOrderBook`

Tests to create (contract/storage/delivery parity):
- `internal/actors/aggregation/runtime/processor_test.go:TestProcessor_BookDelta_AckOnCommitBoundary` (TODO)
- `internal/adapters/storage/orderbook_snapshot_writer_test.go:TestOrderbookSnapshotWriter_IdempotentUpsert` (TODO)
- `internal/interfaces/ws/orderbook_delivery_contract_test.go:TestOrderbookDeliverySlowClientPolicy` (TODO)
- `internal/interfaces/ws/orderbook_delivery_contract_test.go:TestOrderbookDeliveryReplayRangeDeterministic` (TODO)

## Evidence Hooks

Current evidence:
- `internal/core/aggregation/domain/orderbook.go`
- `internal/core/aggregation/app/update_orderbook.go`
- `internal/core/aggregation/app/update_orderbook_test.go`
- `internal/core/aggregation/app/golden_replay_test.go`
- `internal/actors/aggregation/runtime/processor.go`

TODO hooks (skeleton):
- `internal/core/aggregation/domain/snapshot_contract.go` (TODO)
- `internal/core/aggregation/app/publish_orderbook_snapshot.go` (TODO)
- `internal/adapters/storage/timescale/orderbook_snapshot_writer.go` (TODO)
- `internal/adapters/storage/clickhouse/orderbook_snapshot_writer.go` (TODO)
- `internal/interfaces/ws/orderbook_delivery_contract_test.go` (TODO)

## Failure Modes

- Sequence gap: invalid state risk.
  - Mitigation: set `needs_resync`, emit inconsistency event, pause incremental emission.
- Persistent crossed book: false signal risk.
  - Mitigation: instrument quarantine until snapshot refresh.
- Slow hot writer: delayed ack risk.
  - Mitigation: bounded queue + throttle + lag alert.
- Slow WS clients: delivery pressure risk.
  - Mitigation: per-session drop policy + controlled disconnect.
