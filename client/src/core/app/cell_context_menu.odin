package app

import "mr:services"
import "mr:ui"

// S53/S55: Cell context menu — right-click on a grid cell opens a menu for
// widget type (all 12), analytics sub-kinds (OI/DV/CVD/BS), add/remove, span controls.

@(private = "package")
build_cell_context_menu :: proc(
	state: ^App_State,
	workspace_pointer: ui.Pointer_Input,
	viewport_w, viewport_h: f32,
) {
	if !state.cell_context_menu.open do return

	cci := state.cell_context_cell_idx
	current_widget := Widget_Kind.Empty
	current_ak := services.Analytics_Kind.Open_Interest
	if cci >= 0 && cci < state.world.count {
		current_widget = state.world.widgets[cci].kind
		current_ak = state.world.analytics[cci].analytics_kind
	}

	menu_items: [ui.CONTEXT_MENU_MAX_ITEMS]ui.Context_Menu_Item
	menu_count := 0

	// --- Chart widgets (0-7) ---
	CHART_LABELS :: [8]string{"Candle", "Stats", "Counter", "Heatmap", "VPVR", "Trades", "Orderbook", "DOM"}
	chart_labels := CHART_LABELS
	for i in 0 ..< 8 {
		menu_items[menu_count] = ui.Context_Menu_Item{
			label    = chart_labels[i],
			selected = Widget_Kind(i) == current_widget,
		}
		menu_count += 1
	}
	// Empty.
	menu_items[menu_count] = {label = "Empty", selected = current_widget == .Empty}
	empty_idx := menu_count
	menu_count += 1

	// --- Analytics sub-kinds (with divider before first) ---
	ANALYTICS_LABELS :: [4]string{"OI: Open Interest", "DV: Delta Volume", "CVD", "BS: Bar Stats"}
	analytics_labels := ANALYTICS_LABELS
	analytics_base_idx := menu_count
	for i in 0 ..< 4 {
		ak := services.Analytics_Kind(i)
		menu_items[menu_count] = ui.Context_Menu_Item{
			label    = analytics_labels[i],
			selected = current_widget == .Analytics && current_ak == ak,
			divider  = i == 0,
		}
		menu_count += 1
	}

	// --- Session profile widgets (with divider before first) ---
	menu_items[menu_count] = {label = "Session VPVR", selected = current_widget == .Session_VPVR, divider = true}
	svpvr_idx := menu_count
	menu_count += 1
	menu_items[menu_count] = {label = "TPO Profile", selected = current_widget == .TPO}
	tpo_idx := menu_count
	menu_count += 1

	// --- Actions (with divider) ---
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
		ci := menu_res.clicked_idx
		if ci < 8 {
			// Chart widget (direct Widget_Kind mapping 0-7).
			queue_ui_action(state, UI_Action{
				kind        = .Set_Cell_Widget,
				cell_idx    = cci,
				widget_kind = Widget_Kind(ci),
			})
		} else if ci == empty_idx {
			queue_ui_action(state, UI_Action{
				kind        = .Set_Cell_Widget,
				cell_idx    = cci,
				widget_kind = .Empty,
			})
		} else if ci >= analytics_base_idx && ci < analytics_base_idx + 4 {
			// Analytics sub-kind.
			queue_ui_action(state, UI_Action{
				kind           = .Set_Cell_Widget,
				cell_idx       = cci,
				widget_kind    = .Analytics,
				analytics_kind = services.Analytics_Kind(ci - analytics_base_idx),
			})
		} else if ci == svpvr_idx {
			queue_ui_action(state, UI_Action{
				kind        = .Set_Cell_Widget,
				cell_idx    = cci,
				widget_kind = .Session_VPVR,
			})
		} else if ci == tpo_idx {
			queue_ui_action(state, UI_Action{
				kind        = .Set_Cell_Widget,
				cell_idx    = cci,
				widget_kind = .TPO,
			})
		} else if ci == add_cell_idx {
			queue_ui_action(state, UI_Action{kind = .Add_Cell})
		} else if ci == remove_cell_idx {
			queue_ui_action(state, UI_Action{kind = .Remove_Cell, cell_idx = cci})
		} else if ci == expand_right_idx && cci >= 0 && cci < state.world.count {
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Span, cell_idx = cci, col_span = min((state.world.spans[cci].col_span > 1 ? state.world.spans[cci].col_span : 1) + 1, 4), row_span = max(state.world.spans[cci].row_span, 1)})
		} else if ci == expand_down_idx && cci >= 0 && cci < state.world.count {
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Span, cell_idx = cci, col_span = max(state.world.spans[cci].col_span, 1), row_span = min((state.world.spans[cci].row_span > 1 ? state.world.spans[cci].row_span : 1) + 1, 4)})
		} else if ci == reset_size_idx && cci >= 0 && cci < state.world.count {
			queue_ui_action(state, UI_Action{kind = .Set_Cell_Span, cell_idx = cci, col_span = 1, row_span = 1})
		} else if ci == clear_all_idx {
			queue_ui_action(state, UI_Action{kind = .Clear_All_Cells})
		}
	}
}
