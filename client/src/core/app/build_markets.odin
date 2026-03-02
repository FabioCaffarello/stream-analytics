package app

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:ui"

// Venues detail panel -- collapsible venue sections with subscribe/unsubscribe.
@(private = "package")
draw_markets_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y
	bottom := rect.pos.y + rect.size.y

	// Header: "VENUES" + connection status badge.
	ui.push_text(&state.cmd_buf, {rect.pos.x + 2, y + 14}, "VENUES",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

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
	badge_x := rect.pos.x + rect.size.x - badge_w - 4
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y + 2, badge_w, f32(16)),
		conn_label, conn_dot_color, conn_text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 22

	// Stream count summary.
	reg := state.stream_views
	stream_count := 0
	if reg != nil { stream_count = reg.count }
	sc_buf: [24]u8
	sc_str := fmt.bprintf(sc_buf[:], "%d streams", stream_count)
	ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10},
		sc_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {rect.pos.x + 4, y}, to = {rect.pos.x + rect.size.x - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 4

	// Collapsible venue sections.
	vc := services.markets_venue_count(&state.stores.markets)
	item_h := f32(20)

	for vi in 0 ..< vc {
		if y + 20 > bottom do break
		venue := services.markets_venue_at(&state.stores.markets, vi)

		// Venue header (collapsible).
		sec := &state.overlays.exchange_sections[vi]
		hdr_rect := ui.rect_xywh(rect.pos.x + 2, y, rect.size.x - 4, f32(20))
		hdr_hovered := ui.rect_contains(hdr_rect, pointer.pos)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = hdr_rect,
			color = hdr_hovered ? ui.with_alpha(ui.COL_SURFACE_2, 0.9) : ui.with_alpha(ui.COL_SURFACE_2, 0.5),
		})
		arrow := sec.expanded ? "v " : "> "
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 13}, arrow,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 16, y + 13}, venue,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		if hdr_hovered && pointer.left_pressed {
			sec.expanded = !sec.expanded
		}
		y += 22

		if !sec.expanded do continue

		// Symbols under this venue.
		sc := services.markets_symbol_count(&state.stores.markets, venue)
		for si in 0 ..< sc {
			if y + item_h > bottom do break
			entry := services.markets_symbol_at(&state.stores.markets, venue, si)
			if entry == nil do continue

			is_sub := markets_is_subscribed(state, entry.venue, entry.ticker)

			sym_rect := ui.rect_xywh(rect.pos.x + 4, y, rect.size.x - 8, item_h)
			sym_hovered := ui.rect_contains(sym_rect, pointer.pos)
			if sym_hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = sym_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04),
				})
			}

			// Green dot if subscribed.
			if is_sub {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = {pos = {rect.pos.x + 8, y + item_h * 0.5 - 3}, size = {6, 6}},
					color = ui.COL_GREEN,
				})
			}

			ui.push_text(&state.cmd_buf,
				{rect.pos.x + 18, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				entry.ticker, is_sub ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY,
				ui.FONT_SIZE_XS, .Mono)

			// Subscribe/unsubscribe button.
			btn_label := is_sub ? "-" : "+"
			btn_w := f32(18)
			btn_x := rect.pos.x + rect.size.x - btn_w - 6
			btn_rect := ui.rect_xywh(btn_x, y + 2, btn_w, item_h - 4)
			btn_color := is_sub ? ui.with_alpha(ui.COL_RED, 0.15) : ui.with_alpha(ui.COL_GREEN, 0.15)
			btn_hovered := ui.rect_contains(btn_rect, pointer.pos)
			if btn_hovered {
				btn_color = is_sub ? ui.with_alpha(ui.COL_RED, 0.3) : ui.with_alpha(ui.COL_GREEN, 0.3)
			}
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = btn_rect, color = btn_color})
			ui.push_text(&state.cmd_buf,
				{btn_x + btn_w * 0.5 - 3, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				btn_label, is_sub ? ui.COL_RED : ui.COL_GREEN, ui.FONT_SIZE_XS, .Bold)

			if btn_hovered && pointer.left_pressed {
				for mi in 0 ..< state.stores.markets.count {
					me := state.stores.markets.entries[mi]
					if me.venue == entry.venue && me.ticker == entry.ticker {
						if is_sub {
							queue_ui_action(state, UI_Action{kind = .Unsubscribe_Market, market_entry_idx = mi})
						} else {
							queue_ui_action(state, UI_Action{kind = .Subscribe_Market, market_entry_idx = mi})
						}
						break
					}
				}
			}

			y += item_h
		}
		y += 2
	}

	// "Manage..." link at bottom -> opens full exchange manager overlay.
	if y + 22 < bottom {
		y += 2
		manage_rect := ui.rect_xywh(rect.pos.x + 4, y, rect.size.x - 8, f32(20))
		manage_hovered := ui.rect_contains(manage_rect, pointer.pos)
		if manage_hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = manage_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05),
			})
		}
		ui.push_text(&state.cmd_buf,
			{rect.pos.x + 8, y + 13},
			"Manage...", manage_hovered ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS, .Mono)
		if manage_hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{kind = .Toggle_Connection_Modal})
		}
	}
}
