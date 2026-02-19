# Candle Aggregation Architecture (Multi-Timeframe OHLCV)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-18
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`

## Purpose

Define multi-timeframe OHLCV candle aggregation from trade events with deterministic bucketing, bounded state, and hot/cold persistence plan. This is a core product parity gap vs marketmonkey (`actor/trade/` candle sampler).

## Terminology (canonical)

- `instrument`: canonical envelope identity key.
- `subject`: bus routing key with version segment.
- `stream`: subscription pattern (JetStream/WS).
- `timeframe`: candle aggregation window boundary (1m, 5m, 15m, 30m, 1h).
- `envelope`: ordering/idempotency carrier (ADR-0002).
- `payload`: OHLCV candle body emitted by aggregation pipeline.

## Data Planes

### Inputs

- `marketdata.trade.v1.{venue}.{instrument}` (primary — trade ticks drive candle formation)

### Outputs

- Planned derived event: `aggregation.candle.v1.{venue}.{instrument}` (subject root TBD — `aggregation` root accepted in runtime)
- Planned WS stream: `aggregation.candle/{venue}/{symbol}/{timeframe}`

### Storage

Hot:
- `timescale.aggregation_candle_hot`

Cold:
- `clickhouse.aggregation_candle_cold`

Keys/idempotency:
- `(venue, instrument, timeframe, window_start_ts)`
- `idempotency_key` per window + timeframe

## Contracts

Planned candle payload v1:
- `venue`, `instrument`, `timeframe`
- `window_start_ts`, `window_end_ts`
- `open`, `high`, `low`, `close`, `volume`
- `buy_volume`, `sell_volume` (aggressor split)
- `trade_count`
- `seq_first`, `seq_last`
- `is_closed` (boolean — marks final candle for window)

Multi-timeframe hierarchy:
- 1m candles built from trades
- 5m/15m/30m/1h candles built from 1m candles (cascade)
- Only 1m candle formation touches raw trades

## Invariants

- `CA-1`: candle values deterministic for same trade input sequence.
- `CA-2`: closed candle is immutable — no retroactive mutation after `is_closed=true`.
- `CA-3`: supported timeframes: 1m, 5m, 15m, 30m, 1h (fixed set in v1).
- `CA-4`: replay of same fixture yields identical OHLCV values and ordering.
- `CA-5`: `high >= open, close, low` and `low <= open, close, high` always.
- `CA-6`: `volume = buy_volume + sell_volume` always.
- `CA-7`: bounded open candle state per `(venue, instrument)` — max one open candle per timeframe.

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
| Candle domain model (OHLCV aggregate) | TODO | `internal/core/aggregation/domain/candle.go` (TODO) | `internal/core/aggregation/domain/candle_test.go` (TODO) |
| Candle builder use case (multi-timeframe) | TODO | `internal/core/aggregation/app/build_candle.go` (TODO) | `internal/core/aggregation/app/build_candle_test.go` (TODO) |
| Candle hot/cold writers | TODO | `internal/adapters/storage/timescale/candle_writer.go` (TODO) | `internal/adapters/storage/candle_writer_test.go` (TODO) |
| Candle WS delivery | TODO | `internal/interfaces/ws/candle_delivery.go` (TODO) | `internal/interfaces/ws/candle_delivery_test.go` (TODO) |

## Storage Strategy

- Timescale: recent candles for low-latency query and WS delivery.
- ClickHouse: long-term historical candles for analytics/backtesting.
- Suggested retention:
  - hot: 30-90 days
  - cold: 365+ days

## Replay Strategy

- Rebuild 1m candles from `marketdata.trade` in deterministic order.
- Higher timeframes rebuild from 1m candles (cascade).
- Golden tests compare per-window OHLCV values.

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

Tests to create for candle feature:
- `internal/core/aggregation/domain/candle_test.go:TestCandleOHLCVInvariantsHoldForAllInputs` (TODO)
- `internal/core/aggregation/app/build_candle_test.go:TestCandleDeterministicFromSameTradeSequence` (TODO)
- `internal/core/aggregation/app/build_candle_test.go:TestCandleClosedImmutability` (TODO)
- `internal/core/aggregation/app/build_candle_test.go:TestCandleMultiTimeframeCascade` (TODO)
- `internal/core/aggregation/app/build_candle_test.go:TestCandleReplayGoldenOHLCV` (TODO)

## Evidence Hooks

Current related evidence:
- `internal/core/aggregation/app/update_orderbook.go` (closest aggregation use case)
- `internal/shared/replay/player.go`
- `internal/adapters/jetstream/consumer.go`

TODO hooks (skeleton):
- `internal/core/aggregation/domain/candle.go` (TODO)
- `internal/core/aggregation/app/build_candle.go` (TODO)
- `internal/adapters/storage/timescale/candle_writer.go` (TODO)

## Failure Modes

- Gap in trade stream:
  - Mitigation: candle stays open until next trade or timeout; gap flag in metadata.
- Clock drift between venues:
  - Mitigation: use `ts_ingest` (server time) not exchange time for window assignment.
- Cascade lag accumulation:
  - Mitigation: higher-timeframe candles flush on 1m close, not on trade.
