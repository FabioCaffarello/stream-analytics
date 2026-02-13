# Feature Pack: Volume Profiles

## Purpose
- Define minimal VPVR contract scope with deterministic aggregation rules.
- Keep cardinality and payload-risk controls explicit before runtime expansion.
- Tie parity planning to existing cross-venue insights evidence.

## Inputs/Outputs
- Authority: [`docs/contracts/event-bus.md`](../../../docs/contracts/event-bus.md), [`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md), [`docs/architecture/volume-profiles.md`](../../../docs/architecture/volume-profiles.md), [`docs/adrs/ADR-0017-multi-exchange-normalization.md`](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md).
- Inputs:
- `marketdata.trade.v1.{venue}.{instrument}` (primary)
- `marketdata.bookdelta.v1.{venue}.{instrument}` (optional context)
- Outputs:
- `insights.<volume_profile_snapshot>.v1.{venue}.{instrument}` (planned contract token)
- `insights.<volume_profile_delta>.v1.{venue}.{instrument}` (planned contract token)
- WS stream: `insights.volume_profile/{venue}/{symbol}/{timeframe}` (planned)

## Invariants
- Volume additivity and non-negative aggregates ([`docs/architecture/volume-profiles.md`](../../../docs/architecture/volume-profiles.md)).
- Deterministic bucket assignment for same tick size/input order ([`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Subject taxonomy and versioning remain stable ([`ADR-0014`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).
- Cross-venue normalization keeps canonical instrument identity ([`ADR-0017`](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md)).

## Backpressure
- Bounded queue per instrument/timeframe with explicit overload behavior ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade by coarser buckets and lower cadence before dropping critical state ([`docs/architecture/volume-profiles.md`](../../../docs/architecture/volume-profiles.md)).
- Keep drops and queue pressure observable per reason ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Replay
- Replay authority is [`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Reuse `internal/shared/replay` for deterministic fixture ordering.
- Parity test target: stable VPVR window snapshot across repeated runs.

## Evidence Hooks
- `internal/core/insights/app/join_crossvenue_trades.go`
- `internal/core/insights/app/join_crossvenue_trades_test.go`
- `internal/adapters/jetstream/subject_validation.go`
- `internal/shared/replay/golden_test.go`
- TODO: `internal/core/insights/domain/volume_profile.go`
- TODO: `internal/core/insights/app/build_volume_profile.go`
- TODO: `internal/adapters/storage/timescale/volume_profile_writer.go`
- TODO: `internal/interfaces/ws/volume_profile_delivery_test.go`

## Acceptance Tests
- `TestJoinCrossVenueTrades_DeterministicGivenSameInputSequence` - `internal/core/insights/app/join_crossvenue_trades_test.go`
- `TestJoinCrossVenueTrades_GoldenDeterministicSnapshotAndSignalBytes_50Runs` - `internal/core/insights/app/join_crossvenue_trades_test.go`
- `TestJoinEnabled_SubjectsPresent_Passes` - `internal/shared/config/loader_test.go`
- `TestJoinEnabled_MissingSubjects_Fails` - `internal/shared/config/loader_test.go`
- `TestGoldenReplay` - `internal/shared/replay/golden_test.go`
- TODO: `TestVPVRBucketDeterminism` - `internal/core/insights/app/build_volume_profile_test.go`
- TODO: `TestVPVRPointOfControlConsistency` - `internal/core/insights/app/build_volume_profile_test.go`
