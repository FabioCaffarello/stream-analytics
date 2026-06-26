package services

// Market discovery service — fixed-capacity store of available exchanges/symbols.
// Populated from backend GET /api/v1/markets or falls back to static defaults.

import "core:encoding/json"

MARKET_CAP   :: 64
EXCHANGE_CAP :: 16

Market_Entry :: struct {
	venue:       string,
	ticker:      string,
	tick_size:   f64,
	market_type: string,
}

Markets_Store :: struct {
	entries: [MARKET_CAP]Market_Entry,
	count:   int,
	loaded:  bool,
}

// Reset store and populate from static defaults.
markets_load_defaults :: proc(store: ^Markets_Store) {
	store.count = 0
	store.loaded = false

	Default :: struct { venue, ticker, market_type: string, tick_size: f64 }
	defaults := [?]Default{
		{"binance-spot",    "BTCUSDT",    "SPOT",          0.01},
		{"binance-spot",    "ETHUSDT",    "SPOT",          0.01},
		{"binance-spot",    "SOLUSDT",    "SPOT",          0.01},
		{"binance-futures", "BTCUSDT",    "USD_M_FUTURES", 0.01},
		{"binance-futures", "ETHUSDT",    "USD_M_FUTURES", 0.01},
		{"binance-futures", "SOLUSDT",    "USD_M_FUTURES", 0.01},
		{"bybit",           "BTCUSDT",    "USD_M_FUTURES", 0.01},
		{"bybit",           "ETHUSDT",    "USD_M_FUTURES", 0.01},
		{"bybit",           "SOLUSDT",    "USD_M_FUTURES", 0.01},
		{"coinbase",        "BTC-USD",    "SPOT",          0.01},
		{"coinbase",        "ETH-USD",    "SPOT",          0.01},
		{"coinbase",        "SOL-USD",    "SPOT",          0.01},
		{"hyperliquid",     "BTC",        "USD_M_FUTURES", 0.1},
		{"hyperliquid",     "ETH",        "USD_M_FUTURES", 0.01},
		{"hyperliquid",     "SOL",        "USD_M_FUTURES", 0.001},
		{"kraken-spot",     "XBT/USD",    "SPOT",          0.1},
		{"kraken-spot",     "ETH/USD",    "SPOT",          0.01},
		{"kraken-futures",  "PF_XBTUSD",  "USD_M_FUTURES", 0.5},
		{"kraken-futures",  "PF_ETHUSD",  "USD_M_FUTURES", 0.01},
	}
	for d in defaults {
		if store.count >= MARKET_CAP do break
		store.entries[store.count] = Market_Entry{
			venue       = d.venue,
			ticker      = d.ticker,
			tick_size   = d.tick_size,
			market_type = d.market_type,
		}
		store.count += 1
	}
}

// JSON response types matching backend GET /api/v1/markets.
@(private = "file")
Symbol_JSON :: struct {
	ticker:      string `json:"ticker"`,
	tick_size:   f64    `json:"tick_size"`,
	market_type: string `json:"market_type"`,
}

@(private = "file")
Exchange_JSON :: struct {
	name:    string       `json:"name"`,
	symbols: []Symbol_JSON `json:"symbols"`,
}

@(private = "file")
Markets_JSON :: struct {
	exchanges: []Exchange_JSON `json:"exchanges"`,
}

// Parse JSON from /api/v1/markets and populate the store.
// Returns true on success, false on parse failure (store unchanged).
markets_parse_json :: proc(store: ^Markets_Store, data: []u8) -> bool {
	if len(data) == 0 do return false

	root: Markets_JSON
	if json.unmarshal(data, &root) != nil do return false

	store.count = 0
	store.loaded = true
	for ex in root.exchanges {
		for sym in ex.symbols {
			if store.count >= MARKET_CAP do return true
			store.entries[store.count] = Market_Entry{
				venue       = ex.name,
				ticker      = sym.ticker,
				tick_size   = sym.tick_size > 0 ? sym.tick_size : 0.01,
				market_type = sym.market_type,
			}
			store.count += 1
		}
	}
	return true
}

// Get unique venue names from the store.
markets_venue_count :: proc(store: ^Markets_Store) -> int {
	if store.count == 0 do return 0
	seen: [EXCHANGE_CAP]string
	n := 0
	for i in 0 ..< store.count {
		venue := store.entries[i].venue
		found := false
		for j in 0 ..< n {
			if seen[j] == venue { found = true; break }
		}
		if !found && n < EXCHANGE_CAP {
			seen[n] = venue
			n += 1
		}
	}
	return n
}

// Get unique venue name by index (stable order from store).
markets_venue_at :: proc(store: ^Markets_Store, idx: int) -> string {
	seen: [EXCHANGE_CAP]string
	n := 0
	for i in 0 ..< store.count {
		venue := store.entries[i].venue
		found := false
		for j in 0 ..< n {
			if seen[j] == venue { found = true; break }
		}
		if !found && n < EXCHANGE_CAP {
			if n == idx do return venue
			seen[n] = venue
			n += 1
		}
	}
	return ""
}

// Count symbols for a given venue.
markets_symbol_count :: proc(store: ^Markets_Store, venue: string) -> int {
	n := 0
	for i in 0 ..< store.count {
		if store.entries[i].venue == venue do n += 1
	}
	return n
}

// Get symbol entry for a given venue by index.
markets_symbol_at :: proc(store: ^Markets_Store, venue: string, idx: int) -> ^Market_Entry {
	n := 0
	for i in 0 ..< store.count {
		if store.entries[i].venue == venue {
			if n == idx do return &store.entries[i]
			n += 1
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// S20: Session bootstrap parser (GET /api/v1/session)
// ---------------------------------------------------------------------------

Session_Bootstrap :: struct {
	server_time_ms: i64,
	ready:          bool,
}

@(private = "file")
Session_Market_JSON :: struct {
	venue:       string   `json:"venue"`,
	instruments: []string `json:"instruments"`,
}

@(private = "file")
Session_JSON :: struct {
	server_time_ms: i64                  `json:"server_time_ms"`,
	ready:          bool                 `json:"ready"`,
	markets:        []Session_Market_JSON `json:"markets"`,
}

// Parse GET /api/v1/session response. Populates bootstrap fields and optionally
// merges markets into the store (instruments only, no tick_size/market_type —
// those come from the existing fetch_markets path or defaults).
session_parse_json :: proc(store: ^Markets_Store, data: []u8, out: ^Session_Bootstrap) -> bool {
	if len(data) == 0 do return false
	if out == nil do return false

	root: Session_JSON
	if json.unmarshal(data, &root) != nil do return false

	out.server_time_ms = root.server_time_ms
	out.ready = root.ready

	// Merge session markets into store — only add entries that don't already exist.
	// Session markets lack tick_size/market_type so existing defaults take priority.
	for m in root.markets {
		if len(m.venue) == 0 do continue
		for inst in m.instruments {
			if len(inst) == 0 do continue
			if store.count >= MARKET_CAP do break
			// Check for duplicate.
			found := false
			for i in 0 ..< store.count {
				if store.entries[i].venue == m.venue && store.entries[i].ticker == inst {
					found = true
					break
				}
			}
			if !found {
				store.entries[store.count] = Market_Entry{
					venue       = m.venue,
					ticker      = inst,
					tick_size   = 0.01,
					market_type = "SPOT",
				}
				store.count += 1
			}
		}
	}
	store.loaded = true
	return true
}

// ---------------------------------------------------------------------------
// S20: Freshness parser (GET /api/v1/freshness)
// ---------------------------------------------------------------------------

Freshness_Result :: struct {
	active:     bool,
	checked_at: i64,
	channels:   [FRESHNESS_RESULT_CAP]Freshness_Channel_Result,
	count:      int,
}

Freshness_Channel_Result :: struct {
	name:    string,
	flowing: bool,
	lag_ms:  i64,
}

FRESHNESS_RESULT_CAP :: 16

@(private = "file")
Freshness_Channel_JSON :: struct {
	last_event_ts: i64  `json:"last_event_ts"`,
	lag_ms:        i64  `json:"lag_ms"`,
	flowing:       bool `json:"flowing"`,
}

@(private = "file")
Freshness_JSON :: struct {
	venue:      string                              `json:"venue"`,
	instrument: string                              `json:"instrument"`,
	active:     bool                                `json:"active"`,
	channels:   map[string]Freshness_Channel_JSON   `json:"channels"`,
	checked_at: i64                                 `json:"checked_at"`,
}

freshness_parse_json :: proc(data: []u8, out: ^Freshness_Result) -> bool {
	if len(data) == 0 || out == nil do return false

	root: Freshness_JSON
	if json.unmarshal(data, &root) != nil do return false

	out.active = root.active
	out.checked_at = root.checked_at
	out.count = 0
	for name, ch in root.channels {
		if out.count >= FRESHNESS_RESULT_CAP do break
		out.channels[out.count] = Freshness_Channel_Result{
			name    = name,
			flowing = ch.flowing,
			lag_ms  = ch.lag_ms,
		}
		out.count += 1
	}
	return true
}

// ---------------------------------------------------------------------------
// S20: Timeline parser (GET /api/v1/timeline)
// ---------------------------------------------------------------------------

Timeline_Result :: struct {
	first_ts: i64,
	last_ts:  i64,
}

@(private = "file")
Timeline_JSON :: struct {
	venue:      string `json:"venue"`,
	instrument: string `json:"instrument"`,
	timeframe:  string `json:"timeframe"`,
	artifact:   string `json:"artifact"`,
	first_ts:   i64    `json:"first_ts"`,
	last_ts:    i64    `json:"last_ts"`,
}

timeline_parse_json :: proc(data: []u8, out: ^Timeline_Result) -> bool {
	if len(data) == 0 || out == nil do return false

	root: Timeline_JSON
	if json.unmarshal(data, &root) != nil do return false

	out.first_ts = root.first_ts
	out.last_ts = root.last_ts
	return true
}
