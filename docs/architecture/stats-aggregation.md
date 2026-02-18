# Stats Aggregation Architecture (Per-Timeframe Metrics)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-18
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0017-multi-exchange-normalization.md`

## Purpose

Define per-timeframe stats aggregation combining liquidation volume, funding rate, and mark price into time-windowed summaries. This is a product parity gap vs marketmonkey (`actor/stat/` stats aggregator).

## Terminology (canonical)

- `instrument`: canonical envelope identity key.
- `subject`: bus routing key with version segment.
- `timeframe`: aggregation window boundary (1m, 5m, 15m, 1h).
- `envelope`: ordering/idempotency carrier (ADR-0002).
- `payload`: aggregated stats body per window.

## Data Planes

### Inputs

- `marketdata.liquidation.v1.{venue}.{instrument}` (liquidation ticks)
- `marketdata.markprice.v1.{venue}.{instrument}` (mark price ticks)
- `marketdata.fundingrate.v1.{venue}.{instrument}` (funding rate — planned)

### Outputs

- Planned derived event: `aggregation.stats.v1.{venue}.{instrument}`
- Planned WS stream: `aggregation.stats/{venue}/{symbol}/{timeframe}`

### Storage

Hot:
- `timescale.aggregation_stats_hot`

Cold:
- `clickhouse.aggregation_stats_cold`

Keys/idempotency:
- `(venue, instrument, timeframe, window_start_ts)`
- `idempotency_key` per window + timeframe

## Contracts

Planned stats payload v1:
- `venue`, `instrument`, `timeframe`
- `window_start_ts`, `window_end_ts`
- `liq_buy_volume`, `liq_sell_volume`, `liq_total_volume`, `liq_count`
- `markprice_open`, `markprice_high`, `markprice_low`, `markprice_close`
- `funding_rate_avg`, `funding_rate_last` (when available)
- `seq_first`, `seq_last`
- `is_closed`

## Invariants

- `ST-1`: aggregated volumes are non-negative and additive.
- `ST-2`: closed window is immutable after `is_closed=true`.
- `ST-3`: deterministic for same input event sequence.
- `ST-4`: replay of same fixture yields identical stats values.
- `ST-5`: bounded open window state per `(venue, instrument)` — max one open window per timeframe.
- `ST-6`: missing input type (e.g., no funding rate) produces partial stats, not failure.

## Backpressure

- Bounded queue per instrument.
- Progressive degradation under overload:
  1. reduce update cadence for open windows;
  2. prioritize window close over incremental updates;
  3. drop intermediate stats updates (keep close).

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Input event taxonomy and subject validation | Existing | `internal/adapters/jetstream/subject_validation.go` | `internal/adapters/jetstream/subject_validation_test.go` |
| Liquidation/MarkPrice dedup and normalization | Existing (partial) | `internal/core/marketdata/app/ingest.go` | `internal/core/marketdata/app/ingest_test.go` |
| Stats domain model (per-window aggregate) | TODO | `internal/core/aggregation/domain/stats.go` (TODO) | `internal/core/aggregation/domain/stats_test.go` (TODO) |
| Stats builder use case (multi-input) | TODO | `internal/core/aggregation/app/build_stats.go` (TODO) | `internal/core/aggregation/app/build_stats_test.go` (TODO) |
| Stats hot/cold writers | TODO | `internal/adapters/storage/timescale/stats_writer.go` (TODO) | `internal/adapters/storage/stats_writer_test.go` (TODO) |
| Stats WS delivery | TODO | `internal/interfaces/ws/stats_delivery.go` (TODO) | `internal/interfaces/ws/stats_delivery_test.go` (TODO) |
| Funding rate standalone pipeline | TODO | `internal/core/marketdata/app/` (TODO) | — |

## Storage Strategy

- Timescale: recent stats windows for low-latency query.
- ClickHouse: long-term historical stats for analytics/trends.
- Suggested retention:
  - hot: 30-90 days
  - cold: 365+ days

## Replay Strategy

- Rebuild from liquidation + markprice + funding events in deterministic order.
- Golden tests compare per-window stats values.

## Observability

- `stats_build_latency_ms{venue,instrument,timeframe}`
- `stats_close_total{venue,instrument,timeframe}`
- `stats_drop_total{reason}`
- `stats_partial_total{missing_input}` (tracks windows with incomplete input types)

Minimum:
- derivation lag
- close rate
- partial rate

## Acceptance Tests

Tests to create for stats feature:
- `internal/core/aggregation/domain/stats_test.go:TestStatsNonNegativeVolumes` (TODO)
- `internal/core/aggregation/app/build_stats_test.go:TestStatsDeterministicFromSameInputSequence` (TODO)
- `internal/core/aggregation/app/build_stats_test.go:TestStatsClosedWindowImmutability` (TODO)
- `internal/core/aggregation/app/build_stats_test.go:TestStatsPartialInputsProducePartialStats` (TODO)
- `internal/core/aggregation/app/build_stats_test.go:TestStatsReplayGoldenValues` (TODO)

## Evidence Hooks

Current related evidence:
- `internal/core/aggregation/app/update_orderbook.go` (closest aggregation use case)
- `internal/core/marketdata/app/ingest.go` (liquidation/markprice ingestion)

TODO hooks (skeleton):
- `internal/core/aggregation/domain/stats.go` (TODO)
- `internal/core/aggregation/app/build_stats.go` (TODO)
- `internal/adapters/storage/timescale/stats_writer.go` (TODO)

## Failure Modes

- Missing input stream (e.g., no funding rate for venue):
  - Mitigation: produce partial stats with explicit null/zero markers; ST-6 invariant.
- Input stream lag between liquidation and markprice:
  - Mitigation: window closes on time boundary regardless of input completeness.
- Cardinality from many instruments:
  - Mitigation: bounded map per instrument; shed low-activity instruments under overload.
