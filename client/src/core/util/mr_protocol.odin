package util

// MR wire protocol structs and two-pass parser.
// Matches the Go backend's session.go JSON frames exactly.
//
// Approach: Odin's json.unmarshal ignores unknown fields, so we parse twice:
//   1. Envelope-only struct → get type + subject
//   2. Typed frame struct (same raw bytes) → get payload


// --- Transport mode ---

Transport_Mode :: enum u8 {
	Terminal_V1,  // New protocol with HELLO, PING/PONG, RESYNC, METRICS
	Legacy_JSON,  // Old protocol without handshake (auto-downgrade)
}

// --- Server → Client frame types ---

MR_Frame_Type :: enum u8 {
	Unknown,
	Event,
	Snapshot,
	Ack,
	Error,
	Range,
	Last,
	Hello,
	Heartbeat,
	Health,
	Pong,
	Metrics,
}

MR_PROTO_VER :: 1

// First-pass: envelope fields only (payload is ignored by unmarshal).
// Terminal_V1 adds stream_id, ts_server, venue, symbol, channel, protocol_version, server_instance_id.
MR_Envelope :: struct {
	type_str:           string `json:"type"`,
	subject:            string `json:"subject"`,
	seq:                i64    `json:"seq"`,
	ts_ingest:          i64    `json:"ts_ingest"`,
	op:                 string `json:"op"`,
	request_id:         string `json:"request_id"`,
	// Terminal_V1 extended fields (absent in Legacy_JSON mode).
	stream_id:          string `json:"stream_id"`,
	protocol_version:   int    `json:"protocol_version"`,
	server_instance_id: string `json:"server_instance_id"`,
	ts_server:          i64    `json:"ts_server"`,
	venue:              string `json:"venue"`,
	symbol:             string `json:"symbol"`,
	channel:            string `json:"channel"`,
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

// Candle payload — matches Go AggregationCandleV1 (PascalCase, no json tags).
MR_Candle_Payload :: struct {
	Venue:         string `json:"Venue"`,
	Instrument:    string `json:"Instrument"`,
	Timeframe:     string `json:"Timeframe"`,
	WindowStartTs: i64    `json:"WindowStartTs"`,
	WindowEndTs:   i64    `json:"WindowEndTs"`,
	Open:          f64    `json:"Open"`,
	High:          f64    `json:"High"`,
	Low:           f64    `json:"Low"`,
	ClosePrice:    f64    `json:"ClosePrice"`,
	Volume:        f64    `json:"Volume"`,
	BuyVolume:     f64    `json:"BuyVolume"`,
	SellVolume:    f64    `json:"SellVolume"`,
	TradeCount:    i64    `json:"TradeCount"`,
	SeqFirst:      i64    `json:"SeqFirst"`,
	SeqLast:       i64    `json:"SeqLast"`,
	IsClosed:      bool   `json:"IsClosed"`,
}

// Backend wraps candle in {"Candle": {...}}.
MR_Candle_Wrapped :: struct {
	candle: MR_Candle_Payload `json:"Candle"`,
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

MR_Candle_Frame :: struct {
	payload: MR_Candle_Wrapped `json:"payload"`,
}

// Flat candle frame for payloads not wrapped in {"Candle": {...}}.
MR_Candle_Frame_Flat :: struct {
	payload: MR_Candle_Payload `json:"payload"`,
}

MR_Hello_Capabilities :: struct {
	topics:  []string `json:"topics"`,
	venues:  []string `json:"venues"`,
	symbols: []string `json:"symbols"`,
}

MR_Hello_Payload :: struct {
	proto_ver:          int                   `json:"proto_ver"`,
	protocol_version:   int                   `json:"protocol_version"`,
	server_time:        i64                   `json:"server_time"`,
	server_instance_id: string                `json:"server_instance_id"`,
	capabilities:       MR_Hello_Capabilities `json:"capabilities"`,
}

MR_Hello_Frame :: struct {
	payload: MR_Hello_Payload `json:"payload"`,
}

// --- Pong frame (Terminal_V1) ---

MR_Pong_Payload :: struct {
	ts_client:  i64    `json:"ts_client"`,
	ts_server:  i64    `json:"ts_server"`,
	request_id: string `json:"request_id"`,
}

MR_Pong_Frame :: struct {
	type_str:   string          `json:"type"`,
	ts_client:  i64             `json:"ts_client"`,
	ts_server:  i64             `json:"ts_server"`,
	request_id: string          `json:"request_id"`,
}

// --- Metrics frame (Terminal_V1, server-pushed telemetry) ---

MR_Metrics_Payload :: struct {
	ws_dropped_total:                i64 `json:"ws_dropped_total"`,
	ws_queue_len:                    int `json:"ws_queue_len"`,
	ws_lag_ms:                       i64 `json:"ws_lag_ms"`,
	publish_to_deliver_latency_ms:   i64 `json:"publish_to_deliver_latency_ms"`,
	serialize_errors_total:          i64 `json:"serialize_errors_total"`,
	resync_total:                    i64 `json:"resync_total"`,
	active_subscriptions:            int `json:"active_subscriptions"`,
	messages_out_total:              i64 `json:"messages_out_total"`,
}

MR_Metrics_Frame :: struct {
	type_str: string             `json:"type"`,
	payload:  MR_Metrics_Payload `json:"payload"`,
}

// --- Error frame with problem sub-struct (Terminal_V1) ---

MR_Problem :: struct {
	code:    string `json:"code"`,
	message: string `json:"message"`,
}

MR_Error_Frame :: struct {
	type_str:   string     `json:"type"`,
	op:         string     `json:"op"`,
	request_id: string     `json:"request_id"`,
	problem:    MR_Problem `json:"problem"`,
}

// --- ACK frame with stream_id (Terminal_V1) ---

MR_Ack_Frame :: struct {
	type_str:   string `json:"type"`,
	op:         string `json:"op"`,
	request_id: string `json:"request_id"`,
	subject:    string `json:"subject"`,
	stream_id:  string `json:"stream_id"`,
}

// --- Range response frame (getrange) ---
// Server sends: {"type":"range","op":"getrange","request_id":"...","subject":"...","items":[...]}
// Each item has Seq, TsIngest, and Payload (inline JSON after Go-side fix to json.RawMessage).

MR_Range_Item :: struct {
	seq:       i64 `json:"Seq"`,
	ts_ingest: i64 `json:"TsIngest"`,
	// Payload is inline JSON (candle payload) after RangeItem.Payload -> json.RawMessage fix.
	payload:   MR_Candle_Wrapped `json:"Payload"`,
}

// Flat payload variant used by some range responses:
// {"Payload": {"Venue":"...", "WindowStartTs":...}}
MR_Range_Item_Flat :: struct {
	seq:       i64               `json:"Seq"`,
	ts_ingest: i64               `json:"TsIngest"`,
	payload:   MR_Candle_Payload `json:"Payload"`,
}

MR_Range_Frame :: struct {
	type_str:   string           `json:"type"`,
	op:         string           `json:"op"`,
	request_id: string           `json:"request_id"`,
	subject:    string           `json:"subject"`,
	items:      []MR_Range_Item  `json:"items"`,
}

MR_Range_Frame_Flat :: struct {
	type_str:   string                `json:"type"`,
	op:         string                `json:"op"`,
	request_id: string                `json:"request_id"`,
	subject:    string                `json:"subject"`,
	items:      []MR_Range_Item_Flat  `json:"items"`,
}

// --- Parse helpers ---

parse_frame_type :: proc(s: string) -> MR_Frame_Type {
	switch s {
	case "event":     return .Event
	case "snapshot":  return .Snapshot
	case "ack":       return .Ack
	case "error":     return .Error
	case "range":     return .Range
	case "last":      return .Last
	case "hello":     return .Hello
	case "heartbeat": return .Heartbeat
	case "health":    return .Health
	case "pong":      return .Pong
	case "metrics":   return .Metrics
	}
	return .Unknown
}
