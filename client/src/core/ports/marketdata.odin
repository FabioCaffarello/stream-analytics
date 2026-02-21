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
}

MD_Channel :: enum u8 {
	Trades,
	Orderbook,
	Stats,
	Heatmaps,
	VPVR,
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

MD_Event_Data :: struct #raw_union {
	trade:   MD_Trade_Event,
	ob:      MD_Orderbook_Event,
	stats:   MD_Stats_Event,
	heatmap: MD_Heatmap_Event,
	vpvr:    MD_VPVR_Event,
}

MD_Event :: struct {
	kind: MD_Event_Kind,
	unix: i64,
	data: MD_Event_Data,
}

Marketdata_Port :: struct {
	subscribe:   proc(symbol: string, channel: MD_Channel) -> bool,
	unsubscribe: proc(symbol: string, channel: MD_Channel),
	poll:        proc(events_buf: []MD_Event) -> int,
	now_ms:      proc() -> i64,
	conn_status: proc() -> MD_Conn_Status,
}
