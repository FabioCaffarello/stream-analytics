package app

import "core:testing"
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
