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
