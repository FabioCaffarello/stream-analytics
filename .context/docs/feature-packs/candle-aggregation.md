# Feature Pack: Candle Aggregation

**STATUS:** PLANNED | **last_reviewed:** 2026-02-18

## Purpose
- Multi-timeframe OHLCV candle aggregation from trade events; authority: [candle-aggregation](../../../docs/architecture/candle-aggregation.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).

## Inputs/Outputs
- Inputs: `marketdata.trade.v1.{venue}.{instrument}`.
- Outputs: `aggregation.candle.v1.{venue}.{instrument}`.
- Planned WS: `aggregation.candle/{venue}/{symbol}/{timeframe}` ([delivery-ws](../../../docs/contracts/delivery-ws.md)).
- Subject refs: [ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md).

## Invariants
- OHLCV values deterministic for same trade input sequence ([candle-aggregation](../../../docs/architecture/candle-aggregation.md) CA-1, [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Closed candle immutable after commit (CA-2).
- Fixed timeframe set v1: 1m, 5m, 15m, 30m, 1h (CA-3).
- `high >= max(open,close)` and `low <= min(open,close)` always (CA-5).
- `volume = buy_volume + sell_volume` always (CA-6).
- Replay of same fixture yields identical OHLCV (CA-4).
- Bounded open candle state per (venue, instrument) — max one open candle per timeframe (CA-7).

## Backpressure
- Bounded queue per instrument ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Degrade: reduce open-candle update cadence -> prioritize close -> drop intermediate updates.

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/adapters/jetstream/subject_validation.go:24` (ValidateSubjectTaxonomy — input subject gate)
- `internal/shared/replay/player.go:45` (Replay — deterministic replay entry)
- TODO: `internal/core/aggregation/domain/candle.go` (candle aggregate model)
- TODO: `internal/core/aggregation/app/build_candle.go` (builder usecase)

## Acceptance Tests
- `TestValidateSubjectTaxonomy_Valid` -> `internal/adapters/jetstream/subject_validation_test.go:5`
- `TestGoldenReplay` -> `internal/shared/replay/golden_test.go:18`
- TODO: `TestCandleDeterministicFromSameTradeSequence` -> `internal/core/aggregation/app/build_candle_test.go`
- TODO: `TestCandleClosedImmutability` -> `internal/core/aggregation/app/build_candle_test.go`
- TODO: `TestCandleMultiTimeframeCascade` -> `internal/core/aggregation/app/build_candle_test.go`
- TODO: `TestCandleReplayGoldenOHLCV` -> `internal/core/aggregation/app/build_candle_test.go`
