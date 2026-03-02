package services

// Shared MR protocol message parser — pure function, zero platform imports.
// Both native and web adapters call parse_mr_message, then write results
// into their own staging buffers under their own threading model.
//
// Eliminates ~400 LOC of duplicated parsing logic.

import "core:encoding/json"
import "core:math"
import "mr:util"

// --- Shared staging structs (used by both platform adapters) ---

OB_STAGING_DEPTH    :: 50
HEATMAP_STAGING_CAP :: 512
VPVR_STAGING_CAP    :: 256

Parsed_OB :: struct {
	ask_prices: [OB_STAGING_DEPTH]f64,
	ask_sizes:  [OB_STAGING_DEPTH]f64,
	bid_prices: [OB_STAGING_DEPTH]f64,
	bid_sizes:  [OB_STAGING_DEPTH]f64,
	ask_count:  int,
	bid_count:  int,
	is_snapshot: bool,
	last_price: f64,
	unix:       i64,
	subject_id: u64,
	seq:        i64,
}

Parsed_Stats :: struct {
	mark_price: f64,
	funding:    f64,
	tbuy:       f64,
	tsell:      f64,
	unix:       i64,
	subject_id: u64,
	seq:        i64,
}

Parsed_Heatmap :: struct {
	prices:      [HEATMAP_STAGING_CAP]f64,
	sizes:       [HEATMAP_STAGING_CAP]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	max_size:    f64,
	unix:        i64,
	subject_id:  u64,
	seq:         i64,
}

Parsed_VPVR :: struct {
	prices:      [VPVR_STAGING_CAP]f64,
	buys:        [VPVR_STAGING_CAP]f64,
	sells:       [VPVR_STAGING_CAP]f64,
	level_count: int,
	price_group: f64,
	min_price:   f64,
	max_price:   f64,
	unix:        i64,
	subject_id:  u64,
	seq:         i64,
}

Parsed_Candle :: struct {
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
	subject_id:      u64,
	seq:             i64,
}

Parsed_Trade :: struct {
	price:      f64,
	qty:        f64,
	is_buy:     bool,
	unix:       i64,
	subject_id: u64,
	seq:        i64,
}

Parsed_Ack :: struct {
	op:      string,
	subject: string,
}

Parsed_Control :: struct {
	rtt_ms:    i64,
	backlog:   int,
	dropped:   int,
	server_ts: i64,
}

Hello_Reject_Reason :: enum u8 {
	None,
	Missing_Proto_Version,
	Unsupported_Proto_Version,
	Missing_Server_Time,
	Missing_Capabilities,
}

Parsed_Hello :: struct {
	proto_ver:   int,
	server_time: i64,
	topic_count: int,
	venue_count: int,
	symbol_count: int,
	valid:       bool,
	reject:      Hello_Reject_Reason,
}

// --- Parse result discriminated union ---

RANGE_CANDLE_PARSE_MAX :: 32

Parsed_Range_Candles :: struct {
	candles: [RANGE_CANDLE_PARSE_MAX]Parsed_Candle,
	count:   int,
	is_last: bool,
	seq:     i64,
}

Parse_Result_Kind :: enum u8 {
	None,
	Trade,
	Orderbook,
	Stats,
	Heatmap,
	VPVR,
	Candle,
	Range_Candle,
	Ack,
	Hello,
	Heartbeat,
	Health,
	Error,
}

Parse_Result_Data :: struct #raw_union {
	trade:         Parsed_Trade,
	ob:            Parsed_OB,
	stats:         Parsed_Stats,
	heatmap:       Parsed_Heatmap,
	vpvr:          Parsed_VPVR,
	candle:        Parsed_Candle,
	range_candles: Parsed_Range_Candles,
	ack:           Parsed_Ack,
	control:       Parsed_Control,
	hello:         Parsed_Hello,
}

Parse_Result_Meta :: struct {
	seq:          i64,
	server_ts_ms: i64,
	subject_id:   u64,
	is_snapshot:  bool,
}

Parse_Result :: struct {
	kind: Parse_Result_Kind,
	data: Parse_Result_Data,
	meta: Parse_Result_Meta,
}

// --- Telemetry counters (caller accumulates into their own state) ---

Parse_Telemetry :: struct {
	parse_errors:    int,
	envelope_errors: int,
	unknown_streams: int,
}

// --- Main parse entry point ---
// Pure function: works on temp_allocator, results are stack-copied to staging.
// TF gating is handled by WS subject routing — parser accepts all candle events.

parse_mr_message :: proc(raw: []u8, telemetry: ^Parse_Telemetry) -> Parse_Result {
	result: Parse_Result

	// Pass 1: envelope only.
	env: util.MR_Envelope
	if json.unmarshal(raw, &env) != nil {
		if telemetry != nil do telemetry.parse_errors += 1
		return result
	}
	result.meta.seq = env.seq
	result.meta.server_ts_ms = env.ts_ingest
	result.meta.subject_id = util.subject_id64(env.subject)

	ft := util.parse_frame_type(env.type_str)
	result.meta.is_snapshot = ft == .Snapshot

	switch ft {
	case .Ack:
		result.kind = .Ack
		result.data.ack = Parsed_Ack{op = env.op, subject = env.subject}
		return result
	case .Hello:
		result.kind = .Hello
		if h, ok := parse_hello(raw); ok {
			result.data.hello = h
		} else {
			result.data.hello = Parsed_Hello{valid = false, reject = .Missing_Proto_Version}
			if telemetry != nil do telemetry.parse_errors += 1
		}
		return result
	case .Heartbeat:
		result.kind = .Heartbeat
		if c, ok := parse_control(raw, env.ts_ingest); ok {
			result.data.control = c
		}
		return result
	case .Health:
		result.kind = .Health
		if c, ok := parse_control(raw, env.ts_ingest); ok {
			result.data.control = c
		}
		return result
	case .Error:
		result.kind = .Error
		return result
	case .Range:
		if r, ok := parse_range_candles(raw, env.subject); ok {
			r.seq = result.meta.seq
			result.kind = .Range_Candle
			result.data.range_candles = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
		return result
	case .Last, .Unknown:
		return result
	case .Event, .Snapshot:
		// Fall through to payload parsing.
	}

	// Pass 2: re-parse same bytes into typed frame struct.
	stream := util.subject_stream_type(env.subject)
	subject_id := util.subject_id64(env.subject)

	switch stream {
	case "marketdata.trade":
		if r, ok := parse_trade(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Trade
			result.data.trade = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "marketdata.bookdelta":
		if r, ok := parse_book_delta(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Orderbook
			result.data.ob = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "aggregation.stats":
		if r, ok := parse_stats(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Stats
			result.data.stats = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "insights.heatmap_snapshot":
		if r, ok := parse_heatmap(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Heatmap
			result.data.heatmap = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "insights.volume_profile_snapshot":
		if r, ok := parse_vpvr(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .VPVR
			result.data.vpvr = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "aggregation.candle":
		if r, ok := parse_candle(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Candle
			result.data.candle = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case:
		if telemetry != nil do telemetry.unknown_streams += 1
		if stream == "system.health" || stream == "session.health" {
			result.kind = .Health
			if c, ok := parse_control(raw, env.ts_ingest); ok {
				result.data.control = c
			}
		} else if stream == "session.heartbeat" || stream == "system.heartbeat" {
			result.kind = .Heartbeat
			if c, ok := parse_control(raw, env.ts_ingest); ok {
				result.data.control = c
			}
		}
	}

	return result
}

// --- Individual payload parsers ---

@(private = "file")
parse_trade :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Trade, bool) {
	frame: util.MR_Trade_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	trade := frame.payload

	unix := util.normalize_unix_seconds(trade.timestamp_ms if trade.timestamp_ms != 0 else ts)

	return Parsed_Trade{
		price      = trade.price,
		qty        = trade.size,
		is_buy     = trade.side == "buy",
		unix       = unix,
		subject_id = subject_id,
	}, true
}

@(private = "file")
parse_book_delta :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_OB, bool) {
	frame: util.MR_Book_Delta_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	bd := frame.payload

	unix := util.normalize_unix_seconds(bd.timestamp_ms if bd.timestamp_ms != 0 else ts)

	result: Parsed_OB
	ac := min(len(bd.asks), OB_STAGING_DEPTH)
	bc := min(len(bd.bids), OB_STAGING_DEPTH)
	result.ask_count = ac
	result.bid_count = bc
	result.is_snapshot = bd.is_snapshot
	result.unix = unix
	result.subject_id = subject_id

	if ac > 0 && bc > 0 {
		result.last_price = (bd.asks[0].price + bd.bids[0].price) / 2.0
	}

	for i in 0 ..< ac {
		result.ask_prices[i] = bd.asks[i].price
		result.ask_sizes[i]  = bd.asks[i].size
	}
	for i in 0 ..< bc {
		result.bid_prices[i] = bd.bids[i].price
		result.bid_sizes[i]  = bd.bids[i].size
	}
	return result, true
}

@(private = "file")
parse_stats :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Stats, bool) {
	frame: util.MR_Stats_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	s := frame.payload
	if s.window_start_ts == 0 && s.window_end_ts == 0 {
		wrapped: util.MR_Stats_Frame_Wrapped
		if json.unmarshal(raw, &wrapped) == nil {
			s = wrapped.payload.stats
		}
	}

	unix := util.normalize_unix_seconds(s.window_end_ts if s.window_end_ts != 0 else ts)

	return Parsed_Stats{
		mark_price = s.mark_price_close,
		funding    = s.funding_rate_last,
		tbuy       = s.liq_buy_volume,
		tsell      = s.liq_sell_volume,
		unix       = unix,
		subject_id = subject_id,
	}, true
}

@(private = "file")
parse_heatmap :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Heatmap, bool) {
	frame: util.MR_Heatmap_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	hm := frame.payload

	unix := util.normalize_unix_seconds(hm.window_end_ts if hm.window_end_ts != 0 else ts)
	lc := min(len(hm.cells), HEATMAP_STAGING_CAP)

	min_p := math.F64_MAX
	max_p := -math.F64_MAX
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

	result: Parsed_Heatmap
	result.level_count = lc
	result.price_group = price_group
	result.min_price = min_p if lc > 0 else 0
	result.max_price = max_p if lc > 0 else 0
	result.max_size = max_s
	result.unix = unix
	result.subject_id = subject_id
	for i in 0 ..< lc {
		c := hm.cells[i]
		result.prices[i] = (c.price_bucket_low + c.price_bucket_high) / 2.0
		result.sizes[i]  = c.bid_liquidity + c.ask_liquidity + c.trade_volume
	}
	return result, true
}

@(private = "file")
parse_vpvr :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_VPVR, bool) {
	frame: util.MR_VPVR_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	vp := frame.payload

	unix := util.normalize_unix_seconds(vp.window_end_ts if vp.window_end_ts != 0 else ts)
	lc := min(len(vp.buckets), VPVR_STAGING_CAP)

	min_p := math.F64_MAX
	max_p := -math.F64_MAX
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

	result: Parsed_VPVR
	result.level_count = lc
	result.price_group = price_group
	result.min_price = min_p if lc > 0 else 0
	result.max_price = max_p if lc > 0 else 0
	result.unix = unix
	result.subject_id = subject_id
	for i in 0 ..< lc {
		b := vp.buckets[i]
		result.prices[i] = (b.price_low + b.price_high) / 2.0
		result.buys[i]   = b.buy_volume
		result.sells[i]  = b.sell_volume
	}
	return result, true
}

@(private = "file")
Control_Payload :: struct {
	rtt_ms:  i64 `json:"rtt_ms"`,
	backlog: int `json:"backlog"`,
	dropped: int `json:"dropped"`,
}

@(private = "file")
Control_Frame :: struct {
	payload: Control_Payload `json:"payload"`,
}

@(private = "file")
parse_hello :: proc(raw: []u8) -> (Parsed_Hello, bool) {
	frame: util.MR_Hello_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false

	h := Parsed_Hello{
		proto_ver    = frame.payload.proto_ver,
		server_time  = frame.payload.server_time,
		topic_count  = len(frame.payload.capabilities.topics),
		venue_count  = len(frame.payload.capabilities.venues),
		symbol_count = len(frame.payload.capabilities.symbols),
		valid        = true,
		reject       = .None,
	}

	if h.proto_ver <= 0 {
		h.valid = false
		h.reject = .Missing_Proto_Version
		return h, true
	}
	if h.proto_ver != util.MR_PROTO_VER {
		h.valid = false
		h.reject = .Unsupported_Proto_Version
		return h, true
	}
	if h.server_time <= 0 {
		h.valid = false
		h.reject = .Missing_Server_Time
		return h, true
	}
	if h.topic_count <= 0 {
		h.valid = false
		h.reject = .Missing_Capabilities
		return h, true
	}
	return h, true
}

@(private = "file")
parse_control :: proc(raw: []u8, ts: i64) -> (Parsed_Control, bool) {
	frame: Control_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return Parsed_Control{
		rtt_ms    = frame.payload.rtt_ms,
		backlog   = frame.payload.backlog,
		dropped   = frame.payload.dropped,
		server_ts = ts,
	}, true
}

@(private = "file")
parse_range_candles :: proc(raw: []u8, subject: string) -> (Parsed_Range_Candles, bool) {
	frame_wrapped: util.MR_Range_Frame
	if json.unmarshal(raw, &frame_wrapped) != nil do return {}, false

	// Some servers return range item payloads as flat candle payloads instead of
	// {"Candle": {...}}. Parse both and accept whichever is valid.
	frame_flat: util.MR_Range_Frame_Flat
	_ = json.unmarshal(raw, &frame_flat)

	subject_id := util.subject_id64(subject)
	result: Parsed_Range_Candles
	result.is_last = true // current backend emits one frame per getrange request

	item_count := len(frame_wrapped.items)
	if len(frame_flat.items) > item_count {
		item_count = len(frame_flat.items)
	}
	if item_count <= 0 {
		result.count = 0
		return result, true
	}

	start := max(item_count - RANGE_CANDLE_PARSE_MAX, 0)
	out := 0
	for i in start ..< item_count {
		c: util.MR_Candle_Payload
		if i < len(frame_wrapped.items) {
			wrapped := frame_wrapped.items[i].payload.candle
			if wrapped.WindowStartTs > 0 {
				c = wrapped
			}
		}
		if c.WindowStartTs <= 0 && i < len(frame_flat.items) {
			c = frame_flat.items[i].payload
		}
		if c.WindowStartTs <= 0 do continue
		if c.WindowEndTs <= c.WindowStartTs do continue

		result.candles[out] = Parsed_Candle{
			open            = c.Open,
			high            = c.High,
			low             = c.Low,
			close           = c.ClosePrice,
			volume          = c.Volume,
			buy_vol         = c.BuyVolume,
			sell_vol        = c.SellVolume,
			trade_count     = c.TradeCount,
			window_start_ts = c.WindowStartTs,
			window_end_ts   = c.WindowEndTs,
			is_closed       = c.IsClosed,
			subject_id      = subject_id,
		}
		out += 1
		if out >= RANGE_CANDLE_PARSE_MAX do break
	}
	result.count = out
	return result, true
}

@(private = "file")
parse_candle :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Candle, bool) {
	// Try wrapped format first: {"payload": {"Candle": {...}}}
	frame: util.MR_Candle_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	c := frame.payload.candle
	// If wrapped parse yields zero fields, try flat format.
	if c.WindowStartTs == 0 && c.WindowEndTs == 0 {
		flat: util.MR_Candle_Frame_Flat
		if json.unmarshal(raw, &flat) == nil {
			c = flat.payload
		}
	}
	if c.WindowStartTs == 0 do return {}, false

	return Parsed_Candle{
		open            = c.Open,
		high            = c.High,
		low             = c.Low,
		close           = c.ClosePrice,
		volume          = c.Volume,
		buy_vol         = c.BuyVolume,
		sell_vol        = c.SellVolume,
		trade_count     = c.TradeCount,
		window_start_ts = c.WindowStartTs,
		window_end_ts   = c.WindowEndTs,
		is_closed       = c.IsClosed,
		subject_id      = subject_id,
	}, true
}
