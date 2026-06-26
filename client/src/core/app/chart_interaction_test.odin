package app

// S139/S141: Chart Interaction — tests.

import "core:testing"
import "mr:ports"
import "mr:ui"
import "mr:widgets"

// --- Scroll tests (Ctrl+Scroll = horizontal pan) ---

@(test)
test_chart_ctrl_scroll_scrolls_into_history :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = 1.0 // scroll up + ctrl = scroll into history
	input.modifiers.ctrl = true
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, changed, "ctrl+scroll should modify view state")
	testing.expect(t, pane.view.scroll_x > 0, "scroll_x should increase when scrolling into history")
	testing.expect_value(t, pane.view.scroll_x, CHART_SCROLL_SPEED)
}

@(test)
test_chart_ctrl_scroll_clamps_to_zero :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.scroll_x = 0
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = -1.0 // scroll down + ctrl = toward live edge (but already at 0)
	input.modifiers.ctrl = true
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, !changed, "no change when already at live edge")
	testing.expect_value(t, pane.view.scroll_x, f32(0))
}

@(test)
test_chart_ctrl_scroll_clamps_to_max :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.scroll_x = 100
	pane.view.zoom_level = 50
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = 1000.0 // massive scroll into history
	input.modifiers.ctrl = true
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	// max_scroll = candle_count - visible = 200 - 50 = 150
	testing.expect(t, changed, "scroll should change")
	testing.expect_value(t, pane.view.scroll_x, f32(150))
}

@(test)
test_chart_scroll_ignored_outside_body :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = 5.0
	input.mouse.pos = {50, 50} // outside body rect
	pointer := make_test_pointer(50, 50)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, !changed, "scroll outside body should be ignored")
	testing.expect_value(t, pane.view.scroll_x, f32(0))
}

// --- Zoom tests (plain scroll = zoom) ---

@(test)
test_chart_scroll_zooms_in :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 100
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = 1.0 // scroll up = zoom in (fewer candles)
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 500, &pan)

	testing.expect(t, changed, "scroll should change zoom")
	testing.expect(t, pane.view.zoom_level < 100, "zoom should decrease (fewer visible candles) on scroll up")
	testing.expect(t, pane.view.zoom_level >= CHART_ZOOM_MIN, "zoom should not go below minimum")
}

@(test)
test_chart_scroll_zooms_out :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 100
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = -1.0 // scroll down = zoom out (more candles)
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 500, &pan)

	testing.expect(t, changed, "scroll down should change zoom")
	testing.expect(t, pane.view.zoom_level > 100, "zoom should increase (more visible candles) on scroll down")
	testing.expect(t, pane.view.zoom_level <= CHART_ZOOM_MAX, "zoom should not exceed maximum")
}

@(test)
test_chart_zoom_clamped_to_candle_count :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 100
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = -100.0 // massive zoom out
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 50, &pan)

	testing.expect(t, changed, "zoom should change")
	testing.expect_value(t, pane.view.zoom_level, f32(50)) // capped at candle_count
}

// S147-BUG-04: Zoom never goes below CHART_ZOOM_MIN, even with few candles.
@(test)
test_chart_zoom_respects_minimum_with_few_candles :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = CHART_ZOOM_MIN // at minimum
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = 10.0 // zoom in (fewer candles)
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	// Only 3 candles — zoom should stay at CHART_ZOOM_MIN, not drop to 3.
	changed := chart_interaction_update(&pane, input, pointer, body, 3, &pan)

	testing.expect(t, !changed, "zoom should not change below CHART_ZOOM_MIN")
	testing.expect(t, pane.view.zoom_level >= CHART_ZOOM_MIN,
		"zoom must never go below CHART_ZOOM_MIN even with few candles")
}

// S147-BUG-04: Zoom stays at minimum when zooming in at minimum.
@(test)
test_chart_zoom_cannot_go_below_minimum :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = CHART_ZOOM_MIN
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = 100.0 // massive zoom in
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	chart_interaction_update(&pane, input, pointer, body, 500, &pan)

	testing.expect(t, pane.view.zoom_level >= CHART_ZOOM_MIN,
		"zoom should never go below CHART_ZOOM_MIN")
}

// --- Crosshair tests ---

@(test)
test_chart_crosshair_active_when_hovering :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pan: Chart_Pan_State
	input := make_test_input()
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, pane.view.crosshair.active, "crosshair should be active when mouse in body")
	testing.expect_value(t, pane.view.crosshair.mouse_pos.x, f32(150))
	testing.expect_value(t, pane.view.crosshair.mouse_pos.y, f32(150))
}

@(test)
test_chart_crosshair_deactivates_when_leaving :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.crosshair.active = true
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.pos = {50, 50} // outside body
	pointer := make_test_pointer(50, 50)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, changed, "leaving body should trigger change")
	testing.expect(t, !pane.view.crosshair.active, "crosshair should deactivate when leaving body")
}

// --- Arrow key tests ---

@(test)
test_chart_left_arrow_scrolls_one_candle :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.scroll_x = 5
	pan: Chart_Pan_State
	input := make_test_input()
	input.keys.just_pressed = {.Left}
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, changed, "left arrow should change scroll")
	testing.expect_value(t, pane.view.scroll_x, f32(6))
}

@(test)
test_chart_right_arrow_scrolls_toward_live :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.scroll_x = 5
	pan: Chart_Pan_State
	input := make_test_input()
	input.keys.just_pressed = {.Right}
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, changed, "right arrow should change scroll")
	testing.expect_value(t, pane.view.scroll_x, f32(4))
}

// --- Max scroll calculation ---

@(test)
test_chart_max_scroll_auto_zoom :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 0 // auto
	max_s := chart_max_scroll(&pane, 200)
	// auto visible = min(200, 140) = 140, max_scroll = 200 - 140 = 60
	testing.expect_value(t, max_s, f32(60))
}

@(test)
test_chart_max_scroll_explicit_zoom :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 50
	max_s := chart_max_scroll(&pane, 200)
	// max_scroll = 200 - 50 = 150
	testing.expect_value(t, max_s, f32(150))
}

@(test)
test_chart_max_scroll_zero_candles :: proc(t: ^testing.T) {
	pane := make_test_pane()
	max_s := chart_max_scroll(&pane, 0)
	testing.expect_value(t, max_s, f32(0))
}

// --- Reset to live edge ---

@(test)
test_chart_reset_to_live :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.scroll_x = 50
	chart_interaction_reset_to_live(&pane)
	testing.expect_value(t, pane.view.scroll_x, f32(0))
}

// --- Sync to world ---

@(test)
test_chart_sync_to_world :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.count = 2

	pane := make_test_pane()
	pane.view.scroll_x = 42.0
	pane.view.zoom_level = 80.0
	pane.view.crosshair.active = true

	chart_interaction_sync_to_world(state, &pane, 1)

	testing.expect_value(t, state.world.views[1].candle_scroll_x, f32(42.0))
	testing.expect_value(t, state.world.views[1].candle_zoom, f32(80.0))
	testing.expect(t, state.world.views[1].crosshair.active, "crosshair should sync")
}

@(test)
test_chart_sync_to_world_out_of_bounds :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.count = 1

	pane := make_test_pane()
	pane.view.scroll_x = 42.0

	// Should not crash with out-of-bounds index.
	chart_interaction_sync_to_world(state, &pane, 5)
	testing.expect_value(t, state.world.views[0].candle_scroll_x, f32(0)) // unchanged
}

// --- No candle data ---

@(test)
test_chart_no_candles_does_nothing :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pan: Chart_Pan_State
	input := make_test_input()
	input.mouse.scroll.y = 5.0
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 0, &pan)

	testing.expect(t, !changed, "no candles should produce no change")
}

// --- S141: Home/End key tests ---

@(test)
test_chart_home_snaps_to_live :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.scroll_x = 50
	pan: Chart_Pan_State
	input := make_test_input()
	input.keys.just_pressed = {.Home}
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, changed, "Home should change scroll")
	testing.expect_value(t, pane.view.scroll_x, f32(0))
}

@(test)
test_chart_home_at_live_no_change :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.scroll_x = 0
	pan: Chart_Pan_State
	input := make_test_input()
	input.keys.just_pressed = {.Home}
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, !changed, "Home at live edge should not change")
}

@(test)
test_chart_end_snaps_to_oldest :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 50
	pane.view.scroll_x = 0
	pan: Chart_Pan_State
	input := make_test_input()
	input.keys.just_pressed = {.End}
	pointer := make_test_pointer(150, 150)
	body := ui.rect_xywh(100, 100, 200, 200)

	changed := chart_interaction_update(&pane, input, pointer, body, 200, &pan)

	testing.expect(t, changed, "End should change scroll")
	// max_scroll = 200 - 50 = 150
	testing.expect_value(t, pane.view.scroll_x, f32(150))
}

// --- S141: Viewport mode query tests ---

@(test)
test_chart_is_live :: proc(t: ^testing.T) {
	pane := make_test_pane()
	testing.expect(t, chart_is_live(&pane), "default pane should be live")
	pane.view.scroll_x = 10
	testing.expect(t, !chart_is_live(&pane), "scrolled pane should not be live")
	pane.view.scroll_x = 0
	testing.expect(t, chart_is_live(&pane), "reset pane should be live")
}

@(test)
test_chart_scroll_fraction :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 50
	// max_scroll = 200 - 50 = 150
	pane.view.scroll_x = 0
	testing.expect_value(t, chart_scroll_fraction(&pane, 200), f32(0))
	pane.view.scroll_x = 75
	testing.expect_value(t, chart_scroll_fraction(&pane, 200), f32(0.5))
	pane.view.scroll_x = 150
	testing.expect_value(t, chart_scroll_fraction(&pane, 200), f32(1.0))
}

@(test)
test_chart_effective_visible_auto :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 0
	testing.expect_value(t, chart_effective_visible(&pane, 200), 140) // auto = min(200, 140)
	testing.expect_value(t, chart_effective_visible(&pane, 50), 50)   // auto = min(50, 140) = 50
}

@(test)
test_chart_effective_visible_explicit :: proc(t: ^testing.T) {
	pane := make_test_pane()
	pane.view.zoom_level = 80
	testing.expect_value(t, chart_effective_visible(&pane, 200), 80)
	testing.expect_value(t, chart_effective_visible(&pane, 50), 50) // clamped to candle count
}

// --- S141: HUD return-to-live tests ---

@(test)
test_draw_return_to_live_hidden_at_live_edge :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	pointer := make_test_pointer(0, 0)
	vp := ui.rect_xywh(0, 0, 400, 300)

	// At live edge (scroll_x == 0): should return false.
	clicked := draw_return_to_live_hud(state, vp, pointer, 0, 200)
	testing.expect(t, !clicked, "should not show HUD at live edge")
}

@(test)
test_draw_return_to_live_shows_when_scrolled :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.text = make_stub_text_port()
	pointer := make_test_pointer(0, 0) // not clicking
	vp := ui.rect_xywh(0, 0, 400, 300)

	clicked := draw_return_to_live_hud(state, vp, pointer, 50, 200)
	testing.expect(t, !clicked, "should not click when pointer is away")
	// The HUD should have pushed render commands (we just verify no crash).
}

// --- Test helpers ---

@(private = "file")
make_test_pane :: proc() -> Pane {
	p: Pane
	p.id = 1
	p.view.zoom_level = 0 // auto
	return p
}

@(private = "file")
make_test_input :: proc() -> ports.Input_State {
	input: ports.Input_State
	input.mouse.pos = {150, 150}
	return input
}

@(private = "file")
make_test_pointer :: proc(x, y: f32) -> ui.Pointer_Input {
	return ui.Pointer_Input{pos = {x, y}}
}

@(private = "file")
make_stub_text_port :: proc() -> ports.Text_Port {
	return ports.Text_Port{
		measure = proc(size: f32, text: string) -> ui.Vec2 {
			return {f32(len(text)) * 6, size}
		},
	}
}
