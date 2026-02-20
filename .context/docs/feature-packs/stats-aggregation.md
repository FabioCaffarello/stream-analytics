# Feature Pack: Stats Aggregation

**STATUS:** IMPLEMENTED | **last_reviewed:** 2026-02-19

## Purpose
- Per-timeframe stats combining liquidation volume, funding rate, and mark price.
- Authority docs: [stats-aggregation](../../../docs/architecture/stats-aggregation.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).

## Inputs/Outputs
- Inputs: `marketdata.liquidation.v1.{venue}.{instrument}`, `marketdata.markprice.v1.{venue}.{instrument}`.
- Optional direct input path (deferred as standalone): `marketdata.fundingrate.v1.{venue}.{instrument}` (planned, not in event-bus.md matrix).
- Outputs: `aggregation.stats.v1.{venue}.{instrument}`.
- WS stream: `aggregation.stats/{venue}/{symbol}/{timeframe}` ([delivery-ws](../../../docs/contracts/delivery-ws.md)).

## Invariants
- Non-negative additive liquidation volumes.
- Closed window is immutable.
- Deterministic output for identical input sequence.
- Replay of same fixture yields equivalent closed windows.
- Bounded open-window state by `(venue, instrument, timeframe)`.
- Missing input type yields partial stats instead of failure.

## Backpressure
- Bounded in-memory stats windows (`max_windows`).
- Overload strategy preserves close events over intermediate updates.

## Replay
- Deterministic replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden baseline: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/core/aggregation/domain/stats.go`
- `internal/core/aggregation/app/build_stats.go`
- `internal/adapters/storage/timescale/stats_writer.go`
- `internal/adapters/storage/clickhouse/stats_writer.go`
- `internal/actors/aggregation/runtime/processor.go`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go`
- `internal/core/aggregation/app/bench_e2e_pipeline_test.go`

## Acceptance Tests
- `internal/core/aggregation/domain/stats_test.go:TestStatsWindowV1_ApplyLiquidation_ST1NonNegativeAdditive`
- `internal/core/aggregation/domain/stats_test.go:TestStatsWindowV1_Close_Immutability`
- `internal/core/aggregation/domain/stats_test.go:TestStatsWindowV1_PartialInputsAllowed_ST6`
- `internal/core/aggregation/app/build_stats_test.go:TestBuildStats_MixedInputs_CloseAllTimeframes_CrossSourceConsistency`
- `internal/core/aggregation/app/build_stats_test.go:TestBuildStats_Deterministic_SameInputSameOutput`
- `internal/core/aggregation/app/build_stats_golden_test.go:TestBuildStats_GoldenDeterminism_MixedInputs`
- `internal/actors/aggregation/runtime/processor_e2e_test.go:TestProcessorE2E_MarkPriceWithFunding_DualRouting`
- `internal/interfaces/ws/candle_stats_delivery_contract_test.go:TestWSDelivery_StatsClosed_RoutedToSubscriber`
