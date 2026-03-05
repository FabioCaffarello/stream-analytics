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

MD_Transport_State :: enum u8 {
	Connected,    // Socket connected, protocol state not finalized yet
	Hello_Pending,
	Running,
	Desync,
	Backoff,
}

MD_WS_Error_Category :: enum u8 {
	None,
	AuthDenied,
	HandshakeFailed,
	ServerClosed,
	ProtocolError,
	Timeout,
	BackpressureDrop,
}

MD_WS_Error_Action :: enum u8 {
	None,
	Retry,
	Downgrade,
	Resync,
	Stop,
}

MD_Event_Kind :: enum u8 {
	Trade,
	Orderbook_Snapshot,
	Stats,
	Heatmap,
	VPVR,
	Candle,
	Range_Candle_Batch,
	Evidence,
	Signal,
	Tape,
}

MD_Channel :: enum u8 {
	Trades,
	Orderbook,
	Stats,
	Heatmaps,
	VPVR,
	Candles,
	Evidence,
	Signals,
	Tape,
}

MD_Desync_Reason :: enum u8 {
	None,
	Sequence_Gap,
	Snapshot_Gap,
	Protocol_Version,
	Protocol_Invalid,
	Missing_Hello,
	Resync_Required,
}

MD_Trade_Event :: struct {
	price:  f64,
	qty:    f64,
	is_buy: bool,
	unix:   i64,
}

MD_Tape_Event :: struct {
	last_price:      f64,
	total_volume:    f64,
	buy_volume:      f64,
	sell_volume:     f64,
	trade_count:     i64,
	rate_per_sec:    f64,
	imbalance:       f64,
	is_burst:        bool,
	window_start_ts: i64,
	window_end_ts:   i64,
	unix:            i64,
}

MD_Orderbook_Event :: struct {
	ask_prices: [^]f64,
	ask_sizes:  [^]f64,
	bid_prices: [^]f64,
	bid_sizes:  [^]f64,
	ask_count:  int,
	bid_count:  int,
	is_snapshot: bool,
	last_price: f64,
	unix:       i64,
}

MD_Stats_Event :: struct {
	mark_price: f64,
	funding:    f64,
	tbuy:       f64,
	tsell:      f64,
	window_ms:  i64,
	ts_ingest_ms: i64,
	quality_flags: u32,
	unix:       i64,
}

MD_Heatmap_Event :: struct {
	prices:          [^]f64,
	sizes:           [^]f64,
	level_count:     int,
	price_group:     f64,
	min_price:       f64,
	max_price:       f64,
	max_size:        f64,
	unix:            i64,
	window_start_ms: i64,
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

MD_Evidence_Event :: struct {
	kind:          [24]u8,
	kind_len:      u8,
	confidence:    f64,
	reason:        [96]u8,
	reason_len:    u8,
	feature_tags:  [4][24]u8,
	feature_vals:  [4]f64,
	feature_count: int,
	unix:          i64,
}

MD_Signal_Event :: struct {
	kind:            [24]u8,
	kind_len:        u8,
	severity:        [12]u8,
	severity_len:    u8,
	confidence:      f64,
	reason:          [96]u8,
	reason_len:      u8,
	regime:          [24]u8,
	regime_len:      u8,
	regime_strength: f64,
	unix:            i64,
}

// Keep range batches compact enough for per-frame polling, while still allowing
// a meaningful historical seed for candle UI.
RANGE_CANDLE_MAX :: 32

MD_Range_Candle_Batch :: struct {
	candles: [RANGE_CANDLE_MAX]MD_Candle_Event,
	count:   int,
	is_last: bool,
}

MD_Event_Data :: struct #raw_union {
	trade:          MD_Trade_Event,
	tape:           MD_Tape_Event,
	ob:             MD_Orderbook_Event,
	stats:          MD_Stats_Event,
	heatmap:        MD_Heatmap_Event,
	vpvr:           MD_VPVR_Event,
	candle:         MD_Candle_Event,
	range_candles:  MD_Range_Candle_Batch,
	evidence:       MD_Evidence_Event,
	signal:         MD_Signal_Event,
}

MD_Event_Source :: struct {
	subject_id: u64,
	channel:    MD_Channel,
	seq:        i64,
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
	active_subs:            int,
	trade_backlog:          int,
	trade_backlog_cap:      int,
	candle_backlog:         int,
	candle_backlog_cap:     int,
	signal_backlog:         int,
	signal_backlog_cap:     int,
	drop_count:             int,
	drop_trade_ring:        int,
	drop_candle_ring:       int,
	drop_ws_queue:          int,
	drop_payload_oversize:  int,
	reconnect_count:        int,
	latest_pending:         int,
	parse_error_count:      int,
	subscribe_ack_count:    int,
	seq_gap_count:          int,
	resync_count:           int,
	parsed_msgs_total:      u64,
	parsed_bytes_total:     u64,
	parse_arena_resets:     u64,
	alloc_estimate_total:   u64,
	msg_rate:               f64,
	bytes_rate:             f64,
	last_msg_ts_ms:         i64,
	last_server_ts_ms:      i64,
	rtt_ms:                 i64,
	lag_ms:                 i64,
	parse_time_p95_us:      i64,
	parse_time_p99_us:      i64,
	apply_time_p95_us:      i64,
	apply_time_p99_us:      i64,
	batched_decode_time_p95_us: i64,
	batched_decode_time_p99_us: i64,
	protocol_version:       int,
	hello_received:         bool,
	desync:                 bool,
	desync_reason:          MD_Desync_Reason,
	transport_state:        MD_Transport_State,
	ws_error_category:      MD_WS_Error_Category,
	ws_error_action:        MD_WS_Error_Action,
	backend_gap_no_metrics:        int,
	backend_gap_pong_timeout:      int,
	backend_gap_resync_ack_timeout: int,
	backend_gap_missing_ts_server: int,
	backend_gap_seq_gap_recurring: int,
	backend_gap_frequent_drops:    int,
	// Terminal_V1 transport fields.
	transport_mode:         u8,   // 0=Terminal_V1, 1=Legacy_JSON
	server_instance_id:     [32]u8,
	server_instance_id_len: u8,
	server_instance_id_hash: u64,
	auth_mode:              u8,   // 0=none, 1=apikey, 2=jwt
	hello_timeout_count:    int,
	pong_rtt_ms:            i64,
	// Server-pushed metrics (from METRICS frame).
	server_ws_dropped:      i64,
	server_ws_queue_len:    int,
	server_ws_lag_ms:       i64,
	server_serialize_errors: i64,
	server_resync_total:    i64,
	server_pub_deliver_ms:  i64,
	// Server capability limits (from HELLO).
	server_max_subscriptions:    int,
	server_max_frame_bytes:      int,
	server_metrics_cadence_ms:   int,
	server_keepalive_interval_ms: int,
	server_rate_limit_enabled:   bool,
	// Backpressure (from METRICS).
	server_backpressure_level:    int,
	server_queue_capacity:        int,
	server_queue_high_watermark:  int,
	server_recommended_action:    [32]u8,
	server_recommended_action_len: u8,
	// Feature negotiation.
	negotiated_feature_count:     int,
	negotiated_feature_names:     [8][24]u8,
	negotiated_feature_name_lens: [8]u8,
	batched_frames_received:      u64,
	batched_events_received:      u64,
	batched_fastpath_events:      u64,
	batched_fallback_events:      u64,
	canonical_stats_frames:       u64,
	stats_fallback_frames:        u64,
	canonical_evidence_frames:    u64,
	legacy_evidence_frames:       u64,
	evidence_fallback_frames:     u64,
	canonical_signal_frames:      u64,
	legacy_signal_frames:         u64,
	signal_fallback_frames:       u64,
	legacy_evidence_rejected:     u64,
	legacy_signal_rejected:       u64,
	// Integrity counters.
	snapshot_hash_mismatches:     int,
	snapshot_seq_violations:      int,
	prev_seq_violations:         int,
	hash_validation_skipped:     int,  // skipped byte-perfect hash verify (noncanonical)
	// Legacy tracking.
	legacy_downgrade_count:       int,
	legacy_connected_since_ms:    i64,
}

MD_Event :: struct {
	source: MD_Event_Source,
	kind: MD_Event_Kind,
	unix: i64,
	data: MD_Event_Data,
}

Marketdata_Port :: struct {
	subscribe:       proc(venue: string, symbol: string, channel: MD_Channel) -> bool,
	subscribe_tf:    proc(venue: string, symbol: string, channel: MD_Channel, tf: string) -> bool,  // subscribe with explicit TF
	unsubscribe:     proc(venue: string, symbol: string, channel: MD_Channel),
	poll:            proc(events_buf: []MD_Event) -> int,
	now_ms:          proc() -> i64,
	conn_status:     proc() -> MD_Conn_Status,
	metrics:         proc(out: ^MD_Runtime_Metrics) -> bool,
	describe_stream: proc(subject_id: u64, out: ^MD_Stream_Info) -> bool,
	set_candle_tf:   proc(tf: string),
	send_getrange:   proc(subject: string, limit: int, end_ts: i64),
	reconnect_transport: proc(ws_url: string, api_key: string, jwt_token: string = "") -> bool,
	disconnect_transport: proc() -> bool,
	shutdown:        proc(),
	fetch_markets:   proc(buf: [^]u8, cap: i32) -> i32,  // HTTP GET /api/v1/markets; returns bytes written, 0 on failure
	on_reconnect:    proc(),  // Called by app layer when reconnect detected; triggers reconcile
}
