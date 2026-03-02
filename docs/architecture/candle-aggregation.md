# Candle Aggregation Architecture (Multi-Timeframe OHLCV)

**Status:** Active
**Owner:** Product Architect
**Last updated:** 2026-03-01
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`

## Purpose

Define multi-timeframe OHLCV candle aggregation from trade events with deterministic bucketing, bounded state, and hot/cold persistence. This capability is active in runtime and part of pre-launch maturity gates for Odin.

## Terminology (canonical)

- `instrument`: canonical envelope identity key.
- `subject`: bus routing key with version segment.
- `stream`: subscription pattern (JetStream/WS).
- `timeframe`: candle aggregation window boundary (1s, 5s, 1m, 5m, 15m, 30m, 1h, 4h, 1d).
- `envelope`: ordering/idempotency carrier (ADR-0002).
- `payload`: OHLCV candle body emitted by aggregation pipeline.

## Data Planes

### Inputs

- `marketdata.trade.v1.{venue}.{instrument}` (primary — trade ticks drive candle formation)

### Outputs

- Derived event: `aggregation.candle.v1.{venue}.{instrument}`
- WS stream: `aggregation.candle/{venue}/{symbol}/{timeframe}`

### Storage

Hot:
- `timescale.aggregation_candle_hot`

Cold:
- `clickhouse.aggregation_candle_cold`

Keys/idempotency:
- `(venue, instrument, timeframe, window_start_ts)`
- `idempotency_key` per window + timeframe

## Contracts

Candle payload v1:
- `venue`, `instrument`, `timeframe`
- `window_start_ts`, `window_end_ts`
- `open`, `high`, `low`, `close`, `volume`
- `buy_volume`, `sell_volume` (aggressor split)
- `trade_count`
- `seq_first`, `seq_last`
- `is_closed` (boolean — marks final candle for window)

Multi-timeframe hierarchy:
- 1s candles built from trades
- 5s/1m/5m/15m/30m/1h/4h/1d candles built from 1s candles (cascade)
- Only 1s candle formation touches raw trades

## Invariants

- `CA-1`: candle values deterministic for same trade input sequence.
- `CA-2`: closed candle is immutable — no retroactive mutation after `is_closed=true`.
- `CA-3`: supported timeframes: 1s, 5s, 1m, 5m, 15m, 30m, 1h, 4h, 1d (fixed set in v1).
- `CA-4`: replay of same fixture yields identical OHLCV values and ordering.
- `CA-5`: `high >= open, close, low` and `low <= open, close, high` always.
- `CA-6`: `volume = buy_volume + sell_volume` always.
- `CA-7`: bounded open candle state per `(venue, instrument)` — max one open candle per timeframe.
- `CA-8`: window closes are event-driven; empty windows are not synthesized retroactively.

## Backpressure

- Bounded queue per instrument.
- Candles are naturally bounded by time window — cardinality explosion risk is low.
- Progressive degradation under overload:
  1. reduce update cadence for open candles;
  2. prioritize window close over incremental updates;
  3. drop intermediate open-candle updates (keep close).

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Input event taxonomy and subject validation | Existing | `internal/adapters/jetstream/subject_validation.go` | `internal/adapters/jetstream/subject_validation_test.go` |
| Deterministic replay foundation (`ts_ingest`, `seq`) | Existing | `internal/shared/replay/player.go`, `internal/shared/replay/sequencer.go` | `internal/shared/replay/golden_test.go:TestGoldenReplay` |
| Candle domain model (OHLCV aggregate) | Implemented | `internal/core/aggregation/domain/candle.go` | `internal/core/aggregation/domain/candle_test.go` |
| Candle builder use case (multi-timeframe) | Implemented | `internal/core/aggregation/app/build_candle.go` | `internal/core/aggregation/app/build_candle_test.go`, `internal/core/aggregation/app/build_candle_golden_test.go`, `internal/core/aggregation/app/build_candle_soak_test.go` |
| Candle hot/cold writers | Implemented | `internal/adapters/storage/timescale/candle_writer.go`, `internal/adapters/storage/clickhouse/candle_writer.go` | `internal/adapters/storage/timescale/candle_writer_test.go`, `internal/adapters/storage/clickhouse/roundtrip_test.go` |
| Candle WS delivery | Implemented | `internal/actors/delivery/runtime/router.go`, `internal/interfaces/ws/server.go` | `internal/interfaces/ws/candle_stats_delivery_contract_test.go` |

## Storage Strategy

- Timescale: recent candles for low-latency query and WS delivery.
- ClickHouse: long-term historical candles for analytics/backtesting.
- Delivery `getrange` queries operate on canonical `symbol`; when clients send `symbol:market_type`, session use case performs one fallback lookup to canonical `symbol` for compatibility.
- Current operational retention:
  - hot: operator-scheduled cleanup via `cleanup_aggregation_hot_retention` (`1s`,`5s` 14 days; others 90 days).
  - cold (default): 90 days.
  - cold (`1s`,`5s`): 14 days (cost guardrail for sub-minute).

## Replay Strategy

- Rebuild 1s candles from `marketdata.trade` in deterministic order.
- Higher timeframes rebuild from 1s candles (cascade).
- Golden tests compare per-window OHLCV values.
- Under gaps with no trade, closes are emitted only when a later trade advances the window.

## Observability

- `candle_build_latency_ms{venue,instrument,timeframe}`
- `candle_close_total{venue,instrument,timeframe}`
- `candle_drop_total{reason}`
- `candle_queue_depth{venue,instrument}`

Minimum:
- derivation lag
- close rate
- drop rate

## Acceptance Tests

Primary acceptance tests:
- `internal/core/aggregation/domain/candle_test.go:TestCandleV1_NewValidation`
- `internal/core/aggregation/domain/candle_test.go:TestCandleV1_Deterministic`
- `internal/core/aggregation/app/build_candle_test.go:TestBuildCandle_MultiTimeframe_BaseCascades`
- `internal/core/aggregation/app/build_candle_test.go:TestBuildCandle_Deterministic_SameInputSameOutput`
- `internal/core/aggregation/app/build_candle_test.go:TestBuildCandle_GapEventDriven_NoSyntheticBaseClosures`
- `internal/core/aggregation/app/build_candle_golden_test.go:TestBuildCandle_GoldenDeterminism`
- `internal/core/aggregation/app/build_candle_soak_test.go:TestBuildCandle_Soak_HighCardinality`
- `internal/actors/aggregation/runtime/processor_e2e_test.go:TestProcessorE2E_TradeToCandle_WindowClose`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go:TestWSDelivery_CandleClosed_RoutedToSubscriber`

## Evidence Hooks

Current related evidence:
- `internal/core/aggregation/domain/candle.go`
- `internal/core/aggregation/app/build_candle.go`
- `internal/adapters/storage/timescale/candle_writer.go`
- `internal/adapters/storage/clickhouse/candle_writer.go`
- `internal/actors/aggregation/runtime/processor.go`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go`
- `internal/core/aggregation/app/bench_e2e_pipeline_test.go`

## Failure Modes

- Gap in trade stream:
  - Mitigation: event-driven close on next trade (no synthetic empty-window candles); expose gap semantics in contracts/runbooks.
- Clock drift between venues:
  - Mitigation: use `ts_ingest` (server time) not exchange time for window assignment.
- Cascade lag accumulation:
  - Mitigation: higher-timeframe candles flush on 1s close, not on trade.
