# Stage S140 — Time Axis & Timestamp System

**Status:** COMPLETE
**Date:** 2026-03-09

## Summary

Added a professional time axis to the candle chart, rendering timestamp labels and vertical grid lines along the bottom of the chart viewport. The time axis adapts its label format, density, and grid intensity based on the active timeframe, zoom level, and scroll position. Visual indicators communicate live edge, data sparsity, and day boundaries.

## Changes

### New Files
- `client/src/core/layers/time_axis.odin` — Time axis rendering engine

### Modified Files
- `client/src/core/layers/layer_api.odin` — Added `tf_ms: i64` to `Layer_Context`
- `client/src/core/layers/layer_strategies.odin` — Chart viewport split for time axis strip, call `time_axis_render`
- `client/src/core/app/app.odin` — Added `frame_tf_ms: i64` to `App_State`
- `client/src/core/app/layer_canvas.odin` — Thread `tf_ms` through canvas rendering pipeline
- `client/src/core/app/widget_contract.odin` — Pass `ctx.tf_ms` to frame-local state in candle contract
- `client/src/core/app/build_compare.odin` — Set `frame_tf_ms` for compare pane rendering
- `client/src/core/layers/layers_test.odin` — 17 new tests, 3 existing tests updated

## Architecture

### Time Axis Strip
- Bottom 16px of the candle chart viewport is reserved for time labels
- Price candle rendering uses a reduced `chart_vp` (viewport minus TIME_AXIS_H)
- Grid lines extend upward from the time axis into the chart area
- Only active on candle-type widgets with sufficient viewport height (> 48px)

### Label Policy by Timeframe
| TF Tier | Format | Example |
|---------|--------|---------|
| Sub-minute (1s, 5s) | `HH:MM:SS` | `14:30:45` |
| Minute (1m, 5m) | `HH:MM` | `14:30` |
| Hourly+ (15m..4h) | `HH:MM` | `14:00` |
| Day boundaries | `DD Mon` | `10 Mar` |
| Daily (1d) | `DD Mon` | `10 Mar` |

### Adaptive Label Spacing
- Minimum 64px between labels to prevent overlap
- Raw interval snapped to "nice" values per timeframe tier:
  - 1s: 5, 10, 15, 30, 60
  - 1m: 5, 10, 15, 30, 60
  - 1h: 2, 4, 6, 12, 24
  - 1d: 2, 5, 7, 14, 30
- Day boundaries always get a label regardless of interval

### Visual Indicators
- **Grid lines**: Subtle vertical lines at label positions (alpha 0.04), stronger at day boundaries (alpha 0.10)
- **Live edge**: Green accent line (alpha 0.20) on rightmost candle when `scroll_offset == 0`
- **LIVE ONLY badge**: Yellow accent text when candle count < 10 and at live edge (no backfill history)

### Data Flow
```
render_candle_contract → state.frame_tf_ms = ctx.tf_ms
                       → render_cell_layer_canvas
                            → state.frame_tf_ms = TF_OPTION_MS[active_tf_idx]
                            → render_subject_layer_canvas_with_analytics
                                 → Layer_Context.tf_ms = state.frame_tf_ms
                                      → price_candles_render
                                           → time_axis_render(...)
```

### Civil Date Algorithm
- Uses Howard Hinnant's chrono algorithm for Unix ms → (day, month, year) conversion
- Zero external dependencies, O(1) computation
- Tested for epoch (1970-01-01), 2024, and 2026

## Tests

17 new tests added:

| Test | Coverage |
|------|----------|
| `test_time_axis_format_time_label_sub_minute` | HH:MM:SS for 1s TF |
| `test_time_axis_format_time_label_minute` | HH:MM for 1m TF |
| `test_time_axis_format_time_label_hourly` | HH:MM for 1h TF |
| `test_time_axis_format_day_label` | DD Mon format |
| `test_time_axis_format_time_label_daily` | Daily TF → DD Mon |
| `test_time_axis_unix_ms_to_date` | 2024-03-10 decomposition |
| `test_time_axis_unix_ms_to_date_epoch` | Unix epoch (1970-01-01) |
| `test_time_axis_unix_ms_to_date_2026` | 2026-03-09 decomposition |
| `test_time_axis_snap_interval_1s` | raw=3 → 5 for 1s |
| `test_time_axis_snap_interval_1m` | raw=7 → 10 for 1m |
| `test_time_axis_snap_interval_1h` | raw=3 → 4 for 1h |
| `test_time_axis_snap_interval_raw_1` | raw=1 always returns 1 |
| `test_time_axis_render_emits_primitives` | 20 candles, labels + live edge |
| `test_time_axis_render_live_only_badge` | LIVE ONLY with < 10 candles |
| `test_time_axis_no_live_edge_when_scrolled` | No green line when scrolled |
| `test_time_axis_month_abbrev` | Month abbreviation mapping |

3 existing tests updated for time axis primitive count changes.

## Test Results

```
layers:    40 tests — all pass
app:      386 tests — all pass
md_common: 413 tests — all pass
services: 186 tests — all pass
streams:   16 tests — all pass
─────────────────────────
Total:   1,041 tests — all pass
```

Zero regressions, zero wire-breaking changes.
