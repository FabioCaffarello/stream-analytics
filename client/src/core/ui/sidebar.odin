package ui

// Two-zone sidebar: nav rail (always visible, ~44px) + collapsible detail panel (~200px).

NAV_RAIL_W          :: f32(44)
DETAIL_PANEL_W      :: f32(200)
SIDEBAR_EXPANDED_W  :: f32(140)  // width for expanded detail panel content
SIDEBAR_COLLAPSED_W :: f32(0)

// --- Nav Rail ---

NAV_RAIL_MAX_ITEMS :: 8

Nav_Rail_Item :: struct {
	icon:  string,   // single char (e.g. "D" for Dashboard)
	label: string,   // tooltip / accessible label
}

Nav_Rail_Result :: struct {
	clicked_route_idx: int, // -1 if none clicked
}

draw_nav_rail :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	items: []Nav_Rail_Item,
	active_route_idx: int,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
) -> Nav_Rail_Result {
	result := Nav_Rail_Result{clicked_route_idx = -1}
	if rect.size.x <= 0 || rect.size.y <= 0 do return result
	if len(items) == 0 do return result

	// Background.
	push(buf, Cmd_Rect_Filled{rect = rect, color = COL_SURFACE_1})

	// Right border.
	push(buf, Cmd_Line{
		from      = {rect_right(rect) - 1, rect.pos.y},
		to        = {rect_right(rect) - 1, rect_bottom(rect)},
		color     = COL_DIVIDER,
		thickness = 1,
	})

	btn_size := f32(32)
	btn_gap := f32(4)
	start_y := rect.pos.y + 8
	btn_x := rect.pos.x + (rect.size.x - btn_size) * 0.5

	count := min(len(items), NAV_RAIL_MAX_ITEMS)
	for i in 0 ..< count {
		btn_y := start_y + f32(i) * (btn_size + btn_gap)
		btn_rect := Rect{pos = {btn_x, btn_y}, size = {btn_size, btn_size}}
		hovered := rect_contains(btn_rect, pointer.pos)
		pressed := hovered && pointer.left_down
		is_active := i == active_route_idx

		// Background color.
		bg := with_alpha(COL_PRIMARY_DIMMED, 0.6)
		if is_active {
			bg = with_alpha(COL_BLUE, 0.35)
		}
		if pressed {
			bg = adjust_brightness(bg, 0.82)
		} else if hovered {
			bg = COL_SURFACE_3
		}

		push(buf, Cmd_Rect_Filled{rect = btn_rect, color = bg})

		// Active indicator: 4px wide, full button height.
		if is_active {
			indicator := Rect{
				pos  = {rect.pos.x, btn_y},
				size = {4, btn_size},
			}
			push(buf, Cmd_Rect_Filled{rect = indicator, color = COL_BLUE})
		}

		// Border.
		border_alpha := f32(0.12)
		if is_active do border_alpha = 0.25
		if hovered do border_alpha = 0.20
		push(buf, Cmd_Line{from = {btn_rect.pos.x, btn_rect.pos.y}, to = {rect_right(btn_rect), btn_rect.pos.y}, color = with_alpha(COL_WHITE, border_alpha), thickness = 1})
		push(buf, Cmd_Line{from = {rect_right(btn_rect), btn_rect.pos.y}, to = {rect_right(btn_rect), rect_bottom(btn_rect)}, color = with_alpha(COL_WHITE, border_alpha), thickness = 1})
		push(buf, Cmd_Line{from = {rect_right(btn_rect), rect_bottom(btn_rect)}, to = {btn_rect.pos.x, rect_bottom(btn_rect)}, color = with_alpha(COL_WHITE, border_alpha), thickness = 1})
		push(buf, Cmd_Line{from = {btn_rect.pos.x, rect_bottom(btn_rect)}, to = {btn_rect.pos.x, btn_rect.pos.y}, color = with_alpha(COL_WHITE, border_alpha), thickness = 1})

		// Icon text.
		fg := is_active ? COL_TEXT_PRIMARY : COL_TEXT_SECONDARY
		icon_size := measure_proc(FONT_SIZE_SM, items[i].icon)
		icon_pos := align_in_rect(btn_rect, icon_size, .Center, .Middle)
		icon_pos.y += FONT_SIZE_SM * 0.35
		push_text(buf, icon_pos, items[i].icon, fg, FONT_SIZE_SM, .Bold)

		// Click.
		if hovered && pointer.left_pressed {
			result.clicked_route_idx = i
		}

		// Hover tooltip: small label right of button.
		if hovered && len(items[i].label) > 0 {
			tip_x := rect_right(rect) + 4
			tip_y := btn_y + btn_size * 0.5
			tip_size := measure_proc(FONT_SIZE_XS, items[i].label)
			tip_bg := Rect{
				pos  = {tip_x - 2, tip_y - tip_size.y * 0.5 - 2},
				size = {tip_size.x + 8, tip_size.y + 4},
			}
			push(buf, Cmd_Rect_Filled{rect = tip_bg, color = with_alpha(COL_SURFACE_2, 0.95)})
			draw_rect_stroke(buf, tip_bg, COL_BORDER_STRONG)
			push_text(buf, {tip_x + 2, tip_y + FONT_SIZE_XS * 0.35}, items[i].label,
				COL_TEXT_PRIMARY, FONT_SIZE_XS, .Mono)
		}
	}

	return result
}

// --- Sidebar Layout ---

Sidebar_Layout :: struct {
	nav_rail_rect:  Rect,
	detail_rect:    Rect,     // zero-size if collapsed
	workspace_rect: Rect,     // remaining area after sidebar
}

DETAIL_PANEL_W_MIN :: f32(120)
DETAIL_PANEL_W_MAX :: f32(600)
RESIZE_HANDLE_W    :: f32(4)

compute_sidebar_layout :: proc(workspace: Rect, detail_expanded: bool, mobile: bool, detail_w: f32 = DETAIL_PANEL_W) -> Sidebar_Layout {
	layout: Sidebar_Layout

	if mobile {
		// On mobile, no sidebar at all.
		layout.workspace_rect = workspace
		return layout
	}

	remaining := workspace

	// Nav rail always visible on desktop.
	layout.nav_rail_rect = rect_cut_left(&remaining, NAV_RAIL_W)

	// Detail panel (collapsible) with configurable width.
	if detail_expanded {
		eff_w := clamp(detail_w, DETAIL_PANEL_W_MIN, DETAIL_PANEL_W_MAX)
		layout.detail_rect = rect_cut_left(&remaining, eff_w)
	}

	layout.workspace_rect = remaining
	return layout
}

// --- Detail Panel Frame ---

Detail_Panel_Result :: struct {
	content_rect: Rect,
}

draw_detail_panel_frame :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
) -> Detail_Panel_Result {
	result: Detail_Panel_Result
	if rect.size.x <= 0 || rect.size.y <= 0 do return result

	// Background.
	push(buf, Cmd_Rect_Filled{rect = rect, color = COL_SURFACE_1})

	// Right border.
	push(buf, Cmd_Line{
		from      = {rect_right(rect) - 1, rect.pos.y},
		to        = {rect_right(rect) - 1, rect_bottom(rect)},
		color     = COL_DIVIDER,
		thickness = 1,
	})

	// Content area with padding.
	result.content_rect = rect_pad(rect, 6)
	return result
}

// --- Legacy Sidebar (used inside Dashboard detail panel) ---

Sidebar_Item :: struct {
	label:     string,
	icon:      string,   // single char icon (e.g. "C" for candles)
	visible:   bool,
	panel_idx: int,
}

Sidebar_State :: struct {
	expanded:    bool,
	items:       [GRID_MAX_CELLS]Sidebar_Item,
	count:       int,
	hovered_idx: int,
}

Sidebar_Result :: struct {
	toggled_panel: int,   // -1 if none toggled
}

// Draw the sidebar panel toggle list. Used inside the detail panel for Dashboard route.
draw_sidebar :: proc(
	buf: ^Command_Buffer,
	rect: Rect,
	state: ^Sidebar_State,
	pointer: Pointer_Input,
	measure_proc: proc(size: f32, text: string) -> Vec2,
) -> Sidebar_Result {
	result := Sidebar_Result{toggled_panel = -1}
	if rect.size.x <= 0 || rect.size.y <= 0 do return result

	// Section header with divider line.
	push_text(buf, {rect.pos.x + 2, rect.pos.y + 14}, "PANELS",
		COL_TEXT_MUTED, FONT_SIZE_XS, .Bold)
	hdr_sep_y := rect.pos.y + 20
	push(buf, Cmd_Line{
		from      = {rect.pos.x, hdr_sep_y},
		to        = {rect.pos.x + rect.size.x, hdr_sep_y},
		color     = COL_DIVIDER,
		thickness = 1,
	})

	// Items (28px height for easier touch/click targets).
	item_h := f32(28)
	item_y := rect.pos.y + 26
	state.hovered_idx = -1

	for i in 0 ..< state.count {
		item := &state.items[i]
		item_rect := Rect{
			pos  = {rect.pos.x, item_y},
			size = {rect.size.x, item_h},
		}

		hovered := rect_contains(item_rect, pointer.pos)
		if hovered {
			state.hovered_idx = i
		}

		// Hover highlight using elevated surface color.
		if hovered {
			push(buf, Cmd_Rect_Filled{rect = item_rect, color = COL_SURFACE_3})
		}

		// Visibility indicator.
		dot_sz := f32(6)
		dot_x := item_rect.pos.x + 8
		dot_y := item_rect.pos.y + (item_h - dot_sz) * 0.5
		dot_color := item.visible ? COL_GREEN : with_alpha(COL_WHITE, 0.2)
		push(buf, Cmd_Rect_Filled{
			rect = {pos = {dot_x, dot_y}, size = {dot_sz, dot_sz}},
			color = dot_color,
		})

		// Label.
		label_color := item.visible ? COL_TEXT_PRIMARY : COL_TEXT_MUTED
		label_y := item_rect.pos.y + item_h * 0.5 + FONT_SIZE_XS * 0.35
		push_text(buf, {dot_x + dot_sz + 6, label_y}, item.label,
			label_color, FONT_SIZE_XS, .Mono)

		// Click handling.
		if hovered && pointer.left_pressed {
			result.toggled_panel = item.panel_idx
		}

		item_y += item_h
	}

	return result
}

// Initialize sidebar items for the standard 7-panel layout.
init_sidebar :: proc(state: ^Sidebar_State, visible: ^[PANEL_COUNT]bool) {
	LABELS :: [PANEL_COUNT]string{"Candles", "Stats", "Counter", "Heatmap", "VPVR", "Trades", "Orderbook"}
	ICONS  :: [PANEL_COUNT]string{"C", "S", "T", "H", "V", "R", "O"}
	state.count = PANEL_COUNT
	for i in 0 ..< PANEL_COUNT {
		labels := LABELS
		icons := ICONS
		state.items[i] = Sidebar_Item{
			label     = labels[i],
			icon      = icons[i],
			visible   = visible[i],
			panel_idx = i,
		}
	}
	state.hovered_idx = -1
}

// Sync sidebar item visibility from panel_visible array.
sync_sidebar_visibility :: proc(state: ^Sidebar_State, visible: [PANEL_COUNT]bool) {
	for i in 0 ..< state.count {
		if state.items[i].panel_idx >= 0 && state.items[i].panel_idx < PANEL_COUNT {
			state.items[i].visible = visible[state.items[i].panel_idx]
		}
	}
}
