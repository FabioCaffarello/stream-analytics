# Feature Pack: Storage

## Purpose
- Keep storage authority explicit: L0 hot in-memory exists; L1/L2 remain planned.
- Bridge event-plane contracts to persistence boundaries and ack semantics.
- Keep parity planning auditable without claiming unimplemented adapters.

## Inputs/Outputs
- Authority: [`docs/contracts/event-bus.md`](../../../docs/contracts/event-bus.md), [`docs/architecture/storage.md`](../../../docs/architecture/storage.md), [`docs/adrs/ADR-0014-stream-partitioning-strategy.md`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).
- Inputs (existing subjects/events):
- `marketdata.trade.v1.{venue}.{instrument}`
- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`
- Outputs (existing/planned):
- `insights.crossvenue.trade_snapshot.v1.global.{instrument}` (existing)
- `insights.crossvenue.spread_signal.v1.global.{instrument}` (existing)
- `aggregation.snapshot.v1.{venue}.{instrument}` (planned)
- `insights.<heatmap_event>.v1.{venue}.{instrument}` (planned)
- `insights.<volume_profile_event>.v1.{venue}.{instrument}` (planned)

## Invariants
- Ack boundary is commit-based (`ack-on-commit`), not enqueue-based ([`ADR-0004`](../../../docs/adrs/ADR-0004-bus-nats-jetstream.md), [`ADR-0006`](../../../docs/adrs/ADR-0006-storage-hot-vs-cold.md)).
- Ordering inside partition follows `(ts_ingest, seq)` ([`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Subject and partition identity must remain deterministic ([`ADR-0014`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).
- Idempotency key is required and deterministic ([`ADR-0002`](../../../docs/adrs/ADR-0002-event-envelope-and-versioning.md)).

## Backpressure
- Bounded stages only; no silent unbounded queue in data path ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Drops/NAK/TERM must stay observable by reason ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Storage adapters (when implemented) must document overload action precedence per stage ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Replay
- Replay must preserve deterministic ordering and equivalent outputs ([`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Use replay package for fixture/golden flow: `internal/shared/replay/player.go`, `internal/shared/replay/sequencer.go`, `internal/shared/replay/golden_test.go`.
- Consumer replay baseline remains `cmd/consumer/replay_test.go` (`TestReplayIngestGolden1000`).

## Evidence Hooks
- `internal/core/aggregation/ports/ports.go`
- `internal/core/aggregation/app/update_orderbook.go`
- `internal/adapters/jetstream/consumer.go`
- `internal/adapters/jetstream/ingest_conformance_test.go`
- `internal/shared/replay/player.go`
- `internal/shared/replay/sequencer.go`
- TODO: `internal/adapters/storage/timescale/writer.go`
- TODO: `internal/adapters/storage/clickhouse/writer.go`

## Acceptance Tests
- `TestIngestConformance_AckNakTermGoldenTable` - `internal/adapters/jetstream/ingest_conformance_test.go`
- `TestGoldenReplay` - `internal/shared/replay/golden_test.go`
- `TestGoldenReplayByteStable50Runs` - `internal/shared/replay/golden_test.go`
- `TestReplayIngestGolden1000` - `cmd/consumer/replay_test.go`
- TODO: `TestStorageAckOnCommit_NotOnEnqueue` - `internal/adapters/storage/storage_integration_test.go`
- TODO: `TestStoragePoisonRoutesToQuarantine` - `internal/adapters/storage/storage_integration_test.go`
