package services

import "core:testing"

// S149: DOM Store tests.
// Covers: push_trade, fill lookup, VWAP/TWAP, imbalance, max fill, has_data.

@(test)
test_dom_store_empty :: proc(t: ^testing.T) {
	store: DOM_Store
	testing.expect(t, !dom_store_has_data(&store), "empty store has no data")
	testing.expect_value(t, dom_store_vwap(&store), f64(0))
	testing.expect_value(t, dom_store_twap(&store), f64(0))
	testing.expect_value(t, dom_store_imbalance(&store), f64(0))
	testing.expect_value(t, dom_store_max_fill(&store), f64(0))
}

@(test)
test_dom_store_push_trade_basic :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 1.5, true, 1000, 1.0)
	testing.expect(t, dom_store_has_data(&store), "store has data after push")
	testing.expect_value(t, store.trade_count, i64(1))
	testing.expect_value(t, store.total_buy_vol, f64(1.5))
	testing.expect_value(t, store.total_sell_vol, f64(0))
	testing.expect_value(t, store.level_count, 1)
}

@(test)
test_dom_store_push_trade_sell :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 2.0, false, 1000, 1.0)
	testing.expect_value(t, store.total_sell_vol, f64(2.0))
	testing.expect_value(t, store.total_buy_vol, f64(0))
}

@(test)
test_dom_store_push_trade_zero_qty_rejected :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 0, true, 1000, 1.0)
	testing.expect(t, !dom_store_has_data(&store), "zero qty rejected")
}

@(test)
test_dom_store_push_trade_zero_price_rejected :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 0, 1.0, true, 1000, 1.0)
	testing.expect(t, !dom_store_has_data(&store), "zero price rejected")
}

@(test)
test_dom_store_fill_accumulation :: proc(t: ^testing.T) {
	store: DOM_Store
	// Two buys and one sell at same bucket.
	dom_store_push_trade(&store, 100.0, 1.0, true, 1000, 1.0)
	dom_store_push_trade(&store, 100.0, 0.5, true, 1001, 1.0)
	dom_store_push_trade(&store, 100.0, 2.0, false, 1002, 1.0)

	buy_vol, sell_vol := dom_store_get_fill(&store, 100.0)
	testing.expect_value(t, buy_vol, f64(1.5))
	testing.expect_value(t, sell_vol, f64(2.0))
	testing.expect_value(t, store.level_count, 1) // same bucket
}

@(test)
test_dom_store_fill_at_price_with_grouping :: proc(t: ^testing.T) {
	store: DOM_Store
	// Group = 10, so 105 and 108 both map to bucket 100.
	dom_store_push_trade(&store, 105.0, 1.0, true, 1000, 10.0)
	dom_store_push_trade(&store, 108.0, 2.0, false, 1001, 10.0)

	buy_vol, sell_vol := dom_store_fill_at_price(&store, 103.0) // maps to bucket 100
	testing.expect_value(t, buy_vol, f64(1.0))
	testing.expect_value(t, sell_vol, f64(2.0))
}

@(test)
test_dom_store_fill_at_price_miss :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 1.0, true, 1000, 1.0)
	buy_vol, sell_vol := dom_store_fill_at_price(&store, 200.0)
	testing.expect_value(t, buy_vol, f64(0))
	testing.expect_value(t, sell_vol, f64(0))
}

@(test)
test_dom_store_vwap :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 1.0, true, 1000, 1.0)
	dom_store_push_trade(&store, 200.0, 1.0, false, 1001, 1.0)
	// VWAP = (100*1 + 200*1) / (1+1) = 150
	vwap := dom_store_vwap(&store)
	testing.expect_value(t, vwap, f64(150.0))
}

@(test)
test_dom_store_twap :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 1.0, true, 1000, 1.0)
	dom_store_push_trade(&store, 200.0, 1.0, false, 1001, 1.0)
	// TWAP = (100 + 200) / 2 = 150
	twap := dom_store_twap(&store)
	testing.expect_value(t, twap, f64(150.0))
}

@(test)
test_dom_store_imbalance_all_buys :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 5.0, true, 1000, 1.0)
	imb := dom_store_imbalance(&store)
	testing.expect_value(t, imb, f64(1.0)) // +1 = all buys
}

@(test)
test_dom_store_imbalance_all_sells :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 5.0, false, 1000, 1.0)
	imb := dom_store_imbalance(&store)
	testing.expect_value(t, imb, f64(-1.0)) // -1 = all sells
}

@(test)
test_dom_store_imbalance_balanced :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 5.0, true, 1000, 1.0)
	dom_store_push_trade(&store, 100.0, 5.0, false, 1001, 1.0)
	imb := dom_store_imbalance(&store)
	testing.expect_value(t, imb, f64(0)) // balanced
}

@(test)
test_dom_store_max_fill :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 3.0, true, 1000, 1.0)
	dom_store_push_trade(&store, 200.0, 5.0, false, 1001, 1.0)
	max_fill := dom_store_max_fill(&store)
	testing.expect_value(t, max_fill, f64(5.0)) // level at 200 has total 5.0
}

@(test)
test_dom_store_recent_fills_ring :: proc(t: ^testing.T) {
	store: DOM_Store
	// Push DOM_FILL_RING + 10 trades to exercise ring wrap.
	for i in 0 ..< DOM_FILL_RING + 10 {
		dom_store_push_trade(&store, 100.0 + f64(i), 0.1, i % 2 == 0, i64(1000 + i), 1.0)
	}
	testing.expect_value(t, store.recent_count, DOM_FILL_RING) // capped at ring size
	testing.expect_value(t, store.trade_count, i64(DOM_FILL_RING + 10)) // total not capped
}

@(test)
test_dom_store_reset :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 1.0, true, 1000, 1.0)
	testing.expect(t, dom_store_has_data(&store), "has data before reset")
	dom_store_reset(&store)
	testing.expect(t, !dom_store_has_data(&store), "no data after reset")
	testing.expect_value(t, store.level_count, 0)
	testing.expect_value(t, store.recent_count, 0)
}

@(test)
test_dom_store_multiple_price_levels :: proc(t: ^testing.T) {
	store: DOM_Store
	dom_store_push_trade(&store, 100.0, 1.0, true, 1000, 1.0)
	dom_store_push_trade(&store, 101.0, 2.0, true, 1001, 1.0)
	dom_store_push_trade(&store, 102.0, 3.0, false, 1002, 1.0)
	testing.expect_value(t, store.level_count, 3) // three distinct price buckets
}

@(test)
test_dom_store_fill_at_price_nil :: proc(t: ^testing.T) {
	buy_vol, sell_vol := dom_store_fill_at_price(nil, 100.0)
	testing.expect_value(t, buy_vol, f64(0))
	testing.expect_value(t, sell_vol, f64(0))
}
