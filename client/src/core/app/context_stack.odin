package app

// S113/S119: Context Stack — right-side tabbed panel for active pane context.
// Provides quick access to Stats, Trades, OrderBook, Counter, DOM, Analytics,
// Instrument data for the focused pane, without requiring separate grid panes.
// S119: Role-aware tab filtering — shows all tabs for Primary_Chart panes,
// limited tabs for Auxiliary panes.

import "core:fmt"
import "mr:ui"

CONTEXT_STACK_TAB_H :: f32(28)
CONTEXT_ROLE_BADGE_H :: f32(16)  // S119: role badge height above tab bar

@(rodata)
CONTEXT_TAB_LABELS  := [CONTEXT_TAB_COUNT]string{"Stats", "Trades", "OB", "Ctr", "Info", "DOM", "Ana"}

@(rodata)
CONTEXT_TAB_WIDGETS := [CONTEXT_TAB_COUNT]Widget_Kind{.Stats, .Trades, .Orderbook, .Counter, .Empty, .DOM, .Analytics}

// S119: Check if a context tab is available for a given pane role.
context_tab_available_for_role :: proc(tab: Context_Tab, role: Pane_Role) -> bool {
	switch role {
	case .Primary_Chart:
		return true  // all tabs available for chart panes
	case .Auxiliary:
		// Auxiliary panes only get Instrument info tab
		return tab == .Instrument
	case .Context:
		return false  // context panes don't get context stack
	}
	return true
}

// S119: Count available tabs for a role.
@(private = "file")
context_tab_count_for_role :: proc(role: Pane_Role) -> int {
	count := 0
	for ti in 0 ..< CONTEXT_TAB_COUNT {
		if context_tab_available_for_role(Context_Tab(ti), role) {
			count += 1
		}
	}
	return count
}

// S119: Find the next available tab for a role, cycling forward.
context_tab_next_available :: proc(current: Context_Tab, role: Pane_Role) -> Context_Tab {
	start := int(current)
	for offset in 1 ..= CONTEXT_TAB_COUNT {
		idx := (start + offset) % CONTEXT_TAB_COUNT
		tab := Context_Tab(idx)
		if context_tab_available_for_role(tab, role) {
			return tab
		}
	}
	return current  // no other tab available — stay on current
}

// S119: Resolve the focused pane's role (returns .Primary_Chart as default).
@(private = "file")
resolve_focused_pane_role :: proc(state: ^App_State) -> Pane_Role {
	ws := workspace_registry_active(&state.ws_registry)
	if ws == nil do return .Primary_Chart
	pane := pane_pool_get(&ws.pane_pool, ws.focus.active)
	if pane == nil do return .Primary_Chart
	return pane.role
}

// S119: Role badge label.
@(private = "file")
role_badge_label :: proc(role: Pane_Role) -> string {
	switch role {
	case .Primary_Chart: return "CHART"
	case .Auxiliary:      return "AUX"
	case .Context:        return "CTX"
	}
	return "---"
}

// Draw the context stack panel at the given rect.
@(private = "package")
draw_context_stack :: proc(
	state: ^App_State,
	rect: ui.Rect,
	pointer: ui.Pointer_Input,
) {
	if rect.size.x <= 0 || rect.size.y <= 0 do return

	cs_state := &state.chrome.context_stack

	// S119: Resolve focused pane role.
	focused_role := resolve_focused_pane_role(state)

	// Background.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = rect, color = ui.COL_SURFACE_1})

	// Left border.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from      = {rect.pos.x, rect.pos.y},
		to        = {rect.pos.x, ui.rect_bottom(rect)},
		color     = ui.COL_DIVIDER,
		thickness = 1,
	})

	// S119: Role badge strip above tabs.
	badge_rect := ui.Rect{
		pos  = {rect.pos.x, rect.pos.y},
		size = {rect.size.x, CONTEXT_ROLE_BADGE_H},
	}
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = badge_rect, color = ui.COL_SURFACE_0})
	badge_label := role_badge_label(focused_role)
	badge_color := focused_role == .Primary_Chart ? ui.COL_BLUE : ui.COL_TEXT_MUTED
	badge_text_y := badge_rect.pos.y + CONTEXT_ROLE_BADGE_H * 0.5 + ui.FONT_SIZE_XS * 0.3
	ui.push_text(&state.cmd_buf, {rect.pos.x + 6, badge_text_y},
		badge_label, badge_color, ui.FONT_SIZE_XS, .Bold)

	// --- Tab bar ---
	tab_bar_y := rect.pos.y + CONTEXT_ROLE_BADGE_H
	tab_bar_rect := ui.Rect{
		pos  = {rect.pos.x, tab_bar_y},
		size = {rect.size.x, CONTEXT_STACK_TAB_H},
	}
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = tab_bar_rect, color = ui.COL_SURFACE_2})

	// S119: Count available tabs for layout.
	avail_count := context_tab_count_for_role(focused_role)
	if avail_count <= 0 do avail_count = 1  // prevent divide by zero

	// Tab buttons — only render available tabs.
	tab_labels := CONTEXT_TAB_LABELS
	tab_w := rect.size.x / f32(avail_count)
	tab_text_y := tab_bar_rect.pos.y + CONTEXT_STACK_TAB_H * 0.5 + ui.FONT_SIZE_XS * 0.35
	drawn_idx := 0

	for ti in 0 ..< CONTEXT_TAB_COUNT {
		tab := Context_Tab(ti)
		available := context_tab_available_for_role(tab, focused_role)
		if !available do continue

		tab_x := rect.pos.x + f32(drawn_idx) * tab_w
		tab_rect := ui.Rect{pos = {tab_x, tab_bar_rect.pos.y}, size = {tab_w, CONTEXT_STACK_TAB_H}}
		hovered := ui.rect_contains(tab_rect, pointer.pos)
		is_active := tab == cs_state.active_tab

		// Tab background.
		if is_active {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = tab_rect,
				color = ui.with_alpha(ui.COL_BLUE, 0.2),
			})
			// Active indicator (bottom bar).
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = ui.rect_xywh(tab_x, tab_bar_rect.pos.y + CONTEXT_STACK_TAB_H - 2, tab_w, 2),
				color = ui.COL_BLUE,
			})
		} else if hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = tab_rect,
				color = ui.with_alpha(ui.COL_WHITE, 0.04),
			})
		}

		// Tab label.
		label := tab_labels[ti]
		label_w := state.text.measure(ui.FONT_SIZE_XS, label).x
		label_x := tab_x + (tab_w - label_w) * 0.5
		text_color := is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_MUTED
		font_id := is_active ? ui.Font_Id.Bold : ui.Font_Id.Mono
		ui.push_text(&state.cmd_buf, {label_x, tab_text_y},
			label, text_color, ui.FONT_SIZE_XS, font_id)

		// Click handler.
		if hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{kind = .Set_Context_Tab, context_tab = tab})
		}

		drawn_idx += 1
	}

	// S119: If current active tab is not available for this role, snap to first available.
	if !context_tab_available_for_role(cs_state.active_tab, focused_role) {
		for ti in 0 ..< CONTEXT_TAB_COUNT {
			tab := Context_Tab(ti)
			if context_tab_available_for_role(tab, focused_role) {
				cs_state.active_tab = tab
				break
			}
		}
	}

	// Divider below tab bar.
	div_y := tab_bar_rect.pos.y + CONTEXT_STACK_TAB_H
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from      = {rect.pos.x, div_y},
		to        = {ui.rect_right(rect), div_y},
		color     = ui.COL_DIVIDER,
		thickness = 1,
	})

	// --- Content area ---
	content_rect := ui.Rect{
		pos  = {rect.pos.x + 1, div_y + 1},
		size = {rect.size.x - 1, rect.size.y - CONTEXT_ROLE_BADGE_H - CONTEXT_STACK_TAB_H - 1},
	}
	if content_rect.size.y <= 0 do return

	// Render the active tab's widget using the focused cell's data.
	tab_widgets := CONTEXT_TAB_WIDGETS
	active_widget := tab_widgets[int(cs_state.active_tab)]

	if active_widget == .Empty {
		// Instrument info tab — render basic instrument info.
		draw_context_instrument_info(state, content_rect)
		return
	}

	// Find the focused cell index to pull data from.
	fci := state.world.focused
	if fci < 0 || fci >= state.world.count {
		// No focused cell — show placeholder.
		label := "No active pane"
		label_w := state.text.measure(ui.FONT_SIZE_SM, label).x
		cx := content_rect.pos.x + (content_rect.size.x - label_w) * 0.5
		cy := content_rect.pos.y + content_rect.size.y * 0.4
		ui.push_text(&state.cmd_buf, {cx, cy}, label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_SM, .Mono)
		return
	}

	// Render the widget through the existing layer canvas.
	render_cell_layer_canvas(state, fci, active_widget, content_rect)
}

// Render instrument info for the Instrument tab.
@(private = "file")
draw_context_instrument_info :: proc(state: ^App_State, rect: ui.Rect) {
	content := ui.rect_pad(rect, 8)
	cursor_y := content.pos.y + 4
	text_x := content.pos.x

	// Title.
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"INSTRUMENT", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	cursor_y += 18

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {text_x, cursor_y}, to = {text_x + content.size.x - 16, cursor_y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	cursor_y += 6

	// Active instrument details.
	active_venue := "---"
	active_symbol := "---"
	if reg := state.stream_views; reg != nil && reg.count > 0 {
		if slot := stream_view_active_slot(reg); slot != nil {
			if slot.has_stream_info {
				info := slot.stream_info
				if len(info.venue) > 0 do active_venue = info.venue
				if len(info.symbol) > 0 do active_symbol = info.symbol
			}
		}
	}

	// Row: Venue
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"Venue", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {text_x + 60, cursor_y + ui.FONT_SIZE_XS * 0.35},
		active_venue, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)
	cursor_y += 16

	// Row: Symbol
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"Symbol", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {text_x + 60, cursor_y + ui.FONT_SIZE_XS * 0.35},
		active_symbol, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)
	cursor_y += 16

	// Row: TF
	tf_opts := TF_OPTIONS
	tf_str := tf_opts[state.active_tf_idx] if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_opts) else "?"
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"TF", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {text_x + 60, cursor_y + ui.FONT_SIZE_XS * 0.35},
		tf_str, ui.COL_BLUE, ui.FONT_SIZE_XS, .Mono)
	cursor_y += 16

	// Row: Streams
	stream_count_buf: [8]u8
	sc_str := "0"
	if state.stream_views != nil {
		sc_str = fmt.bprintf(stream_count_buf[:], "%d", state.stream_views.count)
	}
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"Streams", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {text_x + 60, cursor_y + ui.FONT_SIZE_XS * 0.35},
		sc_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)
	cursor_y += 16

	// Row: Connection
	conn_disp := current_conn_status_display(state)
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"Status", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {text_x + 60, cursor_y + ui.FONT_SIZE_XS * 0.35},
		conn_disp.label, conn_disp.dot_color, ui.FONT_SIZE_XS, .Mono)
	cursor_y += 16

	// Row: Panes
	ws := workspace_registry_active(&state.ws_registry)
	pane_count := 0
	if ws != nil {
		_, pc := tree_collect_pane_ids(&ws.tree)
		pane_count = pc
	}
	pane_buf: [8]u8
	pane_str := fmt.bprintf(pane_buf[:], "%d", pane_count)
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"Panes", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {text_x + 60, cursor_y + ui.FONT_SIZE_XS * 0.35},
		pane_str, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)

	// S119: Row: Role
	cursor_y += 16
	role := resolve_focused_pane_role(state)
	role_str := role_badge_label(role)
	role_color := role == .Primary_Chart ? ui.COL_BLUE : ui.COL_TEXT_MUTED
	ui.push_text(&state.cmd_buf, {text_x, cursor_y + ui.FONT_SIZE_XS * 0.35},
		"Role", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	ui.push_text(&state.cmd_buf, {text_x + 60, cursor_y + ui.FONT_SIZE_XS * 0.35},
		role_str, role_color, ui.FONT_SIZE_XS, .Mono)
}

// Update context stack resize (drag left edge).
@(private = "package")
update_context_stack_resize :: proc(state: ^App_State, stack_rect: ui.Rect, pointer: ui.Pointer_Input) {
	cs := &state.chrome.context_stack
	handle_w := f32(4)
	handle_rect := ui.Rect{
		pos  = {stack_rect.pos.x - handle_w * 0.5, stack_rect.pos.y},
		size = {handle_w, stack_rect.size.y},
	}

	if cs.resizing {
		if pointer.left_down {
			// Calculate new width from mouse position.
			new_right := ui.rect_right(stack_rect)
			new_w := new_right - pointer.pos.x
			cs.width = clamp(new_w, CONTEXT_STACK_W_MIN, CONTEXT_STACK_W_MAX)
		} else {
			cs.resizing = false
		}
	} else {
		if ui.rect_contains(handle_rect, pointer.pos) {
			// Visual indicator.
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect  = ui.rect_xywh(stack_rect.pos.x - 1, stack_rect.pos.y, 2, stack_rect.size.y),
				color = ui.with_alpha(ui.COL_BLUE, 0.25),
			})
			if pointer.left_pressed {
				cs.resizing = true
			}
		}
	}
}
