package app

import "core:testing"
import "mr:md_common"
import "mr:services"

// S61: Cell_View_Model resolution tests.
// Verify that resolve_cell_view_model bundles surface view, stores,
// effective TF, and widget config into a single coherent read model.
// App_State is ~13MB so must be heap-allocated to avoid stack overflow.

// Helper: create minimal heap-allocated App_State with N cells.
@(private = "file")
make_test_state :: proc(cell_count: int) -> ^App_State {
	state := new(App_State)
	state.world.count = cell_count
	state.active_tf_idx = 2 // 1m
	for ci in 0 ..< cell_count {
		init_world_cell_defaults(state, ci, .Candle)
	}
	return state
}

@(test)
test_view_model_nil_state :: proc(t: ^testing.T) {
	vm := resolve_cell_view_model(nil, 0)
	testing.expect_value(t, vm.cell_idx, 0)
	testing.expect_value(t, int(vm.widget_kind), int(Widget_Kind.Candle))
}

@(test)
test_view_model_out_of_bounds :: proc(t: ^testing.T) {
	state := make_test_state(2)
	defer free(state)
	vm := resolve_cell_view_model(state, -1)
	testing.expect_value(t, vm.cell_idx, 0)
	vm2 := resolve_cell_view_model(state, 99)
	testing.expect_value(t, vm2.cell_idx, 0)
}

@(test)
test_view_model_resolves_widget_kind :: proc(t: ^testing.T) {
	state := make_test_state(3)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Analytics
	state.world.widgets[2].kind = .Session_VPVR

	vm0 := resolve_cell_view_model(state, 0)
	vm1 := resolve_cell_view_model(state, 1)
	vm2 := resolve_cell_view_model(state, 2)

	testing.expect_value(t, int(vm0.widget_kind), int(Widget_Kind.Candle))
	testing.expect_value(t, int(vm1.widget_kind), int(Widget_Kind.Analytics))
	testing.expect_value(t, int(vm2.widget_kind), int(Widget_Kind.Session_VPVR))
}

@(test)
test_view_model_resolves_global_tf :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	state.active_tf_idx = 3 // 5m

	vm := resolve_cell_view_model(state, 0)
	testing.expect_value(t, vm.tf_idx, 3)
	testing.expect_value(t, vm.tf_string, "5m")
	testing.expect_value(t, vm.tf_ms, i64(300_000))
}

@(test)
test_view_model_resolves_per_cell_tf :: proc(t: ^testing.T) {
	state := make_test_state(2)
	defer free(state)
	state.active_tf_idx = 2 // 1m
	state.world.timeframes[1].tf_idx = 6 // 1h

	vm0 := resolve_cell_view_model(state, 0)
	vm1 := resolve_cell_view_model(state, 1)

	testing.expect_value(t, vm0.tf_idx, 2)
	testing.expect_value(t, vm0.tf_string, "1m")
	testing.expect_value(t, vm1.tf_idx, 6)
	testing.expect_value(t, vm1.tf_string, "1h")
	testing.expect_value(t, vm1.tf_ms, i64(3_600_000))
}

@(test)
test_view_model_resolves_analytics_config :: proc(t: ^testing.T) {
	state := make_test_state(2)
	defer free(state)
	state.world.widgets[0].kind = .Analytics
	state.world.analytics[0].analytics_kind = .CVD
	state.world.analytics[0].show_history = true
	state.world.widgets[1].kind = .Analytics
	state.world.analytics[1].analytics_kind = .Bar_Stats
	state.world.analytics[1].show_history = false

	vm0 := resolve_cell_view_model(state, 0)
	vm1 := resolve_cell_view_model(state, 1)

	testing.expect_value(t, int(vm0.analytics_kind), int(services.Analytics_Kind.CVD))
	testing.expect(t, vm0.show_history, "cell 0 should show history")
	testing.expect_value(t, int(vm1.analytics_kind), int(services.Analytics_Kind.Bar_Stats))
	testing.expect(t, !vm1.show_history, "cell 1 should not show history")
}

@(test)
test_view_model_resolves_focused :: proc(t: ^testing.T) {
	state := make_test_state(3)
	defer free(state)
	state.world.focused = 1

	vm0 := resolve_cell_view_model(state, 0)
	vm1 := resolve_cell_view_model(state, 1)
	vm2 := resolve_cell_view_model(state, 2)

	testing.expect(t, !vm0.focused, "cell 0 should not be focused")
	testing.expect(t, vm1.focused, "cell 1 should be focused")
	testing.expect(t, !vm2.focused, "cell 2 should not be focused")
}

@(test)
test_view_model_resolves_cell_idx :: proc(t: ^testing.T) {
	state := make_test_state(4)
	defer free(state)
	for ci in 0 ..< 4 {
		vm := resolve_cell_view_model(state, ci)
		testing.expect_value(t, vm.cell_idx, ci)
	}
}

@(test)
test_view_model_stores_default_to_global :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	// Follow-active cell (stream_idx = -1, no binding) should get global stores.
	vm := resolve_cell_view_model(state, 0)
	testing.expect(t, vm.stores.candle == &state.stores.candle, "candle store should be global")
	testing.expect(t, vm.stores.trades == &state.stores.trades, "trades store should be global")
	testing.expect(t, vm.stores.orderbook == &state.stores.orderbook, "orderbook store should be global")
}

@(test)
test_view_model_surface_composition_empty_by_default :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	vm := resolve_cell_view_model(state, 0)
	testing.expect_value(t, int(vm.surface.composition), int(md_common.Composition_Stage.Empty))
}

@(test)
test_view_model_surface_no_live_data_by_default :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	vm := resolve_cell_view_model(state, 0)
	testing.expect(t, !vm.surface.has_live_data, "no live data expected for fresh state")
}

@(test)
test_view_model_surface_follow_active_not_bound :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	vm := resolve_cell_view_model(state, 0)
	testing.expect(t, !vm.surface.stream_bound, "follow-active cell should not be stream_bound")
}

@(test)
test_view_model_surface_bound_cell :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	vm := resolve_cell_view_model(state, 0)
	testing.expect(t, vm.surface.stream_bound, "cell with binding should be stream_bound")
}
