# Stats Aggregation Architecture (Per-Timeframe Metrics)

**Status:** Active
**Owner:** Product Architect
**Last updated:** 2026-02-19
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0017-multi-exchange-normalization.md`

## Purpose

Define per-timeframe stats aggregation combining liquidation volume, funding rate, and mark price into time-windowed summaries. This capability is active in runtime and part of pre-launch maturity gates for Odin.

## Terminology (canonical)

- `instrument`: canonical envelope identity key.
- `subject`: bus routing key with version segment.
- `timeframe`: aggregation window boundary (1m, 5m, 15m, 30m, 1h).
- `envelope`: ordering/idempotency carrier (ADR-0002).
- `payload`: aggregated stats body per window.

## Data Planes

### Inputs

- `marketdata.liquidation.v1.{venue}.{instrument}` (liquidation ticks)
- `marketdata.markprice.v1.{venue}.{instrument}` (mark price ticks)
- `marketdata.fundingrate.v1.{venue}.{instrument}` (optional direct funding source; current runtime ingests funding from markprice stream)

### Outputs

- Derived event: `aggregation.stats.v1.{venue}.{instrument}`
- WS stream: `aggregation.stats/{venue}/{symbol}/{timeframe}`

### Storage

Hot:
- `timescale.aggregation_stats_hot`

Cold:
- `clickhouse.aggregation_stats_cold`

Keys/idempotency:
- `(venue, instrument, timeframe, window_start_ts)`
- `idempotency_key` per window + timeframe

## Contracts

Stats payload v1:
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
| Stats domain model (per-window aggregate) | Implemented | `internal/core/aggregation/domain/stats.go` | `internal/core/aggregation/domain/stats_test.go` |
| Stats builder use case (multi-input) | Implemented | `internal/core/aggregation/app/build_stats.go` | `internal/core/aggregation/app/build_stats_test.go`, `internal/core/aggregation/app/build_stats_golden_test.go`, `internal/core/aggregation/app/build_stats_soak_test.go` |
| Stats hot/cold writers | Implemented | `internal/adapters/storage/timescale/stats_writer.go`, `internal/adapters/storage/clickhouse/stats_writer.go` | `internal/adapters/storage/timescale/stats_writer_test.go`, `internal/adapters/storage/clickhouse/roundtrip_test.go` |
| Stats WS delivery | Implemented | `internal/actors/delivery/runtime/router.go`, `internal/interfaces/ws/server.go` | `internal/interfaces/ws/candle_stats_delivery_contract_test.go` |
| Funding rate standalone pipeline | Deferred (M9) | `internal/actors/aggregation/runtime/processor.go` (funding embedded via markprice dual-route) | `internal/actors/aggregation/runtime/processor_e2e_test.go` |

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

- `processor_processed_total{event_type="marketdata.liquidation|marketdata.markprice",status}`
- `processor_commit_total{status="stats_hot|stats_cold"}`
- `processor_commit_latency_seconds` (writer commit latency SLI)
- `ws_drops_total{reason}` (delivery path protection for `aggregation.stats` stream)

Minimum:
- derivation lag
- close rate
- partial rate

## Acceptance Tests

Primary acceptance tests:
- `internal/core/aggregation/domain/stats_test.go:TestStatsWindowV1_ApplyLiquidation_ST1NonNegativeAdditive`
- `internal/core/aggregation/domain/stats_test.go:TestStatsWindowV1_Close_Immutability`
- `internal/core/aggregation/domain/stats_test.go:TestStatsWindowV1_PartialInputsAllowed_ST6`
- `internal/core/aggregation/app/build_stats_test.go:TestBuildStats_MixedInputs_CloseAllTimeframes_CrossSourceConsistency`
- `internal/core/aggregation/app/build_stats_test.go:TestBuildStats_Deterministic_SameInputSameOutput`
- `internal/core/aggregation/app/build_stats_golden_test.go:TestBuildStats_GoldenDeterminism_MixedInputs`
- `internal/actors/aggregation/runtime/processor_e2e_test.go:TestProcessorE2E_MarkPriceWithFunding_DualRouting`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go:TestWSDelivery_StatsClosed_RoutedToSubscriber`

## Evidence Hooks

Current related evidence:
- `internal/core/aggregation/domain/stats.go`
- `internal/core/aggregation/app/build_stats.go`
- `internal/adapters/storage/timescale/stats_writer.go`
- `internal/adapters/storage/clickhouse/stats_writer.go`
- `internal/actors/aggregation/runtime/processor.go`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go`
- `internal/core/aggregation/app/bench_e2e_pipeline_test.go`

## Failure Modes

- Missing input stream (e.g., no funding rate for venue):
  - Mitigation: produce partial stats with explicit null/zero markers; ST-6 invariant.
- Input stream lag between liquidation and markprice:
  - Mitigation: window closes on time boundary regardless of input completeness.
- Cardinality from many instruments:
  - Mitigation: bounded map per instrument; shed low-activity instruments under overload.
