# Feature Pack: Heatmap

## Purpose
- Heatmap parity constraints only; authority: [heatmap](../../../docs/architecture/heatmap.md), [event-bus](../../../docs/contracts/event-bus.md), [delivery-ws](../../../docs/contracts/delivery-ws.md).

## Inputs-Outputs
- Inputs: `marketdata.bookdelta.v1.{venue}.{instrument}`, `marketdata.trade.v1.{venue}.{instrument}`.
- Outputs: planned `insights.heatmap_snapshot.v1.{venue}.{instrument}`, planned WS `insights.heatmap/{venue}/{symbol}/{timeframe}`.
- Subject root refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).

## Invariants
- Bucket assignment must be deterministic for equal tick and sequence ([heatmap](../../../docs/architecture/heatmap.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Closed windows are immutable once emitted ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Payload cardinality remains bounded ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Backpressure
- Bounded queue with observable drop reason only ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade via coarser bucket/cadence before drop ([heatmap](../../../docs/architecture/heatmap.md)).

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/adapters/jetstream/subject_validation.go:24`
- `internal/actors/marketdata/runtime/backpressure_queue.go:56`
- `internal/actors/marketdata/runtime/subsystem_test.go:184`
- `internal/shared/replay/player.go:45`
- TODO: `internal/core/insights/app/build_heatmap.go`

## Acceptance Tests
- `TestValidateSubjectTaxonomy_Valid` -> `internal/adapters/jetstream/subject_validation_test.go:5`
- `TestValidateSubjectPattern_InvalidRootFailsFast` -> `internal/adapters/jetstream/subject_validation_test.go:35`
- `TestSubsystem_WsMessage_nilParseFn_dropsMessage` -> `internal/actors/marketdata/runtime/subsystem_test.go:184`
- `TestGoldenReplay` -> `internal/shared/replay/golden_test.go:18`
- TODO: `TestHeatmapBucketizationDeterministic` -> `internal/core/insights/app/build_heatmap_test.go`
