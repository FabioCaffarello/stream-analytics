package app

import "mr:services"

apply_set_cell_widget_action :: proc(state: ^App_State, action: UI_Action) {
	if state == nil do return
	ci := action.cell_idx
	if ci < 0 || ci >= state.world.count do return
	state.world.widgets[ci].kind = action.widget_kind
	// S55: set analytics sub-kind when switching to Analytics widget.
	if action.widget_kind == .Analytics {
		state.world.analytics[ci].analytics_kind = action.analytics_kind
		state.world.analytics[ci].show_history = true
		// S81: Trigger historical analytics fetch for cold start.
		request_analytics_range(state, ci)
	}

	// S112: Sync widget kind + analytics to pane.
	if ws := workspace_registry_active(&state.ws_registry); ws != nil {
		pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
		if ci < pane_count {
			if pane := pane_pool_get(&ws.pane_pool, pane_ids[ci]); pane != nil {
				pane.widget = widget_host_create(action.widget_kind)
				if action.widget_kind == .Analytics {
					pane.analytics.analytics_kind = action.analytics_kind
					pane.analytics.show_history = true
				}
			}
		}
	}

	persist_layout_v6(state)
	reconcile_subscriptions(state)
}

apply_set_cell_stream_action :: proc(state: ^App_State, action: UI_Action) {
	if state == nil do return
	ci := action.cell_idx
	if ci < 0 || ci >= state.world.count do return

	bind := &state.world.bindings[ci]
	old_stream_idx := bind.stream_idx
	old_has_binding := binding_has(bind)
	// PRD-0009: if action carries venue/symbol, set binding and reset stream_idx for lazy resolution.
	if len(action.bind_venue) > 0 && len(action.bind_symbol) > 0 {
		binding_set(bind, action.bind_venue, action.bind_symbol)
		bind.stream_idx = -1
	} else if action.stream_idx < 0 {
		// "Follow Active" — clear binding.
		binding_clear(bind)
		bind.stream_idx = -1
	} else {
		bind.stream_idx = action.stream_idx
	}

	// Reset DOM/footprint stores when stream actually changes.
	stream_changed := bind.stream_idx != old_stream_idx || (len(action.bind_venue) > 0) || old_has_binding
	if stream_changed {
		services.dom_store_reset(&state.stores.dom)
		services.footprint_store_reset(&state.stores.footprint)
	}
	state.world.getranges[ci].pending = false
	state.world.getranges[ci].seeded = false
	state.world.getranges[ci].oldest_ts = 0

	// S112: Sync binding to pane (pane is source of truth for contract path).
	if ws := workspace_registry_active(&state.ws_registry); ws != nil {
		pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
		if ci < pane_count {
			if pane := pane_pool_get(&ws.pane_pool, pane_ids[ci]); pane != nil {
				pane.binding = state.world.bindings[ci]
			}
		}
	}

	persist_layout_v6(state)
	reconcile_subscriptions(state)
	request_cell_candle_range(state, ci)
}

apply_add_cell_action :: proc(state: ^App_State, action: UI_Action) {
	if state == nil do return
	if state.world.count >= CELL_MAX {
		show_toast(state, "Max 12 cells")
		return
	}
	ci := state.world.count
	init_world_cell_defaults(state, ci, action.widget_kind, action.stream_idx)
	// S48: set analytics sub-kind for analytics widgets.
	if action.widget_kind == .Analytics {
		state.world.analytics[ci] = Analytics_Component{
			analytics_kind = action.analytics_kind,
			show_history   = true,
		}
	}
	// PRD-0009: if action carries venue/symbol, set binding.
	if len(action.bind_venue) > 0 && len(action.bind_symbol) > 0 {
		binding_set(&state.world.bindings[ci], action.bind_venue, action.bind_symbol)
		state.world.bindings[ci].stream_idx = -1
	}
	state.world.count += 1
	// S106: Sync workspace tree.
	workspace_on_cell_added(state, ci, action.widget_kind)

	// S112: Sync Entity_World state to the newly created pane.
	if ws := workspace_registry_active(&state.ws_registry); ws != nil {
		pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
		if ci < pane_count {
			if pane := pane_pool_get(&ws.pane_pool, pane_ids[ci]); pane != nil {
				pane.binding = state.world.bindings[ci]
				pane.analytics = state.world.analytics[ci]
			}
		}
	}

	persist_layout_v6(state)
	reconcile_subscriptions(state)
	request_cell_candle_range(state, ci)
}

// S53: Set col/row span for a cell (routed through action queue).
apply_set_cell_span_action :: proc(state: ^App_State, cell_idx, col_span, row_span: int) {
	if state == nil do return
	if cell_idx < 0 || cell_idx >= state.world.count do return
	state.world.spans[cell_idx].col_span = col_span
	state.world.spans[cell_idx].row_span = row_span
	persist_layout_v6(state)
}

// S53: Clear all cells and open widget catalog.
apply_clear_all_cells_action :: proc(state: ^App_State) {
	if state == nil do return
	state.world.count = 0
	state.overlays.show_widget_catalog = true
	state.overlays.catalog_step = 0
	persist_layout_v6(state)
}

// S106: Split the focused pane in the workspace tree.
apply_split_pane_action :: proc(state: ^App_State, dir: Split_Node_Kind) {
	if state == nil do return
	if state.world.count >= CELL_MAX {
		show_toast(state, "Max 12 cells")
		return
	}

	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil do return

	// Determine which pane to split (focused cell → pane at DFS position).
	focus_ci := state.world.focused
	if focus_ci < 0 || focus_ci >= state.world.count {
		focus_ci = state.world.count - 1
		if focus_ci < 0 do return
	}

	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
	if focus_ci >= pane_count do return

	target_id := pane_ids[focus_ci]

	// Add a new cell to Entity_World (same widget kind as focused cell).
	new_ci := state.world.count
	new_widget := state.world.widgets[focus_ci].kind
	init_world_cell_defaults(state, new_ci, new_widget)
	state.world.count += 1

	// Split the pane in the tree.
	new_id := tree_split_pane(&ws.tree, &ws.pane_pool, target_id, dir, new_widget)
	if new_id == PANE_ID_NONE {
		// Rollback Entity_World add.
		state.world.count -= 1
		show_toast(state, "Cannot split: tree full")
		return
	}

	persist_layout_v6(state)
	reconcile_subscriptions(state)
	show_toast(state, dir == .Split_H ? "Split H" : "Split V")
}

// S106: Rotate the split direction at the focused pane's parent.
apply_rotate_split_action :: proc(state: ^App_State) {
	if state == nil do return

	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil do return

	focus_ci := state.world.focused
	if focus_ci < 0 || focus_ci >= state.world.count do return

	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
	if focus_ci >= pane_count do return

	// Find the pane's leaf node, then get its parent (which is a split node).
	target_id := pane_ids[focus_ci]
	for i in 0 ..< int(ws.tree.count) {
		if ws.tree.nodes[i].kind == .Pane && Pane_ID(ws.tree.nodes[i].children[0]) == target_id {
			parent := ws.tree.nodes[i].parent
			if parent >= 0 {
				tree_rotate(&ws.tree, parent)
				show_toast(state, "Rotated split")
			}
			break
		}
	}
}

// S119: Apply workstation preset — 1 chart + 2 auxiliary panes + context stack.
apply_workstation_preset :: proc(state: ^App_State) {
	if state == nil do return
	state.layout_preset = 4

	// Rebuild Entity_World with 3 cells: Candle, Stats, Trades.
	ws_kinds := [3]Widget_Kind{.Candle, .Stats, .Trades}
	count := min(3, CELL_MAX)
	state.world.count = count
	for ci in 0 ..< count {
		state.world.widgets[ci] = {kind = ws_kinds[ci]}
		state.world.bindings[ci] = {}
		state.world.views[ci] = {}
		state.world.indicators[ci] = {}
		state.world.ind_params[ci] = {}
		state.world.charts[ci] = {}
		state.world.subplots[ci] = {}
		state.world.spans[ci] = {}
		state.world.timeframes[ci] = {tf_idx = -1}
		state.world.analytics[ci] = {}
		state.world.getranges[ci] = {}
	}
	// Clear remaining slots.
	for ci in count ..< CELL_MAX {
		state.world.widgets[ci] = {}
		state.world.bindings[ci] = {}
	}
	state.world.focused = 0

	// Build workstation tree.
	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil {
		ws = workspace_registry_alloc(&state.ws_registry)
	}
	if ws != nil {
		ws.pane_pool = {}
		ws.tree, _ = build_workstation_workspace_tree(ws)
		workspace_sync_panes_from_world(state)
		// S119: Auto-expand context stack in workstation mode.
		state.chrome.context_stack.expanded = true
		if state.chrome.context_stack.width <= 0 {
			state.chrome.context_stack.width = CONTEXT_STACK_W_DEFAULT
		}
	}

	persist_layout_v6(state)
	reconcile_subscriptions(state)
	show_toast(state, "Workstation Layout")
}

apply_remove_cell_action :: proc(state: ^App_State, cell_idx: int) {
	if state == nil do return
	if cell_idx < 0 || cell_idx >= state.world.count || state.world.count <= 1 do return

	// BUG-15: Adjust focused cell index before compacting.
	if state.world.focused == cell_idx {
		state.world.focused = -1
	} else if state.world.focused > cell_idx {
		state.world.focused -= 1
	}
	for j in cell_idx ..< state.world.count - 1 {
		state.world.widgets[j]    = state.world.widgets[j + 1]
		state.world.bindings[j]   = state.world.bindings[j + 1]
		state.world.views[j]      = state.world.views[j + 1]
		state.world.indicators[j] = state.world.indicators[j + 1]
		state.world.ind_params[j] = state.world.ind_params[j + 1]
		state.world.charts[j]     = state.world.charts[j + 1]
		state.world.subplots[j]   = state.world.subplots[j + 1]
		state.world.spans[j]      = state.world.spans[j + 1]
		state.world.timeframes[j] = state.world.timeframes[j + 1]
		state.world.analytics[j]  = state.world.analytics[j + 1]
		state.world.getranges[j]  = state.world.getranges[j + 1]
	}
	// S106: Sync workspace tree BEFORE compacting (uses pre-compact index).
	workspace_on_cell_removed(state, cell_idx)
	state.world.count -= 1
	last := state.world.count
	state.world.widgets[last]    = {}
	state.world.bindings[last]   = {}
	state.world.views[last]      = {}
	state.world.indicators[last] = {}
	state.world.ind_params[last] = {}
	state.world.charts[last]     = {}
	state.world.subplots[last]   = {}
	state.world.spans[last]      = {}
	state.world.timeframes[last] = {}
	state.world.analytics[last]  = {}
	state.world.getranges[last]  = {}
	persist_layout_v6(state)
	reconcile_subscriptions(state)
}
