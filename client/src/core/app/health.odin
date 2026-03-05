package app

import "core:fmt"
import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"

@(private = "file")
clamp_nonneg_i64 :: proc(v: i64) -> i64 {
	if v < 0 do return 0
	return v
}

@(private = "file")
md_desync_reason_to_stream :: proc(reason: ports.MD_Desync_Reason) -> streams.Stream_Desync_Reason {
	switch reason {
	case .Sequence_Gap:
		return .Sequence_Gap
	case .Snapshot_Gap:
		return .Snapshot_Gap
	case .Protocol_Version:
		return .Protocol_Version
	case .Protocol_Invalid:
		return .Protocol_Invalid
	case .Missing_Hello:
		return .Missing_Hello
	case .Resync_Required:
		return .Resync_Required
	case .None:
	}
	return .Sequence_Gap
}

// Parameterized candle health computation — accepts store pointer and timing params
// so it can be used for any cell's store, not just the global active store.
compute_candle_health_for_store :: proc(
	store: ^services.Candle_Store,
	last_recv_ms: i64,
	tf_ms: i64,
	now_ms: i64,
) -> Candle_Health {
	if store == nil || store.count <= 0 do return .No_Data
	if now_ms <= 0 do return .OK

	latest := services.get_candle_newest(store, 0)
	end_lag_ms := clamp_nonneg_i64(now_ms - latest.window_end_ts)
	recv_age_ms := clamp_nonneg_i64(now_ms - last_recv_ms)

	// TF-adaptive thresholds.
	lag_warn_closed  := max(2 * tf_ms, 5_000)
	lag_stale_closed := max(3 * tf_ms, 10_000)
	lag_warn_open    := max(tf_ms + tf_ms / 2, 5_000)
	lag_stale_open   := max(2 * tf_ms, 10_000)
	silence_warn     := max(tf_ms / 3, 5_000)
	silence_stale    := max(tf_ms, 10_000)

	if latest.is_closed {
		if end_lag_ms >= lag_stale_closed && recv_age_ms >= silence_stale do return .Stale
		if end_lag_ms >= lag_warn_closed && recv_age_ms >= silence_warn do return .Lagging
		return .OK
	}

	if recv_age_ms >= silence_stale || end_lag_ms >= lag_stale_open do return .Stale
	if recv_age_ms >= silence_warn || end_lag_ms >= lag_warn_open do return .Lagging
	return .OK
}

// Global convenience: delegates to parameterized version using active stream state.
compute_candle_health :: proc(state: ^App_State) -> Candle_Health {
	tf_options := TF_OPTION_MS
	tf_ms: i64 = 60_000
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_options) {
		tf_ms = tf_options[state.active_tf_idx]
	}
	return compute_candle_health_for_store(
		&state.stores.candle,
		state.candle_last_recv_local_ms,
		tf_ms,
		current_now_ms(state),
	)
}

observe_candle_health :: proc(state: ^App_State) -> bool {
	next := compute_candle_health(state)
	if next == state.candle_health do return false
	state.candle_health = next
	return true
}

format_ms_short_into :: proc(buf: []u8, ms: i64) -> string {
	v := ms
	if v < 0 do v = 0
	if v < 1000 do return fmt.bprintf(buf, "%dms", v)
	sec := v / 1000
	if sec < 60 do return fmt.bprintf(buf, "%ds", sec)
	mins := sec / 60
	secs := sec % 60
	return fmt.bprintf(buf, "%dm%02ds", mins, secs)
}

// Parameterized candle health UI — accepts any store + timing params.
build_candle_health_ui_for_store :: proc(
	store: ^services.Candle_Store,
	last_recv_ms: i64,
	tf_ms: i64,
	now_ms: i64,
) -> (label: string, detail: string, color: ui.Color) {
	if store == nil || store.count <= 0 {
		return "NO DATA", "waiting for first candle", ui.with_alpha(ui.COL_WHITE, 0.6)
	}
	health := compute_candle_health_for_store(store, last_recv_ms, tf_ms, now_ms)
	latest := services.get_candle_newest(store, 0)
	end_lag_ms := i64(0)
	recv_age_ms := i64(0)
	if now_ms > 0 {
		end_lag_ms = clamp_nonneg_i64(now_ms - latest.window_end_ts)
		recv_age_ms = clamp_nonneg_i64(now_ms - last_recv_ms)
	}
	status_str := "OK"
	status_color := ui.COL_GREEN
	switch health {
	case .Lagging:
		status_str = "LAG"
		status_color = ui.COL_YELLOW_ACCENT
	case .Stale:
		status_str = "STALE"
		status_color = ui.COL_RED
	case .No_Data:
		status_str = "NO DATA"
		status_color = ui.with_alpha(ui.COL_WHITE, 0.6)
	case .OK:
	}
	phase := latest.is_closed ? "closed" : "open"
	recv_buf: [16]u8
	lag_buf: [16]u8
	detail_buf: [64]u8
	recv_str := format_ms_short_into(recv_buf[:], recv_age_ms)
	lag_str := format_ms_short_into(lag_buf[:], end_lag_ms)
	return status_str,
		fmt.bprintf(detail_buf[:], "%s recv=%s endlag=%s", phase, recv_str, lag_str),
		status_color
}

// Global convenience: delegates to parameterized version using active stream state.
build_candle_health_ui :: proc(state: ^App_State) -> (label: string, detail: string, color: ui.Color) {
	tf_options := TF_OPTION_MS
	tf_ms: i64 = 60_000
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_options) {
		tf_ms = tf_options[state.active_tf_idx]
	}
	return build_candle_health_ui_for_store(
		&state.stores.candle,
		state.candle_last_recv_local_ms,
		tf_ms,
		current_now_ms(state),
	)
}

sample_marketdata_metrics :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.metrics == nil do return
	m: ports.MD_Runtime_Metrics
	if !state.marketdata.metrics(&m) do return
	state.telemetry.metrics_history[state.telemetry.metrics_head] = MD_Metrics_History_Sample{
		frame    = state.frame,
		metrics  = m,
	}
	state.telemetry.metrics_head = (state.telemetry.metrics_head + 1) % MD_METRICS_HISTORY_CAP
	if state.telemetry.metrics_count < MD_METRICS_HISTORY_CAP {
		state.telemetry.metrics_count += 1
	}
	apply_backpressure_assist(state, m)
	refresh_active_stream_health(state, m)
}

@(private = "file")
apply_backpressure_assist :: proc(state: ^App_State, metrics: ports.MD_Runtime_Metrics) {
	if state == nil do return
	level := metrics.server_backpressure_level

	decision := md_common.compute_bp_assist_decision(md_common.BP_Assist_Input{
		prev_enabled          = state.bp_assist.enabled,
		prev_degrade_heatmap  = state.bp_assist.degrade_heatmap,
		prev_degrade_vpvr     = state.bp_assist.degrade_vpvr,
		prev_getrange_divisor = state.bp_assist.getrange_divisor,
		user_enabled          = state.bp_assist.user_enabled,
		cooldown_frames       = state.bp_assist.cooldown_frames,
		level                 = level,
	})

	state.bp_assist.enabled = decision.enabled
	state.bp_assist.degrade_heatmap = decision.degrade_heatmap
	state.bp_assist.degrade_vpvr = decision.degrade_vpvr
	state.bp_assist.getrange_divisor = decision.getrange_divisor
	state.bp_assist.cooldown_frames = decision.cooldown_frames
	state.bp_assist.recommended_action_pending = level >= 2 && !state.bp_assist.user_enabled

	reason := "none"
	if metrics.server_recommended_action_len > 0 {
		action := metrics.server_recommended_action
		reason = string(action[:int(metrics.server_recommended_action_len)])
	} else if level >= 3 {
		reason = "reconnect"
	} else if level >= 2 {
		reason = "reduce_subscriptions"
	}
	rn := min(len(reason), len(state.bp_assist.reason))
	for i in 0 ..< rn {
		state.bp_assist.reason[i] = reason[i]
	}
	state.bp_assist.reason_len = u8(rn)

	if decision.auto_activated {
		fmt.printf("[md-assist] auto_activate level=%d reason=%s\n", level, reason)
	}
	if decision.changed {
		fmt.printf("[md-assist] transition enabled=%v heatmap=%v vpvr=%v div=%d level=%d reason=%s\n",
			decision.enabled, decision.degrade_heatmap, decision.degrade_vpvr,
			decision.getrange_divisor, level, reason)
		reconcile_subscriptions(state)
		show_toast(state, decision.enabled ? "Assist: backpressure protection ON" : "Assist: backpressure protection OFF")
	}
}

@(private = "package")
apply_backpressure_recommendation :: proc(state: ^App_State) {
	if state == nil do return
	level := state.active_metrics.server_backpressure_level
	if level < 2 {
		state.bp_assist.recommended_action_pending = false
		return
	}
	state.bp_assist.user_enabled = true
	state.bp_assist.enabled = true
	state.bp_assist.degrade_heatmap = true
	state.bp_assist.degrade_vpvr = level >= 3
	state.bp_assist.getrange_divisor = level >= 3 ? 4 : 2
	state.bp_assist.recommended_action_pending = false
	state.bp_assist.cooldown_frames = 120
	reconcile_subscriptions(state)
	services.settings_set(&state.settings, services.SETTING_ASSIST_MODE, "1")
	services.settings_flush(&state.settings)
	show_toast(state, "Assist: recommendation applied")
}

refresh_active_stream_health :: proc(state: ^App_State, metrics: ports.MD_Runtime_Metrics) {
	if state == nil do return
	raw_subscribe_acks := max(metrics.subscribe_ack_count, 0)
	prev_ack_metric := state.active_metrics.last_ack_metric
	ack_metric_reset := raw_subscribe_acks < prev_ack_metric
	ack_delta := raw_subscribe_acks - prev_ack_metric
	if ack_delta < 0 do ack_delta = raw_subscribe_acks
	state.active_metrics.last_ack_metric = raw_subscribe_acks

	// Copy Terminal_V1 protocol + server-pushed metrics (independent of active stream).
	state.active_metrics.transport_mode = metrics.transport_mode
	state.active_metrics.auth_mode = metrics.auth_mode
	state.active_metrics.protocol_version = metrics.protocol_version
	state.active_metrics.server_instance_id = metrics.server_instance_id
	state.active_metrics.server_instance_id_len = metrics.server_instance_id_len
	state.active_metrics.server_instance_id_hash = metrics.server_instance_id_hash
	state.active_metrics.hello_timeout_count = metrics.hello_timeout_count
	state.active_metrics.pong_rtt_ms = metrics.pong_rtt_ms
	state.active_metrics.active_subs = metrics.active_subs
	state.active_metrics.transport_state = metrics.transport_state
	state.active_metrics.ws_error_category = metrics.ws_error_category
	state.active_metrics.ws_error_action = metrics.ws_error_action
	state.active_metrics.last_server_ts_ms = metrics.last_server_ts_ms
	state.active_metrics.seq_gap_count = metrics.seq_gap_count
	state.active_metrics.resync_count = metrics.resync_count + state.manual_resync_count
	state.active_metrics.server_ws_dropped = metrics.server_ws_dropped
	state.active_metrics.server_ws_queue_len = metrics.server_ws_queue_len
	state.active_metrics.server_ws_lag_ms = metrics.server_ws_lag_ms
	state.active_metrics.server_serialize_errors = metrics.server_serialize_errors
	state.active_metrics.server_resync_total = metrics.server_resync_total
	state.active_metrics.server_pub_deliver_ms = metrics.server_pub_deliver_ms
	prev_alloc_estimate := state.active_metrics.alloc_estimate_total
	state.active_metrics.drop_trade_ring = metrics.drop_trade_ring
	state.active_metrics.drop_candle_ring = metrics.drop_candle_ring
	state.active_metrics.drop_ws_queue = metrics.drop_ws_queue
	state.active_metrics.drop_payload_oversize = metrics.drop_payload_oversize
	state.active_metrics.alloc_estimate_total = metrics.alloc_estimate_total
	state.active_metrics.alloc_estimate_frame = i64(metrics.alloc_estimate_total) - i64(prev_alloc_estimate)
	if state.active_metrics.alloc_estimate_frame < 0 {
		state.active_metrics.alloc_estimate_frame = i64(metrics.alloc_estimate_total)
	}
	state.active_metrics.parse_time_p95_us = metrics.parse_time_p95_us
	state.active_metrics.apply_time_p95_us = metrics.apply_time_p95_us
	state.active_metrics.batched_decode_time_p95_us = metrics.batched_decode_time_p95_us
	state.active_metrics.backend_gap_no_metrics = metrics.backend_gap_no_metrics
	state.active_metrics.backend_gap_pong_timeout = metrics.backend_gap_pong_timeout
	state.active_metrics.backend_gap_resync_ack_timeout = metrics.backend_gap_resync_ack_timeout
	state.active_metrics.backend_gap_missing_ts_server = metrics.backend_gap_missing_ts_server
	state.active_metrics.backend_gap_seq_gap_recurring = metrics.backend_gap_seq_gap_recurring
	state.active_metrics.backend_gap_frequent_drops = metrics.backend_gap_frequent_drops
	// Capability + backpressure + integrity fields.
	state.active_metrics.server_max_subscriptions = metrics.server_max_subscriptions
	state.active_metrics.server_max_frame_bytes = metrics.server_max_frame_bytes
	state.active_metrics.server_metrics_cadence_ms = metrics.server_metrics_cadence_ms
	state.active_metrics.server_keepalive_interval_ms = metrics.server_keepalive_interval_ms
	state.active_metrics.server_rate_limit_enabled = metrics.server_rate_limit_enabled
	state.active_metrics.server_backpressure_level = metrics.server_backpressure_level
	state.active_metrics.server_queue_capacity = metrics.server_queue_capacity
	state.active_metrics.server_queue_high_watermark = metrics.server_queue_high_watermark
	state.active_metrics.server_recommended_action = metrics.server_recommended_action
	state.active_metrics.server_recommended_action_len = metrics.server_recommended_action_len
	state.active_metrics.negotiated_feature_count = metrics.negotiated_feature_count
	state.active_metrics.negotiated_feature_names = metrics.negotiated_feature_names
	state.active_metrics.negotiated_feature_name_lens = metrics.negotiated_feature_name_lens
	state.active_metrics.batched_frames_received = metrics.batched_frames_received
	state.active_metrics.batched_events_received = metrics.batched_events_received
	state.active_metrics.batched_fastpath_events = metrics.batched_fastpath_events
	state.active_metrics.batched_fallback_events = metrics.batched_fallback_events
	state.active_metrics.canonical_evidence_frames = metrics.canonical_evidence_frames
	state.active_metrics.legacy_evidence_frames = metrics.legacy_evidence_frames
	state.active_metrics.evidence_fallback_frames = metrics.evidence_fallback_frames
	state.active_metrics.canonical_signal_frames = metrics.canonical_signal_frames
	state.active_metrics.legacy_signal_frames = metrics.legacy_signal_frames
	state.active_metrics.signal_fallback_frames = metrics.signal_fallback_frames
	state.active_metrics.legacy_evidence_rejected = metrics.legacy_evidence_rejected
	state.active_metrics.legacy_signal_rejected = metrics.legacy_signal_rejected
	state.active_metrics.snapshot_hash_mismatches = metrics.snapshot_hash_mismatches
	state.active_metrics.snapshot_seq_violations = metrics.snapshot_seq_violations
	state.active_metrics.prev_seq_violations = metrics.prev_seq_violations
	state.active_metrics.hash_validation_skipped = metrics.hash_validation_skipped
	state.active_metrics.legacy_downgrade_count = metrics.legacy_downgrade_count
	state.active_metrics.legacy_connected_since_ms = metrics.legacy_connected_since_ms
	state.active_metrics.assist_enabled = state.bp_assist.enabled
	state.active_metrics.assist_degrade_heatmap = state.bp_assist.degrade_heatmap
	state.active_metrics.assist_degrade_vpvr = state.bp_assist.degrade_vpvr
	state.active_metrics.assist_getrange_divisor = max(state.bp_assist.getrange_divisor, 1)
	state.active_metrics.assist_reason = state.bp_assist.reason
	state.active_metrics.assist_reason_len = state.bp_assist.reason_len
	state.active_metrics.assist_user_enabled = state.bp_assist.user_enabled

	active := streams.registry_active(&state.stream_registry)
	if active == nil {
		state.active_metrics.state = current_conn_status(state) == .Connected ? .Lag : .Offline
		state.active_metrics.desync_reason = .None
		state.active_metrics.rtt_ms = metrics.rtt_ms
		state.active_metrics.lag_ms = metrics.lag_ms
		state.active_metrics.last_msg_ts_ms = metrics.last_msg_ts_ms
		state.active_metrics.drop_count = metrics.drop_count
		state.active_metrics.reconnect_count = metrics.reconnect_count
		state.active_metrics.subscribe_acks = raw_subscribe_acks
		state.active_metrics.candle_backlog = metrics.candle_backlog
		state.active_metrics.msg_rate = metrics.msg_rate
		state.active_metrics.bytes_rate = metrics.bytes_rate
		state.active_metrics.parsed_msgs_total = metrics.parsed_msgs_total
		state.active_metrics.parsed_bytes_total = metrics.parsed_bytes_total
		state.active_metrics.parse_arena_resets = metrics.parse_arena_resets
		return
	}

	streams.controller_mark_connected(&active.status, current_conn_status(state) == .Connected)
	streams.controller_mark_transport_metrics(&active.status, metrics.drop_count, metrics.reconnect_count, metrics.rtt_ms)
	if ack_metric_reset {
		active.status.subscribe_acks = 0
	}
	if ack_delta > 0 {
		active.status.subscribe_acks += ack_delta
	}
	if metrics.desync {
		reason := md_desync_reason_to_stream(metrics.desync_reason)
		// Don't forward seq_gap or protocol_invalid — the stream controller
		// already detects these per-event with proper tolerance for multi-replica.
		#partial switch reason {
		case .Sequence_Gap, .Protocol_Invalid:
			// Handled by controller_mark_message with ±10 / 5s tolerance.
		case:
			streams.controller_mark_desync(&active.status, reason)
		}
	}
	now_ms := current_now_ms(state)
	if now_ms <= 0 do now_ms = metrics.last_msg_ts_ms
	state.active_metrics.state = streams.controller_update_health(&state.stream_controller, &active.status, now_ms)
	state.active_metrics.desync_reason = active.status.desync_reason
	state.active_metrics.rtt_ms = active.status.rtt_ms
	state.active_metrics.lag_ms = active.status.lag_ms
	state.active_metrics.last_msg_ts_ms = active.status.last_local_ts_ms
	state.active_metrics.drop_count = active.status.drop_count
	state.active_metrics.reconnect_count = active.status.reconnect_count
	state.active_metrics.subscribe_acks = active.status.subscribe_acks
	state.active_metrics.candle_backlog = metrics.candle_backlog
	state.active_metrics.msg_rate = metrics.msg_rate
	state.active_metrics.bytes_rate = metrics.bytes_rate
	state.active_metrics.parsed_msgs_total = metrics.parsed_msgs_total
	state.active_metrics.parsed_bytes_total = metrics.parsed_bytes_total
	state.active_metrics.parse_arena_resets = metrics.parse_arena_resets

	// Additional client-side DESYNC detection: only escalate when both stats AND
	// orderbook were previously active (ts > 0) and have gone silent. If they
	// haven't arrived yet (ts == 0), the status bar already shows informational
	// "stats pending" / "snapshot pending" messages — no need for hard DESYNC.
	// Also guard with stream event age: if any events are flowing, skip this check.
	if now_ms > 0 && current_conn_status(state) == .Connected {
		stats_age := i64(0)
		if state.active_metrics.last_stats_ts_ms > 0 {
			stats_age = now_ms - state.active_metrics.last_stats_ts_ms
		}
		ob_age := i64(0)
		if state.active_metrics.last_orderbook_ts_ms > 0 {
			ob_age = now_ms - state.active_metrics.last_orderbook_ts_ms
		}
		stream_event_age := now_ms - active.status.last_local_ts_ms
		if state.active_metrics.last_stats_ts_ms > 0 && stats_age > 12_000 &&
			state.active_metrics.last_orderbook_ts_ms > 0 && ob_age > 12_000 &&
			stream_event_age > 12_000 &&
			state.active_metrics.state != .Offline {
			streams.controller_mark_desync(&active.status, .Snapshot_Stale)
			state.active_metrics.state = .Desync
			state.active_metrics.desync_reason = .Snapshot_Stale
			record_error(state, .Connection, "DESYNC: snapshot stale")
		}
	}
}

metrics_history_summary :: proc(state: ^App_State) -> (ok: bool, qmax: int, drop_delta: int, rc_delta: int) {
	if state == nil do return false, 0, 0, 0
	if state.telemetry.metrics_count <= 0 do return false, 0, 0, 0

	oldest_idx := (state.telemetry.metrics_head - state.telemetry.metrics_count + MD_METRICS_HISTORY_CAP) % MD_METRICS_HISTORY_CAP
	newest_idx := (state.telemetry.metrics_head - 1 + MD_METRICS_HISTORY_CAP) % MD_METRICS_HISTORY_CAP
	oldest := state.telemetry.metrics_history[oldest_idx].metrics
	newest := state.telemetry.metrics_history[newest_idx].metrics

	qmax = 0
	for i in 0 ..< state.telemetry.metrics_count {
		idx := (oldest_idx + i) % MD_METRICS_HISTORY_CAP
		qmax = max(qmax, state.telemetry.metrics_history[idx].metrics.trade_backlog)
	}
	drop_delta = newest.drop_count - oldest.drop_count
	rc_delta = newest.reconnect_count - oldest.reconnect_count
	if drop_delta < 0 do drop_delta = 0
	if rc_delta < 0 do rc_delta = 0
	return true, qmax, drop_delta, rc_delta
}
