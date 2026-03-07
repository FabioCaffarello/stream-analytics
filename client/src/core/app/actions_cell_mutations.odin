package app

import "mr:services"

apply_set_cell_widget_action :: proc(state: ^App_State, cell_idx: int, widget_kind: Widget_Kind) {
	if state == nil do return
	if cell_idx < 0 || cell_idx >= state.world.count do return
	state.world.widgets[cell_idx].kind = widget_kind
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
