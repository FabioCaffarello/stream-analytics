package services

import "core:testing"

// S156: Footprint_Store unit tests.
// Covers: push, get, TF binning, price grouping, ring wrap, capacity, reset, edge cases.

@(test)
test_footprint_store_empty :: proc(t: ^testing.T) {
	store: Footprint_Store
	testing.expect_value(t, store.count, 0)
	_, ok := footprint_store_get(&store, 0)
	testing.expect(t, !ok, "empty store returns not ok")
}

@(test)
test_footprint_store_push_single :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 100.0, 1.5, true, 60_000, 60_000, 1.0)
	testing.expect_value(t, store.count, 1)
	entry, ok := footprint_store_get(&store, 60_000)
	testing.expect(t, ok, "should find entry for window 60000")
	testing.expect_value(t, entry.level_count, 1)
	testing.expect_value(t, entry.levels[0].price, f64(100.0))
	testing.expect_value(t, entry.levels[0].buy_vol, f64(1.5))
	testing.expect_value(t, entry.levels[0].sell_vol, f64(0))
}

@(test)
test_footprint_store_sell :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 100.0, 2.0, false, 60_000, 60_000, 1.0)
	entry, ok := footprint_store_get(&store, 60_000)
	testing.expect(t, ok, "should find entry")
	testing.expect_value(t, entry.levels[0].buy_vol, f64(0))
	testing.expect_value(t, entry.levels[0].sell_vol, f64(2.0))
}

@(test)
test_footprint_store_accumulation :: proc(t: ^testing.T) {
	store: Footprint_Store
	// Two buys and one sell at same price in same window.
	footprint_store_push_trade(&store, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	footprint_store_push_trade(&store, 100.0, 0.5, true, 60_001, 60_000, 1.0)
	footprint_store_push_trade(&store, 100.0, 2.0, false, 60_002, 60_000, 1.0)
	testing.expect_value(t, store.count, 1) // same candle window
	entry, _ := footprint_store_get(&store, 60_000)
	testing.expect_value(t, entry.level_count, 1)
	testing.expect_value(t, entry.levels[0].buy_vol, f64(1.5))
	testing.expect_value(t, entry.levels[0].sell_vol, f64(2.0))
}

@(test)
test_footprint_store_tf_binning :: proc(t: ^testing.T) {
	store: Footprint_Store
	// 1-minute TF (60_000ms). Trades at 90_000 and 120_001 should be in different windows.
	footprint_store_push_trade(&store, 100.0, 1.0, true, 90_000, 60_000, 1.0)   // window = 60_000
	footprint_store_push_trade(&store, 100.0, 1.0, true, 120_001, 60_000, 1.0) // window = 120_000
	testing.expect_value(t, store.count, 2)
	_, ok1 := footprint_store_get(&store, 60_000)
	testing.expect(t, ok1, "should find window 60000")
	_, ok2 := footprint_store_get(&store, 120_000)
	testing.expect(t, ok2, "should find window 120000")
}

@(test)
test_footprint_store_price_grouping :: proc(t: ^testing.T) {
	store: Footprint_Store
	// Group=10: prices 105 and 108 should bucket to 100.
	footprint_store_push_trade(&store, 105.0, 1.0, true, 60_000, 60_000, 10.0)
	footprint_store_push_trade(&store, 108.0, 2.0, false, 60_001, 60_000, 10.0)
	entry, _ := footprint_store_get(&store, 60_000)
	testing.expect_value(t, entry.level_count, 1) // same bucket
	testing.expect_value(t, entry.levels[0].price, f64(100.0))
	testing.expect_value(t, entry.levels[0].buy_vol, f64(1.0))
	testing.expect_value(t, entry.levels[0].sell_vol, f64(2.0))
}

@(test)
test_footprint_store_multiple_price_levels :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	footprint_store_push_trade(&store, 101.0, 2.0, true, 60_001, 60_000, 1.0)
	footprint_store_push_trade(&store, 102.0, 3.0, false, 60_002, 60_000, 1.0)
	entry, _ := footprint_store_get(&store, 60_000)
	testing.expect_value(t, entry.level_count, 3)
}

@(test)
test_footprint_store_zero_qty_rejected :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 100.0, 0, true, 60_000, 60_000, 1.0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_footprint_store_zero_price_rejected :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 0, 1.0, true, 60_000, 60_000, 1.0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_footprint_store_zero_tf_rejected :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 100.0, 1.0, true, 60_000, 0, 1.0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_footprint_store_default_price_group :: proc(t: ^testing.T) {
	store: Footprint_Store
	// price_group=0 should default to 1.0.
	footprint_store_push_trade(&store, 100.5, 1.0, true, 60_000, 60_000, 0)
	entry, _ := footprint_store_get(&store, 60_000)
	testing.expect_value(t, entry.price_group, f64(1.0))
	testing.expect_value(t, entry.levels[0].price, f64(100.0)) // floor(100.5/1)*1
}

@(test)
test_footprint_store_ring_wrap :: proc(t: ^testing.T) {
	store: Footprint_Store
	// Push more candle windows than capacity.
	for i in 0 ..< FOOTPRINT_CANDLE_CAP + 5 {
		ts := i64(i) * 60_000
		footprint_store_push_trade(&store, 100.0, 1.0, true, ts, 60_000, 1.0)
	}
	testing.expect_value(t, store.count, FOOTPRINT_CANDLE_CAP)
	// Oldest windows (0..4) should be evicted.
	_, ok := footprint_store_get(&store, 0)
	testing.expect(t, !ok, "oldest window should be evicted")
	// Latest window should exist.
	latest_ts := i64(FOOTPRINT_CANDLE_CAP + 4) * 60_000
	_, ok2 := footprint_store_get(&store, latest_ts)
	testing.expect(t, ok2, "latest window should exist")
}

@(test)
test_footprint_store_level_cap :: proc(t: ^testing.T) {
	store: Footprint_Store
	// Push more price levels than FOOTPRINT_LEVEL_CAP.
	for i in 0 ..< FOOTPRINT_LEVEL_CAP + 5 {
		footprint_store_push_trade(&store, f64(i), 1.0, true, 60_000, 60_000, 1.0)
	}
	entry, _ := footprint_store_get(&store, 60_000)
	testing.expect_value(t, entry.level_count, FOOTPRINT_LEVEL_CAP) // capped
}

@(test)
test_footprint_store_reset :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	testing.expect_value(t, store.count, 1)
	footprint_store_reset(&store)
	testing.expect_value(t, store.count, 0)
	testing.expect_value(t, store.head, 0)
}

@(test)
test_footprint_store_get_not_found :: proc(t: ^testing.T) {
	store: Footprint_Store
	footprint_store_push_trade(&store, 100.0, 1.0, true, 60_000, 60_000, 1.0)
	_, ok := footprint_store_get(&store, 120_000) // different window
	testing.expect(t, !ok, "wrong window should not be found")
}
