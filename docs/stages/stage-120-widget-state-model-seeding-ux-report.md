# Stage 120 — Widget State Model & Seeding UX

**Status:** COMPLETE
**Date:** 2026-03-09

## Objective

Transform empty and transitional widget states into useful, professional, and informative displays. Eliminate the "dead pane" feeling by giving users clear visual feedback about what each widget is doing and what it needs to become active.

## Changes

### 1. Expanded Pane Visual States (shell_common.odin)

Added two new states to `Pane_Visual_State`:

| State | Trigger | Description |
|-------|---------|-------------|
| `Active` | Composed data | Normal rendering, no overlay |
| `Loading` | Range_Pending | Historical data fetch in progress |
| `Seeding` | Live_Only/Backfilled (generic) | Live feed active, data accumulating |
| **`Snapshot_Pending`** | **Live but store empty (Stats/OB/DOM)** | **Widget awaits initial snapshot** |
| `Empty` | No stream bound | No data source configured |
| **`No_History`** | **Candle + Live_Only** | **Live candles flowing, no backfill** |
| `Offline` | Connection down | Server unreachable |
| `Error` | Desync/Critical | Stream integrity failure |

### 2. Widget-Aware State Resolution

`resolve_pane_visual_state()` now accepts `widget_kind` and `stores` parameters (with defaults for backward compat):

- **Stats** with empty `Stats_Store.count == 0` → `Snapshot_Pending`
- **Orderbook/DOM** with empty `bid_count + ask_count == 0` → `Snapshot_Pending`
- **Candle** with `Live_Only` → `No_History` (was generic `Seeding`)
- All other widgets with `Live_Only/Backfilled` → `Seeding` (unchanged)

### 3. Richer Overlay Rendering

`draw_pane_state_overlay()` enhanced with:

- **Widget glyph**: Large muted letter (C/S/T/B/V/H/D/A/P/#) centered above title in panes ≥100px tall
- **Animated progress bar**: Oscillating indicator for Loading/Seeding/Snapshot_Pending states (120-frame cycle, ~2s at 60fps)
- **Frame-aware animation**: Accepts `frame_seq` parameter to drive progress bar position

### 4. Improved Messaging

All sub-label strings rewritten for clarity:

| State | Old | New |
|-------|-----|-----|
| Empty (Candle) | "No market stream bound" | "Bind a market stream to see candles" |
| Empty (Stats) | "No market stream bound" | "Bind a market stream to see stats" |
| Empty (.Empty) | "Select a widget type" | "Select a widget type from the catalog" |
| Seeding (Stats) | "Stats snapshot pending" | "Stats accumulating" |
| Seeding (Orderbook) | "Book snapshot pending" | "Book levels populating" |

New state messages:
- **Snapshot_Pending**: "Awaiting Snapshot" + "Waiting for first stats snapshot" / "Waiting for order book snapshot" / etc.
- **No_History**: "Live Only" + "Live candles only, no historical backfill"

### 5. Layer Canvas Fallback Improvement (layer_canvas.odin)

- "Empty" text → centered "No widget selected"
- "Waiting stream {hex}" → centered "Waiting for stream" (human-readable)
- Both now centered in viewport when text measure is available

## Files Modified

| File | Changes |
|------|---------|
| `shell_common.odin` | Expanded enum, widget-aware resolver, glyph + progress bar overlay, 4 new sub-label procs |
| `build_cell.odin` | Pass `widget_kind` + `stores` to resolver, `frame_seq` to overlay (both render paths) |
| `layer_canvas.odin` | Centered fallback text, human-readable messages |
| `marketdata_test.odin` | Updated 2 existing tests, added 11 new tests |

## Tests

**280 tests total, all passing.**

New tests (11):
- `test_pane_visual_state_snapshot_pending_stats` — Stats with empty store → Snapshot_Pending
- `test_pane_visual_state_stats_with_data` — Stats with data → Seeding
- `test_pane_visual_state_snapshot_pending_orderbook` — OB with no levels → Snapshot_Pending
- `test_pane_visual_state_snapshot_pending_dom` — DOM with no levels → Snapshot_Pending
- `test_pane_visual_state_no_history_candle` — Candle Live_Only → No_History
- `test_pane_visual_state_candle_backfilled` — Candle Backfilled → Seeding
- `test_pane_visual_state_live_only_trades` — Non-candle Live_Only → Seeding
- `test_widget_state_glyph_coverage` — All non-Empty kinds have glyph
- `test_state_sub_label_snapshot_pending_all` — All kinds have snapshot_pending label
- `test_state_sub_label_no_history_all` — All kinds have no_history label
- (updated) `test_pane_visual_state_live_only` — Now expects No_History for Candle

## Backward Compatibility

- `resolve_pane_visual_state` new parameters have defaults — all existing callers compile unchanged
- `draw_pane_state_overlay` new `frame_seq` parameter defaults to 0 — static progress bar if not passed
- No wire changes, no schema changes, no persistence impact

## Visual State Summary

```
Connection offline ──────────────────────────── Offline
Stream desync / Critical health ─────────────── Error
No stream bound + Empty composition ─────────── Empty
Range_Pending ───────────────────────────────── Loading     [progress bar]
Stats/OB/DOM + Live but store empty ─────────── Snapshot_Pending [progress bar]
Candle + Live_Only ──────────────────────────── No_History
Live_Only/Backfilled (other) ────────────────── Seeding     [progress bar]
Composed ────────────────────────────────────── Active      [no overlay]
```
