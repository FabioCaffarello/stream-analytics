package services

// Fixed-capacity orderbook store. Zero allocation after init.
// Asks sorted ascending (best ask = index 0).
// Bids sorted descending (best bid = index 0).

import "core:math"

OB_DEPTH_CAP :: 50

Orderbook_Store :: struct {
	ask_prices: [OB_DEPTH_CAP]f64,
	ask_sizes:  [OB_DEPTH_CAP]f64,
	bid_prices: [OB_DEPTH_CAP]f64,
	bid_sizes:  [OB_DEPTH_CAP]f64,
	ask_count:  int,
	bid_count:  int,
	last_price: f64,
	unix:       i64,
}

// Replace entire orderbook snapshot. Counts are clamped to OB_DEPTH_CAP.
update_orderbook :: proc(
	store: ^Orderbook_Store,
	ask_prices, ask_sizes: []f64,
	bid_prices, bid_sizes: []f64,
	last_price: f64,
	unix: i64,
) {
	store.ask_count = min(len(ask_prices), OB_DEPTH_CAP)
	store.bid_count = min(len(bid_prices), OB_DEPTH_CAP)
	store.last_price = last_price
	store.unix = unix

	for i in 0 ..< store.ask_count {
		store.ask_prices[i] = ask_prices[i]
		store.ask_sizes[i]  = ask_sizes[i]
	}
	for i in 0 ..< store.bid_count {
		store.bid_prices[i] = bid_prices[i]
		store.bid_sizes[i]  = bid_sizes[i]
	}
}

get_ask :: proc(store: ^Orderbook_Store, i: int) -> (price, size: f64) {
	if i >= store.ask_count do return 0, 0
	return store.ask_prices[i], store.ask_sizes[i]
}

get_bid :: proc(store: ^Orderbook_Store, i: int) -> (price, size: f64) {
	if i >= store.bid_count do return 0, 0
	return store.bid_prices[i], store.bid_sizes[i]
}

best_ask :: proc(store: ^Orderbook_Store) -> f64 {
	if store.ask_count == 0 do return 0
	return store.ask_prices[0]
}

best_bid :: proc(store: ^Orderbook_Store) -> f64 {
	if store.bid_count == 0 do return 0
	return store.bid_prices[0]
}

spread :: proc(store: ^Orderbook_Store) -> f64 {
	ba := best_ask(store)
	bb := best_bid(store)
	if ba == 0 || bb == 0 do return 0
	return ba - bb
}

mid_price :: proc(store: ^Orderbook_Store) -> f64 {
	ba := best_ask(store)
	bb := best_bid(store)
	if ba == 0 || bb == 0 do return store.last_price
	return (ba + bb) * 0.5
}

// Fill with deterministic demo data. 25 levels per side.
// Base ask=42200, base bid=42150, step=10, LCG sizes.
fill_demo_orderbook :: proc(store: ^Orderbook_Store) {
	DEMO_LEVELS :: 25
	store.ask_count = DEMO_LEVELS
	store.bid_count = DEMO_LEVELS
	store.last_price = 42175.0
	store.unix = 1708000256

	base_ask := 42200.0
	base_bid := 42150.0

	for i in 0 ..< DEMO_LEVELS {
		// Asks ascending from best ask.
		store.ask_prices[i] = base_ask + f64(i) * 10.0
		seed_a := u32(i + 1) * 2654435761
		store.ask_sizes[i] = f64((seed_a % 500) + 10) * 0.01

		// Bids descending from best bid.
		store.bid_prices[i] = base_bid - f64(i) * 10.0
		seed_b := u32(i + 100) * 2654435761
		store.bid_sizes[i] = f64((seed_b % 500) + 10) * 0.01
	}
}
