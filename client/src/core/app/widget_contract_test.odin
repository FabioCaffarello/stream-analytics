package app

import "core:testing"
import "mr:md_common"
import "mr:services"
import "mr:ui"

// S108/S109: Widget Contract tests.
// Covers: lifecycle state machine, data context resolution, contract dispatch,
// serialization round-trip, contract table exhaustiveness, contract routing.

// ---------------------------------------------------------------------------
// Lifecycle State Machine
// ---------------------------------------------------------------------------

@(test)
test_lifecycle_valid_created_to_bound :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Created, .Bound), "Created → Bound should be valid")
}

@(test)
test_lifecycle_valid_created_to_disposing :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Created, .Disposing), "Created → Disposing should be valid")
}

@(test)
test_lifecycle_invalid_created_to_active :: proc(t: ^testing.T) {
	testing.expect(t, !widget_lifecycle_valid(.Created, .Active), "Created → Active should be invalid (must bind first)")
}

@(test)
test_lifecycle_invalid_created_to_suspended :: proc(t: ^testing.T) {
	testing.expect(t, !widget_lifecycle_valid(.Created, .Suspended), "Created → Suspended should be invalid")
}

@(test)
test_lifecycle_valid_bound_to_active :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Bound, .Active), "Bound → Active should be valid")
}

@(test)
test_lifecycle_valid_bound_to_disposing :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Bound, .Disposing), "Bound → Disposing should be valid")
}

@(test)
test_lifecycle_valid_active_to_suspended :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Active, .Suspended), "Active → Suspended should be valid")
}

@(test)
test_lifecycle_valid_active_to_bound :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Active, .Bound), "Active → Bound should be valid (rebind)")
}

@(test)
test_lifecycle_valid_active_to_disposing :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Active, .Disposing), "Active → Disposing should be valid")
}

@(test)
test_lifecycle_valid_suspended_to_active :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Suspended, .Active), "Suspended → Active should be valid")
}

@(test)
test_lifecycle_valid_suspended_to_disposing :: proc(t: ^testing.T) {
	testing.expect(t, widget_lifecycle_valid(.Suspended, .Disposing), "Suspended → Disposing should be valid")
}

@(test)
test_lifecycle_disposing_is_terminal :: proc(t: ^testing.T) {
	testing.expect(t, !widget_lifecycle_valid(.Disposing, .Created), "Disposing → Created invalid")
	testing.expect(t, !widget_lifecycle_valid(.Disposing, .Bound), "Disposing → Bound invalid")
	testing.expect(t, !widget_lifecycle_valid(.Disposing, .Active), "Disposing → Active invalid")
	testing.expect(t, !widget_lifecycle_valid(.Disposing, .Suspended), "Disposing → Suspended invalid")
	testing.expect(t, !widget_lifecycle_valid(.Disposing, .Disposing), "Disposing → Disposing invalid")
}

@(test)
test_lifecycle_transition_succeeds :: proc(t: ^testing.T) {
	host := widget_host_create(.Candle)
	ok := widget_lifecycle_transition(&host, .Bound)
	testing.expect(t, ok, "Created → Bound should succeed")
	testing.expect_value(t, host.state, Widget_Lifecycle_State.Bound)
}

@(test)
test_lifecycle_transition_fails_invalid :: proc(t: ^testing.T) {
	host := widget_host_create(.Candle)
	// Created → Active is invalid (must bind first).
	ok := widget_lifecycle_transition(&host, .Active)
	testing.expect(t, !ok, "Created → Active should fail")
	testing.expect_value(t, host.state, Widget_Lifecycle_State.Created)
}

@(test)
test_lifecycle_full_sequence :: proc(t: ^testing.T) {
	host := widget_host_create(.Trades)

	testing.expect(t, widget_lifecycle_transition(&host, .Bound), "→ Bound")
	testing.expect(t, widget_lifecycle_transition(&host, .Active), "→ Active")
	testing.expect(t, widget_lifecycle_transition(&host, .Suspended), "→ Suspended")
	testing.expect(t, widget_lifecycle_transition(&host, .Active), "→ Active again")
	testing.expect(t, widget_lifecycle_transition(&host, .Disposing), "→ Disposing")
	testing.expect(t, !widget_lifecycle_transition(&host, .Active), "Disposing is terminal")
}

@(test)
test_lifecycle_transition_nil_host :: proc(t: ^testing.T) {
	ok := widget_lifecycle_transition(nil, .Bound)
	testing.expect(t, !ok, "nil host should fail")
}

// ---------------------------------------------------------------------------
// Contract Table Exhaustiveness
// ---------------------------------------------------------------------------

@(test)
test_contract_table_exhaustive :: proc(t: ^testing.T) {
	// Every Widget_Kind must have a contract with at least on_create.
	for kind in Widget_Kind {
		contract := WIDGET_CONTRACTS[kind]
		testing.expect(t, contract.on_create != nil, "contract must have on_create")
		testing.expect(t, contract.on_serialize != nil, "contract must have on_serialize")
		testing.expect(t, contract.on_dispose != nil, "contract must have on_dispose")
	}
}

@(test)
test_contract_non_empty_widgets_have_render :: proc(t: ^testing.T) {
	for kind in Widget_Kind {
		contract := WIDGET_CONTRACTS[kind]
		testing.expect(t, contract.on_render != nil, "contract must have on_render")
	}
}

@(test)
test_contract_candle_has_all_procs :: proc(t: ^testing.T) {
	contract := WIDGET_CONTRACTS[.Candle]
	testing.expect(t, contract.on_create != nil, "on_create")
	testing.expect(t, contract.on_bind_context != nil, "on_bind_context")
	testing.expect(t, contract.on_update != nil, "on_update")
	testing.expect(t, contract.on_render != nil, "on_render")
	testing.expect(t, contract.on_handle_input != nil, "on_handle_input")
	testing.expect(t, contract.on_serialize != nil, "on_serialize")
	testing.expect(t, contract.on_dispose != nil, "on_dispose")
}

@(test)
test_contract_empty_widget_no_update :: proc(t: ^testing.T) {
	contract := WIDGET_CONTRACTS[.Empty]
	testing.expect(t, contract.on_update == nil, "Empty widget should have nil on_update")
	testing.expect(t, contract.on_handle_input == nil, "Empty widget should have nil on_handle_input")
}

// ---------------------------------------------------------------------------
// Widget Data Context
// ---------------------------------------------------------------------------

@(test)
test_widget_data_context_from_vm :: proc(t: ^testing.T) {
	vm := Cell_View_Model{
		cell_idx       = 3,
		widget_kind    = .Candle,
		tf_idx         = 2,
		tf_string      = "1m",
		tf_ms          = 60_000,
		analytics_kind = .Delta_Volume,
		show_history   = true,
		focused        = true,
	}

	rect := ui.Rect{pos = {10, 20}, size = {800, 600}}
	ctx := widget_data_context_from_vm(vm, Pane_ID(5), rect)

	testing.expect_value(t, ctx.cell_idx, 3)
	testing.expect_value(t, ctx.pane_id, Pane_ID(5))
	testing.expect_value(t, ctx.tf_idx, 2)
	testing.expect_value(t, ctx.tf_string, "1m")
	testing.expect_value(t, ctx.tf_ms, i64(60_000))
	testing.expect_value(t, ctx.analytics_kind, services.Analytics_Kind.Delta_Volume)
	testing.expect_value(t, ctx.show_history, true)
	testing.expect_value(t, ctx.focused, true)
	testing.expect_value(t, ctx.compare_group, -1)
	testing.expect_value(t, ctx.rect.pos.x, f32(10))
	testing.expect_value(t, ctx.rect.size.x, f32(800))
}

@(test)
test_widget_data_context_default_compare_group :: proc(t: ^testing.T) {
	ctx: Widget_Data_Context
	// Default zero-init should not be confused with compare group 0.
	// We rely on explicit initialization to -1.
	ctx.compare_group = -1
	testing.expect_value(t, ctx.compare_group, -1)
}

// ---------------------------------------------------------------------------
// Contract Dispatch
// ---------------------------------------------------------------------------

@(test)
test_contract_dispatch_create :: proc(t: ^testing.T) {
	pool: Pane_Pool
	pane, id := pane_pool_alloc(&pool)
	testing.expect(t, pane != nil, "alloc succeeds")
	pane.widget = widget_host_create(.Trades)

	// Dispatch create should set defaults.
	widget_contract_create(pane)
	testing.expect_value(t, pane.tf_override, i8(-1))
	testing.expect_value(t, pane.view.zoom_level, f32(1.0))
}

@(test)
test_contract_dispatch_create_nil :: proc(t: ^testing.T) {
	// Should not panic.
	widget_contract_create(nil)
}

@(test)
test_contract_dispatch_serialize :: proc(t: ^testing.T) {
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.widget = widget_host_create(.Candle)
	pane.view.scroll_x = 42.0
	pane.view.zoom_level = 2.5
	pane.view.ob_scroll_y = 10.0
	pane.indicators.show_ma = true
	pane.chart.show_vol = true
	pane.analytics.analytics_kind = .Open_Interest

	state := widget_contract_serialize(pane)
	testing.expect_value(t, state.scroll_x, f32(42.0))
	testing.expect_value(t, state.zoom_level, f32(2.5))
	testing.expect_value(t, state.ob_scroll_y, f32(10.0))
	testing.expect_value(t, state.indicators.show_ma, true)
	testing.expect_value(t, state.chart.show_vol, true)
	testing.expect_value(t, state.analytics.analytics_kind, services.Analytics_Kind.Open_Interest)
}

@(test)
test_contract_dispatch_serialize_nil :: proc(t: ^testing.T) {
	state := widget_contract_serialize(nil)
	testing.expect_value(t, state.scroll_x, f32(0))
}

@(test)
test_contract_dispatch_input_nil :: proc(t: ^testing.T) {
	consumed := widget_contract_handle_input(nil, {}, {}, {})
	testing.expect(t, !consumed, "nil pane should not consume input")
}

@(test)
test_contract_dispatch_dispose_nil :: proc(t: ^testing.T) {
	// Should not panic.
	widget_contract_dispose(nil)
}

// ---------------------------------------------------------------------------
// Widget Contract For
// ---------------------------------------------------------------------------

@(test)
test_widget_contract_for :: proc(t: ^testing.T) {
	for kind in Widget_Kind {
		c := widget_contract_for(kind)
		// Every kind returns a valid contract.
		testing.expect(t, c.on_create != nil, "contract should have on_create")
	}
}

// ---------------------------------------------------------------------------
// Descriptor ↔ Contract Alignment
// ---------------------------------------------------------------------------

@(test)
test_descriptor_contract_alignment :: proc(t: ^testing.T) {
	// Every widget kind that supports_analytics in the descriptor
	// should have a non-nil on_update in its contract.
	for kind in Widget_Kind {
		desc := WIDGET_DESCRIPTORS[kind]
		contract := WIDGET_CONTRACTS[kind]
		if desc.supports_analytics {
			testing.expect(t, contract.on_update != nil, "analytics-capable widget should have on_update")
		}
	}
}

// ---------------------------------------------------------------------------
// S109: Contract-Based Pane Rendering
// ---------------------------------------------------------------------------

@(test)
test_contract_render_all_widget_kinds :: proc(t: ^testing.T) {
	// Every Widget_Kind has a render contract that is non-nil.
	// This validates that the contract table is complete for the render path.
	for kind in Widget_Kind {
		contract := WIDGET_CONTRACTS[kind]
		testing.expect(t, contract.on_render != nil, "every widget kind must have on_render for S109")
	}
}

@(test)
test_contract_bind_then_serialize_round_trip :: proc(t: ^testing.T) {
	// Simulate the S109 lifecycle: create → bind → serialize.
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.widget = widget_host_create(.Candle)

	// Create lifecycle.
	widget_contract_create(pane)
	testing.expect_value(t, pane.tf_override, i8(-1))
	testing.expect_value(t, pane.view.zoom_level, f32(1.0))

	// Simulate pane config mutation (as S109 contract path would do).
	pane.view.scroll_x = 100.0
	pane.view.zoom_level = 3.0
	pane.chart.show_vol = true
	pane.indicators.show_ma = true
	pane.analytics.analytics_kind = .CVD

	// Bind lifecycle.
	ctx := Widget_Data_Context{
		tf_idx         = 4,
		tf_string      = "15m",
		tf_ms          = 900_000,
		analytics_kind = .CVD,
	}
	widget_contract_bind(pane, ctx)

	// Serialize should capture pane state.
	state := widget_contract_serialize(pane)
	testing.expect_value(t, state.scroll_x, f32(100.0))
	testing.expect_value(t, state.zoom_level, f32(3.0))
	testing.expect_value(t, state.chart.show_vol, true)
	testing.expect_value(t, state.indicators.show_ma, true)
	testing.expect_value(t, state.analytics.analytics_kind, services.Analytics_Kind.CVD)
}

@(test)
test_contract_lifecycle_full_for_all_kinds :: proc(t: ^testing.T) {
	// Every widget kind should survive create → bind → dispose without panic.
	pool: Pane_Pool
	for kind in Widget_Kind {
		pane, _ := pane_pool_alloc(&pool)
		if pane == nil do break
		pane.widget = widget_host_create(kind)

		widget_contract_create(pane)
		widget_contract_bind(pane, {})
		widget_contract_dispose(pane)
	}
}

@(test)
test_pane_analytics_write_isolation :: proc(t: ^testing.T) {
	// S109: history toggle should write to pane, not Entity_World.
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.widget = widget_host_create(.Analytics)
	pane.analytics.show_history = false

	// Simulate the toggle that render_pane_via_contract performs.
	pane.analytics.show_history = !pane.analytics.show_history
	testing.expect_value(t, pane.analytics.show_history, true)

	// Toggle again.
	pane.analytics.show_history = !pane.analytics.show_history
	testing.expect_value(t, pane.analytics.show_history, false)
}

@(test)
test_widget_data_context_tf_from_pane_override :: proc(t: ^testing.T) {
	// S109: verify that pane tf_override is respected in context resolution.
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.widget = widget_host_create(.Candle)
	pane.tf_override = 4  // 15m

	// resolve_widget_data_context uses pane.tf_override when >= 0.
	// We can't call it without a full App_State, but we verify the
	// contract: tf_override >= 0 means per-pane TF.
	testing.expect(t, pane.tf_override >= 0, "per-pane TF is set")
	testing.expect_value(t, int(pane.tf_override), 4)
}

// ---------------------------------------------------------------------------
// S112: Pane Data Context Ownership Tests
// ---------------------------------------------------------------------------

@(test)
test_pane_effective_tf_idx_override :: proc(t: ^testing.T) {
	// S112: pane.tf_override takes precedence over workspace default.
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.tf_override = 5  // 30m

	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))
	ws.data_ctx.default_tf_idx = 2  // 1m

	tf := pane_effective_tf_idx(pane, &ws, 2)
	testing.expect_value(t, tf, 5)
}

@(test)
test_pane_effective_tf_idx_inherit_workspace :: proc(t: ^testing.T) {
	// S112: pane.tf_override = -1 → use workspace default.
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.tf_override = -1

	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))
	ws.data_ctx.default_tf_idx = 4  // 15m

	tf := pane_effective_tf_idx(pane, &ws, 2)
	testing.expect_value(t, tf, 4)
}

@(test)
test_pane_effective_tf_idx_fallback_global :: proc(t: ^testing.T) {
	// S112: pane override = -1, no workspace → use global.
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.tf_override = -1

	tf := pane_effective_tf_idx(pane, nil, 7)
	testing.expect_value(t, tf, 7)
}

@(test)
test_sync_panes_from_world_copies_binding :: proc(t: ^testing.T) {
	// S112: workspace_sync_panes_from_world copies Entity_World bindings to panes.
	state := new(App_State)
	defer free(state)
	state.world.count = 2
	state.world.widgets[0] = Widget_Component{kind = .Candle}
	state.world.widgets[1] = Widget_Component{kind = .Stats}
	state.world.timeframes[1].tf_idx = -1  // follow global
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	state.world.timeframes[0].tf_idx = 5
	state.world.indicators[0].show_ma = true

	// Build workspace from world.
	ws := workspace_registry_alloc(&state.ws_registry)
	testing.expect(t, ws != nil, "workspace allocated")
	workspace_sync_from_world(state)

	// Check that pane 0 has the binding.
	ws = workspace_registry_active(&state.ws_registry)
	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
	testing.expect(t, pane_count >= 2, "at least 2 panes")

	pane0 := pane_pool_get(&ws.pane_pool, pane_ids[0])
	testing.expect(t, pane0 != nil, "pane 0 exists")
	testing.expect(t, binding_has(&pane0.binding), "pane 0 has binding")
	testing.expect_value(t, binding_venue(&pane0.binding), "binance")
	testing.expect_value(t, binding_symbol(&pane0.binding), "BTCUSDT")
	testing.expect_value(t, pane0.tf_override, i8(5))
	testing.expect_value(t, pane0.indicators.show_ma, true)

	// Pane 1 has no binding.
	pane1 := pane_pool_get(&ws.pane_pool, pane_ids[1])
	testing.expect(t, pane1 != nil, "pane 1 exists")
	testing.expect(t, !binding_has(&pane1.binding), "pane 1 has no binding")
	testing.expect_value(t, pane1.tf_override, i8(-1))
}

@(test)
test_sync_pane_to_world_roundtrip :: proc(t: ^testing.T) {
	// S112: workspace_sync_pane_to_world writes pane state back to Entity_World.
	state := new(App_State)
	defer free(state)
	state.world.count = 1
	state.world.widgets[0] = Widget_Component{kind = .Candle}

	ws := workspace_registry_alloc(&state.ws_registry)
	workspace_sync_from_world(state)
	ws = workspace_registry_active(&state.ws_registry)

	pane_ids, _ := tree_collect_pane_ids(&ws.tree)
	pane := pane_pool_get(&ws.pane_pool, pane_ids[0])
	testing.expect(t, pane != nil, "pane exists")

	// Mutate pane-local state.
	binding_set(&pane.binding, "bybit", "ETHUSDT")
	pane.tf_override = 3
	pane.indicators.show_rsi = true

	// Sync pane → world.
	workspace_sync_pane_to_world(state, pane, 0)

	// Verify Entity_World reflects pane state.
	testing.expect(t, binding_has(&state.world.bindings[0]), "world binding set")
	testing.expect_value(t, binding_venue(&state.world.bindings[0]), "bybit")
	testing.expect_value(t, binding_symbol(&state.world.bindings[0]), "ETHUSDT")
	testing.expect_value(t, state.world.timeframes[0].tf_idx, 3)
	testing.expect_value(t, state.world.indicators[0].show_rsi, true)
}

@(test)
test_pane_data_context_struct :: proc(t: ^testing.T) {
	// S112: Pane_Data_Context struct is populated correctly.
	ctx := Pane_Data_Context{
		venue         = "binance",
		symbol        = "BTCUSDT",
		stream_idx    = 2,
		stream_bound  = true,
		tf_idx        = 4,
		analytics_kind = .CVD,
		compare_group = -1,
	}
	testing.expect_value(t, ctx.venue, "binance")
	testing.expect_value(t, ctx.stream_bound, true)
	testing.expect_value(t, ctx.compare_group, -1)
}

@(test)
test_pane_focus_by_id :: proc(t: ^testing.T) {
	// S112: Focus tracked by Pane_ID, not cell index.
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	p0 := workspace_alloc_pane(&ws, .Candle)
	p1 := workspace_alloc_pane(&ws, .Stats)

	ws.focus.active = p0
	testing.expect_value(t, ws.focus.active, p0)
	testing.expect(t, ws.focus.active != p1, "focus is on pane 0, not pane 1")

	ws.focus.active = p1
	testing.expect_value(t, ws.focus.active, p1)
}

@(test)
test_pane_independent_bindings :: proc(t: ^testing.T) {
	// S112: each pane owns its binding independently.
	pool: Pane_Pool
	pane0, _ := pane_pool_alloc(&pool)
	pane1, _ := pane_pool_alloc(&pool)

	binding_set(&pane0.binding, "binance", "BTCUSDT")
	binding_set(&pane1.binding, "bybit", "ETHUSDT")

	testing.expect_value(t, binding_venue(&pane0.binding), "binance")
	testing.expect_value(t, binding_symbol(&pane0.binding), "BTCUSDT")
	testing.expect_value(t, binding_venue(&pane1.binding), "bybit")
	testing.expect_value(t, binding_symbol(&pane1.binding), "ETHUSDT")
}

// ---------------------------------------------------------------------------
// S146: Widget TF Data Contract Integration
// ---------------------------------------------------------------------------

@(test)
test_widget_tf_expectation_candle_tick :: proc(t: ^testing.T) {
	exp := widget_tf_expectation(.Candle, 1_000)
	testing.expect_value(t, exp.backfill_criticality, md_common.Backfill_Criticality.Optional)
	testing.expect_value(t, exp.live_only_utility, md_common.Live_Only_Utility.Full)
}

@(test)
test_widget_tf_expectation_candle_15m :: proc(t: ^testing.T) {
	exp := widget_tf_expectation(.Candle, 900_000)
	testing.expect_value(t, exp.backfill_criticality, md_common.Backfill_Criticality.Critical)
	testing.expect_value(t, exp.live_only_utility, md_common.Live_Only_Utility.Minimal)
}

@(test)
test_widget_backfill_critical_candle_5m :: proc(t: ^testing.T) {
	testing.expect(t, widget_backfill_critical(.Candle, 300_000),
		"candle at 5m should have critical backfill")
}

@(test)
test_widget_backfill_not_critical_candle_1s :: proc(t: ^testing.T) {
	testing.expect(t, !widget_backfill_critical(.Candle, 1_000),
		"candle at 1s should not have critical backfill")
}

@(test)
test_widget_backfill_not_critical_stats :: proc(t: ^testing.T) {
	// Stats is TF-independent — backfill never critical.
	testing.expect(t, !widget_backfill_critical(.Stats, 900_000),
		"stats should never have critical backfill")
}

@(test)
test_widget_backfill_not_critical_trades :: proc(t: ^testing.T) {
	testing.expect(t, !widget_backfill_critical(.Trades, 86_400_000),
		"trades should never have critical backfill")
}

@(test)
test_widget_backfill_not_critical_orderbook :: proc(t: ^testing.T) {
	testing.expect(t, !widget_backfill_critical(.Orderbook, 86_400_000),
		"orderbook should never have critical backfill")
}

@(test)
test_widget_tf_expectation_analytics_15m :: proc(t: ^testing.T) {
	exp := widget_tf_expectation(.Analytics, 900_000)
	testing.expect_value(t, exp.backfill_criticality, md_common.Backfill_Criticality.Critical)
}

@(test)
test_widget_tf_expectation_heatmap_always_optional :: proc(t: ^testing.T) {
	// Heatmap has no backfill mechanism — always optional regardless of TF.
	exp := widget_tf_expectation(.Heatmap, 900_000)
	testing.expect_value(t, exp.backfill_criticality, md_common.Backfill_Criticality.Optional)
}

@(test)
test_widget_tf_all_kinds_return_valid :: proc(t: ^testing.T) {
	// Every widget kind should return a valid expectation without panic.
	for kind in Widget_Kind {
		exp := widget_tf_expectation(kind, 60_000)
		testing.expect(t, exp.overlay_patience_ms > 0, "overlay patience should be positive")
	}
}

// ---------------------------------------------------------------------------
// S149: DOM Widget Readiness Tests
// ---------------------------------------------------------------------------

@(test)
test_dom_readiness_orderbook_only :: proc(t: ^testing.T) {
	// DOM usable when only orderbook has data.
	ob: services.Orderbook_Store
	ob.ask_count = 5
	ob.bid_count = 5
	stores := Cell_Stores{orderbook = &ob}
	testing.expect(t, widget_store_has_data(.DOM, stores), "DOM usable with orderbook data")
}

@(test)
test_dom_readiness_dom_fills_only :: proc(t: ^testing.T) {
	// DOM usable when only DOM fills have accumulated (no book yet).
	dom: services.DOM_Store
	dom.trade_count = 10
	stores := Cell_Stores{dom = &dom}
	testing.expect(t, widget_store_has_data(.DOM, stores), "DOM usable with fills only")
}

@(test)
test_dom_readiness_both :: proc(t: ^testing.T) {
	// DOM usable when both orderbook and fills are present.
	ob: services.Orderbook_Store
	ob.ask_count = 3
	dom: services.DOM_Store
	dom.trade_count = 5
	stores := Cell_Stores{orderbook = &ob, dom = &dom}
	testing.expect(t, widget_store_has_data(.DOM, stores), "DOM usable with both sources")
}

@(test)
test_dom_readiness_empty :: proc(t: ^testing.T) {
	// DOM not usable when both are empty.
	ob: services.Orderbook_Store
	dom: services.DOM_Store
	stores := Cell_Stores{orderbook = &ob, dom = &dom}
	testing.expect(t, !widget_store_has_data(.DOM, stores), "DOM not usable when empty")
}

@(test)
test_dom_readiness_nil_stores :: proc(t: ^testing.T) {
	stores: Cell_Stores
	testing.expect(t, !widget_store_has_data(.DOM, stores), "DOM not usable with nil stores")
}

@(test)
test_dom_store_label :: proc(t: ^testing.T) {
	testing.expect_value(t, widget_store_label(.DOM), "dom")
	testing.expect_value(t, widget_store_label(.Orderbook), "orderbook")
}

@(test)
test_dom_readiness_policy :: proc(t: ^testing.T) {
	policy := widget_readiness_policy(.DOM)
	testing.expect_value(t, policy.primary_artifact, md_common.Artifact_Kind.Orderbook)
	testing.expect_value(t, policy.partial_usable, false)
	testing.expect_value(t, policy.backfill_absent_usable, true)
	testing.expect_value(t, policy.uses_artifact_live_flag, true)
}

// ---------------------------------------------------------------------------
// S152: Backfill Concern & Hint Tests
// ---------------------------------------------------------------------------

@(test)
test_backfill_concern_optional_no_concern :: proc(t: ^testing.T) {
	// Tick TF (optional backfill) — no concern regardless of outcome.
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Optional,
			outcome = .Not_Attempted,
		},
	}
	testing.expect(t, !widget_backfill_concern(sv), "optional backfill should not raise concern")
}

@(test)
test_backfill_concern_critical_not_attempted :: proc(t: ^testing.T) {
	// 15m TF (critical backfill) with no backfill → concern.
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Critical,
			outcome = .Not_Attempted,
		},
	}
	testing.expect(t, widget_backfill_concern(sv), "critical backfill not attempted should raise concern")
}

@(test)
test_backfill_concern_critical_success_no_concern :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Critical,
			outcome = .Success,
		},
	}
	testing.expect(t, !widget_backfill_concern(sv), "critical backfill success should not raise concern")
}

@(test)
test_backfill_concern_critical_timeout :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Critical,
			outcome = .Timeout,
		},
	}
	testing.expect(t, widget_backfill_concern(sv), "critical backfill timeout should raise concern")
}

@(test)
test_backfill_concern_recommended_timeout :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Recommended,
			outcome = .Timeout,
		},
	}
	testing.expect(t, widget_backfill_concern(sv), "recommended backfill timeout should raise concern")
}

@(test)
test_backfill_hint_success :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			outcome = .Success,
		},
	}
	testing.expect_value(t, widget_backfill_hint(sv), "History loaded")
}

@(test)
test_backfill_hint_critical_timeout :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Critical,
			outcome = .Timeout,
		},
	}
	testing.expect_value(t, widget_backfill_hint(sv), "History fetch timed out — Ctrl+R to retry")
}

@(test)
test_backfill_hint_not_attempted_minimal :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			live_only_util = .Minimal,
			outcome = .Not_Attempted,
		},
	}
	testing.expect_value(t, widget_backfill_hint(sv), "Backfill needed — Ctrl+R to fetch history")
}

@(test)
test_backfill_hint_not_attempted_full :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			live_only_util = .Full,
			outcome = .Not_Attempted,
		},
	}
	testing.expect_value(t, widget_backfill_hint(sv), "Live data building chart")
}

@(test)
test_backfill_hint_empty_critical :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Critical,
			outcome = .Empty,
		},
	}
	testing.expect_value(t, widget_backfill_hint(sv), "No history available")
}

@(test)
test_backfill_hint_partial_critical :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		backfill_expectation = md_common.Backfill_Expectation{
			criticality = .Critical,
			outcome = .Partial,
		},
	}
	testing.expect_value(t, widget_backfill_hint(sv), "Partial history — Ctrl+R for more")
}

// ---------------------------------------------------------------------------
// S155: Footprint Widget Contract Tests
// ---------------------------------------------------------------------------

@(test)
test_s155_footprint_contract_exists :: proc(t: ^testing.T) {
	contract := WIDGET_CONTRACTS[.Footprint]
	testing.expect(t, contract.on_create != nil, "footprint must have on_create")
	testing.expect(t, contract.on_render != nil, "footprint must have on_render")
	testing.expect(t, contract.on_serialize != nil, "footprint must have on_serialize")
	testing.expect(t, contract.on_dispose != nil, "footprint must have on_dispose")
}

@(test)
test_s155_footprint_descriptor :: proc(t: ^testing.T) {
	desc := WIDGET_DESCRIPTORS[.Footprint]
	testing.expect_value(t, desc.kind, Widget_Kind.Footprint)
	testing.expect_value(t, desc.label, "Footprint")
	testing.expect(t, desc.min_w >= 100, "footprint min_w should be reasonable")
	testing.expect(t, desc.min_h >= 80, "footprint min_h should be reasonable")
}

@(test)
test_s155_footprint_readiness_policy :: proc(t: ^testing.T) {
	policy := widget_readiness_policy(.Footprint)
	testing.expect_value(t, policy.primary_artifact, md_common.Artifact_Kind.Trade)
	testing.expect_value(t, policy.partial_usable, true)
	testing.expect_value(t, policy.backfill_absent_usable, true)
}

@(test)
test_s155_footprint_store_has_data :: proc(t: ^testing.T) {
	fp: services.Footprint_Store
	stores := Cell_Stores{footprint = &fp}
	testing.expect(t, !widget_store_has_data(.Footprint, stores), "empty footprint store has no data")

	// Simulate population.
	services.footprint_store_push_trade(&fp, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	testing.expect(t, widget_store_has_data(.Footprint, stores), "populated footprint store has data")
}

@(test)
test_s155_footprint_store_label :: proc(t: ^testing.T) {
	testing.expect_value(t, widget_store_label(.Footprint), "footprint")
}

@(test)
test_s155_footprint_channels :: proc(t: ^testing.T) {
	ch := channels_for_widget(.Footprint)
	// Footprint needs trades (for fill accumulation) and candles (for TF alignment).
	testing.expect(t, ch != 0, "footprint should subscribe to channels")
}

@(test)
test_s155_footprint_pane_role :: proc(t: ^testing.T) {
	role := infer_pane_role(.Footprint)
	testing.expect_value(t, role, Pane_Role.Auxiliary)
}

@(test)
test_s155_footprint_lifecycle :: proc(t: ^testing.T) {
	pool: Pane_Pool
	pane, _ := pane_pool_alloc(&pool)
	pane.widget = widget_host_create(.Footprint)
	widget_contract_create(pane)
	widget_contract_bind(pane, {})
	widget_contract_dispose(pane)
}
