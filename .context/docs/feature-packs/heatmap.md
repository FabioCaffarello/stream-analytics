# Feature Pack: Heatmap

**STATUS:** ACTIVE | **last_reviewed:** 2026-02-18

## Purpose
- Heatmap parity constraints only; authority: [heatmap](../../../docs/architecture/heatmap.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md).

## Inputs/Outputs
- Inputs: `marketdata.bookdelta.v1.{venue}.{instrument}`, `marketdata.trade.v1.{venue}.{instrument}`.
- Outputs (planned): `insights.heatmap_snapshot.v1.{venue}.{instrument}`, `insights.heatmap_delta.v1.{venue}.{instrument}`.
- Planned WS: `insights.heatmap/{venue}/{symbol}/{timeframe}` ([delivery-ws](../../../docs/contracts/delivery-ws.md)).
- Subject refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).

## Invariants
- Bucket assignment deterministic for same `tick_size` + input ([heatmap](../../../docs/architecture/heatmap.md) HM-1, [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Closed windows immutable after commit ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md) HM-2).
- Bounded resolution: hard cap per `(venue,instrument,timeframe)` ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md) HM-4).
- Caps v1: `max_price_buckets_per_window=512`, `max_size_buckets=5`, `max_cells_per_window=2048`, `max_open_windows_per_partition=2`.
- Replay of same fixture yields same matrix values and ordering (HM-5).
- Payload budget: `max_cells_per_frame` + `max_payload_bytes` ([heatmap](../../../docs/architecture/heatmap.md)).

## Backpressure
- Bounded queue with observable drop reason ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade: coarser bucket → reduced cadence → top-N cells only ([heatmap](../../../docs/architecture/heatmap.md)).

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/adapters/jetstream/subject_validation.go:24` (ValidateSubjectTaxonomy — input subject gate)
- `internal/actors/marketdata/runtime/backpressure_queue.go:56` (wsQueue.Enqueue — bounded queue)
- `internal/shared/ds/boundedmap.go:24` (BoundedMap — bounded state primitive)
- `internal/shared/replay/player.go:45` (Replay — deterministic replay entry)
- `internal/core/insights/domain/heatmap_bucket.go:1` (bucket model — Existing)
- `internal/core/insights/app/build_heatmap.go:1` (builder usecase — Existing)
- `internal/core/insights/app/service.go:1` (InsightsService facade — Existing)

## Acceptance Tests
- `TestValidateSubjectTaxonomy_Valid` -> `internal/adapters/jetstream/subject_validation_test.go:5`
- `TestSubsystem_WsMessage_nilParseFn_dropsMessage` -> `internal/actors/marketdata/runtime/subsystem_test.go:184`
- `TestGoldenReplay` -> `internal/shared/replay/golden_test.go:18`
- TODO: `TestHeatmapBucketDeterministic` -> `internal/core/insights/app/build_heatmap_test.go`
- TODO: `TestHeatmapReplayRebuildEquivalence` -> `internal/core/insights/app/build_heatmap_test.go`
- TODO: `TestHeatmapBoundedBucketsPerPartition` -> `internal/core/insights/app/build_heatmap_test.go`
- TODO: `TestHeatmapPayloadSizeBudget` -> `internal/core/insights/app/build_heatmap_test.go`
