package services

// Fixed-capacity VPVR (Volume Profile Visible Range) store.
// Buckets hold buy/sell volume per price level.
// Zero allocation after init.

import "core:math"

VPVR_BUCKET_CAP :: 200

VPVR_Bucket :: struct {
	price:      f64,
	buy_volume: f64,
	sell_volume: f64,
}

VPVR_Store :: struct {
	buckets:     [VPVR_BUCKET_CAP]VPVR_Bucket,
	count:       int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	max_volume:  f64, // max(buy+sell) across all buckets
	poc_index:   int, // Point of Control (highest volume bucket)
}

// Bulk update from server data.
update_vpvr :: proc(
	store: ^VPVR_Store,
	prices: [^]f64,
	buys: [^]f64,
	sells: [^]f64,
	count: int,
	price_group: f64,
) {
	n := min(count, VPVR_BUCKET_CAP)
	store.count = n
	store.price_group = price_group
	store.max_volume = 0
	store.poc_index = 0

	if n == 0 do return

	store.min_price = prices[0]
	store.max_price = prices[0]

	for i in 0 ..< n {
		store.buckets[i] = VPVR_Bucket{
			price      = prices[i],
			buy_volume = buys[i],
			sell_volume = sells[i],
		}

		total := buys[i] + sells[i]
		if total > store.max_volume {
			store.max_volume = total
			store.poc_index = i
		}

		store.min_price = math.min(store.min_price, prices[i])
		store.max_price = math.max(store.max_price, prices[i])
	}
}

get_vpvr_bucket :: proc(store: ^VPVR_Store, i: int) -> VPVR_Bucket {
	if i >= store.count do return {}
	return store.buckets[i]
}

// Compute the value area (% of volume around POC).
// Returns (vah_idx, val_idx) — Value Area High/Low bucket indices.
compute_value_area :: proc(store: ^VPVR_Store, pct: f64) -> (vah_idx: int, val_idx: int) {
	if store.count == 0 do return 0, 0

	// Total volume.
	total_vol: f64 = 0
	for i in 0 ..< store.count {
		total_vol += store.buckets[i].buy_volume + store.buckets[i].sell_volume
	}
	if total_vol <= 0 do return 0, store.count - 1

	target := total_vol * pct

	// Expand outward from POC.
	lo := store.poc_index
	hi := store.poc_index
	accum := store.buckets[store.poc_index].buy_volume + store.buckets[store.poc_index].sell_volume

	for accum < target && (lo > 0 || hi < store.count - 1) {
		vol_lo: f64 = 0
		vol_hi: f64 = 0
		if lo > 0 {
			vol_lo = store.buckets[lo - 1].buy_volume + store.buckets[lo - 1].sell_volume
		}
		if hi < store.count - 1 {
			vol_hi = store.buckets[hi + 1].buy_volume + store.buckets[hi + 1].sell_volume
		}

		if vol_lo >= vol_hi && lo > 0 {
			lo -= 1
			accum += vol_lo
		} else if hi < store.count - 1 {
			hi += 1
			accum += vol_hi
		} else if lo > 0 {
			lo -= 1
			accum += vol_lo
		} else {
			break
		}
	}

	return hi, lo // vah = high index, val = low index
}

// Fill with deterministic demo data (Gaussian-ish distribution).
fill_demo_vpvr :: proc(store: ^VPVR_Store) {
	NUM_LEVELS :: 80
	base_price := 42000.0
	price_group := 25.0
	center := NUM_LEVELS / 2

	store.count = NUM_LEVELS
	store.price_group = price_group
	store.min_price = base_price
	store.max_price = base_price + f64(NUM_LEVELS - 1) * price_group
	store.max_volume = 0
	store.poc_index = 0

	for i in 0 ..< NUM_LEVELS {
		price := base_price + f64(i) * price_group

		// Gaussian-ish envelope: peak at center, tails fall off.
		dist := f64(i - center)
		sigma := f64(NUM_LEVELS) * 0.25
		envelope := math.exp(-(dist * dist) / (2 * sigma * sigma))

		// LCG noise for variation.
		seed := u32(i + 1) * 2654435761
		noise := f64(seed % 1000) * 0.001 // 0..1

		buy_vol  := envelope * (200 + noise * 100)
		sell_vol := envelope * (180 + (1 - noise) * 80)

		store.buckets[i] = VPVR_Bucket{
			price      = price,
			buy_volume = buy_vol,
			sell_volume = sell_vol,
		}

		total := buy_vol + sell_vol
		if total > store.max_volume {
			store.max_volume = total
			store.poc_index = i
		}
	}
}
