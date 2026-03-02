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

	status := Stream_Status{}
	controller_mark_connected(&status, true)

	controller_mark_message(&status, 1_000, 1_000, 100, true)
	controller_mark_message(&status, 1_100, 1_100, 102, false)

	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Sequence_Gap)
	testing.expect_value(t, status.state, Stream_State.Desync)
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
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 1_000, 1_000, 10, true)
	controller_mark_message(&status, 1_100, 1_100, 9, false)

	testing.expect_value(t, status.state, Stream_State.Desync)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Sequence_Gap)
}

@(test)
test_controller_server_ts_regression_sets_protocol_invalid :: proc(t: ^testing.T) {
	ctrl: Stream_Controller
	controller_init(&ctrl)

	status := Stream_Status{}
	controller_mark_connected(&status, true)
	controller_mark_message(&status, 10_000, 9_000, 1, true)
	controller_mark_message(&status, 10_500, 8_500, 2, false)

	testing.expect_value(t, status.state, Stream_State.Desync)
	testing.expect_value(t, status.desync_reason, Stream_Desync_Reason.Protocol_Invalid)
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
