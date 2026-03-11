# Stage 139 — Chart Interaction Foundation

**Date:** 2026-03-09
**Status:** COMPLETE
**Tests:** 823 total (386 app + 24 layers + 413 md_common), all green

## Objective

Consolidate chart interaction as a first-class capability: scroll, zoom, pan, crosshair, and keyboard navigation — all driven through explicit contracts, separated from rendering, and wired into the existing view state + data pipeline.

## Architecture

### Input → View State → Rendering Pipeline

```
Input_State (platform)
    ↓
chart_interaction_update()         ← S139: new
    ↓ writes
Pane_View_State (scroll_x, zoom_level, crosshair)
    ↓ bridges
Entity_World.views[ci] (candle_scroll_x, candle_zoom)
    ↓ feeds
frame_chart_viewport (App_State, frame-local)
    ↓ populates
Layer_Context.chart_viewport
    ↓ consumed by
price_candles_render() (visible_count, scroll_offset)
```

### Key Design Decisions

1. **Pane_View_State is source of truth** — interaction writes to `pane.view`, then bridges to Entity_World for legacy consumers (GetRange, persistence, compare mode).

2. **Frame-local chart viewport** — `App_State.frame_chart_viewport` is set per-cell before layer rendering and consumed by `Layer_Context` creation. Avoids threading scroll/zoom through 3+ function signatures.

3. **Zero = auto** — `Chart_Viewport{scroll_offset=0, visible_count=0}` produces identical behavior to pre-S139 (show last N candles, no scroll). Fully backward compatible.

4. **Separation of concerns** — `chart_interaction.odin` handles input processing exclusively. Rendering (`layer_strategies.odin`) only reads `Chart_Viewport` from context. No interaction logic in rendering code.

## New Files

| File | Purpose |
|------|---------|
| `client/src/core/app/chart_interaction.odin` | Input→view state processor (scroll, zoom, pan, crosshair, keys) |
| `client/src/core/app/chart_interaction_test.odin` | 17 unit tests covering all interaction paths |

## Modified Files

| File | Change |
|------|--------|
| `client/src/core/layers/layer_api.odin` | Added `Chart_Viewport` struct + field on `Layer_Context` |
| `client/src/core/layers/layer_strategies.odin` | `price_candles_render` uses `chart_viewport` for visible range |
| `client/src/core/layers/layers_test.odin` | 2 new tests: viewport scroll + zoom rendering |
| `client/src/core/app/app.odin` | Added `frame_chart_viewport` + `chart_pan` to `App_State` |
| `client/src/core/app/layer_canvas.odin` | Sets `frame_chart_viewport` from views[ci] before layer rendering |
| `client/src/core/app/build_cell.odin` | Calls `chart_interaction_update` for candle panes (both paths) |
| `client/src/core/app/widget_contract.odin` | `render_candle_contract` sets viewport from pane view state |

## Interaction Contracts

| Input | Action | Target |
|-------|--------|--------|
| Scroll wheel (no modifier) | Horizontal scroll through candle history | `pane.view.scroll_x` |
| Ctrl+Scroll | Zoom (change visible candle count) | `pane.view.zoom_level` |
| Left drag on chart body | Pan (smooth horizontal scroll) | `pane.view.scroll_x` |
| Mouse hover in chart body | Crosshair position update | `pane.view.crosshair` |
| Left arrow key | Scroll 1 candle into history | `pane.view.scroll_x` |
| Right arrow key | Scroll 1 candle toward live edge | `pane.view.scroll_x` |

## Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `CHART_SCROLL_SPEED` | 3.0 | Candles per scroll tick |
| `CHART_ZOOM_SPEED` | 0.1 | Zoom factor per scroll tick |
| `CHART_ZOOM_MIN` | 10 | Minimum visible candles |
| `CHART_ZOOM_MAX` | 500 | Maximum visible candles |
| `CHART_PAN_SENSITIVITY` | 1.0 | Pixels per candle during drag |

## Test Coverage

### chart_interaction_test.odin (17 tests)
- Scroll wheel scrolls into history
- Scroll clamps to zero (live edge)
- Scroll clamps to max (oldest)
- Scroll ignored outside body rect
- Ctrl+scroll zooms in/out
- Zoom clamped to candle count
- Crosshair active when hovering
- Crosshair deactivates when leaving
- Left/right arrow scrolls one candle
- Max scroll calculation (auto/explicit zoom, zero candles)
- Reset to live edge
- Sync to Entity_World (normal + out of bounds)
- No candles does nothing

### layers_test.odin (2 new tests)
- Viewport scroll: 10 candles, visible_count=5, scroll_offset=3 → renders 5 candles
- Viewport zoom: 20 candles, visible_count=10 → renders 10 candles

## Backward Compatibility

- All existing rendering unchanged when `Chart_Viewport` is zero-valued (default)
- GetRange lazy loading continues to read from `Entity_World.views[ci]` (bridged)
- Compare mode scroll/zoom arrays untouched (future: wire through same mechanism)
- Persistence reads from Entity_World (bridged)
- No wire-breaking changes, no schema version bump required

## Future Enablers

- **Live edge snapping**: when `scroll_x == 0`, auto-advance with new candles
- **Draw tools**: crosshair tracking provides price-at-y for annotations
- **Synchronized scroll**: multiple candle panes can share scroll_x
- **Heatmap/VPVR alignment**: `Chart_Viewport` available on `Layer_Context` for all strategies
- **Compare mode**: wire `chart_interaction_update` through compare pane rendering
