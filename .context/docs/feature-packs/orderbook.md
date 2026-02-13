# Feature Pack: Orderbook

## Purpose
- Deterministic orderbook constraints only; authority: [orderbook](../../../docs/architecture/orderbook.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0005](../../../docs/adrs/ADR-0005-sequencing-and-time-normalization.md).

## Inputs-Outputs
- Inputs: `marketdata.bookdelta.v1.{venue}.{instrument}`; optional context `marketdata.trade.v1.{venue}.{instrument}`.
- Outputs: planned `aggregation.snapshot.v1.{venue}.{instrument}`, planned `aggregation.orderbook_inconsistency.v1.{venue}.{instrument}`.
- Partition/subject refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md), [delivery-ws](../../../docs/contracts/delivery-ws.md).

## Invariants
- Seq monotonicity per partition is mandatory ([ADR-0005](../../../docs/adrs/ADR-0005-sequencing-and-time-normalization.md)).
- Subject identity and partitioning remain deterministic ([ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).
- Crossed book cannot be committed as healthy snapshot ([orderbook](../../../docs/architecture/orderbook.md)).
- Replay on equal fixture must be stable ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).

## Backpressure
- Bounded queue and bounded levels per book ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Under pressure, preserve ordering integrity before depth completeness ([orderbook](../../../docs/architecture/orderbook.md)).

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden replay requirements: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/core/aggregation/domain/orderbook.go:132`
- `internal/core/aggregation/app/update_orderbook.go:101`
- `internal/actors/aggregation/runtime/processor.go:225`
- `internal/core/aggregation/app/golden_replay_test.go:40`
- TODO: `internal/adapters/storage/timescale/orderbook_snapshot_writer.go`

## Acceptance Tests
- `TestOrderBook_applyDelta_seqMonotonic` -> `internal/core/aggregation/domain/orderbook_test.go:56`
- `TestOrderBook_crossedBook` -> `internal/core/aggregation/domain/orderbook_test.go:107`
- `TestOrderBook_maxLevelsBoundedPerSide` -> `internal/core/aggregation/domain/orderbook_test.go:221`
- `TestUpdateOrderBook_crossedBook` -> `internal/core/aggregation/app/update_orderbook_test.go:110`
- `TestAggregationGoldenReplayFromFixture` -> `internal/core/aggregation/app/golden_replay_test.go:40`
- `TestProcessor_BookDelta_callsUpdateOrderBook` -> `internal/actors/aggregation/runtime/processor_test.go:186`
- TODO: `TestOrderbookSnapshotWriter_IdempotentUpsert` -> `internal/adapters/storage/orderbook_snapshot_writer_test.go`
