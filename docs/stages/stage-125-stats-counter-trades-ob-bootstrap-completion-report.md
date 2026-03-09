# Stage 125 — Stats/Counter/Trades/OB Bootstrap Completion

**Date:** 2026-03-09
**Status:** COMPLETE

## Problem

Non-candle widgets (Stats, Orderbook, DOM) were stuck in **Snapshot_Pending** when only candle data was flowing. The root cause: `Cell_Surface_View.has_live_data` is a **generic flag** set when ANY artifact has live data. So when candles arrived first (the common case), the Stats/OB/DOM widgets showed "Live feed active, snapshot incoming" when the widget-specific artifact hadn't started flowing yet.

The correct behavior:
- **Loading** → no data at all
- **Seeding** → stream connected (other artifacts live), waiting for this artifact to start
- **Snapshot_Pending** → THIS artifact's live channel is active, store still empty
- **Active** → data in store

## Root Cause

In `resolve_pane_visual_state` (shell_common.odin), the transition from Loading → Snapshot_Pending used `sv.has_live_data` which is true when ANY artifact is live:

```odin
case .Stats:
    if stores.stats != nil && stores.stats.count > 0 do return .Active
    if sv.has_live_data do return .Snapshot_Pending  // ← BUG: candles live ≠ stats live
    return .Loading
```

## Solution

### 1. Per-artifact live flags on Cell_Surface_View

Added `artifact_has_live: [Artifact_Kind]bool` to `Cell_Surface_View`, populated from the resolved `Stream_Apply_State.has_live` in both `resolve_cell_surface_view_with_stores` and `resolve_compare_surface_view`.

### 2. Widget-specific readiness in visual state resolution

Changed `resolve_pane_visual_state` to use per-artifact flags for Stats, Orderbook, and DOM:

```odin
case .Stats:
    if stores.stats != nil && stores.stats.count > 0 do return .Active
    if sv.artifact_has_live[.Stats] do return .Snapshot_Pending   // stats artifact live, store empty
    if sv.has_live_data do return .Seeding                        // stream connected, waiting for stats
    return .Loading
case .Orderbook:
    if stores.orderbook != nil && (ob.bid_count > 0 || ob.ask_count > 0) do return .Active
    if sv.artifact_has_live[.Orderbook] do return .Snapshot_Pending  // OB events flowing, no snapshot
    if sv.has_live_data do return .Seeding                           // stream connected, waiting for OB
    return .Loading
```

### 3. Behavioral improvements

| Widget | Before (candles live, widget not) | After |
|--------|-----------------------------------|-------|
| Stats | Snapshot_Pending ("snapshot incoming") | **Seeding** ("waiting for live feed") |
| Orderbook | Snapshot_Pending ("snapshot incoming") | **Seeding** ("waiting for live feed") |
| DOM | Snapshot_Pending ("snapshot incoming") | **Seeding** ("waiting for live feed") |
| Trades | Seeding (unchanged) | Seeding (unchanged) |
| Counter | Seeding (unchanged) | Seeding (unchanged) |

Snapshot_Pending now only shows when the **widget-specific artifact** is confirmed live but the store is still empty — a brief transitional state, not a prolonged stuck state.

## Files Modified

| File | Change |
|------|--------|
| `client/src/core/app/stream_slots.odin` | Added `artifact_has_live` to `Cell_Surface_View`, populated in both surface view resolvers |
| `client/src/core/app/shell_common.odin` | Per-artifact readiness in `resolve_pane_visual_state` for Stats/OB/DOM |
| `client/src/core/app/marketdata_test.odin` | Updated 3 existing tests, added 9 new tests |

## Tests

- **9 new tests**: `test_s125_*` — bootstrap lifecycle for Stats/OB/Trades/Counter, per-artifact flag propagation, seeding-vs-snapshot_pending distinction
- **3 updated tests**: Snapshot_Pending tests now set artifact-specific live flags
- **321 app tests pass**, 401 md_common tests pass, 186 services tests pass
- **Total: 908 tests, all green**

## Key Insight

The fix is structurally minimal — no new state machines, no timeouts, no new fields on apply_state. The per-artifact live flags were already available in `Stream_Apply_State.has_live`; they just weren't surfaced through `Cell_Surface_View` to the visual state resolver. One new field + 3 lines of logic change per widget.
