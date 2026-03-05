package main

// WASM marketdata port — JS WebSocket bridge, single-threaded.
// Polls messages from a JS-side queue via ws_poll_msg foreign proc.
// Same staging pattern as native (ring + latest-wins), but no mutex needed.

import "core:fmt"
import "core:strings"
import "core:time"
import "mr:md_common"
import "mr:ports"
import "mr:services"
import "mr:util"

foreign import odin_env "odin_env"

@(default_calling_convention = "contextless")
foreign odin_env {
	ws_connect  :: proc(url_ptr: [^]u8, url_len: i32, hdr_ptr: [^]u8, hdr_len: i32) ---
	ws_send     :: proc(ptr: [^]u8, len: i32) ---
	ws_close    :: proc() ---
	ws_state    :: proc() -> i32 ---
	ws_drop_count :: proc() -> u32 ---
	ws_allow_legacy_ws :: proc() -> i32 ---
	ws_poll_msg :: proc(buf_ptr: [^]u8, buf_len: i32) -> i32 ---
	key_state   :: proc() -> u32 ---
	key_pressed_state :: proc() -> u32 ---
	key_released_state :: proc() -> u32 ---
	mouse_x     :: proc() -> f32 ---
	mouse_y     :: proc() -> f32 ---
	mouse_buttons :: proc() -> u32 ---
	mouse_pressed_buttons :: proc() -> u32 ---
	mouse_released_buttons :: proc() -> u32 ---
	mouse_scroll_x :: proc() -> f32 ---
	mouse_scroll_y :: proc() -> f32 ---
	modifier_state :: proc() -> u32 ---
	http_get_sync   :: proc(url_ptr: [^]u8, url_len: i32, out_ptr: [^]u8, out_cap: i32) -> i32 ---
}

// --- Constants ---

WEB_TRADE_RING_CAP   :: 1024
WEB_CANDLE_RING_CAP  :: 32
WEB_SIGNAL_RING_CAP  :: 64
WEB_MAX_SUBS         :: 128
WEB_RECV_BUF_SIZE    :: 128 * 1024 // 128 KB per message max
WEB_PARSE_MAX_MSGS_PER_POLL :: 64
WEB_PARSE_TIME_BUDGET       :: 2 * time.Millisecond

// Reconnection backoff.
WEB_BACKOFF_INITIAL_S :: 0.5
WEB_BACKOFF_MAX_S     :: 30.0
WEB_BACKOFF_MULTIPLIER :: 2.0
WEB_HELLO_TIMEOUT_MS  :: 10_000
WEB_PONG_TIMEOUT_MS   :: 15_000
WEB_METRICS_STALE_MS  :: 20_000
WEB_FREQUENT_DROP_THRESHOLD :: 16
WEB_PERF_SAMPLE_CAP   :: 120

// Default candle timeframe filter.
CANDLE_TF_DEFAULT :: "1m"

// Subscription/metrics limit helpers delegated to md_common.
// (web_effective_sub_limit, web_can_add_subscription, web_metrics_stale_timeout_ms
//  removed — use md_common.effective_sub_limit / can_add_subscription / metrics_stale_timeout_ms)

// --- State ---

Web_Sub_Entry :: struct {
	subject_id:     u64,
	venue:          string,
	symbol:         string,
	channel:        ports.MD_Channel,
	subject:        string,
	stream_id:      [128]u8,
	stream_id_len:  u8,
	is_explicit_tf: bool, // true = per-cell TF sub; skip in global TF resubscribe
}

MD_Web_State :: struct {
	// Trade ring buffer.
	trade_ring:            [WEB_TRADE_RING_CAP]ports.MD_Trade_Event,
	trade_ring_subject_id: [WEB_TRADE_RING_CAP]u64,
	trade_ring_seq:        [WEB_TRADE_RING_CAP]i64,
	trade_write:           int,
	trade_count:           int,

	// Latest-wins staging.
	ob_staging:      services.Parsed_OB,
	ob_dirty:        bool,
	stats_staging:   services.Parsed_Stats,
	stats_dirty:     bool,
	tape_staging:    services.Parsed_Tape,
	tape_dirty:      bool,
	heatmap_staging: services.Parsed_Heatmap,
	heatmap_dirty:   bool,
	vpvr_staging:    services.Parsed_VPVR,
	vpvr_dirty:      bool,
	candle_ring:       [WEB_CANDLE_RING_CAP]services.Parsed_Candle,
	candle_ring_write: int,
	candle_ring_count: int,

	// Range candle staging (getrange response batch).
	range_candle_staging: services.Parsed_Range_Candles,
	range_candle_dirty:   bool,
	evidence_staging:     services.Parsed_Evidence,
	evidence_dirty:       bool,
	signal_ring:          [WEB_SIGNAL_RING_CAP]services.Parsed_Signal,
	signal_ring_write:    int,
	signal_ring_count:    int,

	// Candle timeframe filter (mutable, heap-allocated).
	candle_tf_filter: string,

	// Connection.
	ws_url:    string,
	api_key:   string,
	jwt_token: string,
	reconnect_blocked: bool,
	// Terminal_V1 transport state.
	transport_state: ports.MD_Transport_State,
	transport_mode: util.Transport_Mode,
	auth_mode:      u8, // 0=none, 1=apikey, 2=jwt
	allow_legacy_ws: bool,
	canonical_evidence_subject: bool,
	canonical_signal_subject:   bool,
	accept_legacy_evidence:     bool,
	accept_legacy_signal:       bool,
	ws_error_category: ports.MD_WS_Error_Category,
	ws_error_action:   ports.MD_WS_Error_Action,
	hello_timeout_count: int,
	pong_rtt_ms: i64,
	last_ping_sent_ms: i64,
	// Server-pushed metrics (from METRICS frame).
	server_metrics: services.Parsed_Metrics,
	server_metrics_received: bool,
	// Server capabilities (from HELLO) — consolidated struct.
	caps: md_common.Server_Capabilities,
	// Negotiated features (from Hello ACK).
	negotiated_features: [services.MAX_FEATURE_SLOTS]services.Parsed_Feature_Slot,
	negotiated_feature_count: int,

	// Subscription tracking for reconnect re-subscribe.
	active_subs:  [WEB_MAX_SUBS]Web_Sub_Entry,
	active_count: int,
	pending_getrange_subject: string,
	pending_getrange_limit:   int,
	pending_getrange_end_ts:  i64,
	pending_getrange_queued:  bool,
	rid_counter:       u32,
	drop_count:        int,
	drop_trade_ring:   int,
	drop_candle_ring:  int,
	drop_ws_queue:     int,
	drop_payload_oversize: int,
	reconnect_count:   int,
	seq_gap_count:     int,
	resync_count:      int,
	seq_gap_streak:    int,
	parse_arena:       services.Parse_Arena,
	parse_error_count: int,
	subscribe_ack_count: int,
	rates: md_common.Rate_State,
	last_msg_ts_ms:     i64,
	last_server_ts_ms:  i64,
	last_rtt_ms:        i64,
	last_lag_ms:        i64,
	last_metrics_ts_ms: i64,
	last_pong_ts_ms:    i64,
	protocol_version:   int,
	hello_received:     bool,
	hello_valid:        bool,
	desync:             bool,
	desync_reason:      ports.MD_Desync_Reason,
	backend_gap_no_metrics: int,
	backend_gap_pong_timeout: int,
	backend_gap_resync_ack_timeout: int,
	backend_gap_missing_ts_server: int,
	backend_gap_seq_gap_recurring: int,
	backend_gap_frequent_drops: int,
	connect_started_ms: i64,
	first_data_logged:  bool,
	last_seq_by_sub:    [WEB_MAX_SUBS]i64,
	last_snapshot_seq_by_sub: [WEB_MAX_SUBS]i64,
	last_server_ts_by_sub: [WEB_MAX_SUBS]i64,
	snapshot_logged_by_sub: [WEB_MAX_SUBS]bool,
	// Integrity counters.
	snapshot_hash_mismatches: int,
	snapshot_seq_violations:  int,
	prev_seq_violations:      int,
	hash_validation_skipped:  int,
	// Legacy tracking.
	legacy_downgrade_count:    int,
	legacy_connected_since_ms: i64,
	desync_resub_subject_id: u64, // 0 = none pending
	// RESYNC tracking (Terminal_V1): pending resync for a subject.
	resync_pending_subject_id: u64,  // 0 = none pending
	resync_sent_ms: i64,             // timestamp when RESYNC was sent

	// Receive buffer (reused each poll).
	recv_buf: [WEB_RECV_BUF_SIZE]u8,

	// Temp arrays for poll output (avoids aliasing staging).
	poll_ask_prices:  [services.OB_STAGING_DEPTH]f64,
	poll_ask_sizes:   [services.OB_STAGING_DEPTH]f64,
	poll_bid_prices:  [services.OB_STAGING_DEPTH]f64,
	poll_bid_sizes:   [services.OB_STAGING_DEPTH]f64,
	poll_hm_prices:   [services.HEATMAP_STAGING_CAP]f64,
	poll_hm_sizes:    [services.HEATMAP_STAGING_CAP]f64,
	poll_vpvr_prices: [services.VPVR_STAGING_CAP]f64,
	poll_vpvr_buys:   [services.VPVR_STAGING_CAP]f64,
	poll_vpvr_sells:  [services.VPVR_STAGING_CAP]f64,

	// Reconnection tracking.
	was_connected:   bool,
	reconnect_timer: f64, // seconds until next reconnect attempt
	backoff_s:       f64, // current backoff in seconds
	jitter_seed:     u32,
	last_poll_tick:     time.Tick,
	has_last_poll_tick: bool,

	// Parse budget tuning defaults.
	parse_max_msgs_per_poll: int,
	parse_time_budget:       time.Duration,

	// Optional perf telemetry (sampled logging).
	perf_debug:             bool,
	perf_polls_total:       u64,
	perf_drained_total:     u64,
	perf_budget_hit_total:  u64,
	perf_msg_hit_total:     u64,
	perf_time_hit_total:    u64,
	perf_max_drained:       int,
	perf_last_log_tick:     time.Tick,
	perf_has_last_log_tick: bool,
	parse_samples_us:       [WEB_PERF_SAMPLE_CAP]i64,
	parse_sample_head:      int,
	parse_sample_count:     int,
	apply_samples_us:       [WEB_PERF_SAMPLE_CAP]i64,
	apply_sample_head:      int,
	apply_sample_count:     int,
	batch_decode_samples_us: [WEB_PERF_SAMPLE_CAP]i64,
	batch_decode_sample_head: int,
	batch_decode_sample_count: int,
	batched_frames_received: u64,
	batched_events_received: u64,
	batched_fastpath_events: u64,
	batched_fallback_events: u64,
	canonical_stats_frames:   u64,
	stats_fallback_frames:    u64,
	canonical_evidence_frames: u64,
	legacy_evidence_frames:    u64,
	evidence_fallback_frames:  u64,
	canonical_signal_frames:   u64,
	legacy_signal_frames:      u64,
	signal_fallback_frames:    u64,
	legacy_evidence_rejected:  u64,
	legacy_signal_rejected:    u64,
}

// File-private singleton: Odin procs are bare function pointers (no closures),
// so Marketdata_Port callbacks access state through this global. Only one
// instance exists per process; @(private="file") prevents external access.
@(private = "file")
g_web_state: ^MD_Web_State

@(private = "file")
read_allow_legacy_ws_web :: proc() -> bool {
	if v, ok := web_settings_lookup(services.SETTING_ALLOW_LEGACY_WS); ok {
		return md_common.legacy_switch_from_text(v)
	}
	v := ws_allow_legacy_ws()
	if v == 0 do return false
	if v > 0 do return true
	return md_common.ALLOW_LEGACY_WS_DEFAULT
}

@(private = "file")
read_bool_web_setting :: proc(key: string, default_value: bool) -> bool {
	if v, ok := web_settings_lookup(key); ok {
		return md_common.legacy_switch_from_text(v)
	}
	return default_value
}

@(private = "file")
web_log_safe_url :: proc(url: string) -> string {
	return md_common.sanitize_url_for_log(url)
}

@(private = "file")
set_web_transport_state :: proc(state: ^MD_Web_State, next: ports.MD_Transport_State) {
	if state == nil do return
	state.transport_state = next
}

@(private = "file")
web_record_perf_sample :: proc(samples: ^[WEB_PERF_SAMPLE_CAP]i64, head: ^int, count: ^int, v: i64) {
	if samples == nil || head == nil || count == nil do return
	samples[head^] = max(v, 0)
	head^ = (head^ + 1) % WEB_PERF_SAMPLE_CAP
	if count^ < WEB_PERF_SAMPLE_CAP do count^ += 1
}

@(private = "file")
web_sample_p95_us :: proc(samples: [WEB_PERF_SAMPLE_CAP]i64, head: int, count: int) -> i64 {
	n := count
	if n <= 0 do return 0
	if n > WEB_PERF_SAMPLE_CAP do n = WEB_PERF_SAMPLE_CAP
	start := (head - n + WEB_PERF_SAMPLE_CAP) % WEB_PERF_SAMPLE_CAP
	sorted: [WEB_PERF_SAMPLE_CAP]i64
	for i in 0 ..< n {
		sorted[i] = samples[(start + i) % WEB_PERF_SAMPLE_CAP]
	}
	for i in 1 ..< n {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > key {
			sorted[j + 1] = sorted[j]
			j -= 1
		}
		sorted[j + 1] = key
	}
	return sorted[min((n * 95) / 100, n - 1)]
}

@(private = "file")
apply_web_fault :: proc(state: ^MD_Web_State, category: ports.MD_WS_Error_Category) {
	if state == nil do return
	action := md_common.ws_fault_action(category, state.allow_legacy_ws)
	state.ws_error_category = category
	state.ws_error_action = action
	switch action {
	case .Retry:
		set_web_transport_state(state, .Backoff)
	case .Downgrade:
		state.legacy_downgrade_count += 1
		state.legacy_connected_since_ms = time.now()._nsec / 1_000_000
		fmt.printf("[md-lifecycle] WARN legacy_downgrade count=%d allow=%v\n", state.legacy_downgrade_count, state.allow_legacy_ws)
		state.transport_mode = .Legacy_JSON
		state.hello_timeout_count += 1
		state.hello_received = true
		state.hello_valid = true
		state.desync = false
		state.desync_reason = .None
		set_web_transport_state(state, .Running)
	case .Resync:
		state.desync = true
		if state.desync_reason == .None do state.desync_reason = .Sequence_Gap
		set_web_transport_state(state, .Desync)
	case .Stop:
		state.reconnect_blocked = true
		state.desync = true
		state.desync_reason = .Protocol_Invalid
		set_web_transport_state(state, .Desync)
	case .None:
	}
}

// Feature lookup — delegates to md_common.feature_slot_has_name.
@(private = "file")
web_server_has_feature :: proc(state: ^MD_Web_State, name: string) -> bool {
	return md_common.feature_slot_has_name(state.caps.supported_features, state.caps.supported_feature_count, name)
}

@(private = "file")
web_negotiated_has_feature :: proc(state: ^MD_Web_State, name: string) -> bool {
	return md_common.feature_slot_has_name(state.negotiated_features, state.negotiated_feature_count, name)
}

@(private = "file")
web_feature_setting_value :: proc(key: string) -> string {
	if v, ok := web_settings_lookup(key); ok && len(v) > 0 {
		return v
	}
	return "auto"
}

@(private = "file")
web_supports_decompress :: proc() -> bool {
	// Browser bridge currently drains UTF-8 text frames only.
	return false
}

@(private = "file")
web_frame_within_limit :: proc(state: ^MD_Web_State, frame_len: int) -> bool {
	if state == nil do return false
	if frame_len < 0 do return false
	if md_common.frame_exceeds_limit(state.caps.max_frame_bytes, frame_len) {
		state.drop_payload_oversize += 1
		state.drop_count += 1
		fmt.printf("[md-lifecycle] frame_rejected max_frame_bytes=%d len=%d\n", state.caps.max_frame_bytes, frame_len)
		apply_web_fault(state, .BackpressureDrop)
		return false
	}
	return true
}

// --- Public API ---

make_marketdata_web :: proc(url: string, api_key: string = "", connect: bool = true) -> ports.Marketdata_Port {
	if g_web_state != nil {
		web_shutdown()
	}
	state := new(MD_Web_State)
	state.ws_url = strings.clone(url)
	state.api_key = strings.clone(api_key)
	state.candle_tf_filter = strings.clone(CANDLE_TF_DEFAULT)
	state.parse_max_msgs_per_poll = WEB_PARSE_MAX_MSGS_PER_POLL
	state.parse_time_budget = WEB_PARSE_TIME_BUDGET
	state.perf_debug = false
	state.jitter_seed = u32(time.now()._nsec & 0xFFFFFFFF)
	state.transport_mode = .Terminal_V1
	state.allow_legacy_ws = read_allow_legacy_ws_web()
	state.canonical_evidence_subject = read_bool_web_setting(services.SETTING_CANONICAL_EVIDENCE_SUBJECT, true)
	state.canonical_signal_subject = read_bool_web_setting(services.SETTING_CANONICAL_SIGNAL_SUBJECT, true)
	state.accept_legacy_evidence = read_bool_web_setting(services.SETTING_ACCEPT_LEGACY_EVIDENCE, true)
	state.accept_legacy_signal = read_bool_web_setting(services.SETTING_ACCEPT_LEGACY_SIGNAL, true)
	g_web_state = state

	if connect {
		// Eager connect: initiate WS immediately.
		set_web_transport_state(state, .Hello_Pending)
		hdr_buf: [256]u8
		hdr_len := web_build_auth_header(state, hdr_buf[:])
		url_raw := raw_data(transmute([]u8)url)
		state.connect_started_ms = time.now()._nsec / 1_000_000
		state.first_data_logged = false
		fmt.printf("[md-lifecycle] connect requested_url=%s\n", web_log_safe_url(url))
		ws_connect(url_raw, i32(len(url)), raw_data(hdr_buf[:hdr_len]), i32(hdr_len))
	} else {
		// Deferred connect: stay OFFLINE until explicit reconnect_transport call.
		set_web_transport_state(state, .Backoff)
		state.reconnect_blocked = true
	}

	return ports.Marketdata_Port{
		subscribe       = web_subscribe,
		subscribe_tf    = web_subscribe_tf,
		unsubscribe     = web_unsubscribe,
		poll            = web_poll,
		now_ms          = web_now_ms,
		conn_status     = web_conn_status,
		metrics         = web_metrics,
		describe_stream = web_describe_stream,
		set_candle_tf   = web_set_candle_tf,
		send_getrange   = web_send_getrange,
		reconnect_transport = web_reconnect_transport,
		disconnect_transport = web_disconnect_transport,
		shutdown        = web_shutdown,
		fetch_markets   = web_fetch_markets,
	}
}

// --- Port implementation ---

@(private = "file")
find_web_sub_by_subject :: proc(state: ^MD_Web_State, subject: string) -> int {
	return md_common.find_sub_by_subject(state.active_subs[:state.active_count], subject)
}

@(private = "file")
find_web_sub_by_key :: proc(state: ^MD_Web_State, venue: string, symbol: string, channel: ports.MD_Channel) -> int {
	return md_common.find_sub_by_key(state.active_subs[:state.active_count], venue, symbol, channel)
}

@(private = "file")
find_web_sub_by_subject_id :: proc(state: ^MD_Web_State, subject_id: u64) -> int {
	return md_common.find_sub_by_subject_id(state.active_subs[:state.active_count], subject_id)
}

@(private = "file")
web_subject_for_channel :: proc(state: ^MD_Web_State, venue: string, symbol: string, channel: ports.MD_Channel) -> string {
	if state != nil {
		#partial switch channel {
		case .Evidence:
			stream_type := "liquidity.evidence" if state.canonical_evidence_subject else "insights.microstructure_evidence"
			return util.build_subject_from_stream_type(venue, symbol, stream_type, "raw")
		case .Signals:
			stream_type := "signal" if state.canonical_signal_subject else "signal/composite"
			tf := state.candle_tf_filter
			if len(tf) == 0 do tf = "1m"
			return util.build_subject_from_stream_type(venue, symbol, stream_type, tf)
		}
	}
	return md_common.subject_for_channel(venue, symbol, state.candle_tf_filter, channel)
}

@(private = "file")
web_free_sub_entry :: proc(entry: ^Web_Sub_Entry) {
	md_common.free_sub_entry(entry)
}

@(private = "file")
web_shutdown :: proc() {
	state := g_web_state
	if state == nil do return
	ws_close()
	for i in 0 ..< state.active_count {
		web_free_sub_entry(&state.active_subs[i])
		state.last_seq_by_sub[i] = 0
		state.last_snapshot_seq_by_sub[i] = 0
		state.last_server_ts_by_sub[i] = 0
		state.snapshot_logged_by_sub[i] = false
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
	if len(state.pending_getrange_subject) > 0 {
		delete(state.pending_getrange_subject)
		state.pending_getrange_subject = ""
	}
	state.pending_getrange_limit = 0
	state.pending_getrange_end_ts = 0
	state.pending_getrange_queued = false
	state.was_connected = false
	state.reconnect_timer = 0
	g_web_state = nil
}

@(private = "file")
web_disconnect_transport :: proc() -> bool {
	state := g_web_state
	if state == nil do return false
	fmt.println("[md-lifecycle] disconnect requested=manual")
	ws_close()
	// Manual disconnect should stay offline until explicit reconnect is requested.
	state.reconnect_blocked = true
	state.reconnect_timer = 0
	state.backoff_s = WEB_BACKOFF_INITIAL_S
	state.was_connected = false
	state.desync = false
	state.desync_reason = .None
	state.resync_pending_subject_id = 0
	state.resync_sent_ms = 0
	state.protocol_version = 0
	state.hello_received = false
	state.hello_valid = false
	state.last_metrics_ts_ms = 0
	state.last_pong_ts_ms = 0
	set_web_transport_state(state, .Backoff)
	state.connect_started_ms = 0
	state.first_data_logged = false
	// Clear pending getrange — stale after manual disconnect.
	if len(state.pending_getrange_subject) > 0 {
		delete(state.pending_getrange_subject)
		state.pending_getrange_subject = ""
	}
	state.pending_getrange_queued = false
	// Reset caps so next HELLO repopulates cleanly.
	state.caps.received = false
	fmt.println("[md-lifecycle] disconnect manual_offline=true")
	return true
}

@(private = "file")
web_reconnect_transport :: proc(ws_url: string, api_key: string, jwt_token: string = "") -> bool {
	state := g_web_state
	if state == nil do return false
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
	// Derive auth mode.
	if len(jwt_token) > 0 {
		state.auth_mode = 2
	} else if len(api_key) > 0 {
		state.auth_mode = 1
	} else {
		state.auth_mode = 0
	}
	ws_close()
	state.was_connected = false
	state.reconnect_timer = 0
	state.reconnect_blocked = false
	state.desync = false
	state.desync_reason = .None
	state.resync_pending_subject_id = 0
	state.resync_sent_ms = 0
	state.protocol_version = 0
	state.hello_received = false
	state.hello_valid = false
	state.transport_mode = .Terminal_V1
	state.allow_legacy_ws = read_allow_legacy_ws_web()
	state.canonical_evidence_subject = read_bool_web_setting(services.SETTING_CANONICAL_EVIDENCE_SUBJECT, true)
	state.canonical_signal_subject = read_bool_web_setting(services.SETTING_CANONICAL_SIGNAL_SUBJECT, true)
	state.accept_legacy_evidence = read_bool_web_setting(services.SETTING_ACCEPT_LEGACY_EVIDENCE, true)
	state.accept_legacy_signal = read_bool_web_setting(services.SETTING_ACCEPT_LEGACY_SIGNAL, true)
	state.ws_error_category = .None
	state.ws_error_action = .None
	set_web_transport_state(state, .Backoff)
	state.server_metrics_received = false
	state.caps.received = false
	state.caps.supported_feature_count = 0
	state.negotiated_feature_count = 0
	state.last_metrics_ts_ms = 0
	state.last_pong_ts_ms = 0
	state.connect_started_ms = 0
	state.first_data_logged = false
	fmt.printf("[md-lifecycle] reconnect_requested url=%s\n", web_log_safe_url(state.ws_url))
	return true
}

@(private = "file")
web_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_web_state
	if state == nil do return false

	subject := web_subject_for_channel(state, venue, symbol, channel)
	if len(subject) == 0 do return false
	subject_id := util.subject_id64(subject)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_web_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	sub_limit := md_common.effective_sub_limit(state.caps.max_subscriptions, WEB_MAX_SUBS)
	if !md_common.can_add_subscription(state.active_count, state.caps.max_subscriptions, WEB_MAX_SUBS) {
		fmt.printf("[md-lifecycle] subscribe_rejected limit=%d active=%d\n", sub_limit, state.active_count)
		delete(subject)
		return false
	}

	// Track for reconnect.
	state.active_subs[state.active_count] = Web_Sub_Entry{
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

	if ws_state() != 2 do return false // tracked; reconnect path will subscribe later
	return web_send_subscribe(state, subject)
}

// Subscribe with an explicit timeframe (for per-cell TF support).
@(private = "file")
web_subscribe_tf :: proc(venue: string, symbol: string, channel: ports.MD_Channel, tf: string) -> bool {
	state := g_web_state
	if state == nil do return false

	subject := ""
	#partial switch channel {
	case .Evidence:
		stream_type := "liquidity.evidence" if state.canonical_evidence_subject else "insights.microstructure_evidence"
		subject = util.build_subject_from_stream_type(venue, symbol, stream_type, "raw")
	case .Signals:
		stream_type := "signal" if state.canonical_signal_subject else "signal/composite"
		tf_eff := tf
		if len(tf_eff) == 0 do tf_eff = "1m"
		subject = util.build_subject_from_stream_type(venue, symbol, stream_type, tf_eff)
	}
	if len(subject) == 0 {
		subject = util.build_subject_with_timeframe(venue, symbol, channel, tf)
	}
	if len(subject) == 0 do return false
	subject_id := util.subject_id64(subject)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_web_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	sub_limit := md_common.effective_sub_limit(state.caps.max_subscriptions, WEB_MAX_SUBS)
	if !md_common.can_add_subscription(state.active_count, state.caps.max_subscriptions, WEB_MAX_SUBS) {
		fmt.printf("[md-lifecycle] subscribe_tf_rejected limit=%d active=%d\n", sub_limit, state.active_count)
		delete(subject)
		return false
	}

	state.active_subs[state.active_count] = Web_Sub_Entry{
		subject_id     = subject_id,
		venue          = strings.clone(venue),
		symbol         = strings.clone(symbol),
		channel        = channel,
		subject        = subject,
		is_explicit_tf = true,
	}
	state.last_seq_by_sub[state.active_count] = 0
	state.last_snapshot_seq_by_sub[state.active_count] = 0
	state.last_server_ts_by_sub[state.active_count] = 0
	state.snapshot_logged_by_sub[state.active_count] = false
	state.active_count += 1

	if ws_state() != 2 do return false // tracked; reconnect path will subscribe later
	return web_send_subscribe(state, subject)
}

@(private = "file")
web_describe_stream :: proc(subject_id: u64, out: ^ports.MD_Stream_Info) -> bool {
	state := g_web_state
	if state == nil || out == nil do return false
	if subject_id == 0 do return false

	idx := find_web_sub_by_subject_id(state, subject_id)
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
web_metrics :: proc(out: ^ports.MD_Runtime_Metrics) -> bool {
	state := g_web_state
	if state == nil || out == nil do return false
	queue_drop_count := int(ws_drop_count())
	latest_pending := 0
	if state.ob_dirty do latest_pending += 1
	if state.stats_dirty do latest_pending += 1
	if state.tape_dirty do latest_pending += 1
	if state.heatmap_dirty do latest_pending += 1
	if state.vpvr_dirty do latest_pending += 1
	if state.candle_ring_count > 0 do latest_pending += 1
	if state.signal_ring_count > 0 do latest_pending += 1
	sm := state.server_metrics
	out^ = ports.MD_Runtime_Metrics{
		active_subs       = state.active_count,
		trade_backlog     = state.trade_count,
		candle_backlog    = state.candle_ring_count,
		// Include JS bridge queue drops so metrics reflect end-to-end pressure.
		drop_count        = state.drop_count + queue_drop_count,
		drop_trade_ring   = state.drop_trade_ring,
		drop_candle_ring  = state.drop_candle_ring,
		drop_ws_queue     = state.drop_ws_queue + queue_drop_count,
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
		parse_time_p95_us = web_sample_p95_us(state.parse_samples_us, state.parse_sample_head, state.parse_sample_count),
		apply_time_p95_us = web_sample_p95_us(state.apply_samples_us, state.apply_sample_head, state.apply_sample_count),
		batched_decode_time_p95_us = web_sample_p95_us(state.batch_decode_samples_us, state.batch_decode_sample_head, state.batch_decode_sample_count),
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
			batched_fastpath_events      = state.batched_fastpath_events,
			batched_fallback_events      = state.batched_fallback_events,
			canonical_stats_frames       = state.canonical_stats_frames,
			stats_fallback_frames        = state.stats_fallback_frames,
			canonical_evidence_frames    = state.canonical_evidence_frames,
			legacy_evidence_frames       = state.legacy_evidence_frames,
		evidence_fallback_frames     = state.evidence_fallback_frames,
		canonical_signal_frames      = state.canonical_signal_frames,
		legacy_signal_frames         = state.legacy_signal_frames,
		signal_fallback_frames       = state.signal_fallback_frames,
		legacy_evidence_rejected     = state.legacy_evidence_rejected,
		legacy_signal_rejected       = state.legacy_signal_rejected,
		// Integrity counters.
		snapshot_hash_mismatches     = state.snapshot_hash_mismatches,
		snapshot_seq_violations      = state.snapshot_seq_violations,
		prev_seq_violations          = state.prev_seq_violations,
		hash_validation_skipped      = state.hash_validation_skipped,
		// Legacy tracking.
		legacy_downgrade_count       = state.legacy_downgrade_count,
		legacy_connected_since_ms    = state.legacy_connected_since_ms,
	}
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
	return true
}

@(private = "file")
web_unsubscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) {
	state := g_web_state
	if state == nil do return

	subject := ""
	idx := find_web_sub_by_key(state, venue, symbol, channel)
	if idx >= 0 {
		subject = strings.clone(state.active_subs[idx].subject)
	} else {
		derived_subject := web_subject_for_channel(state, venue, symbol, channel)
		if len(derived_subject) == 0 do return
		// subscribe() deduplicates by subject (multiple channels can collapse into one
		// subject). For unsubscribe(), only send if we still track that subject.
		idx = find_web_sub_by_subject(state, derived_subject)
		if idx < 0 {
			delete(derived_subject)
			return
		}
		subject = strings.clone(state.active_subs[idx].subject)
		delete(derived_subject)
	}
	defer delete(subject)

	// Remove from active subs.
	if idx >= 0 {
		last := state.active_count - 1
		web_free_sub_entry(&state.active_subs[idx])
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

	if ws_state() != 2 do return
	web_send_unsubscribe(state, subject)
}

@(private = "file")
web_send_unsubscribe :: proc(state: ^MD_Web_State, subject: string) -> bool {
	state.rid_counter += 1
	buf: [512]u8
	msg: string
	ok: bool
	// In Terminal_V1: prefer stream_id from ACK when available.
	if state.transport_mode == .Terminal_V1 {
		sid := util.subject_id64(subject)
		if si := find_web_sub_by_subject_id(state, sid); si >= 0 && state.active_subs[si].stream_id_len > 0 {
			stored_sid := string(state.active_subs[si].stream_id[:int(state.active_subs[si].stream_id_len)])
			msg, ok = md_common.build_unsubscribe_msg_v2(buf[:], stored_sid, state.rid_counter)
		}
	}
	if !ok {
		msg, ok = md_common.build_unsubscribe_msg(buf[:], subject, state.rid_counter)
	}
	if !ok do return false
	if !web_frame_within_limit(state, len(msg)) do return false
	ws_send(raw_data(buf[:len(msg)]), i32(len(msg)))
	fmt.printf("[md-lifecycle] unsubscribe_sent subject=%s rid=r%d\n", subject, state.rid_counter)
	return true
}

@(private = "file")
web_send_subscribe :: proc(state: ^MD_Web_State, subject: string) -> bool {
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
	if !web_frame_within_limit(state, len(msg)) do return false
	ws_send(raw_data(buf[:len(msg)]), i32(len(msg)))
	fmt.printf("[md-lifecycle] subscribe_sent subject=%s rid=r%d\n", subject, state.rid_counter)
	return true
}

@(private = "file")
web_resubscribe_timeframe_channels :: proc(state: ^MD_Web_State) {
	if state == nil do return
	is_open := ws_state() == 2

	for i in 0 ..< state.active_count {
		entry := &state.active_subs[i]
		if entry.channel != .Heatmaps && entry.channel != .VPVR && entry.channel != .Candles && entry.channel != .Signals && entry.channel != .Tape do continue
		if entry.is_explicit_tf do continue // per-cell TF sub — don't clobber

		new_subject := ""
		#partial switch entry.channel {
		case .Signals:
			stream_type := "signal" if state.canonical_signal_subject else "signal/composite"
			tf := state.candle_tf_filter
			if len(tf) == 0 do tf = "1m"
			new_subject = util.build_subject_from_stream_type(entry.venue, entry.symbol, stream_type, tf)
		}
		if len(new_subject) == 0 {
			new_subject = util.build_subject_with_timeframe(entry.venue, entry.symbol, entry.channel, state.candle_tf_filter)
		}
		if new_subject == entry.subject {
			delete(new_subject)
			continue
		}

		if is_open {
			web_send_unsubscribe(state, entry.subject)
			web_send_subscribe(state, new_subject)
		}
		delete(entry.subject)
		entry.subject = new_subject
		entry.subject_id = util.subject_id64(new_subject)
	}
}

@(private = "file")
web_queue_getrange :: proc(state: ^MD_Web_State, subject: string, limit: int, end_ts: i64) {
	if state == nil do return
	if len(state.pending_getrange_subject) > 0 {
		delete(state.pending_getrange_subject)
		state.pending_getrange_subject = ""
	}
	state.pending_getrange_subject = strings.clone(subject)
	state.pending_getrange_limit = limit
	state.pending_getrange_end_ts = end_ts
	state.pending_getrange_queued = true
}

@(private = "file")
web_send_getrange_now :: proc(state: ^MD_Web_State, subject: string, limit: int, end_ts: i64) -> bool {
	if state == nil do return false
	if ws_state() != 2 do return false
	if len(subject) == 0 do return false
	state.rid_counter += 1
	buf: [512]u8
	msg, ok := md_common.build_getrange_msg(buf[:], subject, limit, end_ts, state.rid_counter)
	if !ok do return false
	if !web_frame_within_limit(state, len(msg)) do return false
	ws_send(raw_data(buf[:len(msg)]), i32(len(msg)))
	return true
}

@(private = "file")
web_flush_pending_getrange :: proc(state: ^MD_Web_State) {
	if state == nil do return
	if !state.pending_getrange_queued do return
	if !web_send_getrange_now(state, state.pending_getrange_subject, state.pending_getrange_limit, state.pending_getrange_end_ts) do return

	if len(state.pending_getrange_subject) > 0 {
		delete(state.pending_getrange_subject)
		state.pending_getrange_subject = ""
	}
	state.pending_getrange_limit = 0
	state.pending_getrange_end_ts = 0
	state.pending_getrange_queued = false
}

@(private = "file")
web_send_hello :: proc(state: ^MD_Web_State) {
	state.rid_counter += 1
	buf: [512]u8
	features: [md_common.MAX_REQUESTED_FEATURES][24]u8
	feature_lens: [md_common.MAX_REQUESTED_FEATURES]u8
	server_known := state.caps.supported_feature_count > 0
	fc := md_common.resolve_requested_features(
		state.transport_mode,
		.WASM,
		server_known,
		web_server_has_feature(state, "batching"),
		web_server_has_feature(state, "snapshot_hash"),
		web_server_has_feature(state, "prev_seq"),
		web_feature_setting_value(services.SETTING_FEATURE_BATCHING),
		web_feature_setting_value(services.SETTING_FEATURE_SNAPSHOT_HASH),
		web_feature_setting_value(services.SETTING_FEATURE_PREV_SEQ),
		&features, &feature_lens,
		web_server_has_feature(state, "compress"),
		web_feature_setting_value(services.SETTING_FEATURE_COMPRESS),
		web_supports_decompress(),
	)
	msg, ok := md_common.build_hello_msg_v2(buf[:], state.rid_counter, &features, &feature_lens, fc)
	if !ok {
		fmt.printf("[md-lifecycle] WARN hello_v2_buffer_overflow features=%d falling_back_to_basic\n", fc)
		msg, ok = md_common.build_hello_msg(buf[:], state.rid_counter)
	}
	if !ok do return
	if !web_frame_within_limit(state, len(msg)) do return
	ws_send(raw_data(buf[:len(msg)]), i32(len(msg)))
	fmt.printf("[md-lifecycle] hello_sent rid=h%d features=%d\n", state.rid_counter, fc)
}

@(private = "file")
web_send_ping :: proc(state: ^MD_Web_State) {
	state.rid_counter += 1
	ts := time.now()._nsec / 1_000_000
	buf: [256]u8
	msg, ok := md_common.build_ping_msg(buf[:], ts, state.rid_counter)
	if !ok do return
	if !web_frame_within_limit(state, len(msg)) do return
	ws_send(raw_data(buf[:len(msg)]), i32(len(msg)))
	state.last_ping_sent_ms = ts
}

// Build auth header for WS handshake: JWT takes priority over API key.
@(private = "file")
web_build_auth_header :: proc(state: ^MD_Web_State, buf: []u8) -> int {
	n := 0
	if len(state.jwt_token) > 0 {
		prefix :: "Authorization: Bearer "
		for c in prefix { if n < len(buf) - 2 { buf[n] = u8(c); n += 1 } }
		for c in state.jwt_token { if n < len(buf) - 2 { buf[n] = u8(c); n += 1 } }
		buf[n] = '\r'; n += 1
		buf[n] = '\n'; n += 1
	} else if len(state.api_key) > 0 {
		prefix :: "X-API-Key: "
		for c in prefix { if n < len(buf) - 2 { buf[n] = u8(c); n += 1 } }
		for c in state.api_key { if n < len(buf) - 2 { buf[n] = u8(c); n += 1 } }
		buf[n] = '\r'; n += 1
		buf[n] = '\n'; n += 1
	}
	return n
}

WEB_PING_INTERVAL_MS :: 20_000
WEB_RESYNC_TIMEOUT_MS :: 5_000

@(private = "file")
web_send_resync :: proc(state: ^MD_Web_State, subject: string, last_seq: i64) -> bool {
	// Prefer stored stream_id; fall back to subject.
	stream_id := subject
	sid := util.subject_id64(subject)
	if si := find_web_sub_by_subject_id(state, sid); si >= 0 && state.active_subs[si].stream_id_len > 0 {
		stream_id = string(state.active_subs[si].stream_id[:int(state.active_subs[si].stream_id_len)])
	}
	state.rid_counter += 1
	buf: [512]u8
	msg, ok := md_common.build_resync_msg(buf[:], stream_id, last_seq, state.rid_counter)
	if !ok do return false
	if !web_frame_within_limit(state, len(msg)) do return false
	ws_send(raw_data(buf[:len(msg)]), i32(len(msg)))
	state.resync_count += 1
	fmt.printf("[md-lifecycle] resync_sent stream_id=%s last_seq=%d rid=rs%d\n", stream_id, last_seq, state.rid_counter)
	return true
}

@(private = "file")
web_poll :: proc(events_buf: []ports.MD_Event) -> int {
	state := g_web_state
	if state == nil do return 0

	poll_dt_s := 1.0 / 60.0
	now_tick := time.tick_now()
	if state.has_last_poll_tick {
		elapsed := time.tick_since(state.last_poll_tick)
		if elapsed > 0 {
			poll_dt_s = f64(elapsed) / f64(time.Second)
			// Clamp large jumps (tab sleep/background) to keep reconnect backoff progression sane.
			if poll_dt_s > 0.25 do poll_dt_s = 0.25
		} else {
			poll_dt_s = 0
		}
	}
	state.last_poll_tick = now_tick
	state.has_last_poll_tick = true

	// Reconnection: when disconnected, count down and try again.
	current_ws := ws_state()
	is_open := current_ws == 2

	if !is_open && state.was_connected {
		// Just disconnected — start backoff.
		state.backoff_s = WEB_BACKOFF_INITIAL_S
		state.reconnect_timer = state.backoff_s
		state.was_connected = false
		set_web_transport_state(state, .Backoff)
		apply_web_fault(state, .ServerClosed)
		state.connect_started_ms = 0
		state.first_data_logged = false
		fmt.println("[md-lifecycle] disconnect reason=transport_closed")
	}

	if !is_open && current_ws == 0 && state.active_count > 0 && !state.reconnect_blocked {
		// Tick down reconnect timer using real elapsed poll time (works with idle-throttled RAF).
		state.reconnect_timer -= poll_dt_s
		if state.reconnect_timer <= 0 {
			rc_hdr_buf: [256]u8
			rc_hdr_len := web_build_auth_header(state, rc_hdr_buf[:])
			url_raw := raw_data(transmute([]u8)state.ws_url)
			state.reconnect_count += 1
			fmt.printf("[md-lifecycle] reconnect_attempt count=%d url=%s\n", state.reconnect_count, web_log_safe_url(state.ws_url))
			ws_connect(url_raw, i32(len(state.ws_url)), raw_data(rc_hdr_buf[:rc_hdr_len]), i32(rc_hdr_len))
			state.backoff_s = min(state.backoff_s * WEB_BACKOFF_MULTIPLIER, WEB_BACKOFF_MAX_S)
			jittered_ms := md_common.backoff_with_jitter(int(state.backoff_s * 1000), &state.jitter_seed)
			state.reconnect_timer = f64(jittered_ms) / 1000.0
		}
	}

	// If just connected, send HELLO and re-subscribe all active subs.
	if is_open && !state.was_connected {
		state.backoff_s = WEB_BACKOFF_INITIAL_S
		state.allow_legacy_ws = read_allow_legacy_ws_web()
		state.canonical_evidence_subject = read_bool_web_setting(services.SETTING_CANONICAL_EVIDENCE_SUBJECT, true)
		state.canonical_signal_subject = read_bool_web_setting(services.SETTING_CANONICAL_SIGNAL_SUBJECT, true)
		state.accept_legacy_evidence = read_bool_web_setting(services.SETTING_ACCEPT_LEGACY_EVIDENCE, true)
		state.accept_legacy_signal = read_bool_web_setting(services.SETTING_ACCEPT_LEGACY_SIGNAL, true)
		state.ws_error_category = .None
		state.ws_error_action = .None
		state.desync = false
		state.desync_reason = .None
		state.resync_pending_subject_id = 0
		state.resync_sent_ms = 0
		state.protocol_version = 0
		state.hello_received = false
		state.hello_valid = false
		state.transport_mode = .Terminal_V1
		set_web_transport_state(state, .Hello_Pending)
		state.server_metrics_received = false
		state.last_metrics_ts_ms = 0
		state.last_pong_ts_ms = 0
		state.connect_started_ms = time.now()._nsec / 1_000_000
		state.first_data_logged = false
		state.caps.received = false
		state.caps.supported_feature_count = 0
		state.negotiated_feature_count = 0
		for i in 0 ..< state.active_count {
			state.last_seq_by_sub[i] = 0
			state.last_snapshot_seq_by_sub[i] = 0
			state.last_server_ts_by_sub[i] = 0
			state.snapshot_logged_by_sub[i] = false
			state.active_subs[i].stream_id_len = 0 // Clear stale stream_id from previous connection.
		}
		fmt.printf("[md-lifecycle] connect requested_url=%s\n", web_log_safe_url(state.ws_url))
		// Terminal_V1: send HELLO immediately.
		web_send_hello(state)
		for i in 0 ..< state.active_count {
			sub := state.active_subs[i]
			if len(sub.subject) > 0 {
				web_send_subscribe(state, sub.subject)
			}
		}
		web_flush_pending_getrange(state)
	}
	state.was_connected = is_open

	// Hello timeout: if connected but no hello within WEB_HELLO_TIMEOUT_MS,
	// downgrade to Legacy_JSON (server doesn't support Terminal_V1).
	if is_open && !state.hello_received && state.connect_started_ms > 0 && state.transport_mode == .Terminal_V1 {
		now_check := time.now()._nsec / 1_000_000
		if now_check - state.connect_started_ms > WEB_HELLO_TIMEOUT_MS {
			apply_web_fault(state, .Timeout)
			if state.ws_error_action == .Stop {
				fmt.println("[md-lifecycle] hello_timeout — downgrade blocked (ALLOW_LEGACY_WS=OFF)")
				ws_close()
				state.was_connected = false
				return 0
			}
			fmt.println("[md-lifecycle] hello_timeout — downgrade to Legacy_JSON")
		}
	}

	// MR protocol PING: send periodic ping for RTT measurement.
	if is_open && state.hello_received && state.transport_mode == .Terminal_V1 {
		now_ping := time.now()._nsec / 1_000_000
		if state.last_ping_sent_ms == 0 || (now_ping - state.last_ping_sent_ms) > WEB_PING_INTERVAL_MS {
			web_send_ping(state)
		}
	}

	// Drain messages from JS queue with a frame budget to avoid parse spikes.
	drain_start := time.tick_now()
	drained_msgs := 0
	hit_msg_budget := false
	hit_time_budget := false
	for drained_msgs < state.parse_max_msgs_per_poll {
		n := ws_poll_msg(raw_data(state.recv_buf[:]), i32(WEB_RECV_BUF_SIZE))
		if n < 0 {
			// Negative = truncation signal from JS bridge.
			state.parse_error_count += 1
			state.drop_payload_oversize += 1
			state.drop_count += 1
			apply_web_fault(state, .BackpressureDrop)
			continue
		}
		if n == 0 do break
		web_apply_parse_result(state, state.recv_buf[:n])
		drained_msgs += 1
		if state.parse_time_budget > 0 && time.tick_since(drain_start) >= state.parse_time_budget {
			hit_time_budget = true
			break
		}
	}
	if drained_msgs >= state.parse_max_msgs_per_poll {
		hit_msg_budget = true
	}
	web_perf_record_poll(state, drained_msgs, hit_msg_budget, hit_time_budget)
	state.drop_ws_queue = int(ws_drop_count())

	// Backend gap detectors.
	now_gap := time.now()._nsec / 1_000_000
	if state.hello_received && state.transport_mode == .Terminal_V1 {
		no_metrics_gap, next_metrics_ts := md_common.detect_no_metrics_gap(
			state.last_metrics_ts_ms,
			now_gap,
			md_common.metrics_stale_timeout_ms(state.caps.metrics_cadence_ms, WEB_METRICS_STALE_MS),
		)
		if no_metrics_gap {
			state.backend_gap_no_metrics += 1
			state.last_metrics_ts_ms = next_metrics_ts
		}
		pong_timeout_gap, next_pong_ts := md_common.detect_pong_timeout_gap(
			state.last_ping_sent_ms, state.last_pong_ts_ms, now_gap, WEB_PONG_TIMEOUT_MS,
		)
		if pong_timeout_gap {
			state.backend_gap_pong_timeout += 1
			state.last_pong_ts_ms = next_pong_ts
		}
	}
	if state.drop_count > 0 && state.drop_count % WEB_FREQUENT_DROP_THRESHOLD == 0 {
		state.backend_gap_frequent_drops += 1
	}

	// RESYNC timeout: if pending and expired, fall back to unsub+resub.
	if is_open && md_common.detect_resync_ack_timeout(
		state.resync_pending_subject_id,
		state.resync_sent_ms,
		time.now()._nsec / 1_000_000,
		WEB_RESYNC_TIMEOUT_MS,
	) {
		fmt.printf("[md-lifecycle] resync_timeout sid=%x — fallback to unsub+resub\n", state.resync_pending_subject_id)
		state.backend_gap_resync_ack_timeout += 1
		// Queue for legacy resub.
		state.desync_resub_subject_id = state.resync_pending_subject_id
		state.resync_pending_subject_id = 0
		state.resync_sent_ms = 0
	}

	// Desync recovery: targeted re-subscribe for the desynced subject.
	if state.desync_resub_subject_id != 0 && is_open {
		resub_sid := state.desync_resub_subject_id
		state.desync_resub_subject_id = 0
		resub_subject := ""
		resub_last_seq := i64(0)
		if si := find_web_sub_by_subject_id(state, resub_sid); si >= 0 {
			resub_subject = state.active_subs[si].subject
			resub_last_seq = state.last_seq_by_sub[si]
		}
		if len(resub_subject) > 0 {
			if state.transport_mode == .Terminal_V1 && state.resync_pending_subject_id == 0 {
				// Send RESYNC and wait for snapshot response.
				fmt.printf("[md-lifecycle] desync_recovery resync subject=%s last_seq=%d\n", resub_subject, resub_last_seq)
				web_send_resync(state, resub_subject, resub_last_seq)
				state.resync_pending_subject_id = resub_sid
				state.resync_sent_ms = time.now()._nsec / 1_000_000
			} else {
				// Legacy mode or RESYNC already pending: unsub+resub.
				fmt.printf("[md-lifecycle] desync_recovery resub subject=%s\n", resub_subject)
				web_send_unsubscribe(state, resub_subject)
				web_send_subscribe(state, resub_subject)
				state.desync = false
				state.desync_reason = .None
				state.resync_pending_subject_id = 0
				state.resync_sent_ms = 0
			}
		}
	}

	// Copy staging to events_buf (same as native_poll).
	out := 0

	// Reserve slots for latest-wins snapshots so trade bursts do not starve UI-critical panels.
	non_trade_pending := 0
	if state.ob_dirty      do non_trade_pending += 1
	if state.stats_dirty   do non_trade_pending += 1
	if state.tape_dirty    do non_trade_pending += 1
	if state.heatmap_dirty do non_trade_pending += 1
	if state.vpvr_dirty    do non_trade_pending += 1
	non_trade_pending += min(state.candle_ring_count, WEB_CANDLE_RING_CAP)
	non_trade_pending += min(state.signal_ring_count, WEB_SIGNAL_RING_CAP)
	trade_emit_limit := len(events_buf) - non_trade_pending
	if trade_emit_limit < 0 do trade_emit_limit = 0

	// Drain trade ring.
	for out < trade_emit_limit && state.trade_count > 0 {
		oldest := (state.trade_write - state.trade_count + WEB_TRADE_RING_CAP) % WEB_TRADE_RING_CAP
		events_buf[out].source.subject_id = state.trade_ring_subject_id[oldest]
		events_buf[out].source.channel = .Trades
		events_buf[out].source.seq = state.trade_ring_seq[oldest]
		events_buf[out].kind = .Trade
		events_buf[out].unix = state.trade_ring[oldest].unix
		events_buf[out].data.trade = state.trade_ring[oldest]
		state.trade_count -= 1
		out += 1
	}

	// Orderbook snapshot.
	if state.ob_dirty && out < len(events_buf) {
		ob := state.ob_staging
		for i in 0 ..< ob.ask_count {
			state.poll_ask_prices[i] = ob.ask_prices[i]
			state.poll_ask_sizes[i]  = ob.ask_sizes[i]
		}
		for i in 0 ..< ob.bid_count {
			state.poll_bid_prices[i] = ob.bid_prices[i]
			state.poll_bid_sizes[i]  = ob.bid_sizes[i]
		}
		events_buf[out].source.subject_id = ob.subject_id
		events_buf[out].source.channel = .Orderbook
		events_buf[out].source.seq = ob.seq
		events_buf[out].kind = .Orderbook_Snapshot
		events_buf[out].unix = ob.unix
		events_buf[out].data.ob = ports.MD_Orderbook_Event{
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
		out += 1
	}

	// Stats.
	if state.stats_dirty && out < len(events_buf) {
		st := state.stats_staging
		events_buf[out].source.subject_id = st.subject_id
		events_buf[out].source.channel = .Stats
		events_buf[out].source.seq = st.seq
		events_buf[out].kind = .Stats
		events_buf[out].unix = st.unix
		events_buf[out].data.stats = ports.MD_Stats_Event{
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
		out += 1
	}

	// Tape.
	if state.tape_dirty && out < len(events_buf) {
		tp := state.tape_staging
		events_buf[out].source.subject_id = tp.subject_id
		events_buf[out].source.channel = .Tape
		events_buf[out].source.seq = tp.seq
		events_buf[out].kind = .Tape
		events_buf[out].unix = tp.unix
		events_buf[out].data.tape = ports.MD_Tape_Event{
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
		out += 1
	}

	// Heatmap.
	if state.heatmap_dirty && out < len(events_buf) {
		hm := state.heatmap_staging
		lc := min(hm.level_count, services.HEATMAP_STAGING_CAP)
		for i in 0 ..< lc {
			state.poll_hm_prices[i] = hm.prices[i]
			state.poll_hm_sizes[i]  = hm.sizes[i]
		}
		events_buf[out].source.subject_id = hm.subject_id
		events_buf[out].source.channel = .Heatmaps
		events_buf[out].source.seq = hm.seq
		events_buf[out].kind = .Heatmap
		events_buf[out].unix = hm.unix
		events_buf[out].data.heatmap = ports.MD_Heatmap_Event{
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
		out += 1
	}

	// VPVR.
	if state.vpvr_dirty && out < len(events_buf) {
		vp := state.vpvr_staging
		lc := min(vp.level_count, services.VPVR_STAGING_CAP)
		for i in 0 ..< lc {
			state.poll_vpvr_prices[i] = vp.prices[i]
			state.poll_vpvr_buys[i]   = vp.buys[i]
			state.poll_vpvr_sells[i]  = vp.sells[i]
		}
		events_buf[out].source.subject_id = vp.subject_id
		events_buf[out].source.channel = .VPVR
		events_buf[out].source.seq = vp.seq
		events_buf[out].kind = .VPVR
		events_buf[out].unix = vp.unix
		events_buf[out].data.vpvr = ports.MD_VPVR_Event{
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
		out += 1
	}

	// Candle ring — drain all buffered candle events.
	for out < len(events_buf) && state.candle_ring_count > 0 {
		oldest := (state.candle_ring_write - state.candle_ring_count + WEB_CANDLE_RING_CAP) % WEB_CANDLE_RING_CAP
		cs := state.candle_ring[oldest]
		events_buf[out].source.subject_id = cs.subject_id
		events_buf[out].source.channel = .Candles
		events_buf[out].source.seq = cs.seq
		events_buf[out].kind = .Candle
		events_buf[out].unix = util.normalize_unix_seconds(cs.window_end_ts)
		events_buf[out].data.candle = ports.MD_Candle_Event{
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
		out += 1
	}

	// Range candle batch.
	if state.range_candle_dirty && out < len(events_buf) {
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
		events_buf[out].source.subject_id = rc.candles[0].subject_id if rc.count > 0 else 0
		events_buf[out].source.channel = .Candles
		events_buf[out].source.seq = rc.seq
		events_buf[out].kind = .Range_Candle_Batch
		events_buf[out].data.range_candles = batch
		state.range_candle_dirty = false
		out += 1
	}
	if state.evidence_dirty && out < len(events_buf) {
		ev := state.evidence_staging
		web_fill_evidence_event(&events_buf[out], ev)
		state.evidence_dirty = false
		out += 1
	}
	for out < len(events_buf) && state.signal_ring_count > 0 {
		oldest := (state.signal_ring_write - state.signal_ring_count + WEB_SIGNAL_RING_CAP) % WEB_SIGNAL_RING_CAP
		sig := state.signal_ring[oldest]
		web_fill_signal_event(&events_buf[out], sig)
		state.signal_ring_count -= 1
		out += 1
	}

	return out
}

@(private = "file")
web_fill_evidence_event :: proc(dst: ^ports.MD_Event, ev: services.Parsed_Evidence) {
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

@(private = "file")
web_fill_signal_event :: proc(dst: ^ports.MD_Event, sig: services.Parsed_Signal) {
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
web_perf_record_poll :: proc(state: ^MD_Web_State, drained_msgs: int, hit_msg_budget, hit_time_budget: bool) {
	if state == nil do return
	if !state.perf_debug do return

	state.perf_polls_total += 1
	state.perf_drained_total += u64(max(drained_msgs, 0))
	if drained_msgs > state.perf_max_drained do state.perf_max_drained = drained_msgs
	if hit_msg_budget || hit_time_budget do state.perf_budget_hit_total += 1
	if hit_msg_budget do state.perf_msg_hit_total += 1
	if hit_time_budget do state.perf_time_hit_total += 1

	now := time.tick_now()
	if !state.perf_has_last_log_tick {
		state.perf_last_log_tick = now
		state.perf_has_last_log_tick = true
		return
	}
	if time.tick_since(state.perf_last_log_tick) < 2 * time.Second do return

	avg_drained := f64(0)
	if state.perf_polls_total > 0 {
		avg_drained = f64(state.perf_drained_total) / f64(state.perf_polls_total)
	}
	budget_ms := int(state.parse_time_budget / time.Millisecond)
	fmt.printf(
		"[ws-perf] poll_budget max_msgs=%d budget_ms=%d polls=%d drained=%d avg=%.1f max=%d hits=%d (msg=%d time=%d)\n",
		state.parse_max_msgs_per_poll, budget_ms,
		state.perf_polls_total, state.perf_drained_total, avg_drained, state.perf_max_drained,
		state.perf_budget_hit_total, state.perf_msg_hit_total, state.perf_time_hit_total,
	)
	state.perf_last_log_tick = now
}

@(private = "file")
web_now_ms :: proc() -> i64 {
	return md_common.now_ms()
}

@(private = "file")
web_conn_status :: proc() -> ports.MD_Conn_Status {
	state := g_web_state
	if state != nil && state.reconnect_blocked {
		return .Offline
	}
	switch ws_state() {
	case 2: return .Connected
	case 1: return .Connecting
	case 0: return .Reconnecting
	case 3: return .Reconnecting
	}
	return .Offline
}

// --- Message parsing ---
// Delegates to shared services.parse_mr_message, then writes results to staging.
// Single-threaded (WASM) so no mutex needed.

WEB_BATCH_SYNTH_FRAME_CAP :: 96 * 1024

@(private = "file")
web_append_ascii :: proc(buf: []u8, n: ^int, s: string) -> bool {
	for c in s {
		if n^ >= len(buf) do return false
		buf[n^] = u8(c)
		n^ += 1
	}
	return true
}

@(private = "file")
web_append_i64_ascii :: proc(buf: []u8, n: ^int, v: i64) -> bool {
	tmp: [24]u8
	num := fmt.bprintf(tmp[:], "%d", v)
	return web_append_ascii(buf, n, num)
}

@(private = "file")
web_build_batched_event_frame :: proc(
	dst: []u8,
	seg: ^services.Parsed_Batched_Frame,
	ev: services.Parsed_Batch_Event_View,
	src_raw: []u8,
) -> ([]u8, bool) {
	if seg == nil do return nil, false
	if ev.payload_start < 0 || ev.payload_end <= ev.payload_start || ev.payload_end > len(src_raw) do return nil, false

	n := 0
	if !web_append_ascii(dst, &n, `{"type":"event","subject":"`) do return nil, false
	if seg.stream_id_len > 0 {
		if !web_append_ascii(dst, &n, string(seg.stream_id_buf[:int(seg.stream_id_len)])) do return nil, false
	} else {
		if seg.channel_len == 0 || seg.venue_len == 0 || seg.symbol_len == 0 do return nil, false
		if !web_append_ascii(dst, &n, string(seg.channel_buf[:int(seg.channel_len)])) do return nil, false
		if !web_append_ascii(dst, &n, "/") do return nil, false
		if !web_append_ascii(dst, &n, string(seg.venue_buf[:int(seg.venue_len)])) do return nil, false
		if !web_append_ascii(dst, &n, "/") do return nil, false
		if !web_append_ascii(dst, &n, string(seg.symbol_buf[:int(seg.symbol_len)])) do return nil, false
		if !web_append_ascii(dst, &n, "/raw") do return nil, false
	}
	if !web_append_ascii(dst, &n, `","seq":`) do return nil, false
	seq := seg.base_seq + i64(ev.event_index)
	if !web_append_i64_ascii(dst, &n, seq) do return nil, false
	if !web_append_ascii(dst, &n, `,"ts_server":`) do return nil, false
	if !web_append_i64_ascii(dst, &n, seg.ts_server_base + ev.dts) do return nil, false
	if !web_append_ascii(dst, &n, `,"ts_ingest":`) do return nil, false
	if !web_append_i64_ascii(dst, &n, seg.ts_ingest_base + ev.dti) do return nil, false
	if !web_append_ascii(dst, &n, `,"payload":`) do return nil, false
	payload := src_raw[ev.payload_start:ev.payload_end]
	if n + len(payload) + 1 > len(dst) do return nil, false
	copy(dst[n:n + len(payload)], payload)
	n += len(payload)
	if !web_append_ascii(dst, &n, "}") do return nil, false
	return dst[:n], true
}

@(private = "file")
web_process_batched_frame :: proc(state: ^MD_Web_State, raw: []u8) -> bool {
	if state == nil do return false
	decode_start := time.tick_now()
	skip := 0
	first_seg := true
	frame_buf: [WEB_BATCH_SYNTH_FRAME_CAP]u8
	for {
		seg: services.Parsed_Batched_Frame
		if !services.parse_batched_frame(raw, &seg, skip) {
			if first_seg {
				return false
			}
			state.parse_error_count += 1
			break
		}
		if first_seg {
			first_seg = false
			total_events := seg.total_events
			if seg.count > total_events do total_events = seg.count
			if total_events < 0 do total_events = 0
			state.batched_frames_received += 1
			state.batched_events_received += u64(total_events)
		}
		if seg.event_count <= 0 do break

		stream_subject := ""
		if seg.stream_id_len > 0 {
			stream_subject = string(seg.stream_id_buf[:int(seg.stream_id_len)])
		}
		stream_channel := ""
		stream_sid := u64(0)
		if len(stream_subject) > 0 {
			stream_channel = util.subject_stream_type(stream_subject)
			stream_sid = util.subject_id64(stream_subject)
		}

		for i in 0 ..< seg.event_count {
			ev := seg.events[i]
			if ev.payload_start < 0 || ev.payload_end <= ev.payload_start || ev.payload_end > len(raw) {
				state.parse_error_count += 1
				continue
			}
			payload := raw[ev.payload_start:ev.payload_end]
			seq := seg.base_seq + i64(ev.event_index)
			ts_server := seg.ts_server_base + ev.dts
			ts_ingest := seg.ts_ingest_base + ev.dti

			fastpath := false
			if stream_sid != 0 && len(stream_channel) > 0 {
				services.parse_arena_record_message(&state.parse_arena, len(payload))
				parse_start_tick := time.tick_now()
				if result, ok := services.parse_batched_event_payload(stream_channel, payload, seq, ts_server, ts_ingest, stream_sid); ok {
					parse_end_tick := time.tick_now()
					parsed_now_ms := time.now()._nsec / 1_000_000
					md_common.update_parse_rates(&state.rates, parsed_now_ms, len(payload))
					web_apply_parsed_result(state, result, len(payload), parsed_now_ms)
					apply_end_tick := time.tick_now()
					parse_us := i64(time.duration_microseconds(time.tick_diff(parse_start_tick, parse_end_tick)))
					apply_us := i64(time.duration_microseconds(time.tick_diff(parse_end_tick, apply_end_tick)))
					web_record_perf_sample(&state.parse_samples_us, &state.parse_sample_head, &state.parse_sample_count, parse_us)
					web_record_perf_sample(&state.apply_samples_us, &state.apply_sample_head, &state.apply_sample_count, apply_us)
					state.batched_fastpath_events += 1
					fastpath = true
				}
				services.parse_arena_reset_message(&state.parse_arena)
			}
			if fastpath do continue

			state.batched_fallback_events += 1
			synth, ok := web_build_batched_event_frame(frame_buf[:], &seg, ev, raw)
			if !ok {
				state.parse_error_count += 1
				continue
			}
			web_apply_parse_result(state, synth)
		}
		skip += seg.event_count
		if !seg.has_more do break
	}
	decode_us := i64(time.duration_microseconds(time.tick_since(decode_start)))
	web_record_perf_sample(&state.batch_decode_samples_us, &state.batch_decode_sample_head, &state.batch_decode_sample_count, decode_us)
	return true
}

@(private = "file")
web_apply_parse_result :: proc(state: ^MD_Web_State, raw: []u8) {
	if web_process_batched_frame(state, raw) {
		return
	}
	defer services.parse_arena_reset_message(&state.parse_arena)
	parse_start_tick := time.tick_now()

	telemetry: services.Parse_Telemetry
	result := services.parse_mr_message_with_arena(&state.parse_arena, raw, &telemetry)
	parse_end_tick := time.tick_now()
	parsed_now_ms := time.now()._nsec / 1_000_000

	md_common.update_parse_rates(&state.rates, parsed_now_ms, len(raw))

	if telemetry.parse_errors > 0 {
		state.parse_error_count += telemetry.parse_errors
		if state.parse_error_count <= 3 || state.parse_error_count % 50 == 0 {
			fmt.printf("[ws] Parse error #%d (frame_len=%d)\n", state.parse_error_count, len(raw))
		}
	}
	state.canonical_stats_frames += u64(max(telemetry.canonical_stats_frames, 0))
	state.stats_fallback_frames += u64(max(telemetry.stats_fallback_frames, 0))
	state.canonical_evidence_frames += u64(max(telemetry.canonical_evidence_frames, 0))
	state.legacy_evidence_frames += u64(max(telemetry.legacy_evidence_frames, 0))
	state.evidence_fallback_frames += u64(max(telemetry.evidence_fallback_frames, 0))
	state.canonical_signal_frames += u64(max(telemetry.canonical_signal_frames, 0))
	state.legacy_signal_frames += u64(max(telemetry.legacy_signal_frames, 0))
	state.signal_fallback_frames += u64(max(telemetry.signal_fallback_frames, 0))
	web_apply_parsed_result(state, result, len(raw), parsed_now_ms)

	parse_us := i64(time.duration_microseconds(time.tick_diff(parse_start_tick, parse_end_tick)))
	apply_end_tick := time.tick_now()
	apply_us := i64(time.duration_microseconds(time.tick_diff(parse_end_tick, apply_end_tick)))
	web_record_perf_sample(&state.parse_samples_us, &state.parse_sample_head, &state.parse_sample_count, parse_us)
	web_record_perf_sample(&state.apply_samples_us, &state.apply_sample_head, &state.apply_sample_count, apply_us)
}

@(private = "file")
web_apply_parsed_result :: proc(state: ^MD_Web_State, result: services.Parse_Result, frame_len: int, parsed_now_ms: i64) {
	should_log_snapshot := false
	snapshot_subject := ""
	snapshot_seq := i64(0)
	snapshot_sid := u64(0)
	should_log_first_data := false
	first_data_delta_ms := i64(0)

	if result.kind != .None {
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
			if si := find_web_sub_by_subject_id(state, result.meta.subject_id); si >= 0 {
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
						set_web_transport_state(state, .Desync)
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
						set_web_transport_state(state, .Running)
					}
				}
				state.last_seq_by_sub[si] = result.meta.seq
				if result.meta.prev_seq > 0 {
					if md_common.validate_prev_seq(result.meta.prev_seq, prev_seq) {
						state.prev_seq_violations += 1
					}
				}
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
						} else if web_negotiated_has_feature(state, "snapshot_hash") {
							state.hash_validation_skipped += 1
							fmt.printf("[md-lifecycle] snapshot_hash_skipped sid=%x seq=%d reason=noncanonical_input\n", result.meta.subject_id, result.meta.seq)
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
						set_web_transport_state(state, .Desync)
						if state.desync_resub_subject_id == 0 {
							state.desync_resub_subject_id = result.meta.subject_id
						}
					} else if state.desync_reason == .Protocol_Invalid {
						// Auto-recover: valid forward-progressing timestamp clears desync.
						state.desync = false
						state.desync_reason = .None
						set_web_transport_state(state, .Running)
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
					set_web_transport_state(state, .Running)
				}
			}
		}
		if md_common.parse_result_has_data(result.kind) && state.connect_started_ms > 0 && !state.first_data_logged {
			first_data_delta_ms = max(parsed_now_ms - state.connect_started_ms, 0)
			state.first_data_logged = true
			should_log_first_data = true
			if state.desync_reason == .None && state.hello_received && state.hello_valid {
				set_web_transport_state(state, .Running)
			}
		}
	}
	if should_log_snapshot {
		fmt.printf("[md-lifecycle] snapshot_recv subject=%s sid=%x seq=%d\n", snapshot_subject, snapshot_sid, snapshot_seq)
		delete(snapshot_subject)
	}
	if should_log_first_data {
		fmt.printf("[md-lifecycle] first_data_after_connect_ms=%d kind=%v sid=%x\n", first_data_delta_ms, result.kind, result.meta.subject_id)
	}
	if md_common.parse_result_has_data(result.kind) {
		if result.kind == .Evidence && result.meta.legacy_subject && !state.accept_legacy_evidence {
			state.legacy_evidence_rejected += 1
			return
		}
		if result.kind == .Signal && result.meta.legacy_subject && !state.accept_legacy_signal {
			state.legacy_signal_rejected += 1
			return
		}
		if !state.hello_received {
			if state.desync_reason != .Missing_Hello {
				fmt.printf("[md-lifecycle] desync reason=missing_hello kind=%v sid=%x\n", result.kind, result.meta.subject_id)
			}
			state.desync = true
			state.desync_reason = .Missing_Hello
			set_web_transport_state(state, .Desync)
			return
		}
		if !state.hello_valid {
			return
		}
	}

	switch result.kind {
	case .Ack:
		ack := result.data.ack
		state.subscribe_ack_count += 1
		// Store stream_id from ACK into the matching sub entry.
		if len(ack.stream_id) > 0 && len(ack.subject) > 0 {
			sid := util.subject_id64(ack.subject)
			if si := find_web_sub_by_subject_id(state, sid); si >= 0 {
				n := min(len(ack.stream_id), len(state.active_subs[si].stream_id))
				for i in 0 ..< n { state.active_subs[si].stream_id[i] = ack.stream_id[i] }
				state.active_subs[si].stream_id_len = u8(n)
			}
		}
		if len(ack.stream_id) > 0 {
			fmt.printf("[md-lifecycle] ack_recv op=%s subject=%s stream_id=%s\n", ack.op, ack.subject, ack.stream_id)
		} else {
			fmt.printf("[md-lifecycle] ack_recv op=%s subject=%s\n", ack.op, ack.subject)
		}
	case .Hello:
		h := result.data.hello
		state.hello_received = true
		state.protocol_version = h.proto_ver
		state.hello_valid = h.valid
		state.transport_mode = .Terminal_V1
		set_web_transport_state(state, .Hello_Pending)
		md_common.apply_hello_to_capabilities(&state.caps, h)
		if !h.valid {
			state.desync = true
			state.desync_reason = md_common.desync_reason_from_hello_reject(h.reject)
			set_web_transport_state(state, .Desync)
			fmt.printf(
				"[md-lifecycle] hello_rejected proto_ver=%d reject=%v topics=%d venues=%d symbols=%d\n",
				h.proto_ver, h.reject, h.topic_count, h.venue_count, h.symbol_count,
			)
			return
		}
		state.desync = false
		state.desync_reason = .None
		set_web_transport_state(state, .Running)
		fmt.printf(
			"[md-lifecycle] hello_ok proto_ver=%d server_id=%s topics=%d venues=%d symbols=%d\n",
			h.proto_ver, h.server_instance_id, h.topic_count, h.venue_count, h.symbol_count,
		)
	case .Hello_Ack:
		ha := result.data.hello_ack
		state.negotiated_feature_count = ha.negotiated_feature_count
		for i in 0 ..< ha.negotiated_feature_count {
			state.negotiated_features[i] = ha.negotiated_features[i]
		}
		fmt.printf("[md-lifecycle] hello_ack_recv negotiated_features=%d\n", ha.negotiated_feature_count)
	case .Heartbeat, .Health:
		ctrl := result.data.control
		if ctrl.rtt_ms > 0 do state.last_rtt_ms = ctrl.rtt_ms
		if ctrl.dropped > 0 && ctrl.dropped > state.drop_count do state.drop_count = ctrl.dropped
	case .Pong:
		p := result.data.pong
		state.pong_rtt_ms = p.rtt_ms
		if p.rtt_ms > 0 do state.last_rtt_ms = p.rtt_ms
		state.last_pong_ts_ms = parsed_now_ms
	case .Metrics:
		m := result.data.server_metrics
		state.server_metrics = m
		state.server_metrics_received = true
		state.last_metrics_ts_ms = parsed_now_ms
	case .Error:
		ed := result.data.error_detail
		if len(ed.code) > 0 || len(ed.error_code) > 0 {
			fmt.printf("[ws] Error: code=%s error_code=%s action_hint=%s msg=%s op=%s rid=%s\n",
				ed.code, ed.error_code, ed.action_hint, ed.message, ed.op, ed.request_id)
			hint := util.parse_action_hint(ed.action_hint)
			action, meaningful := md_common.action_hint_to_ws_fault(hint)
			if meaningful && hint != .Unspecified {
				state.ws_error_action = action
				switch action {
				case .Retry:
					set_web_transport_state(state, .Backoff)
				case .Resync:
					state.desync = true
					state.desync_reason = .Resync_Required
					set_web_transport_state(state, .Desync)
				case .Stop:
					state.reconnect_blocked = true
					state.desync = true
					state.desync_reason = .Protocol_Invalid
					set_web_transport_state(state, .Desync)
				case .Downgrade, .None:
				}
			} else {
				if strings.contains(ed.code, "AUTH") || strings.contains(ed.code, "UNAUTHORIZED") || strings.contains(ed.code, "TOKEN") {
					apply_web_fault(state, .AuthDenied)
				}
				if ed.code == "ERROR_CODE_RESYNC_REQUIRED" {
					state.desync = true
					state.desync_reason = .Resync_Required
					set_web_transport_state(state, .Desync)
				}
			}
		} else {
			fmt.printf("[ws] Error frame without code (frame_len=%d)\n", frame_len)
		}
	case .Trade:
		t := result.data.trade
		if state.trade_count < WEB_TRADE_RING_CAP {
			state.trade_ring_subject_id[state.trade_write] = t.subject_id
			state.trade_ring_seq[state.trade_write] = t.seq
			state.trade_ring[state.trade_write] = ports.MD_Trade_Event{
				price  = t.price,
				qty    = t.qty,
				is_buy = t.is_buy,
				unix   = t.unix,
			}
			state.trade_write = (state.trade_write + 1) % WEB_TRADE_RING_CAP
			state.trade_count += 1
		} else {
			state.drop_count += 1
			state.drop_trade_ring += 1
			apply_web_fault(state, .BackpressureDrop)
		}
	case .Orderbook:
		state.ob_staging = result.data.ob
		state.ob_dirty = true
	case .Stats:
		state.stats_staging = result.data.stats
		state.stats_dirty = true
	case .Tape:
		state.tape_staging = result.data.tape
		state.tape_dirty = true
	case .Heatmap:
		state.heatmap_staging = result.data.heatmap
		state.heatmap_dirty = true
	case .VPVR:
		state.vpvr_staging = result.data.vpvr
		state.vpvr_dirty = true
	case .Candle:
		// Unified ring write: always write at write ptr, cap count at capacity.
		if state.candle_ring_count >= WEB_CANDLE_RING_CAP {
			state.drop_count += 1
			state.drop_candle_ring += 1
			apply_web_fault(state, .BackpressureDrop)
		}
		state.candle_ring[state.candle_ring_write] = result.data.candle
		state.candle_ring_write = (state.candle_ring_write + 1) % WEB_CANDLE_RING_CAP
		if state.candle_ring_count < WEB_CANDLE_RING_CAP {
			state.candle_ring_count += 1
		}
	case .Range_Candle:
		state.range_candle_staging = result.data.range_candles
		state.range_candle_dirty = true
	case .Evidence:
		state.evidence_staging = result.data.evidence
		state.evidence_dirty = true
	case .Signal:
		if state.signal_ring_count >= WEB_SIGNAL_RING_CAP {
			state.drop_count += 1
			apply_web_fault(state, .BackpressureDrop)
		}
		state.signal_ring[state.signal_ring_write] = result.data.signal
		state.signal_ring_write = (state.signal_ring_write + 1) % WEB_SIGNAL_RING_CAP
		if state.signal_ring_count < WEB_SIGNAL_RING_CAP {
			state.signal_ring_count += 1
		}
	case .None:
		// Ignored (last, unknown frame types).
	}
}

@(private = "file")
web_set_candle_tf :: proc(tf: string) {
	state := g_web_state
	if state == nil do return
	if state.candle_tf_filter == tf do return
	old := state.candle_tf_filter
	state.candle_tf_filter = strings.clone(tf)
	web_resubscribe_timeframe_channels(state)
	delete(old)
}

@(private = "file")
web_send_getrange :: proc(subject: string, limit: int, end_ts: i64) {
	state := g_web_state
	if state == nil do return
	if !web_send_getrange_now(state, subject, limit, end_ts) {
		web_queue_getrange(state, subject, limit, end_ts)
	}
}

@(private = "file")
web_fetch_markets :: proc(out_buf: [^]u8, out_cap: i32) -> i32 {
	state := g_web_state
	if state == nil do return 0
	if out_cap <= 0 do return 0

	// Derive markets endpoint from WS URL.
	// Examples:
	// - ws://host:8080/ws  -> http://host:8080/api/v1/markets
	// - /ws                -> /api/v1/markets (same-origin)
	// - http://host/ws     -> http://host/api/v1/markets
	ws_url := strings.trim_space(state.ws_url)
	if len(ws_url) == 0 do return 0

	http_base := ""
	delete_http_base := false
	if strings.has_prefix(ws_url, "wss://") {
		http_base = strings.concatenate({"https://", ws_url[6:]})
		delete_http_base = true
	} else if strings.has_prefix(ws_url, "ws://") {
		http_base = strings.concatenate({"http://", ws_url[5:]})
		delete_http_base = true
	} else if strings.has_prefix(ws_url, "https://") || strings.has_prefix(ws_url, "http://") {
		http_base = ws_url
	} else if strings.has_prefix(ws_url, "/") {
		http_base = ws_url
	} else {
		return 0
	}
	if delete_http_base {
		defer delete(http_base)
	}

	base_no_ws := http_base
	if strings.has_suffix(base_no_ws, "/ws") {
		base_no_ws = base_no_ws[:len(base_no_ws) - 3]
	}

	url := strings.concatenate({"/api/v1/markets"})
	if len(base_no_ws) == 0 || base_no_ws == "/" {
		// Same-origin fallback endpoint.
	} else {
		delete(url)
		url = strings.concatenate({base_no_ws, "/api/v1/markets"})
	}
	defer delete(url)

	url_raw := raw_data(transmute([]u8)url)
	return http_get_sync(url_raw, i32(len(url)), out_buf, out_cap)
}
