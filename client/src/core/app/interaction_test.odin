package app

// S142/S145: Dashboard interaction & utility pass tests.

import "core:testing"
import "mr:md_common"

// ---------------------------------------------------------------------------
// Compare pane cycling
// ---------------------------------------------------------------------------

@(test)
test_compare_pane_cycle_next_wraps :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.compare.active = true
	state.compare.count = 3
	state.compare.focused_pane = 2

	queue_ui_action(state, UI_Action{kind = .Cycle_Compare_Pane_Next})
	apply_ui_actions(state)

	testing.expect(t, state.compare.focused_pane == 0,
		"Cycling next from last pane should wrap to 0")
}

@(test)
test_compare_pane_cycle_prev_wraps :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.compare.active = true
	state.compare.count = 3
	state.compare.focused_pane = 0

	queue_ui_action(state, UI_Action{kind = .Cycle_Compare_Pane_Prev})
	apply_ui_actions(state)

	testing.expect(t, state.compare.focused_pane == 2,
		"Cycling prev from pane 0 should wrap to last")
}

@(test)
test_compare_pane_cycle_next_normal :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.compare.active = true
	state.compare.count = 4
	state.compare.focused_pane = 1

	queue_ui_action(state, UI_Action{kind = .Cycle_Compare_Pane_Next})
	apply_ui_actions(state)

	testing.expect(t, state.compare.focused_pane == 2,
		"Cycling next from pane 1 should go to 2")
}

@(test)
test_compare_pane_cycle_inactive_noop :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.compare.active = false
	state.compare.count = 3
	state.compare.focused_pane = 1

	queue_ui_action(state, UI_Action{kind = .Cycle_Compare_Pane_Next})
	apply_ui_actions(state)

	testing.expect(t, state.compare.focused_pane == 1,
		"Cycling when compare inactive should be noop")
}

// ---------------------------------------------------------------------------
// CVD / Delta Vol / OI toggles
// ---------------------------------------------------------------------------

@(test)
test_toggle_cvd_action :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.focused = -1

	queue_ui_action(state, UI_Action{kind = .Toggle_CVD})
	apply_ui_actions(state)

	testing.expect(t, state.indicators.show_cvd == true,
		"Toggle CVD should enable show_cvd globally")
}

@(test)
test_toggle_delta_vol_action :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.focused = -1

	queue_ui_action(state, UI_Action{kind = .Toggle_Delta_Vol})
	apply_ui_actions(state)

	testing.expect(t, state.indicators.show_delta_vol == true,
		"Toggle Delta Vol should enable show_delta_vol globally")
}

@(test)
test_toggle_oi_action :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.focused = -1

	queue_ui_action(state, UI_Action{kind = .Toggle_OI})
	apply_ui_actions(state)

	testing.expect(t, state.indicators.show_oi == true,
		"Toggle OI should enable show_oi globally")
}

// ---------------------------------------------------------------------------
// S145: Recovery UX — overlay differentiation + badge rendering
// ---------------------------------------------------------------------------

@(test)
test_s145_degraded_stale_recovering_overlay :: proc(t: ^testing.T) {
	// Stale_Recovering reliability should show "Recovering" title, not generic "Unreliable".
	sv := Cell_Surface_View{
		composition   = .Composed,
		has_live_data = true,
		stream_bound  = true,
		health_level  = .Unhealthy,
		recovery_attempts = 2,
		reliability   = .Stale_Recovering,
	}
	// The key assertion: Stale_Recovering should NOT block render (still shows data).
	testing.expect(t, !md_common.stream_reliability_blocks_render(.Stale_Recovering),
		"Stale_Recovering should not block render")
}

@(test)
test_s145_manual_resync_blocks_render :: proc(t: ^testing.T) {
	// Manual_Resync should block rendering.
	testing.expect(t, md_common.stream_reliability_blocks_render(.Manual_Resync),
		"Manual_Resync should block render")
}

@(test)
test_s145_surface_view_exposes_recovery_attempts :: proc(t: ^testing.T) {
	// Cell_Surface_View should carry recovery_attempts from apply_state.
	sv := Cell_Surface_View{
		recovery_attempts = 2,
	}
	testing.expect(t, sv.recovery_attempts == 2,
		"Cell_Surface_View should expose recovery_attempts")
}

@(test)
test_s145_degraded_aging_does_not_block :: proc(t: ^testing.T) {
	testing.expect(t, !md_common.stream_reliability_blocks_render(.Degraded_Aging),
		"Degraded_Aging should not block render")
}

@(test)
test_s145_stale_unrecoverable_blocks :: proc(t: ^testing.T) {
	testing.expect(t, md_common.stream_reliability_blocks_render(.Stale_Unrecoverable),
		"Stale_Unrecoverable should block render")
}

@(test)
test_s145_desync_blocks :: proc(t: ^testing.T) {
	testing.expect(t, md_common.stream_reliability_blocks_render(.Desync),
		"Desync should block render")
}

@(test)
test_s145_offline_blocks :: proc(t: ^testing.T) {
	testing.expect(t, md_common.stream_reliability_blocks_render(.Offline),
		"Offline should block render")
}

@(test)
test_s145_reliable_does_not_block :: proc(t: ^testing.T) {
	testing.expect(t, !md_common.stream_reliability_blocks_render(.Reliable),
		"Reliable should not block render")
}
