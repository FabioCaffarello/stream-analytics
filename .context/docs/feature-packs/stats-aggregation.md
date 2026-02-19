# Feature Pack: Stats Aggregation

**STATUS:** PLANNED | **last_reviewed:** 2026-02-18

## Purpose
- Per-timeframe stats combining liquidation volume, funding rate, and mark price; authority: [stats-aggregation](../../../docs/architecture/stats-aggregation.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md).

## Inputs/Outputs
- Inputs: `marketdata.liquidation.v1.{venue}.{instrument}`, `marketdata.markprice.v1.{venue}.{instrument}`, `marketdata.fundingrate.v1.{venue}.{instrument}` (planned, not in event-bus.md matrix).
- Outputs: `aggregation.stats.v1.{venue}.{instrument}`.
- Planned WS: `aggregation.stats/{venue}/{symbol}/{timeframe}` ([delivery-ws](../../../docs/contracts/delivery-ws.md)).
- Subject refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).

## Invariants
- Non-negative additive volumes ([stats-aggregation](../../../docs/architecture/stats-aggregation.md) ST-1).
- Closed window immutable after commit (ST-2).
- Deterministic for same input sequence ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md) ST-3).
- Replay of same fixture yields identical stats (ST-4).
- Missing input type produces partial stats, not failure (ST-6).

## Backpressure
- Bounded queue per instrument ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade: reduce update cadence -> prioritize close -> drop intermediate updates.

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/adapters/jetstream/subject_validation.go:24` (ValidateSubjectTaxonomy — input subject gate)
- `internal/core/marketdata/app/ingest.go:1` (liquidation/markprice ingestion — Existing)
- TODO: `internal/core/aggregation/domain/stats.go` (stats aggregate model)
- TODO: `internal/core/aggregation/app/build_stats.go` (builder usecase)

## Acceptance Tests
- `TestValidateSubjectTaxonomy_Valid` -> `internal/adapters/jetstream/subject_validation_test.go:5`
- `TestGoldenReplay` -> `internal/shared/replay/golden_test.go:18`
- TODO: `TestStatsDeterministicFromSameInputSequence` -> `internal/core/aggregation/app/build_stats_test.go`
- TODO: `TestStatsClosedWindowImmutability` -> `internal/core/aggregation/app/build_stats_test.go`
- TODO: `TestStatsPartialInputsProducePartialStats` -> `internal/core/aggregation/app/build_stats_test.go`
- TODO: `TestStatsReplayGoldenValues` -> `internal/core/aggregation/app/build_stats_test.go`
