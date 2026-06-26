package services

import "core:testing"

// S156: Stats_Store unit tests.
// Covers: push, get, ring wrap, capacity, field preservation, edge cases.

@(test)
test_stats_store_empty :: proc(t: ^testing.T) {
	store: Stats_Store
	testing.expect_value(t, store.count, 0)
	entry := get_stats(&store, 0)
	testing.expect_value(t, entry.mark_price, f64(0))
}

@(test)
test_stats_store_push_single :: proc(t: ^testing.T) {
	store: Stats_Store
	push_stats(&store, Stats_Entry{
		mark_price = 42000.0, funding = 0.0001,
		liq_buy = 10, liq_sell = 5, unix = 1000,
	})
	testing.expect_value(t, store.count, 1)
	entry := get_stats(&store, 0)
	testing.expect_value(t, entry.mark_price, f64(42000.0))
	testing.expect_value(t, entry.funding, f64(0.0001))
	testing.expect_value(t, entry.liq_buy, f64(10))
	testing.expect_value(t, entry.liq_sell, f64(5))
}

@(test)
test_stats_store_newest_first :: proc(t: ^testing.T) {
	store: Stats_Store
	push_stats(&store, Stats_Entry{mark_price = 100, unix = 1000})
	push_stats(&store, Stats_Entry{mark_price = 200, unix = 2000})
	latest := get_stats(&store, 0)
	testing.expect_value(t, latest.mark_price, f64(200))
	oldest := get_stats(&store, 1)
	testing.expect_value(t, oldest.mark_price, f64(100))
}

@(test)
test_stats_store_get_out_of_range :: proc(t: ^testing.T) {
	store: Stats_Store
	push_stats(&store, Stats_Entry{mark_price = 42000, unix = 1000})
	entry := get_stats(&store, 1)
	testing.expect_value(t, entry.mark_price, f64(0))
}

@(test)
test_stats_store_ring_wrap :: proc(t: ^testing.T) {
	store: Stats_Store
	for i in 0 ..< STATS_CAP + 5 {
		push_stats(&store, Stats_Entry{mark_price = f64(i), unix = i64(i)})
	}
	testing.expect_value(t, store.count, STATS_CAP)
	latest := get_stats(&store, 0)
	testing.expect_value(t, latest.mark_price, f64(STATS_CAP + 4))
	oldest := get_stats(&store, STATS_CAP - 1)
	testing.expect_value(t, oldest.mark_price, f64(5))
}

@(test)
test_stats_store_quality_flags_preserved :: proc(t: ^testing.T) {
	store: Stats_Store
	push_stats(&store, Stats_Entry{mark_price = 100, quality_flags = 0xFF, unix = 1000})
	entry := get_stats(&store, 0)
	testing.expect_value(t, entry.quality_flags, u32(0xFF))
}

@(test)
test_stats_store_window_ms_preserved :: proc(t: ^testing.T) {
	store: Stats_Store
	push_stats(&store, Stats_Entry{mark_price = 100, window_ms = 60_000, ts_ingest_ms = 999, unix = 1000})
	entry := get_stats(&store, 0)
	testing.expect_value(t, entry.window_ms, i64(60_000))
	testing.expect_value(t, entry.ts_ingest_ms, i64(999))
}

@(test)
test_stats_store_fill_demo :: proc(t: ^testing.T) {
	store: Stats_Store
	fill_demo_stats(&store)
	testing.expect_value(t, store.count, 8)
	// First pushed = oldest = index 7.
	oldest := get_stats(&store, 7)
	testing.expect_value(t, oldest.mark_price, f64(42150))
	// Last pushed = newest = index 0.
	newest := get_stats(&store, 0)
	testing.expect_value(t, newest.mark_price, f64(42165))
}
