package ports

// Marketdata port — platform-injected data source.
// Core drains events per-frame via poll() (non-blocking).
// Native: backed by WebSocket + ring buffer.
// Web:    backed by JS WebSocket bridge (future).

MD_Conn_Status :: enum u8 {
	Offline,       // No server configured / stub mode
	Connecting,    // Attempting connection
	Connected,     // Active and receiving data
	Reconnecting,  // Backoff + retry in progress
}

MD_Event_Kind :: enum u8 {
	Trade,
	Orderbook_Snapshot,
	Stats,
	Heatmap,
	VPVR,
	Candle,
}

MD_Channel :: enum u8 {
	Trades,
	Orderbook,
	Stats,
	Heatmaps,
	VPVR,
	Candles,
}

MD_Trade_Event :: struct {
	price:  f64,
	qty:    f64,
	is_buy: bool,
	unix:   i64,
}

MD_Orderbook_Event :: struct {
	ask_prices: [^]f64,
	ask_sizes:  [^]f64,
	bid_prices: [^]f64,
	bid_sizes:  [^]f64,
	ask_count:  int,
	bid_count:  int,
	last_price: f64,
	unix:       i64,
}

MD_Stats_Event :: struct {
	mark_price: f64,
	funding:    f64,
	tbuy:       i64,
	tsell:      i64,
	unix:       i64,
}

MD_Heatmap_Event :: struct {
	prices:      [^]f64,
	sizes:       [^]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	max_size:    f64,
	unix:        i64,
}

MD_VPVR_Event :: struct {
	prices:      [^]f64,
	buys:        [^]f64,
	sells:       [^]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	unix:        i64,
}

MD_Candle_Event :: struct {
	open:            f64,
	high:            f64,
	low:             f64,
	close:           f64,
	volume:          f64,
	buy_vol:         f64,
	sell_vol:        f64,
	trade_count:     i64,
	window_start_ts: i64,
	window_end_ts:   i64,
	is_closed:       bool,
}

MD_Event_Data :: struct #raw_union {
	trade:   MD_Trade_Event,
	ob:      MD_Orderbook_Event,
	stats:   MD_Stats_Event,
	heatmap: MD_Heatmap_Event,
	vpvr:    MD_VPVR_Event,
	candle:  MD_Candle_Event,
}

MD_Event_Source :: struct {
	subject_id: u64,
	channel:    MD_Channel,
}

MD_Stream_Info :: struct {
	subject_id: u64,
	channel:    MD_Channel,
	venue:      string,
	symbol:     string,
	timeframe:  string,
	subject:    string,
}

MD_Runtime_Metrics :: struct {
	active_subs:       int,
	trade_backlog:     int,
	drop_count:        int,
	reconnect_count:   int,
	latest_pending:    int,
}

MD_Event :: struct {
	source: MD_Event_Source,
	kind: MD_Event_Kind,
	unix: i64,
	data: MD_Event_Data,
}

Marketdata_Port :: struct {
	subscribe:   proc(venue: string, symbol: string, channel: MD_Channel) -> bool,
	unsubscribe: proc(venue: string, symbol: string, channel: MD_Channel),
	poll:        proc(events_buf: []MD_Event) -> int,
	now_ms:      proc() -> i64,
	conn_status: proc() -> MD_Conn_Status,
	metrics:     proc(out: ^MD_Runtime_Metrics) -> bool,
	describe_stream: proc(subject_id: u64, out: ^MD_Stream_Info) -> bool,
	shutdown:    proc(),
}
