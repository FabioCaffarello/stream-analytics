package app

import "mr:ports"
import "mr:streams"

apply_pick_stream_action :: proc(state: ^App_State, subject_id: u64) {
	if state == nil || subject_id == 0 do return
	reg := state.stream_views
	if reg == nil do return

	reg.active_subject_id = subject_id
	reg.has_active = true
	sync_active_stream_view_to_global_stores(state)
	persist_active_stream_subject(state)
	// S25: Canonical apply state sync from new slot drives getrange state.
	// S34: getrange_request_id cleared by sync_active_apply_state_from_slot.
	sync_active_apply_state_from_slot(state)
	ensure_active_candle_subject_id(state)
	state.candle_health = .No_Data
	state.stream_switches_total += 1
	if state.stores.candle.count <= 0 {
		request_active_stream_candle_range(state)
	}
}

apply_resync_active_stream_action :: proc(state: ^App_State) {
	if state == nil do return
	state.manual_resync_count += 1
	current_ack_metric := 0
	if state.marketdata.metrics != nil {
		metrics: ports.MD_Runtime_Metrics
		if state.marketdata.metrics(&metrics) {
			current_ack_metric = max(metrics.subscribe_ack_count, 0)
		}
	}
	state.active_metrics.last_ack_metric = current_ack_metric
	state.active_metrics.subscribe_acks = 0
	// S25: Canonical apply state reset (zeros apply_state + syncs to metrics + getrange).
	// S34: getrange_request_id cleared by apply_state_reset.
	reset_active_apply_state(state)
	ensure_active_candle_subject_id(state)
	if active := streams.registry_active(&state.stream_registry); active != nil {
		streams.controller_mark_desync(&active.status, .Manual)
		active.status.last_snapshot_ts_ms = 0
		active.status.last_local_ts_ms = 0
		active.status.last_server_ts_ms = 0
		active.status.last_message_age_ms = 0
		active.status.last_seq = 0
		active.status.subscribe_acks = 0
	}
	state.active_metrics.state = .Desync
	state.active_metrics.desync_reason = .Manual
	state.prev_subs_count = 0 // force full re-subscribe path
	reconcile_subscriptions(state)
	request_active_stream_candle_range(state)
	show_toast(state, "Resync requested")
}
