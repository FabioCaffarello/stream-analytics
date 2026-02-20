# Odin M6 Evidence - Candle Aggregation Production

Date: 2026-02-19
Milestone: M6 - Candle Aggregation Production

## Scope Closed

- Runtime candle/stats feature toggles are now explicitly enforced in processor routing.
- Candle pipeline remains active and validated end-to-end when enabled.
- WS delivery contract for candle subject validated.
- Candle latency budget evidence captured via benchmark.

## Code Changes

- Runtime gate enforcement:
  - `internal/actors/aggregation/runtime/processor.go`
    - Added explicit config toggles (`CandleEnabled`, `StatsEnabled`) with backward-compatible nil behavior.
    - Trade route now checks candle toggle before invoking candle use case.
    - Liquidation/markprice routes now check stats toggle before invoking stats use case.
- Processor wiring:
  - `cmd/processor/bootstrap.go`
    - Passes `processor.candle.enabled` and `processor.stats.enabled` into runtime config.
- New regression tests:
  - `internal/actors/aggregation/runtime/processor_test.go`
    - `TestProcessor_TradeEnvelopeWithoutJoin_CandleDisabled_SkipsCandle`
    - `TestProcessor_StatsDisabled_SkipsLiquidationAndMarkPriceRoutes`

## Validation Commands

### Runtime and contract tests

```bash
go test ./internal/actors/aggregation/runtime -run 'TestProcessor_(TradeEnvelopeWithoutJoin_CandleDisabled_SkipsCandle|StatsDisabled_SkipsLiquidationAndMarkPriceRoutes|TradeEnvelopeWithoutJoin_ProcessesCandle|LiquidationRoute_EmitsStatsClosed|MarkPriceRoute_WithFunding_EmitsStatsClosed)'
go test ./internal/actors/aggregation/runtime -run 'TestProcessorE2E_(TradeToCandle_WindowClose|LiquidationToStats_WindowClose|MarkPriceWithFunding_DualRouting)'
go test ./internal/interfaces/ws -run 'TestWSDelivery_(CandleClosed_RoutedToSubscriber|StatsClosed_RoutedToSubscriber|CandleClosed_MultiInstrumentSubscriptions)'
go test ./internal/core/aggregation/app -run 'TestCandle|TestBuildCandle|TestBuildCandleFromEvents|TestBuildCandleFromEvents_.*|TestBuildCandle_.*'
```

Result: PASS

### Latency budget benchmark (Trade -> Candle)

```bash
go test ./internal/core/aggregation/app -run '^$' -bench '^BenchmarkE2E_TradeToCandle$' -benchmem -count=5
```

Observed samples:

- 16428 ns/op
- 13859 ns/op
- 13572 ns/op
- 14066 ns/op
- 16119 ns/op

Computed summary:

- max: 16428 ns/op (16.428 us/op)
- median: 14066 ns/op (14.066 us/op)

Budget reference: `docs/perf/performance-budgets.md` target for `E2E (trade->candle)` <= 20 us/op.

Status: PASS (all observed samples below budget).

## Contract and Documentation Sync

- Candle architecture and feature-pack updated from planned/TODO to implemented evidence.
- Subject status updated:
  - `aggregation.candle.v1` promoted to stable in `docs/contracts/subject-registry.yaml`.
  - Event bus matrix aligned in `docs/contracts/event-bus.md`.
- Delivery contract input/output examples now include candle stream in `docs/contracts/delivery-ws.md`.
- PRD current-state/non-goal/risk entries aligned for M6 in `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`.
- TRUTH-MAP candle row updated to implemented with code/test anchors.
