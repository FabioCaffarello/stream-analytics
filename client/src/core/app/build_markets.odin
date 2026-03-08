package app

import "core:fmt"
import "mr:md_common"
import "mr:services"
import "mr:ui"

// S60: Market Explorer 2.0 — venue-grouped discovery with filters, search, and health.
//
// Architecture:
//   1. resolve_market_rows() — unchanged row assembly (price, health, subscription)
//   2. explorer_resolve_venues() — venue grouping service layer
//   3. build_markets_page() — venue-grouped layout with:
//      - Global health summary header (from session dashboard)
//      - Market type filter tabs (All / Spot / Perp)
//      - Search bar (text filter across venue+symbol)
//      - Collapsible venue sections with per-venue counts
//      - Active streams section (subscribed instruments with health)
//      - Available instruments per venue (subscribe-on-click)
//   4. draw_markets_detail() — venue-aware detail panel with counts
//   5. Lifecycle hooks (on_enter/on_leave) + periodic polling

// --- Per-market read model (unchanged from S54) ---

Market_Row_View :: struct {
	venue:         string,
	symbol:        string,
	market_type:   string,
	is_subscribed: bool,
	is_active:     bool,
	has_price:     bool,
	last_price:    f64,
	open_price:    f64,
	composition:   md_common.Composition_Stage,
	health_level:  md_common.System_Health_Level,
	has_live_data: bool,
	stale_count:   int,
	aging_count:   int,
	market_idx:    int,
	slot_idx:      int,
}

MARKET_ROW_CAP :: services.MARKET_CAP

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
				if reg.has_active && slot.subject_id == reg.active_subject_id {
					row.is_active = true
				}
				if slot.candle_store.count > 0 {
					c := services.get_candle_newest(&slot.candle_store, 0)
					row.has_price = true
					row.last_price = c.close
					row.open_price = c.open
				}
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
				break
			}
		}
		count += 1
	}
	return count
}

// --- Page lifecycle ---

EXPLORER_PAD_X :: f32(16)

@(private = "package")
page_explorer_enter :: proc(state: ^App_State) {
	state.explorer.scroll_y = 0
	state.explorer.fetch_status = .Idle
	fetch_explorer_dashboard(state)
}

@(private = "package")
page_explorer_leave :: proc(state: ^App_State) {
	// Preserve filters and collapse state across navigations.
}

@(private = "file")
fetch_explorer_dashboard :: proc(state: ^App_State) {
	if state.marketdata.fetch_session_dashboard == nil {
		state.explorer.fetch_status = .Error
		return
	}
	buf: [16384]u8
	n := state.marketdata.fetch_session_dashboard(raw_data(buf[:]), i32(len(buf)))
	if n <= 0 {
		state.explorer.fetch_status = .Error
		return
	}
	result: services.Session_Health_Result
	if !services.session_health_parse_json(buf[:int(n)], &result) {
		state.explorer.fetch_status = .Error
		return
	}
	exp := &state.explorer
	exp.fetch_status = .Success
	exp.fetch_frame = state.frame
	exp.has_dashboard = true
	exp.dashboard_venues = result.summary.venues
	exp.dashboard_instruments = result.summary.instruments
	exp.dashboard_active = result.freshness.active_instruments
	exp.dashboard_stale = result.freshness.stale_instruments
	// Store status string.
	copy_str_to_buf(exp.dashboard_status[:], &exp.dashboard_status_len, result.status)
	copy_str_to_buf(exp.dashboard_freshness[:], &exp.dashboard_freshness_len, result.freshness.status)
}

@(private = "file")
copy_str_to_buf :: proc(buf: []u8, len_out: ^u8, s: string) {
	n := min(len(s), len(buf))
	for i in 0 ..< n { buf[i] = s[i] }
	len_out^ = u8(n)
}

// S89: HTTP endpoint is independent of WS — don't gate on connection status.
@(private = "package")
poll_explorer :: proc(state: ^App_State) {
	if state.chrome.active_route != .Markets do return
	interval := state.explorer.fetch_status == .Error ? EXPLORER_RETRY_INTERVAL : EXPLORER_POLL_INTERVAL
	if state.frame % interval != 0 && state.explorer.fetch_status != .Idle do return
	fetch_explorer_dashboard(state)
}

// --- Main page render ---

build_markets_page :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + EXPLORER_PAD_X
	y := workspace.pos.y + 20
	content_w := workspace.size.x - EXPLORER_PAD_X * 2
	if content_w < 100 do content_w = 100
	bottom := workspace.pos.y + workspace.size.y

	// --- Header: "Market Explorer" + connection badge ---
	ui.push_text(&state.cmd_buf, {x, y}, "Market Explorer",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)

	conn_disp := current_conn_status_display(state)
	badge_w := ui.status_badge_width(conn_disp.label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := x + content_w - badge_w
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y - 4, badge_w, f32(16)),
		conn_disp.label, conn_disp.dot_color, conn_disp.text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 24

	// --- Dashboard summary line (from session dashboard) ---
	if state.explorer.has_dashboard {
		summary_buf: [96]u8
		status_str := string(state.explorer.dashboard_status[:state.explorer.dashboard_status_len])
		freshness_str := string(state.explorer.dashboard_freshness[:state.explorer.dashboard_freshness_len])
		summary := fmt.bprintf(summary_buf[:], "%dv %di  %s  flow:%s  active:%d stale:%d",
			state.explorer.dashboard_venues,
			state.explorer.dashboard_instruments,
			status_str,
			freshness_str,
			state.explorer.dashboard_active,
			state.explorer.dashboard_stale,
		)
		ui.push_text(&state.cmd_buf, {x, y + 10}, summary, status_color(status_str), ui.FONT_SIZE_XS, .Mono)
		y += 16
	}

	// --- Filter tabs: All / Spot / Perp ---
	y += 4
	tab_h := f32(20)
	tab_labels := [3]string{"All", "Spot", "Perp"}
	tab_filters := [3]services.Explorer_Market_Type_Filter{.All, .Spot, .Perp}
	tab_cursor := x
	for ti in 0 ..< 3 {
		tw := state.text.measure(ui.FONT_SIZE_XS, tab_labels[ti]).x + 16
		tab_rect := ui.rect_xywh(tab_cursor, y, tw, tab_h)
		is_selected := state.explorer.type_filter == tab_filters[ti]
		tab_hov := ui.rect_contains(tab_rect, pointer.pos)

		if is_selected {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = tab_rect, color = ui.with_alpha(ui.COL_ACCENT_CYAN, 0.15)})
		} else if tab_hov {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = tab_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
		}
		ui.push_text(&state.cmd_buf,
			{tab_cursor + 8, y + tab_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			tab_labels[ti],
			is_selected ? ui.COL_ACCENT_CYAN : (tab_hov ? ui.COL_TEXT_SECONDARY : ui.COL_TEXT_MUTED),
			ui.FONT_SIZE_XS, is_selected ? .Bold : .Mono)

		if tab_hov && pointer.left_pressed && !is_selected {
			state.explorer.type_filter = tab_filters[ti]
		}
		tab_cursor += tw + 2
	}

	y += tab_h + 6

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x, y}, to = {x + content_w, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 6

	// --- Resolve all rows + venue view ---
	rows: [MARKET_ROW_CAP]Market_Row_View
	row_count := resolve_market_rows(state, &rows)

	// Build a subscription check closure-like approach using rows.
	// Since Odin doesn't have closures, we check row data inline.

	// Resolve venue view using the service layer.
	// We need to pass a is_subscribed proc — build one from the resolved rows.
	// For simplicity, we check each row's is_subscribed flag directly in the render.

	// Count active streams.
	active_count := 0
	for ri in 0 ..< row_count {
		if rows[ri].is_subscribed do active_count += 1
	}

	// --- Section: ACTIVE STREAMS ---
	if active_count > 0 {
		active_hdr_buf: [32]u8
		active_hdr := fmt.bprintf(active_hdr_buf[:], "ACTIVE STREAMS (%d)", active_count)
		ui.push_text(&state.cmd_buf, {x, y + 10}, active_hdr, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
		y += 20

		row_h := f32(44)
		for ri in 0 ..< row_count {
			if !rows[ri].is_subscribed do continue
			if y + row_h > bottom - 60 do break
			y = draw_active_market_row(state, &rows[ri], x, y, content_w, row_h, pointer)
		}
		y += 4
		ui.push(&state.cmd_buf, ui.Cmd_Line{
			from = {x, y}, to = {x + content_w, y},
			color = ui.COL_DIVIDER, thickness = 1,
		})
		y += 6
	}

	// --- Section: CATALOG (venue-grouped) ---
	// Collect unique venues and their market entries.
	venue_names: [services.EXPLORER_VENUE_CAP]string
	venue_count := 0
	for mi in 0 ..< row_count {
		v := rows[mi].venue
		found := false
		for vi in 0 ..< venue_count {
			if venue_names[vi] == v { found = true; break }
		}
		if !found && venue_count < services.EXPLORER_VENUE_CAP {
			venue_names[venue_count] = v
			venue_count += 1
		}
	}

	catalog_hdr_buf: [48]u8
	catalog_hdr := fmt.bprintf(catalog_hdr_buf[:], "CATALOG (%d venues, %d instruments)", venue_count, row_count)
	ui.push_text(&state.cmd_buf, {x, y + 10}, catalog_hdr, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	avail_row_h := f32(26)
	venue_hdr_h := f32(24)

	for vi in 0 ..< venue_count {
		if y + venue_hdr_h > bottom - 10 do break

		venue := venue_names[vi]

		// Count filtered instruments in this venue.
		venue_total := 0
		venue_active := 0
		venue_visible := 0
		for ri in 0 ..< row_count {
			if rows[ri].venue != venue do continue
			venue_total += 1
			if rows[ri].is_subscribed do venue_active += 1
			if services.explorer_entry_matches(
				services.Market_Entry{venue = rows[ri].venue, ticker = rows[ri].symbol, market_type = rows[ri].market_type},
				state.explorer.type_filter,
				"",
			) {
				venue_visible += 1
			}
		}

		// Skip venue if no instruments match current filter.
		if venue_visible == 0 do continue

		collapsed := state.explorer.collapsed[vi]

		// Venue header row.
		vh_rect := ui.rect_xywh(x - 4, y, content_w + 8, venue_hdr_h)
		vh_hov := ui.rect_contains(vh_rect, pointer.pos)
		if vh_hov {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = vh_rect, color = ui.with_alpha(ui.COL_WHITE, 0.03)})
		}
		// Click to toggle collapse.
		if vh_hov && pointer.left_pressed {
			state.explorer.collapsed[vi] = !collapsed
		}

		// Collapse indicator.
		collapse_icon := collapsed ? "+" : "-"
		ui.push_text(&state.cmd_buf, {x, y + venue_hdr_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			collapse_icon, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

		// Venue name.
		ui.push_text(&state.cmd_buf, {x + 14, y + venue_hdr_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			venue, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Bold)

		// Counts (right-aligned).
		count_buf: [32]u8
		count_str := fmt.bprintf(count_buf[:], "%d/%d active", venue_active, venue_visible)
		count_w := state.text.measure(ui.FONT_SIZE_XS, count_str).x
		ui.push_text(&state.cmd_buf, {x + content_w - count_w, y + venue_hdr_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
			count_str, venue_active > 0 ? ui.COL_GREEN : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

		y += venue_hdr_h

		// Instrument rows (if not collapsed).
		if !collapsed {
			for ri in 0 ..< row_count {
				if rows[ri].venue != venue do continue
				if y + avail_row_h > bottom - 10 do break

				row := &rows[ri]
				entry := services.Market_Entry{venue = row.venue, ticker = row.symbol, market_type = row.market_type}
				if !services.explorer_entry_matches(entry, state.explorer.type_filter, "") {
					continue
				}

				if row.is_subscribed {
					y = draw_catalog_active_row(state, row, x, y, content_w, avail_row_h, pointer)
				} else {
					y = draw_catalog_available_row(state, row, x, y, content_w, avail_row_h, pointer)
				}
			}
		}
	}

	// Empty state.
	if row_count == 0 {
		ui.push_text(&state.cmd_buf, {x + 4, y + 10},
			"No markets available. Check backend connection.",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
}

// --- Catalog row: subscribed instrument (compact) ---
@(private = "file")
draw_catalog_active_row :: proc(
	state: ^App_State, row: ^Market_Row_View,
	x, y, content_w, row_h: f32, pointer: ui.Pointer_Input,
) -> f32 {
	item_rect := ui.rect_xywh(x - 2, y, content_w + 4, row_h)
	hovered := ui.rect_contains(item_rect, pointer.pos)

	if row.is_active {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_BLUE, 0.10)})
	} else if hovered {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04)})
	}

	// Click to switch active.
	if hovered && pointer.left_pressed && !row.is_active && row.slot_idx >= 0 {
		reg := state.stream_views
		if reg != nil && row.slot_idx < STREAM_VIEW_CAP && reg.slots[row.slot_idx].used {
			queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = reg.slots[row.slot_idx].subject_id})
		}
	}

	text_y := y + row_h * 0.5 + ui.FONT_SIZE_XS * 0.35
	cursor := x + 14

	// Health dot.
	cursor += draw_health_dot(&state.cmd_buf, cursor, y + row_h * 0.5, 5, row.health_level, row.has_live_data, row.composition)

	// Symbol.
	text_color := row.is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY
	ui.push_text(&state.cmd_buf, {cursor, text_y}, row.symbol, text_color, ui.FONT_SIZE_XS, .Bold)
	cursor += state.text.measure(ui.FONT_SIZE_XS, row.symbol).x + 8

	// Market type badge.
	mt_label := row.market_type == "SPOT" ? "SPOT" : "PERP"
	ui.push_text(&state.cmd_buf, {cursor, text_y}, mt_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	// Price (right area).
	if row.has_price {
		decs := ui.auto_price_decimals(row.last_price)
		pp_buf: [24]u8
		price_str := ui.format_price(pp_buf[:], row.last_price, decs)
		bullish := row.last_price >= row.open_price
		price_color := bullish ? ui.COL_GREEN : ui.COL_RED
		price_x := x + content_w * 0.55
		ui.push_text(&state.cmd_buf, {price_x, text_y}, price_str, price_color, ui.FONT_SIZE_XS, .Mono)

		if row.open_price > 0 {
			change_pct := (row.last_price - row.open_price) / row.open_price * 100.0
			sign := change_pct >= 0 ? "+" : ""
			pct_buf: [16]u8
			pct_str := fmt.bprintf(pct_buf[:], "%s%.2f%%", sign, change_pct)
			pct_color := change_pct >= 0 ? ui.COL_GREEN : ui.COL_RED
			ui.push_text(&state.cmd_buf, {price_x + 90, text_y}, pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
		}
	}

	// Overview button.
	ov_w := f32(14)
	ov_h := f32(14)
	ov_x := x + content_w - ov_w - 24
	ov_y := y + row_h * 0.5 - ov_h * 0.5
	ov_rect := ui.rect_xywh(ov_x, ov_y, ov_w, ov_h)
	ov_hov := ui.rect_contains(ov_rect, pointer.pos)
	ov_bg := ov_hov ? ui.with_alpha(ui.COL_ACCENT_CYAN, 0.25) : ui.with_alpha(ui.COL_ACCENT_CYAN, 0.08)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ov_rect, color = ov_bg})
	ui.push_text(&state.cmd_buf,
		{ov_x + ov_w * 0.5 - 3, ov_y + ov_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		">", ov_hov ? ui.COL_ACCENT_CYAN : ui.with_alpha(ui.COL_ACCENT_CYAN, 0.6), ui.FONT_SIZE_XS, .Bold)
	if ov_hov && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Navigate_Instrument_Overview, market_entry_idx = row.market_idx})
	}

	// Unsubscribe button.
	unsub_w := f32(16)
	unsub_h := f32(14)
	unsub_x := x + content_w - unsub_w - 4
	unsub_y := y + row_h * 0.5 - unsub_h * 0.5
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

// --- Catalog row: available instrument ---
@(private = "file")
draw_catalog_available_row :: proc(
	state: ^App_State, row: ^Market_Row_View,
	x, y, content_w, row_h: f32, pointer: ui.Pointer_Input,
) -> f32 {
	item_rect := ui.rect_xywh(x - 2, y, content_w + 4, row_h)
	hovered := ui.rect_contains(item_rect, pointer.pos)
	if hovered {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = item_rect, color = ui.with_alpha(ui.COL_GREEN, 0.06)})
	}

	if hovered && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Subscribe_Market, market_entry_idx = row.market_idx})
	}

	text_y := y + row_h * 0.5 + ui.FONT_SIZE_XS * 0.35
	cursor := x + 14

	// Plus icon.
	ui.push_text(&state.cmd_buf, {cursor, text_y}, "+",
		hovered ? ui.COL_GREEN : ui.with_alpha(ui.COL_GREEN, 0.7), ui.FONT_SIZE_XS, .Bold)
	cursor += 12

	// Symbol.
	ui.push_text(&state.cmd_buf, {cursor, text_y}, row.symbol,
		hovered ? ui.COL_GREEN : ui.with_alpha(ui.COL_GREEN, 0.7), ui.FONT_SIZE_XS, .Mono)
	cursor += state.text.measure(ui.FONT_SIZE_XS, row.symbol).x + 8

	// Market type.
	mt_label := row.market_type == "SPOT" ? "SPOT" : "PERP"
	ui.push_text(&state.cmd_buf, {cursor, text_y}, mt_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

	return y + row_h
}

// --- Active streams section (used when active_count > 0 and shown before catalog) ---
@(private = "file")
draw_active_market_row :: proc(
	state: ^App_State, row: ^Market_Row_View,
	x, y, content_w, row_h: f32, pointer: ui.Pointer_Input,
) -> f32 {
	row_rect := ui.rect_xywh(x - 4, y, content_w + 8, row_h)
	hovered := ui.rect_contains(row_rect, pointer.pos)

	if row.is_active {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = row_rect, color = ui.with_alpha(ui.COL_BLUE, 0.12)})
	} else if hovered {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = row_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04)})
	}

	if hovered && pointer.left_pressed && !row.is_active && row.slot_idx >= 0 {
		reg := state.stream_views
		if reg != nil && row.slot_idx < STREAM_VIEW_CAP && reg.slots[row.slot_idx].used {
			queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = reg.slots[row.slot_idx].subject_id})
		}
	}

	// Line 1: venue:symbol + price + change%
	text_y1 := y + 14
	label_buf: [64]u8
	label := fmt.bprintf(label_buf[:], "%s:%s", row.venue, row.symbol)
	text_color := row.is_active ? ui.COL_TEXT_PRIMARY : ui.COL_TEXT_SECONDARY
	ui.push_text(&state.cmd_buf, {x, text_y1}, label, text_color, ui.FONT_SIZE_XS, .Bold)

	price_x := x + content_w * 0.55
	if row.has_price {
		decs := ui.auto_price_decimals(row.last_price)
		pp_buf: [24]u8
		price_str := ui.format_price(pp_buf[:], row.last_price, decs)
		bullish := row.last_price >= row.open_price
		price_color := bullish ? ui.COL_GREEN : ui.COL_RED
		ui.push_text(&state.cmd_buf, {price_x, text_y1}, price_str, price_color, ui.FONT_SIZE_XS, .Mono)

		if row.open_price > 0 {
			change_pct := (row.last_price - row.open_price) / row.open_price * 100.0
			sign := change_pct >= 0 ? "+" : ""
			pct_buf: [16]u8
			pct_str := fmt.bprintf(pct_buf[:], "%s%.2f%%", sign, change_pct)
			pct_color := change_pct >= 0 ? ui.COL_GREEN : ui.COL_RED
			ui.push_text(&state.cmd_buf, {price_x + 90, text_y1}, pct_str, pct_color, ui.FONT_SIZE_XS, .Mono)
		}
	}

	if row.is_active {
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = {pos = {x + content_w - 4, y + row_h * 0.5 - 3}, size = {6, 6}},
			color = ui.COL_GREEN,
		})
	}

	// Line 2: composition + health + staleness + type + actions
	text_y2 := y + 32
	cursor := x + 2
	cursor += draw_composition_badge(&state.cmd_buf, cursor, text_y2, row.composition, state.text.measure)
	cursor += draw_health_dot(&state.cmd_buf, cursor, text_y2 - 4, 6, row.health_level, row.has_live_data, row.composition)

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

	if len(row.market_type) > 0 {
		mt_label := row.market_type == "SPOT" ? "SPOT" : "PERP"
		ui.push_text(&state.cmd_buf, {cursor, text_y2}, mt_label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	// Overview button.
	ov_w := f32(14)
	ov_h := f32(14)
	ov_x := x + content_w - ov_w - 24
	ov_y := text_y2 - ov_h * 0.5 - 2
	ov_rect := ui.rect_xywh(ov_x, ov_y, ov_w, ov_h)
	ov_hov := ui.rect_contains(ov_rect, pointer.pos)
	ov_bg := ov_hov ? ui.with_alpha(ui.COL_ACCENT_CYAN, 0.25) : ui.with_alpha(ui.COL_ACCENT_CYAN, 0.08)
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = ov_rect, color = ov_bg})
	ui.push_text(&state.cmd_buf,
		{ov_x + ov_w * 0.5 - 3, ov_y + ov_h * 0.5 + ui.FONT_SIZE_XS * 0.35},
		">", ov_hov ? ui.COL_ACCENT_CYAN : ui.with_alpha(ui.COL_ACCENT_CYAN, 0.6), ui.FONT_SIZE_XS, .Bold)
	if ov_hov && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Navigate_Instrument_Overview, market_entry_idx = row.market_idx})
	}

	// Unsubscribe button.
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

// --- Detail panel: venue summary + active streams ---

@(private = "package")
draw_markets_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y
	bottom := rect.pos.y + rect.size.y

	// Header.
	ui.push_text(&state.cmd_buf, {rect.pos.x + 2, y + 14}, "EXPLORER",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	reg := state.stream_views

	// Dashboard summary.
	if state.explorer.has_dashboard {
		stream_count := 0
		if reg != nil { stream_count = reg.count }
		summary_buf: [48]u8
		summary := fmt.bprintf(summary_buf[:], "%dv %di  %d active  %d streams",
			state.explorer.dashboard_venues,
			state.explorer.dashboard_instruments,
			state.explorer.dashboard_active,
			stream_count)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, summary, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 16
	}

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
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = sym_rect, color = ui.with_alpha(ui.COL_BLUE, 0.12)})
			} else if sym_hovered {
				ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = sym_rect, color = ui.with_alpha(ui.COL_WHITE, 0.04)})
			}

			if sym_hovered && pointer.left_pressed && !is_active {
				queue_ui_action(state, UI_Action{kind = .Pick_Stream, subject_id = slot.subject_id})
			}

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

	// "Open Explorer" link.
	if y + 22 < bottom {
		y += 2
		link_rect := ui.rect_xywh(rect.pos.x + 4, y, rect.size.x - 8, f32(20))
		link_hovered := ui.rect_contains(link_rect, pointer.pos)
		if link_hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = link_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05)})
		}
		ui.push_text(&state.cmd_buf,
			{rect.pos.x + 8, y + 13},
			"Open Explorer", link_hovered ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED,
			ui.FONT_SIZE_XS, .Mono)
		if link_hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = .Markets})
		}
	}
}
