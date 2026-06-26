package app

// S139/S141: Chart Interaction — Scroll, Pan & Viewport Navigation.
//
// Processes input events (scroll, drag, hover) for chart panes and updates
// Pane_View_State accordingly. Separated from rendering to keep interaction
// logic reusable across candle charts, compare panes, and future chart types.
//
// Input contracts:
//   - Scroll wheel (no modifier): zoom (change visible candle count)
//   - Ctrl+Scroll: horizontal scroll through candle history
//   - Left drag on chart body: pan (smooth horizontal scroll)
//   - Mouse hover: crosshair position update
//   - Left/Right arrow: scroll ±1 candle
//   - Home key: jump to live edge (scroll_x = 0)
//   - End key: jump to oldest candle
//
// Viewport modes:
//   - Live-follow (scroll_x == 0): chart tracks newest candle, new data appears on right
//   - Manual viewport (scroll_x > 0): chart locked to historical position
//
// All mutations target Pane_View_State (pane.view) as source of truth.
// Entity_World bridge is maintained by chart_interaction_sync_to_world.

import "core:fmt"
import "mr:ports"
import "mr:ui"
import "mr:widgets"

// --- Constants ---

CHART_SCROLL_SPEED     :: f32(3.0)   // candles per scroll tick
CHART_ZOOM_SPEED       :: f32(0.1)   // zoom factor per scroll tick
CHART_ZOOM_MIN         :: f32(10)    // minimum visible candles
CHART_ZOOM_MAX         :: f32(500)   // maximum visible candles
CHART_PAN_SENSITIVITY  :: f32(1.0)   // pixels per candle during drag

// --- Pan (drag) tracking ---

Chart_Pan_State :: struct {
	active:     bool,
	start_x:    f32,     // mouse X at drag start
	start_scroll: f32,   // scroll_x at drag start
	pane_id:    Pane_ID, // which pane is being panned
}

// --- Core interaction processor ---

// Process chart input for a focused candle pane.
// Returns true if any view state was modified (caller should trigger re-render).
chart_interaction_update :: proc(
	pane: ^Pane,
	input: ports.Input_State,
	pointer: ui.Pointer_Input,
	body_rect: ui.Rect,
	candle_count: int,
	pan: ^Chart_Pan_State,
) -> bool {
	if pane == nil do return false
	if candle_count <= 0 do return false
	if body_rect.size.x <= 0 || body_rect.size.y <= 0 do return false

	// Only process input when mouse is within the chart body.
	mouse_in_body := ui.rect_contains(body_rect, input.mouse.pos)
	changed := false

	// --- Scroll wheel: zoom or horizontal scroll ---
	if mouse_in_body && (input.mouse.scroll.y != 0 || input.mouse.scroll.x != 0) {
		if input.modifiers.ctrl {
			// Ctrl+Scroll = horizontal pan through history.
			// scroll.y is primary (vertical wheel), scroll.x for horizontal trackpad.
			scroll_delta := input.mouse.scroll.y
			if scroll_delta == 0 do scroll_delta = -input.mouse.scroll.x
			new_scroll := pane.view.scroll_x + scroll_delta * CHART_SCROLL_SPEED
			max_scroll := chart_max_scroll(pane, candle_count)
			new_scroll = clamp(new_scroll, 0, max_scroll)
			if new_scroll != pane.view.scroll_x {
				pane.view.scroll_x = new_scroll
				changed = true
			}
		} else {
			// Scroll = zoom: change visible candle count.
			zoom_delta := -input.mouse.scroll.y * CHART_ZOOM_SPEED
			old_zoom := pane.view.zoom_level
			if old_zoom <= 0 do old_zoom = f32(min(candle_count, 140))
			new_zoom := old_zoom * (1.0 + zoom_delta)
			new_zoom = clamp(new_zoom, CHART_ZOOM_MIN, CHART_ZOOM_MAX)
			// S147-BUG-04: Don't exceed candle count, but never go below CHART_ZOOM_MIN.
			// When candle_count < CHART_ZOOM_MIN, keep zoom at minimum (candles rendered
			// at a reasonable width with blank space rather than oversized bodies).
			new_zoom = min(new_zoom, max(f32(candle_count), CHART_ZOOM_MIN))
			if new_zoom != old_zoom {
				pane.view.zoom_level = new_zoom
				changed = true
			}
		}
	}

	// --- Left drag: pan ---
	if mouse_in_body && pointer.left_pressed && !pan.active {
		// Start pan.
		pan.active = true
		pan.start_x = input.mouse.pos.x
		pan.start_scroll = pane.view.scroll_x
		pan.pane_id = pane.id
	}

	if pan.active && pan.pane_id == pane.id {
		if pointer.left_down {
			// Active drag: compute scroll from horizontal displacement.
			visible := pane.view.zoom_level > 0 ? pane.view.zoom_level : f32(min(candle_count, 140))
			pixels_per_candle := body_rect.size.x / max(visible, 1)
			dx := pan.start_x - input.mouse.pos.x // left drag = scroll into history
			candle_delta := dx / max(pixels_per_candle, 1) * CHART_PAN_SENSITIVITY
			new_scroll := pan.start_scroll + candle_delta
			max_scroll := chart_max_scroll(pane, candle_count)
			new_scroll = clamp(new_scroll, 0, max_scroll)
			if new_scroll != pane.view.scroll_x {
				pane.view.scroll_x = new_scroll
				changed = true
			}
		} else {
			// Released — end pan.
			pan.active = false
		}
	}

	// --- Keyboard: arrows, Home, End ---
	if mouse_in_body {
		if .Left in input.keys.just_pressed && !input.modifiers.ctrl {
			// Left arrow: scroll 1 candle into history.
			max_scroll := chart_max_scroll(pane, candle_count)
			new_scroll := min(pane.view.scroll_x + 1, max_scroll)
			if new_scroll != pane.view.scroll_x {
				pane.view.scroll_x = new_scroll
				changed = true
			}
		}
		if .Right in input.keys.just_pressed && !input.modifiers.ctrl {
			// Right arrow: scroll 1 candle toward live edge.
			new_scroll := max(pane.view.scroll_x - 1, 0)
			if new_scroll != pane.view.scroll_x {
				pane.view.scroll_x = new_scroll
				changed = true
			}
		}
		if .Home in input.keys.just_pressed {
			// Home: snap to live edge.
			if pane.view.scroll_x != 0 {
				pane.view.scroll_x = 0
				changed = true
			}
		}
		if .End in input.keys.just_pressed {
			// End: snap to oldest candle.
			max_scroll := chart_max_scroll(pane, candle_count)
			if pane.view.scroll_x != max_scroll {
				pane.view.scroll_x = max_scroll
				changed = true
			}
		}
	}

	// --- Crosshair update ---
	if mouse_in_body {
		pane.view.crosshair.active = true
		pane.view.crosshair.mouse_pos = input.mouse.pos
	} else if pane.view.crosshair.active {
		pane.view.crosshair.active = false
		changed = true
	}

	return changed
}

// Reset scroll to live edge (scroll_x = 0).
chart_interaction_reset_to_live :: proc(pane: ^Pane) {
	if pane == nil do return
	pane.view.scroll_x = 0
}

// Compute the maximum scroll offset (fully scrolled to oldest candle).
chart_max_scroll :: proc(pane: ^Pane, candle_count: int) -> f32 {
	if pane == nil || candle_count <= 0 do return 0
	visible := pane.view.zoom_level > 0 ? int(pane.view.zoom_level) : min(candle_count, 140)
	return f32(max(candle_count - visible, 0))
}

// --- Viewport mode queries ---

// True when chart is at the live edge (tracking newest candles).
chart_is_live :: proc(pane: ^Pane) -> bool {
	if pane == nil do return true
	return pane.view.scroll_x <= 0
}

// Scroll progress as fraction 0..1 (0 = live edge, 1 = oldest).
chart_scroll_fraction :: proc(pane: ^Pane, candle_count: int) -> f32 {
	if pane == nil do return 0
	ms := chart_max_scroll(pane, candle_count)
	if ms <= 0 do return 0
	return clamp(pane.view.scroll_x / ms, 0, 1)
}

// Effective visible candle count (resolves auto=0 to default).
chart_effective_visible :: proc(pane: ^Pane, candle_count: int) -> int {
	if pane == nil do return 0
	if pane.view.zoom_level > 0 do return min(int(pane.view.zoom_level), candle_count)
	return min(candle_count, 140)
}

// --- "Return to Live" HUD ---

// Draw a "LIVE" button and scroll position bar when chart is scrolled away from live edge.
// Returns true if the user clicked the "LIVE" button (caller should reset scroll_x to 0).
draw_return_to_live_hud :: proc(state: ^App_State, cell_vp: ui.Rect, pointer: ui.Pointer_Input, scroll_x: f32, candle_count: int) -> bool {
	if state == nil do return false
	if scroll_x <= 0 do return false // at live edge, nothing to show

	clicked := false

	// "LIVE" pill button — top-right corner of chart body.
	PILL_W :: f32(42)
	PILL_H :: f32(18)
	pill_x := cell_vp.pos.x + cell_vp.size.x - PILL_W - 6
	pill_y := cell_vp.pos.y + 4
	pill_rect := ui.rect_xywh(pill_x, pill_y, PILL_W, PILL_H)
	pill_hovered := ui.rect_contains(pill_rect, pointer.pos)

	pill_bg := pill_hovered ? ui.with_alpha(ui.COL_GREEN, 0.35) : ui.with_alpha(ui.COL_GREEN, 0.18)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = pill_rect, color = pill_bg})
	// Border for visibility.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(pill_x, pill_y, PILL_W, 1), color = ui.with_alpha(ui.COL_GREEN, 0.4)})
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(pill_x, pill_y + PILL_H - 1, PILL_W, 1), color = ui.with_alpha(ui.COL_GREEN, 0.4)})
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(pill_x, pill_y, 1, PILL_H), color = ui.with_alpha(ui.COL_GREEN, 0.4)})
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ui.rect_xywh(pill_x + PILL_W - 1, pill_y, 1, PILL_H), color = ui.with_alpha(ui.COL_GREEN, 0.4)})

	label_x := pill_x + 4
	label_y := pill_y + PILL_H * 0.5 + ui.FONT_SIZE_XS * 0.35
	ui.push_text(&state.cmd_buf, {label_x, label_y}, "> LIVE", ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)

	if pill_hovered && pointer.left_pressed {
		clicked = true
	}

	// Scroll position bar — thin horizontal bar at bottom of chart, above time axis.
	// Shows proportion of history visible and current position.
	visible_default := min(candle_count, 140)
	max_scroll := f32(max(candle_count - visible_default, 0))
	if max_scroll > 0 && cell_vp.size.x > 40 {
		BAR_H :: f32(3)
		BAR_INSET :: f32(8)
		bar_w := cell_vp.size.x - BAR_INSET * 2
		bar_y := cell_vp.pos.y + cell_vp.size.y - BAR_H - 2

		// Track background.
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = ui.rect_xywh(cell_vp.pos.x + BAR_INSET, bar_y, bar_w, BAR_H),
			color = ui.with_alpha(ui.COL_WHITE, 0.06),
		})

		// Thumb: position proportional to scroll.
		frac := clamp(scroll_x / max_scroll, 0, 1)
		vis_frac := f32(visible_default) / f32(max(candle_count, 1))
		thumb_w := max(bar_w * vis_frac, 8)
		thumb_x := cell_vp.pos.x + BAR_INSET + (bar_w - thumb_w) * (1 - frac) // 0=right (live), 1=left (oldest)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = ui.rect_xywh(thumb_x, bar_y, thumb_w, BAR_H),
			color = ui.with_alpha(ui.COL_BLUE, 0.45),
		})

		// Offset label next to the pill.
		offset_buf: [16]u8
		offset_label := fmt.bprintf(offset_buf[:], "-%d", int(scroll_x))
		ui.push_text(&state.cmd_buf,
			{pill_x - 6 - f32(len(offset_label)) * 6, label_y},
			offset_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	return clicked
}

// --- Entity_World bridge ---

// Sync pane view state to Entity_World views for legacy consumers
// (GetRange lazy loading, persistence, compare mode).
chart_interaction_sync_to_world :: proc(state: ^App_State, pane: ^Pane, ci: int) {
	if state == nil || pane == nil do return
	if ci < 0 || ci >= state.world.count do return
	state.world.views[ci].candle_scroll_x = pane.view.scroll_x
	state.world.views[ci].candle_zoom = pane.view.zoom_level
	state.world.views[ci].crosshair = pane.view.crosshair
}
