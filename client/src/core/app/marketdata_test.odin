package app

import "core:testing"
import "mr:ports"

// S24: Legacy orderbook_snapshot_gate tests removed — proc deleted.
// Snapshot gate logic is now tested in md_common/protocol_engine_test.odin
// via snapshot_gate_check and apply_state_needs_snapshot.

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
