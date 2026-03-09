# Stage 136 — Timeframe Data Policy & Readiness Unification

**Date:** 2026-03-09
**Status:** Complete
**Scope:** Client readiness architecture — policy unification, behavioral improvements, reduced overlay friction

## Problem Statement

Widget readiness logic was scattered across inline `switch` statements in `resolve_pane_visual_state`, with each widget kind having hand-coded store checks and state transitions. This caused:

1. **No single source of truth** for widget readiness contracts
2. **Candle charts on high TFs** showed "Seeding" (Backfilled) or "No History" (Live_Only) overlays for extended periods, even though the chart was fully renderable with available data
3. **No formal `partial_usable` concept** in the visual state machine — the bootstrap expectation had it, but it was informational only
4. **Inconsistency** between widget readiness decisions (each widget had independent inline logic)
5. **No documentation** of when absent backfill still allows useful rendering

## Solution

### 1. Unified Widget Readiness Policy Table

New compile-time policy table `widget_readiness_policies` in `app/widget_readiness.odin`:

| Widget | Primary Artifact | partial_usable | backfill_absent_usable | uses_artifact_live_flag |
|--------|-----------------|----------------|----------------------|----------------------|
| Candle | Candle | false | true | false |
| Stats | Stats | **true** | true | true |
| Counter | Candle | **true** | true | false |
| Trades | Trade | **true** | true | true |
| Orderbook | Orderbook | false | true | true |
| DOM | Orderbook | false | true | true |
| Heatmap | Heatmap | false | true | false |
| VPVR | VPVR | false | true | false |
| Analytics | CVD | **true** | true | false |
| Session_VPVR | Session_Volume_Profile | false | true | false |
| TPO | TPO_Profile | false | true | false |

### 2. Data_Readiness Enum

New 6-level readiness progression:

```
Not_Ready → Loading → Snapshot_Pending → Seeding → Partial_Usable → Live_Usable
```

- `Partial_Usable` and `Live_Usable` both map to `Pane_Visual_State.Active`
- Composition badges (PEND/BFILL/LIVE) communicate transitional state without blocking render

### 3. Unified `widget_data_readiness()` Function

Replaces the 80-line inline switch in `resolve_pane_visual_state` with a policy-driven pure function:

1. **Store check**: If widget's backing store has data → `Partial_Usable` or `Live_Usable`
2. **Composition check** (candle-dependent only): If composition stage indicates data → usable
3. **Artifact live flag**: Per-artifact liveness → `Snapshot_Pending`
4. **Stream liveness**: Any live data → `Seeding`
5. **Stream bound**: Connected → `Loading`
6. **Otherwise**: `Not_Ready`

### 4. Key Behavioral Improvements

| Scenario | Before S136 | After S136 | Impact |
|----------|-------------|------------|--------|
| Candle + Backfilled | Seeding overlay | **Active** (BFILL badge) | Eliminates minutes-long overlay on high TFs |
| Candle + Live_Only | No_History overlay | **Active** (LIVE badge) | Chart renders immediately with live data |
| All widgets | Inline switch logic | Policy-driven lookup | Consistent, maintainable, testable |

**High-TF example (15m candle):**
- Before: GetRange returns in 1s → "Seeding" overlay for up to 15 minutes until first live candle
- After: GetRange returns in 1s → chart renders immediately with BFILL badge → seamless transition to COMP when live arrives

### 5. Backfill-Absent Usability Matrix

When historical backfill is absent, which widgets are still useful?

| Widget | Useful Without Backfill? | Reason |
|--------|-------------------------|--------|
| Stats | **Yes** | Self-contained snapshot (price, volume, funding) |
| Trades | **Yes** | Live trade feed is inherently real-time |
| Orderbook/DOM | **Yes** | Book depth is a point-in-time snapshot |
| Counter | **Yes** | Live tick counter accumulates from subscription |
| Candle | **Yes** | Live candles form a chart from subscription time |
| Heatmap | **Yes** | Accumulates from live orderbook data |
| VPVR | **Yes** | Accumulates from live trade data |
| Analytics (CVD/DV) | **Yes** | Accumulates from live analytics stream |
| Session_VPVR | **Yes** | Session profile builds from current session |
| TPO | **Yes** | TPO blocks accumulate from current session |

### 6. Dependencies by Timeframe

| Bootstrap Source | TF Impact | Widgets |
|-----------------|-----------|---------|
| **Live_Immediate** | None — arrives in <1s regardless of TF | Stats, Trades, OB, OI |
| **Historical_Range** | None — GetRange returns network-speed | Candle |
| **Snapshot_Gate** | None — exchange pushes snapshot on subscribe | Orderbook depth |
| **Live_TF_Gated** | **First data delayed by TF duration** | Delta_Volume, CVD, Bar_Stats |
| **Accumulation** | Compounded by TF (base + TF wait) | Heatmap, VPVR, Session_VP, TPO |

**Live_TF_Gated is the primary high-TF bottleneck.** On 15m TF, CVD/Delta_Volume widgets wait up to 15 minutes for first candle close. Bootstrap hints communicate this to the user.

## Files Changed

### New
- `client/src/core/app/widget_readiness.odin` — Policy table, Data_Readiness enum, pure readiness functions

### Modified
- `client/src/core/app/shell_common.odin` — Refactored `resolve_pane_visual_state` to use policy-driven `widget_data_readiness`; removed `_widget_primary_artifact` (replaced by policy table)
- `client/src/core/app/marketdata_test.odin` — Updated 4 tests for new behavioral expectations; added 22 new S136 tests

## Test Coverage

### Updated Tests (4)
- `test_pane_visual_state_live_only` — Candle + Live_Only now yields Active
- `test_pane_visual_state_backfilled` — Candle + Backfilled now yields Active
- `test_pane_visual_state_no_history_candle` — Candle + Live_Only now yields Active
- `test_pane_visual_state_candle_backfilled` — Candle + Backfilled now yields Active

### New Tests (22)
- `test_s136_data_readiness_ordering` — Enum ordering invariant
- `test_s136_readiness_to_visual_state_mapping` — All 6 readiness → visual state mappings
- `test_s136_policy_table_complete` — Every widget has valid policy
- `test_s136_policy_backfill_absent_usable` — All data widgets are backfill-absent usable
- `test_s136_policy_partial_usable_artifacts` — Correct partial_usable assignments
- `test_s136_widget_store_has_data_empty_stores` — Empty stores → no data for any widget
- `test_s136_widget_store_has_data_candle` — Candle/Counter share candle store
- `test_s136_widget_store_has_data_orderbook` — OB/DOM share orderbook store
- `test_s136_widget_data_readiness_not_ready` — Unbound + empty = Not_Ready
- `test_s136_widget_data_readiness_loading` — Bound + no data = Loading
- `test_s136_candle_backfilled_is_partial_usable` — Key improvement
- `test_s136_candle_live_only_is_partial_usable` — Key improvement
- `test_s136_candle_composed_is_live_usable` — Steady state
- `test_s136_candle_range_pending_is_loading` — In-flight = Loading
- `test_s136_stats_with_data_is_live_usable` — Composed + data
- `test_s136_stats_partial_no_composed` — Non-composed + data
- `test_s136_analytics_seeding_on_high_tf` — TF-gated, no data yet
- `test_s136_orderbook_snapshot_pending_flow` — Artifact live, store empty
- `test_s136_store_data_overrides_composition` — Store data takes priority
- `test_s136_widget_store_label_coverage` — All widgets have diagnostic labels
- `test_s136_candle_store_data_composed_live_usable` — Store + composed
- `test_s136_candle_store_data_backfilled_partial` — Store + backfilled

## Architecture Invariants

1. **Policy table is `@(rodata)`** — compile-time, zero runtime cost
2. **All readiness functions are pure** — deterministic, no mutation, no allocation
3. **Store check is the primary readiness signal** — if store has data, widget is usable
4. **Composition stage is secondary** — only used for candle-dependent widgets as a data-availability signal
5. **Universal gates still override** — Offline, Desync, Critical always take precedence
6. **Composition badges are the transitional state UI** — not blocking overlays
7. **`partial_usable` is policy-driven** — the policy table decides, not inline code
8. **Zero wire-breaking changes** — all changes are client-side readiness logic

## Acceptance Criteria

- [x] Unified readiness policy documented in compile-time table
- [x] Less divergence between widgets (policy-driven vs inline switch)
- [x] Less unnecessary wait on high TFs (Backfilled/Live_Only → Active)
- [x] Backfill-absent usability documented per widget
- [x] TF dependencies formalized (Bootstrap_Source × TF impact)
- [x] Policy aligned between backend expectations and client visual states
- [x] 22 new tests, 4 updated tests, zero regressions
