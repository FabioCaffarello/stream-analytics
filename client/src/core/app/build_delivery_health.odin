package app

import "core:fmt"
import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:ui"

// S59: Delivery Health / Diagnostics page — consolidates backend session dashboard
// with client-side transport metrics into a single diagnostic surface.
//
// Data sources:
//   Backend: GET /api/v1/session/dashboard (status, readiness, freshness, resync, artifacts, summary)
//   Client:  MD_Runtime_Metrics (transport, protocol, RTT, parse timings, desync, reconnects)
//   Client:  Aggregate_Health_Summary (slot composition, recovery, staleness)
//   Client:  Recovery_Event_Log (recent recovery events ring buffer)
//
// The page is organized in a clear information hierarchy:
//   1. Session status summary (backend)
//   2. Connection / transport (client)
//   3. Data flow: freshness + channels (backend)
//   4. Delivery: streams, resyncs, drops (backend)
//   5. Client health: composition, recovery (client)
//   6. Artifacts coverage (backend)

HEALTH_PAD_X :: f32(16)
HEALTH_POLL_INTERVAL :: u64(600) // ~10s at 60fps
HEALTH_RETRY_INTERVAL :: u64(300) // ~5s at 60fps — faster retry on error

// --- Page lifecycle ---

@(private = "package")
page_delivery_health_enter :: proc(state: ^App_State) {
	state.delivery_health = {}
	fetch_delivery_health(state)
}

@(private = "package")
page_delivery_health_leave :: proc(state: ^App_State) {
	state.delivery_health = {}
}

// --- Fetch ---

@(private = "file")
fetch_delivery_health :: proc(state: ^App_State) {
	if state.marketdata.fetch_session_dashboard == nil {
		state.delivery_health.fetch_status = .Error
		return
	}

	buf: [16384]u8 // session dashboard is larger than instrument overview
	n := state.marketdata.fetch_session_dashboard(raw_data(buf[:]), i32(len(buf)))
	if n <= 0 {
		state.delivery_health.fetch_status = .Error
		return
	}

	result: services.Delivery_Health_Result
	if !services.delivery_health_parse_json(buf[:int(n)], &result) {
		state.delivery_health.fetch_status = .Error
		return
	}
	state.delivery_health.view = result
	state.delivery_health.fetch_status = .Success
	state.delivery_health.fetch_frame = state.frame
}

// Poll periodically while on the page.
// S89: HTTP endpoint is independent of WS — don't gate on connection status.
//      Auto-retry on error with shorter interval so first-load succeeds without manual click.
@(private = "package")
poll_delivery_health :: proc(state: ^App_State) {
	if state.chrome.active_route != .Delivery_Health do return
	interval := state.delivery_health.fetch_status == .Error ? HEALTH_RETRY_INTERVAL : HEALTH_POLL_INTERVAL
	if state.frame % interval != 0 && state.delivery_health.fetch_status != .Idle do return
	fetch_delivery_health(state)
}

// --- Page render ---

@(private = "package")
page_delivery_health_render :: proc(state: ^App_State, workspace: ui.Rect, pointer: ui.Pointer_Input) {
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = workspace, color = ui.COL_SURFACE_1})

	x := workspace.pos.x + HEALTH_PAD_X
	y := workspace.pos.y + 20
	content_w := workspace.size.x - HEALTH_PAD_X * 2
	if content_w < 100 do content_w = 100
	bottom := workspace.pos.y + workspace.size.y

	// --- Header ---
	ui.push_text(&state.cmd_buf, {x, y}, "Delivery Health",
		ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_LG, .Bold)

	// Connection badge (right-aligned).
	conn_disp := current_conn_status_display(state)
	badge_w := ui.status_badge_width(conn_disp.label, state.text.measure, ui.FONT_SIZE_XS)
	badge_x := x + content_w - badge_w
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(badge_x, y - 4, badge_w, f32(16)),
		conn_disp.label, conn_disp.dot_color, conn_disp.text_color, state.text.measure, ui.FONT_SIZE_XS)
	y += 24

	// --- Fetch status gate ---
	sh := &state.delivery_health
	if sh.fetch_status == .Idle {
		ui.push_text(&state.cmd_buf, {x, y + 10}, "Loading...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return
	}
	if sh.fetch_status == .Error {
		ui.push_text(&state.cmd_buf, {x, y + 10},
			"Failed to load session data. Check backend connection.",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		y += 16
		retry_rect := ui.rect_xywh(x, y + 4, f32(60), f32(18))
		retry_hov := ui.rect_contains(retry_rect, pointer.pos)
		ui.push_text(&state.cmd_buf, {x + 4, y + 16}, "Retry",
			retry_hov ? ui.COL_ACCENT_CYAN : ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		if retry_hov && pointer.left_pressed {
			fetch_delivery_health(state)
		}
		return
	}

	// --- Success: render sections ---

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {x, y}, to = {x + content_w, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 8

	view := &sh.view

	// Section 1: SESSION STATUS
	y = draw_health_session_status(state, view, x, y, content_w)
	if y > bottom - 20 do return

	// Section 2: CONNECTION / TRANSPORT (client-owned)
	y = draw_health_transport(state, x, y, content_w)
	if y > bottom - 20 do return

	// Section 3: FRESHNESS
	y = draw_health_freshness(state, view, x, y, content_w)
	if y > bottom - 20 do return

	// Section 4: DELIVERY
	y = draw_health_delivery(state, view, x, y, content_w)
	if y > bottom - 20 do return

	// Section 5: CLIENT HEALTH (composition + recovery)
	y = draw_health_client(state, x, y, content_w)
	if y > bottom - 20 do return

	// Section 6: ARTIFACTS COVERAGE
	y = draw_health_artifacts(state, view, x, y, content_w, bottom)
}

// --- Section 1: Session Status ---

@(private = "file")
draw_health_session_status :: proc(
	state: ^App_State, view: ^services.Delivery_Health_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "SESSION", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	status := len(view.status) > 0 ? view.status : "unknown"
	status_color := status_color(status)
	badge_w := ui.status_badge_width(status, state.text.measure, ui.FONT_SIZE_XS)
	ui.status_badge(&state.cmd_buf,
		ui.rect_xywh(x + 70, y + 2, badge_w, f32(16)),
		status, status_color, status_color, state.text.measure, ui.FONT_SIZE_XS)

	// Readiness inline.
	r_status := len(view.readiness_status) > 0 ? view.readiness_status : "unknown"
	r_color := r_status == "ready" ? ui.COL_GREEN : ui.COL_WARNING
	r_x := x + 70 + badge_w + 12
	ui.push_text(&state.cmd_buf, {r_x, y + 10}, "readiness:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	r_x += state.text.measure(ui.FONT_SIZE_XS, "readiness:").x + 4
	ui.push_text(&state.cmd_buf, {r_x, y + 10}, r_status, r_color, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// Summary row: venues + instruments.
	sum_buf: [48]u8
	sum_str := fmt.bprintf(sum_buf[:], "%d venues  %d instruments", view.summary.venues, view.summary.instruments)
	ui.push_text(&state.cmd_buf, {x + 12, y + 10}, sum_str,
		ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

	// Server time age.
	if view.server_time_ms > 0 {
		age_ms := current_now_ms(state) - view.server_time_ms
		age_buf: [16]u8
		age_str := format_ms_short_into(age_buf[:], age_ms)
		ts_buf: [24]u8
		ts_str := fmt.bprintf(ts_buf[:], "updated %s ago", age_str)
		ui.push_text(&state.cmd_buf, {x + 250, y + 10}, ts_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
	y += 22
	return y
}

// --- Section 2: Transport (client-owned) ---

@(private = "file")
draw_health_transport :: proc(
	state: ^App_State,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "TRANSPORT", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 20

	m: ports.MD_Runtime_Metrics
	has_metrics := state.marketdata.metrics != nil && state.marketdata.metrics(&m)
	if !has_metrics {
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "No transport metrics available",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		return y + 22
	}

	// Row 1: transport state + protocol version + transport mode.
	cursor := x + 12
	ts_label := transport_state_label(m.transport_state)
	ts_color := transport_state_color(m.transport_state)
	ui.push_text(&state.cmd_buf, {cursor, y + 10}, ts_label, ts_color, ui.FONT_SIZE_XS, .Mono)
	cursor += state.text.measure(ui.FONT_SIZE_XS, ts_label).x + 16

	if m.protocol_version > 0 {
		pv_buf: [16]u8
		pv_str := fmt.bprintf(pv_buf[:], "proto v%d", m.protocol_version)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, pv_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, pv_str).x + 16
	}

	mode_label := m.transport_mode == 0 ? "Terminal_V1" : "Legacy_JSON"
	mode_color := m.transport_mode == 0 ? ui.COL_GREEN : ui.COL_WARNING
	ui.push_text(&state.cmd_buf, {cursor, y + 10}, mode_label, mode_color, ui.FONT_SIZE_XS, .Mono)
	y += 16

	// Row 2: RTT, lag, msg rate.
	cursor = x + 12
	if m.pong_rtt_ms > 0 {
		rtt_buf: [24]u8
		rtt_str := fmt.bprintf(rtt_buf[:], "RTT: %dms", m.pong_rtt_ms)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, rtt_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, rtt_str).x + 16
	}
	if m.lag_ms > 0 {
		lag_buf: [24]u8
		lag_str := fmt.bprintf(lag_buf[:], "lag: %dms", m.lag_ms)
		lag_color := m.lag_ms > 5000 ? ui.COL_WARNING : ui.COL_TEXT_SECONDARY
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, lag_str, lag_color, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, lag_str).x + 16
	}
	if m.msg_rate > 0 {
		rate_buf: [24]u8
		rate_str := fmt.bprintf(rate_buf[:], "%.0f msg/s", m.msg_rate)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, rate_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, rate_str).x + 16
	}
	if m.bytes_rate > 0 {
		br_buf: [24]u8
		br_str: string
		if m.bytes_rate > 1_000_000 {
			br_str = fmt.bprintf(br_buf[:], "%.1f MB/s", m.bytes_rate / 1_000_000)
		} else if m.bytes_rate > 1_000 {
			br_str = fmt.bprintf(br_buf[:], "%.1f KB/s", m.bytes_rate / 1_000)
		} else {
			br_str = fmt.bprintf(br_buf[:], "%.0f B/s", m.bytes_rate)
		}
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, br_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
	y += 16

	// Row 3: subs, reconnects, desync, drops.
	cursor = x + 12
	sub_buf: [16]u8
	sub_str := fmt.bprintf(sub_buf[:], "subs: %d", m.active_subs)
	ui.push_text(&state.cmd_buf, {cursor, y + 10}, sub_str,
		ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	cursor += state.text.measure(ui.FONT_SIZE_XS, sub_str).x + 16

	if m.reconnect_count > 0 {
		rc_buf: [24]u8
		rc_str := fmt.bprintf(rc_buf[:], "reconnects: %d", m.reconnect_count)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, rc_str,
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, rc_str).x + 16
	}
	if m.desync {
		reason := desync_reason_label(m.desync_reason)
		d_buf: [48]u8
		d_str := fmt.bprintf(d_buf[:], "DESYNC: %s", reason)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, d_str,
			ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, d_str).x + 16
	}
	if m.drop_count > 0 {
		dr_buf: [24]u8
		dr_str := fmt.bprintf(dr_buf[:], "drops: %d", m.drop_count)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, dr_str,
			ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
	}
	y += 16

	// Row 4: parse timings (p95/p99).
	if m.parse_time_p95_us > 0 || m.apply_time_p95_us > 0 {
		cursor = x + 12
		if m.parse_time_p95_us > 0 {
			pt_buf: [32]u8
			pt_str := fmt.bprintf(pt_buf[:], "parse p95: %dus p99: %dus", m.parse_time_p95_us, m.parse_time_p99_us)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, pt_str,
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, pt_str).x + 16
		}
		if m.apply_time_p95_us > 0 {
			at_buf: [32]u8
			at_str := fmt.bprintf(at_buf[:], "apply p95: %dus p99: %dus", m.apply_time_p95_us, m.apply_time_p99_us)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, at_str,
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		}
		y += 16
	}

	// Row 5: server-pushed metrics (if available).
	if m.server_ws_queue_len > 0 || m.server_ws_dropped > 0 || m.server_backpressure_level > 0 {
		cursor = x + 12
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, "server:", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, "server:").x + 4
		if m.server_ws_queue_len > 0 {
			sq_buf: [24]u8
			sq_str := fmt.bprintf(sq_buf[:], "queue: %d", m.server_ws_queue_len)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, sq_str,
				ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, sq_str).x + 12
		}
		if m.server_ws_dropped > 0 {
			sd_buf: [24]u8
			sd_str := fmt.bprintf(sd_buf[:], "dropped: %d", m.server_ws_dropped)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, sd_str,
				ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, sd_str).x + 12
		}
		if m.server_backpressure_level > 0 {
			bp_buf: [24]u8
			bp_str := fmt.bprintf(bp_buf[:], "backpressure: %d", m.server_backpressure_level)
			bp_color := m.server_backpressure_level >= 2 ? ui.COL_RED : ui.COL_WARNING
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, bp_str, bp_color, ui.FONT_SIZE_XS, .Mono)
		}
		y += 16
	}

	y += 6
	return y
}

// --- Section 3: Freshness ---

@(private = "file")
draw_health_freshness :: proc(
	state: ^App_State, view: ^services.Delivery_Health_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "FRESHNESS", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	f := &view.freshness
	status := len(f.status) > 0 ? f.status : "unknown"
	color := freshness_color(status)
	ui.push_text(&state.cmd_buf, {x + 80, y + 10}, status, color, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// Instrument breakdown.
	cursor := x + 12
	if f.active_instruments > 0 || f.stale_instruments > 0 {
		ai_buf: [32]u8
		ai_str := fmt.bprintf(ai_buf[:], "active: %d", f.active_instruments)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, ai_str,
			ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, ai_str).x + 16

		if f.stale_instruments > 0 {
			si_buf: [32]u8
			si_str := fmt.bprintf(si_buf[:], "stale: %d", f.stale_instruments)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, si_str,
				ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, si_str).x + 16
		}
	}

	// Channel breakdown.
	if f.flowing_channels > 0 || f.stale_channels > 0 {
		fc_buf: [32]u8
		fc_str := fmt.bprintf(fc_buf[:], "channels flowing: %d", f.flowing_channels)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, fc_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, fc_str).x + 12

		if f.stale_channels > 0 {
			sc_buf: [32]u8
			sc_str := fmt.bprintf(sc_buf[:], "stale: %d", f.stale_channels)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, sc_str,
				ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		}
	}
	y += 22
	return y
}

// --- Section 4: Delivery ---

@(private = "file")
draw_health_delivery :: proc(
	state: ^App_State, view: ^services.Delivery_Health_Result,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "DELIVERY", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	r := &view.resync
	status := len(r.status) > 0 ? r.status : "unknown"
	color := resync_color(status)
	ui.push_text(&state.cmd_buf, {x + 80, y + 10}, status, color, ui.FONT_SIZE_XS, .Mono)
	y += 20

	cursor := x + 12
	if r.connections_active > 0 {
		ca_buf: [24]u8
		ca_str := fmt.bprintf(ca_buf[:], "connections: %d", r.connections_active)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, ca_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, ca_str).x + 16
	}
	if r.streams > 0 {
		st_buf: [24]u8
		st_str := fmt.bprintf(st_buf[:], "streams: %d", r.streams)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, st_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, st_str).x + 16
	}
	if r.resync_total > 0 {
		rs_buf: [24]u8
		rs_str := fmt.bprintf(rs_buf[:], "resyncs: %d", r.resync_total)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, rs_str,
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, rs_str).x + 16
	}
	if r.drops_total > 0 {
		dr_buf: [24]u8
		dr_str := fmt.bprintf(dr_buf[:], "drops: %d", r.drops_total)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, dr_str,
			ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, dr_str).x + 16
	}
	if r.max_lag_ms > 0 {
		ml_buf: [24]u8
		ml_str := fmt.bprintf(ml_buf[:], "max lag: %dms", r.max_lag_ms)
		lag_color := r.max_lag_ms > 5000 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, ml_str, lag_color, ui.FONT_SIZE_XS, .Mono)
	}
	y += 22
	return y
}

// --- Section 5: Client Health ---

@(private = "file")
draw_health_client :: proc(
	state: ^App_State,
	x, y_in, content_w: f32,
) -> f32 {
	y := y_in
	ui.push_text(&state.cmd_buf, {x, y + 10}, "CLIENT HEALTH", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)

	// Aggregate health from stream slots.
	agg := compute_aggregate_health(state)
	health_label := health_level_label(agg.health_level)
	health_color := health_level_color(agg.health_level)
	ui.push_text(&state.cmd_buf, {x + 110, y + 10}, health_label, health_color, ui.FONT_SIZE_XS, .Mono)
	y += 20

	// Composition breakdown.
	cursor := x + 12
	if agg.slot_count > 0 {
		sc_buf: [24]u8
		sc_str := fmt.bprintf(sc_buf[:], "slots: %d", agg.slot_count)
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, sc_str,
			ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
		cursor += state.text.measure(ui.FONT_SIZE_XS, sc_str).x + 12

		if agg.slots_composed > 0 {
			cc_buf: [24]u8
			cc_str := fmt.bprintf(cc_buf[:], "composed: %d", agg.slots_composed)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, cc_str,
				ui.COL_GREEN, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, cc_str).x + 12
		}
		if agg.slots_live_only > 0 {
			lo_buf: [24]u8
			lo_str := fmt.bprintf(lo_buf[:], "live-only: %d", agg.slots_live_only)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, lo_str,
				ui.COL_YELLOW_ACCENT, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, lo_str).x + 12
		}
		if agg.slots_pending > 0 {
			pb_buf: [24]u8
			pb_str := fmt.bprintf(pb_buf[:], "pending: %d", agg.slots_pending)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, pb_str,
				ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, pb_str).x + 12
		}
		if agg.slots_empty > 0 {
			eb_buf: [24]u8
			eb_str := fmt.bprintf(eb_buf[:], "empty: %d", agg.slots_empty)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, eb_str,
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		}
	} else {
		ui.push_text(&state.cmd_buf, {cursor, y + 10}, "No active slots",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
	y += 16

	// Recovery / staleness row.
	if agg.slots_recovering > 0 || agg.slots_exhausted > 0 || agg.total_stale > 0 || agg.total_aging > 0 {
		cursor = x + 12
		if agg.slots_recovering > 0 {
			rr_buf: [24]u8
			rr_str := fmt.bprintf(rr_buf[:], "recovering: %d", agg.slots_recovering)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, rr_str,
				ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, rr_str).x + 12
		}
		if agg.slots_exhausted > 0 {
			ex_buf: [24]u8
			ex_str := fmt.bprintf(ex_buf[:], "exhausted: %d", agg.slots_exhausted)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, ex_str,
				ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, ex_str).x + 12
		}
		if agg.total_stale > 0 {
			ts_buf: [24]u8
			ts_str := fmt.bprintf(ts_buf[:], "stale: %d", agg.total_stale)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, ts_str,
				ui.COL_RED, ui.FONT_SIZE_XS, .Mono)
			cursor += state.text.measure(ui.FONT_SIZE_XS, ts_str).x + 12
		}
		if agg.total_aging > 0 {
			ta_buf: [24]u8
			ta_str := fmt.bprintf(ta_buf[:], "aging: %d", agg.total_aging)
			ui.push_text(&state.cmd_buf, {cursor, y + 10}, ta_str,
				ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		}
		y += 16
	}

	// Recovery event log (last few entries from ring buffer).
	log := &state.recovery_log
	if log.count > 0 {
		y += 4
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, "Recent recovery events:",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 16

		show_count := min(log.count, 6) // Show last 6 events max.
		for ei in 0 ..< show_count {
			// Walk backwards from head.
			idx := (log.head - 1 - ei + md_common.RECOVERY_EVENT_LOG_CAP) % md_common.RECOVERY_EVENT_LOG_CAP
			if idx < 0 do idx += md_common.RECOVERY_EVENT_LOG_CAP
			ev := log.events[idx]

			kind_label := recovery_kind_label(ev.kind)
			kind_color := recovery_kind_color(ev.kind)

			ev_buf: [64]u8
			ev_str := fmt.bprintf(ev_buf[:], "  slot %d  %s  attempt %d", ev.slot_id, kind_label, ev.attempts)
			ui.push_text(&state.cmd_buf, {x + 12, y + 10}, ev_str,
				kind_color, ui.FONT_SIZE_XS, .Mono)

			if ev.timestamp > 0 {
				age_ms := current_now_ms(state) - ev.timestamp
				age_buf: [16]u8
				age_str := format_ms_short_into(age_buf[:], age_ms)
				ts_buf: [24]u8
				ts_str := fmt.bprintf(ts_buf[:], "%s ago", age_str)
				ui.push_text(&state.cmd_buf, {x + 320, y + 10}, ts_str,
					ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			}
			y += 14
		}
	}

	y += 6
	return y
}

// --- Section 6: Artifacts Coverage ---

@(private = "file")
draw_health_artifacts :: proc(
	state: ^App_State, view: ^services.Delivery_Health_Result,
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
		if y + 40 > bottom do break
		art := &view.artifacts[ai]

		// Artifact name + coverage status badge.
		name := len(art.name) > 0 ? art.name : "?"
		ui.push_text(&state.cmd_buf, {x + 12, y + 10}, name,
			ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)

		cov_status := len(art.coverage.status) > 0 ? art.coverage.status : "unknown"
		cov_color := coverage_color(cov_status)
		badge_w := ui.status_badge_width(cov_status, state.text.measure, ui.FONT_SIZE_XS)
		ui.status_badge(&state.cmd_buf,
			ui.rect_xywh(x + 80, y + 2, badge_w, f32(16)),
			cov_status, cov_color, cov_color, state.text.measure, ui.FONT_SIZE_XS)
		y += 18

		// Coverage detail row.
		c := &art.coverage
		cov_buf: [80]u8
		cov_str := fmt.bprintf(cov_buf[:], "%d/%d available  %d empty  %d unavailable",
			c.available_instruments, c.total_instruments,
			c.empty_instruments, c.unavailable_instruments)
		ui.push_text(&state.cmd_buf, {x + 24, y + 10}, cov_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Timeframes.
		if art.tf_count > 0 {
			tf_buf: [128]u8
			tf_cursor := 0
			for ti in 0 ..< art.tf_count {
				tf := art.timeframes[ti]
				if tf_cursor > 0 && tf_cursor < len(tf_buf) - 1 {
					tf_buf[tf_cursor] = ' '
					tf_cursor += 1
				}
				n := min(len(tf), len(tf_buf) - tf_cursor)
				for ci in 0 ..< n {
					tf_buf[tf_cursor + ci] = tf[ci]
				}
				tf_cursor += n
			}
			ui.push_text(&state.cmd_buf, {x + 24, y + 10},
				string(tf_buf[:tf_cursor]),
				ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			y += 16
		}

		y += 4
	}
	return y
}

// --- Detail panel ---

@(private = "package")
page_delivery_health_render_detail :: proc(state: ^App_State, rect: ui.Rect, pointer: ui.Pointer_Input) {
	y := rect.pos.y

	ui.push_text(&state.cmd_buf, {rect.pos.x + 2, y + 14}, "HEALTH",
		ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Bold)
	y += 22

	sh := &state.delivery_health
	if sh.fetch_status == .Success {
		view := &sh.view
		status := len(view.status) > 0 ? view.status : "unknown"
		color := status_color(status)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, status,
			color, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Summary.
		sum_buf: [32]u8
		sum_str := fmt.bprintf(sum_buf[:], "%dv  %di",
			view.summary.venues, view.summary.instruments)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, sum_str,
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Freshness.
		f_status := len(view.freshness.status) > 0 ? view.freshness.status : "?"
		f_color := freshness_color(f_status)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, f_status,
			f_color, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Delivery.
		d_status := len(view.resync.status) > 0 ? view.resync.status : "?"
		d_color := resync_color(d_status)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, d_status,
			d_color, ui.FONT_SIZE_XS, .Mono)
		y += 16

		// Aggregate client health.
		agg := compute_aggregate_health(state)
		h_label := health_level_label(agg.health_level)
		h_color := health_level_color(agg.health_level)
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, h_label,
			h_color, ui.FONT_SIZE_XS, .Mono)
	} else if sh.fetch_status == .Error {
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, "Error",
			ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
	} else {
		ui.push_text(&state.cmd_buf, {rect.pos.x + 4, y + 10}, "Loading...",
			ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
}

// S64: Color helpers now delegate to shared shell_common helpers.

@(private = "file")
health_level_label :: proc(level: md_common.System_Health_Level) -> string {
	switch level {
	case .Healthy:   return "healthy"
	case .Degraded:  return "degraded"
	case .Unhealthy: return "unhealthy"
	case .Critical:  return "critical"
	}
	return "unknown"
}

@(private = "file")
health_level_color :: proc(level: md_common.System_Health_Level) -> ui.Color {
	switch level {
	case .Healthy:   return ui.COL_GREEN
	case .Degraded:  return ui.COL_WARNING
	case .Unhealthy: return ui.COL_RED
	case .Critical:  return ui.COL_RED
	}
	return ui.COL_TEXT_MUTED
}

@(private = "file")
transport_state_label :: proc(ts: ports.MD_Transport_State) -> string {
	switch ts {
	case .Connected:     return "connected"
	case .Hello_Pending: return "hello_pending"
	case .Running:       return "running"
	case .Desync:        return "desync"
	case .Backoff:       return "backoff"
	}
	return "unknown"
}

@(private = "file")
transport_state_color :: proc(ts: ports.MD_Transport_State) -> ui.Color {
	switch ts {
	case .Running:       return ui.COL_GREEN
	case .Connected:     return ui.COL_YELLOW_ACCENT
	case .Hello_Pending: return ui.COL_YELLOW_ACCENT
	case .Desync:        return ui.COL_RED
	case .Backoff:       return ui.COL_WARNING
	}
	return ui.COL_TEXT_MUTED
}

@(private = "file")
desync_reason_label :: proc(reason: ports.MD_Desync_Reason) -> string {
	switch reason {
	case .Sequence_Gap:     return "sequence_gap"
	case .Snapshot_Gap:     return "snapshot_gap"
	case .Protocol_Version: return "protocol_version"
	case .Protocol_Invalid: return "protocol_invalid"
	case .Missing_Hello:    return "missing_hello"
	case .Resync_Required:  return "resync_required"
	case .None:             return "none"
	}
	return "unknown"
}

@(private = "file")
recovery_kind_label :: proc(kind: md_common.Recovery_Event_Kind) -> string {
	switch kind {
	case .Attempt:   return "attempt"
	case .Success:   return "success"
	case .Exhausted: return "exhausted"
	case .Reset:     return "reset"
	}
	return "?"
}

@(private = "file")
recovery_kind_color :: proc(kind: md_common.Recovery_Event_Kind) -> ui.Color {
	switch kind {
	case .Attempt:   return ui.COL_WARNING
	case .Success:   return ui.COL_GREEN
	case .Exhausted: return ui.COL_RED
	case .Reset:     return ui.COL_TEXT_MUTED
	}
	return ui.COL_TEXT_MUTED
}
