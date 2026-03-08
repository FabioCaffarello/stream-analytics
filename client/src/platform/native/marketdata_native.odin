package main

// Native marketdata port — WebSocket client with background reader thread.
// Ring buffer for trades, single-slot latest-wins for orderbook/stats/heatmap/vpvr.
// Automatic reconnection with exponential backoff + re-subscribe on reconnect.
//
// Threading model:
// - Background reader thread: writes to ring buffers + staging under mutex.
// - Main thread: reads via poll() under same mutex.
// - SPSC guarantee: exactly 1 producer (reader), exactly 1 consumer (main poll).
// - dirty flags: set by writer, read+cleared by reader, both under mutex.
// - ws_url / api_key: heap strings mutated by main thread (reconnect_transport),
//   copied to stack buffers by reader thread (attempt_reconnect) under lock.

import "core:fmt"
import "core:net"
import "core:strconv"
import "core:strings"
import "core:sync"
import "core:thread"
import "core:time"
import "core:bytes"
import "core:compress/zlib"
import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:util"

TRADE_RING_CAP  :: 1024
CANDLE_RING_CAP :: 8
SIGNAL_RING_CAP :: 64
MAX_SUBS :: 128

// --- Reconnection constants ---

BACKOFF_INITIAL_MS :: 500
BACKOFF_MAX_MS     :: 30_000
BACKOFF_MULTIPLIER :: 2
HELLO_TIMEOUT_MS   :: 10_000
PING_INTERVAL_MS      :: 20_000
PONG_TIMEOUT_MS       :: 15_000
METRICS_STALE_MS      :: 20_000
FREQUENT_DROP_THRESHOLD :: 16
PERF_SAMPLE_CAP       :: 120

// Default candle timeframe filter.
CANDLE_TF_DEFAULT :: "1m"

// Subscription/metrics limit helpers delegated to md_common.
// (client_effective_sub_limit, client_can_add_subscription, client_metrics_stale_timeout_ms
//  removed — use md_common.effective_sub_limit / can_add_subscription / metrics_stale_timeout_ms)

// --- Internal state (package-level singleton) ---

Conn_State :: enum u8 {
	Disconnected,
	Connecting,
	Connected,
	Backoff_Wait,
}

Sub_Entry :: struct {
	subject_id:     u64,
	venue:          string,
	symbol:         string,
	channel:        ports.MD_Channel,
	subject:        string,
	stream_id:      [128]u8,
	stream_id_len:  u8,
	is_explicit_tf: bool,
}

MD_Native_State :: struct {
	// Trade ring buffer (SPSC: writer=background, reader=main).
	trade_ring:            [TRADE_RING_CAP]ports.MD_Trade_Event,
	trade_ring_subject_id: [TRADE_RING_CAP]u64,
	trade_ring_seq:        [TRADE_RING_CAP]i64,
	trade_write:           int,
	trade_count:           int,

	// Orderbook snapshot (latest-wins, single-slot).
	ob_staging: services.Parsed_OB,
	ob_dirty:   bool,

	// Stats staging (latest-wins).
	stats_staging: services.Parsed_Stats,
	stats_dirty:   bool,

	// Tape staging (latest-wins).
	tape_staging: services.Parsed_Tape,
	tape_dirty:   bool,

	// Heatmap staging (latest-wins).
	heatmap_staging: services.Parsed_Heatmap,
	heatmap_dirty:   bool,

	// VPVR staging (latest-wins).
	vpvr_staging: services.Parsed_VPVR,
	vpvr_dirty:   bool,

	// Candle ring buffer (SPSC: writer=background, reader=main).
	candle_ring:       [CANDLE_RING_CAP]services.Parsed_Candle,
	candle_ring_write: int,
	candle_ring_count: int,

	// Range candle staging (getrange response batch).
	range_candle_staging: services.Parsed_Range_Candles,
	range_candle_dirty:   bool,
	evidence_staging:     services.Parsed_Evidence,
	evidence_dirty:       bool,
	signal_ring:          [SIGNAL_RING_CAP]services.Parsed_Signal,
	signal_ring_write:    int,
	signal_ring_count:    int,

	// S47: Analytics staging (latest-wins per kind).
	oi_staging:            services.Parsed_Open_Interest,
	oi_dirty:              bool,
	delta_vol_staging:     services.Parsed_Delta_Volume,
	delta_vol_dirty:       bool,
	cvd_staging:           services.Parsed_CVD,
	cvd_dirty:             bool,
	bar_stats_staging:     services.Parsed_Bar_Stats,
	bar_stats_dirty:       bool,

	// Candle timeframe filter (mutable, heap-allocated).
	candle_tf_filter: string,

	// Connection.
	conn:        WS_Connection,
	conn_state:  Conn_State,
	ws_url:      string,
	api_key:     string,
	jwt_token:   string,
	should_stop: bool,
	reconnect_blocked: bool,
	// Terminal_V1 transport state.
	transport_state: ports.MD_Transport_State,
	transport_mode: util.Transport_Mode,
	auth_mode:      u8, // 0=none, 1=apikey, 2=jwt
	allow_legacy_ws: bool,
	ws_error_category: ports.MD_WS_Error_Category,
	ws_error_action:   ports.MD_WS_Error_Action,
	hello_timeout_count: int,
	pong_rtt_ms: i64,
	// Server-pushed metrics (from METRICS frame).
	server_metrics: services.Parsed_Metrics,
	server_metrics_received: bool,
	// Server capabilities (from HELLO) — consolidated struct.
	caps: md_common.Server_Capabilities,
	// Negotiated features (from Hello ACK).
	negotiated_features: [services.MAX_FEATURE_SLOTS]services.Parsed_Feature_Slot,
	negotiated_feature_count: int,
	// Integrity counters.
	snapshot_hash_mismatches: int,
	snapshot_seq_violations:  int,
	prev_seq_violations:     int,
	hash_validation_skipped: int,  // skipped hash byte-verify (noncanonical)
	// Legacy tracking.
	legacy_downgrade_count:    int,
	legacy_connected_since_ms: i64,
	reader_thread: ^thread.Thread,
	mu:          sync.Mutex,
	drop_count:  int,
	drop_trade_ring: int,
	drop_candle_ring: int,
	drop_ws_queue: int,
	drop_payload_oversize: int,
	seq_gap_count: int,
	resync_count: int,
	seq_gap_streak: int,
	last_metrics_ts_ms: i64,
	last_pong_ts_ms: i64,
	backend_gap_no_metrics: int,
	backend_gap_pong_timeout: int,
	backend_gap_resync_ack_timeout: int,
	backend_gap_missing_ts_server: int,
	backend_gap_seq_gap_recurring: int,
	backend_gap_frequent_drops: int,
	last_server_ts_by_sub: [MAX_SUBS]i64,
	parse_samples_us: [PERF_SAMPLE_CAP]i64,
	parse_sample_head: int,
	parse_sample_count: int,
	apply_samples_us: [PERF_SAMPLE_CAP]i64,
	apply_sample_head: int,
	apply_sample_count: int,
	batch_decode_samples_us: [PERF_SAMPLE_CAP]i64,
	batch_decode_sample_head: int,
	batch_decode_sample_count: int,
	batched_frames_received: u64,
	batched_events_received: u64,

	// Reconnection.
	backoff_ms:      int,
	reconnect_count: int, // cumulative reconnect attempts (monotonic)
	reconnect_streak: int, // current consecutive reconnect attempts
	jitter_seed:     u32,
	parse_arena: services.Parse_Arena,
	parse_error_count: int,
	subscribe_ack_count: int,
	rates: md_common.Rate_State,
	last_msg_ts_ms:     i64,
	last_server_ts_ms:  i64,
	last_rtt_ms:        i64,
	last_lag_ms:        i64,
	protocol_version:   int,
	hello_received:     bool,
	hello_valid:        bool,
	desync:             bool,
	desync_reason:      ports.MD_Desync_Reason,
	connect_started_ms: i64,
	first_data_logged:  bool,
	last_ping_sent_ms:  i64,

	// Subscription tracking for reconnect re-subscribe.
	active_subs:  [MAX_SUBS]Sub_Entry,
	active_count: int,
	last_seq_by_sub: [MAX_SUBS]i64,
	last_snapshot_seq_by_sub: [MAX_SUBS]i64,
	snapshot_logged_by_sub: [MAX_SUBS]bool,

	// Desync recovery: targeted re-subscribe for a single subject.
	desync_resub_subject_id: u64, // 0 = none pending
	// RESYNC tracking (Terminal_V1): pending resync for a subject.
	resync_pending_subject_id: u64,  // 0 = none pending
	resync_sent_ms: i64,             // timestamp when RESYNC was sent

	// Request ID counter.
	rid_counter: u32,

	// Temp arrays for poll() — main thread only.
	poll_ask_prices:    [services.OB_STAGING_DEPTH]f64,
	poll_ask_sizes:     [services.OB_STAGING_DEPTH]f64,
	poll_bid_prices:    [services.OB_STAGING_DEPTH]f64,
	poll_bid_sizes:     [services.OB_STAGING_DEPTH]f64,
	poll_hm_prices:     [services.HEATMAP_STAGING_CAP]f64,
	poll_hm_sizes:      [services.HEATMAP_STAGING_CAP]f64,
	poll_vpvr_prices:   [services.VPVR_STAGING_CAP]f64,
	poll_vpvr_buys:     [services.VPVR_STAGING_CAP]f64,
	poll_vpvr_sells:    [services.VPVR_STAGING_CAP]f64,
}

// File-private singleton: Odin procs are bare function pointers (no closures),
// so Marketdata_Port callbacks access state through this global. Only one
// instance exists per process; @(private="file") prevents external access.
@(private = "file")
g_md_state: ^MD_Native_State

@(private = "file")
read_allow_legacy_ws_native :: proc() -> bool {
	// Sprint S9 hard cutover: native runtime no longer supports legacy WS downgrade.
	return false
}

@(private = "file")
set_transport_state :: proc(state: ^MD_Native_State, next: ports.MD_Transport_State) {
	if state == nil do return
	state.transport_state = next
}

@(private = "file")
record_perf_sample :: proc(samples: ^[PERF_SAMPLE_CAP]i64, head: ^int, count: ^int, v: i64) {
	if samples == nil || head == nil || count == nil do return
	samples[head^] = max(v, 0)
	head^ = (head^ + 1) % PERF_SAMPLE_CAP
	if count^ < PERF_SAMPLE_CAP do count^ += 1
}

@(private = "file")
sample_percentile_us :: proc(samples: [PERF_SAMPLE_CAP]i64, head: int, count: int, pct: int) -> i64 {
	return services.ring_percentile_i64(samples, head, count, pct)
}

@(private = "file")
classify_ws_error :: proc(err: WS_Error) -> ports.MD_WS_Error_Category {
	switch err {
	case .Read_Conn_Closed:
		return .ServerClosed
	case .Payload_Too_Large:
		return .BackpressureDrop
	case .DNS_Error, .Handshake_Error, .Dial_Error, .TLS_Not_Supported:
		return .HandshakeFailed
	case .Failed_Header_Read, .Failed_Payload_Read_16, .Failed_Payload_Read_64, .Failed_Payload_Read, .Failed_Mask_Read:
		return .Timeout
	case .Invalid_Frame_Sequence, .Invalid_Control_Frame, .Invalid_Url, .Invalid_Host, .Invalid_Port:
		return .ProtocolError
	case .Send_Error:
		return .ServerClosed
	case .None:
	}
	return .ProtocolError
}

@(private = "file")
apply_ws_fault :: proc(state: ^MD_Native_State, category: ports.MD_WS_Error_Category) {
	if state == nil do return
	action := md_common.ws_fault_action(category)
	state.ws_error_category = category
	state.ws_error_action = action
	switch action {
	case .Retry:
		// Keep reconnect path active.
		set_transport_state(state, .Backoff)
	case .Resync:
		state.desync = true
		if state.desync_reason == .None do state.desync_reason = .Sequence_Gap
		set_transport_state(state, .Desync)
	case .Stop:
		state.desync = true
		state.desync_reason = .Protocol_Invalid
		state.reconnect_blocked = true
		set_transport_state(state, .Desync)
	case .Downgrade:
		native_record_legacy_downgrade_attempt(state, "fault_matrix")
	case .None:
	}
}

@(private = "file")
native_record_legacy_downgrade_attempt :: proc(state: ^MD_Native_State, source: string) {
	if state == nil do return
	state.legacy_downgrade_count += 1
	state.desync = true
	state.desync_reason = .Protocol_Invalid
	state.reconnect_blocked = true
	set_transport_state(state, .Desync)
	fmt.printf(
		"[md-lifecycle] legacy_downgrade_blocked source=%s count=%d\n",
		source,
		state.legacy_downgrade_count,
	)
}

// --- Auth header helper ---
// Builds "Authorization: Bearer <jwt>\r\n" or "X-API-Key: <key>\r\n" into buf.
// Returns (header_string, auth_mode) where auth_mode: 0=none, 1=apikey, 2=jwt.
@(private = "file")
build_auth_header :: proc(buf: []u8, api_key: string, jwt_token: string) -> (string, u8) {
	if len(jwt_token) > 0 {
		return fmt.bprintf(buf[:], "Authorization: Bearer %s\r\n", jwt_token), 2
	}
	if len(api_key) > 0 {
		return fmt.bprintf(buf[:], "X-API-Key: %s\r\n", api_key), 1
	}
	return "", 0
}

@(private = "file")
log_safe_url :: proc(url: string) -> string {
	return md_common.sanitize_url_for_log(url)
}

// Feature lookup — delegates to md_common.feature_slot_has_name.
@(private = "file")
server_has_feature :: proc(state: ^MD_Native_State, name: string) -> bool {
	return md_common.feature_slot_has_name(state.caps.supported_features, state.caps.supported_feature_count, name)
}

@(private = "file")
negotiated_has_feature :: proc(state: ^MD_Native_State, name: string) -> bool {
	return md_common.feature_slot_has_name(state.negotiated_features, state.negotiated_feature_count, name)
}

@(private = "file")
feature_setting_value :: proc(key: string) -> string {
	if v, ok := native_settings_lookup(key); ok && len(v) > 0 {
		return v
	}
	return "auto"
}

@(private = "file")
native_supports_decompress :: proc() -> bool {
	// Native build includes core zlib support.
	return true
}

@(private = "file")
frame_within_limit :: proc(state: ^MD_Native_State, frame_len: int) -> bool {
	if state == nil do return false
	if frame_len < 0 do return false
	if md_common.frame_exceeds_limit(state.caps.max_frame_bytes, frame_len) {
		state.drop_payload_oversize += 1
		state.drop_count += 1
		fmt.printf("[md-lifecycle] frame_rejected max_frame_bytes=%d len=%d\n", state.caps.max_frame_bytes, frame_len)
		apply_ws_fault(state, .BackpressureDrop)
		return false
	}
	return true
}

// Send HELLO frame on initial connect (Terminal_V1 handshake).
// Uses build_hello_msg_v2 with requested_features when available,
// falls back to build_hello_msg for zero-feature case.
@(private = "file")
send_hello :: proc(state: ^MD_Native_State) {
	state.rid_counter += 1
	buf: [512]u8
	features: [md_common.MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [md_common.MAX_REQUESTED_FEATURES]u8
	server_known := state.caps.supported_feature_count > 0
	fc := md_common.resolve_requested_features(
		state.transport_mode,
		.Native,
		server_known,
		server_has_feature(state, "batching"),
		server_has_feature(state, "snapshot_hash"),
		server_has_feature(state, "prev_seq"),
		feature_setting_value(services.SETTING_FEATURE_BATCHING),
		feature_setting_value(services.SETTING_FEATURE_SNAPSHOT_HASH),
		feature_setting_value(services.SETTING_FEATURE_PREV_SEQ),
		&features, &feature_lens,
		server_has_feature(state, "compress"),
		feature_setting_value(services.SETTING_FEATURE_COMPRESS),
		native_supports_decompress(),
	)
	msg, ok := md_common.build_hello_msg_v2(buf[:], state.rid_counter, &features, &feature_lens, fc)
	if !ok {
		fmt.printf("[md-lifecycle] WARN hello_v2_buffer_overflow features=%d falling_back_to_basic\n", fc)
		msg, ok = md_common.build_hello_msg(buf[:], state.rid_counter)
	}
	if !ok do return
	if !frame_within_limit(state, len(msg)) do return
	err := ws_write_text(state.conn, msg)
	if err == nil {
		fmt.printf("[md-lifecycle] hello_sent rid=h%d features=%d\n", state.rid_counter, fc)
	}
}

// Send MR protocol PING with ts_client for RTT measurement.
@(private = "file")
send_ping :: proc(state: ^MD_Native_State) {
	state.rid_counter += 1
	ts := time.now()._nsec / 1_000_000
	buf: [256]u8
	msg, ok := md_common.build_ping_msg(buf[:], ts, state.rid_counter)
	if !ok do return
	if !frame_within_limit(state, len(msg)) do return
	err := ws_write_text(state.conn, msg)
	if err == nil {
		state.last_ping_sent_ms = ts
	}
}

// --- Public API ---

make_marketdata_native :: proc(url: string, api_key: string = "") -> ports.Marketdata_Port {
	if g_md_state != nil {
		native_shutdown()
	}
	state := new(MD_Native_State)
	state.ws_url = strings.clone(url)
	state.api_key = strings.clone(api_key)
	state.backoff_ms = BACKOFF_INITIAL_MS
	state.jitter_seed = u32(time.now()._nsec & 0xFFFFFFFF)
	state.candle_tf_filter = strings.clone(CANDLE_TF_DEFAULT)
	state.transport_mode = .Terminal_V1  // Assume Terminal_V1 until hello timeout.
	state.allow_legacy_ws = read_allow_legacy_ws_native()
	set_transport_state(state, .Backoff)
	g_md_state = state

	// Build auth header string (stack buffer — no temp allocator).
	hdr_buf: [384]u8
	extra_hdr, auth_mode := build_auth_header(hdr_buf[:], api_key, "")
	state.auth_mode = auth_mode

	// Attempt initial connection.
	state.conn_state = .Connecting
	conn, err := ws_dial(url, extra_hdr)
	if err != nil {
		fmt.printf("[marketdata] WS connect failed (err=%v), will retry in background\n", err)
		state.conn_state = .Disconnected
		apply_ws_fault(state, classify_ws_error(err))
	} else {
		safe_url := log_safe_url(url)
		fmt.printf("[marketdata] Connected to %s\n", safe_url)
		fmt.printf("[md-lifecycle] connect url=%s\n", safe_url)
		state.conn = conn
		state.conn_state = .Connected
		state.backoff_ms = BACKOFF_INITIAL_MS
		state.desync = false
		state.desync_reason = .None
		state.protocol_version = 0
		state.hello_received = false
		state.hello_valid = false
		state.last_metrics_ts_ms = 0
		state.last_pong_ts_ms = 0
		state.connect_started_ms = time.now()._nsec / 1_000_000
		state.first_data_logged = false
		set_transport_state(state, .Hello_Pending)
		// Terminal_V1: send HELLO immediately.
		send_hello(state)
	}

	// Start background reader thread (handles reconnection).
	t := thread.create(reader_thread_proc)
	if t != nil {
		t.data = rawptr(state)
		state.reader_thread = t
		thread.start(t)
	}

	return ports.Marketdata_Port{
		subscribe       = native_subscribe,
		subscribe_tf    = native_subscribe_tf,
		unsubscribe     = native_unsubscribe,
		poll            = native_poll,
		now_ms          = native_now_ms,
		conn_status     = native_conn_status,
		metrics         = native_metrics,
		describe_stream = native_describe_stream,
		set_candle_tf   = native_set_candle_tf,
		send_getrange   = native_send_getrange,
		reconnect_transport = native_reconnect_transport,
		disconnect_transport = native_disconnect_transport,
		shutdown        = native_shutdown,
		fetch_markets   = native_fetch_markets,
		fetch_session   = native_fetch_session,
		fetch_freshness = native_fetch_freshness,
		fetch_timeline  = native_fetch_timeline,
		fetch_instrument_overview = native_fetch_instrument_overview,
		fetch_session_dashboard  = native_fetch_session_dashboard,
		fetch_analytics_cvd          = native_fetch_analytics_cvd,
		fetch_analytics_delta_volume = native_fetch_analytics_delta_volume,
		fetch_analytics_bar_stats    = native_fetch_analytics_bar_stats,
		fetch_analytics_oi           = native_fetch_analytics_oi,
		fetch_session_volume_profile = native_fetch_session_volume_profile,
	}
}

// --- Port implementation ---

@(private = "file")
find_sub_by_subject :: proc(state: ^MD_Native_State, subject: string) -> int {
	return md_common.find_sub_by_subject(state.active_subs[:state.active_count], subject)
}

@(private = "file")
find_sub_by_key :: proc(state: ^MD_Native_State, venue: string, symbol: string, channel: ports.MD_Channel) -> int {
	return md_common.find_sub_by_key(state.active_subs[:state.active_count], venue, symbol, channel)
}

@(private = "file")
find_sub_by_subject_id :: proc(state: ^MD_Native_State, subject_id: u64) -> int {
	return md_common.find_sub_by_subject_id(state.active_subs[:state.active_count], subject_id)
}

@(private = "file")
native_subject_for_channel :: proc(state: ^MD_Native_State, venue: string, symbol: string, channel: ports.MD_Channel) -> string {
	return md_common.subject_for_channel(venue, symbol, state.candle_tf_filter, channel)
}

@(private = "file")
native_free_sub_entry :: proc(entry: ^Sub_Entry) {
	md_common.free_sub_entry(entry)
}

@(private = "file")
native_should_stop :: proc(state: ^MD_Native_State) -> bool {
	if state == nil do return true
	sync.lock(&state.mu)
	stop := state.should_stop
	sync.unlock(&state.mu)
	return stop
}

@(private = "file")
native_shutdown :: proc() {
	state := g_md_state
	if state == nil do return

	reader: ^thread.Thread
	sync.lock(&state.mu)
	state.should_stop = true
	reader = state.reader_thread
	state.reader_thread = nil
	for i in 0 ..< state.active_count {
		native_free_sub_entry(&state.active_subs[i])
		state.last_seq_by_sub[i] = 0
		state.last_snapshot_seq_by_sub[i] = 0
		state.last_server_ts_by_sub[i] = 0
	}
	state.active_count = 0
	if len(state.candle_tf_filter) > 0 {
		delete(state.candle_tf_filter)
		state.candle_tf_filter = ""
	}
	if len(state.ws_url) > 0 {
		delete(state.ws_url)
		state.ws_url = ""
	}
	if len(state.api_key) > 0 {
		delete(state.api_key)
		state.api_key = ""
	}
	if len(state.jwt_token) > 0 {
		delete(state.jwt_token)
		state.jwt_token = ""
	}
	state.conn_state = .Disconnected
	ws_close(&state.conn)
	sync.unlock(&state.mu)

	if reader != nil {
		thread.join(reader)
		thread.destroy(reader)
	}
	g_md_state = nil
}

@(private = "file")
native_disconnect_transport :: proc() -> bool {
	state := g_md_state
	if state == nil do return false
	fmt.println("[md-lifecycle] disconnect requested=manual")
	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)
	ws_close(&state.conn)
	state.conn_state = .Disconnected
	set_transport_state(state, .Backoff)
	state.desync = false
	state.desync_reason = .None
	state.protocol_version = 0
	state.hello_received = false
	state.hello_valid = false
	state.last_metrics_ts_ms = 0
	state.last_pong_ts_ms = 0
	state.connect_started_ms = 0
	state.first_data_logged = false
	return true
}

@(private = "file")
native_reconnect_transport :: proc(ws_url: string, api_key: string, jwt_token: string = "") -> bool {
	state := g_md_state
	if state == nil do return false
	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)
	if len(ws_url) > 0 && ws_url != state.ws_url {
		if len(state.ws_url) > 0 do delete(state.ws_url)
		state.ws_url = strings.clone(ws_url)
	}
	if api_key != state.api_key {
		if len(state.api_key) > 0 do delete(state.api_key)
		state.api_key = strings.clone(api_key)
	}
	if jwt_token != state.jwt_token {
		if len(state.jwt_token) > 0 do delete(state.jwt_token)
		state.jwt_token = strings.clone(jwt_token)
	}
	// Derive auth mode from new credentials.
	if len(jwt_token) > 0 {
		state.auth_mode = 2
	} else if len(api_key) > 0 {
		state.auth_mode = 1
	} else {
		state.auth_mode = 0
	}
	ws_close(&state.conn)
	state.conn_state = .Disconnected
	state.backoff_ms = BACKOFF_INITIAL_MS
	state.reconnect_blocked = false
	state.allow_legacy_ws = read_allow_legacy_ws_native()
	state.ws_error_category = .None
	state.ws_error_action = .None
	state.desync = false
	state.desync_reason = .None
	state.protocol_version = 0
	state.hello_received = false
	state.hello_valid = false
	state.transport_mode = .Terminal_V1
	set_transport_state(state, .Backoff)
	state.last_metrics_ts_ms = 0
	state.last_pong_ts_ms = 0
	state.connect_started_ms = 0
	state.first_data_logged = false
	state.server_metrics_received = false
	fmt.printf("[md-lifecycle] reconnect_requested url=%s\n", log_safe_url(state.ws_url))
	return true
}

@(private = "file")
native_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_md_state
	if state == nil do return false

	subject := native_subject_for_channel(state, venue, symbol, channel)
	if len(subject) == 0 do return false
	subject_id := util.subject_id64(subject)

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	sub_limit := md_common.effective_sub_limit(state.caps.max_subscriptions, MAX_SUBS)
	if !md_common.can_add_subscription(state.active_count, state.caps.max_subscriptions, MAX_SUBS) {
		fmt.printf("[md-lifecycle] subscribe_rejected limit=%d active=%d\n", sub_limit, state.active_count)
		delete(subject)
		return false
	}

	// Track subscription for reconnect re-subscribe.
	state.active_subs[state.active_count] = Sub_Entry{
		subject_id = subject_id,
		venue   = strings.clone(venue),
		symbol  = strings.clone(symbol),
		channel = channel,
		subject = subject,
	}
	state.last_seq_by_sub[state.active_count] = 0
	state.last_snapshot_seq_by_sub[state.active_count] = 0
	state.last_server_ts_by_sub[state.active_count] = 0
	state.snapshot_logged_by_sub[state.active_count] = false
	state.active_count += 1

	if state.conn_state != .Connected do return false // tracked; reconnect path will subscribe later
	return send_subscribe(state, subject)
}

// Subscribe with an explicit timeframe (for per-cell TF support).
@(private = "file")
native_subscribe_tf :: proc(venue: string, symbol: string, channel: ports.MD_Channel, tf: string) -> bool {
	state := g_md_state
	if state == nil do return false

	subject := util.build_subject_with_timeframe(venue, symbol, channel, tf)
	if len(subject) == 0 do return false
	subject_id := util.subject_id64(subject)

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	sub_limit := md_common.effective_sub_limit(state.caps.max_subscriptions, MAX_SUBS)
	if !md_common.can_add_subscription(state.active_count, state.caps.max_subscriptions, MAX_SUBS) {
		fmt.printf("[md-lifecycle] subscribe_tf_rejected limit=%d active=%d\n", sub_limit, state.active_count)
		delete(subject)
		return false
	}

	state.active_subs[state.active_count] = Sub_Entry{
		subject_id = subject_id,
		venue   = strings.clone(venue),
		symbol  = strings.clone(symbol),
		channel = channel,
		subject = subject,
	}
	state.last_seq_by_sub[state.active_count] = 0
	state.last_snapshot_seq_by_sub[state.active_count] = 0
	state.last_server_ts_by_sub[state.active_count] = 0
	state.snapshot_logged_by_sub[state.active_count] = false
	state.active_count += 1

	if state.conn_state != .Connected do return false // tracked; reconnect path will subscribe later
	return send_subscribe(state, subject)
}

@(private = "file")
native_describe_stream :: proc(subject_id: u64, out: ^ports.MD_Stream_Info) -> bool {
	state := g_md_state
	if state == nil || out == nil do return false
	if subject_id == 0 do return false

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	idx := find_sub_by_subject_id(state, subject_id)
	if idx < 0 do return false
	sub := state.active_subs[idx]
	out^ = ports.MD_Stream_Info{
		subject_id = sub.subject_id,
		channel    = sub.channel,
		venue      = sub.venue,
		symbol     = sub.symbol,
		timeframe  = util.subject_timeframe(sub.subject),
		subject    = sub.subject,
	}
	return true
}

@(private = "file")
native_metrics :: proc(out: ^ports.MD_Runtime_Metrics) -> bool {
	state := g_md_state
	if state == nil || out == nil do return false

	sync.lock(&state.mu)
	latest_pending := 0
	if state.ob_dirty do latest_pending += 1
	if state.stats_dirty do latest_pending += 1
	if state.tape_dirty do latest_pending += 1
	if state.heatmap_dirty do latest_pending += 1
	if state.vpvr_dirty do latest_pending += 1
	if state.candle_ring_count > 0 do latest_pending += 1
	if state.signal_ring_count > 0 do latest_pending += 1
	// S98: Analytics pending.
	if state.oi_dirty do latest_pending += 1
	if state.delta_vol_dirty do latest_pending += 1
	if state.cvd_dirty do latest_pending += 1
	if state.bar_stats_dirty do latest_pending += 1
	sm := state.server_metrics
	out^ = ports.MD_Runtime_Metrics{
		active_subs       = state.active_count,
		trade_backlog     = state.trade_count,
		trade_backlog_cap = TRADE_RING_CAP,
		candle_backlog    = state.candle_ring_count,
		candle_backlog_cap = CANDLE_RING_CAP,
		signal_backlog    = state.signal_ring_count,
		signal_backlog_cap = SIGNAL_RING_CAP,
		drop_count        = state.drop_count,
		drop_trade_ring   = state.drop_trade_ring,
		drop_candle_ring  = state.drop_candle_ring,
		drop_ws_queue     = state.drop_ws_queue,
		drop_payload_oversize = state.drop_payload_oversize,
		reconnect_count   = state.reconnect_count,
		latest_pending    = latest_pending,
		parse_error_count = state.parse_error_count,
		subscribe_ack_count = state.subscribe_ack_count,
		seq_gap_count     = state.seq_gap_count,
		resync_count      = state.resync_count,
		parsed_msgs_total = state.rates.parsed_msgs_total,
		parsed_bytes_total = state.rates.parsed_bytes_total,
		parse_arena_resets = state.parse_arena.message_resets,
		alloc_estimate_total = state.parse_arena.message_resets,
		msg_rate          = state.rates.msg_rate,
		bytes_rate        = state.rates.bytes_rate,
		last_msg_ts_ms   = state.last_msg_ts_ms,
		last_server_ts_ms = state.last_server_ts_ms,
		rtt_ms           = state.last_rtt_ms,
		lag_ms           = state.last_lag_ms,
		parse_time_p95_us = sample_percentile_us(state.parse_samples_us, state.parse_sample_head, state.parse_sample_count, 95),
		parse_time_p99_us = sample_percentile_us(state.parse_samples_us, state.parse_sample_head, state.parse_sample_count, 99),
		apply_time_p95_us = sample_percentile_us(state.apply_samples_us, state.apply_sample_head, state.apply_sample_count, 95),
		apply_time_p99_us = sample_percentile_us(state.apply_samples_us, state.apply_sample_head, state.apply_sample_count, 99),
		batched_decode_time_p95_us = sample_percentile_us(state.batch_decode_samples_us, state.batch_decode_sample_head, state.batch_decode_sample_count, 95),
		batched_decode_time_p99_us = sample_percentile_us(state.batch_decode_samples_us, state.batch_decode_sample_head, state.batch_decode_sample_count, 99),
		protocol_version = state.protocol_version,
		hello_received   = state.hello_received,
		desync           = state.desync,
		desync_reason    = state.desync_reason,
		transport_state  = state.transport_state,
		ws_error_category = state.ws_error_category,
		ws_error_action   = state.ws_error_action,
		backend_gap_no_metrics = state.backend_gap_no_metrics,
		backend_gap_pong_timeout = state.backend_gap_pong_timeout,
		backend_gap_resync_ack_timeout = state.backend_gap_resync_ack_timeout,
		backend_gap_missing_ts_server = state.backend_gap_missing_ts_server,
		backend_gap_seq_gap_recurring = state.backend_gap_seq_gap_recurring,
		backend_gap_frequent_drops = state.backend_gap_frequent_drops,
		// Terminal_V1 transport fields.
		transport_mode          = u8(state.transport_mode),
		server_instance_id      = state.caps.server_instance_id,
		server_instance_id_len  = state.caps.server_instance_id_len,
		server_instance_id_hash = util.subject_id64(string(state.caps.server_instance_id[:int(state.caps.server_instance_id_len)])),
		auth_mode               = state.auth_mode,
		hello_timeout_count     = state.hello_timeout_count,
		pong_rtt_ms             = state.pong_rtt_ms,
		// Server-pushed metrics.
		server_ws_dropped       = sm.ws_dropped_total,
		server_ws_queue_len     = sm.ws_queue_len,
		server_ws_lag_ms        = sm.ws_lag_ms,
		server_serialize_errors = sm.serialize_errors_total,
		server_resync_total     = sm.resync_total,
		server_pub_deliver_ms   = sm.publish_to_deliver_latency_ms,
		// Server capability limits.
		server_max_subscriptions     = state.caps.max_subscriptions,
		server_max_frame_bytes       = state.caps.max_frame_bytes,
		server_metrics_cadence_ms    = state.caps.metrics_cadence_ms,
		server_keepalive_interval_ms = state.caps.keepalive_interval_ms,
		server_rate_limit_enabled    = state.caps.rate_limit_enabled,
		// Backpressure.
		server_backpressure_level    = sm.backpressure_level,
		server_queue_capacity        = sm.queue_capacity,
		server_queue_high_watermark  = sm.queue_high_watermark,
		// Feature negotiation.
		negotiated_feature_count     = state.negotiated_feature_count,
		batched_frames_received      = state.batched_frames_received,
		batched_events_received      = state.batched_events_received,
		// Integrity counters.
		snapshot_hash_mismatches     = state.snapshot_hash_mismatches,
		snapshot_seq_violations      = state.snapshot_seq_violations,
		prev_seq_violations          = state.prev_seq_violations,
		hash_validation_skipped      = state.hash_validation_skipped,
	}
	// Copy recommended_action from server metrics.
	ra_n := min(int(sm.recommended_action_len), len(out.server_recommended_action))
	for i in 0 ..< ra_n {
		out.server_recommended_action[i] = sm.recommended_action_buf[i]
	}
	out.server_recommended_action_len = u8(ra_n)
	// Copy negotiated feature names for diagnostics display.
	nfc := min(state.negotiated_feature_count, len(out.negotiated_feature_names))
	for i in 0 ..< nfc {
		out.negotiated_feature_names[i] = state.negotiated_features[i].name
		out.negotiated_feature_name_lens[i] = state.negotiated_features[i].len
	}
	sync.unlock(&state.mu)
	return true
}

@(private = "file")
native_unsubscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) {
	state := g_md_state
	if state == nil do return

	subject := ""

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	if idx := find_sub_by_key(state, venue, symbol, channel); idx >= 0 {
		subject = strings.clone(state.active_subs[idx].subject)
	} else {
		subject = native_subject_for_channel(state, venue, symbol, channel)
		if len(subject) == 0 do return
	}
	defer delete(subject)

	// Remove from active subs.
	if idx := find_sub_by_key(state, venue, symbol, channel); idx >= 0 {
		last := state.active_count - 1
		native_free_sub_entry(&state.active_subs[idx])
		if idx != last {
			state.active_subs[idx] = state.active_subs[last]
			state.last_seq_by_sub[idx] = state.last_seq_by_sub[last]
			state.last_snapshot_seq_by_sub[idx] = state.last_snapshot_seq_by_sub[last]
			state.last_server_ts_by_sub[idx] = state.last_server_ts_by_sub[last]
			state.snapshot_logged_by_sub[idx] = state.snapshot_logged_by_sub[last]
		}
		state.active_subs[last] = {}
		state.last_seq_by_sub[last] = 0
		state.last_snapshot_seq_by_sub[last] = 0
		state.last_server_ts_by_sub[last] = 0
		state.snapshot_logged_by_sub[last] = false
		state.active_count -= 1
	}

	if state.conn_state != .Connected do return
	send_unsubscribe(state, subject)
}

@(private = "file")
send_unsubscribe :: proc(state: ^MD_Native_State, subject: string) -> bool {
	state.rid_counter += 1
	buf: [512]u8
	msg: string
	ok: bool
	// In Terminal_V1: prefer stream_id from ACK when available.
	if state.transport_mode == .Terminal_V1 {
		sid := util.subject_id64(subject)
		if si := find_sub_by_subject_id(state, sid); si >= 0 && state.active_subs[si].stream_id_len > 0 {
			stored_sid := string(state.active_subs[si].stream_id[:int(state.active_subs[si].stream_id_len)])
			msg, ok = md_common.build_unsubscribe_msg_v2(buf[:], stored_sid, state.rid_counter)
		}
	}
	if !ok {
		msg, ok = md_common.build_unsubscribe_msg(buf[:], subject, state.rid_counter)
	}
	if !ok do return false
	if !frame_within_limit(state, len(msg)) do return false
	err := ws_write_text(state.conn, string(msg))
	if err == nil {
		rid_buf: [16]u8
		fmt.printf("[md-lifecycle] unsubscribe_sent subject=%s rid=r%s\n", subject, fmt.bprintf(rid_buf[:], "%d", state.rid_counter))
	}
	return err == nil
}

@(private = "file")
send_subscribe :: proc(state: ^MD_Native_State, subject: string) -> bool {
	state.rid_counter += 1
	buf: [768]u8
	msg: string
	ok: bool
	if state.transport_mode == .Terminal_V1 {
		venue, symbol, channel, aggregation := md_common.parse_subject_components(subject)
		msg, ok = md_common.build_subscribe_msg_v2(buf[:], subject, venue, symbol, channel, aggregation, state.rid_counter)
	} else {
		msg, ok = md_common.build_subscribe_msg(buf[:], subject, state.rid_counter)
	}
	if !ok do return false
	if !frame_within_limit(state, len(msg)) do return false
	err := ws_write_text(state.conn, msg)
	if err == nil {
		rid_buf: [16]u8
		fmt.printf("[md-lifecycle] subscribe_sent subject=%s rid=r%s\n", subject, fmt.bprintf(rid_buf[:], "%d", state.rid_counter))
	}
	return err == nil
}

RESYNC_TIMEOUT_MS :: 5_000

@(private = "file")
send_resync :: proc(state: ^MD_Native_State, subject: string, last_seq: i64) -> bool {
	// Prefer stored stream_id; fall back to subject.
	stream_id := subject
	sid := util.subject_id64(subject)
	if si := find_sub_by_subject_id(state, sid); si >= 0 && state.active_subs[si].stream_id_len > 0 {
		stream_id = string(state.active_subs[si].stream_id[:int(state.active_subs[si].stream_id_len)])
	}
	state.rid_counter += 1
	buf: [512]u8
	msg, ok := md_common.build_resync_msg(buf[:], stream_id, last_seq, state.rid_counter)
	if !ok do return false
	if !frame_within_limit(state, len(msg)) do return false
	err := ws_write_text(state.conn, msg)
	if err == nil {
		state.resync_count += 1
		fmt.printf("[md-lifecycle] resync_sent stream_id=%s last_seq=%d\n", stream_id, last_seq)
	}
	return err == nil
}

@(private = "file")
native_resubscribe_timeframe_channels :: proc(state: ^MD_Native_State) {
	if state == nil do return
	is_connected := state.conn_state == .Connected

	for i in 0 ..< state.active_count {
		entry := &state.active_subs[i]
		if entry.channel != .Heatmaps && entry.channel != .VPVR && entry.channel != .Candles && entry.channel != .Signals && entry.channel != .Tape do continue

		new_subject := util.build_subject_with_timeframe(entry.venue, entry.symbol, entry.channel, state.candle_tf_filter)
		if new_subject == entry.subject {
			delete(new_subject)
			continue
		}

		if is_connected {
			send_unsubscribe(state, entry.subject)
			send_subscribe(state, new_subject)
		}
		delete(entry.subject)
		entry.subject = new_subject
		entry.subject_id = util.subject_id64(new_subject)
	}
}

@(private = "file")
native_poll :: proc(events_buf: []ports.MD_Event) -> int {
	state := g_md_state
	if state == nil do return 0

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	n := 0

	// Drain trade events.
	for n < len(events_buf) && state.trade_count > 0 {
		oldest := (state.trade_write - state.trade_count + TRADE_RING_CAP) % TRADE_RING_CAP
		events_buf[n].source.subject_id = state.trade_ring_subject_id[oldest]
		events_buf[n].source.channel = .Trades
		events_buf[n].source.seq = state.trade_ring_seq[oldest]
		events_buf[n].kind = .Trade
		events_buf[n].unix = state.trade_ring[oldest].unix
		events_buf[n].data.trade = state.trade_ring[oldest]
		state.trade_count -= 1
		n += 1
	}

	// Emit latest orderbook snapshot if dirty.
	if state.ob_dirty && n < len(events_buf) {
		ob := state.ob_staging

		for i in 0 ..< ob.ask_count {
			state.poll_ask_prices[i] = ob.ask_prices[i]
			state.poll_ask_sizes[i]  = ob.ask_sizes[i]
		}
		for i in 0 ..< ob.bid_count {
			state.poll_bid_prices[i] = ob.bid_prices[i]
			state.poll_bid_sizes[i]  = ob.bid_sizes[i]
		}

		events_buf[n].source.subject_id = ob.subject_id
		events_buf[n].source.channel = .Orderbook
		events_buf[n].source.seq = ob.seq
		events_buf[n].kind = .Orderbook_Snapshot
		events_buf[n].unix = ob.unix
		events_buf[n].data.ob = ports.MD_Orderbook_Event{
			ask_prices = raw_data(state.poll_ask_prices[:ob.ask_count]),
			ask_sizes  = raw_data(state.poll_ask_sizes[:ob.ask_count]),
			bid_prices = raw_data(state.poll_bid_prices[:ob.bid_count]),
			bid_sizes  = raw_data(state.poll_bid_sizes[:ob.bid_count]),
			ask_count  = ob.ask_count,
			bid_count  = ob.bid_count,
			is_snapshot = ob.is_snapshot,
			last_price = ob.last_price,
			unix       = ob.unix,
		}
		state.ob_dirty = false
		n += 1
	}

	// Emit latest stats if dirty.
	if state.stats_dirty && n < len(events_buf) {
		st := state.stats_staging
		events_buf[n].source.subject_id = st.subject_id
		events_buf[n].source.channel = .Stats
		events_buf[n].source.seq = st.seq
		events_buf[n].kind = .Stats
		events_buf[n].unix = st.unix
			events_buf[n].data.stats = ports.MD_Stats_Event{
				mark_price = st.mark_price,
				funding    = st.funding,
				tbuy       = st.tbuy,
				tsell      = st.tsell,
				window_ms  = st.window_ms,
				ts_ingest_ms = st.ts_ingest_ms,
				quality_flags = st.quality_flags,
				unix       = st.unix,
			}
		state.stats_dirty = false
		n += 1
	}

	// Emit latest tape if dirty.
	if state.tape_dirty && n < len(events_buf) {
		tp := state.tape_staging
		events_buf[n].source.subject_id = tp.subject_id
		events_buf[n].source.channel = .Tape
		events_buf[n].source.seq = tp.seq
		events_buf[n].kind = .Tape
		events_buf[n].unix = tp.unix
		events_buf[n].data.tape = ports.MD_Tape_Event{
			last_price      = tp.last_price,
			total_volume    = tp.total_volume,
			buy_volume      = tp.buy_volume,
			sell_volume     = tp.sell_volume,
			trade_count     = tp.trade_count,
			rate_per_sec    = tp.rate_per_sec,
			imbalance       = tp.imbalance,
			is_burst        = tp.is_burst,
			window_start_ts = tp.window_start_ts,
			window_end_ts   = tp.window_end_ts,
			unix            = tp.unix,
		}
		state.tape_dirty = false
		n += 1
	}

	// Emit latest heatmap if dirty.
	if state.heatmap_dirty && n < len(events_buf) {
		hm := state.heatmap_staging
		lc := min(hm.level_count, services.HEATMAP_STAGING_CAP)

		for i in 0 ..< lc {
			state.poll_hm_prices[i] = hm.prices[i]
			state.poll_hm_sizes[i]  = hm.sizes[i]
		}

		events_buf[n].source.subject_id = hm.subject_id
		events_buf[n].source.channel = .Heatmaps
		events_buf[n].source.seq = hm.seq
		events_buf[n].kind = .Heatmap
		events_buf[n].unix = hm.unix
		events_buf[n].data.heatmap = ports.MD_Heatmap_Event{
			prices          = raw_data(state.poll_hm_prices[:lc]),
			sizes           = raw_data(state.poll_hm_sizes[:lc]),
			level_count     = lc,
			price_group     = hm.price_group,
			min_price       = hm.min_price,
			max_price       = hm.max_price,
			max_size        = hm.max_size,
			unix            = hm.unix,
			window_start_ms = hm.window_start_ms,
		}
		state.heatmap_dirty = false
		n += 1
	}

	// Emit latest VPVR if dirty.
	if state.vpvr_dirty && n < len(events_buf) {
		vp := state.vpvr_staging
		lc := min(vp.level_count, services.VPVR_STAGING_CAP)

		for i in 0 ..< lc {
			state.poll_vpvr_prices[i] = vp.prices[i]
			state.poll_vpvr_buys[i]   = vp.buys[i]
			state.poll_vpvr_sells[i]  = vp.sells[i]
		}

		events_buf[n].source.subject_id = vp.subject_id
		events_buf[n].source.channel = .VPVR
		events_buf[n].source.seq = vp.seq
		events_buf[n].kind = .VPVR
		events_buf[n].unix = vp.unix
		events_buf[n].data.vpvr = ports.MD_VPVR_Event{
			prices      = raw_data(state.poll_vpvr_prices[:lc]),
			buys        = raw_data(state.poll_vpvr_buys[:lc]),
			sells       = raw_data(state.poll_vpvr_sells[:lc]),
			level_count = lc,
			price_group = vp.price_group,
			min_price   = vp.min_price,
			max_price   = vp.max_price,
			unix        = vp.unix,
		}
		state.vpvr_dirty = false
		n += 1
	}

	// Drain candle ring buffer.
	for n < len(events_buf) && state.candle_ring_count > 0 {
		oldest := (state.candle_ring_write - state.candle_ring_count + CANDLE_RING_CAP) % CANDLE_RING_CAP
		cs := state.candle_ring[oldest]
		events_buf[n].source.subject_id = cs.subject_id
		events_buf[n].source.channel = .Candles
		events_buf[n].source.seq = cs.seq
		events_buf[n].kind = .Candle
		events_buf[n].unix = util.normalize_unix_seconds(cs.window_end_ts)
		events_buf[n].data.candle = ports.MD_Candle_Event{
			open            = cs.open,
			high            = cs.high,
			low             = cs.low,
			close           = cs.close,
			volume          = cs.volume,
			buy_vol         = cs.buy_vol,
			sell_vol        = cs.sell_vol,
			trade_count     = cs.trade_count,
			window_start_ts = cs.window_start_ts,
			window_end_ts   = cs.window_end_ts,
			is_closed       = cs.is_closed,
		}
		state.candle_ring_count -= 1
		n += 1
	}

	// Emit range candle batch if dirty.
	if state.range_candle_dirty && n < len(events_buf) {
		rc := state.range_candle_staging
		batch: ports.MD_Range_Candle_Batch
		batch.count = min(rc.count, ports.RANGE_CANDLE_MAX)
		batch.is_last = rc.is_last
		for i in 0 ..< batch.count {
			c := rc.candles[i]
			batch.candles[i] = ports.MD_Candle_Event{
				open            = c.open,
				high            = c.high,
				low             = c.low,
				close           = c.close,
				volume          = c.volume,
				buy_vol         = c.buy_vol,
				sell_vol        = c.sell_vol,
				trade_count     = c.trade_count,
				window_start_ts = c.window_start_ts,
				window_end_ts   = c.window_end_ts,
				is_closed       = c.is_closed,
			}
		}
		events_buf[n].source.subject_id = rc.candles[0].subject_id if rc.count > 0 else 0
		events_buf[n].source.channel = .Candles
		events_buf[n].source.seq = rc.seq
		events_buf[n].kind = .Range_Candle_Batch
		events_buf[n].data.range_candles = batch
		state.range_candle_dirty = false
		n += 1
	}
	if state.evidence_dirty && n < len(events_buf) {
		ev := state.evidence_staging
		native_fill_evidence_event(&events_buf[n], ev)
		state.evidence_dirty = false
		n += 1
	}
	for n < len(events_buf) && state.signal_ring_count > 0 {
		oldest := (state.signal_ring_write - state.signal_ring_count + SIGNAL_RING_CAP) % SIGNAL_RING_CAP
		sig := state.signal_ring[oldest]
		native_fill_signal_event(&events_buf[n], sig)
		state.signal_ring_count -= 1
		n += 1
	}

	// S47: Emit analytics staging events.
	if state.oi_dirty && n < len(events_buf) {
		oi := state.oi_staging
		events_buf[n].source.subject_id = oi.subject_id
		events_buf[n].source.channel = .Analytics_OI  // S98: dedicated analytics channel
		events_buf[n].source.seq = oi.seq
		events_buf[n].kind = .Open_Interest
		events_buf[n].unix = oi.unix
		events_buf[n].data.open_interest = ports.MD_Open_Interest_Event{
			open_interest   = oi.open_interest,
			delta           = oi.delta,
			delta_pct       = oi.delta_pct,
			window_start_ts = oi.window_start_ts,
			window_end_ts   = oi.window_end_ts,
			unix            = oi.unix,
		}
		state.oi_dirty = false
		n += 1
	}
	if state.delta_vol_dirty && n < len(events_buf) {
		dv := state.delta_vol_staging
		events_buf[n].source.subject_id = dv.subject_id
		events_buf[n].source.channel = .Analytics_Delta_Volume  // S98
		events_buf[n].source.seq = dv.seq
		events_buf[n].kind = .Delta_Volume
		events_buf[n].unix = dv.unix
		events_buf[n].data.delta_volume = ports.MD_Delta_Volume_Event{
			buy_volume      = dv.buy_volume,
			sell_volume     = dv.sell_volume,
			delta_volume    = dv.delta_volume,
			window_start_ts = dv.window_start_ts,
			window_end_ts   = dv.window_end_ts,
			unix            = dv.unix,
		}
		state.delta_vol_dirty = false
		n += 1
	}
	if state.cvd_dirty && n < len(events_buf) {
		cv := state.cvd_staging
		events_buf[n].source.subject_id = cv.subject_id
		events_buf[n].source.channel = .Analytics_CVD  // S98
		events_buf[n].source.seq = cv.seq
		events_buf[n].kind = .CVD
		events_buf[n].unix = cv.unix
		events_buf[n].data.cvd = ports.MD_CVD_Event{
			delta_volume    = cv.delta_volume,
			cvd             = cv.cvd,
			window_start_ts = cv.window_start_ts,
			window_end_ts   = cv.window_end_ts,
			unix            = cv.unix,
		}
		state.cvd_dirty = false
		n += 1
	}
	if state.bar_stats_dirty && n < len(events_buf) {
		bs := state.bar_stats_staging
		events_buf[n].source.subject_id = bs.subject_id
		events_buf[n].source.channel = .Analytics_Bar_Stats  // S98
		events_buf[n].source.seq = bs.seq
		events_buf[n].kind = .Bar_Stats
		events_buf[n].unix = bs.unix
		events_buf[n].data.bar_stats = ports.MD_Bar_Stats_Event{
			trade_count     = bs.trade_count,
			buy_count       = bs.buy_count,
			sell_count      = bs.sell_count,
			total_volume    = bs.total_volume,
			buy_volume      = bs.buy_volume,
			sell_volume     = bs.sell_volume,
			vwap_price      = bs.vwap_price,
			imbalance       = bs.imbalance,
			is_burst        = bs.is_burst,
			window_start_ts = bs.window_start_ts,
			window_end_ts   = bs.window_end_ts,
			unix            = bs.unix,
		}
		state.bar_stats_dirty = false
		n += 1
	}

	return n
}

@(private = "package")
native_fill_evidence_event :: proc(dst: ^ports.MD_Event, ev: services.Parsed_Evidence) {
	if dst == nil do return
	dst.source.subject_id = ev.subject_id
	dst.source.channel = .Evidence
	dst.source.seq = ev.seq
	dst.kind = .Evidence
	dst.unix = util.normalize_unix_seconds(ev.unix)
	dst.data.evidence = ports.MD_Evidence_Event{
		kind          = ev.kind,
		kind_len      = ev.kind_len,
		confidence    = ev.confidence,
		reason        = ev.reason,
		reason_len    = ev.reason_len,
		feature_tags  = ev.feature_tags,
		feature_vals  = ev.feature_vals,
		feature_count = ev.feature_count,
		unix          = ev.unix,
	}
}

@(private = "package")
native_fill_signal_event :: proc(dst: ^ports.MD_Event, sig: services.Parsed_Signal) {
	if dst == nil do return
	dst.source.subject_id = sig.subject_id
	dst.source.channel = .Signals
	dst.source.seq = sig.seq
	dst.kind = .Signal
	dst.unix = util.normalize_unix_seconds(sig.unix)
	dst.data.signal = ports.MD_Signal_Event{
		kind            = sig.kind,
		kind_len        = sig.kind_len,
		severity        = sig.severity,
		severity_len    = sig.severity_len,
		confidence      = sig.confidence,
		reason          = sig.reason,
		reason_len      = sig.reason_len,
		regime          = sig.regime,
		regime_len      = sig.regime_len,
		regime_strength = sig.regime_strength,
		unix            = sig.unix,
	}
}

@(private = "file")
native_now_ms :: proc() -> i64 {
	return md_common.now_ms()
}

@(private = "file")
native_conn_status :: proc() -> ports.MD_Conn_Status {
	state := g_md_state
	if state == nil do return .Offline

	sync.lock(&state.mu)
	cs := state.conn_state
	blocked := state.reconnect_blocked
	sync.unlock(&state.mu)
	if blocked do return .Offline

	switch cs {
	case .Connected:    return .Connected
	case .Connecting:   return .Connecting
	case .Backoff_Wait: return .Reconnecting
	case .Disconnected: return .Reconnecting
	}
	return .Offline
}

// --- Background reader thread ---

@(private = "file")
reader_thread_proc :: proc(t: ^thread.Thread) {
	state := cast(^MD_Native_State)t.data

	for {
		if native_should_stop(state) do break

		sync.lock(&state.mu)
		cs := state.conn_state
		sync.unlock(&state.mu)

		if cs != .Connected {
			attempt_reconnect(state)
			continue
		}

		// Hello timeout: deterministic action from ws_fault_action().
		{
			sync.lock(&state.mu)
			hello_ok := state.hello_received
			started := state.connect_started_ms
			mode := state.transport_mode
			sync.unlock(&state.mu)

			if !hello_ok && started > 0 && mode == .Terminal_V1 {
				now := time.now()._nsec / 1_000_000
				if now - started > HELLO_TIMEOUT_MS {
					sync.lock(&state.mu)
					apply_ws_fault(state, .Timeout)
					if state.ws_error_action == .Stop {
						state.conn_state = .Disconnected
						state.backoff_ms = BACKOFF_INITIAL_MS
						state.hello_received = false
						state.hello_valid = false
						state.desync = true
						state.desync_reason = .Protocol_Version
					}
					sync.unlock(&state.mu)
					fmt.println("[md-lifecycle] hello_timeout — downgrade blocked (hard-cutover legacy disabled)")
					ws_close(&state.conn)
					continue
				}
			}
		}

		// MR protocol PING: send periodic ping with ts_client for RTT measurement.
		// In Terminal_V1 mode, uses MR PING. In Legacy_JSON, falls back to WS-level ping.
		{
			sync.lock(&state.mu)
			hello_ok := state.hello_received
			mode := state.transport_mode
			sync.unlock(&state.mu)

			if hello_ok {
				now := time.now()._nsec / 1_000_000
				if state.last_ping_sent_ms == 0 || (now - state.last_ping_sent_ms) > PING_INTERVAL_MS {
					if mode == .Terminal_V1 {
						sync.lock(&state.mu)
						send_ping(state)
						sync.unlock(&state.mu)
					} else {
						ping_err := ws_write_ping(state.conn)
						if ping_err != nil {
							fmt.printf("[md-lifecycle] ping_failed err=%v\n", ping_err)
							sync.lock(&state.mu)
							state.conn_state = .Disconnected
							state.backoff_ms = BACKOFF_INITIAL_MS
							apply_ws_fault(state, .ServerClosed)
							sync.unlock(&state.mu)
							ws_close(&state.conn)
							continue
						}
						state.last_ping_sent_ms = now
					}
				}
			}
		}

		// Backend gap detectors.
		{
			now := time.now()._nsec / 1_000_000
			sync.lock(&state.mu)
			if state.hello_received && state.transport_mode == .Terminal_V1 {
				no_metrics_gap, next_metrics_ts := md_common.detect_no_metrics_gap(
					state.last_metrics_ts_ms,
					now,
					md_common.metrics_stale_timeout_ms(state.caps.metrics_cadence_ms, METRICS_STALE_MS),
				)
				if no_metrics_gap {
					state.backend_gap_no_metrics += 1
					state.last_metrics_ts_ms = next_metrics_ts
				}
				pong_timeout_gap, next_pong_ts := md_common.detect_pong_timeout_gap(
					state.last_ping_sent_ms, state.last_pong_ts_ms, now, PONG_TIMEOUT_MS,
				)
				if pong_timeout_gap {
					state.backend_gap_pong_timeout += 1
					state.last_pong_ts_ms = next_pong_ts
				}
			}
			if state.drop_count > 0 && state.drop_count % FREQUENT_DROP_THRESHOLD == 0 {
				state.backend_gap_frequent_drops += 1
			}
			sync.unlock(&state.mu)
		}

		opcode, payload, rsv1, err := ws_read_message_ex(&state.conn)
		if err != nil {
			category := classify_ws_error(err)
			stop_now := native_should_stop(state)
			if !stop_now {
				if err == .Read_Conn_Closed {
					fmt.println("[marketdata] Connection closed by server")
				} else {
					fmt.printf("[marketdata] Read error: %v\n", err)
				}
			}
			sync.lock(&state.mu)
			if state.should_stop {
				sync.unlock(&state.mu)
				break
			}
			state.conn_state = .Disconnected
			state.backoff_ms = BACKOFF_INITIAL_MS
			apply_ws_fault(state, category)
			if category == .BackpressureDrop {
				state.drop_payload_oversize += 1
				state.drop_count += 1
			}
			state.desync_reason = .None
			state.protocol_version = 0
			state.hello_received = false
			state.hello_valid = false
			state.last_metrics_ts_ms = 0
			state.last_pong_ts_ms = 0
			state.connect_started_ms = 0
			state.first_data_logged = false
			if state.ws_error_action == .Stop {
				state.reconnect_blocked = true
			}
			sync.unlock(&state.mu)
			fmt.printf("[md-lifecycle] disconnect reason=%v\n", err)
			ws_close(&state.conn)
			continue
		}

		handled := false
		if opcode == 0x1 && !rsv1 {
			if process_batched_frame(state, payload) {
				handled = true
			} else {
				apply_parse_result(state, payload)
				handled = true
			}
		} else if opcode == 0x2 || rsv1 {
			if !native_supports_decompress() {
				continue
			}
			decompressed: bytes.Buffer
			if !decode_compressed_payload(payload, &decompressed) {
				bytes.buffer_destroy(&decompressed)
				sync.lock(&state.mu)
				state.parse_error_count += 1
				sync.unlock(&state.mu)
				continue
			}
			decoded := bytes.buffer_to_bytes(&decompressed)
			if process_batched_frame(state, decoded) {
				handled = true
			} else {
				apply_parse_result(state, decoded)
				handled = true
			}
			bytes.buffer_destroy(&decompressed)
		}
		if !handled do continue

		// Desync recovery: send RESYNC in Terminal_V1, fallback to unsub+resub.
		sync.lock(&state.mu)
		resub_sid := state.desync_resub_subject_id
		resub_subject := ""
		resub_last_seq := i64(0)
		transport := state.transport_mode
		resync_pending := state.resync_pending_subject_id
		resync_ts := state.resync_sent_ms
		if resub_sid != 0 {
			if si := find_sub_by_subject_id(state, resub_sid); si >= 0 {
				resub_subject = state.active_subs[si].subject
				resub_last_seq = state.last_seq_by_sub[si]
			}
			state.desync_resub_subject_id = 0
		}
		sync.unlock(&state.mu)

		// Check RESYNC timeout: if pending and expired, fall back to unsub+resub.
		now_resync := time.now()._nsec / 1_000_000
		if md_common.detect_resync_ack_timeout(resync_pending, resync_ts, now_resync, RESYNC_TIMEOUT_MS) {
			fmt.printf("[md-lifecycle] resync_timeout sid=%x — fallback to unsub+resub\n", resync_pending)
			sync.lock(&state.mu)
			state.backend_gap_resync_ack_timeout += 1
			state.resync_pending_subject_id = 0
			state.resync_sent_ms = 0
			// Queue the subject for legacy resub.
			state.desync_resub_subject_id = resync_pending
			sync.unlock(&state.mu)
		}

		if len(resub_subject) > 0 {
			sync.lock(&state.mu)
			is_connected := state.conn_state == .Connected
			sync.unlock(&state.mu)
			if is_connected {
				if transport == .Terminal_V1 && resync_pending == 0 {
					// Send RESYNC and wait for snapshot response.
					fmt.printf("[md-lifecycle] desync_recovery resync subject=%s last_seq=%d\n", resub_subject, resub_last_seq)
					sync.lock(&state.mu)
					send_resync(state, resub_subject, resub_last_seq)
					state.resync_pending_subject_id = resub_sid
					state.resync_sent_ms = time.now()._nsec / 1_000_000
					sync.unlock(&state.mu)
				} else {
					// Legacy mode or RESYNC already pending: fall back to unsub+resub.
					fmt.printf("[md-lifecycle] desync_recovery resub subject=%s\n", resub_subject)
					sync.lock(&state.mu)
					send_unsubscribe(state, resub_subject)
					send_subscribe(state, resub_subject)
					state.desync = false
					state.desync_reason = .None
					state.resync_pending_subject_id = 0
					state.resync_sent_ms = 0
					sync.unlock(&state.mu)
				}
			}
		}
	}
}

@(private = "file")
attempt_reconnect :: proc(state: ^MD_Native_State) {
	// Copy URL, API key, and JWT to stack buffers under lock to avoid data races
	// (main thread may call reconnect_transport concurrently).
	url_stack: [256]u8
	url_stack_len := 0
	key_stack: [128]u8
	key_stack_len := 0
	jwt_stack: [256]u8
	jwt_stack_len := 0

	sync.lock(&state.mu)
	if state.reconnect_blocked {
		sync.unlock(&state.mu)
		time.sleep(1 * time.Second)
		return
	}
	state.conn_state = .Backoff_Wait
	set_transport_state(state, .Backoff)
	backoff := state.backoff_ms
	state.reconnect_count += 1
	state.reconnect_streak += 1
	count := state.reconnect_streak
	url_stack_len = min(len(state.ws_url), len(url_stack))
	for i in 0 ..< url_stack_len { url_stack[i] = state.ws_url[i] }
	key_stack_len = min(len(state.api_key), len(key_stack))
	for i in 0 ..< key_stack_len { key_stack[i] = state.api_key[i] }
	jwt_stack_len = min(len(state.jwt_token), len(jwt_stack))
	for i in 0 ..< jwt_stack_len { jwt_stack[i] = state.jwt_token[i] }
	sync.unlock(&state.mu)

	local_url := string(url_stack[:url_stack_len])
	local_key := string(key_stack[:key_stack_len])
	local_jwt := string(jwt_stack[:jwt_stack_len])

	jittered := md_common.backoff_with_jitter(backoff, &state.jitter_seed)
	fmt.printf("[marketdata] Reconnecting in %dms (attempt %d)\n", jittered, count)
	time.sleep(time.Duration(jittered) * time.Millisecond)

	if native_should_stop(state) do return

	hdr_buf: [384]u8
	extra_hdr, _ := build_auth_header(hdr_buf[:], local_key, local_jwt)

	sync.lock(&state.mu)
	state.conn_state = .Connecting
	sync.unlock(&state.mu)

	conn, err := ws_dial(local_url, extra_hdr)
	if err != nil {
		fmt.printf("[marketdata] Reconnect failed (err=%v)\n", err)
		sync.lock(&state.mu)
		state.conn_state = .Disconnected
		state.backoff_ms = min(backoff * BACKOFF_MULTIPLIER, BACKOFF_MAX_MS)
		apply_ws_fault(state, classify_ws_error(err))
		sync.unlock(&state.mu)
		return
	}

	safe_url := log_safe_url(local_url)
	fmt.printf("[marketdata] Reconnected to %s\n", safe_url)
	fmt.printf("[md-lifecycle] connect url=%s\n", safe_url)
	sync.lock(&state.mu)
	state.conn = conn
	state.conn_state = .Connected
	state.backoff_ms = BACKOFF_INITIAL_MS
	state.reconnect_streak = 0
	state.desync = false
	state.desync_reason = .None
	state.protocol_version = 0
	state.hello_received = false
	state.hello_valid = false
	state.transport_mode = .Terminal_V1 // Assume Terminal_V1 until hello timeout.
	state.allow_legacy_ws = read_allow_legacy_ws_native()
	state.ws_error_category = .None
	state.ws_error_action = .None
	state.server_metrics_received = false
	state.last_metrics_ts_ms = 0
	state.last_pong_ts_ms = 0
	state.connect_started_ms = time.now()._nsec / 1_000_000
	state.first_data_logged = false
	set_transport_state(state, .Hello_Pending)
	for i in 0 ..< state.active_count {
		state.last_seq_by_sub[i] = 0
		state.last_snapshot_seq_by_sub[i] = 0
		state.last_server_ts_by_sub[i] = 0
		state.snapshot_logged_by_sub[i] = false
	}
	// Terminal_V1: send HELLO immediately on reconnect.
	send_hello(state)

	// Snapshot active subscriptions before re-subscribing to avoid races with
	// concurrent subscribe/unsubscribe while we iterate.
	subjects: [MAX_SUBS]string
	sub_count := 0
	for i in 0 ..< state.active_count {
		if sub_count >= MAX_SUBS do break
		subjects[sub_count] = state.active_subs[i].subject
		sub_count += 1
	}
	sync.unlock(&state.mu)

	for i in 0 ..< sub_count {
		subject := subjects[i]
		if len(subject) > 0 {
			sync.lock(&state.mu)
			if state.conn_state == .Connected {
				send_subscribe(state, subject)
			}
			sync.unlock(&state.mu)
		}
	}
}

// --- MR protocol JSON parsing ---
// Delegates to shared services.parse_mr_message, then writes results to staging
// under mutex protection (background thread → main thread handoff).

BATCH_SYNTH_FRAME_CAP :: 96 * 1024
DECOMPRESS_MAX_INPUT  :: 256 * 1024

@(private = "file")
append_ascii :: proc(buf: []u8, n: ^int, s: string) -> bool {
	for c in s {
		if n^ >= len(buf) do return false
		buf[n^] = u8(c)
		n^ += 1
	}
	return true
}

@(private = "file")
append_i64_ascii :: proc(buf: []u8, n: ^int, v: i64) -> bool {
	tmp: [24]u8
	num := fmt.bprintf(tmp[:], "%d", v)
	return append_ascii(buf, n, num)
}

@(private = "file")
build_batched_event_frame :: proc(
	dst: []u8,
	seg: ^services.Parsed_Batched_Frame,
	ev: services.Parsed_Batch_Event_View,
	src_raw: []u8,
) -> ([]u8, bool) {
	if seg == nil do return nil, false
	if ev.payload_start < 0 || ev.payload_end <= ev.payload_start || ev.payload_end > len(src_raw) do return nil, false

	n := 0
	if !append_ascii(dst, &n, `{"type":"event","subject":"`) do return nil, false
	if seg.stream_id_len > 0 {
		if !append_ascii(dst, &n, string(seg.stream_id_buf[:int(seg.stream_id_len)])) do return nil, false
	} else {
		if seg.channel_len == 0 || seg.venue_len == 0 || seg.symbol_len == 0 do return nil, false
		if !append_ascii(dst, &n, string(seg.channel_buf[:int(seg.channel_len)])) do return nil, false
		if !append_ascii(dst, &n, "/") do return nil, false
		if !append_ascii(dst, &n, string(seg.venue_buf[:int(seg.venue_len)])) do return nil, false
		if !append_ascii(dst, &n, "/") do return nil, false
		if !append_ascii(dst, &n, string(seg.symbol_buf[:int(seg.symbol_len)])) do return nil, false
		if !append_ascii(dst, &n, "/raw") do return nil, false
	}
	if !append_ascii(dst, &n, `","seq":`) do return nil, false
	seq := seg.base_seq + i64(ev.event_index)
	if !append_i64_ascii(dst, &n, seq) do return nil, false
	if !append_ascii(dst, &n, `,"ts_server":`) do return nil, false
	if !append_i64_ascii(dst, &n, seg.ts_server_base + ev.dts) do return nil, false
	if !append_ascii(dst, &n, `,"ts_ingest":`) do return nil, false
	if !append_i64_ascii(dst, &n, seg.ts_ingest_base + ev.dti) do return nil, false
	if !append_ascii(dst, &n, `,"payload":`) do return nil, false
	payload := src_raw[ev.payload_start:ev.payload_end]
	if n + len(payload) + 1 > len(dst) do return nil, false
	copy(dst[n:n + len(payload)], payload)
	n += len(payload)
	if !append_ascii(dst, &n, "}") do return nil, false
	return dst[:n], true
}

@(private = "file")
process_batched_frame :: proc(state: ^MD_Native_State, raw: []u8) -> bool {
	head: services.Parsed_Batched_Frame
	if !services.parse_batched_frame(raw, &head) {
		return false
	}
	decode_start := time.tick_now()
	total_events := head.total_events
	if head.count > total_events do total_events = head.count
	if total_events < 0 do total_events = 0

	sync.lock(&state.mu)
	state.batched_frames_received += 1
	state.batched_events_received += u64(total_events)
	sync.unlock(&state.mu)

	skip := 0
	frame_buf: [BATCH_SYNTH_FRAME_CAP]u8
	for {
		seg: services.Parsed_Batched_Frame
		if !services.parse_batched_frame(raw, &seg, skip) {
			sync.lock(&state.mu)
			state.parse_error_count += 1
			sync.unlock(&state.mu)
			break
		}
		if seg.event_count <= 0 do break

		for i in 0 ..< seg.event_count {
			ev := seg.events[i]
			synth, ok := build_batched_event_frame(frame_buf[:], &seg, ev, raw)
			if !ok {
				sync.lock(&state.mu)
				state.parse_error_count += 1
				sync.unlock(&state.mu)
				continue
			}
			apply_parse_result(state, synth)
		}
		skip += seg.event_count
		if !seg.has_more do break
	}

	decode_us := i64(time.duration_microseconds(time.tick_since(decode_start)))
	sync.lock(&state.mu)
	record_perf_sample(&state.batch_decode_samples_us, &state.batch_decode_sample_head, &state.batch_decode_sample_count, decode_us)
	sync.unlock(&state.mu)
	return true
}

@(private = "file")
decode_compressed_payload :: proc(payload: []u8, out: ^bytes.Buffer) -> bool {
	if len(payload) <= 0 || len(payload) > DECOMPRESS_MAX_INPUT do return false
	if out == nil do return false
	bytes.buffer_reset(out)
	if zlib.inflate(payload, out, true) == nil {
		return true
	}
	bytes.buffer_reset(out)
	if zlib.inflate(payload, out, false) == nil {
		return true
	}
	with_tail := make([]u8, len(payload) + 4)
	defer delete(with_tail)
	copy(with_tail[:len(payload)], payload)
	with_tail[len(payload) + 0] = 0x00
	with_tail[len(payload) + 1] = 0x00
	with_tail[len(payload) + 2] = 0xFF
	with_tail[len(payload) + 3] = 0xFF
	bytes.buffer_reset(out)
	if zlib.inflate(with_tail, out, true) == nil {
		return true
	}
	return false
}



@(private = "file")
apply_parse_result :: proc(state: ^MD_Native_State, raw: []u8) {
	defer services.parse_arena_reset_message(&state.parse_arena)
	parse_start_tick := time.tick_now()

	telemetry: services.Parse_Telemetry
	result := services.parse_mr_message_with_arena(&state.parse_arena, raw, &telemetry)
	parse_end_tick := time.tick_now()
	parse_us := i64(time.duration_microseconds(time.tick_diff(parse_start_tick, parse_end_tick)))
	defer {
		apply_end_tick := time.tick_now()
		apply_us := i64(time.duration_microseconds(time.tick_diff(parse_end_tick, apply_end_tick)))
		sync.lock(&state.mu)
		record_perf_sample(&state.parse_samples_us, &state.parse_sample_head, &state.parse_sample_count, parse_us)
		record_perf_sample(&state.apply_samples_us, &state.apply_sample_head, &state.apply_sample_count, apply_us)
		sync.unlock(&state.mu)
	}
	parsed_now_ms := time.now()._nsec / 1_000_000
	snapshot_subject := ""
	should_log_snapshot := false
	snapshot_seq := i64(0)
	snapshot_sid := u64(0)
	should_log_first_data := false
	first_data_delta_ms := i64(0)

	sync.lock(&state.mu)
	md_common.update_parse_rates(&state.rates, parsed_now_ms, len(raw))
	sync.unlock(&state.mu)

	// Accumulate telemetry under lock.
	if telemetry.parse_errors > 0 {
		sync.lock(&state.mu)
		state.parse_error_count += telemetry.parse_errors
		ec := state.parse_error_count
		sync.unlock(&state.mu)
		if ec <= 3 || ec % 50 == 0 {
			fmt.printf("[marketdata] Parse error #%d (frame_len=%d)\n", ec, len(raw))
		}
	}

	if result.kind != .None {
		sync.lock(&state.mu)
		state.last_msg_ts_ms = parsed_now_ms
		if result.meta.server_ts_ms > 0 {
			state.last_server_ts_ms = result.meta.server_ts_ms
			if parsed_now_ms >= result.meta.server_ts_ms {
				state.last_lag_ms = parsed_now_ms - result.meta.server_ts_ms
			}
		}
		if md_common.missing_ts_server_gap(result.meta.has_ts_server, result.kind, state.transport_mode) {
			state.backend_gap_missing_ts_server += 1
		}
		if result.meta.subject_id != 0 && result.meta.seq > 0 {
			if si := find_sub_by_subject_id(state, result.meta.subject_id); si >= 0 {
				prev_seq := state.last_seq_by_sub[si]
				gap, next_streak, recurring := md_common.seq_gap_transition(prev_seq, result.meta.seq, state.seq_gap_streak, 3)
				if gap {
					state.seq_gap_count += 1
					state.seq_gap_streak = next_streak
					if recurring {
						state.backend_gap_seq_gap_recurring += 1
					}
					// Only escalate to desync for significant gaps (>10), consistent
					// with stream controller tolerance for multi-replica interleaving.
					abs_gap := result.meta.seq - prev_seq
					if abs_gap < 0 do abs_gap = -abs_gap
					SEQ_GAP_TOLERANCE :: i64(10)
					if abs_gap > SEQ_GAP_TOLERANCE {
						state.desync = true
						state.desync_reason = .Sequence_Gap
						set_transport_state(state, .Desync)
						if state.desync_resub_subject_id == 0 {
							state.desync_resub_subject_id = result.meta.subject_id
						}
					}
				} else {
					state.seq_gap_streak = next_streak
					// Auto-recover: monotonic sequence resumes — clear seq gap desync.
					if state.desync_reason == .Sequence_Gap {
						state.desync = false
						state.desync_reason = .None
						set_transport_state(state, .Running)
					}
				}
				state.last_seq_by_sub[si] = result.meta.seq
				// prev_seq chaining: validate if server sent prev_seq.
				if result.meta.prev_seq > 0 {
					if md_common.validate_prev_seq(result.meta.prev_seq, prev_seq) {
						state.prev_seq_violations += 1
					}
				}
				// Snapshot integrity: validate monotonicity/consistency + hash format.
				if result.meta.is_snapshot {
					if result.meta.snapshot_seq > 0 {
						if md_common.validate_snapshot_seq_monotonic(result.meta.snapshot_seq, state.last_snapshot_seq_by_sub[si]) {
							state.snapshot_seq_violations += 1
						}
						state.last_snapshot_seq_by_sub[si] = result.meta.snapshot_seq
					}
					if md_common.validate_snapshot_integrity_consistency(result.meta.seq, result.meta.snapshot_seq, result.meta.watermark_seq) {
						state.snapshot_seq_violations += 1
					}
					if result.meta.snapshot_hash_len > 0 {
						if !md_common.validate_snapshot_hash_format(result.meta.snapshot_hash, result.meta.snapshot_hash_len) {
							state.snapshot_hash_mismatches += 1
						} else {
							// Hash is format-valid. Only track "skipped" when feature negotiated.
							if negotiated_has_feature(state, "snapshot_hash") {
								state.hash_validation_skipped += 1
								fmt.printf("[md-lifecycle] snapshot_hash_skipped sid=%x seq=%d reason=noncanonical_input\n", result.meta.subject_id, result.meta.seq)
							}
						}
					}
				}
				if result.meta.server_ts_ms > 0 {
					prev_server_ts := state.last_server_ts_by_sub[si]
					// Allow minor timestamp regressions (up to 5s) — expected with
					// multi-replica processors delivering interleaved events.
					TS_REGRESSION_TOLERANCE_MS :: i64(5_000)
					if prev_server_ts > 0 && result.meta.server_ts_ms < prev_server_ts - TS_REGRESSION_TOLERANCE_MS {
						state.desync = true
						state.desync_reason = .Protocol_Invalid
						set_transport_state(state, .Desync)
						if state.desync_resub_subject_id == 0 {
							state.desync_resub_subject_id = result.meta.subject_id
						}
					} else if state.desync_reason == .Protocol_Invalid {
						// Auto-recover: valid forward-progressing timestamp clears desync.
						state.desync = false
						state.desync_reason = .None
						set_transport_state(state, .Running)
					}
					// Only update watermark on forward progress to avoid ratcheting down.
					if result.meta.server_ts_ms >= prev_server_ts {
						state.last_server_ts_by_sub[si] = result.meta.server_ts_ms
					}
				}
				if result.meta.is_snapshot && !state.snapshot_logged_by_sub[si] {
					state.snapshot_logged_by_sub[si] = true
					snapshot_subject = strings.clone(state.active_subs[si].subject)
					snapshot_seq = result.meta.seq
					snapshot_sid = result.meta.subject_id
					should_log_snapshot = true
				}
				// RESYNC success: snapshot arrived for the pending resync subject.
				if result.meta.is_snapshot && state.resync_pending_subject_id == result.meta.subject_id {
					state.resync_pending_subject_id = 0
					state.resync_sent_ms = 0
					state.desync = false
					state.desync_reason = .None
					set_transport_state(state, .Running)
				}
			}
		}
		if md_common.parse_result_has_data(result.kind) && state.connect_started_ms > 0 && !state.first_data_logged {
			first_data_delta_ms = max(parsed_now_ms - state.connect_started_ms, 0)
			state.first_data_logged = true
			should_log_first_data = true
			if state.desync_reason == .None && state.hello_received && state.hello_valid {
				set_transport_state(state, .Running)
			}
		}
		sync.unlock(&state.mu)
	}
	if should_log_snapshot {
		fmt.printf("[md-lifecycle] snapshot_recv subject=%s sid=%x seq=%d\n", snapshot_subject, snapshot_sid, snapshot_seq)
		delete(snapshot_subject)
	}
	if should_log_first_data {
		fmt.printf("[md-lifecycle] first_data_after_connect_ms=%d kind=%v sid=%x\n", first_data_delta_ms, result.kind, result.meta.subject_id)
	}
	if md_common.parse_result_has_data(result.kind) {
		sync.lock(&state.mu)
		hello_received := state.hello_received
		hello_valid := state.hello_valid
		prev_reason := state.desync_reason
		if !hello_received {
			state.desync = true
			state.desync_reason = .Missing_Hello
			set_transport_state(state, .Desync)
		}
		sync.unlock(&state.mu)
		if !hello_received {
			if prev_reason != .Missing_Hello {
				fmt.printf("[md-lifecycle] desync reason=missing_hello kind=%v sid=%x\n", result.kind, result.meta.subject_id)
			}
			return
		}
		if !hello_valid {
			return
		}
	}

	switch result.kind {
	case .Ack:
		ack := result.data.ack
		sync.lock(&state.mu)
		state.subscribe_ack_count += 1
		// Store stream_id from ACK into the matching sub entry.
		if len(ack.stream_id) > 0 && len(ack.subject) > 0 {
			sid := util.subject_id64(ack.subject)
			if si := find_sub_by_subject_id(state, sid); si >= 0 {
				n := min(len(ack.stream_id), len(state.active_subs[si].stream_id))
				for i in 0 ..< n { state.active_subs[si].stream_id[i] = ack.stream_id[i] }
				state.active_subs[si].stream_id_len = u8(n)
			}
		}
		sync.unlock(&state.mu)
		if len(ack.stream_id) > 0 {
			fmt.printf("[md-lifecycle] ack_recv op=%s subject=%s stream_id=%s\n", ack.op, ack.subject, ack.stream_id)
		} else {
			fmt.printf("[md-lifecycle] ack_recv op=%s subject=%s\n", ack.op, ack.subject)
		}
	case .Hello_Ack:
		ha := result.data.hello_ack
		sync.lock(&state.mu)
		state.negotiated_feature_count = ha.negotiated_feature_count
		for i in 0 ..< ha.negotiated_feature_count {
			state.negotiated_features[i] = ha.negotiated_features[i]
		}
		sync.unlock(&state.mu)
		fmt.printf("[md-lifecycle] hello_ack_recv negotiated_features=%d\n", ha.negotiated_feature_count)
	case .Hello:
		h := result.data.hello
		sync.lock(&state.mu)
		state.hello_received = true
		state.protocol_version = h.proto_ver
		state.hello_valid = h.valid
		state.transport_mode = .Terminal_V1
		set_transport_state(state, .Hello_Pending)
		md_common.apply_hello_to_capabilities(&state.caps, h)
		if !h.valid {
			state.desync = true
			state.desync_reason = md_common.desync_reason_from_hello_reject(h.reject)
			set_transport_state(state, .Desync)
		} else {
			state.desync = false
			state.desync_reason = .None
			set_transport_state(state, .Running)
		}
		sync.unlock(&state.mu)
		if !h.valid {
			fmt.printf(
				"[md-lifecycle] hello_rejected proto_ver=%d reject=%v topics=%d venues=%d symbols=%d\n",
				h.proto_ver, h.reject, h.topic_count, h.venue_count, h.symbol_count,
			)
			return
		}
		fmt.printf(
			"[md-lifecycle] hello_ok proto_ver=%d server_id=%s topics=%d venues=%d symbols=%d\n",
			h.proto_ver, h.server_instance_id, h.topic_count, h.venue_count, h.symbol_count,
		)
	case .Heartbeat, .Health:
		ctrl := result.data.control
		sync.lock(&state.mu)
		if ctrl.rtt_ms > 0 do state.last_rtt_ms = ctrl.rtt_ms
		if ctrl.dropped > 0 && ctrl.dropped > state.drop_count do state.drop_count = ctrl.dropped
		sync.unlock(&state.mu)
	case .Pong:
		p := result.data.pong
		sync.lock(&state.mu)
		state.pong_rtt_ms = p.rtt_ms
		if p.rtt_ms > 0 do state.last_rtt_ms = p.rtt_ms
		state.last_pong_ts_ms = parsed_now_ms
		sync.unlock(&state.mu)
	case .Metrics:
		m := result.data.server_metrics
		sync.lock(&state.mu)
		state.server_metrics = m
		state.server_metrics_received = true
		state.last_metrics_ts_ms = parsed_now_ms
		sync.unlock(&state.mu)
	case .Error:
		ed := result.data.error_detail
		if len(ed.code) > 0 || len(ed.error_code) > 0 {
			fmt.printf("[marketdata] Error: code=%s error_code=%s action_hint=%s msg=%s op=%s rid=%s\n",
				ed.code, ed.error_code, ed.action_hint, ed.message, ed.op, ed.request_id)
			// V1 path: use action_hint for deterministic routing when available.
			hint := util.parse_action_hint(ed.action_hint)
			action, meaningful := md_common.action_hint_to_ws_fault(hint)
			if meaningful && hint != .Unspecified {
				sync.lock(&state.mu)
				state.ws_error_action = action
				switch action {
				case .Retry:
					set_transport_state(state, .Backoff)
				case .Resync:
					state.desync = true
					state.desync_reason = .Resync_Required
					set_transport_state(state, .Desync)
				case .Stop:
					state.desync = true
					state.desync_reason = .Protocol_Invalid
					state.reconnect_blocked = true
					set_transport_state(state, .Desync)
				case .Downgrade:
					native_record_legacy_downgrade_attempt(state, "action_hint")
				case .None:
					// No action needed.
				}
				sync.unlock(&state.mu)
			} else {
				// Legacy fallback: heuristic string matching for pre-V1 servers.
				if strings.contains(ed.code, "AUTH") || strings.contains(ed.code, "UNAUTHORIZED") || strings.contains(ed.code, "TOKEN") {
					sync.lock(&state.mu)
					apply_ws_fault(state, .AuthDenied)
					sync.unlock(&state.mu)
				}
				if ed.code == "ERROR_CODE_RESYNC_REQUIRED" {
					sync.lock(&state.mu)
					state.desync = true
					state.desync_reason = .Resync_Required
					set_transport_state(state, .Desync)
					sync.unlock(&state.mu)
				}
			}
		} else {
			fmt.printf("[marketdata] Error frame without code (frame_len=%d)\n", len(raw))
		}
	case .Trade:
		t := result.data.trade
		sync.lock(&state.mu)
		if state.trade_count < TRADE_RING_CAP {
			state.trade_ring_subject_id[state.trade_write] = t.subject_id
			state.trade_ring_seq[state.trade_write] = t.seq
			state.trade_ring[state.trade_write] = ports.MD_Trade_Event{
				price  = t.price,
				qty    = t.qty,
				is_buy = t.is_buy,
				unix   = t.unix,
			}
			state.trade_write = (state.trade_write + 1) % TRADE_RING_CAP
			state.trade_count += 1
		} else {
			state.drop_count += 1
			state.drop_trade_ring += 1
			apply_ws_fault(state, .BackpressureDrop)
		}
		sync.unlock(&state.mu)
	case .Orderbook:
		sync.lock(&state.mu)
		state.ob_staging = result.data.ob
		state.ob_dirty = true
		sync.unlock(&state.mu)
	case .Stats:
		sync.lock(&state.mu)
		state.stats_staging = result.data.stats
		state.stats_dirty = true
		sync.unlock(&state.mu)
	case .Tape:
		sync.lock(&state.mu)
		state.tape_staging = result.data.tape
		state.tape_dirty = true
		sync.unlock(&state.mu)
	case .Heatmap:
		sync.lock(&state.mu)
		state.heatmap_staging = result.data.heatmap
		state.heatmap_dirty = true
		sync.unlock(&state.mu)
	case .VPVR:
		sync.lock(&state.mu)
		state.vpvr_staging = result.data.vpvr
		state.vpvr_dirty = true
		sync.unlock(&state.mu)
	case .Candle:
		sync.lock(&state.mu)
		if state.candle_ring_count < CANDLE_RING_CAP {
			state.candle_ring[state.candle_ring_write] = result.data.candle
			state.candle_ring_write = (state.candle_ring_write + 1) % CANDLE_RING_CAP
			state.candle_ring_count += 1
		} else {
			// Ring full — overwrite oldest.
			state.drop_count += 1
			state.drop_candle_ring += 1
			apply_ws_fault(state, .BackpressureDrop)
			state.candle_ring[state.candle_ring_write] = result.data.candle
			state.candle_ring_write = (state.candle_ring_write + 1) % CANDLE_RING_CAP
		}
		sync.unlock(&state.mu)
	case .Range_Candle:
		sync.lock(&state.mu)
		state.range_candle_staging = result.data.range_candles
		state.range_candle_dirty = true
		sync.unlock(&state.mu)
	case .Evidence:
		sync.lock(&state.mu)
		state.evidence_staging = result.data.evidence
		state.evidence_dirty = true
		sync.unlock(&state.mu)
	case .Signal:
		sync.lock(&state.mu)
		if state.signal_ring_count >= SIGNAL_RING_CAP {
			state.drop_count += 1
			apply_ws_fault(state, .BackpressureDrop)
		}
		state.signal_ring[state.signal_ring_write] = result.data.signal
		state.signal_ring_write = (state.signal_ring_write + 1) % SIGNAL_RING_CAP
		if state.signal_ring_count < SIGNAL_RING_CAP {
			state.signal_ring_count += 1
		}
		sync.unlock(&state.mu)
	// S47: Analytics substrate staging.
	case .Open_Interest:
		sync.lock(&state.mu)
		state.oi_staging = result.data.open_interest
		state.oi_dirty = true
		sync.unlock(&state.mu)
	case .Delta_Volume:
		sync.lock(&state.mu)
		state.delta_vol_staging = result.data.delta_volume
		state.delta_vol_dirty = true
		sync.unlock(&state.mu)
	case .CVD:
		sync.lock(&state.mu)
		state.cvd_staging = result.data.cvd
		state.cvd_dirty = true
		sync.unlock(&state.mu)
	case .Bar_Stats:
		sync.lock(&state.mu)
		state.bar_stats_staging = result.data.bar_stats
		state.bar_stats_dirty = true
		sync.unlock(&state.mu)
	// S49: Session profile events — staging deferred to S49-5 integration.
	case .Session_Volume_Profile, .TPO_Profile:
	case .None:
		// Ignored (last, unknown frame types).
	}
}

@(private = "file")
native_set_candle_tf :: proc(tf: string) {
	state := g_md_state
	if state == nil do return
	new_tf := strings.clone(tf)
	sync.lock(&state.mu)
	if state.candle_tf_filter == tf {
		sync.unlock(&state.mu)
		delete(new_tf)
		return
	}
	old := state.candle_tf_filter
	state.candle_tf_filter = new_tf
	native_resubscribe_timeframe_channels(state)
	sync.unlock(&state.mu)
	delete(old)
}

@(private = "file")
native_send_getrange :: proc(subject: string, limit: int, end_ts: i64) {
	state := g_md_state
	if state == nil do return

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)
	if state.conn_state != .Connected do return

	state.rid_counter += 1
	buf: [512]u8
	msg, ok := md_common.build_getrange_msg(buf[:], subject, limit, end_ts, state.rid_counter)
	if !ok do return
	if !frame_within_limit(state, len(msg)) do return
	ws_write_text(state.conn, msg)
}

// --- HTTP GET for market discovery ---

@(private = "file")
native_fetch_markets :: proc(out_buf: [^]u8, out_cap: i32) -> i32 {
	state := g_md_state
	if state == nil do return 0
	if out_cap <= 0 do return 0

	// Derive HTTP endpoint from WS URL via shared parser + resolver.
	ws_url := state.ws_url
	parsed, ok := ws_parse_url(ws_url)
	if !ok do return 0
	if parsed.scheme == .WSS do return 0  // can't do HTTPS yet

	endpoint, resolve_err := ws_resolve_host(parsed.host, parsed.port)
	if resolve_err != nil do return 0

	conn, dial_err := net.dial_tcp(endpoint)
	if dial_err != nil do return 0
	defer net.close(conn)

	timeout := 3 * time.Second
	_ = net.set_option(conn, .Receive_Timeout, timeout)
	_ = net.set_option(conn, .Send_Timeout, timeout)

	// Send HTTP/1.0 GET (stack buffer — no temp allocator).
	req_buf: [512]u8
	req := fmt.bprintf(req_buf[:],
		"GET /api/v1/markets HTTP/1.0\r\n" +
		"Host: %s\r\n" +
		"Accept: application/json\r\n" +
		"Connection: close\r\n" +
		"\r\n",
		parsed.host_port)

	req_bytes := transmute([]u8)req
	{
		total_sent := 0
		for total_sent < len(req_bytes) {
			sent, send_err := net.send_tcp(conn, req_bytes[total_sent:])
			if send_err != nil do return 0
			if sent <= 0 do return 0
			total_sent += sent
		}
	}

	// Read entire response into a temporary buffer.
	HTTP_BUF_CAP :: 32 * 1024
	resp_buf: [HTTP_BUF_CAP]u8
	total_read := 0
	for total_read < HTTP_BUF_CAP {
		received, recv_err := net.recv_tcp(conn, resp_buf[total_read:])
		if received > 0 do total_read += received
		if recv_err != nil || received <= 0 do break
	}
	if total_read == 0 do return 0

	// Find body after \r\n\r\n header separator.
	resp := string(resp_buf[:total_read])
	body_start := strings.index(resp, "\r\n\r\n")
	if body_start < 0 do return 0
	body := resp[body_start + 4:]
	if len(body) == 0 do return 0

	// Check for HTTP 200.
	if !strings.has_prefix(resp, "HTTP/1.0 200") && !strings.has_prefix(resp, "HTTP/1.1 200") {
		return 0
	}

	copy_len := min(i32(len(body)), out_cap)
	for i in 0 ..< int(copy_len) {
		out_buf[i] = body[i]
	}
	return copy_len
}

// S20: Shared native HTTP GET helper — fires a blocking request to the given path.
@(private = "file")
native_http_get :: proc(path: string, out_buf: [^]u8, out_cap: i32) -> i32 {
	state := g_md_state
	if state == nil || out_cap <= 0 do return 0

	ws_url := state.ws_url
	parsed, ok := ws_parse_url(ws_url)
	if !ok do return 0
	if parsed.scheme == .WSS do return 0  // can't do HTTPS yet

	endpoint, resolve_err := ws_resolve_host(parsed.host, parsed.port)
	if resolve_err != nil do return 0

	conn, dial_err := net.dial_tcp(endpoint)
	if dial_err != nil do return 0
	defer net.close(conn)

	timeout := 3 * time.Second
	_ = net.set_option(conn, .Receive_Timeout, timeout)
	_ = net.set_option(conn, .Send_Timeout, timeout)

	req_buf: [512]u8
	req := fmt.bprintf(req_buf[:],
		"GET %s HTTP/1.0\r\n" +
		"Host: %s\r\n" +
		"Accept: application/json\r\n" +
		"Connection: close\r\n" +
		"\r\n",
		path, parsed.host_port)

	req_bytes := transmute([]u8)req
	{
		total_sent := 0
		for total_sent < len(req_bytes) {
			sent, send_err := net.send_tcp(conn, req_bytes[total_sent:])
			if send_err != nil do return 0
			if sent <= 0 do return 0
			total_sent += sent
		}
	}

	HTTP_BUF_CAP :: 32 * 1024
	resp_buf: [HTTP_BUF_CAP]u8
	total_read := 0
	for total_read < HTTP_BUF_CAP {
		received, recv_err := net.recv_tcp(conn, resp_buf[total_read:])
		if received > 0 do total_read += received
		if recv_err != nil || received <= 0 do break
	}
	if total_read == 0 do return 0

	resp := string(resp_buf[:total_read])
	body_start := strings.index(resp, "\r\n\r\n")
	if body_start < 0 do return 0
	body := resp[body_start + 4:]
	if len(body) == 0 do return 0
	if !strings.has_prefix(resp, "HTTP/1.0 200") && !strings.has_prefix(resp, "HTTP/1.1 200") {
		return 0
	}

	copy_len := min(i32(len(body)), out_cap)
	for i in 0 ..< int(copy_len) {
		out_buf[i] = body[i]
	}
	return copy_len
}

@(private = "file")
native_fetch_session :: proc(out_buf: [^]u8, out_cap: i32) -> i32 {
	return native_http_get("/api/v1/session", out_buf, out_cap)
}

@(private = "file")
native_fetch_freshness :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 do return 0
	path_buf: [256]u8
	path := fmt.bprintf(path_buf[:], "/api/v1/freshness?venue=%s&instrument=%s", venue, instrument)
	return native_http_get(path, out_buf, out_cap)
}

@(private = "file")
native_fetch_timeline :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string, timeframe: string) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 || len(timeframe) == 0 do return 0
	path_buf: [256]u8
	path := fmt.bprintf(path_buf[:], "/api/v1/timeline?venue=%s&instrument=%s&timeframe=%s&artifact=candle", venue, instrument, timeframe)
	return native_http_get(path, out_buf, out_cap)
}

@(private = "file")
native_fetch_instrument_overview :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 do return 0
	path_buf: [256]u8
	path := fmt.bprintf(path_buf[:], "/api/v1/instrument/overview?venue=%s&instrument=%s", venue, instrument)
	return native_http_get(path, out_buf, out_cap)
}

@(private = "file")
native_fetch_session_dashboard :: proc(out_buf: [^]u8, out_cap: i32) -> i32 {
	return native_http_get("/api/v1/session/dashboard", out_buf, out_cap)
}

// S83: Analytics cold reader port implementations.

@(private = "file")
native_fetch_analytics_cvd :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string, timeframe: string, limit: i32) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 || len(timeframe) == 0 do return 0
	path_buf: [512]u8
	path := fmt.bprintf(path_buf[:], "/api/v1/cvd?venue=%s&instrument=%s&timeframe=%s&fromMs=0&toMs=9999999999999&limit=%d", venue, instrument, timeframe, limit)
	return native_http_get(path, out_buf, out_cap)
}

@(private = "file")
native_fetch_analytics_delta_volume :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string, timeframe: string, limit: i32) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 || len(timeframe) == 0 do return 0
	path_buf: [512]u8
	path := fmt.bprintf(path_buf[:], "/api/v1/delta_volume?venue=%s&instrument=%s&timeframe=%s&fromMs=0&toMs=9999999999999&limit=%d", venue, instrument, timeframe, limit)
	return native_http_get(path, out_buf, out_cap)
}

@(private = "file")
native_fetch_analytics_bar_stats :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string, timeframe: string, limit: i32) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 || len(timeframe) == 0 do return 0
	path_buf: [512]u8
	path := fmt.bprintf(path_buf[:], "/api/v1/bar_stats?venue=%s&instrument=%s&timeframe=%s&fromMs=0&toMs=9999999999999&limit=%d", venue, instrument, timeframe, limit)
	return native_http_get(path, out_buf, out_cap)
}

@(private = "file")
native_fetch_analytics_oi :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string, timeframe: string, limit: i32) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 || len(timeframe) == 0 do return 0
	path_buf: [512]u8
	path := fmt.bprintf(path_buf[:], "/api/v1/oi?venue=%s&instrument=%s&timeframe=%s&fromMs=0&toMs=9999999999999&limit=%d", venue, instrument, timeframe, limit)
	return native_http_get(path, out_buf, out_cap)
}

@(private = "file")
native_fetch_session_volume_profile :: proc(out_buf: [^]u8, out_cap: i32, venue: string, instrument: string, anchor: string) -> i32 {
	if len(venue) == 0 || len(instrument) == 0 do return 0
	path_buf: [512]u8
	a := anchor if len(anchor) > 0 else "current"
	path := fmt.bprintf(path_buf[:], "/api/v1/insights/session-vp?venue=%s&instrument=%s&anchor=%s", venue, instrument, a)
	return native_http_get(path, out_buf, out_cap)
}
