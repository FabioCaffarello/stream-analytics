package main

// Native marketdata port — WebSocket client with background reader thread.
// Ring buffer for trades, single-slot latest-wins for orderbook snapshots.
// Automatic reconnection with exponential backoff on connection loss.

import "core:encoding/json"
import "core:fmt"
import "core:net"
import "core:sync"
import "core:thread"
import "core:time"
import "mr:ports"

TRADE_RING_CAP :: 1024
OB_STAGING_DEPTH :: 50

// --- Reconnection constants ---

BACKOFF_INITIAL_MS :: 500
BACKOFF_MAX_MS     :: 30_000
BACKOFF_MULTIPLIER :: 2

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

MD_Native_State :: struct {
	// Trade ring buffer (SPSC: writer=background, reader=main).
	trade_ring:  [TRADE_RING_CAP]ports.MD_Trade_Event,
	trade_write: int,
	trade_count: int,

	// Orderbook snapshot (latest-wins, single-slot).
	ob_staging: OB_Staging,
	ob_dirty:   bool,

	// Connection.
	conn:        WS_Connection,
	conn_state:  Conn_State,
	ws_url:      string,
	should_stop: bool,
	mu:          sync.Mutex,
	drop_count:  int,

	// Reconnection.
	backoff_ms:      int,
	reconnect_count: int,

	// Temp arrays for poll() — main thread only.
	poll_ask_prices: [OB_STAGING_DEPTH]f64,
	poll_ask_sizes:  [OB_STAGING_DEPTH]f64,
	poll_bid_prices: [OB_STAGING_DEPTH]f64,
	poll_bid_sizes:  [OB_STAGING_DEPTH]f64,
}

@(private = "file")
g_md_state: ^MD_Native_State

// --- Public API ---

// Try to connect to the MR WebSocket server. Returns a stub port on failure
// (first attempt only; subsequent reconnection happens in the background).
make_marketdata_native :: proc(url: string) -> ports.Marketdata_Port {
	state := new(MD_Native_State)
	state.ws_url = url
	state.backoff_ms = BACKOFF_INITIAL_MS
	g_md_state = state

	// Attempt initial connection.
	state.conn_state = .Connecting
	conn, err := ws_dial(url)
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
native_subscribe :: proc(symbol: string, channel: ports.MD_Channel) -> bool {
	state := g_md_state
	if state == nil do return false
	sync.lock(&state.mu)
	connected := state.conn_state == .Connected
	sync.unlock(&state.mu)
	if !connected do return false
	msg := fmt.tprintf(`{"action":"subscribe","symbol":"%s","channel":%d}`, symbol, int(channel))
	err := ws_write_text(state.conn, msg)
	return err == nil
}

@(private = "file")
native_unsubscribe :: proc(symbol: string, channel: ports.MD_Channel) {
	state := g_md_state
	if state == nil do return
	sync.lock(&state.mu)
	connected := state.conn_state == .Connected
	sync.unlock(&state.mu)
	if !connected do return
	msg := fmt.tprintf(`{"action":"unsubscribe","symbol":"%s","channel":%d}`, symbol, int(channel))
	ws_write_text(state.conn, msg)
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

		// Copy to poll-local arrays (main-thread only, valid until next poll).
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
		// If not connected, attempt reconnection.
		sync.lock(&state.mu)
		cs := state.conn_state
		sync.unlock(&state.mu)

		if cs != .Connected {
			attempt_reconnect(state)
			continue
		}

		// Read messages.
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

		parse_ws_message(state, payload)
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

	sync.lock(&state.mu)
	state.conn_state = .Connecting
	sync.unlock(&state.mu)

	conn, err := ws_dial(state.ws_url)
	if err != nil {
		fmt.printf("[marketdata] Reconnect failed (err=%v)\n", err)
		sync.lock(&state.mu)
		state.conn_state = .Disconnected
		// Exponential backoff with cap.
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
	sync.unlock(&state.mu)
}

// --- JSON parsing ---

// Outer envelope matching MR server protocol.
@(private = "file")
WS_Envelope :: struct {
	stream:    int    `json:"Stream"`,
	timeframe: i64    `json:"Timeframe"`,
	data:      string `json:"Data"`,
}

@(private = "file")
WS_Trade :: struct {
	price:  f64  `json:"price"`,
	qty:    f64  `json:"qty"`,
	is_buy: bool `json:"isBuy"`,
	unix:   i64  `json:"unix"`,
}

@(private = "file")
WS_Orderbook :: struct {
	ask_prices: [dynamic]f64 `json:"askPrices"`,
	ask_sizes:  [dynamic]f64 `json:"askSizes"`,
	bid_prices: [dynamic]f64 `json:"bidPrices"`,
	bid_sizes:  [dynamic]f64 `json:"bidSizes"`,
	last_price: f64          `json:"lastPrice"`,
	unix:       i64          `json:"unix"`,
}

@(private = "file")
parse_ws_message :: proc(state: ^MD_Native_State, payload: []u8) {
	envelope: WS_Envelope
	if json.unmarshal(payload, &envelope) != nil do return

	switch envelope.stream {
	case 0: // Trades
		trade: WS_Trade
		if json.unmarshal_string(envelope.data, &trade) != nil do return

		sync.lock(&state.mu)
		if state.trade_count < TRADE_RING_CAP {
			state.trade_ring[state.trade_write] = ports.MD_Trade_Event{
				price  = trade.price,
				qty    = trade.qty,
				is_buy = trade.is_buy,
				unix   = trade.unix,
			}
			state.trade_write = (state.trade_write + 1) % TRADE_RING_CAP
			state.trade_count += 1
		} else {
			state.drop_count += 1
		}
		sync.unlock(&state.mu)

	case 1: // Orderbook
		ob: WS_Orderbook
		if json.unmarshal_string(envelope.data, &ob) != nil do return

		sync.lock(&state.mu)
		ac := min(len(ob.ask_prices), OB_STAGING_DEPTH)
		bc := min(len(ob.bid_prices), OB_STAGING_DEPTH)
		state.ob_staging.ask_count = ac
		state.ob_staging.bid_count = bc
		state.ob_staging.last_price = ob.last_price
		state.ob_staging.unix = ob.unix
		for i in 0 ..< ac {
			state.ob_staging.ask_prices[i] = ob.ask_prices[i]
			state.ob_staging.ask_sizes[i]  = ob.ask_sizes[i]
		}
		for i in 0 ..< bc {
			state.ob_staging.bid_prices[i] = ob.bid_prices[i]
			state.ob_staging.bid_sizes[i]  = ob.bid_sizes[i]
		}
		state.ob_dirty = true
		sync.unlock(&state.mu)
	}

	// Log drop count periodically.
	if state.drop_count > 0 && state.drop_count % 100 == 0 {
		fmt.printf("[marketdata] Dropped %d events (ring full)\n", state.drop_count)
	}
}
