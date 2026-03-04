package app

import "core:testing"
import "mr:ports"

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

@(test)
test_channels_for_bundle_uses_layer_mapping :: proc(t: ^testing.T) {
	candle_mask := channels_for_bundle(legacy_widget_bundle(.Candle))
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Candles))) != 0, "candles channel should be required")
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Stats))) != 0, "stats channel should be required")
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Evidence))) != 0, "evidence channel should be required")
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Signals))) != 0, "signals channel should be required")

	dom_mask := channels_for_bundle(legacy_widget_bundle(.DOM))
	testing.expect(t, (dom_mask & (1 << u8(ports.MD_Channel.Orderbook))) != 0, "dom should require orderbook")
	testing.expect(t, (dom_mask & (1 << u8(ports.MD_Channel.Trades))) != 0, "dom should require trades")
}
