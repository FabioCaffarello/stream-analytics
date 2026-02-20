# Odin M7 Evidence - Stats Aggregation Production

Date: 2026-02-19
Milestone: M7 - Stats Aggregation Production

## Scope Closed

- Stats aggregation pipeline is active for liquidation/markprice/funding inputs across timeframes.
- Cross-source consistency checks are covered at use-case level.
- Runtime and WS contract routes for `aggregation.stats` are validated.
- E2E latency budget evidence captured for `markprice -> stats` path.

## Code Changes

- Added strong per-timeframe + cross-source consistency test:
  - `internal/core/aggregation/app/build_stats_test.go`
  - `TestBuildStats_MixedInputs_CloseAllTimeframes_CrossSourceConsistency`
- Added dedicated E2E benchmark for stats path:
  - `internal/core/aggregation/app/bench_e2e_pipeline_test.go`
  - `BenchmarkE2E_MarkPriceToStats`

## Validation Commands

### Stats core + runtime + WS contract

```bash
go test ./internal/core/aggregation/app -run 'TestBuildStats_(WindowClose_EmitsStatsClosed|PartialInputsProducePartialStats|Deterministic_SameInputSameOutput|BoundedMap_EvictsOldest|MixedInputs_CloseAllTimeframes_CrossSourceConsistency)|TestBuildStats_GoldenDeterminism_.*|TestBuildStats_GoldenPartialInputs|TestBuildStats_Soak_HighCardinality'
go test ./internal/actors/aggregation/runtime -run 'TestProcessorE2E_(LiquidationToStats_WindowClose|MarkPriceWithFunding_DualRouting)|TestProcessor_(LiquidationRoute_EmitsStatsClosed|MarkPriceRoute_WithFunding_EmitsStatsClosed|StatsDisabled_SkipsLiquidationAndMarkPriceRoutes)'
go test ./internal/interfaces/ws -run 'TestWSDelivery_StatsClosed_RoutedToSubscriber'
```

Result: PASS

### E2E latency budget (markprice -> stats)

```bash
go test ./internal/core/aggregation/app -run '^$' -bench '^BenchmarkE2E_MarkPriceToStats$' -benchmem -count=5
```

Observed samples:

- 14810 ns/op
- 19505 ns/op
- 15446 ns/op
- 17924 ns/op
- 14960 ns/op

Computed summary:

- max: 19505 ns/op (19.505 us/op)
- median: 15446 ns/op (15.446 us/op)

Budget reference: `docs/perf/performance-budgets.md` target for `E2E (markprice->stats)` <= 25 us/op.

Status: PASS (all observed samples below budget).

## M7 Exit Criteria Mapping

- Tests by timeframe: PASS (`MixedInputs_CloseAllTimeframes...` validates 1m/5m/15m/30m/1h closure).
- Cross-source consistency: PASS (liquidation + markprice + funding invariants asserted on closed windows).
- Observability ready: PASS (stats stream exposed through existing processor/delivery metrics and dedicated budget row for E2E stats path).

## Contract and Documentation Sync

- Stats architecture and feature-pack updated from planned/TODO to implemented evidence.
- Subject status updated:
  - `aggregation.stats.v1` promoted to stable in `docs/contracts/subject-registry.yaml`.
  - Event bus matrix aligned in `docs/contracts/event-bus.md`.
- Delivery contract input/output examples now include stats stream in `docs/contracts/delivery-ws.md`.
- PRD current-state/non-goal/risk entries aligned for M7 in `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`.
- TRUTH-MAP stats row updated to implemented with code/test anchors.
