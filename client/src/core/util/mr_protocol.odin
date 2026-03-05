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
	Signal,
	Batch,
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
// Terminal_V1 adds stream_id, ts_server, venue, symbol, channel, protocol_version, server_instance_id, prev_seq.
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
	prev_seq:           i64    `json:"prev_seq"`,
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

MR_Snapshot_Price_Level :: struct {
	price:    f64 `json:"Price"`,
	quantity: f64 `json:"Quantity"`,
}

MR_Aggregation_Snapshot :: struct {
	venue:         string                    `json:"Venue"`,
	instrument:    string                    `json:"Instrument"`,
	seq:           i64                       `json:"Seq"`,
	bids:          []MR_Snapshot_Price_Level `json:"Bids"`,
	asks:          []MR_Snapshot_Price_Level `json:"Asks"`,
	best_bid_price: f64                      `json:"BestBidPrice"`,
	best_ask_price: f64                      `json:"BestAskPrice"`,
	ts_ingest_ms:  i64                       `json:"TsIngestMs"`,
}

// Stats payload — flat, matches Go StatsWindowV1 (no json tags → PascalCase).
MR_Stats_Payload :: struct {
	liq_buy_volume:    f64 `json:"LiqBuyVolume"`,
	liq_sell_volume:   f64 `json:"LiqSellVolume"`,
	mark_price_close:  f64 `json:"MarkPriceClose"`,
	funding_rate_last: f64 `json:"FundingRateLast"`,
	window_start_ts:   i64 `json:"WindowStartTs"`,
	window_end_ts:     i64 `json:"WindowEndTs"`,
	window_ms:         i64 `json:"WindowMs"`,
	ts_ingest_ms:      i64 `json:"TsIngestMs"`,
	quality_flags:     u32 `json:"QualityFlags"`,
}

// Compatibility wrapper for payloads encoded as {"Stats": {...}}.
MR_Stats_Wrapper :: struct {
	stats: MR_Stats_Payload `json:"Stats"`,
}

// Tape payload — flat, matches Go AggregationTapeV1 (PascalCase, no json tags).
MR_Tape_Payload :: struct {
	Venue:            string `json:"Venue"`,
	Instrument:       string `json:"Instrument"`,
	Timeframe:        string `json:"Timeframe"`,
	WindowStartTs:    i64    `json:"WindowStartTs"`,
	WindowEndTs:      i64    `json:"WindowEndTs"`,
	TradeCount:       i64    `json:"TradeCount"`,
	BuyCount:         i64    `json:"BuyCount"`,
	SellCount:        i64    `json:"SellCount"`,
	BuyVolume:        f64    `json:"BuyVolume"`,
	SellVolume:       f64    `json:"SellVolume"`,
	TotalVolume:      f64    `json:"TotalVolume"`,
	BuyNotional:      f64    `json:"BuyNotional"`,
	SellNotional:     f64    `json:"SellNotional"`,
	VwapPrice:        f64    `json:"VwapPrice"`,
	MaxPrice:         f64    `json:"MaxPrice"`,
	MinPrice:         f64    `json:"MinPrice"`,
	LastPrice:        f64    `json:"LastPrice"`,
	MaxTradeSize:     f64    `json:"MaxTradeSize"`,
	Rate:             f64    `json:"Rate"`,
	Imbalance:        f64    `json:"Imbalance"`,
	IsBurst:          bool   `json:"IsBurst"`,
	Seq:              i64    `json:"Seq"`,
	TsIngestMs:       i64    `json:"TsIngestMs"`,
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

MR_Microstructure_Evidence_Payload :: struct {
	kind:           string    `json:"kind"`,
	confidence:     f64       `json:"confidence"`,
	features:       []string  `json:"features"`,
	feature_values: []f64     `json:"feature_values"`,
	reason:         string    `json:"reason"`,
	ts_ingest:      i64       `json:"ts_ingest"`,
	seq:            i64       `json:"seq"`,
}

MR_Microstructure_Evidence_Feature :: struct {
	key:   string `json:"key"`,
	value: f64    `json:"value"`,
}

MR_Microstructure_Evidence_Payload_V2 :: struct {
	type_str:    string                              `json:"type"`,
	kind:        string                              `json:"kind"`,
	confidence:  f64                                 `json:"confidence"`,
	features:    []MR_Microstructure_Evidence_Feature `json:"features"`,
	explanation: string                              `json:"explanation"`,
	reason:      string                              `json:"reason"`,
	ts_server:   i64                                 `json:"ts_server"`,
	ts_ingest:   i64                                 `json:"ts_ingest"`,
	seq:         i64                                 `json:"seq"`,
}

MR_Signal_Feature :: struct {
	label: string `json:"label"`,
	value: string `json:"value"`,
}

MR_Signal_Payload :: struct {
	type_str:       string              `json:"type"`,
	kind:           string              `json:"kind"`,
	venue:          string              `json:"venue"`,
	instrument:     string              `json:"instrument"`,
	timeframe:      string              `json:"timeframe"`,
	severity:       string              `json:"severity"`,
	confidence:     f64                 `json:"confidence"`,
	evidence:       []MR_Signal_Feature `json:"evidence"`,
	explanation:    string              `json:"explanation"`,
	rule_version:   string              `json:"rule_version"`,
	regime_kind:    string              `json:"regime_kind"`,
	regime_strength: f64                `json:"regime_strength"`,
	reason:         string              `json:"reason"`,
}

MR_Signal_Feature_V2 :: struct {
	key:   string `json:"key"`,
	value: f64    `json:"value"`,
}

MR_Signal_Payload_V2 :: struct {
	type_str:       string                `json:"type"`,
	kind:           string                `json:"kind"`,
	venue:          string                `json:"venue"`,
	symbol:         string                `json:"symbol"`,
	timeframe:      string                `json:"timeframe"`,
	severity:       string                `json:"severity"`,
	confidence:     f64                   `json:"confidence"`,
	features:       []MR_Signal_Feature_V2 `json:"features"`,
	explanation:    string                `json:"explanation"`,
	rule_version:   string                `json:"rule_version"`,
	regime_kind:    string                `json:"regime_kind"`,
	regime_strength: f64                  `json:"regime_strength"`,
	reason:         string                `json:"reason"`,
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

MR_Aggregation_Snapshot_Frame :: struct {
	payload: MR_Aggregation_Snapshot `json:"payload"`,
}

MR_Stats_Frame :: struct {
	payload: MR_Stats_Payload `json:"payload"`,
}

MR_Stats_Frame_Wrapped :: struct {
	payload: MR_Stats_Wrapper `json:"payload"`,
}

MR_Tape_Frame :: struct {
	payload: MR_Tape_Payload `json:"payload"`,
}

MR_Heatmap_Frame :: struct {
	payload: MR_Heatmap `json:"payload"`,
}

MR_VPVR_Frame :: struct {
	payload: MR_VPVR `json:"payload"`,
}

MR_Microstructure_Evidence_Frame :: struct {
	payload: MR_Microstructure_Evidence_Payload `json:"payload"`,
}

MR_Microstructure_Evidence_Frame_V2 :: struct {
	payload: MR_Microstructure_Evidence_Payload_V2 `json:"payload"`,
}

MR_Signal_Frame :: struct {
	type_str:  string            `json:"type"`,
	subject:   string            `json:"subject"`,
	seq:       i64               `json:"seq"`,
	ts_server: i64               `json:"ts_server"`,
	payload:   MR_Signal_Payload `json:"payload"`,
}

MR_Signal_Frame_V2 :: struct {
	type_str:  string               `json:"type"`,
	subject:   string               `json:"subject"`,
	seq:       i64                  `json:"seq"`,
	ts_server: i64                  `json:"ts_server"`,
	payload:   MR_Signal_Payload_V2 `json:"payload"`,
}

MR_Candle_Frame :: struct {
	payload: MR_Candle_Wrapped `json:"payload"`,
}

// Flat candle frame for payloads not wrapped in {"Candle": {...}}.
MR_Candle_Frame_Flat :: struct {
	payload: MR_Candle_Payload `json:"payload"`,
}

MR_Hello_Rate_Limit :: struct {
	enabled:        bool `json:"enabled"`,
	max_per_second: int  `json:"max_per_second"`,
	burst_capacity: int  `json:"burst_capacity"`,
}

MR_Hello_Capabilities :: struct {
	topics:                          []string             `json:"topics"`,
	venues:                          []string             `json:"venues"`,
	symbols:                         []string             `json:"symbols"`,
	max_subscriptions_per_connection: int                 `json:"max_subscriptions_per_connection"`,
	max_symbols_per_connection:       int                 `json:"max_symbols_per_connection"`,
	max_frame_bytes:                  int                 `json:"max_frame_bytes"`,
	outbound_queue_size:              int                 `json:"outbound_queue_size"`,
	metrics_cadence_ms:               int                 `json:"metrics_cadence_ms"`,
	keepalive_interval_ms:            int                 `json:"keepalive_interval_ms"`,
	supported_features:               []string            `json:"supported_features"`,
	rate_limit:                       MR_Hello_Rate_Limit `json:"rate_limit"`,
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
	ws_dropped_total:                i64    `json:"ws_dropped_total"`,
	ws_queue_len:                    int    `json:"ws_queue_len"`,
	ws_lag_ms:                       i64    `json:"ws_lag_ms"`,
	publish_to_deliver_latency_ms:   i64    `json:"publish_to_deliver_latency_ms"`,
	serialize_errors_total:          i64    `json:"serialize_errors_total"`,
	resync_total:                    i64    `json:"resync_total"`,
	active_subscriptions:            int    `json:"active_subscriptions"`,
	messages_out_total:              i64    `json:"messages_out_total"`,
	backpressure_level:              int    `json:"backpressure_level"`,
	recommended_action:              string `json:"recommended_action"`,
	queue_capacity:                  int    `json:"queue_capacity"`,
	queue_high_watermark:            int    `json:"queue_high_watermark"`,
}

MR_Metrics_Frame :: struct {
	type_str: string             `json:"type"`,
	payload:  MR_Metrics_Payload `json:"payload"`,
}

// --- Error frame with problem sub-struct (Terminal_V1) ---

MR_Problem :: struct {
	code:        string `json:"code"`,
	message:     string `json:"message"`,
	error_code:  string `json:"error_code"`,
	action_hint: string `json:"action_hint"`,
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

// --- Snapshot integrity frame (Terminal_V1) ---

MR_Snapshot_Frame :: struct {
	snapshot_seq:    i64    `json:"snapshot_seq"`,
	watermark_seq:   i64    `json:"watermark_seq"`,
	snapshot_hash:   string `json:"snapshot_hash"`,
	snapshot_source: string `json:"snapshot_source"`,
}

// --- Hello ACK frame with negotiated features (Terminal_V1) ---

MR_Hello_Ack_Frame :: struct {
	negotiated_features: []string `json:"negotiated_features"`,
}

// --- Action hint enum (Terminal_V1 error guidance) ---

MR_Action_Hint :: enum u8 {
	Unspecified,
	None,
	Retry,
	Reconnect,
	Resubscribe,
	Resync,
}

parse_action_hint :: proc(s: string) -> MR_Action_Hint {
	switch s {
	case "none":        return .None
	case "retry":       return .Retry
	case "reconnect":   return .Reconnect
	case "resubscribe": return .Resubscribe
	case "resync":      return .Resync
	}
	return .Unspecified
}

// --- Parse helpers ---

parse_frame_type :: proc(s: string) -> MR_Frame_Type {
	switch s {
	case "event":     return .Event
	case "snapshot":  return .Snapshot
	case "signal":    return .Signal
	case "batch":     return .Batch
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
