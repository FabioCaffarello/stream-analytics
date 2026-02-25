package services

// Ring-buffer stats store. Fixed capacity, zero allocation after init.
// Stores newest-first for efficient recent-stats display.

Stats_Entry :: struct {
	mark_price: f64,
	funding:    f64,
	liq_buy:    f64,
	liq_sell:   f64,
	unix:       i64,
}

STATS_CAP :: 64

Stats_Store :: struct {
	stats: [STATS_CAP]Stats_Entry,
	head:  int,
	count: int,
}

push_stats :: proc(store: ^Stats_Store, entry: Stats_Entry) {
	store.stats[store.head] = entry
	store.head = (store.head + 1) % STATS_CAP
	if store.count < STATS_CAP {
		store.count += 1
	}
}

// Get stat at logical index i (0 = most recent).
get_stats :: proc(store: ^Stats_Store, i: int) -> Stats_Entry {
	if i >= store.count do return {}
	idx := (store.head - 1 - i + STATS_CAP) % STATS_CAP
	return store.stats[idx]
}

// Fill store with deterministic demo data (8 bars matching previous hardcoded sample).
fill_demo_stats :: proc(store: ^Stats_Store) {
	samples := [?]Stats_Entry{
		{mark_price = 42150, funding = 0.0001, liq_buy = 42, liq_sell = 18, unix = 1000},
		{mark_price = 42160, funding = 0.0002, liq_buy = 15, liq_sell = 55, unix = 1060},
		{mark_price = 42140, funding = 0.0001, liq_buy = 70, liq_sell = 30, unix = 1120},
		{mark_price = 42170, funding = 0.0003, liq_buy = 25, liq_sell = 60, unix = 1180},
		{mark_price = 42155, funding = 0.0001, liq_buy = 50, liq_sell = 50, unix = 1240},
		{mark_price = 42180, funding = 0.0002, liq_buy = 80, liq_sell = 10, unix = 1300},
		{mark_price = 42130, funding = 0.0001, liq_buy = 12, liq_sell = 75, unix = 1360},
		{mark_price = 42165, funding = 0.0002, liq_buy = 45, liq_sell = 35, unix = 1420},
	}
	for s in samples {
		push_stats(store, s)
	}
}
