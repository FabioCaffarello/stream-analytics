# Feature Pack: Volume Profiles

**STATUS:** ACTIVE | **last_reviewed:** 2026-02-17

## Purpose
- VPVR constraints only; authority: [volume-profiles](../../../docs/architecture/volume-profiles.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md).

## Inputs/Outputs
- Inputs (sole volume authority): `marketdata.trade.v1.{venue}.{instrument}`.
- Inputs (context-only, NOT volume source): `marketdata.bookdelta.v1.{venue}.{instrument}`.
- Outputs (planned, not in event-bus.md matrix): `insights.volume_profile_snapshot.v1.{venue}.{instrument}`, `insights.volume_profile_delta.v1.{venue}.{instrument}`.
- Planned WS: `insights.volume_profile/{venue}/{symbol}/{timeframe}` ([delivery-ws](../../../docs/contracts/delivery-ws.md)).
- Subject refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).

## Invariants
- Volume additivity: non-negative aggregates only ([volume-profiles](../../../docs/architecture/volume-profiles.md) VP-1).
- Deterministic binning: same `tick_size` + input → same buckets ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md) VP-2). No floating-precision buckets.
- Bounded price bands: hard cap on bucket count per `(venue,instrument,timeframe)` ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md) VP-4). No dynamic bucket creation beyond cap.
- Window policy: time-based only; trade-count windows prohibited in v1 ([volume-profiles](../../../docs/architecture/volume-profiles.md)).
- Replay rebuild: same window fixture → identical snapshot including `poc_price` (VP-5).
- Storage: flat columnar schema — no nested arrays, ClickHouse-friendly ([ADR-0006](../../../docs/adrs/ADR-0006-storage-hot-vs-cold.md)).
- Cross-venue identity: canonical instrument preserved ([ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md)).

## Backpressure
- Bounded queue per instrument/timeframe ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade: coarser buckets → reduced cadence → prioritize window close ([volume-profiles](../../../docs/architecture/volume-profiles.md)).

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/core/insights/app/join_crossvenue_trades.go:161` (Execute — closest existing usecase)
- `internal/core/insights/app/join_crossvenue_trades.go:291` (buildSnapshot — deterministic assembly)
- `internal/adapters/jetstream/subject_validation.go:24` (ValidateSubjectTaxonomy — input gate)
- `internal/shared/ds/boundedmap.go:24` (BoundedMap — cardinality cap primitive)
- TODO: `internal/core/insights/domain/volume_profile.go` (VPVR domain model)
- TODO: `internal/core/insights/app/build_volume_profile.go` (builder usecase)

## Acceptance Tests
- `TestJoinCrossVenueTrades_DeterministicGivenSameInputSequence` -> `internal/core/insights/app/join_crossvenue_trades_test.go:292`
- `TestJoinCrossVenueTrades_GoldenDeterministicSnapshotAndSignalBytes_50Runs` -> `internal/core/insights/app/join_crossvenue_trades_test.go:529`
- `TestJoinEnabled_MissingSubjects_Fails` -> `internal/shared/config/loader_test.go:610`
- `TestJoinEnabled_SubjectsPresent_Passes` -> `internal/shared/config/loader_test.go:627`
- `TestGoldenReplay` -> `internal/shared/replay/golden_test.go:18`
- TODO: `TestVPVRBucketDeterminism` -> `internal/core/insights/app/build_volume_profile_test.go`
- TODO: `TestVPVRReplayRebuildEquivalence` -> `internal/core/insights/app/build_volume_profile_test.go`
- TODO: `TestVPVRBoundedBucketsPerWindow` -> `internal/core/insights/app/build_volume_profile_test.go`
- TODO: `TestVPVRStableSerialization` -> `internal/core/insights/app/build_volume_profile_test.go`
