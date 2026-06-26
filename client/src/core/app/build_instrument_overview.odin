package app

import "core:fmt"
import "mr:services"
import "mr:ui"

// S58: Instrument Overview page — backend-owned composed read model for instrument-level
// diagnostics. Consumes GET /api/v1/instrument/overview as the single canonical source
// instead of reconstructing instrument state from scattered client-side sources.

OVERVIEW_PAD_X :: f32(16)
OVERVIEW_POLL_INTERVAL :: u64(600) // ~10s at 60fps
OVERVIEW_RETRY_INTERVAL :: u64(300) // ~5s at 60fps — faster retry on error

// --- Page lifecycle ---

// S58: on_enter — resolve target instrument from active stream and trigger initial fetch.
@(private = "package")
page_instrument_overview_enter :: proc(state: ^App_State) {
	slot := stream_view_active_slot(state.stream_views)
	if slot == nil || !slot.has_stream_info {
		state.instrument_overview = {}
		return
	}
	venue := slot.stream_info.venue
	symbol := normalized_symbol(slot.stream_info.symbol)

	ov := &state.instrument_overview
	ov^ = {}
	vn := min(len(venue), len(ov.venue))
	for i in 0 ..< vn {
		ov.venue[i] = venue[i]
	}
	ov.venue_len = u8(vn)
	sn := min(len(symbol), len(ov.symbol))
	for i in 0 ..< sn {
		ov.symbol[i] = symbol[i]
	}
	ov.symbol_len = u8(sn)

	fetch_instrument_overview(state)
}

// S58: on_leave — clear overview state.
@(private = "package")
page_instrument_overview_leave :: proc(state: ^App_State) {
	state.instrument_overview = {}
}

// --- Fetch ---

// Fetch overview from backend. Called on page enter and periodically.
@(private = "file")
fetch_instrument_overview :: proc(state: ^App_State) {
	if state.marketdata.fetch_instrument_overview == nil {
		state.instrument_overview.fetch_status = .Error
		return
	}
	ov := &state.instrument_overview
	venue := string(ov.venue[:int(ov.venue_len)])
	symbol := string(ov.symbol[:int(ov.symbol_len)])
	if len(venue) == 0 || len(symbol) == 0 {
		ov.fetch_status = .Error
		return
	}

	buf: [8192]u8
	n := state.marketdata.fetch_instrument_overview(raw_data(buf[:]), i32(len(buf)), venue, symbol)
	if n <= 0 {
		ov.fetch_status = .Error
		return
	}

	result: services.Instrument_Overview_Result
	if !services.instrument_overview_parse_json(buf[:int(n)], &result) {
		ov.fetch_status = .Error
		return
	}
	ov.view = result
	ov.fetch_status = .Success
	ov.fetch_frame = state.frame
}

// Poll overview periodically while on the page.
// S89: HTTP endpoint is independent of WS — don't gate on connection status.
@(private = "package")
poll_instrument_overview :: proc(state: ^App_State) {
	if state.chrome.active_route != .Instrument_Overview do return
	if state.instrument_overview.venue_len == 0 do return
	interval := state.instrument_overview.fetch_status == .Error ? OVERVIEW_RETRY_INTERVAL : OVERVIEW_POLL_INTERVAL
	if state.frame % interval != 0 && state.instrument_overview.fetch_status != .Idle do return
	fetch_instrument_overview(state)
}

// --- Navigation helper ---

// Navigate to instrument overview for a market entry. Switches active stream if needed.
@(private = "package")
apply_navigate_instrument_overview :: proc(state: ^App_State, market_entry_idx: int) {
	if market_entry_idx < 0 || market_entry_idx >= state.stores.markets.count do return

	// Switch active stream to match the target market, if subscribed.
	entry := state.stores.markets.entries[market_entry_idx]
	reg := state.stream_views
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
			// Found matching slot — make it active.
			if !reg.has_active || reg.active_subject_id != slot.subject_id {
				apply_pick_stream_action(state, slot.subject_id)
			}
			break
		}
	}

	page_navigate(state, state.chrome.active_route, .Instrument_Overview)
}

// --- Page render ---

@(private = "package")
page_overview_render :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + OVERVIEW_PAD_X
	y := workspace.pos.y + 20
	content_w := workspace.size.x - OVERVIEW_PAD_X * 2
	if content_w < 100 do content_w = 100
	bottom := workspace.pos.y + workspace.size.y

	ov := &state.instrument_overview

	// --- Back link ---
	back_rect := ui.rect_xywh(x, y - 6, f32(100), f32(20))
	back_hovered := ui.rect_contains(back_rect, pointer.pos)
	ui.push_text(&state.cmd_buf, {x, y + 6}, "<- Markets",
		back_hovered ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	if back_hovered && pointer.left_pressed {
		queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = .Markets})
	}
	y += 24

	// --- Header: venue:symbol ---
	venue := string(ov.venue[:int(ov.venue_len)])
	symbol := string(ov.symbol[:int(ov.symbol_len)])
	hdr_buf: [64]u8
	hdr := fmt.bprintf(hdr_buf[:], "%s : %s", venue, symbol)
	ui.push_text(&state.cmd_buf, {x, y}, hdr, ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)

	// Connection badge (right-aligned).
	conn_disp := current_conn_status_display(state)
	badge_w := ui.status_badge_width(conn_disp.label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := x + content_w - badge_w
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y - 4, badge_w, f32(16)),
		conn_disp.label, conn_disp.dot_color, conn_disp.text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 24

	// --- Fetch status gate ---
	if ov.venue_len == 0 {
		ui.push_text(&state.cmd_buf, {x, y + 10},
			"No instrument selected. Subscribe to a market first.",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}
	if ov.fetch_status == .Idle {
		ui.push_text(&state.cmd_buf, {x, y + 10}, "Loading...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}
	if ov.fetch_status == .Error {
		ui.push_text(&state.cmd_buf, {x, y + 10},
			"Failed to load overview. Check backend connection.",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		// Show retry hint.
		y += 16
		retry_rect := ui.rect_xywh(x, y + 4, f32(60), f32(18))
		retry_hov := ui.rect_contains(retry_rect, pointer.pos)
		ui.push_text(&state.cmd_buf, {x + 4, y + 16}, "Retry",
			retry_hov ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		if retry_hov && pointer.left_pressed {
			fetch_instrument_overview(state)
		}
		return
	}

	// --- Success: render view model sections ---
	view := &ov.view

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x, y}, to = {x + content_w, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 8

	// --- Section: STATUS ---
	y = draw_overview_status_section(state, view, x, y, content_w)
	if y > bottom - 20 do return

	// --- Section: READINESS ---
	y = draw_overview_readiness_section(state, view, x, y, content_w)
	if y > bottom - 20 do return

	// --- Section: FRESHNESS ---
	y = draw_overview_freshness_section(state, view, x, y, content_w)
	if y > bottom - 20 do return

	// --- Section: RESYNC DIAGNOSTICS ---
	y = draw_overview_resync_section(state, view, x, y, content_w)
	if y > bottom - 20 do return

	// --- Section: ARTIFACTS ---
	y = draw_overview_artifacts_section(state, view, x, y, content_w, bottom)
}

// --- Section renderers ---

@(private = "file")
draw_overview_status_section :: proc(
	state: ^App_State, view: ^services.Instrument_Overview_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "STATUS", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	// Overall status badge.
	status := len(view.status) > 0 ? view.status : "unknown"
	status_color := status_color(status)
	badge_w := ui.status_badge_width(status, state.text.measure, ui.FONT_SIZE_XS)
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(x + 60, y + 2, badge_w, f32(16)),
		status, status_color, status_color, state.text.measure, ui.FONT_SIZE_XS)

	// Checked at timestamp.
	if view.checked_at > 0 {
		age_ms := current_now_ms(state) - view.checked_at
		age_buf: [16]u8
		age_str := format_ms_short_into(age_buf[:], age_ms)
		ts_buf: [32]u8
		ts_str := fmt.bprintf(ts_buf[:], "checked %s ago", age_str)
		ui.push_text(&state.cmd_buf, {x + 60 + badge_w + 8, y + 10}, ts_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
	y += 22
	return y
}

@(private = "file")
draw_overview_readiness_section :: proc(
	state: ^App_State, view: ^services.Instrument_Overview_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "READINESS", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	status := len(view.readiness_status) > 0 ? view.readiness_status : "unknown"
	color := status == "ready" ? ui.COL_GREEN : ui.COL_WARNING
	ui.push_text(&state.cmd_buf, {x + 80, y + 10}, status, color, ui.FONT_SIZE_XS, .Mono)
	y += 22
	return y
}

@(private = "file")
draw_overview_freshness_section :: proc(
	state: ^App_State, view: ^services.Instrument_Overview_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "FRESHNESS", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	// Overall freshness status.
	status := len(view.freshness_status) > 0 ? view.freshness_status : "unknown"
	color := freshness_color(status)
	ui.push_text(&state.cmd_buf, {x + 80, y + 10}, status, color, ui.FONT_SIZE_XS, .Mono)

	active_label := view.freshness_active ? "active" : "inactive"
	active_color := view.freshness_active ? ui.COL_GREEN : ui.COL_TEXT_MUTED
	ui.push_text(&state.cmd_buf, {x + 160, y + 10}, active_label, active_color, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// Per-channel breakdown.
	for ci in 0 ..< view.channel_count {
		ch := &view.channels[ci]
		ch_name := len(ch.name) > 0 ? ch.name : "?"
		flow_label := ch.flowing ? "flowing" : "stale"
		flow_color := ch.flowing ? ui.COL_GREEN : ui.COL_WARNING

		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, ch_name,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

		dot_x := x + 200
		dot_sz := f32(5)
		ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
			rect = ui.rect_xywh(dot_x, y + 10 - dot_sz * 0.5, dot_sz, dot_sz),
			color = flow_color,
		})

		ui.push_text(&state.cmd_buf, {dot_x + 10, y + 10}, flow_label,
			flow_color, ui.FONT_SIZE_XS, .Mono)

		if ch.lag_ms > 0 {
			lag_buf: [24]u8
			lag_str := fmt.bprintf(lag_buf[:], "lag %dms", ch.lag_ms)
			ui.push_text(&state.cmd_buf, {dot_x + 80, y + 10}, lag_str,
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		}
		y += 16
	}
	if view.channel_count == 0 {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "No channels",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 16
	}
	y += 4
	return y
}

@(private = "file")
draw_overview_resync_section :: proc(
	state: ^App_State, view: ^services.Instrument_Overview_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "RESYNC", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	status := len(view.resync_status) > 0 ? view.resync_status : "unknown"
	color := resync_color(status)
	ui.push_text(&state.cmd_buf, {x + 80, y + 10}, status, color, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// Metrics row.
	cursor := x + 12
	if view.streams > 0 {
		sb: [16]u8
		s := fmt.bprintf(sb[:], "streams: %d", view.streams)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, s, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, s).x + 16
	}
	if view.resync_total > 0 {
		rb: [24]u8
		r := fmt.bprintf(rb[:], "resyncs: %d", view.resync_total)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, r,
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, r).x + 16
	}
	if view.drops_total > 0 {
		db: [24]u8
		d := fmt.bprintf(db[:], "drops: %d", view.drops_total)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, d,
			ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, d).x + 16
	}
	if view.max_lag_ms > 0 {
		lb: [24]u8
		l := fmt.bprintf(lb[:], "max lag: %dms", view.max_lag_ms)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, l,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
	y += 22
	return y
}

@(private = "file")
draw_overview_artifacts_section :: proc(
	state: ^App_State, view: ^services.Instrument_Overview_Result,
	x, y_in, content_w, bottom: f32,
) -> f32 {
	y := y_in
	art_hdr_buf: [32]u8
	art_hdr := fmt.bprintf(art_hdr_buf[:], "ARTIFACTS (%d)", view.artifact_count)
	ui.push_text(&state.cmd_buf, {x, y + 10}, art_hdr, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	if view.artifact_count == 0 {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "No artifacts available",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return y + 16
	}

	for ai in 0 ..< view.artifact_count {
		if y + 60 > bottom do break
		art := &view.artifacts[ai]

		// Artifact name + status badge.
		name := len(art.name) > 0 ? art.name : "?"
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, name,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)

		tl_status := len(art.timeline.status) > 0 ? art.timeline.status : "unknown"
		tl_color := coverage_color(tl_status)
		badge_w := ui.status_badge_width(tl_status, state.text.measure, ui.FONT_SIZE_XS)
		ui.status_badge(&state.cmd_buf,
			ui.rect_xywh(x + 80, y + 2, badge_w, f32(16)),
			tl_status, tl_color, tl_color, state.text.measure, ui.FONT_SIZE_XS)
		y += 18

		// Timeline coverage.
		if art.timeline.first_ts > 0 || art.timeline.last_ts > 0 {
			now_ms := current_now_ms(state)
			span_ms := art.timeline.last_ts - art.timeline.first_ts
			age_ms := now_ms - art.timeline.last_ts

			span_buf: [16]u8
			span_str := format_ms_short_into(span_buf[:], span_ms)
			age_buf: [16]u8
			age_str := format_ms_short_into(age_buf[:], age_ms)

			tl_buf: [64]u8
			tl_str := fmt.bprintf(tl_buf[:], "span: %s  last: %s ago  tf: %s",
				span_str, age_str,
				len(art.timeline.timeframe) > 0 ? art.timeline.timeframe : "?")
			ui.push_text(&state.cmd_buf, {x + 24, y + 10}, tl_str,
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			y += 16
		}

		// Timeframes list.
		if art.tf_count > 0 {
			tf_buf: [128]u8
			cursor := 0
			for ti in 0 ..< art.tf_count {
				tf := art.timeframes[ti]
				if cursor > 0 && cursor < len(tf_buf) - 1 {
					tf_buf[cursor] = ' '
					cursor += 1
				}
				n := min(len(tf), len(tf_buf) - cursor)
				for ci in 0 ..< n {
					tf_buf[cursor + ci] = tf[ci]
				}
				cursor += n
			}
			ui.push_text(&state.cmd_buf, {x + 24, y + 10},
				string(tf_buf[:cursor]),
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			y += 16
		}

		// Endpoint.
		if len(art.endpoint) > 0 {
			ui.push_text(&state.cmd_buf, {x + 24, y + 10}, art.endpoint,
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			y += 16
		}

		y += 4
	}
	return y
}

// --- Detail panel ---

@(private = "package")
page_overview_render_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y

	ui.push_text(&state.cmd_buf, {rect.pos.x + 2, y + 14}, "OVERVIEW",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	ov := &state.instrument_overview
	venue := string(ov.venue[:int(ov.venue_len)])
	symbol := string(ov.symbol[:int(ov.symbol_len)])

	if ov.venue_len > 0 {
		label_buf: [48]u8
		label := fmt.bprintf(label_buf[:], "%s:%s", venue, symbol)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, label,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Mono)
		y += 16
	}

	view := &ov.view
	if ov.fetch_status == .Success {
		// Quick status summary.
		status := len(view.status) > 0 ? view.status : "unknown"
		color := status_color(status)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, status,
			color, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Channel count.
		ch_buf: [24]u8
		ch_str := fmt.bprintf(ch_buf[:], "%d channels", view.channel_count)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, ch_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 16

		art_buf: [24]u8
		art_str := fmt.bprintf(art_buf[:], "%d artifacts", view.artifact_count)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, art_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}

	// Divider.
	y += 20
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {rect.pos.x + 4, y}, to = {rect.pos.x + rect.size.x - 4, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 8

	// Back to Markets link.
	bottom := rect.pos.y + rect.size.y
	if y + 22 < bottom {
		link_rect := ui.rect_xywh(rect.pos.x + 4, y, rect.size.x - 8, f32(20))
		link_hovered := ui.rect_contains(link_rect, pointer.pos)
		if link_hovered {
			ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{
				rect = link_rect, color = ui.with_alpha(ui.COL_WHITE, 0.05),
			})
		}
		ui.push_text(&state.cmd_buf, {rect.pos.x + 8, y + 13}, "Back to Markets",
			link_hovered ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		if link_hovered && pointer.left_pressed {
			queue_ui_action(state, UI_Action{kind = .Navigate_Route, route = .Markets})
		}
	}
}

// S64: Color helpers now delegate to shared shell_common helpers.
