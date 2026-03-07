package app

import "mr:md_common"

// S24: Adapter layer — bridges Stream_Apply_State → Active_Stream_Metrics.
// S25: Extended to also sync getrange state from apply_state → GetRange_Global_State.
// Called once per frame after drain_marketdata. Widgets read active_metrics;
// the apply_state is the single source of truth (S24 cutover complete).

// Sync the canonical apply state into active_metrics has_live booleans,
// per-artifact timing, and composition stage. This is the ONLY writer
// of these fields (S24/S26, S32: extended to timing fields).
apply_state_sync_to_metrics :: proc(state: ^App_State) {
	if state == nil do return
	s := &state.active_apply_state
	state.active_metrics.has_live_stats   = s.has_live[.Stats]
	state.active_metrics.has_live_candle  = s.has_live[.Candle]
	state.active_metrics.has_live_heatmap = s.has_live[.Heatmap]
	state.active_metrics.has_live_vpvr    = s.has_live[.VPVR]
	// S32: Per-artifact timing synced from canonical source. Adapter is sole writer.
	state.active_metrics.last_stats_ts_ms     = s.last_recv_ms[.Stats]
	state.active_metrics.last_orderbook_ts_ms = s.last_recv_ms[.Orderbook]
	// S26: Composition stage is always derived, never manually written.
	state.active_metrics.context_stage = md_common.apply_state_composition_stage(s^)
}

// S25: Sync getrange state from apply_state → GetRange_Global_State.
// Called after apply_state changes. GetRange_Global_State becomes a
// derived view; the apply_state getrange fields are the source of truth.
// S34: subject_id now synced from canonical apply_state.getrange_request_id.
apply_state_sync_to_getrange :: proc(state: ^App_State) {
	if state == nil do return
	s := &state.active_apply_state
	state.getrange.pending = s.getrange_pending
	state.getrange.seeded = s.getrange_seeded
	state.getrange.oldest_ts = s.getrange_oldest_ts
	state.getrange.sent_frame = s.getrange_sent_frame
	state.getrange.active_candle_subject_id = s.range_candle_subject_id
	state.getrange.subject_id = s.getrange_request_id
}

// S25: Combined sync — metrics + getrange. Called at end of drain_marketdata frame.
apply_state_sync_all :: proc(state: ^App_State) {
	apply_state_sync_to_metrics(state)
	apply_state_sync_to_getrange(state)
}

// Reset the active apply state and sync to metrics.
// S32: Manual timing resets removed — adapter sync drives metrics from apply_state.
reset_active_apply_state :: proc(state: ^App_State) {
	if state == nil do return
	md_common.apply_state_reset(&state.active_apply_state)
	apply_state_sync_all(state)
}

// Apply reconnect policy to the active apply state.
// S35: Logs Reset event if recovery was in progress (attempts > 0).
reconnect_active_apply_state :: proc(state: ^App_State) {
	if state == nil do return
	prev_attempts := state.active_apply_state.recovery_attempts
	md_common.apply_state_on_reconnect(&state.active_apply_state)
	apply_state_sync_all(state)
	// S35: Emit Reset event so the recovery log has a complete audit trail.
	if prev_attempts > 0 {
		md_common.recovery_event_log_push(&state.recovery_log, md_common.Recovery_Event{
			kind = .Reset,
			timestamp = current_now_ms(state),
			attempts = prev_attempts,
			slot_id = u8(stream_view_find_slot(state.stream_views, state.stream_views.active_subject_id)),
		})
	}
}

// Apply TF change policy to the active apply state.
// S32: Manual timing resets removed — adapter sync drives metrics from apply_state.
// S35: Logs Reset event if recovery was in progress (attempts > 0).
tf_change_active_apply_state :: proc(state: ^App_State) {
	if state == nil do return
	prev_attempts := state.active_apply_state.recovery_attempts
	md_common.apply_state_on_tf_change(&state.active_apply_state)
	apply_state_sync_all(state)
	// S35: Emit Reset event so the recovery log has a complete audit trail.
	if prev_attempts > 0 {
		md_common.recovery_event_log_push(&state.recovery_log, md_common.Recovery_Event{
			kind = .Reset,
			timestamp = current_now_ms(state),
			attempts = prev_attempts,
			slot_id = u8(stream_view_find_slot(state.stream_views, state.stream_views.active_subject_id)),
		})
	}
}

// Sync the active slot's apply_state into the global active_apply_state.
// Called when the active stream changes (Tab, Pick_Stream).
sync_active_apply_state_from_slot :: proc(state: ^App_State) {
	if state == nil do return
	reg := state.stream_views
	if reg == nil || !reg.has_active do return
	idx := stream_view_find_slot(reg, reg.active_subject_id)
	if idx < 0 do return
	state.active_apply_state = reg.slots[idx].apply_state
	apply_state_sync_all(state)
}

// S30: Write recovery state from active_apply_state back to the active slot.
// Called after recovery mutations in health.odin to ensure per-stream isolation:
// when switching streams and back, recovery progress is preserved in the slot.
sync_recovery_to_active_slot :: proc(state: ^App_State) {
	if state == nil do return
	reg := state.stream_views
	if reg == nil || !reg.has_active do return
	idx := stream_view_find_slot(reg, reg.active_subject_id)
	if idx < 0 do return
	reg.slots[idx].apply_state.recovery_attempts = state.active_apply_state.recovery_attempts
	reg.slots[idx].apply_state.recovery_last_ms = state.active_apply_state.recovery_last_ms
}

// S31: Compute aggregate health across all active stream view slots.
// Pure derived view — reads slot apply_state, creates no new state.
compute_aggregate_health :: proc(state: ^App_State) -> md_common.Aggregate_Health_Summary {
	if state == nil do return {}
	reg := state.stream_views
	if reg == nil do return {}

	states: [STREAM_VIEW_CAP]md_common.Stream_Apply_State
	used: [STREAM_VIEW_CAP]bool
	for i in 0 ..< STREAM_VIEW_CAP {
		if reg.slots[i].used {
			states[i] = reg.slots[i].apply_state
			used[i] = true
		}
	}

	now_ms := current_now_ms(state)
	tf_options := TF_OPTION_MS
	tf_ms := i64(60_000)
	if state.active_tf_idx >= 0 && state.active_tf_idx < len(tf_options) {
		tf_ms = tf_options[state.active_tf_idx]
	}

	return md_common.aggregate_health_from_slots(states[:], used[:], now_ms, tf_ms)
}
