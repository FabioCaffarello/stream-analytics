package services

// Footprint store — per-candle volume distribution by price level.
// Client-side trade accumulation: trades bucketed into candle windows × price bins.
// Fixed capacity, zero allocation after init.

import "core:math"

FOOTPRINT_CANDLE_CAP :: 200 // max candle windows tracked
FOOTPRINT_LEVEL_CAP  :: 50  // max price levels per candle

Footprint_Level :: struct {
	price:    f64,
	buy_vol:  f64,
	sell_vol: f64,
}

Footprint_Entry :: struct {
	window_start_ts: i64,
	levels:          [FOOTPRINT_LEVEL_CAP]Footprint_Level,
	level_count:     int,
	price_group:     f64,
}

Footprint_Store :: struct {
	entries: [FOOTPRINT_CANDLE_CAP]Footprint_Entry,
	head:    int,
	count:   int,
}

footprint_store_reset :: proc(store: ^Footprint_Store) {
	store^ = {}
}

// Push a trade into the footprint store. Buckets by candle window + price level.
// trade_ts_ms: trade unix timestamp in milliseconds.
// tf_ms: active timeframe in milliseconds (e.g. 60_000 for 1m).
footprint_store_push_trade :: proc(
	store: ^Footprint_Store,
	price, qty: f64,
	is_buy: bool,
	trade_ts_ms: i64,
	tf_ms: i64,
	price_group: f64,
) {
	if qty <= 0 || price <= 0 || tf_ms <= 0 do return

	window_start := (trade_ts_ms / tf_ms) * tf_ms
	group := price_group > 0 ? price_group : 1.0
	bucket_price := math.floor(price / group) * group

	// Find existing entry for this candle window.
	entry_idx := -1
	for i in 0 ..< store.count {
		raw_idx := (store.head - store.count + i + FOOTPRINT_CANDLE_CAP) % FOOTPRINT_CANDLE_CAP
		if store.entries[raw_idx].window_start_ts == window_start {
			entry_idx = raw_idx
			break
		}
	}

	// Create new entry if not found.
	if entry_idx < 0 {
		entry_idx = store.head
		store.entries[entry_idx] = Footprint_Entry{
			window_start_ts = window_start,
			price_group     = group,
		}
		store.head = (store.head + 1) % FOOTPRINT_CANDLE_CAP
		if store.count < FOOTPRINT_CANDLE_CAP {
			store.count += 1
		}
	}

	entry := &store.entries[entry_idx]
	entry.price_group = group

	// Find or create price level in entry.
	found := false
	for li in 0 ..< entry.level_count {
		if entry.levels[li].price == bucket_price {
			if is_buy {
				entry.levels[li].buy_vol += qty
			} else {
				entry.levels[li].sell_vol += qty
			}
			found = true
			break
		}
	}
	if !found && entry.level_count < FOOTPRINT_LEVEL_CAP {
		entry.levels[entry.level_count] = Footprint_Level{
			price    = bucket_price,
			buy_vol  = is_buy ? qty : 0,
			sell_vol = is_buy ? 0 : qty,
		}
		entry.level_count += 1
	}
}

// Look up footprint data for a specific candle window.
footprint_store_get :: proc(store: ^Footprint_Store, window_start_ts: i64) -> (entry: ^Footprint_Entry, ok: bool) {
	for i in 0 ..< store.count {
		raw_idx := (store.head - store.count + i + FOOTPRINT_CANDLE_CAP) % FOOTPRINT_CANDLE_CAP
		if store.entries[raw_idx].window_start_ts == window_start_ts {
			return &store.entries[raw_idx], true
		}
	}
	return nil, false
}
