package app

import "core:fmt"
import "mr:ports"
import "mr:services"
import "mr:streams"
import "mr:ui"

@(private = "file")
clamp_nonneg_i64 :: proc(v: i64) -> i64 {
	if v < 0 do return 0
	return v
}

compute_candle_health :: proc(state: ^App_State) -> Candle_Health {
	if state.candle_store.count <= 0 do return .No_Data

	now_ms := current_now_ms(state)
	if now_ms <= 0 do return .OK

	latest := services.get_candle_newest(&state.candle_store, 0)
	end_lag_ms := clamp_nonneg_i64(now_ms - latest.window_end_ts)
	recv_age_ms := clamp_nonneg_i64(now_ms - state.candle_last_recv_local_ms)

	// TF-adaptive thresholds: scale by active timeframe instead of fixed 1m constants.
	tf_options := TF_OPTION_MS
	tf_ms: i64 = 60_000
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_options) {
		tf_ms = tf_options[state.active_tf_idx]
	}
	lag_warn_closed  := max(2 * tf_ms, 5_000)
	lag_stale_closed := max(3 * tf_ms, 10_000)
	lag_warn_open    := max(tf_ms + tf_ms / 2, 5_000)
	lag_stale_open   := max(2 * tf_ms, 10_000)
	silence_warn     := max(tf_ms / 3, 5_000)
	silence_stale    := max(tf_ms, 10_000)

	if latest.is_closed {
		// Require both end_lag AND recv_age to exceed thresholds — prevents false
		// STALE/LAG when GetRange delivers historical candles with old window_end_ts
		// but trades are still flowing (keeping recv_age fresh).
		if end_lag_ms >= lag_stale_closed && recv_age_ms >= silence_stale do return .Stale
		if end_lag_ms >= lag_warn_closed && recv_age_ms >= silence_warn do return .Lagging
		return .OK
	}

	if recv_age_ms >= silence_stale || end_lag_ms >= lag_stale_open do return .Stale
	if recv_age_ms >= silence_warn || end_lag_ms >= lag_warn_open do return .Lagging
	return .OK
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

build_candle_health_ui :: proc(state: ^App_State) -> (label: string, detail: string, color: ui.Color) {
	if state.candle_store.count <= 0 {
		return "NO DATA", "waiting for first candle", ui.with_alpha(ui.COL_WHITE, 0.6)
	}
	now_ms := current_now_ms(state)
	latest := services.get_candle_newest(&state.candle_store, 0)
	end_lag_ms := i64(0)
	recv_age_ms := i64(0)
	if now_ms > 0 {
		end_lag_ms = clamp_nonneg_i64(now_ms - latest.window_end_ts)
		recv_age_ms = clamp_nonneg_i64(now_ms - state.candle_last_recv_local_ms)
	}
	status_str := "OK"
	status_color := ui.COL_GREEN
	switch state.candle_health {
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

sample_marketdata_metrics :: proc(state: ^App_State) {
	if state == nil do return
	if state.marketdata.metrics == nil do return
	m: ports.MD_Runtime_Metrics
	if !state.marketdata.metrics(&m) do return
	state.md_metrics_history[state.md_metrics_head] = MD_Metrics_History_Sample{
		frame    = state.frame,
		metrics  = m,
	}
	state.md_metrics_head = (state.md_metrics_head + 1) % MD_METRICS_HISTORY_CAP
	if state.md_metrics_count < MD_METRICS_HISTORY_CAP {
		state.md_metrics_count += 1
	}
	refresh_active_stream_health(state, m)
}

refresh_active_stream_health :: proc(state: ^App_State, metrics: ports.MD_Runtime_Metrics) {
	if state == nil do return
	active := streams.registry_active(&state.stream_registry)
	if active == nil {
		state.active_stream_state = current_conn_status(state) == .Connected ? .Lag : .Offline
		state.active_stream_desync_reason = .None
		state.active_stream_rtt_ms = metrics.rtt_ms
		state.active_stream_lag_ms = metrics.lag_ms
		state.active_stream_last_msg_ts_ms = metrics.last_msg_ts_ms
		state.active_stream_drop_count = metrics.drop_count
		state.active_stream_reconnect_count = metrics.reconnect_count
		state.active_stream_subscribe_acks = metrics.subscribe_ack_count
		state.active_stream_candle_backlog = metrics.candle_backlog
		state.active_stream_msg_rate = metrics.msg_rate
		state.active_stream_bytes_rate = metrics.bytes_rate
		state.active_stream_parsed_msgs_total = metrics.parsed_msgs_total
		state.active_stream_parsed_bytes_total = metrics.parsed_bytes_total
		return
	}

	streams.controller_mark_connected(&active.status, current_conn_status(state) == .Connected)
	streams.controller_mark_transport_metrics(&active.status, metrics.drop_count, metrics.reconnect_count, metrics.rtt_ms)
	if metrics.desync {
		streams.controller_mark_desync(&active.status, .Sequence_Gap)
	}
	now_ms := current_now_ms(state)
	if now_ms <= 0 do now_ms = metrics.last_msg_ts_ms
	state.active_stream_state = streams.controller_update_health(&state.stream_controller, &active.status, now_ms)
	state.active_stream_desync_reason = active.status.desync_reason
	state.active_stream_rtt_ms = active.status.rtt_ms
	state.active_stream_lag_ms = active.status.lag_ms
	state.active_stream_last_msg_ts_ms = active.status.last_local_ts_ms
	state.active_stream_drop_count = active.status.drop_count
	state.active_stream_reconnect_count = active.status.reconnect_count
	state.active_stream_subscribe_acks = active.status.subscribe_acks
	state.active_stream_candle_backlog = metrics.candle_backlog
	state.active_stream_msg_rate = metrics.msg_rate
	state.active_stream_bytes_rate = metrics.bytes_rate
	state.active_stream_parsed_msgs_total = metrics.parsed_msgs_total
	state.active_stream_parsed_bytes_total = metrics.parsed_bytes_total

	// Additional client-side DESYNC detection for visible "Waiting for stats/orderbook" regressions.
	if now_ms > 0 && current_conn_status(state) == .Connected {
		stats_age := i64(0)
		if state.active_stream_last_stats_ts_ms > 0 {
			stats_age = now_ms - state.active_stream_last_stats_ts_ms
		}
		ob_age := i64(0)
		if state.active_stream_last_orderbook_ts_ms > 0 {
			ob_age = now_ms - state.active_stream_last_orderbook_ts_ms
		}
		if (state.active_stream_last_stats_ts_ms == 0 || stats_age > 12_000) &&
			(state.active_stream_last_orderbook_ts_ms == 0 || ob_age > 12_000) &&
			state.active_stream_state != .Offline {
			streams.controller_mark_desync(&active.status, .Snapshot_Stale)
			state.active_stream_state = .Desync
			state.active_stream_desync_reason = .Snapshot_Stale
		}
	}
}

metrics_history_summary :: proc(state: ^App_State) -> (ok: bool, qmax: int, drop_delta: int, rc_delta: int) {
	if state == nil do return false, 0, 0, 0
	if state.md_metrics_count <= 0 do return false, 0, 0, 0

	oldest_idx := (state.md_metrics_head - state.md_metrics_count + MD_METRICS_HISTORY_CAP) % MD_METRICS_HISTORY_CAP
	newest_idx := (state.md_metrics_head - 1 + MD_METRICS_HISTORY_CAP) % MD_METRICS_HISTORY_CAP
	oldest := state.md_metrics_history[oldest_idx].metrics
	newest := state.md_metrics_history[newest_idx].metrics

	qmax = 0
	for i in 0 ..< state.md_metrics_count {
		idx := (oldest_idx + i) % MD_METRICS_HISTORY_CAP
		qmax = max(qmax, state.md_metrics_history[idx].metrics.trade_backlog)
	}
	drop_delta = newest.drop_count - oldest.drop_count
	rc_delta = newest.reconnect_count - oldest.reconnect_count
	if drop_delta < 0 do drop_delta = 0
	if rc_delta < 0 do rc_delta = 0
	return true, qmax, drop_delta, rc_delta
}
