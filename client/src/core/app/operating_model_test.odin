package app

// S117: Operating model contract tests.
// Validates context resolution, ownership rules, and invariants.

import "core:testing"

// ---------------------------------------------------------------------------
// Ownership tier tests
// ---------------------------------------------------------------------------

@(test)
test_ownership_global_categories :: proc(t: ^testing.T) {
	// All global state categories must return .Global.
	global_cats := [?]State_Category{
		.Connection, .Route, .Zen_Mode, .Focus_Mode,
		.Compare_Mode, .Global_TF, .Settings,
	}
	for cat in global_cats {
		tier := ownership_of(cat)
		testing.expectf(t, tier == .Global,
			"Expected .Global for %v, got %v", cat, tier)
	}
}

@(test)
test_ownership_workspace_categories :: proc(t: ^testing.T) {
	ws_cats := [?]State_Category{
		.Active_Stream, .Default_TF, .Layout_Tree,
		.Focus_Pane, .Workspace_Mode,
	}
	for cat in ws_cats {
		tier := ownership_of(cat)
		testing.expectf(t, tier == .Workspace,
			"Expected .Workspace for %v, got %v", cat, tier)
	}
}

@(test)
test_ownership_pane_categories :: proc(t: ^testing.T) {
	pane_cats := [?]State_Category{
		.Stream_Binding, .TF_Override, .Widget_Kind,
		.Indicators, .Chart_Config, .Analytics_Config, .View_State,
		.Pane_Role_Cat,  // S119
	}
	for cat in pane_cats {
		tier := ownership_of(cat)
		testing.expectf(t, tier == .Pane,
			"Expected .Pane for %v, got %v", cat, tier)
	}
}

// ---------------------------------------------------------------------------
// Global context resolution
// ---------------------------------------------------------------------------

@(test)
test_resolve_global_context_nil :: proc(t: ^testing.T) {
	ctx := resolve_global_context(nil)
	testing.expect(t, !ctx.connected, "Nil state should be disconnected")
	testing.expect(t, !ctx.zen_active, "Nil state should not be zen")
	testing.expect(t, ctx.active_tf_idx == 0, "Nil state TF should be 0")
}

@(test)
test_resolve_global_context_connected :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.conn.last_conn = .Connected
	state.was_ever_connected = true
	state.chrome.active_route = .Dashboard
	state.active_tf_idx = 3
	state.zen.active = true
	state.focus_mode = false
	state.compare.active = true

	ctx := resolve_global_context(state)
	testing.expect(t, ctx.connected, "Should be connected")
	testing.expect(t, ctx.was_ever_connected, "Should have been connected before")
	testing.expect(t, ctx.active_route == .Dashboard, "Route should be Dashboard")
	testing.expect(t, ctx.active_tf_idx == 3, "TF should be 3")
	testing.expect(t, ctx.zen_active, "Zen should be active")
	testing.expect(t, !ctx.focus_active, "Focus should not be active")
	testing.expect(t, ctx.compare_active, "Compare should be active")
}

// ---------------------------------------------------------------------------
// Workspace context resolution
// ---------------------------------------------------------------------------

@(test)
test_resolve_workspace_context_nil :: proc(t: ^testing.T) {
	gctx := Global_Context{active_tf_idx = 2}
	ctx := resolve_workspace_context(nil, gctx)
	testing.expect(t, ctx.workspace_id == Workspace_ID(0), "Nil ws should have zero ID")
	testing.expect(t, ctx.pane_count == 0, "Nil ws should have 0 panes")
}

@(test)
test_resolve_workspace_context_inherits_global_tf :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))
	ws.data_ctx.default_tf_idx = -1  // inherit from global

	gctx := Global_Context{active_tf_idx = 5}
	ctx := resolve_workspace_context(&ws, gctx)
	testing.expect(t, ctx.default_tf_idx == 5, "Should inherit global TF when workspace TF is -1")
}

@(test)
test_resolve_workspace_context_own_tf :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(2))
	ws.data_ctx.default_tf_idx = 4

	gctx := Global_Context{active_tf_idx = 2}
	ctx := resolve_workspace_context(&ws, gctx)
	testing.expect(t, ctx.default_tf_idx == 4, "Should use workspace's own TF")
}

// ---------------------------------------------------------------------------
// Pane context resolution — TF cascade
// ---------------------------------------------------------------------------

@(test)
test_pane_tf_inherits_from_workspace :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(1), alive = true, tf_override = -1}
	pane.widget = widget_host_create(.Candle)

	wctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = 3,
		focused_pane_id = PANE_ID_NONE,
	}
	gctx := Global_Context{active_tf_idx = 0}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.effective_tf_idx == 3, "Should inherit workspace TF")
	testing.expect(t, ctx.tf_source == .Workspace, "TF source should be Workspace")
}

@(test)
test_pane_tf_override :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(1), alive = true, tf_override = 7}
	pane.widget = widget_host_create(.Candle)

	wctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = 3,
		focused_pane_id = PANE_ID_NONE,
	}
	gctx := Global_Context{active_tf_idx = 0}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.effective_tf_idx == 7, "Should use pane override TF")
	testing.expect(t, ctx.tf_source == .Pane_Override, "TF source should be Pane_Override")
}

@(test)
test_pane_tf_falls_through_to_global :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(1), alive = true, tf_override = -1}
	pane.widget = widget_host_create(.Candle)

	wctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = -1, // workspace also has no override
		focused_pane_id = PANE_ID_NONE,
	}
	gctx := Global_Context{active_tf_idx = 6}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.effective_tf_idx == 6, "Should fall through to global TF")
	testing.expect(t, ctx.tf_source == .Global, "TF source should be Global")
}

// ---------------------------------------------------------------------------
// Pane context resolution — stream binding
// ---------------------------------------------------------------------------

@(test)
test_pane_follows_active_stream :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(1), alive = true, tf_override = -1}
	pane.widget = widget_host_create(.Candle)
	pane.binding.stream_idx = -1
	// No venue/symbol binding → follow active.

	wctx := Resolved_Workspace_Context{
		workspace_id     = Workspace_ID(1),
		active_stream_idx = 2,
		active_venue     = "binance-futures",
		active_symbol    = "BTCUSDT",
		default_tf_idx   = 3,
		focused_pane_id  = PANE_ID_NONE,
	}
	gctx := Global_Context{active_tf_idx = 3}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.follows_active, "Should follow active stream")
	testing.expect(t, !ctx.stream_bound, "Should not be stream_bound")
	testing.expect(t, ctx.stream_idx == 2, "Should inherit workspace active stream idx")
	testing.expect(t, ctx.venue == "binance-futures", "Should inherit workspace venue")
	testing.expect(t, ctx.symbol == "BTCUSDT", "Should inherit workspace symbol")
}

@(test)
test_pane_explicit_binding :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(2), alive = true, tf_override = -1}
	pane.widget = widget_host_create(.Stats)

	// Set explicit venue/symbol binding.
	venue := "kraken"
	for i in 0 ..< len(venue) {
		pane.binding.bound_venue[i] = venue[i]
	}
	pane.binding.bound_venue_len = u8(len(venue))

	sym := "ETHUSDT"
	for i in 0 ..< len(sym) {
		pane.binding.bound_symbol[i] = sym[i]
	}
	pane.binding.bound_symbol_len = u8(len(sym))
	pane.binding.stream_idx = 5

	wctx := Resolved_Workspace_Context{
		workspace_id     = Workspace_ID(1),
		active_stream_idx = 0,
		active_venue     = "binance",
		active_symbol    = "BTCUSDT",
		default_tf_idx   = 3,
		focused_pane_id  = PANE_ID_NONE,
	}
	gctx := Global_Context{active_tf_idx = 3}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.stream_bound, "Should be explicitly bound")
	testing.expect(t, !ctx.follows_active, "Should not follow active")
	testing.expect(t, ctx.venue == "kraken", "Venue should be from pane binding")
	testing.expect(t, ctx.symbol == "ETHUSDT", "Symbol should be from pane binding")
	testing.expect(t, ctx.stream_idx == 5, "Stream idx should be from pane binding")
}

// ---------------------------------------------------------------------------
// Pane context resolution — focus
// ---------------------------------------------------------------------------

@(test)
test_pane_focused :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(3), alive = true, tf_override = -1}
	pane.widget = widget_host_create(.Candle)

	wctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = 2,
		focused_pane_id = Pane_ID(3), // this pane is focused
	}
	gctx := Global_Context{active_tf_idx = 2}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.focused, "Pane should be focused")
}

@(test)
test_pane_not_focused :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(3), alive = true, tf_override = -1}
	pane.widget = widget_host_create(.Candle)

	wctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = 2,
		focused_pane_id = Pane_ID(7), // different pane
	}
	gctx := Global_Context{active_tf_idx = 2}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, !ctx.focused, "Pane should not be focused")
}

// ---------------------------------------------------------------------------
// Invariant checks
// ---------------------------------------------------------------------------

@(test)
test_pane_context_valid_good :: proc(t: ^testing.T) {
	ctx := Resolved_Pane_Context{
		pane_id          = Pane_ID(1),
		effective_tf_idx = 3,
		follows_active   = true,
		stream_bound     = false,
	}
	testing.expect(t, pane_context_valid(ctx), "Valid context should pass")
}

@(test)
test_pane_context_valid_no_id :: proc(t: ^testing.T) {
	ctx := Resolved_Pane_Context{
		pane_id          = PANE_ID_NONE,
		effective_tf_idx = 3,
	}
	testing.expect(t, !pane_context_valid(ctx), "Pane with no ID should fail")
}

@(test)
test_pane_context_valid_bad_tf :: proc(t: ^testing.T) {
	ctx := Resolved_Pane_Context{
		pane_id          = Pane_ID(1),
		effective_tf_idx = -1,
	}
	testing.expect(t, !pane_context_valid(ctx), "Negative TF should fail")
}

@(test)
test_pane_context_valid_tf_out_of_range :: proc(t: ^testing.T) {
	ctx := Resolved_Pane_Context{
		pane_id          = Pane_ID(1),
		effective_tf_idx = 99,
	}
	testing.expect(t, !pane_context_valid(ctx), "TF out of range should fail")
}

@(test)
test_pane_context_valid_follows_active_but_bound :: proc(t: ^testing.T) {
	ctx := Resolved_Pane_Context{
		pane_id          = Pane_ID(1),
		effective_tf_idx = 3,
		follows_active   = true,
		stream_bound     = true,  // contradiction
	}
	testing.expect(t, !pane_context_valid(ctx), "follows_active + stream_bound is invalid")
}

@(test)
test_workspace_context_valid_good :: proc(t: ^testing.T) {
	ctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = 2,
	}
	testing.expect(t, workspace_context_valid(ctx), "Valid workspace context should pass")
}

@(test)
test_workspace_context_valid_zero_id :: proc(t: ^testing.T) {
	ctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(0),
		default_tf_idx = 2,
	}
	testing.expect(t, !workspace_context_valid(ctx), "Zero workspace ID should fail")
}

@(test)
test_workspace_context_valid_bad_tf :: proc(t: ^testing.T) {
	ctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = -1,
	}
	testing.expect(t, !workspace_context_valid(ctx), "Negative default TF should fail")
}

// ---------------------------------------------------------------------------
// S119: Pane role resolution
// ---------------------------------------------------------------------------

@(test)
test_resolved_pane_context_includes_role :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(1), alive = true, tf_override = -1, role = .Primary_Chart}
	pane.widget = widget_host_create(.Candle)

	wctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = 2,
		focused_pane_id = PANE_ID_NONE,
	}
	gctx := Global_Context{active_tf_idx = 2}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.role == .Primary_Chart, "Resolved context should include Primary_Chart role")
}

@(test)
test_resolved_pane_context_auxiliary_role :: proc(t: ^testing.T) {
	pane := Pane{id = Pane_ID(2), alive = true, tf_override = -1, role = .Auxiliary}
	pane.widget = widget_host_create(.Stats)

	wctx := Resolved_Workspace_Context{
		workspace_id   = Workspace_ID(1),
		default_tf_idx = 2,
		focused_pane_id = PANE_ID_NONE,
	}
	gctx := Global_Context{active_tf_idx = 2}

	ctx := resolve_pane_context(&pane, wctx, gctx)
	testing.expect(t, ctx.role == .Auxiliary, "Resolved context should include Auxiliary role")
}

// ---------------------------------------------------------------------------
// Nil-safety
// ---------------------------------------------------------------------------

@(test)
test_resolve_pane_context_nil :: proc(t: ^testing.T) {
	wctx := Resolved_Workspace_Context{workspace_id = Workspace_ID(1), default_tf_idx = 2}
	gctx := Global_Context{active_tf_idx = 2}
	ctx := resolve_pane_context(nil, wctx, gctx)
	testing.expect(t, ctx.pane_id == PANE_ID_NONE, "Nil pane should yield PANE_ID_NONE")
}
