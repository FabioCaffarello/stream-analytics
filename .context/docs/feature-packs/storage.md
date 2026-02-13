# Feature Pack: Storage

## Purpose
- Storage constraints and bridge only; authority: [storage](../../../docs/architecture/storage.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0006](../../../docs/adrs/ADR-0006-storage-hot-vs-cold.md).

## Inputs/Outputs
- Inputs: `marketdata.trade.v1.{venue}.{instrument}`, `marketdata.bookdelta.v1.{venue}.{instrument}`, `marketdata.markprice.v1.{venue}.{instrument}`, `marketdata.liquidation.v1.{venue}.{instrument}`.
- Outputs (runtime, not yet in event-bus.md matrix): `insights.crossvenue.trade_snapshot.v1.global.{instrument}`, `insights.crossvenue.spread_signal.v1.global.{instrument}`.
- Outputs (planned): `aggregation.snapshot.v1.{venue}.{instrument}`.
- Contract refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md), [ADR-0002](../../../docs/adrs/ADR-0002-event-envelope-and-versioning.md).

## Invariants
- Ack boundary is commit-only (`ack-on-commit`), never enqueue ([ADR-0004](../../../docs/adrs/ADR-0004-bus-nats-jetstream.md), [ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Per-partition ordering remains deterministic by `(ts_ingest, seq)` ([ADR-0005](../../../docs/adrs/ADR-0005-sequencing-and-time-normalization.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Idempotency key is mandatory and deterministic ([ADR-0002](../../../docs/adrs/ADR-0002-event-envelope-and-versioning.md)).

## Backpressure
- Bounded stages only; drops/NAK/TERM must be observable with reason ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Overload precedence must be declared before enabling new storage adapters ([storage](../../../docs/architecture/storage.md)).

## Replay
- Equal fixtures must yield equivalent outputs and order ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Replay baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/adapters/jetstream/consumer.go:67` (NewConsumer — ack boundary entry)
- `internal/adapters/jetstream/ingest_policy.go:59` (ClassifyIngestError — ack/nak/term)
- `internal/core/aggregation/ports/ports.go:17` (HotReadModelStore interface)
- `internal/core/aggregation/app/update_orderbook.go:141` (hotStore.Save call-site)
- `internal/shared/envelope/subject.go:9` (canonical subject derivation)
- `internal/shared/replay/player.go:45` (Replay entry)
- `internal/shared/replay/sequencer.go:32` (Enqueue — deterministic seq)
- TODO: `internal/adapters/storage/timescale/writer.go`

## Acceptance Tests
- `TestIngestConformance_AckNakTermGoldenTable` -> `internal/adapters/jetstream/ingest_conformance_test.go:15`
- `TestGoldenReplay` -> `internal/shared/replay/golden_test.go:18`
- `TestGoldenReplayByteStable50Runs` -> `internal/shared/replay/golden_test.go:42`
- `TestReplayIngestGolden1000` -> `cmd/consumer/replay_test.go:63`
- TODO: `TestStorageAckOnCommit_NotOnEnqueue` -> `internal/adapters/storage/storage_integration_test.go`
