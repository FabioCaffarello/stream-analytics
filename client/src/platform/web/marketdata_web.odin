package main

// WASM marketdata port — JS WebSocket bridge, single-threaded.
// Polls messages from a JS-side queue via ws_poll_msg foreign proc.
// Same staging pattern as native (ring + latest-wins), but no mutex needed.

import "core:fmt"
import "core:strconv"
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
	ws_poll_msg :: proc(buf_ptr: [^]u8, buf_len: i32) -> i32 ---
	url_query_param :: proc(name_ptr: [^]u8, name_len: i32, out_ptr: [^]u8, out_cap: i32) -> i32 ---
}

// --- Constants ---

WEB_TRADE_RING_CAP   :: 1024
WEB_MAX_SUBS         :: 128
WEB_RECV_BUF_SIZE    :: 128 * 1024 // 128 KB per message max
WEB_PARSE_MAX_MSGS_PER_POLL :: 64
WEB_PARSE_TIME_BUDGET       :: 2 * time.Millisecond

// Reconnection backoff.
WEB_BACKOFF_INITIAL_S :: 0.5
WEB_BACKOFF_MAX_S     :: 30.0
WEB_BACKOFF_MULTIPLIER :: 2.0

// Candle timeframe filter: delivery routes /raw candles, show only this TF on chart.
CANDLE_TF_FILTER :: "1m"

// --- State ---

Web_Sub_Entry :: struct {
	subject_id: u64,
	venue:   string,
	symbol:  string,
	channel: ports.MD_Channel,
	subject: string,
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
	candle_staging:  services.Parsed_Candle,
	candle_dirty:    bool,

	// Connection.
	ws_url:  string,
	api_key: string,

	// Subscription tracking for reconnect re-subscribe.
	active_subs:  [WEB_MAX_SUBS]Web_Sub_Entry,
	active_count: int,
	rid_counter:       u32,
	drop_count:        int,
	reconnect_count:   int,
	parse_error_count: int,

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
	state.ws_url = url
	state.api_key = api_key
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
	hdr := ""
	if len(api_key) > 0 {
		hdr = fmt.tprintf("X-API-Key: %s\r\n", api_key)
	}
	url_raw := raw_data(transmute([]u8)url)
	hdr_raw := raw_data(transmute([]u8)hdr)
	ws_connect(url_raw, i32(len(url)), hdr_raw, i32(len(hdr)))

	return ports.Marketdata_Port{
		subscribe   = web_subscribe,
		unsubscribe = web_unsubscribe,
		poll        = web_poll,
		now_ms      = web_now_ms,
		conn_status = web_conn_status,
		metrics     = web_metrics,
		describe_stream = web_describe_stream,
		shutdown    = web_shutdown,
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
find_web_sub_by_subject_id :: proc(state: ^MD_Web_State, subject_id: u64) -> int {
	for i in 0 ..< state.active_count {
		if state.active_subs[i].subject_id == subject_id do return i
	}
	return -1
}

@(private = "file")
web_shutdown :: proc() {
	state := g_web_state
	if state == nil do return
	ws_close()
	state.active_count = 0
	state.was_connected = false
	state.reconnect_timer = 0
	g_web_state = nil
}

@(private = "file")
web_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_web_state
	if state == nil do return false

	subject := util.build_subject(venue, symbol, channel)
	subject_id := util.subject_id64(subject)

	// Track for reconnect (dedup by subject).
	if find_web_sub_by_subject(state, subject) == -1 && state.active_count < WEB_MAX_SUBS {
		state.active_subs[state.active_count] = Web_Sub_Entry{
			subject_id = subject_id,
			venue   = venue,
			symbol  = symbol,
			channel = channel,
			subject = subject,
		}
		state.active_count += 1
	}

	if ws_state() != 2 do return false // not open
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
	latest_pending := 0
	if state.ob_dirty do latest_pending += 1
	if state.stats_dirty do latest_pending += 1
	if state.heatmap_dirty do latest_pending += 1
	if state.vpvr_dirty do latest_pending += 1
	if state.candle_dirty do latest_pending += 1
	out^ = ports.MD_Runtime_Metrics{
		active_subs       = state.active_count,
		trade_backlog     = state.trade_count,
		drop_count        = state.drop_count,
		reconnect_count   = state.reconnect_count,
		latest_pending    = latest_pending,
		parse_error_count = state.parse_error_count,
	}
	return true
}

@(private = "file")
web_unsubscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) {
	state := g_web_state
	if state == nil do return

	subject := util.build_subject(venue, symbol, channel)

	// Remove from active subs.
	if idx := find_web_sub_by_subject(state, subject); idx >= 0 {
		state.active_subs[idx] = state.active_subs[state.active_count - 1]
		state.active_count -= 1
	}

	if ws_state() != 2 do return

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
	}

	if !is_open && current_ws == 0 && state.active_count > 0 {
		// Tick down reconnect timer using real elapsed poll time (works with idle-throttled RAF).
		state.reconnect_timer -= poll_dt_s
		if state.reconnect_timer <= 0 {
			hdr := ""
			if len(state.api_key) > 0 {
				hdr = fmt.tprintf("X-API-Key: %s\r\n", state.api_key)
			}
				url_raw := raw_data(transmute([]u8)state.ws_url)
				hdr_raw := raw_data(transmute([]u8)hdr)
				state.reconnect_count += 1
				ws_connect(url_raw, i32(len(state.ws_url)), hdr_raw, i32(len(hdr)))
				state.backoff_s = min(state.backoff_s * WEB_BACKOFF_MULTIPLIER, WEB_BACKOFF_MAX_S)
				state.reconnect_timer = state.backoff_s
		}
	}

	// If just connected, re-subscribe all active subs.
	if is_open && !state.was_connected {
		state.backoff_s = WEB_BACKOFF_INITIAL_S
		for i in 0 ..< state.active_count {
			sub := state.active_subs[i]
			if len(sub.subject) > 0 {
				web_send_subscribe(state, sub.subject)
			}
		}
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
	if state.candle_dirty  do non_trade_pending += 1
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

	// Candle.
	if state.candle_dirty && out < len(events_buf) {
		cs := state.candle_staging
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
		state.candle_dirty = false
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
web_apply_parse_result :: proc(state: ^MD_Web_State, raw: []u8) {
	telemetry: services.Parse_Telemetry
	result := services.parse_mr_message(raw, &telemetry, CANDLE_TF_FILTER)

	if telemetry.parse_errors > 0 {
		state.parse_error_count += telemetry.parse_errors
		if state.parse_error_count <= 3 || state.parse_error_count % 50 == 0 {
			preview_len := min(len(raw), 120)
			fmt.printf("[ws] Parse error #%d: %s\n", state.parse_error_count, string(raw[:preview_len]))
		}
	}

	switch result.kind {
	case .Ack:
		ack := result.data.ack
		fmt.printf("[ws] Ack: op=%s subject=%s\n", ack.op, ack.subject)
	case .Error:
		preview_len := min(len(raw), 200)
		fmt.printf("[ws] Error: %s\n", string(raw[:preview_len]))
	case .Trade:
		t := result.data.trade
		if state.trade_count < WEB_TRADE_RING_CAP {
			state.trade_count += 1
		} else {
			state.drop_count += 1
		}
		state.trade_ring_subject_id[state.trade_write] = t.subject_id
		state.trade_ring[state.trade_write] = ports.MD_Trade_Event{
			price  = t.price,
			qty    = t.qty,
			is_buy = t.is_buy,
			unix   = t.unix,
		}
		state.trade_write = (state.trade_write + 1) % WEB_TRADE_RING_CAP
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
		state.candle_staging = result.data.candle
		state.candle_dirty = true
	case .None:
		// Ignored (range, last, unknown frame types).
	}
}
