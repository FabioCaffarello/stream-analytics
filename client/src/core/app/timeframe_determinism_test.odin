package app

import "core:testing"
import "mr:layers"
import "mr:services"

// ═══════════════════════════════════════════════════════════════
// S115: Timeframe Determinism Closure Pack
// Validates deterministic behavior across all TF switching paths:
//   • Global TF changes (1s ↔ 5s ↔ 1m ↔ 5m ↔ 15m)
//   • Per-cell TF overrides
//   • Compare mode per-pane TF overrides
//   • Rapid switching, multi-pane isolation, persistence round-trips
// ═══════════════════════════════════════════════════════════════

// ---------------------------------------------------------------------------
// 1. Effective TF Resolution — Deterministic Hierarchy
// ---------------------------------------------------------------------------

// S115: cell_effective_tf_idx returns per-cell TF when set.
@(test)
test_s115_effective_tf_per_cell_wins :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.count = 1
	state.active_tf_idx = 2  // 1m
	state.world.timeframes[0].tf_idx = 4  // 15m

	testing.expect_value(t, cell_effective_tf_idx(state, 0), 4)
	testing.expect_value(t, cell_effective_tf_string(state, 0), "15m")
	testing.expect_value(t, cell_effective_tf_ms(state, 0), i64(900_000))
}

// S115: cell_effective_tf_idx falls back to global when per-cell = -1.
@(test)
test_s115_effective_tf_fallback_global :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.count = 1
	state.active_tf_idx = 3  // 5m
	state.world.timeframes[0].tf_idx = -1

	testing.expect_value(t, cell_effective_tf_idx(state, 0), 3)
	testing.expect_value(t, cell_effective_tf_string(state, 0), "5m")
	testing.expect_value(t, cell_effective_tf_ms(state, 0), i64(300_000))
}

// S115: Compare pane per-pane TF takes precedence over global.
@(test)
test_s115_compare_pane_tf_wins :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.active_tf_idx = 2  // 1m
	state.compare.active = true
	state.compare.count = 2
	state.compare.tf_idx[0] = 1  // 5s
	state.compare.tf_idx[1] = -1  // follow global

	testing.expect_value(t, compare_pane_effective_tf_idx(state, 0), 1)
	testing.expect_value(t, compare_pane_effective_tf_string(state, 0), "5s")
	testing.expect_value(t, compare_pane_effective_tf_idx(state, 1), 2)
	testing.expect_value(t, compare_pane_effective_tf_string(state, 1), "1m")
}

// S115: Out-of-range compare pane index falls back to global.
@(test)
test_s115_compare_pane_tf_out_of_range_fallback :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.active_tf_idx = 5  // 30m

	testing.expect_value(t, compare_pane_effective_tf_idx(state, -1), 5)
	testing.expect_value(t, compare_pane_effective_tf_idx(state, 4), 5)
	testing.expect_value(t, compare_pane_effective_tf_idx(state, 99), 5)
}

// ---------------------------------------------------------------------------
// 2. Global TF Switching — Store Clearing Determinism
// ---------------------------------------------------------------------------

// S115: Sequential global TF switches (1m→5m→15m) clear stores each time.
@(test)
test_s115_sequential_global_tf_switch :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 0  // 1s
	state.world.timeframes[0].tf_idx = -1

	// Bind cell 0.
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	state.world.bindings[0].stream_idx = 0
	slot := &state.stream_views.slots[0]
	slot.used = true
	slot.subject_id = 100

	// Switch 1s → 5s (idx 1).
	slot.candle_store.head = 5
	slot.candle_store.count = 10
	apply_set_timeframe_action(state, 1)
	testing.expect_value(t, state.active_tf_idx, 1)
	testing.expect_value(t, slot.candle_store.head, 0)
	testing.expect_value(t, slot.candle_store.count, 0)

	// Switch 5s → 1m (idx 2).
	slot.candle_store.head = 3
	slot.candle_store.count = 7
	apply_set_timeframe_action(state, 2)
	testing.expect_value(t, state.active_tf_idx, 2)
	testing.expect_value(t, slot.candle_store.head, 0)
	testing.expect_value(t, slot.candle_store.count, 0)

	// Switch 1m → 5m (idx 3).
	slot.candle_store.head = 1
	slot.candle_store.count = 4
	apply_set_timeframe_action(state, 3)
	testing.expect_value(t, state.active_tf_idx, 3)
	testing.expect_value(t, slot.candle_store.head, 0)
	testing.expect_value(t, slot.candle_store.count, 0)

	// Switch 5m → 15m (idx 4).
	slot.candle_store.head = 2
	slot.candle_store.count = 6
	apply_set_timeframe_action(state, 4)
	testing.expect_value(t, state.active_tf_idx, 4)
	testing.expect_value(t, slot.candle_store.head, 0)
	testing.expect_value(t, slot.candle_store.count, 0)
}

// S115: Global TF switch resets scroll/zoom for all global-following cells.
@(test)
test_s115_global_tf_resets_scroll_zoom :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 3
	state.active_tf_idx = 2
	// Cell 0, 1: follow global. Cell 2: per-cell TF.
	state.world.timeframes[0].tf_idx = -1
	state.world.timeframes[1].tf_idx = -1
	state.world.timeframes[2].tf_idx = 5  // 30m override

	state.world.views[0].candle_scroll_x = 100.0
	state.world.views[0].candle_zoom = 2.0
	state.world.views[1].candle_scroll_x = 50.0
	state.world.views[1].candle_zoom = 1.5
	state.world.views[2].candle_scroll_x = 200.0
	state.world.views[2].candle_zoom = 3.0

	apply_set_timeframe_action(state, 4)

	// Global followers should be reset.
	testing.expect_value(t, state.world.views[0].candle_scroll_x, f32(0))
	testing.expect_value(t, state.world.views[0].candle_zoom, f32(0))
	testing.expect_value(t, state.world.views[1].candle_scroll_x, f32(0))
	testing.expect_value(t, state.world.views[1].candle_zoom, f32(0))
	// Per-cell TF cell should be untouched.
	testing.expect_value(t, state.world.views[2].candle_scroll_x, f32(200.0))
	testing.expect_value(t, state.world.views[2].candle_zoom, f32(3.0))
}

// S115: Global TF switch resets getrange state for all global-following cells.
@(test)
test_s115_global_tf_resets_getrange :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 2
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.world.timeframes[1].tf_idx = 4  // per-cell

	// Seed getrange states.
	state.world.getranges[0].pending = true
	state.world.getranges[0].seeded = true
	state.world.getranges[0].oldest_ts = 1000
	state.world.getranges[1].pending = true
	state.world.getranges[1].seeded = true
	state.world.getranges[1].oldest_ts = 2000

	apply_set_timeframe_action(state, 3)

	// Global-follower cell 0 should have getrange reset.
	testing.expect_value(t, state.world.getranges[0].pending, false)
	testing.expect_value(t, state.world.getranges[0].seeded, false)
	testing.expect_value(t, state.world.getranges[0].oldest_ts, i64(0))
	// Per-cell TF cell 1 should be untouched.
	testing.expect_value(t, state.world.getranges[1].pending, true)
	testing.expect_value(t, state.world.getranges[1].seeded, true)
	testing.expect_value(t, state.world.getranges[1].oldest_ts, i64(2000))
}

// S115: Global TF switch clears candle health.
@(test)
test_s115_global_tf_clears_candle_health :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 0
	state.active_tf_idx = 2
	state.candle_health = .OK

	apply_set_timeframe_action(state, 4)
	testing.expect_value(t, state.candle_health, Candle_Health.No_Data)
}

// S115: Global TF switch clears timeline state.
@(test)
test_s115_global_tf_clears_timeline :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 0
	state.active_tf_idx = 2
	state.timeline.first_ts = 1000
	state.timeline.last_ts = 5000

	apply_set_timeframe_action(state, 4)
	testing.expect_value(t, state.timeline.first_ts, i64(0))
	testing.expect_value(t, state.timeline.last_ts, i64(0))
}

// S115: Global TF switch no-ops when already at target TF.
@(test)
test_s115_global_tf_noop_same :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 0
	state.active_tf_idx = 3
	result := apply_set_timeframe_action(state, 3)
	testing.expect(t, !result, "same TF should be no-op")
}

// S115: Global TF switch rejects out-of-range indices.
@(test)
test_s115_global_tf_rejects_invalid :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 0
	state.active_tf_idx = 2

	testing.expect(t, !apply_set_timeframe_action(state, -1), "negative idx rejected")
	testing.expect(t, !apply_set_timeframe_action(state, 9), "idx >= len(TF_OPTIONS) rejected")
	testing.expect_value(t, state.active_tf_idx, 2)
}

// ---------------------------------------------------------------------------
// 3. Per-Cell TF Switching — Isolation & Store Clearing
// ---------------------------------------------------------------------------

// S115: Per-cell TF change resets cell's getrange state.
@(test)
test_s115_per_cell_tf_resets_getrange :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.world.getranges[0].pending = true
	state.world.getranges[0].seeded = true
	state.world.getranges[0].oldest_ts = 5000

	apply_set_cell_timeframe_action(state, 0, 4)

	testing.expect_value(t, state.world.getranges[0].pending, false)
	testing.expect_value(t, state.world.getranges[0].seeded, false)
	testing.expect_value(t, state.world.getranges[0].oldest_ts, i64(0))
}

// S115: Per-cell TF change resets candle health.
@(test)
test_s115_per_cell_tf_resets_candle_health :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.candle_health = .OK

	apply_set_cell_timeframe_action(state, 0, 4)
	testing.expect_value(t, state.candle_health, Candle_Health.No_Data)
}

// S115: Per-cell TF change resets scroll/zoom.
@(test)
test_s115_per_cell_tf_resets_scroll_zoom :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.world.views[0].candle_scroll_x = 50.0
	state.world.views[0].candle_zoom = 2.0

	apply_set_cell_timeframe_action(state, 0, 4)

	testing.expect_value(t, state.world.views[0].candle_scroll_x, f32(0))
	testing.expect_value(t, state.world.views[0].candle_zoom, f32(0))
}

// S115: Per-cell TF change no-ops when already at target TF.
@(test)
test_s115_per_cell_tf_noop_same :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = 4

	result := apply_set_cell_timeframe_action(state, 0, 4)
	testing.expect(t, !result, "same TF should be no-op")
}

// S115: Per-cell TF reverting to -1 re-follows global.
@(test)
test_s115_per_cell_tf_revert_to_global :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 3  // 5m
	state.world.timeframes[0].tf_idx = 5  // 30m override

	result := apply_set_cell_timeframe_action(state, 0, -1)
	testing.expect(t, result, "revert to global should succeed")
	testing.expect_value(t, state.world.timeframes[0].tf_idx, -1)
	testing.expect_value(t, cell_effective_tf_idx(state, 0), 3)
}

// S115: Multiple cells — per-cell TF change on one doesn't affect others.
@(test)
test_s115_per_cell_tf_multi_cell_isolation :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 3
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.world.timeframes[1].tf_idx = 3  // 5m
	state.world.timeframes[2].tf_idx = -1

	// Allocate slots for cell 1.
	binding_set(&state.world.bindings[1], "binance", "ETHUSDT")
	state.world.bindings[1].stream_idx = 1
	slot := &state.stream_views.slots[1]
	slot.used = true
	slot.subject_id = 200
	slot.candle_store.head = 5
	slot.candle_store.count = 10

	// Change cell 1 TF from 5m to 15m.
	apply_set_cell_timeframe_action(state, 1, 4)

	// Cell 1 store should be cleared.
	testing.expect_value(t, slot.candle_store.head, 0)
	testing.expect_value(t, slot.candle_store.count, 0)

	// Other cells' TF should be untouched.
	testing.expect_value(t, state.world.timeframes[0].tf_idx, -1)
	testing.expect_value(t, state.world.timeframes[2].tf_idx, -1)

	// Effective TFs should be correct.
	testing.expect_value(t, cell_effective_tf_idx(state, 0), 2)  // global
	testing.expect_value(t, cell_effective_tf_idx(state, 1), 4)  // per-cell (15m)
	testing.expect_value(t, cell_effective_tf_idx(state, 2), 2)  // global
}

// ---------------------------------------------------------------------------
// 4. Compare Mode TF — Per-Pane Isolation
// ---------------------------------------------------------------------------

// S115: Compare pane TF change resets getrange/scroll/zoom for that pane only.
@(test)
test_s115_compare_pane_tf_resets_state :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.compare.active = true
	state.compare.count = 3
	state.active_tf_idx = 2
	state.compare.tf_idx = {-1, -1, -1, -1}

	state.compare.scroll_x = {10, 20, 30, 0}
	state.compare.zoom = {1.5, 2.0, 2.5, 0}
	state.compare.getranges[0].pending = true
	state.compare.getranges[1].pending = true
	state.compare.getranges[2].pending = true

	apply_set_compare_pane_timeframe(state, 1, 4)

	// Pane 1 should be reset.
	testing.expect_value(t, state.compare.tf_idx[1], 4)
	testing.expect_value(t, state.compare.scroll_x[1], f32(0))
	testing.expect_value(t, state.compare.zoom[1], f32(0))
	testing.expect_value(t, state.compare.getranges[1].pending, false)

	// Pane 0 and 2 should be untouched.
	testing.expect_value(t, state.compare.tf_idx[0], -1)
	testing.expect_value(t, state.compare.scroll_x[0], f32(10))
	testing.expect_value(t, state.compare.zoom[0], f32(1.5))
	testing.expect_value(t, state.compare.getranges[0].pending, true)
	testing.expect_value(t, state.compare.tf_idx[2], -1)
	testing.expect_value(t, state.compare.scroll_x[2], f32(30))
	testing.expect_value(t, state.compare.zoom[2], f32(2.5))
	testing.expect_value(t, state.compare.getranges[2].pending, true)
}

// S115: Compare pane TF no-ops when already at target.
@(test)
test_s115_compare_pane_tf_noop_same :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.compare.active = true
	state.compare.count = 2
	state.compare.tf_idx[0] = 3

	result := apply_set_compare_pane_timeframe(state, 0, 3)
	testing.expect(t, !result, "same TF should be no-op")
}

// S115: Compare pane TF rejects out-of-range pane index.
@(test)
test_s115_compare_pane_tf_rejects_invalid_pane :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.compare.active = true
	state.compare.count = 2

	testing.expect(t, !apply_set_compare_pane_timeframe(state, -1, 3), "negative pane rejected")
	testing.expect(t, !apply_set_compare_pane_timeframe(state, 2, 3), "pane >= count rejected")
	testing.expect(t, !apply_set_compare_pane_timeframe(state, 4, 3), "pane 4 rejected")
}

// S115: Global TF change skips compare panes with per-pane TF override.
@(test)
test_s115_global_tf_skips_compare_pane_overrides :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 0
	state.active_tf_idx = 2
	state.compare.active = true
	state.compare.count = 3
	state.compare.tf_idx = {-1, 5, -1, -1}  // pane 1 has per-pane override
	state.compare.scroll_x = {10, 20, 30, 0}
	state.compare.zoom = {1.0, 2.0, 3.0, 0}

	apply_set_timeframe_action(state, 4)

	// Global followers (pane 0, 2) should be reset.
	testing.expect_value(t, state.compare.scroll_x[0], f32(0))
	testing.expect_value(t, state.compare.zoom[0], f32(0))
	testing.expect_value(t, state.compare.scroll_x[2], f32(0))
	testing.expect_value(t, state.compare.zoom[2], f32(0))

	// Per-pane override (pane 1) should be untouched.
	testing.expect_value(t, state.compare.scroll_x[1], f32(20))
	testing.expect_value(t, state.compare.zoom[1], f32(2.0))
	testing.expect_value(t, state.compare.tf_idx[1], 5)
}

// ---------------------------------------------------------------------------
// 5. Rapid TF Switching — Stress Tests
// ---------------------------------------------------------------------------

// S115: Rapid global TF cycling through all 9 timeframes.
@(test)
test_s115_rapid_global_tf_cycle :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 0
	state.world.timeframes[0].tf_idx = -1

	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	state.world.bindings[0].stream_idx = 0
	slot := &state.stream_views.slots[0]
	slot.used = true
	slot.subject_id = 100

	// Cycle through all TFs forward.
	for idx in 1 ..< 9 {
		slot.candle_store.head = 3
		slot.candle_store.count = 5
		apply_set_timeframe_action(state, idx)
		testing.expect_value(t, state.active_tf_idx, idx)
		testing.expect_value(t, slot.candle_store.head, 0)
		testing.expect_value(t, slot.candle_store.count, 0)
	}

	// Cycle back.
	for idx := 7; idx >= 0; idx -= 1 {
		slot.candle_store.head = 1
		slot.candle_store.count = 2
		apply_set_timeframe_action(state, idx)
		testing.expect_value(t, state.active_tf_idx, idx)
		testing.expect_value(t, slot.candle_store.head, 0)
		testing.expect_value(t, slot.candle_store.count, 0)
	}
}

// S115: Rapid per-cell TF switching back and forth.
@(test)
test_s115_rapid_per_cell_tf_toggle :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1

	binding_set(&state.world.bindings[0], "binance", "ETHUSDT")
	state.world.bindings[0].stream_idx = 0
	slot := &state.stream_views.slots[0]
	slot.used = true
	slot.subject_id = 200

	// Toggle per-cell TF between 5m (3) and 15m (4) rapidly.
	for _ in 0 ..< 10 {
		slot.candle_store.count = 5
		apply_set_cell_timeframe_action(state, 0, 3)
		testing.expect_value(t, state.world.timeframes[0].tf_idx, 3)
		testing.expect_value(t, slot.candle_store.count, 0)

		slot.candle_store.count = 5
		apply_set_cell_timeframe_action(state, 0, 4)
		testing.expect_value(t, state.world.timeframes[0].tf_idx, 4)
		testing.expect_value(t, slot.candle_store.count, 0)
	}
}

// ---------------------------------------------------------------------------
// 6. Analytics Store Clearing on TF Switch
// ---------------------------------------------------------------------------

// S115: Global TF change clears analytics for all global-following bound cells.
@(test)
test_s115_global_tf_clears_analytics_multi_cell :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 2
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.world.timeframes[1].tf_idx = -1

	// Cell 0 bound.
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	state.world.bindings[0].stream_idx = 0
	slot0 := &state.stream_views.slots[0]
	slot0.used = true
	slot0.subject_id = 100
	stream0 := layers.market_store_stream_get_or_alloc(&state.layer_store, 100)
	services.push_analytics(&stream0.analytics, services.Analytics_Entry{kind = .CVD, ts_ms = 1000, seq = 1})

	// Cell 1 bound.
	binding_set(&state.world.bindings[1], "binance", "ETHUSDT")
	state.world.bindings[1].stream_idx = 1
	slot1 := &state.stream_views.slots[1]
	slot1.used = true
	slot1.subject_id = 200
	stream1 := layers.market_store_stream_get_or_alloc(&state.layer_store, 200)
	services.push_analytics(&stream1.analytics, services.Analytics_Entry{kind = .Delta_Volume, ts_ms = 2000, seq = 1})

	testing.expect_value(t, stream0.analytics.count, 1)
	testing.expect_value(t, stream1.analytics.count, 1)

	apply_set_timeframe_action(state, 4)

	// Both should be cleared.
	testing.expect_value(t, stream0.analytics.count, 0)
	testing.expect_value(t, stream1.analytics.count, 0)
}

// S115: Per-cell TF change clears analytics only for that cell's stream.
@(test)
test_s115_per_cell_tf_clears_analytics_isolation :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 2
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.world.timeframes[1].tf_idx = -1

	// Cell 0 bound.
	binding_set(&state.world.bindings[0], "binance", "BTCUSDT")
	state.world.bindings[0].stream_idx = 0
	slot0 := &state.stream_views.slots[0]
	slot0.used = true
	slot0.subject_id = 100
	stream0 := layers.market_store_stream_get_or_alloc(&state.layer_store, 100)
	services.push_analytics(&stream0.analytics, services.Analytics_Entry{kind = .CVD, ts_ms = 1000, seq = 1})

	// Cell 1 bound.
	binding_set(&state.world.bindings[1], "binance", "ETHUSDT")
	state.world.bindings[1].stream_idx = 1
	slot1 := &state.stream_views.slots[1]
	slot1.used = true
	slot1.subject_id = 200
	stream1 := layers.market_store_stream_get_or_alloc(&state.layer_store, 200)
	services.push_analytics(&stream1.analytics, services.Analytics_Entry{kind = .Delta_Volume, ts_ms = 2000, seq = 1})

	// Change cell 0 TF only.
	apply_set_cell_timeframe_action(state, 0, 4)

	// Cell 0 analytics cleared.
	testing.expect_value(t, stream0.analytics.count, 0)
	// Cell 1 analytics untouched.
	testing.expect_value(t, stream1.analytics.count, 1)
}

// ---------------------------------------------------------------------------
// 7. Apply State Reset on TF Switch
// ---------------------------------------------------------------------------

// S115: Global TF change resets apply_state for active slot and all bound cells.
@(test)
test_s115_global_tf_resets_apply_state_comprehensive :: proc(t: ^testing.T) {
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
	slot.subject_id = 100
	slot.apply_state.has_live[.Candle] = true
	slot.apply_state.has_live[.Heatmap] = true
	slot.apply_state.has_live[.VPVR] = true

	apply_set_timeframe_action(state, 4)

	testing.expect(t, !slot.apply_state.has_live[.Candle], "candle has_live reset")
	testing.expect(t, !slot.apply_state.has_live[.Heatmap], "heatmap has_live reset")
	testing.expect(t, !slot.apply_state.has_live[.VPVR], "vpvr has_live reset")
}

// ---------------------------------------------------------------------------
// 8. Heatmap/VPVR Store Clearing
// ---------------------------------------------------------------------------

// S115: Global TF change clears heatmap and vpvr stores for bound cells.
@(test)
test_s115_global_tf_clears_heatmap_vpvr :: proc(t: ^testing.T) {
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
	slot.subject_id = 100
	slot.heatmap_store.count = 5
	slot.vpvr_store.count = 3
	slot.vpvr_store.max_volume = 1000.0

	apply_set_timeframe_action(state, 4)

	testing.expect_value(t, slot.heatmap_store.count, 0)
	testing.expect_value(t, slot.vpvr_store.count, 0)
	testing.expect_value(t, slot.vpvr_store.max_volume, f64(0))
}

// ---------------------------------------------------------------------------
// 9. Workspace Persistence — TF Round-Trip
// ---------------------------------------------------------------------------

// S115: Per-cell TF round-trip through V6 layout persistence for all TF indices.
@(test)
test_s115_persistence_all_tf_indices :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.world.count = 9
	state.active_tf_idx = 2
	state.layout_mode = .Custom
	state.custom_grid_def.col_count = 2
	state.custom_grid_def.col_weights[0] = 0.6
	state.custom_grid_def.col_weights[1] = 0.4
	state.custom_grid_def.row_count = 2
	state.custom_grid_def.row_weights[0] = 0.5
	state.custom_grid_def.row_weights[1] = 0.5
	for ci in 0 ..< 9 {
		init_world_cell_defaults(state, ci, .Candle)
	}

	// Set each cell to a different TF index (covering all 9 TFs).
	for i in 0 ..< 9 {
		state.world.timeframes[i].tf_idx = i
	}

	buf: [4096]u8
	off := build_layout_v6_string(state, buf[:])
	v6 := string(buf[:off])

	restored := new(App_State)
	defer free(restored)
	ok := restore_layout_v6_from_string(restored, v6)
	testing.expect(t, ok, "restore should succeed")

	for i in 0 ..< 9 {
		testing.expect_value(t, restored.world.timeframes[i].tf_idx, i)
	}
}

// S115: Global TF index round-trip (persist + restore cycle).
@(test)
test_s115_persistence_global_tf_cycle :: proc(t: ^testing.T) {
	// Verify all TF indices persist correctly as the setting string.
	opts := TF_OPTIONS
	for idx in 0 ..< len(opts) {
		state := new(App_State)
		defer free(state)
		state.stream_views = new(Stream_View_Registry)
		defer free(state.stream_views)
		state.world.count = 0
		state.active_tf_idx = 0

		apply_set_timeframe_action(state, idx)
		testing.expect_value(t, state.active_tf_idx, idx)
	}
}

// ---------------------------------------------------------------------------
// 10. TF Constants — Structural Invariants
// ---------------------------------------------------------------------------

// S115: TF_OPTIONS and TF_OPTION_MS arrays have matching lengths.
@(test)
test_s115_tf_arrays_aligned :: proc(t: ^testing.T) {
	testing.expect_value(t, len(TF_OPTIONS), len(TF_OPTION_MS))
	testing.expect_value(t, len(TF_OPTIONS), 9)
}

// S115: TF_OPTION_MS values are strictly increasing.
@(test)
test_s115_tf_ms_monotonically_increasing :: proc(t: ^testing.T) {
	ms := TF_OPTION_MS
	for i in 1 ..< len(ms) {
		testing.expect(t, ms[i] > ms[i - 1], "TF_OPTION_MS must be strictly increasing")
	}
}

// S115: TF_OPTIONS labels are non-empty and distinct.
@(test)
test_s115_tf_labels_distinct :: proc(t: ^testing.T) {
	opts := TF_OPTIONS
	for i in 0 ..< len(opts) {
		testing.expect(t, len(opts[i]) > 0, "TF label must be non-empty")
		for j in i + 1 ..< len(opts) {
			testing.expect(t, opts[i] != opts[j], "TF labels must be distinct")
		}
	}
}

// S115: Known TF indices map to expected labels.
@(test)
test_s115_tf_index_to_label :: proc(t: ^testing.T) {
	opts := TF_OPTIONS
	testing.expect_value(t, opts[0], "1s")
	testing.expect_value(t, opts[1], "5s")
	testing.expect_value(t, opts[2], "1m")
	testing.expect_value(t, opts[3], "5m")
	testing.expect_value(t, opts[4], "15m")
	testing.expect_value(t, opts[5], "30m")
	testing.expect_value(t, opts[6], "1h")
	testing.expect_value(t, opts[7], "4h")
	testing.expect_value(t, opts[8], "1d")
}

// ---------------------------------------------------------------------------
// 11. Pane-Level TF Resolution (Workspace Tree Path)
// ---------------------------------------------------------------------------

// S115: pane_effective_tf_idx respects 3-tier hierarchy (pane → workspace → global).
@(test)
test_s115_pane_effective_tf_hierarchy :: proc(t: ^testing.T) {
	pool: Pane_Pool

	// Tier 1: Pane override wins.
	pane, _ := pane_pool_alloc(&pool)
	pane.tf_override = 6  // 1h
	ws: Workspace
	workspace_init(&ws, Workspace_ID(1))
	ws.data_ctx.default_tf_idx = 3  // 5m
	testing.expect_value(t, pane_effective_tf_idx(pane, &ws, 2), 6)

	// Tier 2: Workspace default wins over global.
	pane.tf_override = -1
	testing.expect_value(t, pane_effective_tf_idx(pane, &ws, 2), 3)

	// Tier 3: Global fallback.
	testing.expect_value(t, pane_effective_tf_idx(pane, nil, 7), 7)
}

// S115: Per-cell TF change syncs to pane (S112 contract).
@(test)
test_s115_per_cell_tf_syncs_to_pane :: proc(t: ^testing.T) {
	state := new(App_State)
	defer free(state)
	state.stream_views = new(Stream_View_Registry)
	defer free(state.stream_views)

	state.world.count = 1
	state.active_tf_idx = 2
	state.world.timeframes[0].tf_idx = -1
	state.world.widgets[0] = Widget_Component{kind = .Candle}

	// Build workspace so the sync path has a target.
	ws := workspace_registry_alloc(&state.ws_registry)
	workspace_sync_from_world(state)

	apply_set_cell_timeframe_action(state, 0, 5)

	// Verify pane got the override.
	ws = workspace_registry_active(&state.ws_registry)
	pane_ids, pane_count := tree_collect_pane_ids(&ws.tree)
	testing.expect(t, pane_count >= 1, "at least 1 pane")
	pane := pane_pool_get(&ws.pane_pool, pane_ids[0])
	testing.expect(t, pane != nil, "pane exists")
	testing.expect_value(t, pane.tf_override, i8(5))
	testing.expect_value(t, pane.view.scroll_x, f32(0))
	testing.expect_value(t, pane.view.zoom_level, f32(1.0))
}
