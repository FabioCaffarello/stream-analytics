package app

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

draw_help_overlay :: proc(state: ^App_State, viewport_w, viewport_h: f32) {
	// Semi-transparent backdrop.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, 0.75},
	})

	// Centered panel.
	panel_w := f32(280)
	panel_h := f32(500)
	if panel_w > viewport_w - 20 do panel_w = viewport_w - 20
	if panel_h > viewport_h - 20 do panel_h = viewport_h - 20
	px := (viewport_w - panel_w) * 0.5
	py := (viewport_h - panel_h) * 0.5
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})

	// Title.
	y := py + 24
	ui.push_text(&state.cmd_buf, {px + 16, y}, "Keyboard Shortcuts",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)
	y += 28

	// Shortcut entries.
	Help_Entry :: struct { key, desc: string }
	entries := [?]Help_Entry{
		{"Tab / Shift+Tab", "Cycle stream"},
		{"1-9", "Timeframe"},
		{"S", "Toggle detail panel"},
		{"C", "Compare mode"},
		{"F", "Focus mode (scalper)"},
		{"G", "Stream picker"},
		{"M", "Toggle MA overlay"},
		{"B", "Toggle Bollinger Bands"},
		{"V", "Toggle VWAP"},
		{"R", "Toggle RSI"},
		{"I", "Toggle MACD"},
		{"H", "Toggle Funding Rate"},
		{"J", "Toggle Liquidations"},
		{"K", "Toggle Trade Counter"},
		{"Del / Bksp", "Delete draw tool"},
		{"Dbl-click", "Add price line"},
		{"Shift+Drag", "Rectangle zone"},
		{"Escape", "Exit mode/overlay"},
		{"?", "This help"},
		{"Scroll", "Zoom chart"},
		{"Right-click", "Cell context menu"},
	}
	for e in entries {
		ui.push_text(&state.cmd_buf, {px + 16, y}, e.key,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_SM, .Mono)
		ui.push_text(&state.cmd_buf, {px + 140, y}, e.desc,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_SM, .Mono)
		y += 20
	}

	// Close hint.
	hint := "Press ? to close"
	hint_w := state.text.measure(ui.FONT_SIZE_XS, hint).x
	ui.push_text(&state.cmd_buf, {px + (panel_w - hint_w) * 0.5, py + panel_h - 20},
		hint, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
}

// PRD-0009: Connection Status diagnostics view (replaces Exchange Manager subscribe/unsubscribe panel).
draw_exchange_manager :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	// Semi-transparent backdrop.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, 0.75},
	})

	// Panel dimensions.
	panel_w := f32(320)
	panel_h := f32(360)
	if panel_w > viewport_w - 20 do panel_w = viewport_w - 20
	if panel_h > viewport_h - 20 do panel_h = viewport_h - 20
	px := (viewport_w - panel_w) * 0.5
	py := (viewport_h - panel_h) * 0.5
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}

	// Click-outside to close.
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
	}

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})
	ui.draw_rect_stroke(&state.cmd_buf, panel_rect, ui.COL_BORDER_STRONG)

	// --- Header: Title + WS status badge ---
	y := py + 20
	ui.push_text(&state.cmd_buf, {px + 16, y}, "Connection Status",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)

	conn_status: ports.MD_Conn_Status = .Offline
	if state.marketdata.conn_status != nil {
		conn_status = state.marketdata.conn_status()
	}
	conn_label: string
	conn_dot_color: ui.Color
	conn_text_color: ui.Color
	switch conn_status {
	case .Connected:    conn_label = "LIVE";         conn_dot_color = ui.COL_GREEN;          conn_text_color = ui.COL_GREEN
	case .Connecting:   conn_label = "CONNECTING";   conn_dot_color = ui.COL_YELLOW_ACCENT;  conn_text_color = ui.COL_YELLOW_ACCENT
	case .Reconnecting: conn_label = "RECONNECTING"; conn_dot_color = ui.COL_YELLOW_ACCENT;  conn_text_color = ui.COL_YELLOW_ACCENT
	case .Offline:      conn_label = "OFFLINE";      conn_dot_color = ui.with_alpha(ui.COL_WHITE, 0.35); conn_text_color = ui.COL_TEXT_MUTED
	}

	badge_w := ui.status_badge_width(conn_label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := px + panel_w - badge_w - 16
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y - 6, badge_w, f32(18)),
		conn_label, conn_dot_color, conn_text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 28

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {px + 12, y}, to = {px + panel_w - 12, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 10

	reg := state.stream_views

	// --- Stream slot usage ---
	stream_count := 0
	if reg != nil { stream_count = reg.count }
	slots_buf: [32]u8
	slots_str := fmt.bprintf(slots_buf[:], "%d/%d stream slots", stream_count, STREAM_VIEW_CAP)
	ui.push_text(&state.cmd_buf, {px + 16, y}, slots_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// --- Active subscriptions per venue ---
	content_bottom := py + panel_h - 44
	item_h := f32(20)

	ui.push_text(&state.cmd_buf, {px + 16, y}, "Active Subscriptions",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	if reg != nil {
		for si in 0 ..< STREAM_VIEW_CAP {
			if y + item_h > content_bottom do break
			if !reg.slots[si].used do continue
			slot := &reg.slots[si]
			if !slot.has_stream_info { refresh_stream_info_for_slot(state, slot) }
			if !slot.has_stream_info do continue

			// Green dot.
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = {pos = {px + 20, y + item_h * 0.5 - 2}, size = {4, 4}},
				color = ui.COL_GREEN,
			})

			sl_buf: [48]u8
			label := fmt.bprintf(sl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
			ui.push_text(&state.cmd_buf, {px + 30, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				label, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)

			// Channel indicator.
			ch_label := "---"
			if slot.has_channel {
				switch slot.channel {
				case .Trades:    ch_label = "T"
				case .Orderbook: ch_label = "OB"
				case .Stats:     ch_label = "S"
				case .Heatmaps:  ch_label = "HM"
				case .VPVR:      ch_label = "VP"
				case .Candles:   ch_label = "C"
				}
			}
			ui.push_text(&state.cmd_buf, {px + panel_w - 40, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				ch_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			y += item_h
		}
	}

	if stream_count == 0 {
		ui.push_text(&state.cmd_buf, {px + 16, y}, "No active subscriptions",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += item_h
	}

	// --- Cell bindings summary ---
	y += 8
	ui.push_text(&state.cmd_buf, {px + 16, y}, "Cell Bindings",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 18

	for ci in 0 ..< state.cell_count {
		if y + item_h > content_bottom do break
		cell := &state.cell_assignments[ci]
		cb_buf: [48]u8
		label: string
		if cell_has_binding(cell) {
			label = fmt.bprintf(cb_buf[:], "[%d] %s/%s", ci, cell_bound_venue(cell), cell_bound_symbol(cell))
		} else {
			label = fmt.bprintf(cb_buf[:], "[%d] Follow Active", ci)
		}
		color := cell_has_binding(cell) ? ui.COL_TEXT_SECONDARY : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {px + 20, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			label, color, ui.FONT_SIZE_XS, .Mono)
		y += item_h
	}

	// --- Footer: Close ---
	footer_y := py + panel_h - 36
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {px + 12, footer_y}, to = {px + panel_w - 12, footer_y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	footer_y += 8

	close_w := f32(50)
	close_h := f32(20)
	close_x := px + panel_w - close_w - 16
	close_res := ui.button(&state.cmd_buf,
		ui.rect_xywh(close_x, footer_y + 2, close_w, close_h),
		"Close", pointer, state.text.measure, ui.FONT_SIZE_XS)
	if close_res.clicked {
		queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
	}
}

draw_stream_picker :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	// Semi-transparent backdrop.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, 0.75},
	})

	reg := state.stream_views
	stream_count := 0
	if reg != nil {
		for i in 0 ..< STREAM_VIEW_CAP {
			if reg.slots[i].used do stream_count += 1
		}
	}

	// Count available (not yet connected) markets from discovery store.
	available_count := 0
	available_indices: [services.MARKET_CAP]int
	for mi in 0 ..< state.markets_store.count {
		entry := state.markets_store.entries[mi]
		already_connected := false
		if reg != nil {
			for si in 0 ..< STREAM_VIEW_CAP {
				if !reg.slots[si].used do continue
				slot := &reg.slots[si]
				if !slot.has_stream_info {
					refresh_stream_info_for_slot(state, slot)
				}
				if slot.has_stream_info &&
					slot.stream_info.venue == entry.venue &&
					slot.stream_info.symbol == entry.ticker {
					already_connected = true
					break
				}
			}
		}
		if !already_connected {
			available_indices[available_count] = mi
			available_count += 1
		}
	}

	item_h := f32(24)
	panel_w := f32(280)
	connected_h := f32(max(stream_count, 1)) * item_h
	available_h := available_count > 0 ? f32(available_count) * item_h + 24 : f32(0)
	panel_h := f32(52) + connected_h + available_h + 28
	if panel_h > viewport_h - 40 do panel_h = viewport_h - 40
	if panel_w > viewport_w - 20 do panel_w = viewport_w - 20
	px := (viewport_w - panel_w) * 0.5
	py := (viewport_h - panel_h) * 0.5
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}

	// Click-outside to close.
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		queue_ui_action(state, UI_Action{kind = .Toggle_Stream_Picker})
	}

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})
	ui.draw_rect_stroke(&state.cmd_buf, panel_rect, ui.COL_BORDER_STRONG)

	// Title.
	y := py + 24
	ui.push_text(&state.cmd_buf, {px + 16, y}, "Switch Stream",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)
	y += 28

	// --- Section 1: CONNECTED streams ---
	if stream_count > 0 {
		ui.push_text(&state.cmd_buf, {px + 12, y + 10},
			"CONNECTED", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		y += 18

		for i in 0 ..< STREAM_VIEW_CAP {
			if y + item_h > py + panel_h - 20 do break
			if !reg.slots[i].used do continue
			slot := &reg.slots[i]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}

			is_active := reg.has_active && slot.subject_id == reg.active_subject_id
			item_rect := ui.Rect{pos = {px + 8, y}, size = {panel_w - 16, item_h}}

			hovered := ui.rect_contains(item_rect, pointer.pos)
			if is_active {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = item_rect, color = ui.with_alpha(ui.COL_BLUE, 0.2),
				})
			} else if hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = item_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05),
				})
			}

			if hovered && pointer.left_pressed {
				queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = slot.subject_id})
			}

			label := "---"
			sl_buf: [64]u8
			if slot.has_stream_info && len(slot.stream_info.venue) > 0 {
				label = fmt.bprintf(sl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)
			}

			text_color := is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY
			ui.push_text(&state.cmd_buf, {item_rect.pos.x + 6, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				label, text_color, ui.FONT_SIZE_XS, .Mono)

			// Active indicator dot.
			if is_active {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {item_rect.pos.x + item_rect.size.x - 12, y + item_h * 0.5 - 3}, size = {6, 6}},
					color = ui.COL_GREEN,
				})
			}

			y += item_h
		}
	} else {
		ui.push_text(&state.cmd_buf, {px + 16, y + 10},
			"No streams connected", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 20
	}

	// --- Section 2: AVAILABLE markets (from discovery) ---
	if available_count > 0 {
		y += 6
		// Divider line.
		ui.push(&state.cmd_buf, ui.Cmd_Line{
			from = {px + 12, y}, to = {px + panel_w - 12, y},
			color = ui.COL_DIVIDER, thickness = 1,
		})
		y += 6

		ui.push_text(&state.cmd_buf, {px + 12, y + 10},
			"AVAILABLE", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		y += 18

		for ai in 0 ..< available_count {
			if y + item_h > py + panel_h - 20 do break
			mi := available_indices[ai]
			entry := state.markets_store.entries[mi]

			item_rect := ui.Rect{pos = {px + 8, y}, size = {panel_w - 16, item_h}}
			hovered := ui.rect_contains(item_rect, pointer.pos)
			if hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = item_rect, color = ui.with_alpha(ui.COL_GREEN, 0.08),
				})
			}

			if hovered && pointer.left_pressed {
				queue_ui_action(state, UI_Action{kind = .Subscribe_Market, market_entry_idx = mi})
			}

			al_buf: [64]u8
			al_label := fmt.bprintf(al_buf[:], "+ %s:%s", entry.venue, entry.ticker)
			ui.push_text(&state.cmd_buf, {item_rect.pos.x + 6, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				al_label, ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)

			y += item_h
		}
	}

	// Close hint.
	hint := "G or Esc to close"
	hint_w := state.text.measure(ui.FONT_SIZE_XS, hint).x
	ui.push_text(&state.cmd_buf, {px + (panel_w - hint_w) * 0.5, py + panel_h - 16},
		hint, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
}

// Compact stream picker anchored to a cell's stream badge (PRD-0006-B M2, PRD-0009).
// Shows ALL available markets from discovery, not just connected streams.
draw_cell_stream_picker :: proc(state: ^App_State, anchor: ui.Vec2, cell_idx: int,
	viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	item_h := f32(22)
	panel_w := f32(200)
	// Items: "Follow Active" + all markets from discovery store.
	market_count := state.markets_store.count
	panel_h := f32(market_count + 1) * item_h + 8
	if panel_h > 300 do panel_h = 300

	// Position below the badge anchor, clamped to viewport.
	px := clamp(anchor.x, 0, viewport_w - panel_w)
	py := clamp(anchor.y, 0, viewport_h - panel_h)
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}

	// Click-outside to close.
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		state.cell_stream_picker_open = -1
		return
	}

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})
	ui.draw_rect_stroke(&state.cmd_buf, panel_rect, ui.COL_BORDER_STRONG)

	y := py + 4
	cell := &state.cell_assignments[cell_idx] if cell_idx >= 0 && cell_idx < state.cell_count else nil
	has_binding := cell != nil && cell_has_binding(cell)

	// "Follow Active" option.
	fa_active := cell != nil && !has_binding && cell.stream_idx < 0
	fa_rect := ui.Rect{pos = {px + 4, y}, size = {panel_w - 8, item_h}}
	fa_hovered := ui.rect_contains(fa_rect, pointer.pos)
	if fa_active {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = fa_rect, color = ui.with_alpha(ui.COL_BLUE, 0.15)})
	} else if fa_hovered {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = fa_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
	}
	ui.push_text(&state.cmd_buf, {fa_rect.pos.x + 6, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		"Follow Active", fa_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	if fa_hovered && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Set_Cell_Stream, cell_idx = cell_idx, stream_idx = -1})
		state.cell_stream_picker_open = -1
	}
	y += item_h

	// All available markets from discovery store.
	bound_venue := cell_bound_venue(cell) if cell != nil else ""
	bound_symbol := cell_bound_symbol(cell) if cell != nil else ""
	for mi in 0 ..< market_count {
		if y + item_h > py + panel_h do break
		entry := state.markets_store.entries[mi]

		is_selected := has_binding && bound_venue == entry.venue && bound_symbol == entry.ticker
		is_sub := markets_is_subscribed(state, entry.venue, entry.ticker)
		item_rect := ui.Rect{pos = {px + 4, y}, size = {panel_w - 8, item_h}}
		hovered := ui.rect_contains(item_rect, pointer.pos)
		if is_selected {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_BLUE, 0.15)})
		} else if hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
		}

		// Green dot for subscribed markets.
		if is_sub {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = {pos = {item_rect.pos.x + 2, y + item_h * 0.5 - 2}, size = {4, 4}},
				color = ui.COL_GREEN,
			})
		}

		sl_buf: [40]u8
		label := fmt.bprintf(sl_buf[:], "%s:%s", entry.venue, entry.ticker)
		ui.push_text(&state.cmd_buf, {item_rect.pos.x + 10, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			label, is_selected ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

		if hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{
				kind = .Set_Cell_Stream, cell_idx = cell_idx,
				bind_venue = entry.venue, bind_symbol = entry.ticker,
			})
			state.cell_stream_picker_open = -1
		}
		y += item_h
	}
}

// ═══════════════════════════════════════════════════════════════
// Widget Catalog — two-step modal: choose widget type → stream.
// ═══════════════════════════════════════════════════════════════

draw_widget_catalog :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	// Backdrop.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, 0.6},
	})

	panel_w := f32(260)
	panel_h := f32(320)
	if panel_w > viewport_w - 20 do panel_w = viewport_w - 20
	if panel_h > viewport_h - 20 do panel_h = viewport_h - 20
	px := (viewport_w - panel_w) * 0.5
	py := (viewport_h - panel_h) * 0.5
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})

	y := py + 20

	if state.catalog_step == 0 {
		// --- Step 1: Widget type grid ---
		ui.push_text(&state.cmd_buf, {px + 16, y}, "Add Widget",
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)
		y += 28

		ui.push_text(&state.cmd_buf, {px + 16, y}, "Choose widget type:",
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		y += 22

		Widget_Entry :: struct { kind: Widget_Kind, label: string }
		entries := [8]Widget_Entry{
			{.Candle,    "Candle"},
			{.Orderbook, "Orderbook"},
			{.Trades,    "Trades"},
			{.DOM,       "DOM"},
			{.Stats,     "Stats"},
			{.Counter,   "Counter"},
			{.Heatmap,   "Heatmap"},
			{.VPVR,      "VPVR"},
		}

		cols :: 3
		cell_w := (panel_w - 32) / f32(cols)
		cell_h := f32(36)
		gx := px + 16

		for ei in 0 ..< 8 {
			col := ei % cols
			row := ei / cols
			cx := gx + f32(col) * cell_w
			cy := y + f32(row) * cell_h
			btn_rect := ui.Rect{pos = {cx + 2, cy + 2}, size = {cell_w - 4, cell_h - 4}}
			hovered := ui.rect_contains(btn_rect, pointer.pos)

			bg := hovered ? ui.with_alpha(ui.COL_BLUE, 0.2) : ui.with_alpha(ui.COL_WHITE, 0.04)
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = btn_rect, color = bg})
			ui.push_text(&state.cmd_buf,
				{btn_rect.pos.x + 6, cy + cell_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				entries[ei].label,
				hovered ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY,
				ui.FONT_SIZE_XS, .Mono)

			if hovered && pointer.left_pressed {
				state.catalog_selected_widget = entries[ei].kind
				state.catalog_step = 1
			}
		}

		y += f32((8 + cols - 1) / cols) * cell_h + 12

	} else {
		// --- Step 2: Stream picker (PRD-0009: show ALL available markets) ---
		ui.push_text(&state.cmd_buf, {px + 16, y}, "Choose Stream",
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)
		y += 28

		// Back button.
		back_rect := ui.Rect{pos = {px + 16, y}, size = {50, 20}}
		back_hov := ui.rect_contains(back_rect, pointer.pos)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = back_rect,
			color = back_hov ? ui.with_alpha(ui.COL_WHITE, 0.08) : ui.with_alpha(ui.COL_WHITE, 0.03)})
		ui.push_text(&state.cmd_buf, {back_rect.pos.x + 4, y + 10 + ui.FONT_SIZE_XS * 0.35},
			"< Back", back_hov ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		if back_hov && pointer.left_pressed {
			state.catalog_step = 0
		}
		y += 28

		item_h := f32(24)

		// "Follow Active" option.
		fa_rect := ui.Rect{pos = {px + 8, y}, size = {panel_w - 16, item_h}}
		fa_hov := ui.rect_contains(fa_rect, pointer.pos)
		if fa_hov {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = fa_rect, color = ui.with_alpha(ui.COL_BLUE, 0.15)})
		}
		ui.push_text(&state.cmd_buf, {fa_rect.pos.x + 6, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			"Follow Active", fa_hov ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		if fa_hov && pointer.left_pressed {
			queue_ui_action(state, UI_Action{
				kind = .Add_Cell,
				widget_kind = state.catalog_selected_widget,
				stream_idx = -1,
			})
			state.show_widget_catalog = false
		}
		y += item_h

		// All available markets from discovery store.
		for mi in 0 ..< state.markets_store.count {
			if y + item_h > py + panel_h - 40 do break
			entry := state.markets_store.entries[mi]
			is_sub := markets_is_subscribed(state, entry.venue, entry.ticker)

			sr := ui.Rect{pos = {px + 8, y}, size = {panel_w - 16, item_h}}
			sr_hov := ui.rect_contains(sr, pointer.pos)
			if sr_hov {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = sr, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
			}
			if is_sub {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {sr.pos.x + 2, y + item_h * 0.5 - 2}, size = {4, 4}},
					color = ui.COL_GREEN,
				})
			}
			sl_buf: [40]u8
			label := fmt.bprintf(sl_buf[:], "%s:%s", entry.venue, entry.ticker)
			ui.push_text(&state.cmd_buf, {sr.pos.x + 10, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				label, sr_hov ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			if sr_hov && pointer.left_pressed {
				queue_ui_action(state, UI_Action{
					kind = .Add_Cell,
					widget_kind = state.catalog_selected_widget,
					bind_venue = entry.venue,
					bind_symbol = entry.ticker,
				})
				state.show_widget_catalog = false
			}
			y += item_h
		}
	}

	// Close button at bottom.
	close_y := py + panel_h - 32
	close_rect := ui.Rect{pos = {px + panel_w * 0.5 - 30, close_y}, size = {60, 22}}
	close_hov := ui.rect_contains(close_rect, pointer.pos)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = close_rect,
		color = close_hov ? ui.with_alpha(ui.COL_WHITE, 0.1) : ui.with_alpha(ui.COL_WHITE, 0.04)})
	ui.push_text(&state.cmd_buf, {close_rect.pos.x + 14, close_y + 11 + ui.FONT_SIZE_XS * 0.35},
		"Close", close_hov ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	if close_hov && pointer.left_pressed {
		state.show_widget_catalog = false
	}

	// Click outside panel closes.
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		state.show_widget_catalog = false
	}
}
