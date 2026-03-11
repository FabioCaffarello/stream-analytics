package services

import "core:testing"

// S156: Trades_Store unit tests.
// Covers: push, get, ring wrap, capacity, ordering, edge cases.

@(test)
test_trades_store_empty :: proc(t: ^testing.T) {
	store: Trades_Store
	testing.expect_value(t, store.count, 0)
	testing.expect_value(t, store.head, 0)
	entry := get_trade(&store, 0)
	testing.expect_value(t, entry.price, f64(0))
}

@(test)
test_trades_store_push_single :: proc(t: ^testing.T) {
	store: Trades_Store
	push_trade(&store, Trade_Entry{price = 100.0, qty = 1.5, side = .Buy, unix = 1000})
	testing.expect_value(t, store.count, 1)
	entry := get_trade(&store, 0)
	testing.expect_value(t, entry.price, f64(100.0))
	testing.expect_value(t, entry.qty, f64(1.5))
	testing.expect_value(t, entry.side, Trade_Side.Buy)
	testing.expect_value(t, entry.unix, i64(1000))
}

@(test)
test_trades_store_newest_first :: proc(t: ^testing.T) {
	store: Trades_Store
	push_trade(&store, Trade_Entry{price = 100.0, qty = 1.0, side = .Buy, unix = 1000})
	push_trade(&store, Trade_Entry{price = 200.0, qty = 2.0, side = .Sell, unix = 2000})
	// Index 0 = most recent.
	latest := get_trade(&store, 0)
	testing.expect_value(t, latest.price, f64(200.0))
	oldest := get_trade(&store, 1)
	testing.expect_value(t, oldest.price, f64(100.0))
}

@(test)
test_trades_store_get_out_of_range :: proc(t: ^testing.T) {
	store: Trades_Store
	push_trade(&store, Trade_Entry{price = 50.0, qty = 1.0, side = .Buy, unix = 1000})
	entry := get_trade(&store, 1) // only 1 entry, index 1 is out of range
	testing.expect_value(t, entry.price, f64(0))
}

@(test)
test_trades_store_ring_wrap :: proc(t: ^testing.T) {
	store: Trades_Store
	// Fill beyond capacity to exercise ring wrap.
	for i in 0 ..< TRADES_CAP + 10 {
		push_trade(&store, Trade_Entry{price = f64(i), qty = 1.0, side = .Buy, unix = i64(i)})
	}
	testing.expect_value(t, store.count, TRADES_CAP)
	// Most recent should be TRADES_CAP + 9.
	latest := get_trade(&store, 0)
	testing.expect_value(t, latest.price, f64(TRADES_CAP + 9))
	// Oldest should be 10 (first 10 were evicted).
	oldest := get_trade(&store, TRADES_CAP - 1)
	testing.expect_value(t, oldest.price, f64(10))
}

@(test)
test_trades_store_count_capped :: proc(t: ^testing.T) {
	store: Trades_Store
	for i in 0 ..< TRADES_CAP * 2 {
		push_trade(&store, Trade_Entry{price = f64(i), qty = 0.1, side = .Sell, unix = i64(i)})
	}
	testing.expect_value(t, store.count, TRADES_CAP)
}

@(test)
test_trades_store_sell_side :: proc(t: ^testing.T) {
	store: Trades_Store
	push_trade(&store, Trade_Entry{price = 300.0, qty = 5.0, side = .Sell, unix = 9000})
	entry := get_trade(&store, 0)
	testing.expect_value(t, entry.side, Trade_Side.Sell)
	testing.expect_value(t, entry.qty, f64(5.0))
}

@(test)
test_trades_store_fill_demo :: proc(t: ^testing.T) {
	store: Trades_Store
	fill_demo_trades(&store)
	testing.expect_value(t, store.count, TRADES_CAP)
	// All entries should have non-zero prices.
	for i in 0 ..< TRADES_CAP {
		entry := get_trade(&store, i)
		testing.expect(t, entry.price > 0, "demo trade should have positive price")
	}
}
