# Stage 94 — Analytics on Chart Runtime

## Objective

Port CVD and Delta Volume subplots to the real chart runtime, based on the
`mr:layers` pipeline. Also add OI subplot for completeness. All three analytics
subplots render as graphical visualizations below the main candle chart,
controlled by per-cell `show_cvd`, `show_delta_vol`, and `show_oi` flags.

## Architecture

### Subplot rendering flow

```
build_cell.odin: render_cell_widget()
  → render_cell_layer_canvas(state, ci, .Candle, cell_vp)
    → check indicators[ci].show_cvd / show_delta_vol / show_oi
    → if any active:
        render_cell_layer_canvas_with_subplots()
          1. Split cell_vp into main_vp (upper) + subplot strips (lower)
          2. Render main chart bundle in main_vp
          3. For each active subplot (DV → CVD → OI order):
             - Create subplot viewport
             - Call subplot_*_render() → Layer_Outputs → canvas_render_outputs()
```

### Viewport splitting

- Each subplot gets 20% of cell height, clamped to [30px, 80px]
- Main chart always gets at least 40% of viewport
- Subplots are clipped to their viewport via `Cmd_Clip_Push/Pop`

### Subplot render functions

Three public render functions in `layers/layer_strategies.odin`:

| Function | Kind | Visualization |
|----------|------|---------------|
| `subplot_cvd_render` | CVD | Line chart of cumulative volume delta |
| `subplot_delta_vol_render` | Delta_Volume | Vertical bars (positive up, negative down) |
| `subplot_oi_render` | Open_Interest | Line chart of open interest |

All subplot renders:
- Use `analytics_collect_by_kind()` to gather up to 48 entries (oldest first)
- Draw background + top divider line
- Emit primitives at z=23 (bg), z=24 (divider/zero), z=25 (data), z=26 (label)
- Show latest value label in top-left corner

### Data path

Subplots consume the same `Analytics_Store` ring buffer used by the existing
Analytics layer. No new data sources or pipelines were introduced.

## Changes

### New files
- None

### Modified files

| File | Change |
|------|--------|
| `services/analytics_store.odin` | Added `analytics_collect_by_kind()` — collect N entries of a specific kind in oldest-first order |
| `services/settings_store.odin` | Added `SETTING_SHOW_CVD`, `SETTING_SHOW_DELTA_VOL`, `SETTING_SHOW_OI` keys + known_keys |
| `layers/layer_api.odin` | Added `Subplot_Flags` struct + `subplot_flags_count()` + field on `Layer_Context` |
| `layers/layer_strategies.odin` | Added `subplot_cvd_render`, `subplot_delta_vol_render`, `subplot_oi_render`, helpers `subplot_push_bg`, `subplot_val_to_y`; removed unused `core:math` import |
| `app/layer_canvas.odin` | Added `render_cell_layer_canvas_with_subplots()` — viewport splitting + subplot orchestration |
| `app/settings.odin` | Extended indicator toggles from 8 to 11 (added CVD Subplot, Delta Vol Subplot, Open Interest) |
| `app/actions.odin` | Extended `set_indicator_on_cell` and `toggle_focused_indicator` from 8 to 11 indicators |
| `app/top_bar.odin` | Extended indicator pills from 8 to 11 (C=CVD, D=DeltaVol, O=OI) |
| `app/app.odin` | Added settings loading for `show_cvd`, `show_delta_vol`, `show_oi` on startup |
| `widgets/chart_types.odin` | Added `oi_enabled`, `oi_rendered` to `Indicator_Render_Probe` |
| `layers/layers_test.odin` | Added 7 new tests for subplot rendering + helpers |

## Tests added

| Test | Validates |
|------|-----------|
| `test_subplot_cvd_renders_line_segments` | CVD subplot emits line segments + label for 5 entries |
| `test_subplot_delta_vol_renders_bars` | DV subplot emits 4 bars for 4 entries |
| `test_subplot_oi_renders_line_segments` | OI subplot emits line segments + label for 3 entries |
| `test_subplot_cvd_no_data_emits_nothing` | CVD subplot with no CVD data produces 0 primitives |
| `test_subplot_cvd_single_entry_needs_two` | CVD subplot with 1 entry (need 2 for lines) produces 0 primitives |
| `test_subplot_flags_count` | `subplot_flags_count` returns correct count for 0/1/2/3 flags |
| `test_analytics_collect_by_kind` | `analytics_collect_by_kind` returns correct entries in oldest-first order |

## Design decisions

1. **Subplots are NOT layer strategies** — they are called directly from `layer_canvas.odin`,
   not through `layer_registry_render_bundle`. This avoids adding new `Layer_ID` enum values
   and keeps the bundle mask system unchanged.

2. **Deterministic subplot ordering** — Delta Vol → CVD → OI (top to bottom). This order
   matches the typical analysis flow (volume delta first, then cumulative, then structural).

3. **Viewport isolation** — Each subplot gets its own `Cmd_Clip_Push/Pop` pair, preventing
   any render primitive from bleeding into adjacent subplots.

4. **No new data pipelines** — Subplots consume the existing `Analytics_Store` ring buffer.
   The `analytics_collect_by_kind` helper provides oldest-first iteration for plotting.

## Acceptance criteria

- [x] CVD subplot visible below candle chart when `show_cvd` is enabled
- [x] Delta Volume subplot visible below candle chart when `show_delta_vol` is enabled
- [x] OI subplot visible below candle chart when `show_oi` is enabled
- [x] Flags persisted via settings store (`show_cvd`, `show_delta_vol`, `show_oi`)
- [x] Toggles available in settings page, top bar pills, and keyboard shortcuts
- [x] No runtime duplication — single chart runtime, single data pipeline
- [x] Deterministic render ordering: main chart → DV subplot → CVD subplot → OI subplot
- [x] All core packages compile cleanly (`make check-core`)
- [x] 7 new tests pass
