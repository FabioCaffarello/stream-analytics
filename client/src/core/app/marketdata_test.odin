package app

import "core:testing"

@(test)
test_orderbook_snapshot_gate_accepts_explicit_snapshot :: proc(t: ^testing.T) {
	next_seen, mark_snapshot, gap := orderbook_snapshot_gate(false, true, 0, 0)
	testing.expect_value(t, next_seen, true)
	testing.expect_value(t, mark_snapshot, true)
	testing.expect_value(t, gap, false)
}

@(test)
test_orderbook_snapshot_gate_bootstraps_non_empty_delta :: proc(t: ^testing.T) {
	next_seen, mark_snapshot, gap := orderbook_snapshot_gate(false, false, 3, 2)
	testing.expect_value(t, next_seen, true)
	testing.expect_value(t, mark_snapshot, true)
	testing.expect_value(t, gap, false)
}

@(test)
test_orderbook_snapshot_gate_rejects_empty_delta_before_snapshot :: proc(t: ^testing.T) {
	next_seen, mark_snapshot, gap := orderbook_snapshot_gate(false, false, 0, 0)
	testing.expect_value(t, next_seen, false)
	testing.expect_value(t, mark_snapshot, false)
	testing.expect_value(t, gap, true)
}
