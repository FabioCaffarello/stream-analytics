# Stage 124 — Timeframe-Aware Widget Readiness

**Date:** 2026-03-09
**Status:** COMPLETE
**Branch:** codex/s9-legacy-removal-cutover

## Problem

Widget panes were stuck in "Loading" or "Seeding" states longer than necessary because readiness was gated on **candle composition** (`Composition_Stage`), which tracks GetRange + live candle arrival. Non-candle widgets (Stats, Orderbook, Trades, Heatmap, etc.) have independent data sources that arrive within seconds, yet they were blocked by slow candle backfill — especially problematic on 1m/5m/15m timeframes where GetRange fetches more data.

### Before S124

```
resolve_pane_visual_state flow:
  Offline/Desync/Critical/Empty → early return
  Range_Pending → Loading (ALL widgets)          ← Problem: Stats/OB/Trades blocked
  Live_Only|Backfilled → widget snapshot check → Seeding  ← Problem: widgets with data still Seeding
  Composed → Active                               ← Only candle-complete state
```

A Stats widget that received its snapshot in 200ms still showed "Loading" for 3-5s while candle GetRange completed.

## Solution

Restructured `resolve_pane_visual_state` with **widget-specific readiness paths** that check each widget's own data store before falling through to candle composition:

```
resolve_pane_visual_state flow (S124):
  Offline/Desync/Critical/Empty → early return (unchanged)
  Widget-specific readiness:
    Stats:        store.count > 0 → Active | has_live → Snapshot_Pending | Loading
    Orderbook:    bid/ask > 0     → Active | has_live → Snapshot_Pending | Loading
    DOM:          bid/ask > 0     → Active | has_live → Snapshot_Pending | Loading
    Trades:       store.count > 0 → Active | has_live → Seeding | Loading
    Counter:      candle.count > 0→ Active | has_live → Seeding | Loading
    Heatmap:      store.count > 0 → Active | has_live → Seeding | Loading
    VPVR:         store.count > 0 → Active | has_live → Seeding | Loading
    Analytics:    store.count > 0 → Active | has_live → Seeding | Loading
    Session_VPVR: store.count > 0 → Active | has_live → Seeding | Loading
    TPO:          period_count > 0→ Active | has_live → Seeding | Loading
  Candle/Empty: composition-driven (Range_Pending→Loading, Live_Only→No_History, etc.)
```

### Widget Readiness Classification

| Widget | Data Source | Ready When | Independent of Candle? |
|--------|-----------|-----------|----------------------|
| **Stats** | Stats snapshot | `count > 0` | Yes |
| **Orderbook** | OB snapshot | `bid_count > 0 \|\| ask_count > 0` | Yes |
| **DOM** | OB snapshot | Same as Orderbook | Yes |
| **Trades** | Trade feed | `count > 0` | Yes |
| **Counter** | Candle data | `candle.count > 0` | Yes (uses live candles) |
| **Heatmap** | HM snapshots | `count > 0` | Yes |
| **VPVR** | VPVR buckets | `count > 0` | Yes |
| **Analytics** | Async range fetch | `count > 0` | Yes |
| **Session_VPVR** | HTTP fetch | `count > 0` | Yes |
| **TPO** | HTTP fetch | `period_count > 0` | Yes |
| **Candle** | GetRange + live | Composition = Composed | No (canonical) |

## Files Changed

| File | Change |
|------|--------|
| `client/src/core/app/shell_common.odin` | Rewritten `resolve_pane_visual_state` with per-widget readiness paths |
| `client/src/core/app/marketdata_test.odin` | Updated 5 existing tests, added 12 new S124 tests |

## Tests

- **Updated tests (5):** Fixed to use realistic `has_live_data` values and new expected states
- **New tests (12):**
  - `test_s124_stats_active_during_range_pending` — Stats Active while candle GetRange flies
  - `test_s124_orderbook_active_during_range_pending` — OB Active during Range_Pending
  - `test_s124_trades_active_with_data` — Trades Active during Live_Only
  - `test_s124_counter_active_with_candles` — Counter Active with live candles
  - `test_s124_heatmap_active_with_data` — Heatmap Active during Range_Pending
  - `test_s124_analytics_active_with_data` — Analytics Active during Range_Pending
  - `test_s124_candle_still_composition_driven` — Candle still respects composition
  - `test_s124_stats_loading_no_live` — Stats Loading when no data at all
  - `test_s124_vpvr_active_with_data` — VPVR Active during Live_Only
  - `test_s124_session_vpvr_active_with_data` — Session VPVR Active during Backfilled
  - `test_s124_tpo_active_with_data` — TPO Active during Range_Pending
  - `test_s124_universal_gates_still_override` — Offline/Desync/Critical override data

**Total:** 313 app tests passing, 401 md_common tests passing

## Design Decisions

1. **Store-driven readiness over composition-driven:** Each widget checks its own data store `count > 0` rather than relying on candle composition stage. This is the minimal, correct gate.

2. **Three-tier fallback per widget:** `has_data → Active | has_live_data → transitional | Loading`. The middle tier distinguishes between "stream connected, awaiting widget-specific data" vs "nothing flowing yet".

3. **Candle remains composition-driven:** Candle widget still uses the full `Composition_Stage` flow (Range_Pending → Loading, Live_Only → No_History, Backfilled → Seeding, Composed → Active). This is correct because candle rendering depends on historical + live coherence.

4. **Universal gates preserved:** Offline, Desync, Critical health, and Empty still override all widget-specific readiness. These represent system-level issues that no widget data can compensate for.

5. **No new enums or types:** The change is purely in the resolution logic. `Pane_Visual_State` enum unchanged, `Cell_Stores` unchanged. Zero new abstractions.

## Operational Impact

- **Stats/OB/Trades widgets:** Now render within ~200ms of stream connection (was 3-5s on 1m TF)
- **1m/5m/15m timeframes:** Dramatically fewer panes stuck in Loading/Seeding
- **Candle widget:** Unchanged behavior (composition-driven)
- **Zero regressions:** All existing overlays (Offline, Error, Empty, No_History) work identically
