package app

import "mr:ui"

// S53: Cell context menu — extracted from build_dashboard.odin.
// Right-click on a grid cell opens a menu for widget type, add/remove, span controls.

@(private = "package")
build_cell_context_menu :: proc(
	state: ^App_State,
	workspace_pointer: ui.Pointer_Input,
	viewport_w, viewport_h: f32,
) {
	if !state.cell_context_menu.open do return

	cci := state.cell_context_cell_idx
	current_widget := Widget_Kind.Empty
	if cci >= 0 && cci < state.world.count {
		current_widget = state.world.widgets[cci].kind
	}
	WIDGET_LABELS :: [12]string{"Candle", "Stats", "Counter", "Heatmap", "VPVR", "Trades", "Orderbook", "DOM", "Empty", "Analytics", "Session VPVR", "TPO"}
	widget_labels := WIDGET_LABELS
	menu_items: [ui.CONTEXT_MENU_MAX_ITEMS]ui.Context_Menu_Item
	menu_count := 0
	WIDGET_MENU_COUNT :: 10
	for i in 0 ..< WIDGET_MENU_COUNT {
		menu_items[menu_count] = ui.Context_Menu_Item{
			label    = widget_labels[i],
			selected = Widget_Kind(i) == current_widget,
		}
		menu_count += 1
	}
	// Add Cell + Remove Cell.
	menu_items[menu_count] = {label = "+ Add Cell", divider = true}
	add_cell_idx := menu_count
	menu_count += 1
	menu_items[menu_count] = {label = "- Remove", divider = false}
	remove_cell_idx := menu_count
	menu_count += 1
	// Span controls (PRD-0007 M2).
	expand_right_idx := -1
	expand_down_idx := -1
	reset_size_idx := -1
	clear_all_idx := -1
	if state.layout_mode == .Custom {
		menu_items[menu_count] = {label = "Expand ->", divider = true}
		expand_right_idx = menu_count
		menu_count += 1
		menu_items[menu_count] = {label = "Expand v", divider = false}
		expand_down_idx = menu_count
		menu_count += 1
		has_span := cci >= 0 && cci < state.world.count &&
			(state.world.spans[cci].col_span > 1 || state.world.spans[cci].row_span > 1)
		if has_span {
			menu_items[menu_count] = {label = "Reset Size", divider = false}
			reset_size_idx = menu_count
			menu_count += 1
		}
		menu_items[menu_count] = {label = "Clear All", divider = true}
		clear_all_idx = menu_count
		menu_count += 1
	}

	menu_res := ui.context_menu(&state.cmd_buf, &state.cell_context_menu,
		menu_items[:menu_count], workspace_pointer, state.text.measure,
		ui.Rect{pos = {0, 0}, size = {viewport_w, viewport_h}})
	if menu_res.clicked_idx >= 0 {
		if menu_res.clicked_idx < WIDGET_MENU_COUNT {
			queue_ui_action(state, UI_Action{
				kind        = .Set_Cell_Widget,
				cell_idx    = cci,
				widget_kind = Widget_Kind(menu_res.clicked_idx),
			})
		} else if menu_res.clicked_idx == add_cell_idx {
			queue_ui_action(state, UI_Action{kind = .Add_Cell})
		} else if menu_res.clicked_idx == remove_cell_idx {
			queue_ui_action(state, UI_Action{kind = .Remove_Cell, cell_idx = cci})
		} else if menu_res.clicked_idx == expand_right_idx && cci >= 0 && cci < state.world.count {
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Span, cell_idx = cci, col_span = min((state.world.spans[cci].col_span > 1 ? state.world.spans[cci].col_span : 1) + 1, 4), row_span = max(state.world.spans[cci].row_span, 1)})
		} else if menu_res.clicked_idx == expand_down_idx && cci >= 0 && cci < state.world.count {
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Span, cell_idx = cci, col_span = max(state.world.spans[cci].col_span, 1), row_span = min((state.world.spans[cci].row_span > 1 ? state.world.spans[cci].row_span : 1) + 1, 4)})
		} else if menu_res.clicked_idx == reset_size_idx && cci >= 0 && cci < state.world.count {
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Span, cell_idx = cci, col_span = 1, row_span = 1})
		} else if menu_res.clicked_idx == clear_all_idx {
			queue_ui_action(state, UI_Action{kind = .Clear_All_Cells})
		}
	}
}
