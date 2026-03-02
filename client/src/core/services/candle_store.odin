package services

// Ring-buffer candle store. Fixed capacity, zero allocation after init.
// Stores candles in chronological order (oldest at tail, newest at head-1).
// Ring semantics: when full, oldest candle is evicted.

CANDLE_CAP :: 750

Candle_Entry :: struct {
	open:            f64,
	high:            f64,
	low:             f64,
	close:           f64,
	volume:          f64,
	buy_vol:         f64,
	sell_vol:        f64,
	trade_count:     i64,
	window_start_ts: i64,  // Unix ms
	window_end_ts:   i64,  // Unix ms
	is_closed:       bool,
}

Candle_Store :: struct {
	candles: [CANDLE_CAP]Candle_Entry,
	head:    int,  // next write position
	count:   int,  // valid entries (≤ CANDLE_CAP)
}

// Push a candle into the store. If the latest candle has the same window_start_ts,
// it is updated in-place (open candle replaced by closed). Otherwise appended.
push_candle :: proc(store: ^Candle_Store, entry: Candle_Entry) {
	// Check if we should update the last candle (same window start = same bar).
	if store.count > 0 {
		last_idx := (store.head - 1 + CANDLE_CAP) % CANDLE_CAP
		if store.candles[last_idx].window_start_ts == entry.window_start_ts {
			store.candles[last_idx] = entry
			return
		}
	}
	store.candles[store.head] = entry
	store.head = (store.head + 1) % CANDLE_CAP
	if store.count < CANDLE_CAP {
		store.count += 1
	}
}

// Get candle at logical index i (0 = oldest visible candle).
get_candle :: proc(store: ^Candle_Store, i: int) -> Candle_Entry {
	if i < 0 || i >= store.count do return {}
	// Oldest entry is at (head - count) wrapped.
	idx := (store.head - store.count + i + CANDLE_CAP) % CANDLE_CAP
	return store.candles[idx]
}

@(private = "file")
logical_to_raw_idx :: proc(store: ^Candle_Store, i: int) -> int {
	return (store.head - store.count + i + CANDLE_CAP) % CANDLE_CAP
}

// Bulk-load historical candles (oldest first). Clears existing data.
// Used for GetRange responses before live streaming resumes.
bulk_load_candles :: proc(store: ^Candle_Store, entries: []Candle_Entry) {
	store.head = 0
	store.count = 0
	for e in entries {
		push_candle(store, e)
	}
}

// Get candle at logical index i (0 = most recent).
get_candle_newest :: proc(store: ^Candle_Store, i: int) -> Candle_Entry {
	if i < 0 || i >= store.count do return {}
	idx := (store.head - 1 - i + CANDLE_CAP) % CANDLE_CAP
	return store.candles[idx]
}

// Upsert a candle while preserving chronological order by window_start_ts.
// Used for historical range batches that can contain older candles.
// If store is full, older-than-oldest inserts are ignored and newer-than-newest
// inserts evict the oldest candle, matching ring semantics.
upsert_candle_chrono :: proc(store: ^Candle_Store, entry: Candle_Entry) {
	if entry.window_start_ts <= 0 do return
	if entry.window_end_ts <= entry.window_start_ts do return

	// Fast path for empty store.
	if store.count <= 0 {
		push_candle(store, entry)
		return
	}

	n := store.count
	insert_idx := n
	for i in 0 ..< n {
		c := get_candle(store, i)
		if c.window_start_ts == entry.window_start_ts {
			raw := logical_to_raw_idx(store, i)
			store.candles[raw] = entry
			return
		}
		if c.window_start_ts > entry.window_start_ts {
			insert_idx = i
			break
		}
	}

	// Simple append for newer candles while capacity remains.
	if insert_idx == n && n < CANDLE_CAP {
		push_candle(store, entry)
		return
	}

	scratch: [CANDLE_CAP]Candle_Entry
	for i in 0 ..< n {
		scratch[i] = get_candle(store, i)
	}

	// Full-store policy:
	// - newer-than-newest: drop oldest and append
	// - older-than-oldest or mid-window missing gap: ignore
	if n >= CANDLE_CAP {
		if insert_idx == n {
			for i in 1 ..< n {
				scratch[i - 1] = scratch[i]
			}
			scratch[n - 1] = entry
			bulk_load_candles(store, scratch[:n])
		}
		return
	}

	// Insert into non-full store at computed sorted position.
	for i := n; i > insert_idx; i -= 1 {
		scratch[i] = scratch[i - 1]
	}
	scratch[insert_idx] = entry
	bulk_load_candles(store, scratch[:n + 1])
}

// Fill store with deterministic demo candles for offline mode.
fill_demo_candles :: proc(store: ^Candle_Store) {
	base_price := f64(65000)
	base_ts := i64(1710000000) // unix seconds

	offsets := [?]f64{
		0, 50, -30, 80, -20, 100, -50, 120, -10, 60,
		-40, 90, 30, -70, 110, -80, 150, -30, 70, -20,
	}

	for i in 0 ..< len(offsets) {
		open := base_price + offsets[i]
		delta := offsets[(i + 3) % len(offsets)] * 0.5
		close := open + delta
		high := max(open, close) + 30
		low := min(open, close) - 25
		ts := base_ts + i64(i) * 60

		push_candle(store, Candle_Entry{
			open            = open,
			high            = high,
			low             = low,
			close           = close,
			volume          = 10.5 + f64(i % 5) * 2.0,
			buy_vol         = 6.0 + f64(i % 3),
			sell_vol        = 4.5 + f64(i % 4),
			trade_count     = i64(80 + i * 5),
			window_start_ts = ts * 1000,
			window_end_ts   = (ts + 60) * 1000,
			is_closed       = true,
		})
	}
}
