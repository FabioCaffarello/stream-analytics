# Stage 95 — Compare Mode Analytics

**Date:** 2026-03-08
**Status:** COMPLETE

## Summary

Adds per-pane analytics subplot support (CVD, Delta Volume, Open Interest) to compare mode candle panes. Each compare pane can independently toggle subplot indicators, with full state isolation between panes.

## Changes

### 1. Per-Pane Subplot Flags (`components.odin`)
- Added `show_cvd`, `show_delta_vol`, `show_oi` as `[4]bool` arrays to `Compare_State`
- Each pane has independent subplot toggles — no cross-pane contamination

### 2. Toggle Action (`app.odin`, `actions.odin`)
- `Toggle_Compare_Subplot` action kind added to `UI_Action_Kind`
- `subplot_idx` field added to `UI_Action` (0=CVD, 1=DeltaVol, 2=OI)
- `apply_toggle_compare_subplot(state, pane_idx, subplot_idx)` — toggles flag and triggers analytics fetch on enable

### 3. Compare Entry Initialization (`actions.odin`)
- `apply_enter_compare` copies subplot flags from focused cell's `Indicator_Component` (or global defaults)
- `apply_add_compare_stream` copies subplot flags from pane 0 (consistent with existing pattern for vol/heatmap/vpvr)
- Subplot analytics data fetched on compare entry for panes with active subplots

### 4. Subplot Pills in Compare Header (`build_compare.odin`)
- Three clickable indicator pills (C/D/O) rendered in pane header when widget is Candle
- Green (CVD), Red (Delta Vol), Cyan (OI) — matching top bar color taxonomy
- Active pills have higher alpha, inactive are dimmed
- Click queues `Toggle_Compare_Subplot` action

### 5. Compare Pane Subplot Rendering (`layer_canvas.odin`, `build_compare.odin`)
- `render_compare_pane_with_subplots` — same viewport splitting as cell subplots (20% per subplot, clamped 30-80px, main ≥ 40%)
- Rendering order: Delta Vol → CVD → OI (top to bottom, consistent with cell subplots)
- Each subplot clipped to its viewport — no overflow between subplots or panes
- Compare Candle panes now route through subplot-aware path when any subplot flag is active

### 6. Subplot Analytics Data Fetch (`stream_slots.odin`)
- `request_compare_pane_subplot_analytics(state, pane_idx)` — fetches all active subplot kinds for a pane
- `request_compare_pane_subplot_analytics_kind(state, pane_idx, kind)` — fetches specific kind, resolves venue/symbol/TF from pane's slot
- Triggered on: compare entry, add stream, TF change, subplot toggle-on
- Uses per-slot `analytics_store` for isolation (same store resolution as existing compare analytics)

### 7. TF Change Integration (`stream_slots.odin`)
- `apply_set_compare_pane_timeframe` now calls `request_compare_pane_subplot_analytics` after TF change
- Analytics store already cleared by existing TF change logic — refetch populates with new TF data

## Files Modified

| File | Change |
|------|--------|
| `components.odin` | `show_cvd/show_delta_vol/show_oi [4]bool` on `Compare_State` |
| `app.odin` | `Toggle_Compare_Subplot` action kind, `subplot_idx` field |
| `actions.odin` | Toggle handler, compare entry/add subplot copy + fetch |
| `build_compare.odin` | Subplot pills UI, candle→subplot routing |
| `layer_canvas.odin` | `render_compare_pane_with_subplots` proc |
| `stream_slots.odin` | Subplot analytics fetch procs, TF change integration |
| `marketdata_test.odin` | 6 new tests |

## Tests

6 new tests in `marketdata_test.odin`:

| Test | Purpose |
|------|---------|
| `test_compare_subplot_flags_per_pane_isolation` | Independent flags across 4 panes |
| `test_compare_subplot_toggle_isolation` | Toggle one pane doesn't affect others |
| `test_compare_subplot_multi_flag_same_pane` | Multiple subplots active on same pane |
| `test_apply_toggle_compare_subplot` | Toggle proc correctness (on/off, all 3 kinds) |
| `test_apply_toggle_compare_subplot_bounds` | Rejects invalid indices and inactive state |
| `test_compare_subplot_flags_zero_init` | Zero-init produces all-false flags |

**Total:** 245 tests (34 app + 22 layers + 189 services), all passing.

## State Isolation Guarantees

1. **Per-pane subplot flags**: Independent `[4]bool` arrays — no shared state
2. **Per-slot analytics store**: Each pane reads from its bound `Stream_View_Slot.analytics_store`
3. **Per-pane TF**: Subplot analytics fetched with pane's effective TF (per-pane or global)
4. **Per-pane viewport**: Subplot rendering clipped to pane's allocated rect
5. **No cross-pane leakage**: Toggle on pane N has zero effect on pane M

## Architecture Notes

- Subplot rendering in compare mode reuses the same `subplot_*_render` functions from `layer_strategies.odin` — no duplication
- The `render_compare_pane_with_subplots` proc mirrors `render_cell_layer_canvas_with_subplots` but takes `subject_id` directly instead of resolving from ECS cell index
- Subplot flags are not persisted in workspace schema (compare mode is transient session state)
