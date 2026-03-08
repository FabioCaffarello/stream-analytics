package app

import "core:testing"
import "mr:ports"

// S24: Legacy orderbook_snapshot_gate tests removed — proc deleted.
// Snapshot gate logic is now tested in md_common/protocol_engine_test.odin
// via snapshot_gate_check and apply_state_needs_snapshot.

@(test)
test_channels_for_widget_direct_mapping :: proc(t: ^testing.T) {
	// S62: channels_for_widget replaces legacy_widget_bundle → channels_for_bundle indirection.
	candle_mask := channels_for_widget(.Candle)
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Candles))) != 0, "candles channel should be required")
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Stats))) != 0, "stats channel should be required")
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Evidence))) != 0, "evidence channel should be required")
	testing.expect(t, (candle_mask & (1 << u8(ports.MD_Channel.Signals))) != 0, "signals channel should be required")

	dom_mask := channels_for_widget(.DOM)
	testing.expect(t, (dom_mask & (1 << u8(ports.MD_Channel.Orderbook))) != 0, "dom should require orderbook")
	testing.expect(t, (dom_mask & (1 << u8(ports.MD_Channel.Trades))) != 0, "dom should require trades")
}

@(test)
test_channels_for_widget_empty_returns_zero :: proc(t: ^testing.T) {
	testing.expect(t, channels_for_widget(.Empty) == 0, "empty widget should need no channels")
	testing.expect(t, channels_for_widget(.Analytics) == 0, "analytics renders from cell stores, no channels")
	testing.expect(t, channels_for_widget(.Session_VPVR) == 0, "session vpvr renders from cell stores")
	testing.expect(t, channels_for_widget(.TPO) == 0, "tpo renders from cell stores")
}

@(test)
test_compare_widget_kind_for_idx :: proc(t: ^testing.T) {
	testing.expect(t, compare_widget_kind_for_idx(0) == .Orderbook, "compare idx 0 should be orderbook")
	testing.expect(t, compare_widget_kind_for_idx(1) == .Trades, "compare idx 1 should be trades")
	testing.expect(t, compare_widget_kind_for_idx(2) == .Candle, "compare idx 2 should be candle")
	testing.expect(t, compare_widget_kind_for_idx(99) == .Candle, "out-of-range should default to candle")
}

@(test)
test_layer_bundle_for_widget_non_zero :: proc(t: ^testing.T) {
	// All visible widget kinds should produce a non-zero bundle.
	testing.expect(t, layer_bundle_for_widget(.Candle) != 0, "candle bundle should be non-zero")
	testing.expect(t, layer_bundle_for_widget(.Trades) != 0, "trades bundle should be non-zero")
	testing.expect(t, layer_bundle_for_widget(.Orderbook) != 0, "orderbook bundle should be non-zero")
	testing.expect(t, layer_bundle_for_widget(.DOM) != 0, "dom bundle should be non-zero")
	testing.expect(t, layer_bundle_for_widget(.Heatmap) != 0, "heatmap bundle should be non-zero")
	testing.expect(t, layer_bundle_for_widget(.VPVR) != 0, "vpvr bundle should be non-zero")
	testing.expect(t, layer_bundle_for_widget(.Stats) != 0, "stats bundle should be non-zero")
	testing.expect(t, layer_bundle_for_widget(.Counter) != 0, "counter bundle should be non-zero")
	// Non-layer widgets return empty.
	testing.expect(t, layer_bundle_for_widget(.Empty) == 0, "empty widget should have no bundle")
	testing.expect(t, layer_bundle_for_widget(.Analytics) == 0, "analytics should have no bundle")
}
