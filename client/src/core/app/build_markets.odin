package app

import "core:fmt"
import "mr:md_common"
import "mr:services"
import "mr:ui"

// S54: Market_Row_View — per-market read model for the Markets page.
// Pure derived view assembled once per frame. No mutation, no allocations.
Market_Row_View :: struct {
	venue:         string,
	symbol:        string,
	market_type:   string,
	is_subscribed: bool,
	is_active:     bool,
	// Price data (from slot candle store).
	has_price:     bool,
	last_price:    f64,
	open_price:    f64,
	// Health (from slot apply_state).
	composition:   md_common.Composition_Stage,
	health_level:  md_common.System_Health_Level,
	has_live_data: bool,
	stale_count:   int,
	aging_count:   int,
	// Identity back-reference.
	market_idx:    int,  // index into Markets_Store.entries
	slot_idx:      int,  // stream view slot index, or -1
}

MARKET_ROW_CAP :: services.MARKET_CAP

// Resolve all market rows from current state. Returns count of rows populated.
resolve_market_rows :: proc(state: ^App_State, rows: ^[MARKET_ROW_CAP]Market_Row_View) -> int {
	if state == nil || rows == nil do return 0
	count := 0
	reg := state.stream_views
	now_ms := current_now_ms(state)
	global_tf_ms := global_tf_ms(state)

	for mi in 0 ..< state.stores.markets.count {
		if count >= MARKET_ROW_CAP do break
		entry := state.stores.markets.entries[mi]
		row := &rows[count]
		row^ = {}
		row.venue = entry.venue
		row.symbol = entry.ticker
		row.market_type = entry.market_type
		row.market_idx = mi
		row.slot_idx = -1

		// Find matching stream view slot.
		if reg != nil {
			want_venue := normalized_venue(entry.venue)
			want_symbol := normalized_symbol(entry.ticker)
			for si in 0 ..< STREAM_VIEW_CAP {
				if !reg.slots[si].used do continue
				slot := &reg.slots[si]
				if !slot.has_stream_info {
					refresh_stream_info_for_slot(state, slot)
				}
				if !slot.has_stream_info do continue
				if normalized_venue(slot.stream_info.venue) != want_venue do continue
				if normalized_symbol(slot.stream_info.symbol) != want_symbol do continue

				row.is_subscribed = true
				row.slot_idx = si

				// Active check.
				if reg.has_active && slot.subject_id == reg.active_subject_id {
					row.is_active = true
				}

				// Price from candle store.
				if slot.candle_store.count > 0 {
					c := services.get_candle_newest(&slot.candle_store, 0)
					row.has_price = true
					row.last_price = c.close
					row.open_price = c.open
				}

				// Health from apply_state.
				apply := slot.apply_state
				row.composition = md_common.apply_state_composition_stage(apply)
				row.health_level = md_common.stream_health_level(apply, now_ms, global_tf_ms)
				row.stale_count, row.aging_count = md_common.apply_state_stale_artifact_count(apply, now_ms, global_tf_ms)
				for kind in md_common.Artifact_Kind {
					if apply.has_live[kind] {
						row.has_live_data = true
						break
					}
				}
				break // first matching slot is enough
			}
		}
		count += 1
	}
	return count
}

// S54: Markets page — full operations center with session status, active streams, and discovery.
build_markets_page :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + SETTINGS_PAD_X
	y := workspace.pos.y + 20
	content_w := workspace.size.x - SETTINGS_PAD_X * 2
	if content_w < 100 do content_w = 100
	bottom := workspace.pos.y + workspace.size.y

	// --- Page header: title + connection + freshness badges ---
	ui.push_text(&state.cmd_buf, {x, y}, "Markets",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)

	// Connection badge (right-aligned).
	conn_disp := current_conn_status_display(state)
	badge_w := ui.status_badge_width(conn_disp.label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := x + content_w - badge_w
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y - 4, badge_w, f32(16)),
		conn_disp.label, conn_disp.dot_color, conn_disp.text_color, state.text.measure, ui.FONT_SIZE_XS)

	// Freshness badge (left of connection badge).
	if state.freshness.loaded && current_conn_status(state) == .Connected {
		fr_label := state.freshness.active ? "FLOWING" : "STALE"
		fr_color := state.freshness.active ? ui.COL_GREEN : ui.COL_YELLOW_ACCENT
		fr_w := ui.status_badge_width(fr_label, state.text.measure, ui.FONT_SIZE_XS)
		ui.status_badge(&state.cmd_buf,
			ui.rect_xywh(badge_x - fr_w - 6, y - 4, fr_w, f32(16)),
			fr_label, fr_color, fr_color, state.text.measure, ui.FONT_SIZE_XS)
	}
	y += 24

	// --- Session status line ---
	session_buf: [80]u8
	session_str: string
	if state.bootstrap.has_session {
		if state.bootstrap.ready {
			session_str = "Session: ready"
		} else {
			session_str = "Session: not ready"
		}
	} else {
		session_str = "Session: no bootstrap"
	}
	ui.push_text(&state.cmd_buf, {x, y + 10}, session_str,
		state.bootstrap.has_session && !state.bootstrap.ready ? ui.COL_WARNING : ui.COL_TEXT_MUTED,
		ui.FONT_SIZE_XS, .Mono)
	y += 18

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x, y}, to = {x + content_w, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 6

	// --- Resolve all market rows ---
	rows: [MARKET_ROW_CAP]Market_Row_View
	row_count := resolve_market_rows(state, &rows)

	// Partition into active (subscribed) and available (not subscribed).
	active_count := 0
	available_count := 0
	for ri in 0 ..< row_count {
		if rows[ri].is_subscribed {
			active_count += 1
		} else {
			available_count += 1
		}
	}

	// --- Section: ACTIVE STREAMS ---
	active_hdr_buf: [32]u8
	active_hdr := fmt.bprintf(active_hdr_buf[:], "ACTIVE STREAMS (%d)", active_count)
	ui.push_text(&state.cmd_buf, {x, y + 10}, active_hdr, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 20

	row_h := f32(44)

	if active_count == 0 {
		ui.push_text(&state.cmd_buf, {x + 4, y + 10},
			"No streams — subscribe from Available Markets below",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 22
	} else {
		for ri in 0 ..< row_count {
			if !rows[ri].is_subscribed do continue
			if y + row_h > bottom - 40 do break
			y = draw_active_market_row(state, &rows[ri], x, y, content_w, row_h, pointer)
		}
	}

	y += 4
	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x, y}, to = {x + content_w, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 6

	// --- Section: AVAILABLE MARKETS ---
	avail_hdr_buf: [32]u8
	avail_hdr := fmt.bprintf(avail_hdr_buf[:], "AVAILABLE (%d)", available_count)
	ui.push_text(&state.cmd_buf, {x, y + 10}, avail_hdr, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 20

	avail_row_h := f32(24)
	if available_count == 0 && active_count > 0 {
		ui.push_text(&state.cmd_buf, {x + 4, y + 10},
			"All markets subscribed",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 18
	} else {
		for ri in 0 ..< row_count {
			if rows[ri].is_subscribed do continue
			if y + avail_row_h > bottom - 10 do break
			y = draw_available_market_row(state, &rows[ri], x, y, content_w, avail_row_h, pointer)
		}
	}
}

// Draw a single active (subscribed) market row. Returns new y after row.
@(private = "file")
draw_active_market_row :: proc(
	state: ^App_State, row: ^Market_Row_View,
	x, y, content_w, row_h: f32, pointer: ui.Pointer_Input,
) -> f32 {
	row_rect := ui.rect_xywh(x - 4, y, content_w + 8, row_h)
	hovered := ui.rect_contains(row_rect, pointer.pos)

	// Background: active=blue, hovered=subtle highlight.
	if row.is_active {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = row_rect, color = ui.with_alpha(ui.COL_BLUE, 0.12),
		})
	} else if hovered {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = row_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04),
		})
	}

	// Click to switch active stream.
	if hovered && pointer.left_pressed && !row.is_active && row.slot_idx >= 0 {
		reg := state.stream_views
		if reg != nil && row.slot_idx < STREAM_VIEW_CAP && reg.slots[row.slot_idx].used {
			queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = reg.slots[row.slot_idx].subject_id})
		}
	}

	// --- Line 1: venue:symbol + price + change% ---
	text_y1 := y + 14
	label_buf: [64]u8
	label := fmt.bprintf(label_buf[:], "%s:%s", row.venue, row.symbol)
	text_color := row.is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY
	ui.push_text(&state.cmd_buf, {x, text_y1}, label, text_color, ui.FONT_SIZE_XS, .Bold)

	// Price (right-aligned area).
	price_x := x + content_w * 0.55
	if row.has_price {
		decs := ui.auto_price_decimals(row.last_price)
		pp_buf: [24]u8
		price_str := ui.format_price(pp_buf[:], row.last_price, decs)
		bullish := row.last_price >= row.open_price
		price_color := bullish ? ui.COL_GREEN : ui.COL_RED
		ui.push_text(&state.cmd_buf, {price_x, text_y1}, price_str, price_color, ui.FONT_SIZE_XS, .Mono)

		// Change %.
		if row.open_price > 0 {
			change_pct := (row.last_price - row.open_price) / row.open_price * 100.0
			sign := change_pct >= 0 ? "+" : ""
			pct_buf: [16]u8
			pct_str := fmt.bprintf(pct_buf[:], "%s%.2f%%", sign, change_pct)
			pct_color := change_pct >= 0 ? ui.COL_GREEN : ui.COL_RED
			ui.push_text(&state.cmd_buf, {price_x + 90, text_y1}, pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
		}
	}

	// Active dot (rightmost).
	if row.is_active {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = {pos = {x + content_w - 4, y + row_h * 0.5 - 3}, size = {6, 6}},
			color = ui.COL_GREEN,
		})
	}

	// --- Line 2: composition badge + health dot + staleness + unsubscribe ---
	text_y2 := y + 32
	cursor := x + 2

	// Composition badge.
	cursor += draw_composition_badge(&state.cmd_buf, cursor, text_y2, row.composition, state.text.measure)

	// Health dot.
	cursor += draw_health_dot(&state.cmd_buf, cursor, text_y2 - 4, 6, row.health_level, row.has_live_data, row.composition)

	// Staleness info.
	if row.stale_count > 0 {
		stale_buf: [16]u8
		stale_str := fmt.bprintf(stale_buf[:], "%d stale", row.stale_count)
		ui.push_text(&state.cmd_buf, {cursor, text_y2}, stale_str, ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, stale_str).x + 6
	} else if row.aging_count > 0 {
		aging_buf: [16]u8
		aging_str := fmt.bprintf(aging_buf[:], "%d aging", row.aging_count)
		ui.push_text(&state.cmd_buf, {cursor, text_y2}, aging_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, aging_str).x + 6
	}

	// Market type badge.
	if len(row.market_type) > 0 {
		mt_label := row.market_type == "SPOT" ? "SPOT" : "PERP"
		ui.push_text(&state.cmd_buf, {cursor, text_y2}, mt_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	// Unsubscribe button (right side, line 2).
	unsub_w := f32(16)
	unsub_h := f32(14)
	unsub_x := x + content_w - unsub_w - 4
	unsub_y := text_y2 - unsub_h * 0.5 - 2
	unsub_rect := ui.rect_xywh(unsub_x, unsub_y, unsub_w, unsub_h)
	unsub_hov := ui.rect_contains(unsub_rect, pointer.pos)
	unsub_bg := unsub_hov ? ui.with_alpha(ui.COL_RED, 0.3) : ui.with_alpha(ui.COL_RED, 0.12)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = unsub_rect, color = unsub_bg})
	ui.push_text(&state.cmd_buf,
		{unsub_x + unsub_w * 0.5 - 3, unsub_y + unsub_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		"-", unsub_hov ? ui.COL_RED : ui.with_alpha(ui.COL_RED, 0.7), ui.FONT_SIZE_XS, .Bold)
	if unsub_hov && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Unsubscribe_Market, market_entry_idx = row.market_idx})
	}

	return y + row_h
}

// Draw a single available (not subscribed) market row. Returns new y after row.
@(private = "file")
draw_available_market_row :: proc(
	state: ^App_State, row: ^Market_Row_View,
	x, y, content_w, row_h: f32, pointer: ui.Pointer_Input,
) -> f32 {
	item_rect := ui.rect_xywh(x - 2, y, content_w + 4, row_h)
	hovered := ui.rect_contains(item_rect, pointer.pos)
	if hovered {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = item_rect, color = ui.with_alpha(ui.COL_GREEN, 0.06),
		})
	}

	// Click to subscribe.
	if hovered && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Subscribe_Market, market_entry_idx = row.market_idx})
	}

	text_y := y + row_h * 0.5 + ui.FONT_SIZE_XS * 0.35
	label_buf: [64]u8
	label := fmt.bprintf(label_buf[:], "+ %s:%s", row.venue, row.symbol)
	ui.push_text(&state.cmd_buf, {x + 4, text_y}, label,
		hovered ? ui.COL_GREEN : ui.with_alpha(ui.COL_GREEN, 0.7),
		ui.FONT_SIZE_XS, .Mono)

	// Market type.
	if len(row.market_type) > 0 {
		mt_label := row.market_type == "SPOT" ? "SPOT" : "PERP"
		ui.push_text(&state.cmd_buf, {x + content_w - 36, text_y}, mt_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	return y + row_h
}

// S54: Compact markets detail panel — summary with link to Markets page.
@(private = "package")
draw_markets_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y
	bottom := rect.pos.y + rect.size.y

	// Header: "MARKETS" + connection badge.
	ui.push_text(&state.cmd_buf, {rect.pos.x + 2, y + 14}, "MARKETS",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	conn_disp := current_conn_status_display(state)
	badge_w := ui.status_badge_width(conn_disp.label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := rect.pos.x + rect.size.x - badge_w - 4
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y + 2, badge_w, f32(16)),
		conn_disp.label, conn_disp.dot_color, conn_disp.text_color, state.text.measure, ui.FONT_SIZE_XS)
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

	// Compact list of subscribed markets with health dot.
	item_h := f32(20)
	if reg != nil {
		for si in 0 ..< STREAM_VIEW_CAP {
			if !reg.slots[si].used do continue
			if y + item_h > bottom - 28 do break
			slot := &reg.slots[si]
			if !slot.has_stream_info {
				refresh_stream_info_for_slot(state, slot)
			}
			if !slot.has_stream_info do continue

			is_active := reg.has_active && slot.subject_id == reg.active_subject_id
			sl_buf: [48]u8
			sl_label := fmt.bprintf(sl_buf[:], "%s:%s", slot.stream_info.venue, slot.stream_info.symbol)

			sym_rect := ui.rect_xywh(rect.pos.x + 2, y, rect.size.x - 4, item_h)
			sym_hovered := ui.rect_contains(sym_rect, pointer.pos)
			if is_active {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = sym_rect, color = ui.with_alpha(ui.COL_BLUE, 0.12),
				})
			} else if sym_hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
					rect = sym_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04),
				})
			}

			// Click to switch active.
			if sym_hovered && pointer.left_pressed && !is_active {
				queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = slot.subject_id})
			}

			// Health dot.
			now_ms := current_now_ms(state)
			tf_ms := global_tf_ms(state)
			apply := slot.apply_state
			health := md_common.stream_health_level(apply, now_ms, tf_ms)
			has_live := false
			for kind in md_common.Artifact_Kind {
				if apply.has_live[kind] { has_live = true; break }
			}
			comp := md_common.apply_state_composition_stage(apply)
			draw_health_dot(&state.cmd_buf, rect.pos.x + 6, y + item_h * 0.5, 5, health, has_live, comp)

			ui.push_text(&state.cmd_buf,
				{rect.pos.x + 16, y + item_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
				sl_label, is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY,
				ui.FONT_SIZE_XS, .Mono)

			y += item_h
		}
	}

	// "Open Markets" link at bottom → navigates to Markets route.
	if y + 22 < bottom {
		y += 2
		link_rect := ui.rect_xywh(rect.pos.x + 4, y, rect.size.x - 8, f32(20))
		link_hovered := ui.rect_contains(link_rect, pointer.pos)
		if link_hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = link_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05),
			})
		}
		ui.push_text(&state.cmd_buf,
			{rect.pos.x + 8, y + 13},
			"Open Markets", link_hovered ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS, .Mono)
		if link_hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = .Markets})
		}
	}
}
