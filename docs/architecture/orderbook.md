# Orderbook Architecture (Snapshots + Delivery)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0005-sequencing-and-time-normalization.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`

## Purpose

Define the orderbook state contract, snapshot generation, and downstream delivery while preserving per-instrument determinism and bounded behavior under burst.

## Data Planes

### Inputs

- `marketdata.bookdelta.v1.{venue}.{instrument}` (primary)
- `marketdata.trade.v1.{venue}.{instrument}` (optional microstructure context)

### Outputs

- Planned derived event: `aggregation.snapshot.v1.{venue}.{instrument}`
- Planned derived event: `aggregation.orderbook.inconsistent.v1.{venue}.{instrument}`
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

Planned snapshot payload v1 minimum:
- `venue`, `instrument`, `seq`, `ts_ingest`
- `best_bid`, `best_ask`, `spread`
- `bids[]`, `asks[]` truncated by `max_levels`
- deterministic orderbook `checksum`

Inconsistency contract:
- mandatory `reason` (`crossed_book`, `sequence_gap`, `invalid_level`)
- `needs_resync=true`

## Invariants

- `OB-1`: single writer per `(venue, instrument, market_type)`.
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

## Acceptance Tests

Planned test names:
- `TestOrderbookApplyDeltaMonotonicSeq`
- `TestOrderbookCrossedBookTriggersInconsistentEvent`
- `TestOrderbookSnapshotBoundedByMaxLevels`
- `TestOrderbookReplayGoldenDeterministic`
- `TestOrderbookAckOnCommit`
- `TestOrderbookSoak_NoGoroutineLeak`

Scenarios:
- out-of-order sequence;
- crossed-book delta;
- level burst above budget;
- replay of 1000+ events with stable checksum.

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
