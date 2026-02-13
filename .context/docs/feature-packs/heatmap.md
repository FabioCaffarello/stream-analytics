# Feature Pack: Heatmap

## Purpose
- Define deterministic bucketized heatmap contract for parity planning.
- Keep payload-budget and boundedness rules explicit for delivery safety.
- Anchor implementation status as TODO where runtime code is still missing.

## Inputs/Outputs
- Authority: [`docs/contracts/event-bus.md`](../../../docs/contracts/event-bus.md), [`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md), [`docs/architecture/heatmap.md`](../../../docs/architecture/heatmap.md), [`docs/adrs/ADR-0014-stream-partitioning-strategy.md`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).
- Inputs:
- `marketdata.bookdelta.v1.{venue}.{instrument}`
- `marketdata.trade.v1.{venue}.{instrument}`
- Outputs:
- `insights.<heatmap_event>.v1.{venue}.{instrument}` (planned contract token)
- WS stream: `insights.heatmap/{venue}/{symbol}/{timeframe}` (planned)

## Invariants
- Bucket assignment is deterministic for same tick size and inputs ([`docs/architecture/heatmap.md`](../../../docs/architecture/heatmap.md)).
- Closed windows are immutable after commit ([`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Subject taxonomy must stay valid under registered roots ([`ADR-0014`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).
- Payload/cardinality limits stay bounded for runtime safety ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Backpressure
- Use bounded queues and observable drops; no silent loss ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade by coarsening buckets and lowering cadence under pressure ([`docs/architecture/heatmap.md`](../../../docs/architecture/heatmap.md)).
- Prefer keep-latest behavior for stale non-critical frames ([`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md)).

## Replay
- Replay invariants follow [`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Reuse `internal/shared/replay` for deterministic input ordering and golden checks.
- Heatmap parity closes when window hash stability is testable across repeated runs.

## Evidence Hooks
- `internal/adapters/jetstream/subject_validation.go`
- `internal/actors/marketdata/runtime/backpressure_queue.go`
- `internal/shared/replay/player.go`
- TODO: `internal/core/insights/domain/heatmap_bucket.go`
- TODO: `internal/core/insights/app/build_heatmap.go`
- TODO: `internal/adapters/storage/timescale/heatmap_writer.go`
- TODO: `internal/interfaces/ws/heatmap_delivery_test.go`

## Acceptance Tests
- `TestValidateSubjectTaxonomy_Valid` - `internal/adapters/jetstream/subject_validation_test.go`
- `TestValidateSubjectPattern_InvalidRootFailsFast` - `internal/adapters/jetstream/subject_validation_test.go`
- `TestGoldenReplay` - `internal/shared/replay/golden_test.go`
- `TestSubsystem_WsMessage_nilParseFn_dropsMessage` - `internal/actors/marketdata/runtime/subsystem_test.go`
- TODO: `TestHeatmapBucketizationDeterministic` - `internal/core/insights/app/build_heatmap_test.go`
- TODO: `TestHeatmapPayloadBudgetHardCap` - `internal/core/insights/app/build_heatmap_test.go`
- TODO: `TestHeatmapReplayGoldenMatrixHash` - `internal/core/insights/app/build_heatmap_test.go`
