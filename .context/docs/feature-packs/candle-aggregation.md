# Feature Pack: Candle Aggregation

**STATUS:** IMPLEMENTED | **last_reviewed:** 2026-03-01

## Purpose
- Multi-timeframe OHLCV candle aggregation from trade events.
- Authority docs: [candle-aggregation](../../../docs/architecture/candle-aggregation.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).

## Inputs/Outputs
- Inputs: `marketdata.trade.v1.{venue}.{instrument}`.
- Outputs: `aggregation.candle.v1.{venue}.{instrument}`.
- WS stream: `aggregation.candle/{venue}/{symbol}/{timeframe}` ([delivery-ws](../../../docs/contracts/delivery-ws.md)).
- `getrange` compatibility: `to_ms` is canonical; `end_ts` remains accepted for backward compatibility.
- Symbol compatibility: `SYMBOL:MARKET_TYPE` aliases are resolved to canonical `SYMBOL` in delivery range fallback.

## Invariants
- Deterministic OHLCV for identical input sequence.
- Closed candle is immutable.
- Fixed timeframe set v1: `1m`, `5m`, `15m`, `30m`, `1h`.
- `high >= max(open, close)` and `low <= min(open, close)`.
- `volume = buy_volume + sell_volume`.

## Backpressure
- Bounded in-memory candle windows (`max_candles`).
- Overload strategy preserves close events over intermediate updates.

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/core/aggregation/domain/candle.go`
- `internal/core/aggregation/app/build_candle.go`
- `internal/adapters/storage/timescale/candle_writer.go`
- `internal/adapters/storage/clickhouse/candle_writer.go`
- `internal/actors/aggregation/runtime/processor.go`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go`
- `internal/core/aggregation/app/bench_e2e_pipeline_test.go`

## Acceptance Tests
- `internal/core/aggregation/domain/candle_test.go:TestCandleV1_NewValidation`
- `internal/core/aggregation/domain/candle_test.go:TestCandleV1_Deterministic`
- `internal/core/aggregation/app/build_candle_test.go:TestBuildCandle_MultiTimeframe_1mCascades`
- `internal/core/aggregation/app/build_candle_test.go:TestBuildCandle_Deterministic_SameInputSameOutput`
- `internal/core/aggregation/app/build_candle_golden_test.go:TestBuildCandle_GoldenDeterminism`
- `internal/core/aggregation/app/build_candle_soak_test.go:TestBuildCandle_Soak_HighCardinality`
- `internal/actors/aggregation/runtime/processor_e2e_test.go:TestProcessorE2E_TradeToCandle_WindowClose`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go:TestWSDelivery_CandleClosed_RoutedToSubscriber`
