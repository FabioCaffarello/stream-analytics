package app

import "core:fmt"
import "core:strings"
import "mr:services"
import "mr:ui"

draw_help_overlay :: proc(state: ^App_State, viewport_w, viewport_h: f32) {
	prev_z := state.cmd_buf.current_z_layer
	state.cmd_buf.current_z_layer = ui.Z_MODAL
	defer { state.cmd_buf.current_z_layer = prev_z }
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
		{"Ctrl+K", "Connection manager"},
		{"Ctrl+H", "Telemetry HUD"},
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

draw_exchange_manager :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	// BUG-13: Click-outside check at START to consume the click event and avoid triggering underlying UI.
	panel_w := f32(460)
	panel_h := f32(420)
	if panel_w > viewport_w - 20 do panel_w = viewport_w - 20
	if panel_h > viewport_h - 20 do panel_h = viewport_h - 20
	px := (viewport_w - panel_w) * 0.5
	py := (viewport_h - panel_h) * 0.5
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}
	// S63: Direct mutation for immediate close feedback (was queued action).
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		state.overlays.show_exchange_manager = false
		return
	}

	prev_z := state.cmd_buf.current_z_layer
	state.cmd_buf.current_z_layer = ui.Z_MODAL
	defer { state.cmd_buf.current_z_layer = prev_z }
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, 0.75},
	})

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})
	ui.draw_rect_stroke(&state.cmd_buf, panel_rect, ui.COL_BORDER_STRONG)

	y := py + 18
	ui.push_text(&state.cmd_buf, {px + 16, y}, "Connection Manager", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)

	conn_disp := current_conn_status_display(state)
	badge_w := ui.status_badge_width(conn_disp.label, state.text.measure, ui.FONT_SIZE_XS)
	ui.status_badge(&state.cmd_buf, ui.rect_xywh(px + panel_w - badge_w - 14, y - 6, badge_w, 18),
		conn_disp.label, conn_disp.dot_color, conn_disp.text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 26

	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {px + 12, y}, to = {px + panel_w - 12, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 10

	ui.push_text(&state.cmd_buf, {px + 16, y}, "Profiles", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 16

	list_h := panel_h - 130
	item_h := f32(24)
	selected_idx := clamp(state.connection_manager_selected_profile, 0, max(state.profiles.count - 1, 0))

	for pi in 0 ..< state.profiles.count {
		if y + item_h > py + 20 + list_h do break
		p := &state.profiles.profiles[pi]
		name := services.profile_name(p)
		vs_buf: [96]u8
		vs := fmt.bprintf(vs_buf[:], "%s:%s", services.profile_venue(p), services.profile_symbol(p))
		item_rect := ui.rect_xywh(px + 12, y, panel_w - 24, item_h)
		hovered := ui.rect_contains(item_rect, pointer.pos)
		selected := pi == selected_idx
		if selected {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_BLUE, 0.18)})
		} else if hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
		}
		if hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{kind = .Select_Profile, profile_idx = pi})
		}
		ui.push_text(&state.cmd_buf, {item_rect.pos.x + 6, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			name, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		ui.push_text(&state.cmd_buf, {item_rect.pos.x + 110, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			vs, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		y += item_h
	}
	if state.profiles.count <= 0 {
		ui.push_text(&state.cmd_buf, {px + 16, y + 10}, "No profiles saved", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	footer_y := py + panel_h - 56
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {px + 12, footer_y}, to = {px + panel_w - 12, footer_y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	footer_y += 10

	selected_market_idx := -1
	if selected_idx >= 0 && selected_idx < state.profiles.count {
		p := &state.profiles.profiles[selected_idx]
		profile_venue := normalized_venue(services.profile_venue(p))
		profile_symbol := services.profile_symbol(p)
		profile_market_type := services.profile_market_type(p)
		symbol_base := profile_symbol
		if sep := strings.last_index(profile_symbol, ":"); sep > 0 {
			symbol_base = profile_symbol[:sep]
			if len(profile_market_type) == 0 && sep < len(profile_symbol) - 1 {
				profile_market_type = profile_symbol[sep + 1:]
			}
		}
		for mi in 0 ..< state.stores.markets.count {
			entry := state.stores.markets.entries[mi]
			if normalized_venue(entry.venue) != profile_venue do continue
			if entry.ticker != symbol_base do continue
			if len(profile_market_type) > 0 && len(entry.market_type) > 0 && entry.market_type != profile_market_type do continue
			selected_market_idx = mi
			break
		}
	}

	btn_h := f32(20)
	btn_x := px + 12
	add_btn := ui.button(&state.cmd_buf, ui.rect_xywh(btn_x, footer_y, 52, btn_h),
		"Add", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if add_btn.clicked {
		queue_ui_action(state, UI_Action{kind = .Add_Profile})
	}
	btn_x += 56
	apply_btn := ui.button(&state.cmd_buf, ui.rect_xywh(btn_x, footer_y, 52, btn_h),
		"Apply", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if apply_btn.clicked {
		queue_ui_action(state, UI_Action{kind = .Apply_Profile, profile_idx = selected_idx})
	}
	btn_x += 56
	connect_btn := ui.button(&state.cmd_buf, ui.rect_xywh(btn_x, footer_y, 62, btn_h),
		"Connect", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if connect_btn.clicked {
		queue_ui_action(state, UI_Action{kind = .Connect_Profile, profile_idx = selected_idx})
	}
	btn_x += 66
	add_stream_btn := ui.button(&state.cmd_buf, ui.rect_xywh(btn_x, footer_y, 76, btn_h),
		"+ Stream", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if add_stream_btn.clicked {
		if selected_market_idx >= 0 {
			queue_ui_action(state, UI_Action{kind = .Subscribe_Market, market_entry_idx = selected_market_idx})
		} else {
			show_toast(state, "Profile market unavailable")
		}
	}
	btn_x += 80
	disconnect_btn := ui.button(&state.cmd_buf, ui.rect_xywh(btn_x, footer_y, 66, btn_h),
		"Disconnect", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if disconnect_btn.clicked {
		queue_ui_action(state, UI_Action{kind = .Disconnect_Profile})
	}
	btn_x += 70
	del_btn := ui.button(&state.cmd_buf, ui.rect_xywh(btn_x, footer_y, 52, btn_h),
		"Delete", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if del_btn.clicked {
		queue_ui_action(state, UI_Action{kind = .Remove_Profile, profile_idx = selected_idx})
	}

	close_btn := ui.button(&state.cmd_buf, ui.rect_xywh(px + panel_w - 58, py + 8, 44, 18),
		"Close", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
	if close_btn.clicked {
		queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
	}

}

draw_stream_picker :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	prev_z := state.cmd_buf.current_z_layer
	state.cmd_buf.current_z_layer = ui.Z_MODAL
	defer { state.cmd_buf.current_z_layer = prev_z }
	// Semi-transparent backdrop.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, 0.75},
	})

	reg := state.stream_views

	// Deduplicate connected slots by venue+symbol: keep only the first slot per market.
	deduped_slot_indices: [STREAM_VIEW_CAP]int
	deduped_count := 0
	if reg != nil {
		Seen_Market :: struct { venue: string, symbol: string }
		seen: [STREAM_VIEW_CAP]Seen_Market
		seen_count := 0
		for i in 0 ..< STREAM_VIEW_CAP {
			if !reg.slots[i].used do continue
			slot := &reg.slots[i]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue
			// Check if this venue+symbol pair was already seen.
			already := false
			for si in 0 ..< seen_count {
				if seen[si].venue == slot.stream_info.venue &&
					seen[si].symbol == slot.stream_info.symbol {
					already = true
					break
				}
			}
			if already do continue
			if seen_count < STREAM_VIEW_CAP {
				seen[seen_count] = {venue = slot.stream_info.venue, symbol = slot.stream_info.symbol}
				seen_count += 1
			}
			deduped_slot_indices[deduped_count] = i
			deduped_count += 1
		}
	}
	stream_count := deduped_count

	// Count available (not yet connected) markets from discovery store.
	available_count := 0
	available_indices: [services.MARKET_CAP]int
	for mi in 0 ..< state.stores.markets.count {
		entry := state.stores.markets.entries[mi]
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

	// S63: Click-outside closes immediately with direct mutation + early return (was queue without return).
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		state.overlays.show_stream_picker = false
		return
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

		for di in 0 ..< deduped_count {
			if y + item_h > py + panel_h - 20 do break
			i := deduped_slot_indices[di]
			slot := &reg.slots[i]

			// A market row is "active" if any slot for this venue+symbol is the active subject.
			is_active := false
			if reg.has_active && slot.has_stream_info {
				for si in 0 ..< STREAM_VIEW_CAP {
					if !reg.slots[si].used do continue
					if reg.slots[si].subject_id != reg.active_subject_id do continue
					s := &reg.slots[si]
					if !s.has_stream_info { refresh_stream_info_for_slot(state, s) }
					if s.has_stream_info &&
						s.stream_info.venue == slot.stream_info.venue &&
						s.stream_info.symbol == slot.stream_info.symbol {
						is_active = true
						break
					}
				}
			}

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
			entry := state.stores.markets.entries[mi]

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
	prev_z := state.cmd_buf.current_z_layer
	state.cmd_buf.current_z_layer = ui.Z_OVERLAY
	defer { state.cmd_buf.current_z_layer = prev_z }
	item_h := f32(22)
	panel_w := f32(200)
	// Items: "Follow Active" + all markets from discovery store.
	market_count := state.stores.markets.count
	panel_h := f32(market_count + 1) * item_h + 8
	if panel_h > 300 do panel_h = 300

	// Position below the badge anchor, clamped to viewport.
	px := clamp(anchor.x, 0, viewport_w - panel_w)
	py := clamp(anchor.y, 0, viewport_h - panel_h)
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}

	// Click-outside to close.
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		state.overlays.cell_stream_picker_open = -1
		return
	}

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})
	ui.draw_rect_stroke(&state.cmd_buf, panel_rect, ui.COL_BORDER_STRONG)

	y := py + 4
	bind := &state.world.bindings[cell_idx] if cell_idx >= 0 && cell_idx < state.world.count else nil
	has_binding := binding_has(bind)

	// "Follow Active" option.
	fa_active := bind != nil && !has_binding && bind.stream_idx < 0
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
		state.overlays.cell_stream_picker_open = -1
	}
	y += item_h

	// All available markets from discovery store.
	bound_venue := binding_venue(bind) if bind != nil else ""
	bound_symbol := binding_symbol(bind) if bind != nil else ""
	for mi in 0 ..< market_count {
		if y + item_h > py + panel_h do break
		entry := state.stores.markets.entries[mi]
		entry_venue := normalized_venue(entry.venue)
		entry_symbol := entry.ticker
		entry_symbol_buf: [80]u8
		if len(entry.market_type) > 0 && !strings.contains(entry_symbol, ":") {
			entry_symbol = fmt.bprintf(entry_symbol_buf[:], "%s:%s", entry_symbol, entry.market_type)
		}

		is_selected := has_binding && bound_venue == entry_venue && bound_symbol == entry_symbol
		is_sub := markets_is_subscribed(state, entry_venue, entry_symbol)
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
		label := fmt.bprintf(sl_buf[:], "%s:%s", entry_venue, entry_symbol)
		ui.push_text(&state.cmd_buf, {item_rect.pos.x + 10, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			label, is_selected ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

		if hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{
				kind = .Set_Cell_Stream, cell_idx = cell_idx,
				bind_venue = entry_venue, bind_symbol = entry_symbol,
			})
			state.overlays.cell_stream_picker_open = -1
		}
		y += item_h
	}
}

// ═══════════════════════════════════════════════════════════════
// Widget Catalog — two-step modal: choose widget type → stream.
// ═══════════════════════════════════════════════════════════════

draw_widget_catalog :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	// BUG-14: Click-outside check at START to consume the click.
	panel_w := f32(260)
	panel_h := f32(320)
	if panel_w > viewport_w - 20 do panel_w = viewport_w - 20
	if panel_h > viewport_h - 20 do panel_h = viewport_h - 20
	px := (viewport_w - panel_w) * 0.5
	py := (viewport_h - panel_h) * 0.5
	panel_rect := ui.Rect{pos = {px, py}, size = {panel_w, panel_h}}
	if pointer.left_pressed && !ui.rect_contains(panel_rect, pointer.pos) {
		state.overlays.show_widget_catalog = false
		return
	}

	prev_z := state.cmd_buf.current_z_layer
	state.cmd_buf.current_z_layer = ui.Z_MODAL
	defer { state.cmd_buf.current_z_layer = prev_z }
	// Backdrop.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
		rect  = {pos = {0, 0}, size = {viewport_w, viewport_h}},
		color = {0, 0, 0, 0.6},
	})

	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = ui.COL_SURFACE_1})

	y := py + 20

	if state.overlays.catalog_step == 0 {
		// --- Step 1: Widget type grid (S55: grouped by category) ---
		ui.push_text(&state.cmd_buf, {px + 16, y}, "Add Widget",
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_MD, .Bold)
		y += 28

		Widget_Entry :: struct { kind: Widget_Kind, label: string, analytics_kind: services.Analytics_Kind }

		cols :: 4
		cell_w := (panel_w - 32) / f32(cols)
		cell_h := f32(36)
		gx := px + 16

		// Helper: render a group of widget entries as a cols-wide grid.
		catalog_render_group :: proc(
			state: ^App_State,
			entries: []Widget_Entry,
			gx, cell_w, cell_h: f32,
			y: ^f32,
			pointer: ui.Pointer_Input,
		) {
			cols :: 4
			for ei in 0 ..< len(entries) {
				col := ei % cols
				row := ei / cols
				cx := gx + f32(col) * cell_w
				cy := y^ + f32(row) * cell_h
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
					state.overlays.catalog_selected = entries[ei].kind
					state.overlays.catalog_analytics_kind = entries[ei].analytics_kind
					state.overlays.catalog_step = 1
				}
			}
			row_count := (len(entries) + cols - 1) / cols
			y^ += f32(row_count) * cell_h + 4
		}

		// --- CHART ---
		ui.push_text(&state.cmd_buf, {gx, y}, "CHART",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		y += 16
		chart_entries := [8]Widget_Entry{
			{.Candle,    "Candle", {}},
			{.Orderbook, "Orderbook", {}},
			{.Trades,    "Trades", {}},
			{.DOM,       "DOM", {}},
			{.Stats,     "Stats", {}},
			{.Counter,   "Counter", {}},
			{.Heatmap,   "Heatmap", {}},
			{.VPVR,      "VPVR", {}},
		}
		catalog_render_group(state, chart_entries[:], gx, cell_w, cell_h, &y, pointer)

		// --- ANALYTICS ---
		ui.push_text(&state.cmd_buf, {gx, y}, "ANALYTICS",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		y += 16
		analytics_entries := [4]Widget_Entry{
			{.Analytics, "Open Interest", .Open_Interest},
			{.Analytics, "Delta Volume",  .Delta_Volume},
			{.Analytics, "CVD",           .CVD},
			{.Analytics, "Bar Stats",     .Bar_Stats},
		}
		catalog_render_group(state, analytics_entries[:], gx, cell_w, cell_h, &y, pointer)

		// --- PROFILES ---
		ui.push_text(&state.cmd_buf, {gx, y}, "PROFILES",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		y += 16
		profile_entries := [2]Widget_Entry{
			{.Session_VPVR, "Session VPVR", {}},
			{.TPO,          "TPO Profile",  {}},
		}
		catalog_render_group(state, profile_entries[:], gx, cell_w, cell_h, &y, pointer)

		y += 8

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
			state.overlays.catalog_step = 0
			state.overlays.catalog_selected = .Empty
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
				widget_kind = state.overlays.catalog_selected,
				analytics_kind = state.overlays.catalog_analytics_kind,
				stream_idx = -1,
			})
			state.overlays.show_widget_catalog = false
		}
		y += item_h

		// All available markets from discovery store.
		for mi in 0 ..< state.stores.markets.count {
			if y + item_h > py + panel_h - 40 do break
			entry := state.stores.markets.entries[mi]
			entry_venue := normalized_venue(entry.venue)
			entry_symbol := entry.ticker
			entry_symbol_buf: [80]u8
			if len(entry.market_type) > 0 && !strings.contains(entry_symbol, ":") {
				entry_symbol = fmt.bprintf(entry_symbol_buf[:], "%s:%s", entry_symbol, entry.market_type)
			}
			is_sub := markets_is_subscribed(state, entry_venue, entry_symbol)

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
			label := fmt.bprintf(sl_buf[:], "%s:%s", entry_venue, entry_symbol)
			ui.push_text(&state.cmd_buf, {sr.pos.x + 10, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				label, sr_hov ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			if sr_hov && pointer.left_pressed {
				queue_ui_action(state, UI_Action{
					kind = .Add_Cell,
					widget_kind = state.overlays.catalog_selected,
					analytics_kind = state.overlays.catalog_analytics_kind,
					bind_venue = entry_venue,
					bind_symbol = entry_symbol,
				})
				state.overlays.show_widget_catalog = false
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
		state.overlays.show_widget_catalog = false
	}

}
