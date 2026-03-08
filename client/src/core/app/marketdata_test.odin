package app

import "core:testing"
import "mr:layers"
import "mr:ports"
import "mr:services"

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

	// S98: Candle widget should include analytics channels for subplot support.
	testing.expect(t, (candle_mask & (1 << u16(ports.MD_Channel.Analytics_CVD))) != 0, "candle should include analytics CVD")
	testing.expect(t, (candle_mask & (1 << u16(ports.MD_Channel.Analytics_Delta_Volume))) != 0, "candle should include analytics DV")
	testing.expect(t, (candle_mask & (1 << u16(ports.MD_Channel.Analytics_OI))) != 0, "candle should include analytics OI")
	testing.expect(t, (candle_mask & (1 << u16(ports.MD_Channel.Analytics_Bar_Stats))) != 0, "candle should include analytics BS")

	dom_mask := channels_for_widget(.DOM)
	testing.expect(t, (dom_mask & (1 << u8(ports.MD_Channel.Orderbook))) != 0, "dom should require orderbook")
	testing.expect(t, (dom_mask & (1 << u8(ports.MD_Channel.Trades))) != 0, "dom should require trades")
}

@(test)
test_channels_for_widget_empty_returns_zero :: proc(t: ^testing.T) {
	testing.expect(t, channels_for_widget(.Empty) == 0, "empty widget should need no channels")
	// S98: Analytics subscribes to dedicated analytics channels (CVD/DV/OI/BS).
	testing.expect(t, channels_for_widget(.Analytics) != 0, "analytics needs dedicated analytics channels")
	testing.expect(t, channels_for_widget(.Session_VPVR) == 0, "session vpvr renders from cell stores")
	testing.expect(t, channels_for_widget(.TPO) == 0, "tpo renders from cell stores")
}

@(test)
test_compare_widget_kind_for_idx :: proc(t: ^testing.T) {
	testing.expect(t, compare_widget_kind_for_idx(0) == .Orderbook, "compare idx 0 should be orderbook")
	testing.expect(t, compare_widget_kind_for_idx(1) == .Trades, "compare idx 1 should be trades")
	testing.expect(t, compare_widget_kind_for_idx(2) == .Candle, "compare idx 2 should be candle")
	testing.expect(t, compare_widget_kind_for_idx(3) == .Analytics, "compare idx 3 should be analytics") // S84
	testing.expect(t, compare_widget_kind_for_idx(99) == .Candle, "out-of-range should default to candle")
}

// S84: Compare mode analytics state isolation — per-pane analytics_kind is independent.
@(test)
test_compare_analytics_kind_per_pane_isolation :: proc(t: ^testing.T) {
	cmp: Compare_State
	cmp.count = 4
	cmp.analytics_kind[0] = .Open_Interest
	cmp.analytics_kind[1] = .CVD
	cmp.analytics_kind[2] = .Delta_Volume
	cmp.analytics_kind[3] = .Bar_Stats

	// Each pane has independent analytics kind — no cross-contamination.
	testing.expect(t, cmp.analytics_kind[0] == .Open_Interest, "pane 0 should be OI")
	testing.expect(t, cmp.analytics_kind[1] == .CVD, "pane 1 should be CVD")
	testing.expect(t, cmp.analytics_kind[2] == .Delta_Volume, "pane 2 should be DV")
	testing.expect(t, cmp.analytics_kind[3] == .Bar_Stats, "pane 3 should be BS")

	// Mutating one pane does not affect others.
	cmp.analytics_kind[1] = .Open_Interest
	testing.expect(t, cmp.analytics_kind[0] == .Open_Interest, "pane 0 unchanged after pane 1 mutation")
	testing.expect(t, cmp.analytics_kind[2] == .Delta_Volume, "pane 2 unchanged after pane 1 mutation")
	testing.expect(t, cmp.analytics_kind[3] == .Bar_Stats, "pane 3 unchanged after pane 1 mutation")
}

// S84: Compare widget options include analytics.
@(test)
test_compare_widget_options_has_analytics :: proc(t: ^testing.T) {
	opts := COMPARE_WIDGET_OPTIONS
	testing.expect(t, len(opts) == 4, "compare should have 4 widget options")
	testing.expect(t, opts[3] == "Analytics", "compare idx 3 label should be Analytics")
}

// S98: Analytics compare requires dedicated analytics channel subscriptions.
@(test)
test_compare_analytics_channels :: proc(t: ^testing.T) {
	ak := compare_widget_kind_for_idx(3)
	ch := channels_for_widget(ak)
	// Analytics subscribes to dedicated channels (CVD/DV/OI/BS).
	testing.expect(t, ch != 0, "analytics compare should require channels")
	testing.expect(t, (ch & (1 << u16(ports.MD_Channel.Analytics_CVD))) != 0, "analytics compare should require CVD channel")
	testing.expect(t, (ch & (1 << u16(ports.MD_Channel.Analytics_Delta_Volume))) != 0, "analytics compare should require DV channel")
	testing.expect(t, (ch & (1 << u16(ports.MD_Channel.Analytics_OI))) != 0, "analytics compare should require OI channel")
	testing.expect(t, (ch & (1 << u16(ports.MD_Channel.Analytics_Bar_Stats))) != 0, "analytics compare should require BS channel")
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
	testing.expect(t, layer_bundle_for_widget(.Analytics) != 0, "analytics should have a layer bundle")
}

// S95: Per-pane subplot flag isolation — each pane has independent CVD/DV/OI toggles.
@(test)
test_compare_subplot_flags_per_pane_isolation :: proc(t: ^testing.T) {
	cmp: Compare_State
	cmp.count = 4
	cmp.show_cvd[0] = true
	cmp.show_delta_vol[1] = true
	cmp.show_oi[2] = true

	// Each pane's subplot flags are independent.
	testing.expect(t, cmp.show_cvd[0] == true, "pane 0 CVD should be on")
	testing.expect(t, cmp.show_cvd[1] == false, "pane 1 CVD should be off")
	testing.expect(t, cmp.show_delta_vol[0] == false, "pane 0 DV should be off")
	testing.expect(t, cmp.show_delta_vol[1] == true, "pane 1 DV should be on")
	testing.expect(t, cmp.show_oi[0] == false, "pane 0 OI should be off")
	testing.expect(t, cmp.show_oi[2] == true, "pane 2 OI should be on")
	testing.expect(t, cmp.show_oi[3] == false, "pane 3 OI should be off")
}

// S95: Toggle subplot does not affect other panes.
@(test)
test_compare_subplot_toggle_isolation :: proc(t: ^testing.T) {
	cmp: Compare_State
	cmp.count = 3
	cmp.show_cvd[0] = true
	cmp.show_cvd[1] = true
	cmp.show_cvd[2] = true

	// Toggle off pane 1 only.
	cmp.show_cvd[1] = false

	testing.expect(t, cmp.show_cvd[0] == true, "pane 0 CVD should remain on")
	testing.expect(t, cmp.show_cvd[1] == false, "pane 1 CVD should be off after toggle")
	testing.expect(t, cmp.show_cvd[2] == true, "pane 2 CVD should remain on")
}

// S95: Multiple subplot flags can be active simultaneously on same pane.
@(test)
test_compare_subplot_multi_flag_same_pane :: proc(t: ^testing.T) {
	cmp: Compare_State
	cmp.count = 2
	cmp.show_cvd[0] = true
	cmp.show_delta_vol[0] = true
	cmp.show_oi[0] = true

	// Pane 0 has all three subplots, pane 1 has none.
	testing.expect(t, cmp.show_cvd[0] && cmp.show_delta_vol[0] && cmp.show_oi[0],
		"pane 0 should have all 3 subplots active")
	testing.expect(t, !cmp.show_cvd[1] && !cmp.show_delta_vol[1] && !cmp.show_oi[1],
		"pane 1 should have no subplots active")
}

// S95: apply_toggle_compare_subplot toggles the correct flag.
@(test)
test_apply_toggle_compare_subplot :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.compare.active = true
	state.compare.count = 2

	// Toggle CVD on pane 0.
	apply_toggle_compare_subplot(state, 0, 0)
	testing.expect(t, state.compare.show_cvd[0] == true, "CVD should be on after toggle")
	testing.expect(t, state.compare.show_cvd[1] == false, "pane 1 CVD should be unaffected")

	// Toggle CVD off pane 0.
	apply_toggle_compare_subplot(state, 0, 0)
	testing.expect(t, state.compare.show_cvd[0] == false, "CVD should be off after second toggle")

	// Toggle DeltaVol on pane 1.
	apply_toggle_compare_subplot(state, 1, 1)
	testing.expect(t, state.compare.show_delta_vol[1] == true, "DV on pane 1 should be on")
	testing.expect(t, state.compare.show_delta_vol[0] == false, "DV on pane 0 should be unaffected")

	// Toggle OI on pane 0.
	apply_toggle_compare_subplot(state, 0, 2)
	testing.expect(t, state.compare.show_oi[0] == true, "OI on pane 0 should be on")
}

// S95: apply_toggle_compare_subplot rejects invalid pane/subplot indices.
@(test)
test_apply_toggle_compare_subplot_bounds :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.compare.active = true
	state.compare.count = 2

	// Invalid pane index — should not crash or modify state.
	apply_toggle_compare_subplot(state, -1, 0)
	apply_toggle_compare_subplot(state, 4, 0)
	// Invalid subplot index — should not toggle anything.
	apply_toggle_compare_subplot(state, 0, 5)
	testing.expect(t, state.compare.show_cvd[0] == false, "no change on invalid subplot idx")

	// Not active — should be no-op.
	state.compare.active = false
	apply_toggle_compare_subplot(state, 0, 0)
	testing.expect(t, state.compare.show_cvd[0] == false, "no change when compare inactive")
}

// S95: Subplot flags default to false (zero-init).
@(test)
test_compare_subplot_flags_zero_init :: proc(t: ^testing.T) {
	cmp: Compare_State
	for i in 0 ..< 4 {
		testing.expect(t, cmp.show_cvd[i] == false, "CVD should default false")
		testing.expect(t, cmp.show_delta_vol[i] == false, "DV should default false")
		testing.expect(t, cmp.show_oi[i] == false, "OI should default false")
	}
}

// ---------------------------------------------------------------------------
// S99: Analytics Truth Unification — layer_store is canonical analytics source.
// ---------------------------------------------------------------------------

// S100: Analytics accessible directly from layer_store active stream (no mirror).
@(test)
test_s100_analytics_from_active_stream :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	// Seed a stream in layer_store and push analytics.
	sid := u64(42)
	stream := layers.market_store_stream_get_or_alloc(&state.layer_store, sid)
	testing.expect(t, stream != nil, "stream should be allocated")
	layers.market_store_set_active_subject(&state.layer_store, sid)

	services.push_analytics(&stream.analytics, services.Analytics_Entry{
		kind = .CVD,
		ts_ms = 1000,
		seq = 1,
		values = {-2.5, 150.0, 0, 0, 0, 0, 0, 0},
	})

	// Active stream analytics should be directly accessible.
	store := active_analytics_store(state)
	testing.expect(t, store != nil, "active analytics store should exist")
	testing.expect_value(t, store.count, 1)
	entry, ok := services.get_analytics_latest(store, .CVD)
	testing.expect(t, ok, "active stream should contain CVD entry")
	testing.expect(t, entry.values[1] == 150.0, "CVD value should match")
}

// S99: resolve_stores_for_cell returns layer_store analytics (not slot).
@(test)
test_s99_resolve_stores_analytics_from_layer_store :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	reg := new(Stream_View_Registry)
	defer free(reg)
	state.stream_views = reg

	sid := u64(100)

	// Allocate slot for the stream.
	slot := stream_view_get_or_alloc_slot(reg, sid, 1, state)
	testing.expect(t, slot != nil, "slot should be allocated")
	slot_idx := stream_view_find_slot(reg, sid)

	// Allocate stream in layer_store.
	stream := layers.market_store_stream_get_or_alloc(&state.layer_store, sid)
	layers.market_store_set_active_subject(&state.layer_store, sid)

	// Push analytics to layer_store stream.
	services.push_analytics(&stream.analytics, services.Analytics_Entry{
		kind = .Delta_Volume,
		ts_ms = 2000,
		seq = 10,
		values = {12.0, 9.0, 3.0, 0, 0, 0, 0, 0},
	})

	// Setup cell binding to point at this slot.
	state.world.count = 1
	state.world.bindings[0].stream_idx = slot_idx
	// Set active stream to something else so we hit the per-cell resolution path.
	reg.active_subject_id = 999
	reg.has_active = true

	// Resolve stores for cell 0.
	stores := resolve_stores_for_cell(state, 0)
	testing.expect(t, stores.analytics != nil, "analytics store should be resolved")

	// The analytics pointer should be the layer_store stream's analytics.
	testing.expect(t, stores.analytics == &stream.analytics,
		"analytics store should point to layer_store Market_Stream")

	// Verify data is accessible.
	entry, ok := services.get_analytics_latest(stores.analytics, .Delta_Volume)
	testing.expect(t, ok, "should find delta volume entry")
	testing.expect(t, entry.values[2] == 3.0, "delta volume should be 3.0")
}

// S99: Historical + realtime compose into same store.
@(test)
test_s99_historical_and_realtime_compose :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	sid := u64(200)
	stream := layers.market_store_stream_get_or_alloc(&state.layer_store, sid)
	layers.market_store_set_active_subject(&state.layer_store, sid)

	// Simulate historical data (HTTP fetch writes to layer_store stream).
	services.push_analytics(&stream.analytics, services.Analytics_Entry{
		kind = .CVD,
		ts_ms = 1000,
		seq = 1,
		values = {0, 100.0, 0, 0, 0, 0, 0, 0},
	})
	services.push_analytics(&stream.analytics, services.Analytics_Entry{
		kind = .CVD,
		ts_ms = 2000,
		seq = 2,
		values = {0, 200.0, 0, 0, 0, 0, 0, 0},
	})

	// Simulate realtime data (WS push via market_store_reduce_analytics).
	services.push_analytics(&stream.analytics, services.Analytics_Entry{
		kind = .CVD,
		ts_ms = 3000,
		seq = 3,
		values = {0, 350.0, 0, 0, 0, 0, 0, 0},
	})

	// All three entries should be in a single store.
	testing.expect_value(t, stream.analytics.count, 3)

	// Collect all CVD entries — should be oldest-first.
	entries: [48]services.Analytics_Entry
	n := services.analytics_collect_by_kind(&stream.analytics, .CVD, entries[:])
	testing.expect_value(t, n, 3)
	testing.expect(t, entries[0].values[1] == 100.0, "oldest should be first historical")
	testing.expect(t, entries[1].values[1] == 200.0, "middle should be second historical")
	testing.expect(t, entries[2].values[1] == 350.0, "newest should be realtime")
}

// S99: resolve_analytics_store_for_subject returns layer_store stream.
@(test)
test_s99_resolve_analytics_store_for_subject :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	sid := u64(300)

	// Before any stream exists: returns nil.
	// Note: market_store_stream_get_or_alloc creates the stream, so resolve will too.
	store := resolve_analytics_store_for_subject(state, sid)
	testing.expect(t, store != nil, "should allocate stream and return analytics store")

	// Null subject returns nil.
	store_nil := resolve_analytics_store_for_subject(state, 0)
	testing.expect(t, store_nil == nil, "zero subject_id should return nil")

	// Nil state returns nil.
	store_nil2 := resolve_analytics_store_for_subject(nil, sid)
	testing.expect(t, store_nil2 == nil, "nil state should return nil")
}

// S99: TF change clears analytics on layer_store stream (not slot).
@(test)
test_s99_tf_change_clears_layer_store_analytics :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	sid := u64(400)
	stream := layers.market_store_stream_get_or_alloc(&state.layer_store, sid)

	// Push analytics data.
	services.push_analytics(&stream.analytics, services.Analytics_Entry{
		kind = .Open_Interest,
		ts_ms = 5000,
		seq = 1,
		values = {50000.0, 100.0, 0.2, 0, 0, 0, 0, 0},
	})
	testing.expect_value(t, stream.analytics.count, 1)

	// Simulate the TF-change clear (same as stream_views.odin does now).
	if ms := layers.market_store_stream_for_subject(&state.layer_store, sid); ms != nil {
		services.analytics_store_clear(&ms.analytics)
	}

	// Analytics should be cleared.
	testing.expect_value(t, stream.analytics.count, 0)
}

// S99: Stream_View_Slot no longer has analytics_store field.
@(test)
test_s99_slot_no_analytics_store :: proc(t: ^testing.T) {
	// Verify the slot struct has the expected fields but NOT analytics_store.
	slot := new(Stream_View_Slot)
	defer free(slot)
	slot.used = true
	slot.subject_id = 1
	// session_vpvr_store and tpo_store still exist on slot.
	slot.session_vpvr_store = {}
	slot.tpo_store = {}
	testing.expect(t, slot.used, "slot should be usable without analytics_store")
}
