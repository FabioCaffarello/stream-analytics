package services

// S82: Session volume profile snapshot parser.
// Parses a JSON session VP response from GET /api/v1/insights/session-vp
// and populates Session_VPVR_Store using parallel arrays.
//
// Expected JSON format:
// {
//   "venue": "BINANCE",
//   "instrument": "BTCUSDT",
//   "session_anchor": "US_2026-03-08",
//   "window_start_ts": 1709856000000,
//   "window_end_ts": 0,
//   "is_active": true,
//   "poc_price": 67500.0,
//   "value_area_low": 67000.0,
//   "value_area_high": 68000.0,
//   "total_volume": 12345.67,
//   "buy_volume": 6789.0,
//   "sell_volume": 5556.67,
//   "buckets": [
//     {"price_low": 67000.0, "price_high": 67100.0, "buy_volume": 100.0, "sell_volume": 80.0, "total_volume": 180.0},
//     ...
//   ]
// }
//
// Uses json_extract_f64, json_extract_i64, json_extract_bool, json_find_key, find_byte
// from analytics_range.odin (same package).

import "core:math"

SESSION_VPVR_RANGE_BUDGET :: 200 // max buckets to parse (matches store cap)

// parse_session_vpvr_snapshot parses a JSON session volume profile response
// and populates the Session_VPVR_Store.
// Returns: number of buckets parsed, or 0 on failure.
parse_session_vpvr_snapshot :: proc(store: ^Session_VPVR_Store, raw: []u8) -> int {
	if store == nil || len(raw) < 2 do return 0

	// Extract top-level scalar fields.
	poc_price := json_extract_f64(raw, "poc_price")
	vah := json_extract_f64(raw, "value_area_high")
	val := json_extract_f64(raw, "value_area_low")
	total_vol := json_extract_f64(raw, "total_volume")
	buy_vol := json_extract_f64(raw, "buy_volume")
	sell_vol := json_extract_f64(raw, "sell_volume")
	start_ts := json_extract_i64(raw, "window_start_ts")
	end_ts := json_extract_i64(raw, "window_end_ts")

	// Extract session_anchor label.
	anchor := json_extract_string(raw, "session_anchor")
	if len(anchor) > 0 {
		set_session_vpvr_label(store, anchor)
	}

	// Find "buckets" array.
	buckets_key := json_find_key(raw, "buckets")
	if buckets_key < 0 do return 0

	// Find the opening '[' after the key.
	arr_start := find_byte(raw, buckets_key, '[')
	if arr_start < 0 do return 0

	// Parse bucket objects into parallel arrays.
	prices: [SESSION_VPVR_RANGE_BUDGET]f64
	buys: [SESSION_VPVR_RANGE_BUDGET]f64
	sells: [SESSION_VPVR_RANGE_BUDGET]f64

	count := 0
	pos := arr_start + 1
	poc_idx := 0
	max_total := f64(0)

	for pos < len(raw) && count < SESSION_VPVR_RANGE_BUDGET {
		obj_start := find_byte(raw, pos, '{')
		if obj_start < 0 do break
		obj_end := find_byte(raw, obj_start, '}')
		if obj_end < 0 do break

		// Check if we've left the buckets array (hit ']' before next '{').
		arr_end := find_byte(raw, pos, ']')
		if arr_end >= 0 && arr_end < obj_start do break

		obj := raw[obj_start:obj_end + 1]

		price_low := json_extract_f64(obj, "price_low")
		price_high := json_extract_f64(obj, "price_high")
		bucket_buy := json_extract_f64(obj, "buy_volume")
		bucket_sell := json_extract_f64(obj, "sell_volume")
		bucket_total := json_extract_f64(obj, "total_volume")

		// Use midpoint as the bucket price.
		prices[count] = (price_low + price_high) * 0.5
		buys[count] = bucket_buy
		sells[count] = bucket_sell

		// Track POC: bucket with highest total_volume.
		if bucket_total > max_total {
			max_total = bucket_total
			poc_idx = count
		}

		count += 1
		pos = obj_end + 1
	}

	if count == 0 do return 0

	// Use server-provided poc_price to refine poc_idx if available.
	if poc_price > 0 {
		best_dist := math.abs(prices[0] - poc_price)
		for i in 1 ..< count {
			d := math.abs(prices[i] - poc_price)
			if d < best_dist {
				best_dist = d
				poc_idx = i
			}
		}
	}

	// Populate store via update_session_vpvr.
	update_session_vpvr(
		store,
		raw_data(prices[:]),
		raw_data(buys[:]),
		raw_data(sells[:]),
		count,
		poc_idx,
		vah,
		val,
		total_vol,
		buy_vol,
		sell_vol,
		start_ts,
		end_ts,
	)

	return count
}

// parse_session_vpvr_is_active extracts the is_active boolean from the response.
parse_session_vpvr_is_active :: proc(raw: []u8) -> bool {
	if len(raw) < 2 do return false
	return json_extract_bool(raw, "is_active")
}

// parse_session_vpvr_session_times extracts window_start_ts and window_end_ts.
parse_session_vpvr_session_times :: proc(raw: []u8) -> (start_ms: i64, end_ms: i64) {
	if len(raw) < 2 do return 0, 0
	start_ms = json_extract_i64(raw, "window_start_ts")
	end_ms = json_extract_i64(raw, "window_end_ts")
	return
}

// json_extract_string extracts a string value for a given key.
// Returns a slice into the raw buffer (no allocation).
@(private = "file")
json_extract_string :: proc(obj: []u8, key: string) -> string {
	pos := json_find_key(obj, key)
	if pos < 0 do return ""
	colon := find_byte(obj, pos + len(key) + 1, ':')
	if colon < 0 do return ""
	start := colon + 1
	for start < len(obj) && (obj[start] == ' ' || obj[start] == '\t') do start += 1
	if start >= len(obj) || obj[start] != '"' do return ""
	start += 1 // skip opening quote
	end := find_byte(obj, start, '"')
	if end < 0 do return ""
	return string(obj[start:end])
}
