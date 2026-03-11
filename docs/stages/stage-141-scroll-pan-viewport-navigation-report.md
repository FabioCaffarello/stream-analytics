# Stage 141 — Scroll, Pan & Viewport Navigation

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Enable professional chart navigation with viewport-aware scroll, pan, zoom, and explicit live-follow / manual-viewport modes. All overlays (candles, analytics subplots, time axis labels) must track the same viewport.

## Changes

### 1. Home/End Key Navigation (`chart_interaction.odin`, `ports/input.odin`)

- **Home key**: Snap to live edge (`scroll_x = 0`)
- **End key**: Snap to oldest candle (max scroll offset)
- Added `Home` and `End` to `ports.Key` enum

### 2. Web Platform Key Expansion (`input.js`, `runtime.js`, `marketdata_web.odin`, `main.odin`)

- Expanded key bitmap from 32-bit single word to lo/hi dual word
- Added `key_state_hi`, `key_pressed_state_hi`, `key_released_state_hi` FFI functions
- Mapped: `D` (bit 31 lo), `Delete` (bit 0 hi), `Home` (bit 1 hi), `End` (bit 2 hi)
- Fixed long-standing D key not being wired on web (was documented as limitation in S46)

### 3. Viewport Mode Queries (`chart_interaction.odin`)

- `chart_is_live(pane)` — true when at live edge
- `chart_scroll_fraction(pane, candle_count)` — 0..1 scroll position
- `chart_effective_visible(pane, candle_count)` — resolved visible count

### 4. "Return to Live" HUD (`chart_interaction.odin`, `build_cell.odin`)

- Green "> LIVE" pill button in top-right of chart when scrolled away from live edge
- Click returns to live edge
- Scroll position bar at bottom of chart showing viewport position in history
- Offset label showing candle count from live edge (e.g., "-50")
- Wired into both legacy cell path and contract-based pane path

### 5. Subplot Viewport Synchronization (`layer_strategies.odin`, `layer_canvas.odin`)

- Added `subplot_viewport_window()` — applies Chart_Viewport windowing to analytics entries
- CVD, Delta Volume, and OI subplots now respect scroll offset and visible count
- Y-axis scaling computed on windowed range only (proper auto-scale when scrolled)
- Labels show rightmost visible entry, not overall latest
- `chart_viewport` passed to subplot Layer_Context in both cell and compare paths

### 6. Viewport Modes (implicit, no new state)

- **Live-follow** (`scroll_x == 0`): chart tracks newest candle, new data appears on right
- **Manual viewport** (`scroll_x > 0`): chart locked to historical position
- Return to live: Home key, "> LIVE" button, or arrow-key to scroll_x == 0

## Files Changed

| File | Change |
|------|--------|
| `client/src/core/ports/input.odin` | +Home, +End keys |
| `client/src/core/app/chart_interaction.odin` | Home/End keys, viewport queries, "Return to Live" HUD |
| `client/src/core/app/chart_interaction_test.odin` | +10 tests (Home/End, viewport queries, HUD) |
| `client/src/core/app/build_cell.odin` | Wire HUD into both cell paths |
| `client/src/core/app/layer_canvas.odin` | Pass chart_viewport to subplot contexts |
| `client/src/core/layers/layer_strategies.odin` | subplot_viewport_window, windowed CVD/DV/OI |
| `client/src/core/layers/layers_test.odin` | +7 tests (viewport windowing) |
| `client/web/modules/input.js` | Lo/hi key state, Home/End/D mapping |
| `client/web/runtime.js` | Wire hi key state functions |
| `client/src/platform/web/marketdata_web.odin` | +3 foreign procs (hi key state) |
| `client/src/platform/web/main.odin` | Decode hi keys, fix D key mapping |

## Test Results

- **App tests:** 395 pass (was 377, +18 new)
- **Layers tests:** 47 pass (was 40, +7 new)
- **Total:** 442 tests, all green
- **WASM compile:** OK
- **Core check:** All 10 packages OK

## New Tests

### chart_interaction_test.odin (+10)
1. `test_chart_home_snaps_to_live` — Home key resets scroll_x to 0
2. `test_chart_home_at_live_no_change` — Home at live edge is no-op
3. `test_chart_end_snaps_to_oldest` — End key sets max scroll
4. `test_chart_is_live` — Viewport mode query
5. `test_chart_scroll_fraction` — Fraction 0/0.5/1.0
6. `test_chart_effective_visible_auto` — Auto zoom resolves to 140
7. `test_chart_effective_visible_explicit` — Explicit zoom clamped
8. `test_draw_return_to_live_hidden_at_live_edge` — No HUD at live
9. `test_draw_return_to_live_shows_when_scrolled` — HUD renders without crash

### layers_test.odin (+7)
1. `test_subplot_viewport_window_auto` — Zero viewport = full range
2. `test_subplot_viewport_window_scrolled` — Correct start/count
3. `test_subplot_viewport_window_at_live_edge` — scroll=0 with explicit visible
4. `test_subplot_viewport_window_clamped` — visible > total
5. `test_subplot_viewport_window_empty` — 0 entries
6. `test_subplot_cvd_viewport_windowed_render` — CVD with viewport window
7. `test_subplot_delta_vol_viewport_windowed` — DV bar count matches window

## Architecture Notes

- No new state fields — viewport mode is implicit from `scroll_x == 0`
- Backward compatible — zero Chart_Viewport = auto behavior unchanged
- Subplot windowing uses proportional index mapping (not timestamp-based)
- Web key state expanded to 64-bit via lo/hi word pair
- D key (Ctrl+D snapshot) now correctly wired on web

## Input Summary

| Input | Mode | Effect |
|-------|------|--------|
| Scroll wheel | Any | Horizontal scroll through history |
| Ctrl+Scroll | Any | Zoom (change visible candle count) |
| Left drag | Any | Pan (smooth horizontal scroll) |
| Left/Right arrow | Any | Scroll ±1 candle |
| Home | Manual | Snap to live edge |
| End | Live | Snap to oldest candle |
| "> LIVE" button | Manual | Click to return to live edge |

## Zero Regressions

- All pre-existing 395 app + 40 layers tests pass unchanged
- WASM compile clean
- No wire-breaking changes
