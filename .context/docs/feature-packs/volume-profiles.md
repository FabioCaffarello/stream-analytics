# Feature Pack: Volume Profiles

## Purpose
- Volume-profile constraints only; authority: [volume-profiles](../../../docs/architecture/volume-profiles.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md).

## Inputs-Outputs
- Inputs: `marketdata.trade.v1.{venue}.{instrument}`; optional `marketdata.bookdelta.v1.{venue}.{instrument}`.
- Outputs: planned `insights.volume_profile_snapshot.v1.{venue}.{instrument}`, planned `insights.volume_profile_delta.v1.{venue}.{instrument}`, planned WS `insights.volume_profile/{venue}/{symbol}/{timeframe}`.
- Subject/version refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md), [delivery-ws](../../../docs/contracts/delivery-ws.md).

## Invariants
- Volume additivity with non-negative aggregates ([volume-profiles](../../../docs/architecture/volume-profiles.md)).
- Deterministic bucket assignment for equal input sequence ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Canonical instrument identity preserved cross-venue ([ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md)).

## Backpressure
- Bounded queue per instrument/timeframe with explicit overload behavior ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade by coarser buckets/cadence before dropping state ([volume-profiles](../../../docs/architecture/volume-profiles.md)).

## Replay
- Replay invariants source: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden replay reference: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/core/insights/app/join_crossvenue_trades.go:161`
- `internal/core/insights/app/join_crossvenue_trades.go:291`
- `internal/adapters/jetstream/subject_validation.go:24`
- `internal/shared/replay/golden_test.go:18`
- TODO: `internal/core/insights/app/build_volume_profile.go`

## Acceptance Tests
- `TestJoinCrossVenueTrades_DeterministicGivenSameInputSequence` -> `internal/core/insights/app/join_crossvenue_trades_test.go:292`
- `TestJoinCrossVenueTrades_GoldenDeterministicSnapshotAndSignalBytes_50Runs` -> `internal/core/insights/app/join_crossvenue_trades_test.go:529`
- `TestJoinEnabled_SubjectsPresent_Passes` -> `internal/shared/config/loader_test.go:627`
- `TestJoinEnabled_MissingSubjects_Fails` -> `internal/shared/config/loader_test.go:610`
- TODO: `TestVPVRBucketDeterminism` -> `internal/core/insights/app/build_volume_profile_test.go`
