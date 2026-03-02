package streams

Stream_Controller :: struct {
	lag_warn_ms:         i64,
	desync_stale_ms:     i64,
	clock_drift_warn_ms: i64,
	backoff_initial_ms:  int,
	backoff_max_ms:      int,
	backoff_current_ms:  int,
}

controller_init :: proc(ctrl: ^Stream_Controller) {
	if ctrl == nil do return
	ctrl^ = {
		lag_warn_ms = 4_000,
		desync_stale_ms = 12_000,
		clock_drift_warn_ms = 8_000,
		backoff_initial_ms = 500,
		backoff_max_ms = 30_000,
		backoff_current_ms = 500,
	}
}

controller_next_backoff_ms :: proc(ctrl: ^Stream_Controller) -> int {
	if ctrl == nil do return 1_000
	next := ctrl.backoff_current_ms
	if next <= 0 do next = ctrl.backoff_initial_ms
	ctrl.backoff_current_ms = min(max(next * 2, ctrl.backoff_initial_ms), ctrl.backoff_max_ms)
	return next
}

controller_reset_backoff :: proc(ctrl: ^Stream_Controller) {
	if ctrl == nil do return
	ctrl.backoff_current_ms = ctrl.backoff_initial_ms
}

controller_mark_connected :: proc(status: ^Stream_Status, connected: bool) {
	if status == nil do return
	status.connected = connected
	if !connected {
		status.state = .Offline
	}
}

controller_mark_ack :: proc(status: ^Stream_Status) {
	if status == nil do return
	status.subscribe_acks += 1
}

controller_mark_message :: proc(
	status: ^Stream_Status,
	local_ts_ms: i64,
	server_ts_ms: i64,
	seq: i64,
	is_snapshot: bool,
) {
	if status == nil do return
	status.connected = true
	if seq > 0 {
		if status.last_seq > 0 && (seq > status.last_seq + 1 || seq < status.last_seq) {
			status.desync_reason = .Sequence_Gap
			status.state = .Desync
		}
		status.last_seq = seq
	}
	if local_ts_ms > 0 do status.last_local_ts_ms = local_ts_ms
	if server_ts_ms > 0 {
		if status.last_server_ts_ms > 0 && server_ts_ms < status.last_server_ts_ms {
			status.desync_reason = .Protocol_Invalid
			status.state = .Desync
		}
		status.last_server_ts_ms = server_ts_ms
	}
	if is_snapshot && local_ts_ms > 0 {
		status.last_snapshot_ts_ms = local_ts_ms
	}
	if server_ts_ms > 0 && local_ts_ms >= server_ts_ms {
		status.lag_ms = local_ts_ms - server_ts_ms
	}
}

controller_mark_transport_metrics :: proc(status: ^Stream_Status, drop_count: int, reconnect_count: int, rtt_ms: i64) {
	if status == nil do return
	status.drop_count = drop_count
	status.reconnect_count = reconnect_count
	if rtt_ms > 0 do status.rtt_ms = rtt_ms
}

controller_mark_desync :: proc(status: ^Stream_Status, reason: Stream_Desync_Reason) {
	if status == nil do return
	status.desync_reason = reason
	status.state = .Desync
}

controller_clear_desync :: proc(status: ^Stream_Status) {
	if status == nil do return
	status.desync_reason = .None
	if status.connected {
		status.state = .Live
	} else {
		status.state = .Offline
	}
}

controller_update_health :: proc(ctrl: ^Stream_Controller, status: ^Stream_Status, now_ms: i64) -> Stream_State {
	if status == nil do return .Offline
	if !status.connected {
		status.state = .Offline
		return status.state
	}
	if now_ms > 0 && status.last_local_ts_ms > 0 {
		age := now_ms - status.last_local_ts_ms
		if age < 0 do age = 0
		status.last_message_age_ms = age
	}
	if status.desync_reason != .None {
		status.state = .Desync
		return status.state
	}
	if now_ms > 0 && status.last_snapshot_ts_ms > 0 {
		snapshot_age := now_ms - status.last_snapshot_ts_ms
		if snapshot_age > ctrl.desync_stale_ms {
			status.desync_reason = .Snapshot_Stale
			status.state = .Desync
			return status.state
		}
	}
	if status.lag_ms > ctrl.clock_drift_warn_ms {
		status.desync_reason = .Clock_Drift
		status.state = .Desync
		return status.state
	}
	if status.last_message_age_ms > ctrl.lag_warn_ms {
		status.state = .Lag
		return status.state
	}
	status.state = .Live
	return status.state
}
