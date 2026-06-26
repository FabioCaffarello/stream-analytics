package app

// S119: Pane role + context stack evolution tests.

import "core:testing"

// ---------------------------------------------------------------------------
// infer_pane_role
// ---------------------------------------------------------------------------

@(test)
test_infer_pane_role_candle :: proc(t: ^testing.T) {
	role := infer_pane_role(.Candle)
	testing.expect(t, role == .Primary_Chart, "Candle should infer Primary_Chart")
}

@(test)
test_infer_pane_role_auxiliary_widgets :: proc(t: ^testing.T) {
	aux_kinds := [?]Widget_Kind{.Stats, .Counter, .Trades, .Orderbook, .Heatmap, .VPVR, .DOM, .Analytics, .Session_VPVR, .TPO, .Empty}
	for kind in aux_kinds {
		role := infer_pane_role(kind)
		testing.expectf(t, role == .Auxiliary,
			"Widget %v should infer Auxiliary, got %v", kind, role)
	}
}

// ---------------------------------------------------------------------------
// Pane allocation sets role
// ---------------------------------------------------------------------------

@(test)
test_pane_alloc_sets_role_candle :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Candle)
	testing.expect(t, pid != PANE_ID_NONE, "Should alloc pane")
	pane := pane_pool_get(&ws.pane_pool, pid)
	testing.expect(t, pane != nil, "Pane should exist")
	testing.expect(t, pane.role == .Primary_Chart, "Candle pane should have Primary_Chart role")
}

@(test)
test_pane_alloc_sets_role_stats :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	pid := workspace_alloc_pane(&ws, .Stats)
	pane := pane_pool_get(&ws.pane_pool, pid)
	testing.expect(t, pane != nil, "Pane should exist")
	testing.expect(t, pane.role == .Auxiliary, "Stats pane should have Auxiliary role")
}

// ---------------------------------------------------------------------------
// Context tab availability
// ---------------------------------------------------------------------------

@(test)
test_context_tab_available_primary :: proc(t: ^testing.T) {
	// All tabs should be available for Primary_Chart.
	for ti in 0 ..< CONTEXT_TAB_COUNT {
		tab := Context_Tab(ti)
		avail := context_tab_available_for_role(tab, .Primary_Chart)
		testing.expectf(t, avail, "Tab %v should be available for Primary_Chart", tab)
	}
}

@(test)
test_context_tab_available_auxiliary :: proc(t: ^testing.T) {
	// Only Instrument tab should be available for Auxiliary.
	for ti in 0 ..< CONTEXT_TAB_COUNT {
		tab := Context_Tab(ti)
		avail := context_tab_available_for_role(tab, .Auxiliary)
		if tab == .Instrument {
			testing.expectf(t, avail, "Instrument tab should be available for Auxiliary")
		} else {
			testing.expectf(t, !avail, "Tab %v should NOT be available for Auxiliary", tab)
		}
	}
}

@(test)
test_context_tab_available_context :: proc(t: ^testing.T) {
	// No tabs should be available for Context role.
	for ti in 0 ..< CONTEXT_TAB_COUNT {
		tab := Context_Tab(ti)
		avail := context_tab_available_for_role(tab, .Context)
		testing.expectf(t, !avail, "Tab %v should NOT be available for Context role", tab)
	}
}

// ---------------------------------------------------------------------------
// Context tab cycling
// ---------------------------------------------------------------------------

@(test)
test_context_tab_next_available_primary :: proc(t: ^testing.T) {
	// Cycling from Stats for Primary_Chart should go to Trades.
	next := context_tab_next_available(.Stats, .Primary_Chart)
	testing.expect(t, next == .Trades, "Next from Stats for Primary_Chart should be Trades")
}

@(test)
test_context_tab_next_wraps :: proc(t: ^testing.T) {
	// Cycling from Analytics (last) for Primary_Chart should wrap to Stats.
	next := context_tab_next_available(.Analytics, .Primary_Chart)
	testing.expect(t, next == .Stats, "Next from Analytics should wrap to Stats")
}

@(test)
test_context_tab_next_skips_unavailable :: proc(t: ^testing.T) {
	// For Auxiliary role, cycling from Instrument should stay on Instrument (only available tab).
	next := context_tab_next_available(.Instrument, .Auxiliary)
	testing.expect(t, next == .Instrument, "Auxiliary has only Instrument, should stay")
}

// ---------------------------------------------------------------------------
// Workstation tree
// ---------------------------------------------------------------------------

@(test)
test_workstation_tree_valid :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	tree, pane_ids := build_workstation_workspace_tree(&ws)
	ws.tree = tree

	testing.expect(t, tree_validate(&tree), "Workstation tree should be valid")
	testing.expect(t, tree_pane_count(&tree) == 3, "Should have 3 panes")

	// All pane IDs should be valid.
	for i in 0 ..< 3 {
		testing.expectf(t, pane_ids[i] != PANE_ID_NONE,
			"Pane %d should have valid ID", i)
	}
}

@(test)
test_workstation_tree_roles :: proc(t: ^testing.T) {
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))

	_, pane_ids := build_workstation_workspace_tree(&ws)

	// First pane (Candle) should be Primary_Chart.
	p0 := pane_pool_get(&ws.pane_pool, pane_ids[0])
	testing.expect(t, p0 != nil, "Pane 0 should exist")
	testing.expect(t, p0.role == .Primary_Chart, "Pane 0 should be Primary_Chart")
	testing.expect(t, p0.widget.kind == .Candle, "Pane 0 should be Candle widget")

	// Second pane (Stats) should be Auxiliary.
	p1 := pane_pool_get(&ws.pane_pool, pane_ids[1])
	testing.expect(t, p1 != nil, "Pane 1 should exist")
	testing.expect(t, p1.role == .Auxiliary, "Pane 1 should be Auxiliary")

	// Third pane (Trades) should be Auxiliary.
	p2 := pane_pool_get(&ws.pane_pool, pane_ids[2])
	testing.expect(t, p2 != nil, "Pane 2 should exist")
	testing.expect(t, p2.role == .Auxiliary, "Pane 2 should be Auxiliary")
}

// ---------------------------------------------------------------------------
// Schema version
// ---------------------------------------------------------------------------

@(test)
test_workspace_schema_version_s119 :: proc(t: ^testing.T) {
	testing.expect(t, WORKSPACE_SCHEMA_VERSION >= 11,
		"Schema version should be >= 11 for S119")
}

// ---------------------------------------------------------------------------
// Ownership: Pane_Role_Cat
// ---------------------------------------------------------------------------

@(test)
test_ownership_pane_role :: proc(t: ^testing.T) {
	tier := ownership_of(.Pane_Role_Cat)
	testing.expect(t, tier == .Pane, "Pane_Role_Cat should be owned by Pane tier")
}
