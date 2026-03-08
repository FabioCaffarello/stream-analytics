package services

// S47: Analytics store — ring buffer for analytics substrate entries.
// Stores Open Interest, Delta Volume, CVD, and Bar Stats in a unified
// flat structure with kind-dependent value slots. Zero allocation.

Analytics_Kind :: enum u8 {
	Open_Interest,
	Delta_Volume,
	CVD,
	Bar_Stats,
}

// Value slot indices per kind (documented for widget consumers):
//   OI:  [0]=open_interest, [1]=delta, [2]=delta_pct
//   DV:  [0]=buy_vol, [1]=sell_vol, [2]=delta_vol
//   CVD: [0]=delta_vol, [1]=cvd
//   BS:  [0]=trade_count, [1]=buy_count, [2]=sell_count, [3]=total_vol,
//        [4]=buy_vol, [5]=sell_vol, [6]=vwap, [7]=imbalance

ANALYTICS_VALUE_SLOTS :: 8

Analytics_Entry :: struct {
	kind:            Analytics_Kind,
	ts_ms:           i64,
	seq:             i64,
	window_start_ms: i64,
	window_end_ms:   i64,
	values:          [ANALYTICS_VALUE_SLOTS]f64,
	flags:           u8,   // bit 0 = is_burst (Bar_Stats)
	cadence_hint_ms: i64,  // estimated inter-arrival interval (0 = unknown); OI only
	confidence:      u8,   // 0=unknown, 1=high, 2=medium, 3=low; OI only
}

ANALYTICS_STORE_CAP :: 64

Analytics_Store :: struct {
	entries: [ANALYTICS_STORE_CAP]Analytics_Entry,
	head:    int,
	count:   int,
}

push_analytics :: proc(store: ^Analytics_Store, entry: Analytics_Entry) {
	store.entries[store.head] = entry
	store.head = (store.head + 1) % ANALYTICS_STORE_CAP
	if store.count < ANALYTICS_STORE_CAP {
		store.count += 1
	}
}

// Get entry at logical index i (0 = most recent).
get_analytics :: proc(store: ^Analytics_Store, i: int) -> Analytics_Entry {
	if i >= store.count do return {}
	idx := (store.head - 1 - i + ANALYTICS_STORE_CAP) % ANALYTICS_STORE_CAP
	return store.entries[idx]
}

// Get the latest entry of a specific kind. Returns entry and true if found.
get_analytics_latest :: proc(store: ^Analytics_Store, kind: Analytics_Kind) -> (Analytics_Entry, bool) {
	for i := 0; i < store.count; i += 1 {
		idx := (store.head - 1 - i + ANALYTICS_STORE_CAP) % ANALYTICS_STORE_CAP
		if store.entries[idx].kind == kind {
			return store.entries[idx], true
		}
	}
	return {}, false
}

// Count entries of a specific kind.
analytics_count_by_kind :: proc(store: ^Analytics_Store, kind: Analytics_Kind) -> int {
	n := 0
	for i := 0; i < store.count; i += 1 {
		idx := (store.head - 1 - i + ANALYTICS_STORE_CAP) % ANALYTICS_STORE_CAP
		if store.entries[idx].kind == kind do n += 1
	}
	return n
}

// Clear the store.
analytics_store_clear :: proc(store: ^Analytics_Store) {
	store.head = 0
	store.count = 0
}
