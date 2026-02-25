package main

// WASM marketdata port — JS WebSocket bridge, single-threaded.
// Polls messages from a JS-side queue via ws_poll_msg foreign proc.
// Same staging pattern as native (ring + latest-wins), but no mutex needed.

import "core:encoding/json"
import "core:fmt"
import "core:math"
import "core:time"
import "mr:ports"
import "mr:util"

foreign import odin_env "odin_env"

@(default_calling_convention = "contextless")
foreign odin_env {
	ws_connect  :: proc(url_ptr: [^]u8, url_len: i32, hdr_ptr: [^]u8, hdr_len: i32) ---
	ws_send     :: proc(ptr: [^]u8, len: i32) ---
	ws_close    :: proc() ---
	ws_state    :: proc() -> i32 ---
	ws_poll_msg :: proc(buf_ptr: [^]u8, buf_len: i32) -> i32 ---
}

// --- Constants ---

WEB_TRADE_RING_CAP   :: 1024
WEB_OB_DEPTH         :: 50
WEB_HEATMAP_CAP      :: 512
WEB_VPVR_CAP         :: 256
WEB_MAX_SUBS         :: 64
WEB_RECV_BUF_SIZE    :: 32 * 1024 // 32 KB per message max

// Backend envelopes/payloads use unix milliseconds; core widgets use unix seconds
// (same convention used in the MarketMonkey-derived layers).
normalize_unix_seconds :: proc(ts: i64) -> i64 {
	if ts > 10_000_000_000 do return ts / 1000
	return ts
}

// --- State ---

Web_OB_Staging :: struct {
	ask_prices: [WEB_OB_DEPTH]f64,
	ask_sizes:  [WEB_OB_DEPTH]f64,
	bid_prices: [WEB_OB_DEPTH]f64,
	bid_sizes:  [WEB_OB_DEPTH]f64,
	ask_count:  int,
	bid_count:  int,
	last_price: f64,
	unix:       i64,
}

Web_Stats_Staging :: struct {
	mark_price: f64,
	funding:    f64,
	tbuy:       i64,
	tsell:      i64,
	unix:       i64,
}

Web_Heatmap_Staging :: struct {
	prices:      [WEB_HEATMAP_CAP]f64,
	sizes:       [WEB_HEATMAP_CAP]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	max_size:    f64,
	unix:        i64,
}

Web_VPVR_Staging :: struct {
	prices:      [WEB_VPVR_CAP]f64,
	buys:        [WEB_VPVR_CAP]f64,
	sells:       [WEB_VPVR_CAP]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	unix:        i64,
}

Web_Sub_Entry :: struct {
	venue:   string,
	symbol:  string,
	channel: ports.MD_Channel,
	subject: string,
}

MD_Web_State :: struct {
	// Trade ring buffer.
	trade_ring:  [WEB_TRADE_RING_CAP]ports.MD_Trade_Event,
	trade_write: int,
	trade_count: int,

	// Latest-wins staging.
	ob_staging:      Web_OB_Staging,
	ob_dirty:        bool,
	stats_staging:   Web_Stats_Staging,
	stats_dirty:     bool,
	heatmap_staging: Web_Heatmap_Staging,
	heatmap_dirty:   bool,
	vpvr_staging:    Web_VPVR_Staging,
	vpvr_dirty:      bool,

	// Connection.
	ws_url:  string,
	api_key: string,

	// Subscription tracking for reconnect re-subscribe.
	active_subs:  [WEB_MAX_SUBS]Web_Sub_Entry,
	active_count: int,
	rid_counter:  u32,

	// Receive buffer (reused each poll).
	recv_buf: [WEB_RECV_BUF_SIZE]u8,

	// Temp arrays for poll output (avoids aliasing staging).
	poll_ask_prices:  [WEB_OB_DEPTH]f64,
	poll_ask_sizes:   [WEB_OB_DEPTH]f64,
	poll_bid_prices:  [WEB_OB_DEPTH]f64,
	poll_bid_sizes:   [WEB_OB_DEPTH]f64,
	poll_hm_prices:   [WEB_HEATMAP_CAP]f64,
	poll_hm_sizes:    [WEB_HEATMAP_CAP]f64,
	poll_vpvr_prices: [WEB_VPVR_CAP]f64,
	poll_vpvr_buys:   [WEB_VPVR_CAP]f64,
	poll_vpvr_sells:  [WEB_VPVR_CAP]f64,

	// Reconnection tracking.
	was_connected:   bool,
	reconnect_timer: f64, // seconds until next reconnect attempt
	backoff_s:       f64, // current backoff in seconds
}

@(private = "file")
g_web_state: ^MD_Web_State

// --- Public API ---

make_marketdata_web :: proc(url: string, api_key: string = "") -> ports.Marketdata_Port {
	state := new(MD_Web_State)
	state.ws_url = url
	state.api_key = api_key
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
	}
}

// --- Port implementation ---

@(private = "file")
web_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_web_state
	if state == nil do return false

	subject := util.build_subject(venue, symbol, channel)

	// Track for reconnect.
	if state.active_count < WEB_MAX_SUBS {
		state.active_subs[state.active_count] = Web_Sub_Entry{
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
web_unsubscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) {
	state := g_web_state
	if state == nil do return

	subject := util.build_subject(venue, symbol, channel)

	// Remove from active subs.
	for i in 0 ..< state.active_count {
		if state.active_subs[i].subject == subject {
			state.active_subs[i] = state.active_subs[state.active_count - 1]
			state.active_count -= 1
			break
		}
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

	// Reconnection: when disconnected, count down and try again.
	current_ws := ws_state()
	is_open := current_ws == 2

	if !is_open && state.was_connected {
		// Just disconnected — start backoff.
		state.backoff_s = 0.5
		state.reconnect_timer = state.backoff_s
		state.was_connected = false
	}

	if !is_open && current_ws == 0 && state.active_count > 0 {
		// Tick down reconnect timer (~16ms per frame).
		state.reconnect_timer -= 1.0 / 60.0
		if state.reconnect_timer <= 0 {
			hdr := ""
			if len(state.api_key) > 0 {
				hdr = fmt.tprintf("X-API-Key: %s\r\n", state.api_key)
			}
			url_raw := raw_data(transmute([]u8)state.ws_url)
			hdr_raw := raw_data(transmute([]u8)hdr)
			ws_connect(url_raw, i32(len(state.ws_url)), hdr_raw, i32(len(hdr)))
			state.backoff_s = min(state.backoff_s * 2.0, 30.0)
			state.reconnect_timer = state.backoff_s
		}
	}

	// If just connected, re-subscribe all active subs.
	if is_open && !state.was_connected {
		state.backoff_s = 0.5
		for i in 0 ..< state.active_count {
			sub := state.active_subs[i]
			if len(sub.subject) > 0 {
				web_send_subscribe(state, sub.subject)
			}
		}
	}
	state.was_connected = is_open

	// Drain messages from JS queue.
	for {
		n := ws_poll_msg(raw_data(state.recv_buf[:]), i32(WEB_RECV_BUF_SIZE))
		if n <= 0 do break
		web_parse_message(state, state.recv_buf[:n])
	}

	// Copy staging to events_buf (same as native_poll).
	out := 0

	// Drain trade ring.
	for out < len(events_buf) && state.trade_count > 0 {
		oldest := (state.trade_write - state.trade_count + WEB_TRADE_RING_CAP) % WEB_TRADE_RING_CAP
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
		lc := min(hm.level_count, WEB_HEATMAP_CAP)
		for i in 0 ..< lc {
			state.poll_hm_prices[i] = hm.prices[i]
			state.poll_hm_sizes[i]  = hm.sizes[i]
		}
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
		lc := min(vp.level_count, WEB_VPVR_CAP)
		for i in 0 ..< lc {
			state.poll_vpvr_prices[i] = vp.prices[i]
			state.poll_vpvr_buys[i]   = vp.buys[i]
			state.poll_vpvr_sells[i]  = vp.sells[i]
		}
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

	return out
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

// --- Message parsing (mirrors native parse_mr_message) ---

@(private = "file")
web_parse_message :: proc(state: ^MD_Web_State, raw: []u8) {
	env: util.MR_Envelope
	if json.unmarshal(raw, &env) != nil do return

	ft := util.parse_frame_type(env.type_str)

	switch ft {
	case .Ack:
		fmt.printf("[ws] Ack: op=%s subject=%s\n", env.op, env.subject)
		return
	case .Error:
		preview_len := min(len(raw), 200)
		fmt.printf("[ws] Error: %s\n", string(raw[:preview_len]))
		return
	case .Range, .Last, .Unknown:
		return
	case .Event, .Snapshot:
		// Fall through to payload parsing.
	}

	stream := util.subject_stream_type(env.subject)

	switch stream {
	case "marketdata.trade":
		web_handle_trade(state, raw, env.ts_ingest)
	case "marketdata.bookdelta":
		web_handle_book_delta(state, raw, env.ts_ingest)
	case "aggregation.stats":
		web_handle_stats(state, raw, env.ts_ingest)
	case "insights.heatmap_snapshot":
		web_handle_heatmap(state, raw, env.ts_ingest)
	case "insights.volume_profile_snapshot":
		web_handle_vpvr(state, raw, env.ts_ingest)
	}
}

@(private = "file")
web_handle_trade :: proc(state: ^MD_Web_State, raw: []u8, ts: i64) {
	frame: util.MR_Trade_Frame
	if json.unmarshal(raw, &frame) != nil do return
	trade := frame.payload

	unix := normalize_unix_seconds(trade.timestamp_ms if trade.timestamp_ms != 0 else ts)

	if state.trade_count < WEB_TRADE_RING_CAP {
		state.trade_ring[state.trade_write] = ports.MD_Trade_Event{
			price  = trade.price,
			qty    = trade.size,
			is_buy = trade.side == "buy",
			unix   = unix,
		}
		state.trade_write = (state.trade_write + 1) % WEB_TRADE_RING_CAP
		state.trade_count += 1
	}
}

@(private = "file")
web_handle_book_delta :: proc(state: ^MD_Web_State, raw: []u8, ts: i64) {
	frame: util.MR_Book_Delta_Frame
	if json.unmarshal(raw, &frame) != nil do return
	bd := frame.payload

	unix := normalize_unix_seconds(bd.timestamp_ms if bd.timestamp_ms != 0 else ts)

	ac := min(len(bd.asks), WEB_OB_DEPTH)
	bc := min(len(bd.bids), WEB_OB_DEPTH)
	state.ob_staging.ask_count = ac
	state.ob_staging.bid_count = bc
	state.ob_staging.unix = unix

	if ac > 0 && bc > 0 {
		state.ob_staging.last_price = (bd.asks[0].price + bd.bids[0].price) / 2.0
	}

	for i in 0 ..< ac {
		state.ob_staging.ask_prices[i] = bd.asks[i].price
		state.ob_staging.ask_sizes[i]  = bd.asks[i].size
	}
	for i in 0 ..< bc {
		state.ob_staging.bid_prices[i] = bd.bids[i].price
		state.ob_staging.bid_sizes[i]  = bd.bids[i].size
	}
	state.ob_dirty = true
}

@(private = "file")
web_handle_stats :: proc(state: ^MD_Web_State, raw: []u8, ts: i64) {
	frame: util.MR_Stats_Frame
	if json.unmarshal(raw, &frame) != nil do return
	s := frame.payload
	if s.window_start_ts == 0 && s.window_end_ts == 0 {
		wrapped: util.MR_Stats_Frame_Wrapped
		if json.unmarshal(raw, &wrapped) == nil {
			s = wrapped.payload.stats
		}
	}

	unix := normalize_unix_seconds(s.window_end_ts if s.window_end_ts != 0 else ts)

	state.stats_staging = Web_Stats_Staging{
		mark_price = s.mark_price_close,
		funding    = s.funding_rate_last,
		tbuy       = i64(s.liq_buy_volume),
		tsell      = i64(s.liq_sell_volume),
		unix       = unix,
	}
	state.stats_dirty = true
}

@(private = "file")
web_handle_heatmap :: proc(state: ^MD_Web_State, raw: []u8, ts: i64) {
	frame: util.MR_Heatmap_Frame
	if json.unmarshal(raw, &frame) != nil do return
	hm := frame.payload

	unix := normalize_unix_seconds(hm.window_end_ts if hm.window_end_ts != 0 else ts)
	lc := min(len(hm.cells), WEB_HEATMAP_CAP)

	min_p := math.F64_MAX
	max_p := f64(0)
	max_s := f64(0)
	price_group := f64(0)

	for i in 0 ..< lc {
		c := hm.cells[i]
		mid := (c.price_bucket_low + c.price_bucket_high) / 2.0
		total := c.bid_liquidity + c.ask_liquidity + c.trade_volume

		if i == 0 {
			price_group = c.price_bucket_high - c.price_bucket_low
		}
		if mid < min_p do min_p = mid
		if mid > max_p do max_p = mid
		if total > max_s do max_s = total
	}

	state.heatmap_staging.level_count = lc
	state.heatmap_staging.price_group = price_group
	state.heatmap_staging.min_price = min_p if lc > 0 else 0
	state.heatmap_staging.max_price = max_p
	state.heatmap_staging.max_size = max_s
	state.heatmap_staging.unix = unix
	for i in 0 ..< lc {
		c := hm.cells[i]
		state.heatmap_staging.prices[i] = (c.price_bucket_low + c.price_bucket_high) / 2.0
		state.heatmap_staging.sizes[i]  = c.bid_liquidity + c.ask_liquidity + c.trade_volume
	}
	state.heatmap_dirty = true
}

@(private = "file")
web_handle_vpvr :: proc(state: ^MD_Web_State, raw: []u8, ts: i64) {
	frame: util.MR_VPVR_Frame
	if json.unmarshal(raw, &frame) != nil do return
	vp := frame.payload

	unix := normalize_unix_seconds(vp.window_end_ts if vp.window_end_ts != 0 else ts)
	lc := min(len(vp.buckets), WEB_VPVR_CAP)

	min_p := math.F64_MAX
	max_p := f64(0)
	price_group := f64(0)

	for i in 0 ..< lc {
		b := vp.buckets[i]
		mid := (b.price_low + b.price_high) / 2.0
		if i == 0 {
			price_group = b.price_high - b.price_low
		}
		if mid < min_p do min_p = mid
		if mid > max_p do max_p = mid
	}

	state.vpvr_staging.level_count = lc
	state.vpvr_staging.price_group = price_group
	state.vpvr_staging.min_price = min_p if lc > 0 else 0
	state.vpvr_staging.max_price = max_p
	state.vpvr_staging.unix = unix
	for i in 0 ..< lc {
		b := vp.buckets[i]
		state.vpvr_staging.prices[i] = (b.price_low + b.price_high) / 2.0
		state.vpvr_staging.buys[i]   = b.buy_volume
		state.vpvr_staging.sells[i]  = b.sell_volume
	}
	state.vpvr_dirty = true
}
