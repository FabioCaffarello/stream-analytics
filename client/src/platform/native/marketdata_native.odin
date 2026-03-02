package main

// Native marketdata port — WebSocket client with background reader thread.
// Ring buffer for trades, single-slot latest-wins for orderbook/stats/heatmap/vpvr.
// Automatic reconnection with exponential backoff + re-subscribe on reconnect.

import "core:fmt"
import "core:net"
import "core:strconv"
import "core:strings"
import "core:sync"
import "core:thread"
import "core:time"
import "mr:ports"
import "mr:services"
import "mr:util"

TRADE_RING_CAP  :: 1024
CANDLE_RING_CAP :: 8
MAX_SUBS :: 128

// --- Reconnection constants ---

BACKOFF_INITIAL_MS :: 500
BACKOFF_MAX_MS     :: 30_000
BACKOFF_MULTIPLIER :: 2

// Default candle timeframe filter.
CANDLE_TF_DEFAULT :: "1m"

// --- Internal state (package-level singleton) ---

Conn_State :: enum u8 {
	Disconnected,
	Connecting,
	Connected,
	Backoff_Wait,
}

Sub_Entry :: struct {
	subject_id: u64,
	venue:   string,
	symbol:  string,
	channel: ports.MD_Channel,
	subject: string,
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

	// Candle timeframe filter (mutable, heap-allocated).
	candle_tf_filter: string,

	// Connection.
	conn:        WS_Connection,
	conn_state:  Conn_State,
	ws_url:      string,
	api_key:     string,
	should_stop: bool,
	reader_thread: ^thread.Thread,
	mu:          sync.Mutex,
	drop_count:  int,

	// Reconnection.
	backoff_ms:      int,
	reconnect_count: int, // cumulative reconnect attempts (monotonic)
	reconnect_streak: int, // current consecutive reconnect attempts
	parse_arena: services.Parse_Arena,
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
	protocol_version:   int,
	hello_received:     bool,
	hello_valid:        bool,
	desync:             bool,
	desync_reason:      ports.MD_Desync_Reason,
	connect_started_ms: i64,
	first_data_logged:  bool,

	// Subscription tracking for reconnect re-subscribe.
	active_subs:  [MAX_SUBS]Sub_Entry,
	active_count: int,
	last_seq_by_sub: [MAX_SUBS]i64,
	snapshot_logged_by_sub: [MAX_SUBS]bool,

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

@(private = "file")
g_md_state: ^MD_Native_State

// --- Public API ---

make_marketdata_native :: proc(url: string, api_key: string = "") -> ports.Marketdata_Port {
	if g_md_state != nil {
		native_shutdown()
	}
	state := new(MD_Native_State)
	state.ws_url = strings.clone(url)
	state.api_key = strings.clone(api_key)
	state.backoff_ms = BACKOFF_INITIAL_MS
	state.candle_tf_filter = strings.clone(CANDLE_TF_DEFAULT)
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
		fmt.printf("[md-lifecycle] connect url=%s\n", url)
		state.conn = conn
		state.conn_state = .Connected
		state.backoff_ms = BACKOFF_INITIAL_MS
		state.desync = false
		state.desync_reason = .None
		state.protocol_version = 0
		state.hello_received = false
		state.hello_valid = false
		state.connect_started_ms = time.now()._nsec / 1_000_000
		state.first_data_logged = false
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
	}
}

// --- Port implementation ---

@(private = "file")
find_sub_by_subject :: proc(state: ^MD_Native_State, subject: string) -> int {
	for i in 0 ..< state.active_count {
		if state.active_subs[i].subject == subject do return i
	}
	return -1
}

@(private = "file")
find_sub_by_key :: proc(state: ^MD_Native_State, venue: string, symbol: string, channel: ports.MD_Channel) -> int {
	for i in 0 ..< state.active_count {
		sub := state.active_subs[i]
		if sub.channel == channel && sub.venue == venue && sub.symbol == symbol do return i
	}
	return -1
}

@(private = "file")
find_sub_by_subject_id :: proc(state: ^MD_Native_State, subject_id: u64) -> int {
	for i in 0 ..< state.active_count {
		if state.active_subs[i].subject_id == subject_id do return i
	}
	return -1
}

@(private = "file")
native_subject_for_channel :: proc(state: ^MD_Native_State, venue: string, symbol: string, channel: ports.MD_Channel) -> string {
	tf := ""
	if channel == .Heatmaps || channel == .VPVR || channel == .Candles {
		tf = state.candle_tf_filter
	}
	return util.build_subject_with_timeframe(venue, symbol, channel, tf)
}

@(private = "file")
native_free_sub_entry :: proc(entry: ^Sub_Entry) {
	if entry == nil do return
	if len(entry.venue) > 0 do delete(entry.venue)
	if len(entry.symbol) > 0 do delete(entry.symbol)
	if len(entry.subject) > 0 do delete(entry.subject)
	entry^ = {}
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
	state.desync = false
	state.desync_reason = .None
	state.protocol_version = 0
	state.hello_received = false
	state.hello_valid = false
	state.connect_started_ms = 0
	state.first_data_logged = false
	return true
}

@(private = "file")
native_reconnect_transport :: proc(ws_url: string, api_key: string) -> bool {
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
	ws_close(&state.conn)
	state.conn_state = .Disconnected
	state.backoff_ms = BACKOFF_INITIAL_MS
	state.desync = false
	state.desync_reason = .None
	state.protocol_version = 0
	state.hello_received = false
	state.hello_valid = false
	state.connect_started_ms = 0
	state.first_data_logged = false
	fmt.printf("[md-lifecycle] reconnect_requested url=%s\n", state.ws_url)
	return true
}

@(private = "file")
native_subscribe :: proc(venue: string, symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_md_state
	if state == nil do return false

	subject := native_subject_for_channel(state, venue, symbol, channel)
	subject_id := util.subject_id64(subject)

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	if state.active_count >= MAX_SUBS {
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
	subject_id := util.subject_id64(subject)

	sync.lock(&state.mu)
	defer sync.unlock(&state.mu)

	// Idempotent subscribe: do not re-send for already tracked subjects.
	if find_sub_by_subject(state, subject) != -1 {
		delete(subject)
		return true
	}
	if state.active_count >= MAX_SUBS {
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
	if state.heatmap_dirty do latest_pending += 1
	if state.vpvr_dirty do latest_pending += 1
	if state.candle_ring_count > 0 do latest_pending += 1
	out^ = ports.MD_Runtime_Metrics{
		active_subs       = state.active_count,
		trade_backlog     = state.trade_count,
		candle_backlog    = state.candle_ring_count,
		drop_count        = state.drop_count,
		reconnect_count   = state.reconnect_count,
		latest_pending    = latest_pending,
		parse_error_count = state.parse_error_count,
		subscribe_ack_count = state.subscribe_ack_count,
		parsed_msgs_total = state.parsed_msgs_total,
		parsed_bytes_total = state.parsed_bytes_total,
		parse_arena_resets = state.parse_arena.message_resets,
		msg_rate          = state.msg_rate,
		bytes_rate        = state.bytes_rate,
		last_msg_ts_ms   = state.last_msg_ts_ms,
		rtt_ms           = state.last_rtt_ms,
		lag_ms           = state.last_lag_ms,
		protocol_version = state.protocol_version,
		hello_received   = state.hello_received,
		desync           = state.desync,
		desync_reason    = state.desync_reason,
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
	}
	defer delete(subject)

	// Remove from active subs.
	if idx := find_sub_by_key(state, venue, symbol, channel); idx >= 0 {
		last := state.active_count - 1
		native_free_sub_entry(&state.active_subs[idx])
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

	if state.conn_state != .Connected do return
	send_unsubscribe(state, subject)
}

@(private = "file")
send_unsubscribe :: proc(state: ^MD_Native_State, subject: string) -> bool {
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
	err := ws_write_text(state.conn, string(unsub_buf[:un]))
	if err == nil {
		fmt.printf("[md-lifecycle] unsubscribe_sent subject=%s rid=r%s\n", subject, unsub_rid)
	}
	return err == nil
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
	if err == nil {
		fmt.printf("[md-lifecycle] subscribe_sent subject=%s rid=r%s\n", subject, rid_str)
	}
	return err == nil
}

@(private = "file")
native_resubscribe_timeframe_channels :: proc(state: ^MD_Native_State) {
	if state == nil do return
	is_connected := state.conn_state == .Connected

	for i in 0 ..< state.active_count {
		entry := &state.active_subs[i]
		if entry.channel != .Heatmaps && entry.channel != .VPVR && entry.channel != .Candles do continue

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
			unix       = st.unix,
		}
		state.stats_dirty = false
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

	for {
		if native_should_stop(state) do break

		sync.lock(&state.mu)
		cs := state.conn_state
		sync.unlock(&state.mu)

		if cs != .Connected {
			attempt_reconnect(state)
			continue
		}

		opcode, payload, err := ws_read_message(state.conn)
		if err != nil {
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
			state.desync_reason = .None
			state.protocol_version = 0
			state.hello_received = false
			state.hello_valid = false
			state.connect_started_ms = 0
			state.first_data_logged = false
			sync.unlock(&state.mu)
			fmt.printf("[md-lifecycle] disconnect reason=%v\n", err)
			ws_close(&state.conn)
			continue
		}

		if opcode != 0x1 do continue // Only handle text frames.

		apply_parse_result(state, payload)
	}
}

@(private = "file")
attempt_reconnect :: proc(state: ^MD_Native_State) {
	sync.lock(&state.mu)
	state.conn_state = .Backoff_Wait
	backoff := state.backoff_ms
	state.reconnect_count += 1
	state.reconnect_streak += 1
	count := state.reconnect_streak
	sync.unlock(&state.mu)

	fmt.printf("[marketdata] Reconnecting in %dms (attempt %d)\n", backoff, count)
	time.sleep(time.Duration(backoff) * time.Millisecond)

	if native_should_stop(state) do return

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
	fmt.printf("[md-lifecycle] connect url=%s\n", state.ws_url)
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
	state.connect_started_ms = time.now()._nsec / 1_000_000
	state.first_data_logged = false
	for i in 0 ..< state.active_count {
		state.snapshot_logged_by_sub[i] = false
	}

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

@(private = "file")
native_update_parse_rates :: proc(state: ^MD_Native_State, now_ms: i64, bytes: int) {
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
native_parse_result_has_data :: proc(kind: services.Parse_Result_Kind) -> bool {
	switch kind {
	case .Trade, .Orderbook, .Stats, .Heatmap, .VPVR, .Candle, .Range_Candle:
		return true
	case .None, .Ack, .Hello, .Heartbeat, .Health, .Error:
		return false
	}
	return false
}

@(private = "file")
native_desync_reason_from_hello_reject :: proc(reject: services.Hello_Reject_Reason) -> ports.MD_Desync_Reason {
	switch reject {
	case .Unsupported_Proto_Version:
		return .Protocol_Version
	case .Missing_Proto_Version, .Missing_Server_Time, .Missing_Capabilities:
		return .Protocol_Invalid
	case .None:
	}
	return .Protocol_Invalid
}

@(private = "file")
apply_parse_result :: proc(state: ^MD_Native_State, raw: []u8) {
	defer services.parse_arena_reset_message(&state.parse_arena)

	telemetry: services.Parse_Telemetry
	result := services.parse_mr_message_with_arena(&state.parse_arena, raw, &telemetry)
	parsed_now_ms := time.now()._nsec / 1_000_000
	snapshot_subject := ""
	should_log_snapshot := false
	snapshot_seq := i64(0)
	snapshot_sid := u64(0)
	should_log_first_data := false
	first_data_delta_ms := i64(0)

	sync.lock(&state.mu)
	native_update_parse_rates(state, parsed_now_ms, len(raw))
	sync.unlock(&state.mu)

	// Accumulate telemetry under lock.
	if telemetry.parse_errors > 0 {
		sync.lock(&state.mu)
		state.parse_error_count += telemetry.parse_errors
		ec := state.parse_error_count
		sync.unlock(&state.mu)
		if ec <= 3 || ec % 50 == 0 {
			preview_len := min(len(raw), 120)
			fmt.printf("[marketdata] Parse error #%d: %s\n", ec, string(raw[:preview_len]))
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
		if result.meta.subject_id != 0 && result.meta.seq > 0 {
				if si := find_sub_by_subject_id(state, result.meta.subject_id); si >= 0 {
					prev_seq := state.last_seq_by_sub[si]
					if prev_seq > 0 && result.meta.seq > prev_seq + 1 {
						state.desync = true
						state.desync_reason = .Sequence_Gap
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
		if native_parse_result_has_data(result.kind) && state.connect_started_ms > 0 && !state.first_data_logged {
			first_data_delta_ms = max(parsed_now_ms - state.connect_started_ms, 0)
			state.first_data_logged = true
			should_log_first_data = true
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
	if native_parse_result_has_data(result.kind) {
		sync.lock(&state.mu)
		hello_received := state.hello_received
		hello_valid := state.hello_valid
		prev_reason := state.desync_reason
		if !hello_received {
			state.desync = true
			state.desync_reason = .Missing_Hello
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
		sync.unlock(&state.mu)
		fmt.printf("[md-lifecycle] ack_recv op=%s subject=%s\n", ack.op, ack.subject)
	case .Hello:
		h := result.data.hello
		sync.lock(&state.mu)
		state.hello_received = true
		state.protocol_version = h.proto_ver
		state.hello_valid = h.valid
		if !h.valid {
			state.desync = true
			state.desync_reason = native_desync_reason_from_hello_reject(h.reject)
		} else {
			state.desync = false
			state.desync_reason = .None
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
			"[md-lifecycle] hello_ok proto_ver=%d topics=%d venues=%d symbols=%d\n",
			h.proto_ver, h.topic_count, h.venue_count, h.symbol_count,
		)
	case .Heartbeat, .Health:
		ctrl := result.data.control
		sync.lock(&state.mu)
		if ctrl.rtt_ms > 0 do state.last_rtt_ms = ctrl.rtt_ms
		if ctrl.dropped > 0 && ctrl.dropped > state.drop_count do state.drop_count = ctrl.dropped
		sync.unlock(&state.mu)
	case .Error:
		preview_len := min(len(raw), 200)
		fmt.printf("[marketdata] Error: %s\n", string(raw[:preview_len]))
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
			state.candle_ring[state.candle_ring_write] = result.data.candle
			state.candle_ring_write = (state.candle_ring_write + 1) % CANDLE_RING_CAP
		}
		sync.unlock(&state.mu)
	case .Range_Candle:
		sync.lock(&state.mu)
		state.range_candle_staging = result.data.range_candles
		state.range_candle_dirty = true
		sync.unlock(&state.mu)
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
	n := 0
	prefix :: `{"op":"getrange","subject":"`
	for c in prefix { buf[n] = u8(c); n += 1 }
	for c in subject { buf[n] = u8(c); n += 1 }
	mid :: `","params":{"limit":`
	for c in mid { buf[n] = u8(c); n += 1 }
	limit_str := fmt.tprintf("%d", limit)
	for c in limit_str { buf[n] = u8(c); n += 1 }
	if end_ts > 0 {
		end_mid :: `,"to_ms":`
		for c in end_mid { buf[n] = u8(c); n += 1 }
		end_str := fmt.tprintf("%d", end_ts)
		for c in end_str { buf[n] = u8(c); n += 1 }
	}
	mid2 :: `},"request_id":"gr`
	for c in mid2 { buf[n] = u8(c); n += 1 }
	rid_str := fmt.tprintf("%d", state.rid_counter)
	for c in rid_str { buf[n] = u8(c); n += 1 }
	suffix :: `"}`
	for c in suffix { buf[n] = u8(c); n += 1 }

	ws_write_text(state.conn, string(buf[:n]))
}

// --- HTTP GET for market discovery ---

@(private = "file")
native_fetch_markets :: proc(out_buf: [^]u8, out_cap: i32) -> i32 {
	state := g_md_state
	if state == nil do return 0
	if out_cap <= 0 do return 0

	// Derive HTTP base URL from WS URL: "ws://host:port/ws" -> "host:port"
	ws_url := state.ws_url
	if !strings.has_prefix(ws_url, "ws://") do return 0
	url_no_scheme := ws_url[5:]
	host_port := url_no_scheme
	if idx := strings.index(url_no_scheme, "/"); idx != -1 {
		host_port = url_no_scheme[:idx]
	}
	host_str, port_str := host_port, "80"
	if colon_idx := strings.index(host_port, ":"); colon_idx != -1 {
		host_str = host_port[:colon_idx]
		port_str = host_port[colon_idx + 1:]
	}

	ip, ok := net.parse_ip4_address(host_str)
	if !ok {
		if host_str == "localhost" {
			ip = net.IP4_Address{127, 0, 0, 1}
		} else {
			return 0
		}
	}
	port, port_ok := strconv.parse_int(port_str)
	if !port_ok || port < 0 || port > 65535 do return 0

	endpoint := net.Endpoint{address = ip, port = port}
	conn, dial_err := net.dial_tcp(endpoint)
	if dial_err != nil do return 0
	defer net.close(conn)

	timeout := 3 * time.Second
	_ = net.set_option(conn, .Receive_Timeout, timeout)
	_ = net.set_option(conn, .Send_Timeout, timeout)

	// Send HTTP/1.0 GET (1.0 means no chunked encoding).
	req := fmt.tprintf(
		"GET /api/v1/markets HTTP/1.0\r\n" +
		"Host: %s\r\n" +
		"Accept: application/json\r\n" +
		"Connection: close\r\n" +
		"\r\n",
		host_port)

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
