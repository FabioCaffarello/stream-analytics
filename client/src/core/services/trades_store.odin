package services

// Ring-buffer trade store. Fixed capacity, zero allocation after init.
// Trades are stored newest-first for efficient recent-trade display.

Trade_Side :: enum u8 {
	Buy,
	Sell,
}

Trade_Entry :: struct {
	price: f64,
	qty:   f64,
	side:  Trade_Side,
	unix:  i64,
}

TRADES_CAP :: 256

Trades_Store :: struct {
	trades: [TRADES_CAP]Trade_Entry,
	head:   int, // next write position
	count:  int, // valid entries (≤ TRADES_CAP)
}

push_trade :: proc(store: ^Trades_Store, entry: Trade_Entry) {
	store.trades[store.head] = entry
	store.head = (store.head + 1) % TRADES_CAP
	if store.count < TRADES_CAP {
		store.count += 1
	}
}

// Get trade at logical index i (0 = most recent).
get_trade :: proc(store: ^Trades_Store, i: int) -> Trade_Entry {
	if i >= store.count do return {}
	idx := (store.head - 1 - i + TRADES_CAP) % TRADES_CAP
	return store.trades[idx]
}

// Fill store with deterministic fake data for demo.
fill_demo_trades :: proc(store: ^Trades_Store) {
	base_price := 42150.0
	base_time  : i64 = 1708000000

	for i in 0 ..< TRADES_CAP {
		// Deterministic pseudo-random using simple LCG.
		seed := u32(i) * 2654435761 // Knuth multiplicative hash
		price_offset := f64(i32(seed % 2000) - 1000) * 0.01
		qty := f64((seed >> 8) % 500 + 1) * 0.001
		side := Trade_Side(seed % 2)

		push_trade(store, Trade_Entry{
			price = base_price + price_offset,
			qty   = qty,
			side  = side,
			unix  = base_time + i64(i),
		})
	}
}
