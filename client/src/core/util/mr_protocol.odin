package util

// MR wire protocol structs and two-pass parser.
// Matches the Go backend's session.go JSON frames exactly.
//
// Approach: Odin's json.unmarshal ignores unknown fields, so we parse twice:
//   1. Envelope-only struct → get type + subject
//   2. Typed frame struct (same raw bytes) → get payload


// --- Server → Client frame types ---

MR_Frame_Type :: enum u8 {
	Unknown,
	Event,
	Snapshot,
	Ack,
	Error,
	Range,
	Last,
}

// First-pass: envelope fields only (payload is ignored by unmarshal).
MR_Envelope :: struct {
	type_str:   string `json:"type"`,
	subject:    string `json:"subject"`,
	seq:        i64    `json:"seq"`,
	ts_ingest:  i64    `json:"ts_ingest"`,
	op:         string `json:"op"`,
	request_id: string `json:"request_id"`,
}

// --- Payload structs per stream type ---

MR_Trade :: struct {
	price:        f64    `json:"Price"`,
	size:         f64    `json:"Size"`,
	side:         string `json:"Side"`,
	trade_id:     string `json:"TradeID"`,
	timestamp_ms: i64    `json:"Timestamp"`,
}

MR_Price_Level :: struct {
	price: f64 `json:"Price"`,
	size:  f64 `json:"Size"`,
}

MR_Book_Delta :: struct {
	bids:         []MR_Price_Level `json:"Bids"`,
	asks:         []MR_Price_Level `json:"Asks"`,
	is_snapshot:  bool             `json:"IsSnapshot"`,
	timestamp_ms: i64              `json:"Timestamp"`,
}

// Stats payload — flat, matches Go StatsWindowV1 (no json tags → PascalCase).
MR_Stats_Payload :: struct {
	liq_buy_volume:    f64 `json:"LiqBuyVolume"`,
	liq_sell_volume:   f64 `json:"LiqSellVolume"`,
	mark_price_close:  f64 `json:"MarkPriceClose"`,
	funding_rate_last: f64 `json:"FundingRateLast"`,
	window_start_ts:   i64 `json:"WindowStartTs"`,
	window_end_ts:     i64 `json:"WindowEndTs"`,
}

// Compatibility wrapper for payloads encoded as {"Stats": {...}}.
MR_Stats_Wrapper :: struct {
	stats: MR_Stats_Payload `json:"Stats"`,
}

MR_Heatmap_Cell :: struct {
	price_bucket_low:  f64 `json:"price_bucket_low"`,
	price_bucket_high: f64 `json:"price_bucket_high"`,
	bid_liquidity:     f64 `json:"bid_liquidity"`,
	ask_liquidity:     f64 `json:"ask_liquidity"`,
	trade_volume:      f64 `json:"trade_volume"`,
}

MR_Heatmap :: struct {
	cells:           []MR_Heatmap_Cell `json:"cells"`,
	window_start_ts: i64               `json:"window_start_ts"`,
	window_end_ts:   i64               `json:"window_end_ts"`,
}

MR_VPVR_Bucket :: struct {
	price_low:    f64 `json:"price_low"`,
	price_high:   f64 `json:"price_high"`,
	buy_volume:   f64 `json:"buy_volume"`,
	sell_volume:  f64 `json:"sell_volume"`,
	total_volume: f64 `json:"total_volume"`,
}

MR_VPVR :: struct {
	buckets:         []MR_VPVR_Bucket `json:"buckets"`,
	poc_price:       f64              `json:"poc_price"`,
	value_area_low:  f64              `json:"value_area_low"`,
	value_area_high: f64              `json:"value_area_high"`,
	window_start_ts: i64              `json:"window_start_ts"`,
	window_end_ts:   i64              `json:"window_end_ts"`,
}

// --- Second-pass typed frame structs (only the payload field matters) ---

MR_Trade_Frame :: struct {
	payload: MR_Trade `json:"payload"`,
}

MR_Book_Delta_Frame :: struct {
	payload: MR_Book_Delta `json:"payload"`,
}

MR_Stats_Frame :: struct {
	payload: MR_Stats_Payload `json:"payload"`,
}

MR_Stats_Frame_Wrapped :: struct {
	payload: MR_Stats_Wrapper `json:"payload"`,
}

MR_Heatmap_Frame :: struct {
	payload: MR_Heatmap `json:"payload"`,
}

MR_VPVR_Frame :: struct {
	payload: MR_VPVR `json:"payload"`,
}

// --- Parse helpers ---

parse_frame_type :: proc(s: string) -> MR_Frame_Type {
	switch s {
	case "event":    return .Event
	case "snapshot": return .Snapshot
	case "ack":      return .Ack
	case "error":    return .Error
	case "range":    return .Range
	case "last":     return .Last
	}
	return .Unknown
}
