package services

import "core:testing"

// S156: Orderbook_Store unit tests.
// Covers: update, get, best bid/ask, spread, mid_price, clamping, edge cases.

@(test)
test_ob_store_empty :: proc(t: ^testing.T) {
	store: Orderbook_Store
	testing.expect_value(t, store.ask_count, 0)
	testing.expect_value(t, store.bid_count, 0)
	testing.expect_value(t, best_ask(&store), f64(0))
	testing.expect_value(t, best_bid(&store), f64(0))
	testing.expect_value(t, spread(&store), f64(0))
}

@(test)
test_ob_store_update_basic :: proc(t: ^testing.T) {
	store: Orderbook_Store
	asks_p := [?]f64{100.0, 101.0}
	asks_s := [?]f64{5.0, 3.0}
	bids_p := [?]f64{99.0, 98.0}
	bids_s := [?]f64{4.0, 2.0}
	update_orderbook(&store, asks_p[:], asks_s[:], bids_p[:], bids_s[:], 99.5, 1000)

	testing.expect_value(t, store.ask_count, 2)
	testing.expect_value(t, store.bid_count, 2)
	testing.expect_value(t, store.last_price, f64(99.5))
	testing.expect_value(t, store.unix, i64(1000))
}

@(test)
test_ob_store_best_ask_bid :: proc(t: ^testing.T) {
	store: Orderbook_Store
	asks_p := [?]f64{100.0, 101.0}
	asks_s := [?]f64{5.0, 3.0}
	bids_p := [?]f64{99.0, 98.0}
	bids_s := [?]f64{4.0, 2.0}
	update_orderbook(&store, asks_p[:], asks_s[:], bids_p[:], bids_s[:], 99.5, 1000)

	testing.expect_value(t, best_ask(&store), f64(100.0))
	testing.expect_value(t, best_bid(&store), f64(99.0))
}

@(test)
test_ob_store_spread :: proc(t: ^testing.T) {
	store: Orderbook_Store
	asks_p := [?]f64{100.0}
	asks_s := [?]f64{5.0}
	bids_p := [?]f64{99.0}
	bids_s := [?]f64{4.0}
	update_orderbook(&store, asks_p[:], asks_s[:], bids_p[:], bids_s[:], 99.5, 1000)

	testing.expect_value(t, spread(&store), f64(1.0))
}

@(test)
test_ob_store_mid_price :: proc(t: ^testing.T) {
	store: Orderbook_Store
	asks_p := [?]f64{100.0}
	asks_s := [?]f64{5.0}
	bids_p := [?]f64{98.0}
	bids_s := [?]f64{4.0}
	update_orderbook(&store, asks_p[:], asks_s[:], bids_p[:], bids_s[:], 99.0, 1000)

	testing.expect_value(t, mid_price(&store), f64(99.0)) // (100+98)/2
}

@(test)
test_ob_store_mid_price_fallback_to_last :: proc(t: ^testing.T) {
	store: Orderbook_Store
	// Only asks, no bids — mid_price falls back to last_price.
	asks_p := [?]f64{100.0}
	asks_s := [?]f64{5.0}
	update_orderbook(&store, asks_p[:], asks_s[:], nil, nil, 42000.0, 1000)

	testing.expect_value(t, mid_price(&store), f64(42000.0))
}

@(test)
test_ob_store_get_ask_bid :: proc(t: ^testing.T) {
	store: Orderbook_Store
	asks_p := [?]f64{100.0, 101.0, 102.0}
	asks_s := [?]f64{5.0, 3.0, 1.0}
	bids_p := [?]f64{99.0, 98.0}
	bids_s := [?]f64{4.0, 2.0}
	update_orderbook(&store, asks_p[:], asks_s[:], bids_p[:], bids_s[:], 99.5, 1000)

	p, s := get_ask(&store, 2)
	testing.expect_value(t, p, f64(102.0))
	testing.expect_value(t, s, f64(1.0))

	p2, s2 := get_bid(&store, 1)
	testing.expect_value(t, p2, f64(98.0))
	testing.expect_value(t, s2, f64(2.0))
}

@(test)
test_ob_store_get_out_of_range :: proc(t: ^testing.T) {
	store: Orderbook_Store
	asks_p := [?]f64{100.0}
	asks_s := [?]f64{5.0}
	update_orderbook(&store, asks_p[:], asks_s[:], nil, nil, 99.0, 1000)

	p, s := get_ask(&store, 1)
	testing.expect_value(t, p, f64(0))
	testing.expect_value(t, s, f64(0))

	p2, s2 := get_bid(&store, 0)
	testing.expect_value(t, p2, f64(0))
	testing.expect_value(t, s2, f64(0))
}

@(test)
test_ob_store_depth_clamped :: proc(t: ^testing.T) {
	store: Orderbook_Store
	// Provide more levels than OB_DEPTH_CAP.
	big_p: [OB_DEPTH_CAP + 20]f64
	big_s: [OB_DEPTH_CAP + 20]f64
	for i in 0 ..< OB_DEPTH_CAP + 20 {
		big_p[i] = f64(100 + i)
		big_s[i] = f64(i + 1)
	}
	update_orderbook(&store, big_p[:], big_s[:], big_p[:], big_s[:], 99.0, 1000)

	testing.expect_value(t, store.ask_count, OB_DEPTH_CAP)
	testing.expect_value(t, store.bid_count, OB_DEPTH_CAP)
}

@(test)
test_ob_store_snapshot_replacement :: proc(t: ^testing.T) {
	store: Orderbook_Store
	// First update.
	asks1 := [?]f64{100.0}
	sizes1 := [?]f64{5.0}
	update_orderbook(&store, asks1[:], sizes1[:], asks1[:], sizes1[:], 99.0, 1000)
	testing.expect_value(t, store.ask_count, 1)

	// Second update replaces completely.
	asks2 := [?]f64{200.0, 201.0, 202.0}
	sizes2 := [?]f64{1.0, 2.0, 3.0}
	update_orderbook(&store, asks2[:], sizes2[:], nil, nil, 199.0, 2000)
	testing.expect_value(t, store.ask_count, 3)
	testing.expect_value(t, store.bid_count, 0)
	testing.expect_value(t, best_ask(&store), f64(200.0))
	testing.expect_value(t, store.last_price, f64(199.0))
}

@(test)
test_ob_store_fill_demo :: proc(t: ^testing.T) {
	store: Orderbook_Store
	fill_demo_orderbook(&store)
	testing.expect_value(t, store.ask_count, 25)
	testing.expect_value(t, store.bid_count, 25)
	testing.expect_value(t, store.last_price, f64(42175.0))
	// Best ask should be 42200.
	testing.expect_value(t, best_ask(&store), f64(42200.0))
	// Best bid should be 42150.
	testing.expect_value(t, best_bid(&store), f64(42150.0))
}
