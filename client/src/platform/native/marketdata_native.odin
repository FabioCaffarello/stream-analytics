package main

// Native marketdata port — WebSocket client with background reader thread.
// Ring buffer for trades, single-slot latest-wins for orderbook/stats/heatmap/vpvr.
// Automatic reconnection with exponential backoff + re-subscribe on reconnect.

import "core:encoding/json"
import "core:fmt"
import "core:math"
import "core:sync"
import "core:thread"
import "core:time"
import "mr:ports"
import "mr:util"

TRADE_RING_CAP :: 1024
OB_STAGING_DEPTH :: 50
HEATMAP_STAGING_CAP :: 512
VPVR_STAGING_CAP :: 256
MAX_SUBS :: 64

// --- Reconnection constants ---

BACKOFF_INITIAL_MS :: 500
BACKOFF_MAX_MS     :: 30_000
BACKOFF_MULTIPLIER :: 2

// Backend envelopes/payloads use unix milliseconds; core widgets use unix seconds
// (same convention used in the MarketMonkey-derived layers).
normalize_unix_seconds :: proc(ts: i64) -> i64 {
	if ts > 10_000_000_000 do return ts / 1000
	return ts
}

// --- Internal state (package-level singleton) ---

Conn_State :: enum u8 {
	Disconnected,
	Connecting,
	Connected,
	Backoff_Wait,
}

OB_Staging :: struct {
	ask_prices: [OB_STAGING_DEPTH]f64,
	ask_sizes:  [OB_STAGING_DEPTH]f64,
	bid_prices: [OB_STAGING_DEPTH]f64,
	bid_sizes:  [OB_STAGING_DEPTH]f64,
	ask_count:  int,
	bid_count:  int,
	last_price: f64,
	unix:       i64,
}

Stats_Staging :: struct {
	mark_price: f64,
	funding:    f64,
	tbuy:       i64,
	tsell:      i64,
	unix:       i64,
}

Heatmap_Staging :: struct {
	prices:      [HEATMAP_STAGING_CAP]f64,
	sizes:       [HEATMAP_STAGING_CAP]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	max_size:    f64,
	unix:        i64,
}

VPVR_Staging :: struct {
	prices: [VPVR_STAGING_CAP]f64,
	buys:   [VPVR_STAGING_CAP]f64,
	sells:  [VPVR_STAGING_CAP]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	unix:        i64,
}

Sub_Entry :: struct {
	venue:   string,
	symbol:  string,
	channel: ports.MD_Channel,
	subject: string,
}

MD_Native_State :: struct {
	// Trade ring buffer (SPSC: writer=background, reader=main).
	trade_ring:  [TRADE_RING_CAP]ports.MD_Trade_Event,
	trade_write: int,
	trade_count: int,

	// Orderbook snapshot (latest-wins, single-slot).
	ob_staging: OB_Staging,
	ob_dirty:   bool,

	// Stats staging (latest-wins).
	stats_staging: Stats_Staging,
	stats_dirty:   bool,

	// Heatmap staging (latest-wins).
	heatmap_staging: Heatmap_Staging,
	heatmap_dirty:   bool,

	// VPVR staging (latest-wins).
	vpvr_staging: VPVR_Staging,
	vpvr_dirty:   bool,

	// Connection.
	conn:        WS_Connection,
	conn_state:  Conn_State,
	ws_url:      string,
	api_key:     string,
	should_stop: bool,
	mu:          sync.Mutex,
	drop_count:  int,

	// Reconnection.
	backoff_ms:      int,
	reconnect_count: int,

	// Subscription tracking for reconnect re-subscribe.
	active_subs:  [MAX_SUBS]Sub_Entry,
	active_count: int,

	// Request ID counter.
	rid_counter: u32,

	// Temp arrays for poll() — main thread only.
	poll_ask_prices:    [OB_STAGING_DEPTH]f64,
	poll_ask_sizes:     [OB_STAGING_DEPTH]f64,
	poll_bid_prices:    [OB_STAGING_DEPTH]f64,
	poll_bid_sizes:     [OB_STAGING_DEPTH]f64,
	poll_hm_prices:     [HEATMAP_STAGING_CAP]f64,
	poll_hm_sizes:      [HEATMAP_STAGING_CAP]f64,
	poll_vpvr_prices:   [VPVR_STAGING_CAP]f64,
	poll_vpvr_buys:     [VPVR_STAGING_CAP]f64,
	poll_vpvr_sells:    [VPVR_STAGING_CAP]f64,
}

@(private = "file")
g_md_state: ^MD_Native_State

// --- Public API ---

make_marketdata_native :: proc(url: string, api_key: string = "") -> ports.Marketdata_Port {
	state := new(MD_Native_State)
	state.ws_url = url
	state.api_key = api_key
	state.backoff_ms = BACKOFF_INITIAL_MS
	g_md_state = state

	// Build auth header string.
	extra_hdr := ""
	if len(api_key) > 0 {
		extra_hdr = fmt.tprintf("X-API-Key: %s\r\n", api_key)
	}

	// Attempt initial connection.
	state.conn_state = .Connecting
	conn, err := ws_dial(url, extra_hdr)
	if err != nil {
		fmt.printf("[marketdata] WS connect failed (err=%v), will retry in background\n", err)
		state.conn_state = .Disconnected
	} else {
		fmt.printf("[marketdata] Connected to %s\n", url)
		state.conn = conn
		state.conn_state = .Connected
		state.backoff_ms = BACKOFF_INITIAL_MS
	}

	// Start background reader thread (handles reconnection).
	t := thread.create(reader_thread_proc)
	if t != nil {
		t.data = rawptr(state)
		thread.start(t)
	}

	return ports.Marketdata_Port{
		subscribe   = native_subscribe,
		unsubscribe = native_unsubscribe,
		poll        = native_poll,
		now_ms      = native_now_ms,
		conn_status = native_conn_status,
	}
}

// --- Port implementation ---

@(private = "file")
native_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_md_state
	if state == nil do return false

	subject := util.build_subject(venue, symbol, channel)

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	// Track subscription for reconnect re-subscribe.
	if state.active_count < MAX_SUBS {
		state.active_subs[state.active_count] = Sub_Entry{
			venue   = venue,
			symbol  = symbol,
			channel = channel,
			subject = subject,
		}
		state.active_count += 1
	}

	if state.conn_state != .Connected do return false

	return send_subscribe(state, subject)
}

@(private = "file")
native_unsubscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) {
	state := g_md_state
	if state == nil do return

	subject := util.build_subject(venue, symbol, channel)

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	// Remove from active subs.
	for i in 0 ..< state.active_count {
		if state.active_subs[i].subject == subject {
			state.active_subs[i] = state.active_subs[state.active_count - 1]
			state.active_count -= 1
			break
		}
	}

	if state.conn_state != .Connected do return

	state.rid_counter += 1
	unsub_buf: [256]u8
	un := 0
	unsub_prefix :: `{"op":"unsubscribe","subject":"`
	for c in unsub_prefix { unsub_buf[un] = u8(c); un += 1 }
	for c in subject { unsub_buf[un] = u8(c); un += 1 }
	unsub_mid :: `","request_id":"r`
	for c in unsub_mid { unsub_buf[un] = u8(c); un += 1 }
	unsub_rid := fmt.tprintf("%d", state.rid_counter)
	for c in unsub_rid { unsub_buf[un] = u8(c); un += 1 }
	unsub_suffix :: `"}`
	for c in unsub_suffix { unsub_buf[un] = u8(c); un += 1 }
	ws_write_text(state.conn, string(unsub_buf[:un]))
}

@(private = "file")
send_subscribe :: proc(state: ^MD_Native_State, subject: string) -> bool {
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

	msg := string(buf[:n])
	err := ws_write_text(state.conn, msg)
	return err == nil
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

		events_buf[n].kind = .Orderbook_Snapshot
		events_buf[n].unix = ob.unix
		events_buf[n].data.ob = ports.MD_Orderbook_Event{
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
		n += 1
	}

	// Emit latest stats if dirty.
	if state.stats_dirty && n < len(events_buf) {
		st := state.stats_staging
		events_buf[n].kind = .Stats
		events_buf[n].unix = st.unix
		events_buf[n].data.stats = ports.MD_Stats_Event{
			mark_price = st.mark_price,
			funding    = st.funding,
			tbuy       = st.tbuy,
			tsell      = st.tsell,
			unix       = st.unix,
		}
		state.stats_dirty = false
		n += 1
	}

	// Emit latest heatmap if dirty.
	if state.heatmap_dirty && n < len(events_buf) {
		hm := state.heatmap_staging
		lc := min(hm.level_count, HEATMAP_STAGING_CAP)

		for i in 0 ..< lc {
			state.poll_hm_prices[i] = hm.prices[i]
			state.poll_hm_sizes[i]  = hm.sizes[i]
		}

		events_buf[n].kind = .Heatmap
		events_buf[n].unix = hm.unix
		events_buf[n].data.heatmap = ports.MD_Heatmap_Event{
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
		n += 1
	}

	// Emit latest VPVR if dirty.
	if state.vpvr_dirty && n < len(events_buf) {
		vp := state.vpvr_staging
		lc := min(vp.level_count, VPVR_STAGING_CAP)

		for i in 0 ..< lc {
			state.poll_vpvr_prices[i] = vp.prices[i]
			state.poll_vpvr_buys[i]   = vp.buys[i]
			state.poll_vpvr_sells[i]  = vp.sells[i]
		}

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

	return n
}

@(private = "file")
native_now_ms :: proc() -> i64 {
	return time.now()._nsec / 1_000_000
}

@(private = "file")
native_conn_status :: proc() -> ports.MD_Conn_Status {
	state := g_md_state
	if state == nil do return .Offline

	sync.lock(&state.mu)
	cs := state.conn_state
	sync.unlock(&state.mu)

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

	for !state.should_stop {
		sync.lock(&state.mu)
		cs := state.conn_state
		sync.unlock(&state.mu)

		if cs != .Connected {
			attempt_reconnect(state)
			continue
		}

		opcode, payload, err := ws_read_message(state.conn)
		if err != nil {
			if err == .Read_Conn_Closed {
				fmt.println("[marketdata] Connection closed by server")
			} else {
				fmt.printf("[marketdata] Read error: %v\n", err)
			}
			sync.lock(&state.mu)
			state.conn_state = .Disconnected
			state.backoff_ms = BACKOFF_INITIAL_MS
			sync.unlock(&state.mu)
			ws_close(&state.conn)
			continue
		}

		if opcode != 0x1 do continue // Only handle text frames.

		parse_mr_message(state, payload)
		free_all(context.temp_allocator)
	}
}

@(private = "file")
attempt_reconnect :: proc(state: ^MD_Native_State) {
	sync.lock(&state.mu)
	state.conn_state = .Backoff_Wait
	backoff := state.backoff_ms
	state.reconnect_count += 1
	count := state.reconnect_count
	sync.unlock(&state.mu)

	fmt.printf("[marketdata] Reconnecting in %dms (attempt %d)\n", backoff, count)
	time.sleep(time.Duration(backoff) * time.Millisecond)

	if state.should_stop do return

	extra_hdr := ""
	if len(state.api_key) > 0 {
		extra_hdr = fmt.tprintf("X-API-Key: %s\r\n", state.api_key)
	}

	sync.lock(&state.mu)
	state.conn_state = .Connecting
	sync.unlock(&state.mu)

	conn, err := ws_dial(state.ws_url, extra_hdr)
	if err != nil {
		fmt.printf("[marketdata] Reconnect failed (err=%v)\n", err)
		sync.lock(&state.mu)
		state.conn_state = .Disconnected
		state.backoff_ms = min(backoff * BACKOFF_MULTIPLIER, BACKOFF_MAX_MS)
		sync.unlock(&state.mu)
		return
	}

	fmt.printf("[marketdata] Reconnected to %s\n", state.ws_url)
	sync.lock(&state.mu)
	state.conn = conn
	state.conn_state = .Connected
	state.backoff_ms = BACKOFF_INITIAL_MS
	state.reconnect_count = 0

	// Re-subscribe all active subscriptions.
	sub_count := state.active_count
	sync.unlock(&state.mu)

	for i in 0 ..< sub_count {
		sync.lock(&state.mu)
		subject := state.active_subs[i].subject
		sync.unlock(&state.mu)
		if len(subject) > 0 {
			sync.lock(&state.mu)
			send_subscribe(state, subject)
			sync.unlock(&state.mu)
		}
	}
}

// --- MR protocol JSON parsing ---
// Two-pass: (1) unmarshal envelope for type+subject, (2) unmarshal same raw
// bytes into typed frame struct (json.unmarshal ignores unknown fields).

@(private = "file")
parse_mr_message :: proc(state: ^MD_Native_State, raw: []u8) {
	// Pass 1: envelope only.
	env: util.MR_Envelope
	if json.unmarshal(raw, &env) != nil do return

	ft := util.parse_frame_type(env.type_str)

	switch ft {
	case .Ack:
		fmt.printf("[marketdata] Ack: op=%s subject=%s\n", env.op, env.subject)
		return
	case .Error:
		preview_len := min(len(raw), 200)
		fmt.printf("[marketdata] Error: %s\n", string(raw[:preview_len]))
		return
	case .Range, .Last, .Unknown:
		return
	case .Event, .Snapshot:
		// Fall through to payload parsing.
	}

	// Pass 2: re-parse same bytes into typed frame struct.
	stream := util.subject_stream_type(env.subject)

	switch stream {
	case "marketdata.trade":
		handle_trade(state, raw, env.ts_ingest)
	case "marketdata.bookdelta":
		handle_book_delta(state, raw, env.ts_ingest)
	case "aggregation.stats":
		handle_stats(state, raw, env.ts_ingest)
	case "insights.heatmap_snapshot":
		handle_heatmap(state, raw, env.ts_ingest)
	case "insights.volume_profile_snapshot":
		handle_vpvr(state, raw, env.ts_ingest)
	}
}

@(private = "file")
handle_trade :: proc(state: ^MD_Native_State, raw: []u8, ts: i64) {
	frame: util.MR_Trade_Frame
	if json.unmarshal(raw, &frame) != nil do return
	trade := frame.payload

	unix := normalize_unix_seconds(trade.timestamp_ms if trade.timestamp_ms != 0 else ts)

	sync.lock(&state.mu)
	if state.trade_count < TRADE_RING_CAP {
		state.trade_ring[state.trade_write] = ports.MD_Trade_Event{
			price  = trade.price,
			qty    = trade.size,
			is_buy = trade.side == "buy",
			unix   = unix,
		}
		state.trade_write = (state.trade_write + 1) % TRADE_RING_CAP
		state.trade_count += 1
	} else {
		state.drop_count += 1
	}
	sync.unlock(&state.mu)

	if state.drop_count > 0 && state.drop_count % 100 == 0 {
		fmt.printf("[marketdata] Dropped %d events (ring full)\n", state.drop_count)
	}
}

@(private = "file")
handle_book_delta :: proc(state: ^MD_Native_State, raw: []u8, ts: i64) {
	frame: util.MR_Book_Delta_Frame
	if json.unmarshal(raw, &frame) != nil do return
	bd := frame.payload

	unix := normalize_unix_seconds(bd.timestamp_ms if bd.timestamp_ms != 0 else ts)

	sync.lock(&state.mu)
	ac := min(len(bd.asks), OB_STAGING_DEPTH)
	bc := min(len(bd.bids), OB_STAGING_DEPTH)
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
	sync.unlock(&state.mu)
}

@(private = "file")
handle_stats :: proc(state: ^MD_Native_State, raw: []u8, ts: i64) {
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

	sync.lock(&state.mu)
	state.stats_staging = Stats_Staging{
		mark_price = s.mark_price_close,
		funding    = s.funding_rate_last,
		tbuy       = i64(s.liq_buy_volume),
		tsell      = i64(s.liq_sell_volume),
		unix       = unix,
	}
	state.stats_dirty = true
	sync.unlock(&state.mu)
}

@(private = "file")
handle_heatmap :: proc(state: ^MD_Native_State, raw: []u8, ts: i64) {
	frame: util.MR_Heatmap_Frame
	if json.unmarshal(raw, &frame) != nil do return
	hm := frame.payload

	unix := normalize_unix_seconds(hm.window_end_ts if hm.window_end_ts != 0 else ts)
	lc := min(len(hm.cells), HEATMAP_STAGING_CAP)

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

	sync.lock(&state.mu)
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
	sync.unlock(&state.mu)
}

@(private = "file")
handle_vpvr :: proc(state: ^MD_Native_State, raw: []u8, ts: i64) {
	frame: util.MR_VPVR_Frame
	if json.unmarshal(raw, &frame) != nil do return
	vp := frame.payload

	unix := normalize_unix_seconds(vp.window_end_ts if vp.window_end_ts != 0 else ts)
	lc := min(len(vp.buckets), VPVR_STAGING_CAP)

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

	sync.lock(&state.mu)
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
	sync.unlock(&state.mu)
}
