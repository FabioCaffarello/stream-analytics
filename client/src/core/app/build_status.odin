package app

import "core:fmt"
import "mr:layers"
import "mr:md_common"
import "mr:services"
import "mr:streams"
import "mr:ui"

@(private = "package")
refresh_telemetry_hud_cache :: proc(state: ^App_State) {
	if state == nil do return

	now_ms := current_now_ms(state)
	if now_ms <= 0 do return
	if state.telemetry.hud_cache.last_update_ms > 0 &&
		now_ms - state.telemetry.hud_cache.last_update_ms < 250 {
		return
	}
	state.telemetry.hud_cache.last_update_ms = now_ms

	state.telemetry.hud_cache.mps_len = len(fmt.bprintf(
		state.telemetry.hud_cache.mps_buf[:], "MPS:%.1f", state.active_metrics.msg_rate,
	))

	bytes_per_sec := i64(state.active_metrics.bytes_rate)
	if bytes_per_sec >= 1024 * 1024 {
		state.telemetry.hud_cache.bps_len = len(fmt.bprintf(
			state.telemetry.hud_cache.bps_buf[:], "BPS:%dMB/s", bytes_per_sec / (1024 * 1024),
		))
	} else {
		state.telemetry.hud_cache.bps_len = len(fmt.bprintf(
			state.telemetry.hud_cache.bps_buf[:], "BPS:%dKB/s", bytes_per_sec / 1024,
		))
	}

	state.telemetry.hud_cache.cb_len = len(fmt.bprintf(
		state.telemetry.hud_cache.cb_buf[:], "CB:%d", max(state.active_metrics.candle_backlog, 0),
	))
	state.telemetry.hud_cache.arena_len = len(fmt.bprintf(
		state.telemetry.hud_cache.arena_buf[:], "Arena:%d/%d", ui.frame_arena_usage(&state.cmd_buf), ui.frame_arena_capacity(&state.cmd_buf),
	))
	state.telemetry.hud_cache.pm_len = len(fmt.bprintf(
		state.telemetry.hud_cache.pm_buf[:], "PM:%d", state.active_metrics.parsed_msgs_total,
	))
	state.telemetry.hud_cache.pr_len = len(fmt.bprintf(
		state.telemetry.hud_cache.pr_buf[:], "PR:%d", state.active_metrics.parse_arena_resets,
	))
	pb_mb := i64(state.active_metrics.parsed_bytes_total / u64(1024 * 1024))
	state.telemetry.hud_cache.pb_len = len(fmt.bprintf(
		state.telemetry.hud_cache.pb_buf[:], "PB:%dMB", pb_mb,
	))
	frame_p95_us := i64(0)
	if state.telemetry.frame_time_count > 0 {
		_, frame_p95_us, _ = frame_time_percentiles(state)
	}
	// Perf timing: frame p95 + parser/apply p95 + alloc estimate + phase timings.
	state.telemetry.hud_cache.phase_len = len(fmt.bprintf(
		state.telemetry.hud_cache.phase_buf[:], "F95:%dus PR:%dus AP:%dus BD:%dus AL:%d D:%dus U:%dus R:%dus",
		max(frame_p95_us, 0),
		max(state.active_metrics.parse_time_p95_us, 0),
		max(state.active_metrics.apply_time_p95_us, 0),
		max(state.active_metrics.batched_decode_time_p95_us, 0),
		state.active_metrics.alloc_estimate_total,
		max(state.telemetry.drain_us, 0),
		max(state.telemetry.actions_us, 0),
		max(state.telemetry.render_us, 0),
	))
}

// Record a persistent error for status bar display.
@(private = "package")
record_error :: proc(state: ^App_State, kind: Error_Kind, msg: string) {
	if state == nil do return
	n := min(len(msg), len(state.error_state.text))
	for i in 0 ..< n {
		state.error_state.text[i] = msg[i]
	}
	state.error_state.len = n
	state.error_state.frame = state.frame
	state.error_state.error_kind = kind
}

@(private = "package")
desync_reason_short :: proc(reason: streams.Stream_Desync_Reason) -> string {
	switch reason {
	case .Sequence_Gap:
		return "seq gap"
	case .Snapshot_Gap:
		return "snapshot gap"
	case .Snapshot_Stale:
		return "snapshot stale"
	case .Clock_Drift:
		return "clock drift"
	case .Protocol_Version:
		return "proto ver"
	case .Protocol_Invalid:
		return "compat mismatch"
	case .Missing_Hello:
		return "hello missing"
	case .Resync_Required:
		return "resync required"
	case .Manual:
		return "manual"
	case .None:
	}
	return ""
}

@(private = "package")
desync_wait_message :: proc(reason: streams.Stream_Desync_Reason) -> string {
	switch reason {
	case .Sequence_Gap:
		return "DESYNC: sequence gap (Resync)"
	case .Snapshot_Gap:
		return "DESYNC: snapshot gap (Resync)"
	case .Snapshot_Stale:
		return "DESYNC: snapshot stale (Resync)"
	case .Clock_Drift:
		return "DESYNC: clock drift (Resync)"
	case .Protocol_Version:
		return "DESYNC: protocol version mismatch"
	case .Protocol_Invalid:
		return "DESYNC: incompatible protocol (legacy downgrade blocked)"
	case .Missing_Hello:
		return "DESYNC: hello handshake missing"
	case .Resync_Required:
		return "DESYNC: server requires resync"
	case .Manual:
		return "DESYNC: manual resync in progress"
	case .None:
	}
	return "DESYNC (Resync)"
}

@(private = "package")
active_stream_waiting_primary_data :: proc(state: ^App_State) -> bool {
	if state == nil do return false
	if current_conn_status(state) != .Connected do return false
	if state.active_metrics.state == .Offline do return false
	return state.active_metrics.last_stats_ts_ms <= 0 && state.active_metrics.last_orderbook_ts_ms <= 0
}

@(private = "package")
active_stream_reason_short :: proc(state: ^App_State) -> string {
	if state == nil do return ""
	if state.active_metrics.state == .Desync {
		return desync_reason_short(state.active_metrics.desync_reason)
	}
	if state.active_metrics.subscribe_acks <= 0 do return "sub not acked"

	snapshot_ts_ms := i64(0)
	if active := streams.registry_active(&state.stream_registry); active != nil {
		snapshot_ts_ms = active.status.last_snapshot_ts_ms
	}
	if snapshot_ts_ms <= 0 do return "snapshot pending"
	if state.active_metrics.last_stats_ts_ms <= 0 do return "stats pending"
	if state.active_metrics.state == .Lag do return "lagging"
	return ""
}

@(private = "package")
stats_wait_message :: proc(
	stream_state: streams.Stream_State,
	desync_reason: streams.Stream_Desync_Reason,
	subscribe_acks: int,
	stats_last_ts_ms: i64,
) -> string {
	switch stream_state {
	case .Offline:
		return "OFFLINE"
	case .Desync:
		return desync_wait_message(desync_reason)
	case .Lag, .Live:
		if subscribe_acks <= 0 do return "Waiting ACK (stats)..."
		if stats_last_ts_ms <= 0 do return "LIVE (no data): stats pending"
		if stream_state == .Lag do return "LAG (stats delayed)..."
	}
	return "Waiting for stats..."
}

@(private = "package")
orderbook_wait_message :: proc(
	stream_state: streams.Stream_State,
	desync_reason: streams.Stream_Desync_Reason,
	subscribe_acks: int,
	snapshot_ts_ms: i64,
	orderbook_last_ts_ms: i64,
) -> string {
	switch stream_state {
	case .Offline:
		return "OFFLINE"
	case .Desync:
		return desync_wait_message(desync_reason)
	case .Lag, .Live:
		if subscribe_acks <= 0 do return "Waiting ACK (orderbook)..."
		if snapshot_ts_ms <= 0 do return "LIVE (no data): snapshot pending"
		if orderbook_last_ts_ms <= 0 do return "LIVE (no data): orderbook pending"
		if stream_state == .Lag do return "LAG (orderbook delayed)..."
	}
	return "Waiting for orderbook..."
}

// Stream Health panel: floating overlay shown when telemetry HUD is enabled.
// Displays stream registry table, transport metrics, and log entries.
@(private = "package")
build_health_panel :: proc(state: ^App_State, viewport_w, viewport_h: f32, pointer: ui.Pointer_Input) {
	if state == nil do return

	PANEL_W :: f32(420)
	PANEL_H :: f32(460)
	ROW_H :: f32(14)
	SECTION_GAP :: f32(8)
	LOG_VISIBLE :: 20

	pw := min(PANEL_W, viewport_w - 16)
	ph := min(PANEL_H, viewport_h - 48)
	px := viewport_w - pw - 8
	py := TOP_BAR_H + 4
	panel_rect := ui.Rect{pos = {px, py}, size = {pw, ph}}

	prev_z := state.cmd_buf.current_z_layer
	state.cmd_buf.current_z_layer = ui.Z_OVERLAY
	defer { state.cmd_buf.current_z_layer = prev_z }

	// Panel background.
	ui.push(&state.cmd_buf, ui.Cmd_Rect_Filled{rect = panel_rect, color = {0.08, 0.08, 0.10, 0.95}})
	ui.draw_rect_stroke(&state.cmd_buf, panel_rect, ui.COL_BORDER_STRONG)

	y := py + 4
	lx := px + 6 // left margin

	// === STREAMS section ===
	ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "STREAMS", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
	y += ROW_H + 2

	// Column headers.
	COL_ID   :: f32(0)
	COL_ST   :: f32(120)
	COL_LAG  :: f32(160)
	COL_RTT  :: f32(210)
	COL_AGE  :: f32(260)
	COL_DROP :: f32(310)
	COL_RC   :: f32(350)
	headers := [?]struct{off: f32, label: string}{
		{COL_ID, "Stream"},
		{COL_ST, "State"},
		{COL_LAG, "Lag"},
		{COL_RTT, "RTT"},
		{COL_AGE, "Age"},
		{COL_DROP, "Drop"},
		{COL_RC, "RC"},
	}
	for h in headers {
		ui.push_text(&state.cmd_buf, {lx + h.off, y + ROW_H - 2}, h.label, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
	}
	y += ROW_H + 1

	// Divider.
	ui.push(&state.cmd_buf, ui.Cmd_Line{
		from = {lx, y}, to = {px + pw - 6, y},
		color = ui.COL_DIVIDER, thickness = 1,
	})
	y += 2

	// Stream rows from registry.
	reg := &state.stream_registry
	stream_rows := 0
	for i in 0 ..< streams.STREAM_CAP {
		h := &reg.handles[i]
		if !h.used do continue
		if y + ROW_H > py + ph - 100 do break // leave room for metrics + logs
		stream_rows += 1

		// venue:symbol
		id_buf: [48]u8
		id_str := fmt.bprintf(id_buf[:], "%s:%s", streams.stream_venue(h), streams.stream_symbol(h))
		ui.push_text(&state.cmd_buf, {lx + COL_ID, y + ROW_H - 2}, id_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)

		// State
		state_label := "OFF"
		state_color := ui.COL_TEXT_MUTED
		switch h.status.state {
		case .Live:
			state_label = "LIVE"
			state_color = ui.COL_GREEN
		case .Lag:
			state_label = "LAG"
			state_color = ui.COL_YELLOW_ACCENT
		case .Desync:
			state_label = "DSYNC"
			state_color = ui.COL_RED
		case .Offline:
		}
		ui.push_text(&state.cmd_buf, {lx + COL_ST, y + ROW_H - 2}, state_label, state_color, ui.FONT_SIZE_XS, .Mono)

		// Lag
		lag_buf: [16]u8
		lag_str := fmt.bprintf(lag_buf[:], "%d", max(h.status.lag_ms, 0))
		ui.push_text(&state.cmd_buf, {lx + COL_LAG, y + ROW_H - 2}, lag_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

		// RTT
		rtt_buf: [16]u8
		rtt_str := fmt.bprintf(rtt_buf[:], "%d", max(h.status.rtt_ms, 0))
		ui.push_text(&state.cmd_buf, {lx + COL_RTT, y + ROW_H - 2}, rtt_str, ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)

		// Message age
		age_buf: [16]u8
		age_str := fmt.bprintf(age_buf[:], "%d", max(h.status.last_message_age_ms, 0))
		age_color := h.status.last_message_age_ms > 4_000 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {lx + COL_AGE, y + ROW_H - 2}, age_str, age_color, ui.FONT_SIZE_XS, .Mono)

		// Drop count
		drop_buf: [12]u8
		drop_str := fmt.bprintf(drop_buf[:], "%d", max(h.status.drop_count, 0))
		drop_color := h.status.drop_count > 0 ? ui.COL_RED : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {lx + COL_DROP, y + ROW_H - 2}, drop_str, drop_color, ui.FONT_SIZE_XS, .Mono)

		// Reconnect count
		rc_buf: [12]u8
		rc_str := fmt.bprintf(rc_buf[:], "%d", max(h.status.reconnect_count, 0))
		rc_color := h.status.reconnect_count > 0 ? ui.COL_WARNING : ui.COL_TEXT_MUTED
		ui.push_text(&state.cmd_buf, {lx + COL_RC, y + ROW_H - 2}, rc_str, rc_color, ui.FONT_SIZE_XS, .Mono)

		y += ROW_H
	}
	if stream_rows == 0 {
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "(no streams)", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += ROW_H
	}
	y += SECTION_GAP

	// === TRANSPORT section ===
	ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "TRANSPORT", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
	y += ROW_H + 2

	// Row 1: msg_rate, bytes_rate
	mr_buf: [48]u8
	mr_str := fmt.bprintf(mr_buf[:], "msg/s:%.1f  bytes/s:%.0f", state.active_metrics.msg_rate, state.active_metrics.bytes_rate)
	ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, mr_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += ROW_H

	// Row 2: parsed totals
	pt_buf: [96]u8
	pt_str := fmt.bprintf(pt_buf[:], "msgs:%d  bytes:%dMB  resets:%d  batch:%d/%d fp:%d fb:%d",
		state.active_metrics.parsed_msgs_total,
		state.active_metrics.parsed_bytes_total / u64(1024 * 1024),
		state.active_metrics.parse_arena_resets,
		state.active_metrics.batched_frames_received,
		state.active_metrics.batched_events_received,
		state.active_metrics.batched_fastpath_events,
		state.active_metrics.batched_fallback_events)
	ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, pt_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += ROW_H

	// Row 3: parser/apply/batched decode p95 + alloc estimate.
	pf_buf: [120]u8
	pf_str := fmt.bprintf(pf_buf[:], "parse_p95:%dus  apply_p95:%dus  batch_p95:%dus  alloc:%d",
		max(state.active_metrics.parse_time_p95_us, 0),
		max(state.active_metrics.apply_time_p95_us, 0),
		max(state.active_metrics.batched_decode_time_p95_us, 0),
		state.active_metrics.alloc_estimate_total)
	ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, pf_str, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += ROW_H + SECTION_GAP

	if state.active_metrics.transport_mode != 0 || state.active_metrics.legacy_downgrade_count > 0 {
		legacy_buf: [128]u8
		legacy_str := fmt.bprintf(
			legacy_buf[:],
			"LEGACY fallback active (downgrade:%d) - migrate to /ws and set allow_legacy_ws=OFF",
			max(state.active_metrics.legacy_downgrade_count, 0),
		)
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, legacy_str, ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
		y += ROW_H + SECTION_GAP
	}
	if state.active_metrics.assist_enabled {
		assist_reason := "auto"
		if state.active_metrics.assist_reason_len > 0 {
			assist_reason = string(state.active_metrics.assist_reason[:int(state.active_metrics.assist_reason_len)])
		}
		assist_buf: [156]u8
		assist_str := fmt.bprintf(
			assist_buf[:],
			"ASSIST ON (%s) heatmap:%s vpvr:%s getrange:/%d reason:%s",
			state.active_metrics.assist_user_enabled ? "auto" : "manual",
			state.active_metrics.assist_degrade_heatmap ? "off" : "on",
			state.active_metrics.assist_degrade_vpvr ? "off" : "on",
			max(state.active_metrics.assist_getrange_divisor, 1),
			assist_reason,
		)
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, assist_str, ui.COL_YELLOW_ACCENT, ui.FONT_SIZE_XS, .Mono)
		y += ROW_H + SECTION_GAP
	}

	// === SERVER LIMITS section (from HELLO capabilities) ===
	ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "Server Limits:", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
	y += ROW_H + 2
	limits_subs := "n/a"
	if state.active_metrics.server_max_subscriptions > 0 {
		limits_subs_buf: [16]u8
		limits_subs = fmt.bprintf(limits_subs_buf[:], "%d", state.active_metrics.server_max_subscriptions)
	}
	limits_frame := "n/a"
	if state.active_metrics.server_max_frame_bytes > 0 {
		limits_frame_buf: [24]u8
		limits_frame = fmt.bprintf(
			limits_frame_buf[:],
			"%dKB",
			max(state.active_metrics.server_max_frame_bytes, 0) / 1024,
		)
	}
	limits_cadence := "n/a"
	if state.active_metrics.server_metrics_cadence_ms > 0 {
		limits_cadence_buf: [24]u8
		limits_cadence = fmt.bprintf(limits_cadence_buf[:], "%dms", state.active_metrics.server_metrics_cadence_ms)
	}
	limits_line_buf: [128]u8
	limits_line := fmt.bprintf(
		limits_line_buf[:],
		"Subs:%s  Frame:%s  Metrics cadence:%s",
		limits_subs,
		limits_frame,
		limits_cadence,
	)
	ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, limits_line, ui.COL_TEXT_SECONDARY, ui.FONT_SIZE_XS, .Mono)
	y += ROW_H + SECTION_GAP

	// === SERVER section (server-pushed metrics from METRICS frame) ===
	sm := state.active_metrics
	if sm.server_ws_queue_len > 0 || sm.server_ws_dropped > 0 || sm.server_ws_lag_ms > 0 ||
		sm.server_serialize_errors > 0 || sm.server_resync_total > 0 || sm.server_pub_deliver_ms > 0 ||
		sm.server_backpressure_level > 0 {
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "SERVER", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		y += ROW_H + 2

		// Row 1: queue, dropped, lag
		sv1_buf: [64]u8
		sv1_str := fmt.bprintf(sv1_buf[:], "queue:%d  dropped:%d  lag:%dms",
			max(sm.server_ws_queue_len, 0), max(sm.server_ws_dropped, 0), max(sm.server_ws_lag_ms, 0))
		queue_color := sm.server_ws_queue_len > 128 ? ui.COL_YELLOW_ACCENT : ui.COL_TEXT_SECONDARY
		if sm.server_ws_dropped > 0 do queue_color = ui.COL_RED
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, sv1_str, queue_color, ui.FONT_SIZE_XS, .Mono)
		y += ROW_H

		// Row 2: p2d, ser_err, resyncs
		sv2_buf: [64]u8
		sv2_str := fmt.bprintf(sv2_buf[:], "p2d:%dms  ser_err:%d  resyncs:%d",
			max(sm.server_pub_deliver_ms, 0), max(sm.server_serialize_errors, 0), max(sm.server_resync_total, 0))
		sv2_color := sm.server_serialize_errors > 0 ? ui.COL_WARNING : ui.COL_TEXT_SECONDARY
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, sv2_str, sv2_color, ui.FONT_SIZE_XS, .Mono)
		y += ROW_H

		// Row 3: backpressure level + queue capacity
		if sm.server_backpressure_level > 0 || sm.server_queue_capacity > 0 {
			bp_state := md_common.backpressure_state_from_level(sm.server_backpressure_level)
			bp_label := "Normal"
			bp_color := ui.COL_TEXT_SECONDARY
			switch bp_state {
			case .Normal:
			case .Elevated:
				bp_label = "Elevated"
				bp_color = ui.COL_YELLOW_ACCENT
			case .High:
				bp_label = "High"
				bp_color = ui.COL_WARNING
			case .Critical:
				bp_label = "Critical"
				bp_color = ui.COL_RED
			}
			bp_buf: [80]u8
			bp_str := fmt.bprintf(bp_buf[:], "BP:%d(%s) Q:%d/%d HW:%d",
				max(sm.server_backpressure_level, 0), bp_label,
				max(sm.server_ws_queue_len, 0), max(sm.server_queue_capacity, 0),
				max(sm.server_queue_high_watermark, 0))
			ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, bp_str, bp_color, ui.FONT_SIZE_XS, .Mono)
			y += ROW_H
		}
		if sm.server_backpressure_level >= 2 && !state.active_metrics.assist_user_enabled {
			action := "reduce_subscriptions"
			if sm.server_recommended_action_len > 0 {
				action = string(sm.server_recommended_action[:int(sm.server_recommended_action_len)])
			}
			act_buf: [128]u8
			act_str := fmt.bprintf(act_buf[:], "recommended_action:%s", action)
			ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, act_str, ui.COL_WARNING, ui.FONT_SIZE_XS, .Mono)
			apply_btn := ui.button(
				&state.cmd_buf,
				ui.rect_xywh(lx + 220, y, 60, ROW_H + 2),
				"Apply",
				pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono,
			)
			if apply_btn.clicked {
				apply_backpressure_recommendation(state)
			}
			y += ROW_H
		}
		y += SECTION_GAP
		}

		// === EVIDENCE section ===
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "EVIDENCE", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
		y += ROW_H + 2
		ev_visible := min(state.evidence.count, 8)
		ev_now_ms := current_now_ms(state)
		if ev_visible <= 0 {
			ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "(no evidence yet)", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
			y += ROW_H
		} else {
			for ei in 0 ..< ev_visible {
				idx := (state.evidence.head - 1 - ei + EVIDENCE_HISTORY_CAP) % EVIDENCE_HISTORY_CAP
				ev := state.evidence.entries[idx]
				kind := "EV"
				if ev.kind_len > 0 {
					kind = string(ev.kind[:int(ev.kind_len)])
				}
				// Severity color from confidence: <0.5=gray, <0.7=yellow, <0.85=orange, >=0.85=red.
				ev_color := ui.COL_TEXT_MUTED
				sev_label := "LOW"
				if ev.confidence >= 0.85 {
					ev_color = ui.COL_RED
					sev_label = "CRIT"
				} else if ev.confidence >= 0.7 {
					ev_color = ui.COL_ACCENT_ORANGE
					sev_label = "HIGH"
				} else if ev.confidence >= 0.5 {
					ev_color = ui.COL_WARNING
					sev_label = "MED"
				}
				// Timestamp age.
				age_str := ""
				age_buf: [16]u8
				if ev_now_ms > 0 && ev.unix > 0 {
					age_s := max(ev_now_ms - ev.unix, 0) / 1000
					if age_s < 60 {
						age_str = fmt.bprintf(age_buf[:], "%ds", age_s)
					} else {
						age_str = fmt.bprintf(age_buf[:], "%dm%ds", age_s / 60, age_s % 60)
					}
				}
				// Build feature values inline: tag=val pairs.
				feat_buf: [96]u8
				feat_n := 0
				fc := min(ev.feature_count, len(ev.feature_tags))
				for fi in 0 ..< fc {
					tag_len := 0
					for ti in 0 ..< len(ev.feature_tags[fi]) {
						if ev.feature_tags[fi][ti] == 0 do break
						tag_len += 1
					}
					if tag_len <= 0 do continue
					tag := string(ev.feature_tags[fi][:tag_len])
					pair_buf: [48]u8
					pair := fmt.bprintf(pair_buf[:], " %s=%.1f", tag, ev.feature_vals[fi])
					for c in pair {
						if feat_n >= len(feat_buf) do break
						feat_buf[feat_n] = u8(c)
						feat_n += 1
					}
				}
				feat_str := string(feat_buf[:feat_n])
				ev_line: [196]u8
				ev_str := fmt.bprintf(ev_line[:], "[%s] %s c=%.2f%s %s", sev_label, kind, ev.confidence, feat_str, age_str)
				ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, ev_str, ev_color, ui.FONT_SIZE_XS, .Mono)
				y += ROW_H
			}
		}
		y += SECTION_GAP

		// === LOG section ===
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "LOG", ui.COL_TEXT_PRIMARY, ui.FONT_SIZE_XS, .Bold)
	y += ROW_H + 2

	log_count := services.log_count(&state.log_state.buf)
	visible := min(log_count, LOG_VISIBLE)
	remaining_h := (py + ph - 24) - y // leave room for button
	max_rows := int(remaining_h / ROW_H)
	if max_rows < 1 do max_rows = 1
	visible = min(visible, max_rows)

	if visible <= 0 {
		ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, "(no log entries)", ui.COL_TEXT_MUTED, ui.FONT_SIZE_XS, .Mono)
		y += ROW_H
	} else {
		for li in 0 ..< visible {
			entry, ok := services.log_get(&state.log_state.buf, li)
			if !ok do break
			entry_text := string(entry.text[:entry.text_len])
			log_color := ui.COL_TEXT_SECONDARY
			switch entry.level {
			case .WARN:
				log_color = ui.COL_WARNING
			case .ERR:
				log_color = ui.COL_RED
			case .INFO:
			}
			ui.push_text(&state.cmd_buf, {lx, y + ROW_H - 2}, entry_text, log_color, ui.FONT_SIZE_XS, .Mono)
			y += ROW_H
		}
	}
	y += 4

	// === Copy Diagnostics button ===
	btn_w := f32(120)
	btn_h := f32(18)
	btn_rect := ui.rect_xywh(lx, y, btn_w, btn_h)
	if btn_rect.pos.y + btn_h < py + ph {
		btn := ui.button(&state.cmd_buf, btn_rect, "Copy Diagnostics", pointer, state.text.measure, ui.FONT_SIZE_XS, .Mono)
		if btn.clicked {
			copy_diagnostics_to_clipboard(state)
		}
	}
}

// Build diagnostics string and copy to clipboard.
// SECURITY: Output is user-visible (Copy Diagnostics clipboard).
// NEVER include api_key, jwt_token, or credential material.
// Allowed: transport_mode, protocol_version, server_instance_id,
// limits, cadences, negotiated_features, counters.
@(private = "file")
copy_diagnostics_to_clipboard :: proc(state: ^App_State) {
	if state == nil do return
	buf: [4096]u8
	n := 0

	// Helper to append a string.
	append_str :: proc(buf: []u8, n: ^int, s: string) {
		for c in s {
			if n^ >= len(buf) do return
			buf[n^] = u8(c)
			n^ += 1
		}
	}
	// Helper to append a formatted line.
	append_line :: proc(buf: []u8, n: ^int, line_buf: []u8, line_len: int) {
		for i in 0 ..< line_len {
			if n^ >= len(buf) do return
			buf[n^] = line_buf[i]
			n^ += 1
		}
		if n^ < len(buf) { buf[n^] = '\n'; n^ += 1 }
	}

	// Header
	hdr: [64]u8
	hdr_len := len(fmt.bprintf(hdr[:], "=== MR Diagnostics (frame %d) ===", state.frame))
	append_line(buf[:], &n, hdr[:], hdr_len)

	// Streams
	append_str(buf[:], &n, "\nSTREAMS:\n")
	reg := &state.stream_registry
	for i in 0 ..< streams.STREAM_CAP {
		h := &reg.handles[i]
		if !h.used do continue
		line: [128]u8
		state_str := "OFF"
		switch h.status.state {
		case .Live:   state_str = "LIVE"
		case .Lag:    state_str = "LAG"
		case .Desync: state_str = "DESYNC"
		case .Offline:
		}
		line_len := len(fmt.bprintf(line[:], "  %s:%s  %s  lag=%d rtt=%d age=%d drop=%d rc=%d",
			streams.stream_venue(h), streams.stream_symbol(h), state_str,
			max(h.status.lag_ms, 0), max(h.status.rtt_ms, 0),
			max(h.status.last_message_age_ms, 0),
			max(h.status.drop_count, 0), max(h.status.reconnect_count, 0)))
		append_line(buf[:], &n, line[:], line_len)
	}

	append_str(buf[:], &n, "\nLAYERS:\n")
	layer_diags: [layers.LAYER_REGISTRY_CAP]layers.Layer_Diagnostics
	layer_count := layers.layer_registry_collect_diagnostics(&state.layer_registry, &state.layer_store, layer_diags[:])
	for i in 0 ..< layer_count {
		diag := layer_diags[i]
		layer_name := "unknown"
		switch diag.id {
		case .Price_Candles: layer_name = "Price/Candles"
		case .Trades_Tape: layer_name = "Trades Tape"
		case .OrderBook_DOM: layer_name = "OrderBook/DOM"
		case .VPVR_Heatmap: layer_name = "VPVR/Heatmap"
		case .Evidence: layer_name = "Evidence"
		case .Signal: layer_name = "Signal"
		}
		lstate := diag.enabled ? "on" : "off"
		data_state := diag.has_data ? "data" : "empty"
		line: [128]u8
		line_len := len(fmt.bprintf(
			line[:],
			"  %s %s %s renders=%d dropped=%d",
			layer_name, lstate, data_state, diag.render_invocations, diag.dropped_outputs,
		))
		append_line(buf[:], &n, line[:], line_len)
	}

	// Transport
	append_str(buf[:], &n, "\nTRANSPORT:\n")
	frame_p95_us := i64(0)
	if state.telemetry.frame_time_count > 0 {
		_, frame_p95_us, _ = frame_time_percentiles(state)
	}
	t1: [128]u8
	t1_len := len(fmt.bprintf(t1[:], "  msg/s=%.1f bytes/s=%.0f msgs=%d bytes=%dMB resets=%d batch=%d/%d fp=%d fb=%d",
		state.active_metrics.msg_rate, state.active_metrics.bytes_rate,
		state.active_metrics.parsed_msgs_total,
		state.active_metrics.parsed_bytes_total / u64(1024 * 1024),
		state.active_metrics.parse_arena_resets,
		state.active_metrics.batched_frames_received,
		state.active_metrics.batched_events_received,
		state.active_metrics.batched_fastpath_events,
		state.active_metrics.batched_fallback_events))
	append_line(buf[:], &n, t1[:], t1_len)
	t1b: [128]u8
	t1b_len := len(fmt.bprintf(
		t1b[:],
		"  frame_p95=%dus parse_p95=%dus apply_p95=%dus batch_p95=%dus",
		max(frame_p95_us, 0),
		max(state.active_metrics.parse_time_p95_us, 0),
		max(state.active_metrics.apply_time_p95_us, 0),
		max(state.active_metrics.batched_decode_time_p95_us, 0),
	))
	append_line(buf[:], &n, t1b[:], t1b_len)

	// Protocol
	append_str(buf[:], &n, "\nPROTOCOL:\n")
	am := state.active_metrics
	transport_label := am.transport_mode == 0 ? "TerminalV1" : "LegacyJSON"
	auth_label := "none"
	if am.auth_mode == 1 do auth_label = "apikey"
	if am.auth_mode == 2 do auth_label = "jwt"
	transport_state_label := "Connected"
	switch am.transport_state {
	case .Connected: transport_state_label = "Connected"
	case .Hello_Pending: transport_state_label = "HelloPending"
	case .Running: transport_state_label = "Running"
	case .Desync: transport_state_label = "Desync"
	case .Backoff: transport_state_label = "Backoff"
	}
	ws_category_label := "None"
	switch am.ws_error_category {
	case .None: ws_category_label = "None"
	case .AuthDenied: ws_category_label = "AuthDenied"
	case .HandshakeFailed: ws_category_label = "HandshakeFailed"
	case .ServerClosed: ws_category_label = "ServerClosed"
	case .ProtocolError: ws_category_label = "ProtocolError"
	case .Timeout: ws_category_label = "Timeout"
	case .BackpressureDrop: ws_category_label = "BackpressureDrop"
	}
	ws_action_label := "None"
	switch am.ws_error_action {
	case .None: ws_action_label = "None"
	case .Retry: ws_action_label = "Retry"
	case .Downgrade: ws_action_label = "Downgrade"
	case .Resync: ws_action_label = "Resync"
	case .Stop: ws_action_label = "Stop"
	}
	sid_buf: [64]u8
	sid_len := min(int(am.server_instance_id_len), len(am.server_instance_id))
	sid_out := 0
	for i in 0 ..< sid_len {
		if sid_out >= len(sid_buf) do break
		c := am.server_instance_id[i]
		if c >= 32 && c <= 126 {
			sid_buf[sid_out] = c
		} else {
			sid_buf[sid_out] = '?'
		}
		sid_out += 1
	}
	server_id := "(none)"
	if sid_out > 0 do server_id = string(sid_buf[:sid_out])
	p1: [128]u8
	p1_len := len(fmt.bprintf(p1[:], "  transport_mode=%s protocol_version=%d server_instance_id=%s",
		transport_label, am.protocol_version, server_id))
	append_line(buf[:], &n, p1[:], p1_len)
	p2: [128]u8
	p2_len := len(fmt.bprintf(p2[:], "  auth_mode=%s active_subs=%d state=%s ws_fault=%s/%s hello_timeouts=%d",
		auth_label, am.active_subs, transport_state_label, ws_category_label, ws_action_label, am.hello_timeout_count))
	append_line(buf[:], &n, p2[:], p2_len)
	p3: [128]u8
	p3_len := len(fmt.bprintf(p3[:], "  last_server_ts=%d seq_gap_count=%d resync_count=%d",
		max(am.last_server_ts_ms, 0), max(am.seq_gap_count, 0), max(am.resync_count, 0)))
	append_line(buf[:], &n, p3[:], p3_len)
	p4: [128]u8
	p4_len := len(fmt.bprintf(p4[:], "  drops_by_reason trade_ring=%d candle_ring=%d ws_queue=%d payload_oversize=%d total=%d",
		max(am.drop_trade_ring, 0), max(am.drop_candle_ring, 0), max(am.drop_ws_queue, 0), max(am.drop_payload_oversize, 0), max(am.drop_count, 0)))
	append_line(buf[:], &n, p4[:], p4_len)
	p5: [96]u8
	p5_len := len(fmt.bprintf(p5[:], "  rtt=%dms lag=%dms pong_rtt=%dms reconnects=%d",
		max(am.rtt_ms, 0), max(am.lag_ms, 0), max(am.pong_rtt_ms, 0), max(am.reconnect_count, 0)))
	append_line(buf[:], &n, p5[:], p5_len)
	if am.transport_mode != 0 || am.legacy_downgrade_count > 0 {
		p6: [160]u8
		p6_len := len(fmt.bprintf(
			p6[:],
			"  recommendation: disable legacy fallback in profile (allow_legacy_ws=off), downgrades=%d",
			max(am.legacy_downgrade_count, 0),
		))
		append_line(buf[:], &n, p6[:], p6_len)
	}
	if am.assist_enabled {
		assist_reason := "auto"
		if am.assist_reason_len > 0 {
			assist_reason = string(am.assist_reason[:int(am.assist_reason_len)])
		}
		hm := am.assist_degrade_heatmap ? "off" : "on"
		vp := am.assist_degrade_vpvr ? "off" : "on"
		p7: [160]u8
		p7_len := len(fmt.bprintf(
			p7[:],
			"  assist: enabled mode=%s heatmap=%s vpvr=%s getrange_div=%d reason=%s",
			am.assist_user_enabled ? "auto" : "manual",
			hm, vp, max(am.assist_getrange_divisor, 1), assist_reason,
		))
		append_line(buf[:], &n, p7[:], p7_len)
	}

	// Negotiated features.
	if am.negotiated_feature_count > 0 {
		append_str(buf[:], &n, "\nNEGOTIATED FEATURES:\n")
		nf: [128]u8
		nf_n := 0
		nf_n += len(fmt.bprintf(nf[nf_n:], "  count=%d", am.negotiated_feature_count))
		nfc := min(am.negotiated_feature_count, len(am.negotiated_feature_names))
		for i in 0 ..< nfc {
			fl := int(am.negotiated_feature_name_lens[i])
			if fl <= 0 || fl > len(am.negotiated_feature_names[i]) do continue
			if nf_n < len(nf) { nf[nf_n] = ' '; nf_n += 1 }
			fname := string(am.negotiated_feature_names[i][:fl])
			for c in fname {
				if nf_n >= len(nf) do break
				nf[nf_n] = u8(c)
				nf_n += 1
			}
		}
		append_line(buf[:], &n, nf[:], nf_n)
	}

	append_str(buf[:], &n, "\nSERVER LIMITS:\n")
	lsub := "n/a"
	if am.server_max_subscriptions > 0 {
		lsub_buf: [16]u8
		lsub = fmt.bprintf(lsub_buf[:], "%d", am.server_max_subscriptions)
	}
	lframe := "n/a"
	if am.server_max_frame_bytes > 0 {
		lframe_buf: [24]u8
		lframe = fmt.bprintf(lframe_buf[:], "%dKB", max(am.server_max_frame_bytes, 0) / 1024)
	}
	lcadence := "n/a"
	if am.server_metrics_cadence_ms > 0 {
		lcadence_buf: [24]u8
		lcadence = fmt.bprintf(lcadence_buf[:], "%dms", am.server_metrics_cadence_ms)
	}
	sl: [128]u8
	sl_len := len(fmt.bprintf(sl[:], "  subs=%s frame=%s metrics_cadence=%s", lsub, lframe, lcadence))
	append_line(buf[:], &n, sl[:], sl_len)

	// Server metrics
	if am.server_ws_queue_len > 0 || am.server_ws_dropped > 0 || am.server_ws_lag_ms > 0 ||
		am.server_serialize_errors > 0 || am.server_resync_total > 0 || am.server_pub_deliver_ms > 0 {
		append_str(buf[:], &n, "\nSERVER METRICS:\n")
		s1: [96]u8
		s1_len := len(fmt.bprintf(s1[:], "  queue=%d dropped=%d lag=%dms p2d=%dms ser_err=%d resyncs=%d",
			max(am.server_ws_queue_len, 0), max(am.server_ws_dropped, 0),
			max(am.server_ws_lag_ms, 0), max(am.server_pub_deliver_ms, 0),
			max(am.server_serialize_errors, 0), max(am.server_resync_total, 0)))
		append_line(buf[:], &n, s1[:], s1_len)
	}

	// Backpressure
	if am.server_backpressure_level > 0 || am.server_queue_capacity > 0 {
		append_str(buf[:], &n, "\nBACKPRESSURE:\n")
		bp: [96]u8
		bp_len := len(fmt.bprintf(bp[:], "  level=%d queue=%d/%d high_watermark=%d",
			max(am.server_backpressure_level, 0),
			max(am.server_ws_queue_len, 0), max(am.server_queue_capacity, 0),
			max(am.server_queue_high_watermark, 0)))
		append_line(buf[:], &n, bp[:], bp_len)
		if am.server_recommended_action_len > 0 {
			bpa: [96]u8
			bpa_len := len(fmt.bprintf(
				bpa[:],
				"  recommended_action=%s",
				string(am.server_recommended_action[:int(am.server_recommended_action_len)]),
			))
			append_line(buf[:], &n, bpa[:], bpa_len)
		}
	}

	// Evidence entries (last 16).
	if state.evidence.count > 0 {
		append_str(buf[:], &n, "\nEVIDENCE (recent):\n")
		ev_show := min(state.evidence.count, 16)
		for ei in 0 ..< ev_show {
			ev_idx := (state.evidence.head - 1 - ei + EVIDENCE_HISTORY_CAP) % EVIDENCE_HISTORY_CAP
			ev := state.evidence.entries[ev_idx]
			ev_kind := "?"
			if ev.kind_len > 0 {
				ev_kind = string(ev.kind[:int(ev.kind_len)])
			}
			el: [160]u8
			fc := min(ev.feature_count, len(ev.feature_tags))
			if fc > 0 {
				// Include first feature tag=val pair.
				tl := 0
				for ti in 0 ..< len(ev.feature_tags[0]) {
					if ev.feature_tags[0][ti] == 0 do break
					tl += 1
				}
				tag := ""
				if tl > 0 do tag = string(ev.feature_tags[0][:tl])
				el_len := len(fmt.bprintf(el[:], "  %s c=%.2f %s=%.1f ts=%d", ev_kind, ev.confidence, tag, ev.feature_vals[0], ev.unix))
				append_line(buf[:], &n, el[:], el_len)
			} else {
				el_len := len(fmt.bprintf(el[:], "  %s c=%.2f ts=%d", ev_kind, ev.confidence, ev.unix))
				append_line(buf[:], &n, el[:], el_len)
			}
		}
	}

	append_str(buf[:], &n, "\nBACKEND GAPS DETECTED:\n")
	g1: [128]u8
	g1_len := len(fmt.bprintf(g1[:], "  no_metrics=%d pong_timeout=%d resync_ack_timeout=%d",
		max(am.backend_gap_no_metrics, 0), max(am.backend_gap_pong_timeout, 0), max(am.backend_gap_resync_ack_timeout, 0)))
	append_line(buf[:], &n, g1[:], g1_len)
	g2: [128]u8
	g2_len := len(fmt.bprintf(g2[:], "  missing_ts_server=%d recurring_seq_gaps=%d frequent_drops=%d",
		max(am.backend_gap_missing_ts_server, 0), max(am.backend_gap_seq_gap_recurring, 0), max(am.backend_gap_frequent_drops, 0)))
	append_line(buf[:], &n, g2[:], g2_len)

	// Recent log
	append_str(buf[:], &n, "\nLOG (recent):\n")
	log_count := services.log_count(&state.log_state.buf)
	show := min(log_count, 20)
	for li in 0 ..< show {
		entry, ok := services.log_get(&state.log_state.buf, li)
		if !ok do break
		level_prefix := "I"
		switch entry.level {
		case .WARN: level_prefix = "W"
		case .ERR:  level_prefix = "E"
		case .INFO:
		}
		line: [140]u8
		entry_text := string(entry.text[:entry.text_len])
		line_len := len(fmt.bprintf(line[:], "  [%s] %s", level_prefix, entry_text))
		append_line(buf[:], &n, line[:], line_len)
	}

	if n > 0 {
		services.settings_clipboard_write(&state.settings, string(buf[:n]))
	}
}
