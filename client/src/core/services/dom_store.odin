package services

// DOM (Depth of Market) fill-tracking store.
// Accumulates trade volume per price level for market fill display.
// Fixed capacity, zero allocation after init.

import "core:math"

DOM_FILL_CAP  :: 512 // max price level buckets
DOM_FILL_RING :: 128 // recent fill events

DOM_Fill_Level :: struct {
	price:    f64,
	buy_vol:  f64,
	sell_vol: f64,
}

DOM_Fill_Event :: struct {
	price:  f64,
	qty:    f64,
	is_buy: bool,
	unix:   i64,
}

DOM_Store :: struct {
	levels:       [DOM_FILL_CAP]DOM_Fill_Level,
	level_count:  int,
	price_group:  f64,
	// VWAP/TWAP accumulators.
	vwap_sum_pv:  f64, // sum(price * vol)
	vwap_sum_v:   f64, // sum(vol)
	twap_sum:     f64, // sum(price)
	twap_count:   i64, // trade count
	// Recent fills ring buffer.
	recent_fills: [DOM_FILL_RING]DOM_Fill_Event,
	recent_head:  int,
	recent_count: int,
	// Totals.
	total_buy_vol:  f64,
	total_sell_vol: f64,
	trade_count:    i64,
}

dom_store_reset :: proc(store: ^DOM_Store) {
	store^ = {}
}

dom_store_push_trade :: proc(store: ^DOM_Store, price, qty: f64, is_buy: bool, unix: i64, price_group: f64) {
	if qty <= 0 || price <= 0 do return

	// Track price group.
	if price_group > 0 {
		store.price_group = price_group
	}

	group := store.price_group > 0 ? store.price_group : 1.0
	bucket_price := math.floor(price / group) * group

	// Find or create level.
	found := false
	for i in 0 ..< store.level_count {
		if store.levels[i].price == bucket_price {
			if is_buy {
				store.levels[i].buy_vol += qty
			} else {
				store.levels[i].sell_vol += qty
			}
			found = true
			break
		}
	}
	if !found && store.level_count < DOM_FILL_CAP {
		store.levels[store.level_count] = DOM_Fill_Level{
			price    = bucket_price,
			buy_vol  = is_buy ? qty : 0,
			sell_vol = is_buy ? 0 : qty,
		}
		store.level_count += 1
	}

	// VWAP/TWAP accumulators.
	store.vwap_sum_pv += price * qty
	store.vwap_sum_v += qty
	store.twap_sum += price
	store.twap_count += 1

	// Totals.
	if is_buy {
		store.total_buy_vol += qty
	} else {
		store.total_sell_vol += qty
	}
	store.trade_count += 1

	// Recent fills ring.
	store.recent_fills[store.recent_head] = DOM_Fill_Event{
		price  = price,
		qty    = qty,
		is_buy = is_buy,
		unix   = unix,
	}
	store.recent_head = (store.recent_head + 1) % DOM_FILL_RING
	if store.recent_count < DOM_FILL_RING {
		store.recent_count += 1
	}
}

dom_store_vwap :: proc(store: ^DOM_Store) -> f64 {
	if store.vwap_sum_v <= 0 do return 0
	return store.vwap_sum_pv / store.vwap_sum_v
}

dom_store_twap :: proc(store: ^DOM_Store) -> f64 {
	if store.twap_count <= 0 do return 0
	return store.twap_sum / f64(store.twap_count)
}

dom_store_get_fill :: proc(store: ^DOM_Store, bucket_price: f64) -> (buy_vol, sell_vol: f64) {
	for i in 0 ..< store.level_count {
		if store.levels[i].price == bucket_price {
			return store.levels[i].buy_vol, store.levels[i].sell_vol
		}
	}
	return 0, 0
}

// S149: Check if the DOM store has any accumulated data.
dom_store_has_data :: proc(store: ^DOM_Store) -> bool {
	return store != nil && store.trade_count > 0
}

// S149: Buy/sell imbalance ratio. Returns [-1, +1].
// +1 = all buys, -1 = all sells, 0 = balanced.
dom_store_imbalance :: proc(store: ^DOM_Store) -> f64 {
	total := store.total_buy_vol + store.total_sell_vol
	if total <= 0 do return 0
	return (store.total_buy_vol - store.total_sell_vol) / total
}

// S149: Find fill volume for an exact orderbook price.
// Uses the store's price_group to bucket the lookup price.
dom_store_fill_at_price :: proc(store: ^DOM_Store, price: f64) -> (buy_vol, sell_vol: f64) {
	if store == nil || store.level_count <= 0 do return 0, 0
	group := store.price_group > 0 ? store.price_group : 1.0
	bucket := math.floor(price / group) * group
	return dom_store_get_fill(store, bucket)
}

// S149: Maximum fill volume across all levels (for normalization).
dom_store_max_fill :: proc(store: ^DOM_Store) -> f64 {
	max_vol := f64(0)
	for i in 0 ..< store.level_count {
		lv := store.levels[i].buy_vol + store.levels[i].sell_vol
		if lv > max_vol do max_vol = lv
	}
	return max_vol
}
