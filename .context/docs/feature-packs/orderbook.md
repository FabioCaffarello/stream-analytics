# Feature Pack: Orderbook

## Purpose
- Define deterministic snapshot/inconsistency behavior per instrument partition.
- Keep orderbook parity scoped to validated runtime behavior plus explicit TODOs.
- Bridge domain rules to delivery expectations without bypassing contracts.

## Inputs/Outputs
- Authority: [`docs/contracts/event-bus.md`](../../../docs/contracts/event-bus.md), [`docs/architecture/orderbook.md`](../../../docs/architecture/orderbook.md), [`docs/adrs/ADR-0014-stream-partitioning-strategy.md`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).
- Inputs:
- `marketdata.bookdelta.v1.{venue}.{instrument}` (primary)
- `marketdata.trade.v1.{venue}.{instrument}` (optional context)
- Outputs:
- `aggregation.snapshot.v1.{venue}.{instrument}` (planned contract)
- `aggregation.<orderbook_inconsistency>.v1.{venue}.{instrument}` (planned contract)
- WS stream: `aggregation.snapshot/{venue}/{symbol}/{timeframe}`

## Invariants
- Seq monotonicity per stream partition must hold ([`ADR-0005`](../../../docs/adrs/ADR-0005-sequencing-and-time-normalization.md)).
- Subject/partition identity must stay deterministic ([`ADR-0014`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).
- Crossed book cannot be committed as valid snapshot ([`docs/architecture/orderbook.md`](../../../docs/architecture/orderbook.md)).
- Replay on same fixture must produce byte-stable snapshots ([`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).

## Backpressure
- Apply bounded queue and bounded levels per book ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Prefer ordering integrity over level completeness under overload ([`docs/architecture/orderbook.md`](../../../docs/architecture/orderbook.md)).
- Keep commit-based ack boundary when wiring durable publish/store ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Replay
- Deterministic replay authority is [`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Reuse replay package (`internal/shared/replay/*`) plus aggregation golden coverage.
- Validate top-of-book and snapshot determinism with `internal/core/aggregation/app/golden_replay_test.go`.

## Evidence Hooks
- `internal/core/aggregation/domain/orderbook.go`
- `internal/core/aggregation/app/update_orderbook.go`
- `internal/core/aggregation/app/golden_replay_test.go`
- `internal/actors/aggregation/runtime/processor.go`
- TODO: `internal/adapters/storage/timescale/orderbook_snapshot_writer.go`
- TODO: `internal/adapters/storage/clickhouse/orderbook_snapshot_writer.go`

## Acceptance Tests
- `TestOrderBook_applyDelta_seqMonotonic` - `internal/core/aggregation/domain/orderbook_test.go`
- `TestOrderBook_crossedBook` - `internal/core/aggregation/domain/orderbook_test.go`
- `TestOrderBook_maxLevelsBoundedPerSide` - `internal/core/aggregation/domain/orderbook_test.go`
- `TestUpdateOrderBook_crossedBook` - `internal/core/aggregation/app/update_orderbook_test.go`
- `TestAggregationGoldenReplayFromFixture` - `internal/core/aggregation/app/golden_replay_test.go`
- `TestProcessor_BookDelta_callsUpdateOrderBook` - `internal/actors/aggregation/runtime/processor_test.go`
- TODO: `TestOrderbookSnapshotWriter_IdempotentUpsert` - `internal/adapters/storage/orderbook_snapshot_writer_test.go`
