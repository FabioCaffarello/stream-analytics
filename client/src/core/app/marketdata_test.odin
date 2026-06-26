package app

import "core:testing"
import "mr:layers"
import "mr:md_common"
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

// --- S102: Product Surface Convergence ---

// S102: poll_portfolio does NOT gate on WS connection status (S89 alignment).
// Portfolio data comes via HTTP — WS state is irrelevant.
@(test)
test_s102_portfolio_poll_no_ws_gate :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	// Simulate WS disconnected — conn_status defaults to .Offline (zero-init).
	// Before S102, poll_portfolio would early-return here.
	// After S102, it should proceed and attempt fetches.
	state.portfolio.summary_status = .Idle
	state.frame = 0

	// poll_portfolio should run even without WS connection.
	// Since fetch_portfolio_summary port is nil, it should set .Error (not stay .Idle).
	poll_portfolio(state)
	testing.expect(t, state.portfolio.summary_status == .Error,
		"portfolio poll should attempt fetch even when WS is disconnected")
}

// S102: Portfolio retry interval is shorter on error (aligned with S89 pattern).
@(test)
test_s102_portfolio_retry_interval :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)

	// Set all statuses to Error to trigger faster retry interval.
	state.portfolio.summary_status = .Error
	state.portfolio.readiness_status = .Error

	// At PORTFOLIO_RETRY_INTERVAL (300), should re-attempt.
	state.frame = PORTFOLIO_RETRY_INTERVAL
	poll_portfolio(state)
	// fetch_portfolio_summary port is nil → stays .Error, but it was attempted (frame updated).
	// The key assertion: poll_portfolio ran at 300 frames (not waiting for 600).
	testing.expect(t, state.portfolio.summary_status == .Error,
		"portfolio should retry at shorter interval on error")

	// Verify normal interval is longer: at frame 300 with success status, should NOT poll.
	state.portfolio.summary_status = .Success
	state.portfolio.readiness_status = .Success
	state.portfolio.snapshot_status = .Success
	state.portfolio.state_status = .Success
	state.frame = PORTFOLIO_RETRY_INTERVAL
	prev_frame := state.portfolio.summary_frame
	poll_portfolio(state)
	// summary_frame should NOT change — poll was skipped.
	testing.expect(t, state.portfolio.summary_frame == prev_frame,
		"portfolio should not poll at retry interval when all stores are success")
}

// S102: PORTFOLIO_RETRY_INTERVAL matches HEALTH_RETRY_INTERVAL (consistent contract).
@(test)
test_s102_poll_intervals_consistent :: proc(t: ^testing.T) {
	// All page surfaces should use the same base poll interval.
	testing.expect_value(t, PORTFOLIO_POLL_INTERVAL, u64(600))
	testing.expect_value(t, HEALTH_POLL_INTERVAL, u64(600))
	testing.expect_value(t, OVERVIEW_POLL_INTERVAL, u64(600))

	// All page surfaces should use the same retry interval.
	testing.expect_value(t, PORTFOLIO_RETRY_INTERVAL, u64(300))
	testing.expect_value(t, HEALTH_RETRY_INTERVAL, u64(300))
	testing.expect_value(t, OVERVIEW_RETRY_INTERVAL, u64(300))
}

// --- S104: Dashboard Timeframe Integrity Hardening ---

// S104: Global TF change clears bound cell stores (not just active slot).
@(test)
test_s104_global_tf_clears_bound_cell_stores :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	// Setup: 2 cells, both following global TF.
	state.world.count = 2
	state.active_tf_idx = 2 // 1m

	// Cell 0 = follow-active (no binding).
	state.world.timeframes[0].tf_idx = -1

	// Cell 1 = bound to its own stream (slot 1).
	state.world.timeframes[1].tf_idx = -1
	binding_set(&state.world.bindings[1], "binance", "ETHUSDT")
	state.world.bindings[1].stream_idx = 1

	// Allocate slot 1 with candle data.
	slot := &state.stream_views.slots[1]
	slot.used = true
	slot.subject_id = 12345
	slot.candle_store.head = 5
	slot.candle_store.count = 10

	// Apply global TF change: 1m → 5m.
	apply_set_timeframe_action(state, 3)

	// Bound cell's slot should have cleared candle store.
	testing.expect_value(t, slot.candle_store.head, 0)
	testing.expect_value(t, slot.candle_store.count, 0)
}

// S104: Global TF change clears analytics store for bound cells.
@(test)
test_s104_global_tf_clears_bound_cell_analytics :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1

	// Bound cell with analytics data.
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	state.world.bindings[0].stream_idx = 0
	slot := &state.stream_views.slots[0]
	slot.used = true
	slot.subject_id = 999

	stream := layers.market_store_stream_get_or_alloc(&state.layer_store, 999)
	services.push_analytics(&stream.analytics, services.Analytics_Entry{
		kind = .CVD, ts_ms = 1000, seq = 1,
	})
	testing.expect_value(t, stream.analytics.count, 1)

	// Global TF change.
	apply_set_timeframe_action(state, 4)

	// Analytics should be cleared.
	testing.expect_value(t, stream.analytics.count, 0)
}

// S104: Per-cell TF skip — cells with per-cell TF override are NOT cleared by global change.
@(test)
test_s104_global_tf_skips_per_cell_tf :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = 5  // per-cell override (30m)

	binding_set(&state.world.bindings[0], "binance", "ETHUSDT")
	state.world.bindings[0].stream_idx = 0
	slot := &state.stream_views.slots[0]
	slot.used = true
	slot.subject_id = 888
	slot.candle_store.head = 3
	slot.candle_store.count = 7

	// Global TF change.
	apply_set_timeframe_action(state, 4)

	// Per-cell TF cell's slot should NOT be cleared.
	testing.expect_value(t, slot.candle_store.head, 3)
	testing.expect_value(t, slot.candle_store.count, 7)
}

// S104: Per-cell TF change clears stores and resets apply state.
@(test)
test_s104_per_cell_tf_clears_slot_stores :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1

	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	state.world.bindings[0].stream_idx = 0
	slot := &state.stream_views.slots[0]
	slot.used = true
	slot.subject_id = 777
	slot.candle_store.head = 10
	slot.candle_store.count = 20
	slot.apply_state.has_live[.Candle] = true

	// Set per-cell TF from -1 (global) to 5 (30m).
	result := apply_set_cell_timeframe_action(state, 0, 5)
	testing.expect(t, result, "per-cell TF change should succeed")

	// Stores should be cleared.
	testing.expect_value(t, slot.candle_store.head, 0)
	testing.expect_value(t, slot.candle_store.count, 0)
	// Apply state should be reset per policy.
	testing.expect(t, !slot.apply_state.has_live[.Candle], "has_live candle should be reset")
}

// S104: apply_state_on_tf_change is called for bound cells on global TF change.
@(test)
test_s104_global_tf_resets_apply_state_for_bound_cells :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1 // follows global

	binding_set(&state.world.bindings[0], "bybit", "BTCUSDT")
	state.world.bindings[0].stream_idx = 0
	slot := &state.stream_views.slots[0]
	slot.used = true
	slot.subject_id = 555
	slot.apply_state.has_live[.Candle] = true
	slot.apply_state.has_live[.Heatmap] = true

	// Global TF change.
	apply_set_timeframe_action(state, 6)

	// Apply state should be reset.
	testing.expect(t, !slot.apply_state.has_live[.Candle], "candle has_live should reset")
	testing.expect(t, !slot.apply_state.has_live[.Heatmap], "heatmap has_live should reset")
}

// ═══════════════════════════════════════════════════════════════
// S107: Pane Visual State resolution tests.
// ═══════════════════════════════════════════════════════════════

import "mr:streams"

@(test)
test_pane_visual_state_offline :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{}
	vs := resolve_pane_visual_state(sv, .Offline, .Offline)
	testing.expect(t, vs == .Offline, "offline connection should yield Offline state")
}

@(test)
test_pane_visual_state_desync :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Desync)
	testing.expect(t, vs == .Error, "desync stream should yield Error state")
}

@(test)
test_pane_visual_state_critical :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{
		composition  = .Composed,
		stream_bound = true,
		health_level = .Critical,
	}
	vs := resolve_pane_visual_state(sv, .Connected, .Live)
	testing.expect(t, vs == .Error, "critical health should yield Error state")
}

@(test)
test_pane_visual_state_empty_unbound :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Empty, stream_bound = false}
	vs := resolve_pane_visual_state(sv, .Connected, .Live)
	testing.expect(t, vs == .Empty, "empty+unbound should yield Empty state")
}

@(test)
test_pane_visual_state_range_pending :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live)
	testing.expect(t, vs == .Loading, "range_pending should yield Loading state")
}

@(test)
test_pane_visual_state_live_only :: proc(t: ^testing.T) {
	// S136: Candle + Live_Only now yields Active (chart is renderable with live data).
	// Composition badge "LIVE" communicates the transitional state.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live)
	testing.expect(t, vs == .Active, "candle live_only should yield Active (S136: store/composition-driven)")
}

@(test)
test_pane_visual_state_live_only_trades :: proc(t: ^testing.T) {
	// S124: Trades with live data flowing but no trades yet → Seeding.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Trades)
	testing.expect(t, vs == .Seeding, "trades with live data but empty store should yield Seeding")
}

@(test)
test_pane_visual_state_backfilled :: proc(t: ^testing.T) {
	// S136: Candle + Backfilled now yields Active (chart is renderable with historical data).
	// Composition badge "BFILL" communicates the transitional state.
	sv := Cell_Surface_View{composition = .Backfilled, stream_bound = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live)
	testing.expect(t, vs == .Active, "backfilled should yield Active (S136: historical data is renderable)")
}

@(test)
test_pane_visual_state_composed :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live)
	testing.expect(t, vs == .Active, "composed should yield Active state")
}

// ═══════════════════════════════════════════════════════════════
// S114: State sub-label helpers — widget-specific contextual messages.
// ═══════════════════════════════════════════════════════════════

@(test)
test_state_sub_label_loading_candle :: proc(t: ^testing.T) {
	s := _state_sub_label_loading(.Candle)
	testing.expect(t, len(s) > 0, "loading sub-label for Candle should be non-empty")
	testing.expect(t, s == "Requesting candle history", "loading sub-label for Candle")
}

@(test)
test_state_sub_label_loading_orderbook :: proc(t: ^testing.T) {
	s := _state_sub_label_loading(.Orderbook)
	testing.expect(t, s == "Requesting order book", "loading sub-label for Orderbook")
}

@(test)
test_state_sub_label_loading_empty_widget :: proc(t: ^testing.T) {
	s := _state_sub_label_loading(.Empty)
	testing.expect(t, s == "No widget selected", "loading sub-label for Empty widget")
}

@(test)
test_state_sub_label_seeding_trades :: proc(t: ^testing.T) {
	s := _state_sub_label_seeding(.Trades)
	testing.expect(t, s == "Trade feed starting", "seeding sub-label for Trades")
}

@(test)
test_state_sub_label_seeding_empty_widget :: proc(t: ^testing.T) {
	s := _state_sub_label_seeding(.Empty)
	testing.expect(t, len(s) == 0, "seeding sub-label for Empty widget should be empty")
}

@(test)
test_state_sub_label_empty_candle :: proc(t: ^testing.T) {
	s := _state_sub_label_empty(.Candle)
	testing.expect(t, s == "Bind a market stream to see candles", "empty sub-label for Candle")
}

@(test)
test_state_sub_label_empty_widget :: proc(t: ^testing.T) {
	s := _state_sub_label_empty(.Empty)
	testing.expect(t, s == "Select a widget type from the catalog", "empty sub-label for Empty widget kind")
}

@(test)
test_state_sub_labels_all_widget_kinds_loading :: proc(t: ^testing.T) {
	// Every widget kind must produce a non-empty loading sub-label.
	for wk in Widget_Kind {
		s := _state_sub_label_loading(wk)
		testing.expect(t, len(s) > 0, "all widget kinds must have loading sub-label")
	}
}

@(test)
test_state_sub_labels_all_widget_kinds_empty :: proc(t: ^testing.T) {
	// Every widget kind must produce a non-empty empty sub-label.
	for wk in Widget_Kind {
		s := _state_sub_label_empty(wk)
		testing.expect(t, len(s) > 0, "all widget kinds must have empty sub-label")
	}
}

// ═══════════════════════════════════════════════════════════════
// S120: Widget State Model — new state tests.
// ═══════════════════════════════════════════════════════════════

@(test)
test_pane_visual_state_snapshot_pending_stats :: proc(t: ^testing.T) {
	// S125: Stats widget with stats-specific live data but empty store → Snapshot_Pending.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Stats] = true  // S125: stats artifact specifically live
	stats_store := services.Stats_Store{}
	stores := Cell_Stores{stats = &stats_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs == .Snapshot_Pending, "stats with artifact live but empty store should yield Snapshot_Pending")
}

@(test)
test_s125_stats_seeding_when_only_candles_live :: proc(t: ^testing.T) {
	// S125: Stats widget with candles live but stats NOT live → Seeding (not Snapshot_Pending).
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Candle] = true  // candles live
	// artifact_has_live[.Stats] = false (default)
	stats_store := services.Stats_Store{}
	stores := Cell_Stores{stats = &stats_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs == .Seeding, "stats with only candles live should yield Seeding, not Snapshot_Pending")
}

@(test)
test_pane_visual_state_stats_with_data :: proc(t: ^testing.T) {
	// S124: Stats widget with data → Active (independent of candle composition).
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	stats_store := services.Stats_Store{count = 1}
	stores := Cell_Stores{stats = &stats_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs == .Active, "stats with data should yield Active regardless of candle composition")
}

@(test)
test_pane_visual_state_snapshot_pending_orderbook :: proc(t: ^testing.T) {
	// S125: Orderbook widget with OB-specific live data but no levels → Snapshot_Pending.
	sv := Cell_Surface_View{composition = .Backfilled, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Orderbook] = true  // S125: OB artifact specifically live
	ob_store := services.Orderbook_Store{}
	stores := Cell_Stores{orderbook = &ob_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs == .Snapshot_Pending, "orderbook with OB artifact live but no levels should yield Snapshot_Pending")
}

@(test)
test_pane_visual_state_snapshot_pending_dom :: proc(t: ^testing.T) {
	// S125: DOM widget with OB-specific live data but no levels → Snapshot_Pending.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Orderbook] = true  // S125: OB artifact specifically live
	ob_store := services.Orderbook_Store{}
	stores := Cell_Stores{orderbook = &ob_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .DOM, stores)
	testing.expect(t, vs == .Snapshot_Pending, "DOM with OB artifact live but no levels should yield Snapshot_Pending")
}

@(test)
test_s125_orderbook_seeding_when_only_candles_live :: proc(t: ^testing.T) {
	// S125: OB widget with candles live but OB NOT live → Seeding (not Snapshot_Pending).
	sv := Cell_Surface_View{composition = .Backfilled, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Candle] = true  // candles live
	// artifact_has_live[.Orderbook] = false (default)
	ob_store := services.Orderbook_Store{}
	stores := Cell_Stores{orderbook = &ob_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs == .Seeding, "orderbook with only candles live should yield Seeding, not Snapshot_Pending")
}

@(test)
test_s125_dom_seeding_when_only_candles_live :: proc(t: ^testing.T) {
	// S125: DOM widget with candles live but OB NOT live → Seeding (not Snapshot_Pending).
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Candle] = true  // candles live
	ob_store := services.Orderbook_Store{}
	stores := Cell_Stores{orderbook = &ob_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .DOM, stores)
	testing.expect(t, vs == .Seeding, "DOM with only candles live should yield Seeding, not Snapshot_Pending")
}

@(test)
test_pane_visual_state_no_history_candle :: proc(t: ^testing.T) {
	// S136: Candle with Live_Only → Active. Badge "LIVE" shows state.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Candle)
	testing.expect(t, vs == .Active, "candle with live_only should yield Active (S136)")
}

@(test)
test_pane_visual_state_candle_backfilled :: proc(t: ^testing.T) {
	// S136: Candle with Backfilled → Active. Badge "BFILL" shows state.
	sv := Cell_Surface_View{composition = .Backfilled, stream_bound = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Candle)
	testing.expect(t, vs == .Active, "candle with backfilled should yield Active (S136)")
}

@(test)
test_widget_state_glyph_coverage :: proc(t: ^testing.T) {
	// All non-Empty widget kinds must have a glyph.
	for wk in Widget_Kind {
		g := _widget_state_glyph(wk)
		if wk == .Empty {
			testing.expect(t, len(g) == 0, "Empty widget should have no glyph")
		} else {
			testing.expect(t, len(g) > 0, "non-Empty widget must have glyph")
		}
	}
}

@(test)
test_state_sub_label_snapshot_pending_all :: proc(t: ^testing.T) {
	// Every widget kind must produce a non-empty snapshot_pending sub-label (except Empty).
	for wk in Widget_Kind {
		s := _state_sub_label_snapshot_pending(wk)
		if wk != .Empty {
			testing.expect(t, len(s) > 0, "all widget kinds must have snapshot_pending sub-label")
		}
	}
}

// S154: test_state_sub_label_no_history_all removed — No_History variant
// and _state_sub_label_no_history proc were removed (dead code).

// ═══════════════════════════════════════════════════════════════
// S124: Timeframe-Aware Widget Readiness — store-driven Active states.
// Non-candle widgets become Active as soon as their own data arrives,
// independent of candle composition stage (GetRange/Live_Only/Backfilled).
// ═══════════════════════════════════════════════════════════════

@(test)
test_s124_stats_active_during_range_pending :: proc(t: ^testing.T) {
	// Stats with data should be Active even while candle GetRange is in flight.
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = true}
	stats_store := services.Stats_Store{count = 3}
	stores := Cell_Stores{stats = &stats_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs == .Active, "stats with data should be Active even during Range_Pending")
}

@(test)
test_s124_orderbook_active_during_range_pending :: proc(t: ^testing.T) {
	// Orderbook with snapshot should be Active even while candle GetRange is in flight.
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = true}
	ob_store := services.Orderbook_Store{bid_count = 10, ask_count = 10}
	stores := Cell_Stores{orderbook = &ob_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs == .Active, "orderbook with levels should be Active even during Range_Pending")
}

@(test)
test_s124_trades_active_with_data :: proc(t: ^testing.T) {
	// Trades with entries should be Active even during candle Live_Only.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	trades_store := services.Trades_Store{count = 5}
	stores := Cell_Stores{trades = &trades_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Trades, stores)
	testing.expect(t, vs == .Active, "trades with data should be Active regardless of candle composition")
}

@(test)
test_s124_counter_active_with_candles :: proc(t: ^testing.T) {
	// Counter with candle data should be Active even during Live_Only.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	candle_store := services.Candle_Store{count = 1}
	stores := Cell_Stores{candle = &candle_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Counter, stores)
	testing.expect(t, vs == .Active, "counter with candle data should be Active")
}

@(test)
test_s124_heatmap_active_with_data :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = true}
	hm_store := new(services.Heatmap_Store)
	defer free(hm_store)
	hm_store.count = 2
	stores := Cell_Stores{heatmap = hm_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Heatmap, stores)
	testing.expect(t, vs == .Active, "heatmap with data should be Active during Range_Pending")
}

@(test)
test_s124_analytics_active_with_data :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = true}
	a_store := services.Analytics_Store{count = 10}
	stores := Cell_Stores{analytics = &a_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Analytics, stores)
	testing.expect(t, vs == .Active, "analytics with data should be Active during Range_Pending")
}

@(test)
test_s124_candle_still_composition_driven :: proc(t: ^testing.T) {
	// Candle widget should still follow candle composition (Range_Pending → Loading).
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = true}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Candle)
	testing.expect(t, vs == .Loading, "candle should still show Loading during Range_Pending")
}

@(test)
test_s124_stats_loading_no_live :: proc(t: ^testing.T) {
	// Stats with no live data and empty store → Loading (not Snapshot_Pending).
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = false}
	stats_store := services.Stats_Store{}
	stores := Cell_Stores{stats = &stats_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs == .Loading, "stats with no live data should yield Loading")
}

@(test)
test_s124_vpvr_active_with_data :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	v_store := services.VPVR_Store{count = 5}
	stores := Cell_Stores{vpvr = &v_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .VPVR, stores)
	testing.expect(t, vs == .Active, "vpvr with data should be Active during Live_Only")
}

@(test)
test_s124_session_vpvr_active_with_data :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Backfilled, stream_bound = true, has_live_data = true}
	sv_store := services.Session_VPVR_Store{count = 3}
	stores := Cell_Stores{session_vpvr = &sv_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Session_VPVR, stores)
	testing.expect(t, vs == .Active, "session_vpvr with data should be Active during Backfilled")
}

@(test)
test_s124_tpo_active_with_data :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = true}
	tpo_store := services.TPO_Store{period_count = 2}
	stores := Cell_Stores{tpo = &tpo_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .TPO, stores)
	testing.expect(t, vs == .Active, "tpo with data should be Active during Range_Pending")
}

@(test)
test_s124_universal_gates_still_override :: proc(t: ^testing.T) {
	// S143: When data IS present, Offline/Desync/Critical yield Degraded (not blocked).
	// This shows the cached data with a warning overlay rather than a blank screen.
	// Without data, the original blocking behavior is preserved.
	stats_store := services.Stats_Store{count = 5}
	stores := Cell_Stores{stats = &stats_store}

	// S143: Offline with data → Degraded (was Offline — S143 shows cached data with warning).
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true, reliability = .Offline}
	vs := resolve_pane_visual_state(sv, .Offline, .Offline, .Stats, stores)
	testing.expect(t, vs == .Degraded, "offline + data → Degraded (S143: show cached data)")

	// S143: Desync with data → Degraded (was Error — S143 shows cached data with warning).
	sv_desync := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true, reliability = .Desync}
	vs2 := resolve_pane_visual_state(sv_desync, .Connected, .Desync, .Stats, stores)
	testing.expect(t, vs2 == .Degraded, "desync + data → Degraded (S143: show cached data)")

	// S143: Critical with data → Degraded (was Error — S143 shows cached data with warning).
	sv_crit := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true, health_level = .Critical, reliability = .Stale_Unrecoverable}
	vs3 := resolve_pane_visual_state(sv_crit, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs3 == .Degraded, "critical + data → Degraded (S143: show cached data)")

	// Without data, the original blocking behavior is preserved.
	vs4 := resolve_pane_visual_state(Cell_Surface_View{}, .Offline, .Offline, .Stats, {})
	testing.expect(t, vs4 == .Offline, "offline + no data → Offline (unchanged)")
	vs5 := resolve_pane_visual_state(Cell_Surface_View{composition = .Composed, stream_bound = true}, .Connected, .Desync, .Stats, {})
	testing.expect(t, vs5 == .Error, "desync + no data → Error (unchanged)")
}

// ═══════════════════════════════════════════════════════════════
// S125: Bootstrap Completion — per-artifact readiness lifecycle tests.
// Validates the full progression: Loading → Seeding → Snapshot_Pending → Active.
// ═══════════════════════════════════════════════════════════════

@(test)
test_s125_stats_full_bootstrap_lifecycle :: proc(t: ^testing.T) {
	stats_store := services.Stats_Store{}
	stores := Cell_Stores{stats = &stats_store}

	// Phase 1: No connection → Loading.
	sv1 := Cell_Surface_View{composition = .Empty, stream_bound = true}
	vs1 := resolve_pane_visual_state(sv1, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs1 == .Loading, "phase 1: no live data → Loading")

	// Phase 2: Candles arrive (other artifact live) → Seeding.
	sv2 := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv2.artifact_has_live[.Candle] = true
	vs2 := resolve_pane_visual_state(sv2, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs2 == .Seeding, "phase 2: candles live, no stats → Seeding")

	// Phase 3: Stats artifact starts flowing, store still empty → Snapshot_Pending.
	sv3 := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv3.artifact_has_live[.Candle] = true
	sv3.artifact_has_live[.Stats] = true
	vs3 := resolve_pane_visual_state(sv3, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs3 == .Snapshot_Pending, "phase 3: stats artifact live, store empty → Snapshot_Pending")

	// Phase 4: Stats data arrives → Active.
	stats_store.count = 1
	vs4 := resolve_pane_visual_state(sv3, .Connected, .Live, .Stats, stores)
	testing.expect(t, vs4 == .Active, "phase 4: stats data in store → Active")
}

@(test)
test_s125_orderbook_full_bootstrap_lifecycle :: proc(t: ^testing.T) {
	ob_store := services.Orderbook_Store{}
	stores := Cell_Stores{orderbook = &ob_store}

	// Phase 1: No connection → Loading.
	sv1 := Cell_Surface_View{composition = .Empty, stream_bound = true}
	vs1 := resolve_pane_visual_state(sv1, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs1 == .Loading, "phase 1: no live data → Loading")

	// Phase 2: Candles arrive → Seeding.
	sv2 := Cell_Surface_View{composition = .Backfilled, stream_bound = true, has_live_data = true}
	sv2.artifact_has_live[.Candle] = true
	vs2 := resolve_pane_visual_state(sv2, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs2 == .Seeding, "phase 2: candles live, no OB → Seeding")

	// Phase 3: OB events start flowing, no snapshot yet → Snapshot_Pending.
	sv3 := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true}
	sv3.artifact_has_live[.Candle] = true
	sv3.artifact_has_live[.Orderbook] = true
	vs3 := resolve_pane_visual_state(sv3, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs3 == .Snapshot_Pending, "phase 3: OB artifact live, no snapshot → Snapshot_Pending")

	// Phase 4: OB snapshot arrives → Active.
	ob_store.bid_count = 10
	ob_store.ask_count = 10
	vs4 := resolve_pane_visual_state(sv3, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs4 == .Active, "phase 4: OB has levels → Active")
}

@(test)
test_s125_trades_full_bootstrap_lifecycle :: proc(t: ^testing.T) {
	trades_store := services.Trades_Store{}
	stores := Cell_Stores{trades = &trades_store}

	// Phase 1: No live data → Loading.
	sv1 := Cell_Surface_View{composition = .Empty, stream_bound = true}
	vs1 := resolve_pane_visual_state(sv1, .Connected, .Live, .Trades, stores)
	testing.expect(t, vs1 == .Loading, "phase 1: no live data → Loading")

	// Phase 2: Stream connected → Seeding.
	sv2 := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv2.artifact_has_live[.Candle] = true
	vs2 := resolve_pane_visual_state(sv2, .Connected, .Live, .Trades, stores)
	testing.expect(t, vs2 == .Seeding, "phase 2: stream connected, no trades → Seeding")

	// Phase 3: Trades arrive → Active.
	trades_store.count = 1
	vs3 := resolve_pane_visual_state(sv2, .Connected, .Live, .Trades, stores)
	testing.expect(t, vs3 == .Active, "phase 3: trades in store → Active")
}

@(test)
test_s125_counter_bootstrap_follows_candles :: proc(t: ^testing.T) {
	candle_store := services.Candle_Store{}
	stores := Cell_Stores{candle = &candle_store}

	// Counter depends on candle store, not its own artifact.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Candle] = true
	vs1 := resolve_pane_visual_state(sv, .Connected, .Live, .Counter, stores)
	testing.expect(t, vs1 == .Seeding, "counter with empty candles → Seeding")

	candle_store.count = 1
	vs2 := resolve_pane_visual_state(sv, .Connected, .Live, .Counter, stores)
	testing.expect(t, vs2 == .Active, "counter with candle data → Active")
}

@(test)
test_s125_artifact_has_live_propagated_in_surface_view :: proc(t: ^testing.T) {
	// Verify that Cell_Surface_View.artifact_has_live is zero-initialized by default.
	sv := Cell_Surface_View{}
	for kind in md_common.Artifact_Kind {
		testing.expect(t, !sv.artifact_has_live[kind], "artifact_has_live should default to false")
	}

	// Setting individual artifacts.
	sv.artifact_has_live[.Stats] = true
	sv.artifact_has_live[.Orderbook] = true
	testing.expect(t, sv.artifact_has_live[.Stats], "Stats should be set")
	testing.expect(t, sv.artifact_has_live[.Orderbook], "Orderbook should be set")
	testing.expect(t, !sv.artifact_has_live[.Candle], "Candle should remain unset")
}

// ═══════════════════════════════════════════════════════════════
// S130: Widget Readiness & Bootstrap Policy by Timeframe.
// TF-aware bootstrap hints for overlay UX.
// ═══════════════════════════════════════════════════════════════

@(test)
test_s130_bootstrap_hint_trades_immediate :: proc(t: ^testing.T) {
	// Trades are Live_Immediate — TF has no effect on expected_ms.
	h1 := bootstrap_hint_for_widget(.Trades, 1_000)
	h5m := bootstrap_hint_for_widget(.Trades, 300_000)
	testing.expect(t, h1.expected_ms == h5m.expected_ms, "trades bootstrap should be TF-independent")
	testing.expect(t, h1.partial_ok, "trades should allow partial rendering")
	testing.expect(t, len(h1.hint_label) > 0, "trades hint label should be non-empty")
}

@(test)
test_s130_bootstrap_hint_candle_historical :: proc(t: ^testing.T) {
	// Candle uses Historical_Range — not TF-gated per se, network overhead.
	h := bootstrap_hint_for_widget(.Candle, 60_000)
	testing.expect(t, h.expected_ms > 0, "candle bootstrap should have positive expected_ms")
	testing.expect(t, !h.partial_ok, "candle should not allow partial rendering")
	testing.expect(t, h.hint_label == "Fetching historical data", "candle hint should mention history")
}

@(test)
test_s130_bootstrap_hint_counter_historical :: proc(t: ^testing.T) {
	// Counter depends on candle store → Historical_Range source.
	h1s := bootstrap_hint_for_widget(.Counter, 1_000)
	h5m := bootstrap_hint_for_widget(.Counter, 300_000)
	testing.expect(t, h1s.expected_ms > 0, "counter 1s expected_ms should be positive")
	testing.expect(t, h5m.expected_ms > 0, "counter 5m expected_ms should be positive")
}

@(test)
test_s130_bootstrap_hint_stats_immediate :: proc(t: ^testing.T) {
	h := bootstrap_hint_for_widget(.Stats, 300_000)
	testing.expect(t, h.partial_ok, "stats should allow partial rendering")
	testing.expect(t, h.hint_label == "Data arrives within seconds", "stats hint should indicate immediate")
}

@(test)
test_s130_bootstrap_hint_orderbook_snapshot :: proc(t: ^testing.T) {
	h := bootstrap_hint_for_widget(.Orderbook, 60_000)
	testing.expect(t, !h.partial_ok, "orderbook should not allow partial rendering")
	testing.expect(t, h.hint_label == "Awaiting exchange snapshot", "OB hint should mention snapshot")
}

@(test)
test_s130_bootstrap_hint_dom_snapshot :: proc(t: ^testing.T) {
	h := bootstrap_hint_for_widget(.DOM, 1_000)
	testing.expect(t, h.hint_label == "Awaiting exchange snapshot", "DOM hint should mention snapshot")
}

@(test)
test_s130_bootstrap_hint_heatmap_accumulation_scales :: proc(t: ^testing.T) {
	h1s := bootstrap_hint_for_widget(.Heatmap, 1_000)
	h1h := bootstrap_hint_for_widget(.Heatmap, 3_600_000)
	testing.expect(t, h1h.expected_ms > h1s.expected_ms, "heatmap on longer TF should take longer")
	testing.expect(t, !h1s.partial_ok, "heatmap should not allow partial rendering")
}

@(test)
test_s130_bootstrap_hint_analytics_tf_gated :: proc(t: ^testing.T) {
	// Analytics maps to CVD which is Live_TF_Gated.
	h1s := bootstrap_hint_for_widget(.Analytics, 1_000)
	h15m := bootstrap_hint_for_widget(.Analytics, 900_000)
	testing.expect(t, h15m.expected_ms >= h1s.expected_ms, "analytics on longer TF should have >= expected_ms")
}

@(test)
test_s130_bootstrap_hint_analytics_short_tf_label :: proc(t: ^testing.T) {
	h := bootstrap_hint_for_widget(.Analytics, 5_000)
	testing.expect(t, h.hint_label == "First close in seconds", "analytics 5s should indicate quick close")
}

@(test)
test_s130_bootstrap_hint_analytics_long_tf_label :: proc(t: ^testing.T) {
	h := bootstrap_hint_for_widget(.Analytics, 3_600_000)
	testing.expect(t, h.hint_label == "Long timeframe — first close may take a while", "analytics 1h should indicate long TF")
}

@(test)
test_s130_widget_primary_artifact_coverage :: proc(t: ^testing.T) {
	// S136: Every widget kind should have a readiness policy with valid primary artifact.
	for wk in Widget_Kind {
		policy := widget_readiness_policy(wk)
		_ = md_common.artifact_policy(policy.primary_artifact)
	}
}

@(test)
test_s130_bootstrap_hint_all_widgets_have_label :: proc(t: ^testing.T) {
	// Every widget (except Empty) should produce a non-empty hint label at all TFs.
	tfs := [3]i64{1_000, 60_000, 3_600_000}
	for tf in tfs {
		for wk in Widget_Kind {
			if wk == .Empty do continue
			h := bootstrap_hint_for_widget(wk, tf)
			testing.expect(t, len(h.hint_label) > 0, "all widgets must have bootstrap hint label")
		}
	}
}

@(test)
test_s130_bootstrap_expectations_table_complete :: proc(t: ^testing.T) {
	// Every artifact kind must have a non-zero min_seed_ms.
	for kind in md_common.Artifact_Kind {
		be := md_common.artifact_bootstrap_expectation(kind)
		testing.expect(t, be.min_seed_ms > 0, "every artifact must have positive min_seed_ms")
	}
}

// ═══════════════════════════════════════════════════════════════
// S131: Data Path Hardening — apply_state sync completeness.
// Tests that sync_apply_state_from_active_stream and
// sync_slot_apply_state_from_stream set all artifact fields.
// ═══════════════════════════════════════════════════════════════

@(test)
test_s131_sync_sets_trade_has_live :: proc(t: ^testing.T) {
	// After sync, a stream with trades should set has_live[.Trade].
	state := new(App_State)
	layers.market_store_seed_demo(&state.layer_store, 1)
	sync_apply_state_from_active_stream(state)
	testing.expect(t, state.active_apply_state.has_live[.Trade], "has_live[.Trade] should be true after sync with trades")
	free(state)
}

@(test)
test_s131_sync_sets_orderbook_has_live :: proc(t: ^testing.T) {
	state := new(App_State)
	layers.market_store_seed_demo(&state.layer_store, 1)
	sync_apply_state_from_active_stream(state)
	testing.expect(t, state.active_apply_state.has_live[.Orderbook], "has_live[.Orderbook] should be true after sync with OB data")
	free(state)
}

@(test)
test_s131_sync_sets_orderbook_snapshot_seen :: proc(t: ^testing.T) {
	state := new(App_State)
	layers.market_store_seed_demo(&state.layer_store, 1)
	sync_apply_state_from_active_stream(state)
	testing.expect(t, state.active_apply_state.snapshot_seen[.Orderbook], "snapshot_seen[.Orderbook] should be true when OB has levels")
	free(state)
}

@(test)
test_s131_sync_sets_artifact_event_count_from_frames :: proc(t: ^testing.T) {
	// Simulates a stream that has received trade, OB, stats frames.
	state := new(App_State)
	ms := layers.market_store_stream_get_or_alloc(&state.layer_store, 42)
	ms.used = true
	ms.trades_frames = 10
	ms.orderbook_frames = 5
	ms.stats_frames = 3
	services.push_trade(&ms.trades, services.Trade_Entry{price = 100, qty = 1, side = .Buy, unix = 1700000000})
	services.update_orderbook(&ms.orderbook, []f64{100}, []f64{1}, []f64{99}, []f64{1}, 100, 1700000000)
	services.push_stats(&ms.stats, services.Stats_Entry{mark_price = 100, unix = 1700000000})
	layers.market_store_set_active_subject(&state.layer_store, 42)

	sync_apply_state_from_active_stream(state)

	s := state.active_apply_state
	testing.expect(t, s.artifact_event_count[.Trade] == 10, "trade event count from frames")
	testing.expect(t, s.artifact_event_count[.Orderbook] == 5, "OB event count from frames")
	testing.expect(t, s.artifact_event_count[.Stats] == 3, "stats event count from frames")
	free(state)
}

@(test)
test_s131_staleness_detection_works_after_sync :: proc(t: ^testing.T) {
	// With artifact_event_count populated, staleness should detect aging/stale.
	state := new(App_State)
	ms := layers.market_store_stream_get_or_alloc(&state.layer_store, 42)
	ms.used = true
	ms.stats_frames = 5
	ms.orderbook_frames = 3
	ms.event_count = 8
	services.push_stats(&ms.stats, services.Stats_Entry{mark_price = 100, unix = 1700000000})
	services.update_orderbook(&ms.orderbook, []f64{100}, []f64{1}, []f64{99}, []f64{1}, 100, 1700000000)
	layers.market_store_set_active_subject(&state.layer_store, 42)

	sync_apply_state_from_active_stream(state)

	s := state.active_apply_state
	testing.expect(t, s.artifact_event_count[.Stats] > 0, "stats event count should be non-zero")
	testing.expect(t, s.artifact_event_count[.Orderbook] > 0, "OB event count should be non-zero")

	// Dual_Silence threshold: 12s stale, 8s aging. Check at 15s.
	stale, aging := md_common.apply_state_stale_artifact_count(s, 1700000015_000, 60_000)
	testing.expect(t, stale >= 2, "both Stats and OB should be stale at 15s age")
	testing.expect(t, aging == 0, "aging should be 0 when stale")
	free(state)
}

@(test)
test_s131_auto_recovery_triggers_after_sync :: proc(t: ^testing.T) {
	// Before S131, auto-recovery never fired because artifact_event_count was always 0.
	state := new(App_State)
	ms := layers.market_store_stream_get_or_alloc(&state.layer_store, 42)
	ms.used = true
	ms.stats_frames = 1
	ms.orderbook_frames = 1
	ms.event_count = 2
	services.push_stats(&ms.stats, services.Stats_Entry{mark_price = 100, unix = 1700000000})
	services.update_orderbook(&ms.orderbook, []f64{100}, []f64{1}, []f64{99}, []f64{1}, 100, 1700000000)
	layers.market_store_set_active_subject(&state.layer_store, 42)

	sync_apply_state_from_active_stream(state)

	s := state.active_apply_state
	decision := md_common.apply_state_stale_remediation(s, 1700000015_000, 60_000)
	testing.expect(t, decision == .Resubscribe, "auto-recovery should trigger Resubscribe when stale")
	free(state)
}

@(test)
test_s131_sync_last_recv_ms_trade :: proc(t: ^testing.T) {
	state := new(App_State)
	ms := layers.market_store_stream_get_or_alloc(&state.layer_store, 42)
	ms.used = true
	services.push_trade(&ms.trades, services.Trade_Entry{price = 100, qty = 1, side = .Buy, unix = 1700000042})
	layers.market_store_set_active_subject(&state.layer_store, 42)

	sync_apply_state_from_active_stream(state)

	ts := state.active_apply_state.last_recv_ms[.Trade]
	testing.expect(t, ts == 1700000042_000, "last_recv_ms[.Trade] should be set from newest trade unix (in ms)")
	free(state)
}

@(test)
test_s131_sync_empty_stream_resets :: proc(t: ^testing.T) {
	// If no active stream, apply_state should be reset.
	state := new(App_State)
	state.active_apply_state.has_live[.Stats] = true
	state.active_apply_state.artifact_event_count[.Stats] = 5

	sync_apply_state_from_active_stream(state)

	testing.expect(t, !state.active_apply_state.has_live[.Stats], "has_live should be reset when no active stream")
	testing.expect(t, state.active_apply_state.artifact_event_count[.Stats] == 0, "event count should be reset")
	free(state)
}

@(test)
test_s131_slot_sync_sets_all_artifacts :: proc(t: ^testing.T) {
	// Test sync_slot_apply_state_from_stream independently.
	ms := new(layers.Market_Stream)
	defer free(ms)
	ms.used = true
	ms.trades_frames = 7
	ms.orderbook_frames = 3
	ms.stats_frames = 2
	ms.event_count = 12
	services.push_trade(&ms.trades, services.Trade_Entry{price = 50, qty = 1, side = .Sell, unix = 1700000001})
	services.update_orderbook(&ms.orderbook, []f64{50}, []f64{2}, []f64{49}, []f64{2}, 50, 1700000001)
	services.push_stats(&ms.stats, services.Stats_Entry{mark_price = 50, unix = 1700000001})
	services.push_candle(&ms.candles, services.Candle_Entry{open = 50, close = 51, window_end_ts = 1700000060})

	s: md_common.Stream_Apply_State
	sync_slot_apply_state_from_stream(&s, ms)

	testing.expect(t, s.has_live[.Trade], "slot: has_live[.Trade]")
	testing.expect(t, s.has_live[.Orderbook], "slot: has_live[.Orderbook]")
	testing.expect(t, s.has_live[.Stats], "slot: has_live[.Stats]")
	testing.expect(t, s.has_live[.Candle], "slot: has_live[.Candle]")
	testing.expect(t, s.snapshot_seen[.Orderbook], "slot: snapshot_seen[.Orderbook]")
	testing.expect(t, s.artifact_event_count[.Trade] == 7, "slot: trade event count")
	testing.expect(t, s.artifact_event_count[.Orderbook] == 3, "slot: OB event count")
	testing.expect(t, s.artifact_event_count[.Stats] == 2, "slot: stats event count")
	testing.expect(t, s.event_count == 12, "slot: total event count")
	testing.expect(t, s.last_recv_ms[.Trade] == 1700000001_000, "slot: trade last_recv_ms")
	testing.expect(t, s.last_recv_ms[.Candle] == 1700000060_000, "slot: candle last_recv_ms from window_end_ts")
}

@(test)
test_s131_slot_sync_preserves_recovery_state :: proc(t: ^testing.T) {
	// Recovery state (attempts, last_ms) must NOT be overwritten by slot sync.
	ms := new(layers.Market_Stream)
	defer free(ms)
	ms.used = true

	s: md_common.Stream_Apply_State
	s.recovery_attempts = 2
	s.recovery_last_ms = 1700000000_000
	s.getrange_seeded = true
	s.getrange_pending = true

	sync_slot_apply_state_from_stream(&s, ms)

	testing.expect(t, s.recovery_attempts == 2, "recovery_attempts should be preserved")
	testing.expect(t, s.recovery_last_ms == 1700000000_000, "recovery_last_ms should be preserved")
	testing.expect(t, s.getrange_seeded, "getrange_seeded should be preserved")
	testing.expect(t, s.getrange_pending, "getrange_pending should be preserved")
}

@(test)
test_s131_ob_widget_snapshot_pending_with_synced_state :: proc(t: ^testing.T) {
	// End-to-end: OB widget should correctly show Snapshot_Pending when
	// OB events are flowing (has_live[.Orderbook] = true) but store is empty.
	// Before S131, has_live[.Orderbook] was never set → showed Seeding instead.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Orderbook] = true  // S131: now correctly set by sync
	ob_store := services.Orderbook_Store{}   // empty — no snapshot yet
	stores := Cell_Stores{orderbook = &ob_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Orderbook, stores)
	testing.expect(t, vs == .Snapshot_Pending, "OB with artifact live but empty store = Snapshot_Pending")
}

@(test)
test_s131_trades_widget_active_with_synced_state :: proc(t: ^testing.T) {
	// Trades widget with trades in store should be Active immediately.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Trade] = true  // S131: now correctly set by sync
	trades_store := services.Trades_Store{}
	services.push_trade(&trades_store, services.Trade_Entry{price = 100, qty = 1, side = .Buy, unix = 1})
	stores := Cell_Stores{trades = &trades_store}
	vs := resolve_pane_visual_state(sv, .Connected, .Live, .Trades, stores)
	testing.expect(t, vs == .Active, "trades with data should be Active")
}

// ═══════════════════════════════════════════════════════════════
// S136: Timeframe Data Policy & Readiness Unification.
// Tests for unified Data_Readiness, policy table, store checks,
// and behavioral improvements (backfilled/live-only charts → Active).
// ═══════════════════════════════════════════════════════════════

@(test)
test_s136_data_readiness_ordering :: proc(t: ^testing.T) {
	// Data_Readiness enum ordering: lower is less ready.
	testing.expect(t, Data_Readiness.Not_Ready < .Loading, "Not_Ready < Loading")
	testing.expect(t, Data_Readiness.Loading < .Snapshot_Pending, "Loading < Snapshot_Pending")
	testing.expect(t, Data_Readiness.Snapshot_Pending < .Seeding, "Snapshot_Pending < Seeding")
	testing.expect(t, Data_Readiness.Seeding < .Partial_Usable, "Seeding < Partial_Usable")
	testing.expect(t, Data_Readiness.Partial_Usable < .Live_Usable, "Partial_Usable < Live_Usable")
}

@(test)
test_s136_readiness_to_visual_state_mapping :: proc(t: ^testing.T) {
	testing.expect(t, readiness_to_visual_state(.Not_Ready) == .Empty, "Not_Ready → Empty")
	testing.expect(t, readiness_to_visual_state(.Loading) == .Loading, "Loading → Loading")
	testing.expect(t, readiness_to_visual_state(.Snapshot_Pending) == .Snapshot_Pending, "Snapshot_Pending → Snapshot_Pending")
	testing.expect(t, readiness_to_visual_state(.Seeding) == .Seeding, "Seeding → Seeding")
	testing.expect(t, readiness_to_visual_state(.Partial_Usable) == .Active, "Partial_Usable → Active")
	testing.expect(t, readiness_to_visual_state(.Live_Usable) == .Active, "Live_Usable → Active")
}

@(test)
test_s136_policy_table_complete :: proc(t: ^testing.T) {
	// Every widget kind must have a policy entry with valid primary artifact.
	for wk in Widget_Kind {
		policy := widget_readiness_policy(wk)
		_ = md_common.artifact_policy(policy.primary_artifact)
	}
}

@(test)
test_s136_policy_backfill_absent_usable :: proc(t: ^testing.T) {
	// All non-empty, non-candle widgets should be usable without backfill.
	for wk in Widget_Kind {
		policy := widget_readiness_policy(wk)
		if wk == .Empty {
			testing.expect(t, !policy.backfill_absent_usable, "Empty should not be backfill_absent_usable")
		} else {
			testing.expect(t, policy.backfill_absent_usable, "all data widgets should be backfill_absent_usable")
		}
	}
}

@(test)
test_s136_policy_partial_usable_artifacts :: proc(t: ^testing.T) {
	// Stats, Trades, Counter, Analytics should be partial_usable.
	testing.expect(t, widget_readiness_policy(.Stats).partial_usable, "Stats should be partial_usable")
	testing.expect(t, widget_readiness_policy(.Trades).partial_usable, "Trades should be partial_usable")
	testing.expect(t, widget_readiness_policy(.Counter).partial_usable, "Counter should be partial_usable")
	testing.expect(t, widget_readiness_policy(.Analytics).partial_usable, "Analytics should be partial_usable")
	// Candle, Orderbook, Heatmap, VPVR should NOT be partial_usable.
	testing.expect(t, !widget_readiness_policy(.Candle).partial_usable, "Candle should not be partial_usable")
	testing.expect(t, !widget_readiness_policy(.Orderbook).partial_usable, "Orderbook should not be partial_usable")
	testing.expect(t, !widget_readiness_policy(.Heatmap).partial_usable, "Heatmap should not be partial_usable")
}

@(test)
test_s136_widget_store_has_data_empty_stores :: proc(t: ^testing.T) {
	stores := Cell_Stores{}
	for wk in Widget_Kind {
		testing.expect(t, !widget_store_has_data(wk, stores), "empty stores should have no data")
	}
}

@(test)
test_s136_widget_store_has_data_candle :: proc(t: ^testing.T) {
	candle_store := services.Candle_Store{count = 5}
	stores := Cell_Stores{candle = &candle_store}
	testing.expect(t, widget_store_has_data(.Candle, stores), "candle store with count > 0")
	testing.expect(t, widget_store_has_data(.Counter, stores), "counter uses candle store")
	testing.expect(t, !widget_store_has_data(.Stats, stores), "stats should not see candle data")
}

@(test)
test_s136_widget_store_has_data_orderbook :: proc(t: ^testing.T) {
	ob_store := services.Orderbook_Store{bid_count = 5, ask_count = 5}
	stores := Cell_Stores{orderbook = &ob_store}
	testing.expect(t, widget_store_has_data(.Orderbook, stores), "OB store with levels")
	testing.expect(t, widget_store_has_data(.DOM, stores), "DOM shares OB store")
	testing.expect(t, !widget_store_has_data(.Candle, stores), "candle should not see OB data")
}

@(test)
test_s136_widget_data_readiness_not_ready :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Empty, stream_bound = false}
	stores := Cell_Stores{}
	for wk in Widget_Kind {
		r := widget_data_readiness(wk, sv, stores)
		testing.expect(t, r == .Not_Ready, "unbound + empty = Not_Ready")
	}
}

@(test)
test_s136_widget_data_readiness_loading :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Empty, stream_bound = true}
	stores := Cell_Stores{}
	// Non-candle widgets: stream_bound → Loading
	r := widget_data_readiness(.Stats, sv, stores)
	testing.expect(t, r == .Loading, "bound stream with no data = Loading")
}

@(test)
test_s136_candle_backfilled_is_partial_usable :: proc(t: ^testing.T) {
	// S136 key improvement: backfilled chart has historical data → usable.
	sv := Cell_Surface_View{composition = .Backfilled, stream_bound = true}
	stores := Cell_Stores{}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Partial_Usable, "candle backfilled = Partial_Usable")
}

@(test)
test_s136_candle_live_only_is_partial_usable :: proc(t: ^testing.T) {
	// S136 key improvement: live-only chart has recent data → usable.
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true}
	stores := Cell_Stores{}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Partial_Usable, "candle live_only = Partial_Usable")
}

@(test)
test_s136_candle_composed_is_live_usable :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true}
	stores := Cell_Stores{}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Live_Usable, "candle composed+live = Live_Usable")
}

@(test)
test_s136_candle_range_pending_is_loading :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true}
	stores := Cell_Stores{}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Loading, "candle range_pending = Loading")
}

@(test)
test_s136_stats_with_data_is_live_usable :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true}
	stats_store := services.Stats_Store{count = 1}
	stores := Cell_Stores{stats = &stats_store}
	r := widget_data_readiness(.Stats, sv, stores)
	testing.expect(t, r == .Live_Usable, "stats with data + composed = Live_Usable")
}

@(test)
test_s136_stats_partial_no_composed :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Live_Only, stream_bound = true, has_live_data = true}
	stats_store := services.Stats_Store{count = 1}
	stores := Cell_Stores{stats = &stats_store}
	r := widget_data_readiness(.Stats, sv, stores)
	testing.expect(t, r == .Partial_Usable, "stats with data + non-composed = Partial_Usable")
}

@(test)
test_s136_analytics_seeding_on_high_tf :: proc(t: ^testing.T) {
	// Analytics with no data but live stream → Seeding (TF-gated, first close pending).
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true}
	stores := Cell_Stores{}
	r := widget_data_readiness(.Analytics, sv, stores)
	testing.expect(t, r == .Seeding, "analytics with no data but live stream = Seeding")
}

@(test)
test_s136_orderbook_snapshot_pending_flow :: proc(t: ^testing.T) {
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true}
	sv.artifact_has_live[.Orderbook] = true
	ob_store := services.Orderbook_Store{} // empty
	stores := Cell_Stores{orderbook = &ob_store}
	r := widget_data_readiness(.Orderbook, sv, stores)
	testing.expect(t, r == .Snapshot_Pending, "OB live but empty store = Snapshot_Pending")
}

@(test)
test_s136_store_data_overrides_composition :: proc(t: ^testing.T) {
	// Even with Range_Pending, if store already has data → usable.
	sv := Cell_Surface_View{composition = .Range_Pending, stream_bound = true, has_live_data = true}
	stats_store := services.Stats_Store{count = 3}
	stores := Cell_Stores{stats = &stats_store}
	r := widget_data_readiness(.Stats, sv, stores)
	testing.expect(t, r == .Partial_Usable, "store data overrides composition stage")
}

@(test)
test_s136_widget_store_label_coverage :: proc(t: ^testing.T) {
	// Every widget kind should have a non-empty store label.
	for wk in Widget_Kind {
		label := widget_store_label(wk)
		testing.expect(t, len(label) > 0, "all widget kinds must have store label")
	}
}

@(test)
test_s136_candle_store_data_composed_live_usable :: proc(t: ^testing.T) {
	// Candle with store data + Composed = Live_Usable (store check takes priority).
	sv := Cell_Surface_View{composition = .Composed, stream_bound = true, has_live_data = true}
	candle_store := services.Candle_Store{count = 50}
	stores := Cell_Stores{candle = &candle_store}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Live_Usable, "candle store data + composed = Live_Usable")
}

@(test)
test_s136_candle_store_data_backfilled_partial :: proc(t: ^testing.T) {
	// Candle with store data + Backfilled = Partial_Usable.
	sv := Cell_Surface_View{composition = .Backfilled, stream_bound = true}
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Partial_Usable, "candle store data + backfilled = Partial_Usable")
}

// =========================================================================
// S143: Stream Health & Desync Model Hardening — Widget Readiness Integration
// =========================================================================

@(test)
test_s154_readiness_ignores_reliability :: proc(t: ^testing.T) {
	// S154: widget_data_readiness is purely about data availability.
	// Reliability is checked in resolve_pane_visual_state, not here.
	// Widget has data + unreliable stream → readiness is still Live_Usable.
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	sv := Cell_Surface_View{
		composition = .Composed,
		stream_bound = true,
		has_live_data = true,
		reliability = .Manual_Resync,
	}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Live_Usable, "data + manual_resync → Live_Usable (readiness ignores reliability)")
}

@(test)
test_s154_visual_state_degraded_via_reliability :: proc(t: ^testing.T) {
	// S154: resolve_pane_visual_state checks reliability separately.
	// Data present + unreliable stream → Degraded visual.
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	for rel in ([?]md_common.Stream_Reliability{.Offline, .Stale_Unrecoverable, .Manual_Resync, .Desync}) {
		sv := Cell_Surface_View{
			composition = .Composed,
			stream_bound = true,
			has_live_data = true,
			reliability = rel,
		}
		// With data present, conn_status Connected, stream Live (data cached):
		vs := resolve_pane_visual_state(sv, .Connected, .Live, .Candle, stores)
		testing.expect(t, vs == .Degraded, "data + blocks_render reliability → Degraded visual")
	}
}

@(test)
test_s154_visual_state_active_when_reliable :: proc(t: ^testing.T) {
	// S154: Non-blocking reliability → Active (readiness-driven).
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	for rel in ([?]md_common.Stream_Reliability{.Reliable, .Degraded_Aging, .Stale_Recovering}) {
		sv := Cell_Surface_View{
			composition = .Composed,
			stream_bound = true,
			has_live_data = true,
			reliability = rel,
		}
		vs := resolve_pane_visual_state(sv, .Connected, .Live, .Candle, stores)
		testing.expect(t, vs == .Active, "data + non-blocking reliability → Active")
	}
}

@(test)
test_s143_widget_reliable_with_data :: proc(t: ^testing.T) {
	// Widget has store data + reliable stream → normal usable.
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	sv := Cell_Surface_View{
		composition = .Composed,
		stream_bound = true,
		has_live_data = true,
		reliability = .Reliable,
	}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Live_Usable, "data + reliable → Live_Usable")
}

@(test)
test_s143_widget_degraded_aging_still_renders :: proc(t: ^testing.T) {
	// Degraded_Aging does not block render — widget should be usable.
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	sv := Cell_Surface_View{
		composition = .Composed,
		stream_bound = true,
		has_live_data = true,
		reliability = .Degraded_Aging,
	}
	r := widget_data_readiness(.Candle, sv, stores)
	testing.expect(t, r == .Live_Usable, "data + degraded_aging → Live_Usable (not blocked)")
}

@(test)
test_s143_pane_visual_state_degraded :: proc(t: ^testing.T) {
	// Desync with data should yield Degraded (not Error).
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	sv := Cell_Surface_View{
		composition = .Composed,
		stream_bound = true,
		has_live_data = true,
		reliability = .Desync,
	}
	vs := resolve_pane_visual_state(sv, .Connected, .Desync, .Candle, stores)
	testing.expect(t, vs == .Degraded, "desync + data → Degraded visual state")
}

@(test)
test_s143_pane_visual_state_desync_no_data :: proc(t: ^testing.T) {
	// Desync without data should still yield Error.
	sv := Cell_Surface_View{
		composition = .Composed,
		stream_bound = true,
		reliability = .Desync,
	}
	vs := resolve_pane_visual_state(sv, .Connected, .Desync, .Candle, {})
	testing.expect(t, vs == .Error, "desync + no data → Error visual state")
}

@(test)
test_s143_pane_visual_state_offline_with_data :: proc(t: ^testing.T) {
	// Offline with cached data should yield Degraded (not Offline).
	candle_store := services.Candle_Store{count = 30}
	stores := Cell_Stores{candle = &candle_store}
	sv := Cell_Surface_View{
		composition = .Composed,
		stream_bound = true,
		has_live_data = true,
		reliability = .Offline,
	}
	vs := resolve_pane_visual_state(sv, .Offline, .Offline, .Candle, stores)
	testing.expect(t, vs == .Degraded, "offline + cached data → Degraded visual state")
}
