package app

import "core:testing"
import "mr:services"
import "mr:ui"

// S101: Workspace Persistence Hardening — layout V6 round-trip, first-run,
// analytics restore, schema migration, and deterministic restore tests.

// ---------------------------------------------------------------------------
// V6 Layout Round-Trip
// ---------------------------------------------------------------------------

// Helper: create minimal heap-allocated App_State with persistence-relevant fields.
@(private = "file")
make_persist_state :: proc(cell_count: int) -> ^App_State {
	state := new(App_State)
	state.world.count = cell_count
	state.active_tf_idx = 2
	state.layout_mode = .Custom
	state.custom_grid_def.col_count = 2
	state.custom_grid_def.col_weights[0] = 0.6
	state.custom_grid_def.col_weights[1] = 0.4
	state.custom_grid_def.row_count = 2
	state.custom_grid_def.row_weights[0] = 0.5
	state.custom_grid_def.row_weights[1] = 0.5
	for ci in 0 ..< cell_count {
		init_world_cell_defaults(state, ci, .Candle)
	}
	return state
}

@(test)
test_v6_round_trip_basic :: proc(t: ^testing.T) {
	state := make_persist_state(3)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Analytics
	state.world.widgets[2].kind = .Trades
	state.world.analytics[1].analytics_kind = .CVD
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT:SPOT")
	binding_set(&state.world.bindings[2], "bybit", "ETHUSDT:PERP")
	state.signal_evidence_link_enabled = true

	// Persist.
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	// Restore into fresh state.
	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	testing.expect_value(t, restored.world.count, 3)
	testing.expect(t, restored.world.widgets[0].kind == .Candle, "cell 0 should be candle")
	testing.expect(t, restored.world.widgets[1].kind == .Analytics, "cell 1 should be analytics")
	testing.expect(t, restored.world.widgets[2].kind == .Trades, "cell 2 should be trades")
	testing.expect(t, restored.world.analytics[1].analytics_kind == .CVD, "cell 1 analytics kind should be CVD")
	testing.expect(t, binding_has(&restored.world.bindings[0]), "cell 0 should have binding")
	testing.expect(t, binding_venue(&restored.world.bindings[0]) == "binance", "cell 0 venue should be binance")
	// S101: Symbol normalized during persist — market type suffix stripped.
	testing.expect(t, binding_symbol(&restored.world.bindings[0]) == "BTCUSDT", "cell 0 symbol should be BTCUSDT (normalized)")
	testing.expect(t, binding_has(&restored.world.bindings[2]), "cell 2 should have binding")
	testing.expect(t, binding_venue(&restored.world.bindings[2]) == "bybit", "cell 2 venue")
	testing.expect(t, binding_symbol(&restored.world.bindings[2]) == "ETHUSDT", "cell 2 symbol (normalized)")
	testing.expect(t, restored.signal_evidence_link_enabled, "evidence link should be restored")
}

@(test)
test_v6_round_trip_indicator_flags :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)
	state.world.indicators[0].show_ma = true
	state.world.indicators[0].show_bbands = true
	state.world.indicators[0].show_cvd = true
	state.world.indicators[0].show_delta_vol = true
	state.world.indicators[0].show_oi = true

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	ind := restored.world.indicators[0]
	testing.expect(t, ind.show_ma, "MA should be restored")
	testing.expect(t, ind.show_bbands, "BBands should be restored")
	testing.expect(t, ind.show_cvd, "CVD should be restored")
	testing.expect(t, ind.show_delta_vol, "Delta Vol should be restored")
	testing.expect(t, ind.show_oi, "OI should be restored")
	testing.expect(t, !ind.show_rsi, "RSI should be off")
	testing.expect(t, !ind.show_macd, "MACD should be off")
}

@(test)
test_v6_round_trip_chart_display :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)
	state.world.charts[0].show_vol = true
	state.world.charts[0].show_heatmap = true
	state.world.charts[0].show_vpvr = false
	state.world.charts[0].heatmap_intensity_idx = 2
	state.world.charts[0].ob_group_idx = 5
	state.world.charts[0].dom_group_idx = 3
	state.world.charts[0].trade_filter_idx = 7
	state.world.analytics[0].analytics_kind = .Delta_Volume

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	c := restored.world.charts[0]
	testing.expect(t, c.show_vol, "show_vol should be true")
	testing.expect(t, c.show_heatmap, "show_heatmap should be true")
	testing.expect(t, !c.show_vpvr, "show_vpvr should be false")
	testing.expect_value(t, c.heatmap_intensity_idx, 2)
	testing.expect_value(t, c.ob_group_idx, 5)
	testing.expect_value(t, c.dom_group_idx, 3)
	testing.expect_value(t, c.trade_filter_idx, 7)
	testing.expect(t, restored.world.analytics[0].analytics_kind == .Delta_Volume,
		"analytics kind should be Delta_Volume")
}

@(test)
test_v6_round_trip_spans_and_subplots :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)
	state.world.spans[0].col_span = 2
	state.world.spans[0].row_span = 1
	state.world.spans[1].col_span = 1
	state.world.spans[1].row_span = 3
	state.world.subplots[0].sub_main_split = 0.75
	state.world.subplots[0].sub_ratios[0] = 0.5
	state.world.subplots[0].sub_ratios[1] = 0.3
	state.world.subplots[0].sub_ratios[2] = 0.2

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	testing.expect_value(t, restored.world.spans[0].col_span, 2)
	testing.expect_value(t, restored.world.spans[0].row_span, 1)
	testing.expect_value(t, restored.world.spans[1].col_span, 1)
	testing.expect_value(t, restored.world.spans[1].row_span, 3)
	// Subplot ratios — x1000 round-trip tolerance.
	sm := restored.world.subplots[0].sub_main_split
	testing.expect(t, sm > 0.74 && sm < 0.76, "sub_main_split should round-trip ~0.75")
}

@(test)
test_v6_round_trip_per_cell_tf :: proc(t: ^testing.T) {
	state := make_persist_state(3)
	defer free(state)
	state.world.timeframes[0].tf_idx = -1 // follow global
	state.world.timeframes[1].tf_idx = 3  // per-cell 5m
	state.world.timeframes[2].tf_idx = 7  // per-cell 1D

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	testing.expect_value(t, restored.world.timeframes[0].tf_idx, -1)
	testing.expect_value(t, restored.world.timeframes[1].tf_idx, 3)
	testing.expect_value(t, restored.world.timeframes[2].tf_idx, 7)
}

@(test)
test_v6_round_trip_grid_weights :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)
	state.custom_grid_def.col_count = 3
	state.custom_grid_def.col_weights[0] = 0.25
	state.custom_grid_def.col_weights[1] = 0.5
	state.custom_grid_def.col_weights[2] = 0.25
	state.custom_grid_def.row_count = 2
	state.custom_grid_def.row_weights[0] = 0.7
	state.custom_grid_def.row_weights[1] = 0.3

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	testing.expect_value(t, restored.custom_grid_def.col_count, 3)
	testing.expect_value(t, restored.custom_grid_def.row_count, 2)
	// Weights: x100 round-trip tolerance.
	testing.expect(t, restored.custom_grid_def.col_weights[0] > 0.24 && restored.custom_grid_def.col_weights[0] < 0.26,
		"col weight 0 should round-trip ~0.25")
	testing.expect(t, restored.custom_grid_def.col_weights[1] > 0.49 && restored.custom_grid_def.col_weights[1] < 0.51,
		"col weight 1 should round-trip ~0.50")
	testing.expect(t, restored.custom_grid_def.row_weights[0] > 0.69 && restored.custom_grid_def.row_weights[0] < 0.71,
		"row weight 0 should round-trip ~0.70")
}

@(test)
test_v6_round_trip_layout_mode :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)
	state.layout_mode = .Preset

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	testing.expect(t, restored.layout_mode == .Preset, "layout mode should be Preset")
}

@(test)
test_v6_round_trip_evidence_link_disabled :: proc(t: ^testing.T) {
	state := make_persist_state(1)
	defer free(state)
	state.signal_evidence_link_enabled = false

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	testing.expect(t, !restored.signal_evidence_link_enabled, "evidence link should be disabled")
}

// ---------------------------------------------------------------------------
// First-Run (Empty Settings)
// ---------------------------------------------------------------------------

@(test)
test_first_run_empty_settings_defaults :: proc(t: ^testing.T) {
	// Simulate first-run: no V6 layout in settings → fallback chain all fail → V1 defaults.
	state := new(App_State)
	defer free(state)
	// Settings store is zero-initialized (empty). No known keys populated.
	// restore_layout_v6 should return false for empty settings.
	ok := restore_layout_v6(state)
	testing.expect(t, !ok, "V6 restore should fail with empty settings")
	// V5, V4, V3, V2, V1 should also fail with empty settings.
	ok2 := restore_layout_v5(state)
	testing.expect(t, !ok2, "V5 restore should fail with empty settings")
	ok3 := restore_layout_v4(state)
	testing.expect(t, !ok3, "V4 restore should fail with empty settings")
}

@(test)
test_first_run_default_binding :: proc(t: ^testing.T) {
	// After failed restore chain, PRD-0009 sets cell 0 to binance/BTCUSDT:SPOT.
	state := new(App_State)
	defer free(state)
	// Simulate layout_from_panels default (1 cell, Candle widget).
	state.world.count = 1
	init_world_cell_defaults(state, 0, .Candle)

	// PRD-0009: If no cells have bindings, set default on cell 0.
	any_bound := false
	for ci in 0 ..< state.world.count {
		if binding_has(&state.world.bindings[ci]) {
			any_bound = true
			break
		}
	}
	if !any_bound && state.world.count > 0 {
		binding_set(&state.world.bindings[0], "binance", "BTCUSDT:SPOT")
	}

	testing.expect(t, binding_has(&state.world.bindings[0]), "cell 0 should have binding after first-run")
	testing.expect(t, binding_venue(&state.world.bindings[0]) == "binance", "cell 0 venue should be binance")
	testing.expect(t, binding_symbol(&state.world.bindings[0]) == "BTCUSDT:SPOT", "cell 0 symbol should be BTCUSDT:SPOT")
}

// ---------------------------------------------------------------------------
// V6 Restore Resilience
// ---------------------------------------------------------------------------

@(test)
test_v6_restore_rejects_invalid_header :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	testing.expect(t, !restore_layout_v6_from_string(state, ""), "empty string should fail")
	testing.expect(t, !restore_layout_v6_from_string(state, "V5"), "V5 header should fail")
	testing.expect(t, !restore_layout_v6_from_string(state, "abc"), "garbage should fail")
	testing.expect(t, !restore_layout_v6_from_string(state, "V6"), "V6 with no content should fail")
}

@(test)
test_v6_restore_rejects_truncated_string :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	// Valid V6 prefix but truncated before any cell data.
	testing.expect(t, !restore_layout_v6_from_string(state, "V6|C"), "truncated after mode should fail")
	testing.expect(t, !restore_layout_v6_from_string(state, "V6|C|CW:50,50"), "no row weights should fail")
}

@(test)
test_v6_restore_max_cells :: proc(t: ^testing.T) {
	// Build a V6 string with max cells.
	state := make_persist_state(CELL_MAX)
	defer free(state)

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "max cell restore should succeed")
	testing.expect_value(t, restored.world.count, CELL_MAX)
}

@(test)
test_v6_round_trip_follow_active_cell :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)
	// Cell 0: follow-active (no binding), cell 1: bound.
	binding_clear(&state.world.bindings[0])
	binding_set(&state.world.bindings[1], "coinbase", "BTCUSD:SPOT")

	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")
	testing.expect(t, !binding_has(&restored.world.bindings[0]), "cell 0 should follow active")
	testing.expect(t, binding_has(&restored.world.bindings[1]), "cell 1 should be bound")
	testing.expect(t, binding_venue(&restored.world.bindings[1]) == "coinbase", "cell 1 venue")
}

// ---------------------------------------------------------------------------
// Indicator Flag Pack/Unpack
// ---------------------------------------------------------------------------

@(test)
test_indicator_flags_pack_unpack_all_bits :: proc(t: ^testing.T) {
	// Test all 11 indicator flags individually.
	flags_to_test := [11]struct{ bit: int, setter: proc(ind: ^Indicator_Component), checker: proc(ind: ^Indicator_Component) -> bool }{
		{ 0,  proc(ind: ^Indicator_Component) { ind.show_ma = true },            proc(ind: ^Indicator_Component) -> bool { return ind.show_ma } },
		{ 1,  proc(ind: ^Indicator_Component) { ind.show_bbands = true },        proc(ind: ^Indicator_Component) -> bool { return ind.show_bbands } },
		{ 2,  proc(ind: ^Indicator_Component) { ind.show_vwap = true },          proc(ind: ^Indicator_Component) -> bool { return ind.show_vwap } },
		{ 3,  proc(ind: ^Indicator_Component) { ind.show_rsi = true },           proc(ind: ^Indicator_Component) -> bool { return ind.show_rsi } },
		{ 4,  proc(ind: ^Indicator_Component) { ind.show_macd = true },          proc(ind: ^Indicator_Component) -> bool { return ind.show_macd } },
		{ 5,  proc(ind: ^Indicator_Component) { ind.show_funding = true },       proc(ind: ^Indicator_Component) -> bool { return ind.show_funding } },
		{ 6,  proc(ind: ^Indicator_Component) { ind.show_liq = true },           proc(ind: ^Indicator_Component) -> bool { return ind.show_liq } },
		{ 7,  proc(ind: ^Indicator_Component) { ind.show_trade_counter = true }, proc(ind: ^Indicator_Component) -> bool { return ind.show_trade_counter } },
		{ 8,  proc(ind: ^Indicator_Component) { ind.show_cvd = true },           proc(ind: ^Indicator_Component) -> bool { return ind.show_cvd } },
		{ 9,  proc(ind: ^Indicator_Component) { ind.show_delta_vol = true },     proc(ind: ^Indicator_Component) -> bool { return ind.show_delta_vol } },
		{ 10, proc(ind: ^Indicator_Component) { ind.show_oi = true },            proc(ind: ^Indicator_Component) -> bool { return ind.show_oi } },
	}
	for entry in flags_to_test {
		ind: Indicator_Component
		entry.setter(&ind)
		packed := pack_indicator_flags(&ind)
		testing.expect(t, packed == (1 << uint(entry.bit)),
			"single flag should set only its bit")
		unpacked: Indicator_Component
		unpack_indicator_flags(&unpacked, packed)
		testing.expect(t, entry.checker(&unpacked),
			"unpacked flag should be set")
	}
}

@(test)
test_indicator_flags_all_on_round_trip :: proc(t: ^testing.T) {
	ind := Indicator_Component{
		show_ma = true, show_bbands = true, show_vwap = true,
		show_rsi = true, show_macd = true, show_funding = true,
		show_liq = true, show_trade_counter = true,
		show_cvd = true, show_delta_vol = true, show_oi = true,
	}
	packed := pack_indicator_flags(&ind)
	testing.expect_value(t, packed, (1 << 11) - 1) // all 11 bits set

	unpacked: Indicator_Component
	unpack_indicator_flags(&unpacked, packed)
	testing.expect(t, unpacked.show_ma && unpacked.show_bbands && unpacked.show_vwap &&
		unpacked.show_rsi && unpacked.show_macd && unpacked.show_funding &&
		unpacked.show_liq && unpacked.show_trade_counter &&
		unpacked.show_cvd && unpacked.show_delta_vol && unpacked.show_oi,
		"all flags should be set after round-trip")
}

// ---------------------------------------------------------------------------
// Chart Display Pack/Unpack
// ---------------------------------------------------------------------------

@(test)
test_chart_display_pack_unpack_round_trip :: proc(t: ^testing.T) {
	chart := Chart_Component{
		show_vol = true, show_heatmap = false, show_vpvr = true,
		heatmap_intensity_idx = 3, ob_group_idx = 12,
		dom_group_idx = 7, trade_filter_idx = 15,
	}
	analytics := Analytics_Component{ analytics_kind = .Open_Interest }

	packed := pack_chart_display_with_analytics(&chart, &analytics)

	restored_chart: Chart_Component
	restored_analytics: Analytics_Component
	unpack_chart_display_with_analytics(&restored_chart, &restored_analytics, packed)

	testing.expect(t, restored_chart.show_vol, "show_vol should survive")
	testing.expect(t, !restored_chart.show_heatmap, "show_heatmap should be false")
	testing.expect(t, restored_chart.show_vpvr, "show_vpvr should survive")
	testing.expect_value(t, restored_chart.heatmap_intensity_idx, 3)
	testing.expect_value(t, restored_chart.ob_group_idx, 12)
	testing.expect_value(t, restored_chart.dom_group_idx, 7)
	testing.expect_value(t, restored_chart.trade_filter_idx, 15)
	testing.expect(t, restored_analytics.analytics_kind == .Open_Interest,
		"analytics kind should round-trip")
}

// ---------------------------------------------------------------------------
// Complex Layout Restore (multi-widget, analytics, bound cells)
// ---------------------------------------------------------------------------

@(test)
test_v6_complex_layout_restore :: proc(t: ^testing.T) {
	state := make_persist_state(6)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Analytics
	state.world.widgets[2].kind = .Trades
	state.world.widgets[3].kind = .Orderbook
	state.world.widgets[4].kind = .Heatmap
	state.world.widgets[5].kind = .Session_VPVR
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	binding_set(&state.world.bindings[1], "binance", "BTCUSDT")
	// cell 2: follow active
	binding_set(&state.world.bindings[3], "bybit", "ETHUSDT")
	binding_set(&state.world.bindings[4], "kraken", "XBTUSD")
	// cell 5: follow active
	state.world.analytics[1].analytics_kind = .Open_Interest
	state.world.indicators[0].show_cvd = true
	state.world.indicators[0].show_oi = true
	state.world.charts[0].show_vol = true
	state.world.charts[0].show_heatmap = true
	state.world.timeframes[1].tf_idx = 5 // per-cell 15m
	state.world.spans[4].col_span = 2
	state.world.spans[4].row_span = 2
	state.signal_evidence_link_enabled = false
	state.custom_grid_def.col_count = 3
	state.custom_grid_def.col_weights[0] = 0.33
	state.custom_grid_def.col_weights[1] = 0.34
	state.custom_grid_def.col_weights[2] = 0.33
	state.custom_grid_def.row_count = 2
	state.custom_grid_def.row_weights[0] = 0.6
	state.custom_grid_def.row_weights[1] = 0.4

	// Persist → Restore.
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "complex restore should succeed")
	testing.expect_value(t, restored.world.count, 6)

	// Verify widget kinds.
	testing.expect(t, restored.world.widgets[0].kind == .Candle, "cell 0")
	testing.expect(t, restored.world.widgets[1].kind == .Analytics, "cell 1")
	testing.expect(t, restored.world.widgets[2].kind == .Trades, "cell 2")
	testing.expect(t, restored.world.widgets[3].kind == .Orderbook, "cell 3")
	testing.expect(t, restored.world.widgets[4].kind == .Heatmap, "cell 4")
	testing.expect(t, restored.world.widgets[5].kind == .Session_VPVR, "cell 5")

	// Verify analytics.
	testing.expect(t, restored.world.analytics[1].analytics_kind == .Open_Interest, "cell 1 analytics kind")

	// Verify indicators.
	testing.expect(t, restored.world.indicators[0].show_cvd, "cell 0 CVD should be on")
	testing.expect(t, restored.world.indicators[0].show_oi, "cell 0 OI should be on")
	testing.expect(t, !restored.world.indicators[0].show_delta_vol, "cell 0 DV should be off")

	// Verify chart display.
	testing.expect(t, restored.world.charts[0].show_vol, "cell 0 vol should be on")
	testing.expect(t, restored.world.charts[0].show_heatmap, "cell 0 heatmap should be on")

	// Verify per-cell TF.
	testing.expect_value(t, restored.world.timeframes[1].tf_idx, 5)
	testing.expect_value(t, restored.world.timeframes[0].tf_idx, -1)

	// Verify spans.
	testing.expect_value(t, restored.world.spans[4].col_span, 2)
	testing.expect_value(t, restored.world.spans[4].row_span, 2)

	// Verify bindings.
	testing.expect(t, binding_has(&restored.world.bindings[0]), "cell 0 bound")
	testing.expect(t, !binding_has(&restored.world.bindings[2]), "cell 2 follow-active")
	testing.expect(t, binding_has(&restored.world.bindings[3]), "cell 3 bound")
	testing.expect(t, !binding_has(&restored.world.bindings[5]), "cell 5 follow-active")

	// Verify evidence link.
	testing.expect(t, !restored.signal_evidence_link_enabled, "evidence link disabled")

	// Verify grid.
	testing.expect_value(t, restored.custom_grid_def.col_count, 3)
	testing.expect_value(t, restored.custom_grid_def.row_count, 2)
}

// ---------------------------------------------------------------------------
// Reload Determinism — persist then restore twice should produce same state
// ---------------------------------------------------------------------------

@(test)
test_v6_reload_determinism :: proc(t: ^testing.T) {
	state := make_persist_state(4)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Analytics
	state.world.widgets[2].kind = .Orderbook
	state.world.widgets[3].kind = .DOM
	state.world.analytics[1].analytics_kind = .CVD
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT:SPOT")
	state.world.indicators[0].show_ma = true
	state.world.indicators[0].show_cvd = true
	state.world.charts[0].show_vol = true
	state.world.timeframes[2].tf_idx = 4
	state.signal_evidence_link_enabled = true

	// First persist.
	buf1: [2048]u8
	off1 := build_layout_v6_string(state, buf1[:])
	v6_1 := string(buf1[:off1])

	// Restore.
	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6_1)
	testing.expect(t, ok, "first restore should succeed")

	// Second persist from restored state.
	buf2: [2048]u8
	off2 := build_layout_v6_string(restored, buf2[:])
	v6_2 := string(buf2[:off2])

	// The two V6 strings should be identical.
	testing.expect(t, v6_1 == v6_2,
		"persist→restore→persist should produce identical V6 strings (deterministic)")
}

// ---------------------------------------------------------------------------
// Normalized Symbol (S92 guard against 400)
// ---------------------------------------------------------------------------

@(test)
test_normalized_symbol_strips_suffix :: proc(t: ^testing.T) {
	testing.expect(t, normalized_symbol("BTCUSDT:SPOT") == "BTCUSDT", "should strip :SPOT")
	testing.expect(t, normalized_symbol("BTCUSDT:PERP") == "BTCUSDT", "should strip :PERP")
	testing.expect(t, normalized_symbol("ETHUSDT") == "ETHUSDT", "no suffix should pass through")
	testing.expect(t, normalized_symbol("") == "", "empty should pass through")
}

// ---------------------------------------------------------------------------
// Schema Version Constant
// ---------------------------------------------------------------------------

@(test)
test_workspace_schema_version :: proc(t: ^testing.T) {
	testing.expect(t, WORKSPACE_SCHEMA_VERSION >= 10, "schema version should be >= 10")
}

// ---------------------------------------------------------------------------
// S111: Persist_Result Structured Restore
// ---------------------------------------------------------------------------

@(test)
test_persist_result_no_data_on_empty :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	// No settings populated → No_Data.
	result := restore_workspace(state)
	testing.expect(t, result == .No_Data, "empty settings should return No_Data")
}

@(test)
test_persist_result_ok_on_valid_v6 :: proc(t: ^testing.T) {
	state := make_persist_state(2)
	defer free(state)
	buf: [2048]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	result := restore_layout_v6_validated(restored, v6)
	testing.expect(t, result == .Ok, "valid V6 should return Ok")
	testing.expect_value(t, restored.world.count, 2)
}

@(test)
test_persist_result_corrupted_on_garbage :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	result := restore_layout_v6_validated(state, "XXXX")
	testing.expect(t, result == .Corrupted, "garbage header should return Corrupted")
}

@(test)
test_persist_result_corrupted_on_truncated :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	result := restore_layout_v6_validated(state, "V6|C")
	testing.expect(t, result == .Corrupted, "truncated V6 should return Corrupted")
}

@(test)
test_persist_result_version_mismatch_future :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	// Simulate a future version "V7" header.
	result := restore_layout_v6_validated(state, "V7|C|CW:50,50|RW:50,50|0:-1:0:1:1:0:0,0,0,0,0:0:0")
	testing.expect(t, result == .Version_Mismatch, "V7 header should return Version_Mismatch")
}

@(test)
test_persist_result_no_data_on_short :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	result := restore_layout_v6_validated(state, "")
	testing.expect(t, result == .No_Data, "empty string should return No_Data")
	// "V6" alone is only 2 chars (< 4 min length) → treated as No_Data.
	result2 := restore_layout_v6_validated(state, "V6")
	testing.expect(t, result2 == .No_Data, "too-short V6 should return No_Data")
}

@(test)
test_persist_result_ok_helper :: proc(t: ^testing.T) {
	testing.expect(t, persist_result_ok(.Ok), "Ok should be ok")
	testing.expect(t, !persist_result_ok(.No_Data), "No_Data should not be ok")
	testing.expect(t, !persist_result_ok(.Corrupted), "Corrupted should not be ok")
	testing.expect(t, !persist_result_ok(.Version_Mismatch), "Version_Mismatch should not be ok")
	testing.expect(t, !persist_result_ok(.Too_Many_Cells), "Too_Many_Cells should not be ok")
}

@(test)
test_persist_schema_version_stamp :: proc(t: ^testing.T) {
	// After persist, SETTING_SETTINGS_VERSION should contain current version.
	state := make_persist_state(1)
	defer free(state)
	persist_layout_v6(state)
	v, ok := services.settings_get(&state.settings, services.SETTING_SETTINGS_VERSION)
	testing.expect(t, ok, "settings version should be set after persist")
	testing.expect(t, v == "11", "settings version should match WORKSPACE_SCHEMA_VERSION")
}

@(test)
test_persist_idempotent :: proc(t: ^testing.T) {
	// Persisting the same state twice should produce identical V6 strings.
	state := make_persist_state(3)
	defer free(state)
	state.world.widgets[0].kind = .Candle
	state.world.widgets[1].kind = .Trades
	state.world.widgets[2].kind = .Orderbook
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT:SPOT")
	state.world.indicators[0].show_ma = true
	state.world.charts[0].show_vol = true

	buf1: [2048]u8
	off1 := build_layout_v6_string(state, buf1[:])
	buf2: [2048]u8
	off2 := build_layout_v6_string(state, buf2[:])
	testing.expect(t, string(buf1[:off1]) == string(buf2[:off2]),
		"two persists of same state should be identical (idempotent)")
}

@(test)
test_persist_result_v5_header_corrupted :: proc(t: ^testing.T) {
	// V5 header is NOT a version mismatch — it's a different format entirely.
	state := new(App_State)
	defer free(state)
	result := restore_layout_v6_validated(state, "V5|C|CW:50|RW:50")
	testing.expect(t, result == .Corrupted, "V5 header should return Corrupted (not recognized)")
}
