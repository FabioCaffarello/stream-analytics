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

// --- S80: Cell_View_Model chart + indicator enrichment tests ---

@(test)
test_view_model_resolves_chart_display :: proc(t: ^testing.T) {
	state := make_test_state(2)
	defer free(state)
	state.world.charts[0].show_vol = true
	state.world.charts[0].show_heatmap = false
	state.world.charts[0].show_vpvr = true
	state.world.charts[0].ob_group_idx = 3
	state.world.charts[0].trade_filter_idx = 2
	state.world.charts[1].show_vol = false
	state.world.charts[1].heatmap_intensity_idx = 2

	vm0 := resolve_cell_view_model(state, 0)
	vm1 := resolve_cell_view_model(state, 1)

	testing.expect(t, vm0.chart.show_vol, "cell 0 should show vol")
	testing.expect(t, !vm0.chart.show_heatmap, "cell 0 should not show heatmap")
	testing.expect(t, vm0.chart.show_vpvr, "cell 0 should show vpvr")
	testing.expect_value(t, vm0.chart.ob_group_idx, 3)
	testing.expect_value(t, vm0.chart.trade_filter_idx, 2)
	testing.expect(t, !vm1.chart.show_vol, "cell 1 should not show vol")
	testing.expect_value(t, vm1.chart.heatmap_intensity_idx, 2)
}

@(test)
test_view_model_resolves_indicators :: proc(t: ^testing.T) {
	state := make_test_state(2)
	defer free(state)
	state.world.indicators[0].show_ma = true
	state.world.indicators[0].show_rsi = true
	state.world.indicators[1].show_bbands = true
	state.world.indicators[1].show_funding = true

	vm0 := resolve_cell_view_model(state, 0)
	vm1 := resolve_cell_view_model(state, 1)

	testing.expect(t, vm0.indicators.show_ma, "cell 0 should show MA")
	testing.expect(t, vm0.indicators.show_rsi, "cell 0 should show RSI")
	testing.expect(t, !vm0.indicators.show_bbands, "cell 0 should not show BBands")
	testing.expect(t, vm1.indicators.show_bbands, "cell 1 should show BBands")
	testing.expect(t, vm1.indicators.show_funding, "cell 1 should show Funding")
	testing.expect(t, !vm1.indicators.show_ma, "cell 1 should not show MA")
}

@(test)
test_view_model_resolves_indicator_params :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	state.world.ind_params[0].ma_periods = {10, 25, 100}
	state.world.ind_params[0].rsi_period = 21
	state.world.ind_params[0].bb_sigma = 3.0

	vm := resolve_cell_view_model(state, 0)

	testing.expect_value(t, vm.ind_params.ma_periods[0], 10)
	testing.expect_value(t, vm.ind_params.ma_periods[1], 25)
	testing.expect_value(t, vm.ind_params.ma_periods[2], 100)
	testing.expect_value(t, vm.ind_params.rsi_period, 21)
	testing.expect(t, vm.ind_params.bb_sigma == 3.0, "bb_sigma should be 3.0")
}

@(test)
test_view_model_chart_is_value_copy :: proc(t: ^testing.T) {
	// Verify that the view model holds a value copy — mutations don't leak back.
	state := make_test_state(1)
	defer free(state)
	state.world.charts[0].show_vol = true

	vm := resolve_cell_view_model(state, 0)
	testing.expect(t, vm.chart.show_vol, "initial value should be true")

	// Mutate the ECS component — view model should be unaffected.
	state.world.charts[0].show_vol = false
	testing.expect(t, vm.chart.show_vol, "view model should still be true (value copy)")
}

// --- S80: Snapshot chart_display + indicator_flags capture tests ---

@(test)
test_snapshot_captures_chart_display :: proc(t: ^testing.T) {
	state := make_test_state(2)
	defer free(state)
	state.world.charts[0].show_vol = true
	state.world.charts[0].show_heatmap = true
	state.world.charts[0].show_vpvr = false
	state.world.charts[0].ob_group_idx = 5
	state.world.charts[1].show_vol = false
	state.world.charts[1].show_heatmap = false
	state.world.charts[1].show_vpvr = true

	snap := capture_runtime_snapshot(state)

	testing.expect_value(t, snap.cell_count, 2)
	// Unpack cell 0 chart display.
	cd0 := snap.cells[0].chart_display
	testing.expect(t, (cd0 & (1 << 0)) != 0, "cell 0 should have show_vol")
	testing.expect(t, (cd0 & (1 << 1)) != 0, "cell 0 should have show_heatmap")
	testing.expect(t, (cd0 & (1 << 2)) == 0, "cell 0 should not have show_vpvr")
	testing.expect_value(t, (cd0 >> 5) & 0xF, 5) // ob_group_idx

	// Unpack cell 1 chart display.
	cd1 := snap.cells[1].chart_display
	testing.expect(t, (cd1 & (1 << 0)) == 0, "cell 1 should not have show_vol")
	testing.expect(t, (cd1 & (1 << 2)) != 0, "cell 1 should have show_vpvr")
}

@(test)
test_snapshot_captures_indicator_flags :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	state.world.indicators[0].show_ma = true
	state.world.indicators[0].show_vwap = true
	state.world.indicators[0].show_macd = true

	snap := capture_runtime_snapshot(state)

	flags := snap.cells[0].indicator_flags
	testing.expect(t, (flags & (1 << 0)) != 0, "should have show_ma")     // bit 0
	testing.expect(t, (flags & (1 << 1)) == 0, "should not have show_bbands") // bit 1
	testing.expect(t, (flags & (1 << 2)) != 0, "should have show_vwap")   // bit 2
	testing.expect(t, (flags & (1 << 4)) != 0, "should have show_macd")   // bit 4
}

@(test)
test_snapshot_captures_active_route :: proc(t: ^testing.T) {
	state := make_test_state(1)
	defer free(state)
	state.chrome.active_route = .Portfolio

	snap := capture_runtime_snapshot(state)

	testing.expect_value(t, snap.active_route, u8(Route.Portfolio))
}

// --- S80: Layout V6 deterministic round-trip test ---

@(test)
test_layout_v6_roundtrip_deterministic :: proc(t: ^testing.T) {
	state := make_test_state(3)
	defer free(state)

	// Set up diverse per-cell state.
	state.layout_mode = .Custom
	state.custom_grid_def.col_count = 2
	state.custom_grid_def.col_weights[0] = 0.6
	state.custom_grid_def.col_weights[1] = 0.4
	state.custom_grid_def.row_count = 2
	state.custom_grid_def.row_weights[0] = 0.5
	state.custom_grid_def.row_weights[1] = 0.5
	state.signal_evidence_link_enabled = true

	// Cell 0: Candle with indicators and chart display.
	state.world.widgets[0].kind = .Candle
	state.world.charts[0].show_vol = true
	state.world.charts[0].show_heatmap = true
	state.world.charts[0].ob_group_idx = 3
	state.world.charts[0].trade_filter_idx = 2
	state.world.indicators[0].show_ma = true
	state.world.indicators[0].show_rsi = true
	state.world.timeframes[0].tf_idx = 4 // 15m
	state.world.spans[0].col_span = 2

	// Cell 1: Analytics with binding.
	state.world.widgets[1].kind = .Analytics
	state.world.analytics[1].analytics_kind = .CVD
	binding_set(&state.world.bindings[1], "binance", "ETHUSDT")

	// Cell 2: Orderbook.
	state.world.widgets[2].kind = .Orderbook

	// Serialize.
	buf1: [2048]u8
	n1 := build_layout_v6_string(state, buf1[:])
	testing.expect(t, n1 > 0, "serialization should produce output")

	// Create fresh state and restore.
	state2 := make_test_state(1)
	defer free(state2)
	v6_str := string(buf1[:n1])
	ok := restore_layout_v6_from_string(state2, v6_str)
	testing.expect(t, ok, "restore should succeed")

	// Verify round-trip: cell count.
	testing.expect_value(t, state2.world.count, 3)

	// Verify round-trip: widget kinds.
	testing.expect_value(t, int(state2.world.widgets[0].kind), int(Widget_Kind.Candle))
	testing.expect_value(t, int(state2.world.widgets[1].kind), int(Widget_Kind.Analytics))
	testing.expect_value(t, int(state2.world.widgets[2].kind), int(Widget_Kind.Orderbook))

	// Verify round-trip: chart display preserved.
	testing.expect(t, state2.world.charts[0].show_vol, "cell 0 show_vol should survive round-trip")
	testing.expect(t, state2.world.charts[0].show_heatmap, "cell 0 show_heatmap should survive round-trip")
	testing.expect_value(t, state2.world.charts[0].ob_group_idx, 3)
	testing.expect_value(t, state2.world.charts[0].trade_filter_idx, 2)

	// Verify round-trip: TF override.
	testing.expect_value(t, state2.world.timeframes[0].tf_idx, 4)

	// Verify round-trip: binding.
	testing.expect(t, binding_has(&state2.world.bindings[1]), "cell 1 should have binding")
	testing.expect_value(t, binding_venue(&state2.world.bindings[1]), "binance")
	testing.expect_value(t, binding_symbol(&state2.world.bindings[1]), "ETHUSDT")

	// Verify round-trip: analytics kind via chart display bits 17-18.
	testing.expect_value(t, int(state2.world.analytics[1].analytics_kind), int(services.Analytics_Kind.CVD))

	// Verify round-trip: layout mode.
	testing.expect_value(t, int(state2.layout_mode), int(Layout_Mode.Custom))

	// Verify round-trip: signal_evidence_link.
	testing.expect(t, state2.signal_evidence_link_enabled, "signal_evidence_link should survive round-trip")

	// Re-serialize and compare: must be byte-identical.
	buf2: [2048]u8
	n2 := build_layout_v6_string(state2, buf2[:])
	testing.expect_value(t, n1, n2)
	testing.expect(t, string(buf1[:n1]) == string(buf2[:n2]), "re-serialized layout should be byte-identical")
}
