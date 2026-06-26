package streams

import "core:testing"

@(test)
test_controller_mark_ack_increments_counter :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_ack(&status)
	controller_mark_ack(&status)

	testing.expect_value(t, status.subscribe_acks, 2)
}

@(test)
test_controller_snapshot_then_delta_gap_sets_desync :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)

	// Large seq gap (>10) triggers Sequence_Gap desync.
	status := Stream_Status{}
	controller_mark_connected(&status, true)

	controller_mark_message(&status, 1_000, 1_000, 100, true)
	controller_mark_message(&status, 1_100, 1_100, 115, false) // gap=15 > threshold=10

	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Sequence_Gap)
	testing.expect_value(t, status.state, Stream_State.Desync)
}

@(test)
test_controller_small_seq_gap_tolerated :: proc(t: ^testing.T) {
	// Small seq gaps (<= 10) from multi-replica interleaving are tolerated.
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)

	controller_mark_message(&status, 1_000, 1_000, 100, true)
	controller_mark_message(&status, 1_100, 1_100, 102, false) // gap=2 <= threshold=10

	result := controller_update_health(&ctrl, &status, 1_200)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
	testing.expect_value(t, result, Stream_State.Live)
}

@(test)
test_controller_stale_snapshot_triggers_desync :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)
	ctrl.desync_stale_ms = 5_000

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 10_000, 10_000, 1, true)

	controller_update_health(&ctrl, &status, 16_001)

	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Snapshot_Stale)
	testing.expect_value(t, status.state, Stream_State.Desync)
}

@(test)
test_controller_stale_snapshot_not_triggered_when_events_flowing :: proc(t: ^testing.T) {
	// If non-snapshot events (trades, deltas) keep flowing, snapshot staleness
	// should NOT trigger — the stream is alive even without explicit snapshots.
	ctrl: Stream_Controller
	controller_init(&ctrl)
	ctrl.desync_stale_ms = 5_000

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	// Initial snapshot at t=10_000.
	controller_mark_message(&status, 10_000, 10_000, 1, true)
	// Non-snapshot events keep arriving at t=14_000, t=15_000.
	controller_mark_message(&status, 14_000, 14_000, 2, false)
	controller_mark_message(&status, 15_000, 15_000, 3, false)

	// At t=16_001, snapshot is 6s stale but last event is only 1s ago.
	result := controller_update_health(&ctrl, &status, 16_001)
	testing.expect_value(t, result, Stream_State.Live)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
}

@(test)
test_controller_clock_drift_desync :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)
	ctrl.clock_drift_warn_ms = 8_000

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	// Simulate large lag (server ts much earlier than local ts).
	controller_mark_message(&status, 20_000, 10_000, 1, true)
	// lag_ms = 20_000 - 10_000 = 10_000 > 8_000

	result := controller_update_health(&ctrl, &status, 20_001)

	testing.expect_value(t, result, Stream_State.Desync)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Clock_Drift)
}

@(test)
test_controller_reconnect_clears_state :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 1_000, 1_000, 1, true)
	controller_mark_ack(&status)

	// Simulate disconnect.
	controller_mark_connected(&status, false)
	testing.expect_value(t, status.state, Stream_State.Offline)

	// Reconnect.
	controller_mark_connected(&status, true)
	controller_clear_desync(&status)
	result := controller_update_health(&ctrl, &status, 2_000)

	testing.expect_value(t, result, Stream_State.Live)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
}

@(test)
test_controller_reconnect_clears_stale_state :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)
	ctrl.desync_stale_ms = 5_000

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 10_000, 10_000, 1, true)
	controller_update_health(&ctrl, &status, 16_001)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Snapshot_Stale)
	testing.expect_value(t, status.state, Stream_State.Desync)

	// Disconnect + reconnect should allow stale latch to be cleared before new data arrives.
	controller_mark_connected(&status, false)
	controller_mark_connected(&status, true)
	controller_clear_desync(&status)
	controller_mark_ack(&status)
	controller_mark_message(&status, 16_500, 16_500, 2, false)
	result := controller_update_health(&ctrl, &status, 16_700)

	testing.expect_value(t, result, Stream_State.Live)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
}

@(test)
test_controller_lag_then_recovery :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)
	ctrl.lag_warn_ms = 4_000

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 10_000, 10_000, 1, true)

	// Check at now=15_000 → age = 5_000 > 4_000 → Lag.
	result1 := controller_update_health(&ctrl, &status, 15_000)
	testing.expect_value(t, result1, Stream_State.Lag)

	// New message arrives, bringing age down.
	controller_mark_message(&status, 15_500, 15_500, 2, false)
	result2 := controller_update_health(&ctrl, &status, 16_000)
	testing.expect_value(t, result2, Stream_State.Live)
}

@(test)
test_controller_seq_regression_sets_desync :: proc(t: ^testing.T) {
	// Large backward seq jump (>10) triggers Sequence_Gap.
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 1_000, 1_000, 100, true)
	controller_mark_message(&status, 1_100, 1_100, 80, false) // regression of 20 > threshold=10

	testing.expect_value(t, status.state, Stream_State.Desync)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Sequence_Gap)
}

@(test)
test_controller_server_ts_regression_sets_protocol_invalid :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)

	// Large regression (>5s tolerance) triggers Protocol_Invalid.
	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 10_000, 20_000, 1, true)
	controller_mark_message(&status, 10_500, 14_000, 2, false) // 6s regression > 5s tolerance

	testing.expect_value(t, status.state, Stream_State.Desync)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Protocol_Invalid)
}

@(test)
test_controller_minor_ts_regression_tolerated :: proc(t: ^testing.T) {
	// Minor timestamp regressions (<5s) from multi-replica interleaving are tolerated.
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 10_000, 9_000, 1, true)
	controller_mark_message(&status, 10_500, 8_500, 2, false) // 500ms regression < 5s tolerance

	result := controller_update_health(&ctrl, &status, 10_600)
	testing.expect_value(t, result, Stream_State.Live)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
}

@(test)
test_controller_clock_drift_auto_recovery :: proc(t: ^testing.T) {
	// Clock_Drift should auto-recover when lag drops below threshold.
	ctrl: Stream_Controller
	controller_init(&ctrl)
	ctrl.clock_drift_warn_ms = 8_000

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	// Large lag triggers Clock_Drift.
	controller_mark_message(&status, 20_000, 10_000, 1, true)
	result1 := controller_update_health(&ctrl, &status, 20_001)
	testing.expect_value(t, result1, Stream_State.Desync)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Clock_Drift)

	// Lag recovers — new message with normal lag.
	controller_mark_message(&status, 21_000, 20_500, 2, false)
	// lag_ms = 500 < 8_000 → should auto-recover.
	result2 := controller_update_health(&ctrl, &status, 21_001)
	testing.expect_value(t, result2, Stream_State.Live)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
}

@(test)
test_controller_snapshot_stale_auto_recovery :: proc(t: ^testing.T) {
	// Snapshot_Stale should auto-recover when events resume flowing.
	ctrl: Stream_Controller
	controller_init(&ctrl)
	ctrl.desync_stale_ms = 5_000

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	// Only snapshot, no other events → goes stale.
	controller_mark_message(&status, 10_000, 10_000, 1, true)
	result1 := controller_update_health(&ctrl, &status, 16_001)
	testing.expect_value(t, result1, Stream_State.Desync)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Snapshot_Stale)

	// Events resume flowing.
	controller_mark_message(&status, 16_500, 16_500, 2, false)
	result2 := controller_update_health(&ctrl, &status, 17_000)
	// event_age = 500ms < 5_000ms → should auto-recover.
	testing.expect_value(t, result2, Stream_State.Live)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
}

@(test)
test_controller_seq_monotonic_property :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)

	for i in 0 ..< 200 {
		seq := i64(i + 1)
		local_ts := i64(1_000 + i * 10)
		server_ts := local_ts - 2
		controller_mark_message(&status, local_ts, server_ts, seq, i == 0)
	}

	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.None)
}
