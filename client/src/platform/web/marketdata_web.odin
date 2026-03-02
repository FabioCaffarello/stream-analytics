package main

// WASM marketdata port — JS WebSocket bridge, single-threaded.
// Polls messages from a JS-side queue via ws_poll_msg foreign proc.
// Same staging pattern as native (ring + latest-wins), but no mutex needed.

import "core:fmt"
import "core:strconv"
import "core:strings"
import "core:time"
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
	url_query_param :: proc(name_ptr: [^]u8, name_len: i32, out_ptr: [^]u8, out_cap: i32) -> i32 ---
	http_get_sync   :: proc(url_ptr: [^]u8, url_len: i32, out_ptr: [^]u8, out_cap: i32) -> i32 ---
}

// --- Constants ---

WEB_TRADE_RING_CAP   :: 1024
WEB_CANDLE_RING_CAP  :: 8
WEB_MAX_SUBS         :: 128
WEB_RECV_BUF_SIZE    :: 128 * 1024 // 128 KB per message max
WEB_PARSE_MAX_MSGS_PER_POLL :: 64
WEB_PARSE_TIME_BUDGET       :: 2 * time.Millisecond

// Reconnection backoff.
WEB_BACKOFF_INITIAL_S :: 0.5
WEB_BACKOFF_MAX_S     :: 30.0
WEB_BACKOFF_MULTIPLIER :: 2.0

// Default candle timeframe filter.
CANDLE_TF_DEFAULT :: "1m"

// --- State ---

Web_Sub_Entry :: struct {
	subject_id:     u64,
	venue:          string,
	symbol:         string,
	channel:        ports.MD_Channel,
	subject:        string,
	is_explicit_tf: bool, // true = per-cell TF sub; skip in global TF resubscribe
}

MD_Web_State :: struct {
	// Trade ring buffer.
	trade_ring:            [WEB_TRADE_RING_CAP]ports.MD_Trade_Event,
	trade_ring_subject_id: [WEB_TRADE_RING_CAP]u64,
	trade_write:           int,
	trade_count:           int,

	// Latest-wins staging.
	ob_staging:      services.Parsed_OB,
	ob_dirty:        bool,
	stats_staging:   services.Parsed_Stats,
	stats_dirty:     bool,
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

	// Candle timeframe filter (mutable, heap-allocated).
	candle_tf_filter: string,

	// Connection.
	ws_url:  string,
	api_key: string,

	// Subscription tracking for reconnect re-subscribe.
	active_subs:  [WEB_MAX_SUBS]Web_Sub_Entry,
	active_count: int,
	pending_getrange_subject: string,
	pending_getrange_limit:   int,
	pending_getrange_end_ts:  i64,
	pending_getrange_queued:  bool,
	rid_counter:       u32,
	drop_count:        int,
	reconnect_count:   int,
	parse_error_count: int,
	subscribe_ack_count: int,
	parsed_msgs_total:   u64,
	parsed_bytes_total:  u64,
	msg_rate:            f64,
	bytes_rate:          f64,
	rate_window_msgs:    u64,
	rate_window_bytes:   u64,
	rate_window_start_ms: i64,
	last_msg_ts_ms:     i64,
	last_server_ts_ms:  i64,
	last_rtt_ms:        i64,
	last_lag_ms:        i64,
	desync:             bool,
	connect_started_ms: i64,
	first_data_logged:  bool,
	last_seq_by_sub:    [WEB_MAX_SUBS]i64,
	snapshot_logged_by_sub: [WEB_MAX_SUBS]bool,

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
	last_poll_tick:     time.Tick,
	has_last_poll_tick: bool,

	// Parse budget tuning (configurable via URL query params).
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
}

@(private = "file")
g_web_state: ^MD_Web_State

// --- Public API ---

make_marketdata_web :: proc(url: string, api_key: string = "") -> ports.Marketdata_Port {
	if g_web_state != nil {
		web_shutdown()
	}
	state := new(MD_Web_State)
	state.ws_url = strings.clone(url)
	state.api_key = strings.clone(api_key)
	state.candle_tf_filter = strings.clone(CANDLE_TF_DEFAULT)
	state.parse_max_msgs_per_poll = WEB_PARSE_MAX_MSGS_PER_POLL
	state.parse_time_budget = WEB_PARSE_TIME_BUDGET
	state.parse_max_msgs_per_poll = clamp_positive_int(
		web_query_param_int("ws_parse_max_msgs", state.parse_max_msgs_per_poll),
		1, 1024,
		state.parse_max_msgs_per_poll,
	)
	parse_budget_ms := clamp_positive_int(
		web_query_param_int("ws_parse_budget_ms", int(state.parse_time_budget / time.Millisecond)),
		0, 50,
		int(state.parse_time_budget / time.Millisecond),
	)
	state.parse_time_budget = time.Duration(parse_budget_ms) * time.Millisecond
	state.perf_debug = web_query_param_int("ws_perf_debug", 0) > 0
	g_web_state = state

	// Initiate connection via JS bridge.
	hdr_buf: [128]u8
	hdr_len := 0
	if len(api_key) > 0 {
		prefix :: "X-API-Key: "
		for c in prefix { hdr_buf[hdr_len] = u8(c); hdr_len += 1 }
		for c in api_key { if hdr_len < len(hdr_buf) - 2 { hdr_buf[hdr_len] = u8(c); hdr_len += 1 } }
		hdr_buf[hdr_len] = '\r'; hdr_len += 1
		hdr_buf[hdr_len] = '\n'; hdr_len += 1
	}
	url_raw := raw_data(transmute([]u8)url)
	state.connect_started_ms = time.now()._nsec / 1_000_000
	state.first_data_logged = false
	fmt.printf("[md-lifecycle] connect url=%s\n", url)
	ws_connect(url_raw, i32(len(url)), raw_data(hdr_buf[:hdr_len]), i32(hdr_len))

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

@(private = "file")
clamp_positive_int :: proc(v: int, lo: int, hi: int, fallback: int) -> int {
	if hi < lo do return fallback
	if v < lo || v > hi do return fallback
	return v
}

@(private = "file")
web_query_param_int :: proc(name: string, fallback: int) -> int {
	buf: [32]u8
	n := url_query_param(
		raw_data(transmute([]u8)name), i32(len(name)),
		raw_data(buf[:]), i32(len(buf)),
	)
	if n <= 0 do return fallback
	if n > i32(len(buf)) do n = i32(len(buf))
	v, ok := strconv.parse_int(string(buf[:int(n)]))
	if !ok do return fallback
	return int(v)
}

// --- Port implementation ---

@(private = "file")
find_web_sub_by_subject :: proc(state: ^MD_Web_State, subject: string) -> int {
	for i in 0 ..< state.active_count {
		if state.active_subs[i].subject == subject do return i
	}
	return -1
}

@(private = "file")
find_web_sub_by_key :: proc(state: ^MD_Web_State, venue: string, symbol: string, channel: ports.MD_Channel) -> int {
	for i in 0 ..< state.active_count {
		sub := state.active_subs[i]
		if sub.channel == channel && sub.venue == venue && sub.symbol == symbol do return i
	}
	return -1
}

@(private = "file")
find_web_sub_by_subject_id :: proc(state: ^MD_Web_State, subject_id: u64) -> int {
	for i in 0 ..< state.active_count {
		if state.active_subs[i].subject_id == subject_id do return i
	}
	return -1
}

@(private = "file")
web_subject_for_channel :: proc(state: ^MD_Web_State, venue: string, symbol: string, channel: ports.MD_Channel) -> string {
	tf := ""
	if channel == .Heatmaps || channel == .VPVR || channel == .Candles {
		tf = state.candle_tf_filter
	}
	return util.build_subject_with_timeframe(venue, symbol, channel, tf)
}

@(private = "file")
web_free_sub_entry :: proc(entry: ^Web_Sub_Entry) {
	if entry == nil do return
	if len(entry.venue) > 0 do delete(entry.venue)
	if len(entry.symbol) > 0 do delete(entry.symbol)
	if len(entry.subject) > 0 do delete(entry.subject)
	entry^ = {}
}

@(private = "file")
web_shutdown :: proc() {
	state := g_web_state
	if state == nil do return
	ws_close()
	for i in 0 ..< state.active_count {
		web_free_sub_entry(&state.active_subs[i])
		state.last_seq_by_sub[i] = 0
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
	state.was_connected = false
	state.desync = false
	state.connect_started_ms = 0
	state.first_data_logged = false
	return true
}

@(private = "file")
web_reconnect_transport :: proc(ws_url: string, api_key: string) -> bool {
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
	ws_close()
	state.was_connected = false
	state.reconnect_timer = 0
	state.desync = false
	state.connect_started_ms = 0
	state.first_data_logged = false
	fmt.printf("[md-lifecycle] reconnect_requested url=%s\n", state.ws_url)
	return true
}

@(private = "file")
web_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_web_state
	if state == nil do return false

	subject := web_subject_for_channel(state, venue, symbol, channel)
	subject_id := util.subject_id64(subject)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_web_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	if state.active_count >= WEB_MAX_SUBS {
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

	subject := util.build_subject_with_timeframe(venue, symbol, channel, tf)
	subject_id := util.subject_id64(subject)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_web_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	if state.active_count >= WEB_MAX_SUBS {
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
	if state.heatmap_dirty do latest_pending += 1
	if state.vpvr_dirty do latest_pending += 1
	if state.candle_ring_count > 0 do latest_pending += 1
	out^ = ports.MD_Runtime_Metrics{
		active_subs       = state.active_count,
		trade_backlog     = state.trade_count,
		candle_backlog    = state.candle_ring_count,
		// Include JS bridge queue drops so metrics reflect end-to-end pressure.
		drop_count        = state.drop_count + queue_drop_count,
		reconnect_count   = state.reconnect_count,
		latest_pending    = latest_pending,
		parse_error_count = state.parse_error_count,
		subscribe_ack_count = state.subscribe_ack_count,
		parsed_msgs_total = state.parsed_msgs_total,
		parsed_bytes_total = state.parsed_bytes_total,
		msg_rate          = state.msg_rate,
		bytes_rate        = state.bytes_rate,
		last_msg_ts_ms   = state.last_msg_ts_ms,
		rtt_ms           = state.last_rtt_ms,
		lag_ms           = state.last_lag_ms,
		desync           = state.desync,
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
		subject = web_subject_for_channel(state, venue, symbol, channel)
	}
	defer delete(subject)

	// Remove from active subs.
	if idx >= 0 {
		last := state.active_count - 1
		web_free_sub_entry(&state.active_subs[idx])
		if idx != last {
			state.active_subs[idx] = state.active_subs[last]
			state.last_seq_by_sub[idx] = state.last_seq_by_sub[last]
			state.snapshot_logged_by_sub[idx] = state.snapshot_logged_by_sub[last]
		}
		state.active_subs[last] = {}
		state.last_seq_by_sub[last] = 0
		state.snapshot_logged_by_sub[last] = false
		state.active_count -= 1
	}

	if ws_state() != 2 do return
	web_send_unsubscribe(state, subject)
}

@(private = "file")
web_send_unsubscribe :: proc(state: ^MD_Web_State, subject: string) -> bool {
	state.rid_counter += 1
	buf: [256]u8
	n := 0
	prefix :: `{"op":"unsubscribe","subject":"`
	for c in prefix { buf[n] = u8(c); n += 1 }
	for c in subject { buf[n] = u8(c); n += 1 }
	mid :: `","request_id":"r`
	for c in mid { buf[n] = u8(c); n += 1 }
	rid_str := fmt.tprintf("%d", state.rid_counter)
	for c in rid_str { buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { buf[n] = u8(c); n += 1 }
	ws_send(raw_data(buf[:n]), i32(n))
	fmt.printf("[md-lifecycle] unsubscribe_sent subject=%s rid=r%s\n", subject, rid_str)
	return true
}

@(private = "file")
web_send_subscribe :: proc(state: ^MD_Web_State, subject: string) -> bool {
	state.rid_counter += 1
	buf: [256]u8
	n := 0
	prefix :: `{"op":"subscribe","subject":"`
	for c in prefix { buf[n] = u8(c); n += 1 }
	for c in subject { buf[n] = u8(c); n += 1 }
	mid :: `","request_id":"r`
	for c in mid { buf[n] = u8(c); n += 1 }
	rid_str := fmt.tprintf("%d", state.rid_counter)
	for c in rid_str { buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { buf[n] = u8(c); n += 1 }
	ws_send(raw_data(buf[:n]), i32(n))
	fmt.printf("[md-lifecycle] subscribe_sent subject=%s rid=r%s\n", subject, rid_str)
	return true
}

@(private = "file")
web_resubscribe_timeframe_channels :: proc(state: ^MD_Web_State) {
	if state == nil do return
	is_open := ws_state() == 2

	for i in 0 ..< state.active_count {
		entry := &state.active_subs[i]
		if entry.channel != .Heatmaps && entry.channel != .VPVR && entry.channel != .Candles do continue
		if entry.is_explicit_tf do continue // per-cell TF sub — don't clobber

		new_subject := util.build_subject_with_timeframe(entry.venue, entry.symbol, entry.channel, state.candle_tf_filter)
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
	n := 0
	prefix :: `{"op":"getrange","subject":"`
	for c in prefix { buf[n] = u8(c); n += 1 }
	for c in subject { buf[n] = u8(c); n += 1 }
	mid :: `","params":{"limit":`
	for c in mid { buf[n] = u8(c); n += 1 }
	limit_str := fmt.tprintf("%d", limit)
	for c in limit_str { buf[n] = u8(c); n += 1 }
	if end_ts > 0 {
		// WS server range contract expects to_ms; keep end_ts for backward compatibility.
		end_mid :: `,"to_ms":`
		for c in end_mid { buf[n] = u8(c); n += 1 }
		end_str := fmt.tprintf("%d", end_ts)
		for c in end_str { buf[n] = u8(c); n += 1 }
		end_mid_compat :: `,"end_ts":`
		for c in end_mid_compat { buf[n] = u8(c); n += 1 }
		for c in end_str { buf[n] = u8(c); n += 1 }
	}
	mid2 :: `},"request_id":"gr`
	for c in mid2 { buf[n] = u8(c); n += 1 }
	rid_str := fmt.tprintf("%d", state.rid_counter)
	for c in rid_str { buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { buf[n] = u8(c); n += 1 }

	ws_send(raw_data(buf[:n]), i32(n))
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
		state.connect_started_ms = 0
		state.first_data_logged = false
		fmt.println("[md-lifecycle] disconnect reason=transport_closed")
	}

	if !is_open && current_ws == 0 && state.active_count > 0 {
		// Tick down reconnect timer using real elapsed poll time (works with idle-throttled RAF).
		state.reconnect_timer -= poll_dt_s
		if state.reconnect_timer <= 0 {
			rc_hdr_buf: [128]u8
			rc_hdr_len := 0
			if len(state.api_key) > 0 {
				rc_prefix :: "X-API-Key: "
				for c in rc_prefix { rc_hdr_buf[rc_hdr_len] = u8(c); rc_hdr_len += 1 }
				for c in state.api_key { if rc_hdr_len < len(rc_hdr_buf) - 2 { rc_hdr_buf[rc_hdr_len] = u8(c); rc_hdr_len += 1 } }
				rc_hdr_buf[rc_hdr_len] = '\r'; rc_hdr_len += 1
				rc_hdr_buf[rc_hdr_len] = '\n'; rc_hdr_len += 1
			}
			url_raw := raw_data(transmute([]u8)state.ws_url)
			state.reconnect_count += 1
			fmt.printf("[md-lifecycle] reconnect_attempt count=%d url=%s\n", state.reconnect_count, state.ws_url)
			ws_connect(url_raw, i32(len(state.ws_url)), raw_data(rc_hdr_buf[:rc_hdr_len]), i32(rc_hdr_len))
			state.backoff_s = min(state.backoff_s * WEB_BACKOFF_MULTIPLIER, WEB_BACKOFF_MAX_S)
			state.reconnect_timer = state.backoff_s
		}
	}

	// If just connected, re-subscribe all active subs.
	if is_open && !state.was_connected {
		state.backoff_s = WEB_BACKOFF_INITIAL_S
		state.desync = false
		state.connect_started_ms = time.now()._nsec / 1_000_000
		state.first_data_logged = false
		for i in 0 ..< state.active_count {
			state.snapshot_logged_by_sub[i] = false
		}
		fmt.printf("[md-lifecycle] connect url=%s\n", state.ws_url)
		for i in 0 ..< state.active_count {
			sub := state.active_subs[i]
			if len(sub.subject) > 0 {
				web_send_subscribe(state, sub.subject)
			}
		}
		web_flush_pending_getrange(state)
	}
	state.was_connected = is_open

	// Drain messages from JS queue with a frame budget to avoid parse spikes.
	drain_start := time.tick_now()
	drained_msgs := 0
	hit_msg_budget := false
	hit_time_budget := false
	for drained_msgs < state.parse_max_msgs_per_poll {
		n := ws_poll_msg(raw_data(state.recv_buf[:]), i32(WEB_RECV_BUF_SIZE))
		if n <= 0 do break
		if n < 0 {
			// Negative = truncation signal from JS bridge.
			state.parse_error_count += 1
			continue
		}
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

	// Copy staging to events_buf (same as native_poll).
	out := 0

	// Reserve slots for latest-wins snapshots so trade bursts do not starve UI-critical panels.
	non_trade_pending := 0
	if state.ob_dirty      do non_trade_pending += 1
	if state.stats_dirty   do non_trade_pending += 1
	if state.heatmap_dirty do non_trade_pending += 1
	if state.vpvr_dirty    do non_trade_pending += 1
	non_trade_pending += min(state.candle_ring_count, WEB_CANDLE_RING_CAP)
	trade_emit_limit := len(events_buf) - non_trade_pending
	if trade_emit_limit < 0 do trade_emit_limit = 0

	// Drain trade ring.
	for out < trade_emit_limit && state.trade_count > 0 {
		oldest := (state.trade_write - state.trade_count + WEB_TRADE_RING_CAP) % WEB_TRADE_RING_CAP
		events_buf[out].source.subject_id = state.trade_ring_subject_id[oldest]
		events_buf[out].source.channel = .Trades
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
		events_buf[out].kind = .Orderbook_Snapshot
		events_buf[out].unix = ob.unix
		events_buf[out].data.ob = ports.MD_Orderbook_Event{
			ask_prices = raw_data(state.poll_ask_prices[:ob.ask_count]),
			ask_sizes  = raw_data(state.poll_ask_sizes[:ob.ask_count]),
			bid_prices = raw_data(state.poll_bid_prices[:ob.bid_count]),
			bid_sizes  = raw_data(state.poll_bid_sizes[:ob.bid_count]),
			ask_count  = ob.ask_count,
			bid_count  = ob.bid_count,
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
		events_buf[out].kind = .Stats
		events_buf[out].unix = st.unix
		events_buf[out].data.stats = ports.MD_Stats_Event{
			mark_price = st.mark_price,
			funding    = st.funding,
			tbuy       = st.tbuy,
			tsell      = st.tsell,
			unix       = st.unix,
		}
		state.stats_dirty = false
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
		events_buf[out].kind = .Heatmap
		events_buf[out].unix = hm.unix
		events_buf[out].data.heatmap = ports.MD_Heatmap_Event{
			prices      = raw_data(state.poll_hm_prices[:lc]),
			sizes       = raw_data(state.poll_hm_sizes[:lc]),
			level_count = lc,
			price_group = hm.price_group,
			min_price   = hm.min_price,
			max_price   = hm.max_price,
			max_size    = hm.max_size,
			unix        = hm.unix,
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
		events_buf[out].kind = .Range_Candle_Batch
		events_buf[out].data.range_candles = batch
		state.range_candle_dirty = false
		out += 1
	}

	return out
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
	return time.now()._nsec / 1_000_000
}

@(private = "file")
web_conn_status :: proc() -> ports.MD_Conn_Status {
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

@(private = "file")
web_update_parse_rates :: proc(state: ^MD_Web_State, now_ms: i64, bytes: int) {
	if state == nil do return
	safe_bytes := bytes
	if safe_bytes < 0 do safe_bytes = 0
	state.parsed_msgs_total += 1
	state.parsed_bytes_total += u64(safe_bytes)
	state.rate_window_msgs += 1
	state.rate_window_bytes += u64(safe_bytes)
	if state.rate_window_start_ms <= 0 {
		state.rate_window_start_ms = now_ms
		return
	}
	elapsed_ms := now_ms - state.rate_window_start_ms
	if elapsed_ms < 1 do return
	if elapsed_ms >= 1000 {
		secs := f64(elapsed_ms) / 1000.0
		state.msg_rate = f64(state.rate_window_msgs) / secs
		state.bytes_rate = f64(state.rate_window_bytes) / secs
		state.rate_window_msgs = 0
		state.rate_window_bytes = 0
		state.rate_window_start_ms = now_ms
	}
}

@(private = "file")
web_parse_result_has_data :: proc(kind: services.Parse_Result_Kind) -> bool {
	switch kind {
	case .Trade, .Orderbook, .Stats, .Heatmap, .VPVR, .Candle, .Range_Candle:
		return true
	case .None, .Ack, .Hello, .Heartbeat, .Health, .Error:
		return false
	}
	return false
}

@(private = "file")
web_apply_parse_result :: proc(state: ^MD_Web_State, raw: []u8) {
	telemetry: services.Parse_Telemetry
	result := services.parse_mr_message(raw, &telemetry)
	parsed_now_ms := time.now()._nsec / 1_000_000
	should_log_snapshot := false
	snapshot_subject := ""
	snapshot_seq := i64(0)
	snapshot_sid := u64(0)
	should_log_first_data := false
	first_data_delta_ms := i64(0)

	web_update_parse_rates(state, parsed_now_ms, len(raw))

	if telemetry.parse_errors > 0 {
		state.parse_error_count += telemetry.parse_errors
		if state.parse_error_count <= 3 || state.parse_error_count % 50 == 0 {
			preview_len := min(len(raw), 120)
			fmt.printf("[ws] Parse error #%d: %s\n", state.parse_error_count, string(raw[:preview_len]))
		}
	}

	if result.kind != .None {
		state.last_msg_ts_ms = parsed_now_ms
		if result.meta.server_ts_ms > 0 {
			state.last_server_ts_ms = result.meta.server_ts_ms
			if parsed_now_ms >= result.meta.server_ts_ms {
				state.last_lag_ms = parsed_now_ms - result.meta.server_ts_ms
			}
		}
		if result.meta.subject_id != 0 && result.meta.seq > 0 {
			if si := find_web_sub_by_subject_id(state, result.meta.subject_id); si >= 0 {
				prev_seq := state.last_seq_by_sub[si]
				if prev_seq > 0 && result.meta.seq > prev_seq + 1 {
					state.desync = true
				}
				state.last_seq_by_sub[si] = result.meta.seq
				if result.meta.is_snapshot && !state.snapshot_logged_by_sub[si] {
					state.snapshot_logged_by_sub[si] = true
					snapshot_subject = strings.clone(state.active_subs[si].subject)
					snapshot_seq = result.meta.seq
					snapshot_sid = result.meta.subject_id
					should_log_snapshot = true
				}
			}
		}
		if web_parse_result_has_data(result.kind) && state.connect_started_ms > 0 && !state.first_data_logged {
			first_data_delta_ms = max(parsed_now_ms - state.connect_started_ms, 0)
			state.first_data_logged = true
			should_log_first_data = true
		}
	}
	if should_log_snapshot {
		fmt.printf("[md-lifecycle] snapshot_recv subject=%s sid=%x seq=%d\n", snapshot_subject, snapshot_sid, snapshot_seq)
		delete(snapshot_subject)
	}
	if should_log_first_data {
		fmt.printf("[md-lifecycle] first_data_after_connect_ms=%d kind=%v sid=%x\n", first_data_delta_ms, result.kind, result.meta.subject_id)
	}

	switch result.kind {
	case .Ack:
		ack := result.data.ack
		state.subscribe_ack_count += 1
		fmt.printf("[md-lifecycle] ack_recv op=%s subject=%s\n", ack.op, ack.subject)
	case .Hello:
		// HELLO frame accepted for protocol compatibility.
	case .Heartbeat, .Health:
		ctrl := result.data.control
		if ctrl.rtt_ms > 0 do state.last_rtt_ms = ctrl.rtt_ms
		if ctrl.dropped > 0 && ctrl.dropped > state.drop_count do state.drop_count = ctrl.dropped
	case .Error:
		preview_len := min(len(raw), 200)
		fmt.printf("[ws] Error: %s\n", string(raw[:preview_len]))
	case .Trade:
		t := result.data.trade
		if state.trade_count < WEB_TRADE_RING_CAP {
			state.trade_ring_subject_id[state.trade_write] = t.subject_id
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
		}
	case .Orderbook:
		state.ob_staging = result.data.ob
		state.ob_dirty = true
	case .Stats:
		state.stats_staging = result.data.stats
		state.stats_dirty = true
	case .Heatmap:
		state.heatmap_staging = result.data.heatmap
		state.heatmap_dirty = true
	case .VPVR:
		state.vpvr_staging = result.data.vpvr
		state.vpvr_dirty = true
	case .Candle:
		if state.candle_ring_count < WEB_CANDLE_RING_CAP {
			state.candle_ring[state.candle_ring_write] = result.data.candle
			state.candle_ring_write = (state.candle_ring_write + 1) % WEB_CANDLE_RING_CAP
			state.candle_ring_count += 1
		} else {
			// Ring full — overwrite oldest (advance read pointer implicitly).
			state.candle_ring[state.candle_ring_write] = result.data.candle
			state.candle_ring_write = (state.candle_ring_write + 1) % WEB_CANDLE_RING_CAP
		}
	case .Range_Candle:
		state.range_candle_staging = result.data.range_candles
		state.range_candle_dirty = true
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
