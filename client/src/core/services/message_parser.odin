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
	prices:          [HEATMAP_STAGING_CAP]f64,
	sizes:           [HEATMAP_STAGING_CAP]f64,
	level_count:     int,
	price_group:     f64,
	min_price:       f64,
	max_price:       f64,
	max_size:        f64,
	unix:            i64,
	window_start_ms: i64,
	subject_id:      u64,
	seq:             i64,
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

Parsed_Evidence :: struct {
	kind:          [24]u8,
	kind_len:      u8,
	confidence:    f64,
	reason:        [96]u8,
	reason_len:    u8,
	feature_tags:  [4][24]u8,
	feature_vals:  [4]f64,
	feature_count: int,
	unix:          i64,
	subject_id:    u64,
	seq:           i64,
}

Parsed_Signal :: struct {
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
	op:        string,
	subject:   string,
	stream_id: string,
}

Parsed_Hello_Ack :: struct {
	negotiated_features:       [MAX_FEATURE_SLOTS]Parsed_Feature_Slot,
	negotiated_feature_count:  int,
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

// Fixed-size feature slot for zero-alloc feature storage.
Parsed_Feature_Slot :: struct {
	name: [24]u8,
	len:  u8,
}

MAX_FEATURE_SLOTS :: 8

Parsed_Hello :: struct {
	proto_ver:              int,
	server_time:            i64,
	server_instance_id:     string,
	topic_count:            int,
	venue_count:            int,
	symbol_count:           int,
	valid:                  bool,
	reject:                 Hello_Reject_Reason,
	// Capability limits (Terminal_V1).
	max_subscriptions:      int,
	max_symbols:            int,
	max_frame_bytes:        int,
	outbound_queue_size:    int,
	metrics_cadence_ms:     int,
	keepalive_interval_ms:  int,
	// Rate limit sub-object.
	rate_limit_enabled:     bool,
	rate_limit_max_per_sec: int,
	rate_limit_burst:       int,
	// Supported features.
	supported_features:       [MAX_FEATURE_SLOTS]Parsed_Feature_Slot,
	supported_feature_count:  int,
}

Parsed_Pong :: struct {
	ts_client:  i64,
	ts_server:  i64,
	rtt_ms:     i64,
	request_id: string,
}

Parsed_Metrics :: struct {
	ws_dropped_total:              i64,
	ws_queue_len:                  int,
	ws_lag_ms:                     i64,
	publish_to_deliver_latency_ms: i64,
	serialize_errors_total:        i64,
	resync_total:                  i64,
	active_subscriptions:          int,
	messages_out_total:            i64,
	// Backpressure fields (Terminal_V1).
	backpressure_level:            int,
	recommended_action_buf:        [32]u8,
	recommended_action_len:        u8,
	queue_capacity:                int,
	queue_high_watermark:          int,
}

Parsed_Error :: struct {
	op:          string,
	request_id:  string,
	code:        string,
	message:     string,
	error_code:  string,
	action_hint: string,
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
	Evidence,
	Signal,
	Range_Candle,
	Ack,
	Hello,
	Hello_Ack,
	Heartbeat,
	Health,
	Error,
	Pong,
	Metrics,
}

Parse_Result_Data :: struct #raw_union {
	trade:          Parsed_Trade,
	ob:             Parsed_OB,
	stats:          Parsed_Stats,
	heatmap:        Parsed_Heatmap,
	vpvr:           Parsed_VPVR,
	candle:         Parsed_Candle,
	evidence:       Parsed_Evidence,
	signal:         Parsed_Signal,
	range_candles:  Parsed_Range_Candles,
	ack:            Parsed_Ack,
	control:        Parsed_Control,
	hello:          Parsed_Hello,
	hello_ack:      Parsed_Hello_Ack,
	pong:           Parsed_Pong,
	server_metrics: Parsed_Metrics,
	error_detail:   Parsed_Error,
}

Parse_Result_Meta :: struct {
	seq:              i64,
	server_ts_ms:     i64,
	has_ts_server:    bool,
	subject_id:       u64,
	is_snapshot:      bool,
	// Terminal_V1 integrity fields.
	prev_seq:         i64,
	snapshot_seq:     i64,
	watermark_seq:    i64,
	snapshot_hash:    [16]u8,
	snapshot_hash_len: u8,
}

Parse_Result :: struct {
	kind: Parse_Result_Kind,
	data: Parse_Result_Data,
	meta: Parse_Result_Meta,
}

// --- Batched frame parser (zero-alloc event views) ---

BATCH_EVENT_VIEW_CAP :: 32

Parsed_Batch_Event_View :: struct {
	event_index:   int,
	dseq:          i64,
	dprev:         i64,
	dts:           i64,
	dti:           i64,
	payload_start: int,
	payload_end:   int,
}

Parsed_Batched_Frame :: struct {
	stream_id_buf: [128]u8,
	stream_id_len: u8,
	venue_buf:     [24]u8,
	venue_len:     u8,
	symbol_buf:    [32]u8,
	symbol_len:    u8,
	channel_buf:   [32]u8,
	channel_len:   u8,
	base_seq:      i64,
	count:         int,
	ts_server_base: i64,
	ts_ingest_base: i64,
	total_events:  int,
	event_count:   int,
	has_more:      bool,
	events:        [BATCH_EVENT_VIEW_CAP]Parsed_Batch_Event_View,
}

@(private = "file")
batch_is_ws :: proc(c: u8) -> bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

@(private = "file")
batch_skip_ws :: proc(raw: []u8, idx: ^int) {
	for idx^ < len(raw) && batch_is_ws(raw[idx^]) {
		idx^ += 1
	}
}

@(private = "file")
batch_parse_string_span :: proc(raw: []u8, idx: ^int, start: ^int, end: ^int) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) || raw[idx^] != '"' do return false
	idx^ += 1
	start^ = idx^
	escaped := false
	for idx^ < len(raw) {
		c := raw[idx^]
		if escaped {
			escaped = false
			idx^ += 1
			continue
		}
		if c == '\\' {
			escaped = true
			idx^ += 1
			continue
		}
		if c == '"' {
			end^ = idx^
			idx^ += 1
			return true
		}
		idx^ += 1
	}
	return false
}

@(private = "file")
batch_parse_int :: proc(raw: []u8, idx: ^int, out: ^i64) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) do return false
	sign := i64(1)
	if raw[idx^] == '-' {
		sign = -1
		idx^ += 1
	}
	if idx^ >= len(raw) || raw[idx^] < '0' || raw[idx^] > '9' do return false
	value := i64(0)
	for idx^ < len(raw) {
		c := raw[idx^]
		if c < '0' || c > '9' do break
		value = value * 10 + i64(c - '0')
		idx^ += 1
	}
	out^ = value * sign
	return true
}

@(private = "file")
batch_skip_number :: proc(raw: []u8, idx: ^int) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) do return false
	c := raw[idx^]
	if c == '-' || c == '+' do idx^ += 1
	have_digit := false
	for idx^ < len(raw) {
		ch := raw[idx^]
		if ch < '0' || ch > '9' do break
		have_digit = true
		idx^ += 1
	}
	if idx^ < len(raw) && raw[idx^] == '.' {
		idx^ += 1
		for idx^ < len(raw) {
			ch := raw[idx^]
			if ch < '0' || ch > '9' do break
			have_digit = true
			idx^ += 1
		}
	}
	if idx^ < len(raw) && (raw[idx^] == 'e' || raw[idx^] == 'E') {
		idx^ += 1
		if idx^ < len(raw) && (raw[idx^] == '+' || raw[idx^] == '-') do idx^ += 1
		for idx^ < len(raw) {
			ch := raw[idx^]
			if ch < '0' || ch > '9' do break
			have_digit = true
			idx^ += 1
		}
	}
	return have_digit
}

@(private = "file")
batch_skip_literal :: proc(raw: []u8, idx: ^int, lit: string) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ + len(lit) > len(raw) do return false
	for i in 0 ..< len(lit) {
		if raw[idx^ + i] != lit[i] do return false
	}
	idx^ += len(lit)
	return true
}

@(private = "file")
batch_skip_value :: proc(raw: []u8, idx: ^int) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) do return false
	switch raw[idx^] {
	case '"':
		s, e := 0, 0
		return batch_parse_string_span(raw, idx, &s, &e)
	case '{':
		idx^ += 1
		batch_skip_ws(raw, idx)
		if idx^ < len(raw) && raw[idx^] == '}' {
			idx^ += 1
			return true
		}
		for {
			ks, ke := 0, 0
			if !batch_parse_string_span(raw, idx, &ks, &ke) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) || raw[idx^] != ':' do return false
			idx^ += 1
			if !batch_skip_value(raw, idx) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) do return false
			if raw[idx^] == ',' {
				idx^ += 1
				continue
			}
			if raw[idx^] == '}' {
				idx^ += 1
				return true
			}
			return false
		}
	case '[':
		idx^ += 1
		batch_skip_ws(raw, idx)
		if idx^ < len(raw) && raw[idx^] == ']' {
			idx^ += 1
			return true
		}
		for {
			if !batch_skip_value(raw, idx) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) do return false
			if raw[idx^] == ',' {
				idx^ += 1
				continue
			}
			if raw[idx^] == ']' {
				idx^ += 1
				return true
			}
			return false
		}
	case 't':
		return batch_skip_literal(raw, idx, "true")
	case 'f':
		return batch_skip_literal(raw, idx, "false")
	case 'n':
		return batch_skip_literal(raw, idx, "null")
	case:
		return batch_skip_number(raw, idx)
	}
}

@(private = "file")
batch_key_equals :: proc(raw: []u8, start, end: int, lit: string) -> bool {
	if end-start != len(lit) do return false
	for i in 0 ..< len(lit) {
		if raw[start + i] != lit[i] do return false
	}
	return true
}

@(private = "file")
batch_copy_string :: proc(dst: []u8, raw: []u8, start, end: int) -> u8 {
	n := end - start
	if n < 0 do return 0
	if n > len(dst) do n = len(dst)
	for i in 0 ..< n {
		dst[i] = raw[start + i]
	}
	return u8(n)
}

@(private = "file")
batch_parse_events_segment :: proc(
	raw: []u8,
	idx: ^int,
	skip_events: int,
	out: ^Parsed_Batched_Frame,
) -> bool {
	batch_skip_ws(raw, idx)
	if idx^ >= len(raw) || raw[idx^] != '[' do return false
	idx^ += 1

	total := 0
	stored := 0

	batch_skip_ws(raw, idx)
	if idx^ < len(raw) && raw[idx^] == ']' {
		idx^ += 1
		out.total_events = 0
		out.event_count = 0
		out.has_more = false
		return true
	}

	for {
		batch_skip_ws(raw, idx)
		if idx^ >= len(raw) || raw[idx^] != '{' do return false
		idx^ += 1

		event := Parsed_Batch_Event_View{
			event_index = total,
		}
		for {
			ks, ke := 0, 0
			if !batch_parse_string_span(raw, idx, &ks, &ke) do return false
			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) || raw[idx^] != ':' do return false
			idx^ += 1

			switch {
			case batch_key_equals(raw, ks, ke, "dseq"):
				if !batch_parse_int(raw, idx, &event.dseq) do return false
			case batch_key_equals(raw, ks, ke, "dprev"):
				if !batch_parse_int(raw, idx, &event.dprev) do return false
			case batch_key_equals(raw, ks, ke, "dts"):
				if !batch_parse_int(raw, idx, &event.dts) do return false
			case batch_key_equals(raw, ks, ke, "dti"):
				if !batch_parse_int(raw, idx, &event.dti) do return false
			case batch_key_equals(raw, ks, ke, "p"):
				batch_skip_ws(raw, idx)
				event.payload_start = idx^
				if !batch_skip_value(raw, idx) do return false
				event.payload_end = idx^
			case:
				if !batch_skip_value(raw, idx) do return false
			}

			batch_skip_ws(raw, idx)
			if idx^ >= len(raw) do return false
			if raw[idx^] == ',' {
				idx^ += 1
				continue
			}
			if raw[idx^] == '}' {
				idx^ += 1
				break
			}
			return false
		}

		if total >= skip_events && stored < BATCH_EVENT_VIEW_CAP {
			out.events[stored] = event
			stored += 1
		}
		total += 1

		batch_skip_ws(raw, idx)
		if idx^ >= len(raw) do return false
		if raw[idx^] == ',' {
			idx^ += 1
			continue
		}
		if raw[idx^] == ']' {
			idx^ += 1
			break
		}
		return false
	}

	out.total_events = total
	out.event_count = stored
	out.has_more = skip_events + stored < total
	return true
}

// Parse a batched frame and expose event payload views without allocating.
// skip_events allows deterministic split processing when event count exceeds cap.
parse_batched_frame :: proc(raw: []u8, out: ^Parsed_Batched_Frame, skip_events: int = 0) -> bool {
	if out == nil do return false
	out^ = {}
	skip := skip_events
	if skip < 0 do skip = 0

	idx := 0
	batch_skip_ws(raw, &idx)
	if idx >= len(raw) || raw[idx] != '{' do return false
	idx += 1

	is_batch := false
	for {
		ks, ke := 0, 0
		if !batch_parse_string_span(raw, &idx, &ks, &ke) do return false
		batch_skip_ws(raw, &idx)
		if idx >= len(raw) || raw[idx] != ':' do return false
		idx += 1

		switch {
		case batch_key_equals(raw, ks, ke, "type"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			is_batch = batch_key_equals(raw, vs, ve, "batch")
		case batch_key_equals(raw, ks, ke, "stream_id"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.stream_id_len = batch_copy_string(out.stream_id_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "venue"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.venue_len = batch_copy_string(out.venue_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "symbol"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.symbol_len = batch_copy_string(out.symbol_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "channel"):
			vs, ve := 0, 0
			if !batch_parse_string_span(raw, &idx, &vs, &ve) do return false
			out.channel_len = batch_copy_string(out.channel_buf[:], raw, vs, ve)
		case batch_key_equals(raw, ks, ke, "base_seq"):
			if !batch_parse_int(raw, &idx, &out.base_seq) do return false
		case batch_key_equals(raw, ks, ke, "count"):
			tmp := i64(0)
			if !batch_parse_int(raw, &idx, &tmp) do return false
			out.count = int(tmp)
		case batch_key_equals(raw, ks, ke, "ts_server_base"):
			if !batch_parse_int(raw, &idx, &out.ts_server_base) do return false
		case batch_key_equals(raw, ks, ke, "ts_ingest_base"):
			if !batch_parse_int(raw, &idx, &out.ts_ingest_base) do return false
		case batch_key_equals(raw, ks, ke, "events"):
			if !batch_parse_events_segment(raw, &idx, skip, out) do return false
		case:
			if !batch_skip_value(raw, &idx) do return false
		}

		batch_skip_ws(raw, &idx)
		if idx >= len(raw) do return false
		if raw[idx] == ',' {
			idx += 1
			continue
		}
		if raw[idx] == '}' {
			idx += 1
			break
		}
		return false
	}

	// Defensive default when "count" field is absent.
	if out.count <= 0 do out.count = out.total_events
	return is_batch
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
	result.meta.prev_seq = env.prev_seq
	// Prefer ts_server (Terminal_V1) over ts_ingest (legacy).
	result.meta.server_ts_ms = env.ts_server if env.ts_server > 0 else env.ts_ingest
	result.meta.has_ts_server = env.ts_server > 0
	result.meta.subject_id = util.subject_id64(env.subject)

	ft := util.parse_frame_type(env.type_str)
	result.meta.is_snapshot = ft == .Snapshot

	switch ft {
	case .Ack:
		// Hello ACK: op="hello" ack frame carries negotiated_features.
		if env.op == "hello" {
			result.kind = .Hello_Ack
			if ha, ok := parse_hello_ack(raw); ok {
				result.data.hello_ack = ha
			}
			return result
		}
		result.kind = .Ack
		result.data.ack = Parsed_Ack{op = env.op, subject = env.subject, stream_id = env.stream_id}
		return result
	case .Pong:
		result.kind = .Pong
		if p, ok := parse_pong(raw); ok {
			result.data.pong = p
		}
		return result
	case .Metrics:
		result.kind = .Metrics
		if m, ok := parse_metrics(raw); ok {
			result.data.server_metrics = m
		}
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
		if e, ok := parse_error(raw); ok {
			result.data.error_detail = e
		}
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
	case .Batch, .Last, .Unknown:
		return result
	case .Event, .Snapshot, .Signal:
		// For snapshot frames: parse integrity fields if present.
		if ft == .Snapshot {
			snap: util.MR_Snapshot_Frame
			if json.unmarshal(raw, &snap) == nil {
				result.meta.snapshot_seq = snap.snapshot_seq
				result.meta.watermark_seq = snap.watermark_seq
				// Copy snapshot hash (up to 16 hex chars).
				hash_n := min(len(snap.snapshot_hash), len(result.meta.snapshot_hash))
				for i in 0 ..< hash_n {
					result.meta.snapshot_hash[i] = snap.snapshot_hash[i]
				}
				result.meta.snapshot_hash_len = u8(hash_n)
			}
		}
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
			// Trust envelope type=snapshot even when payload omits/incorrectly sets IsSnapshot.
			// Delivery can emit snapshot envelopes backed by cached payloads that don't carry
			// a reliable IsSnapshot marker.
			if result.meta.is_snapshot {
				r.is_snapshot = true
			}
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
	case "insights.microstructure_evidence":
		if r, ok := parse_microstructure_evidence(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Evidence
			result.data.evidence = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "signal":
		if r, ok := parse_signal(raw, result.meta.server_ts_ms, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Signal
			result.data.signal = r
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

parse_microstructure_evidence :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Evidence, bool) {
	frame: util.MR_Microstructure_Evidence_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	p := frame.payload
	out: Parsed_Evidence
	out.confidence = p.confidence
	out.unix = p.ts_ingest if p.ts_ingest > 0 else ts
	out.subject_id = subject_id
	out.seq = p.seq

	nk := min(len(p.kind), len(out.kind))
	for i in 0 ..< nk {
		out.kind[i] = p.kind[i]
	}
	out.kind_len = u8(nk)

	nr := min(len(p.reason), len(out.reason))
	for i in 0 ..< nr {
		out.reason[i] = p.reason[i]
	}
	out.reason_len = u8(nr)

	fc := min(len(p.features), len(out.feature_tags))
	for fi in 0 ..< fc {
		tn := min(len(p.features[fi]), len(out.feature_tags[fi]))
		for tj in 0 ..< tn {
			out.feature_tags[fi][tj] = p.features[fi][tj]
		}
	}
	fv := min(len(p.feature_values), len(out.feature_vals))
	for vi in 0 ..< fv {
		out.feature_vals[vi] = p.feature_values[vi]
	}
	out.feature_count = fc
	return out, true
}

parse_signal :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Signal, bool) {
	frame: util.MR_Signal_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	p := frame.payload
	if !f64_valid(p.confidence) || p.confidence < 0 || p.confidence > 1 do return {}, false
	if !f64_valid(p.regime_strength) do return {}, false
	if len(p.kind) == 0 || len(p.severity) == 0 do return {}, false

	regime := p.regime_kind
	if len(regime) == 0 {
		if p.regime_strength != 0 do return {}, false
	} else if p.regime_strength < 0 || p.regime_strength > 1 {
		return {}, false
	}

	ts_source := ts
	if ts_source <= 0 do ts_source = frame.ts_server
	unix := util.normalize_unix_seconds(ts_source)

	out: Parsed_Signal
	out.confidence = p.confidence
	out.regime_strength = p.regime_strength
	out.unix = unix
	out.subject_id = subject_id
	out.seq = frame.seq

	nk := min(len(p.kind), len(out.kind))
	for i in 0 ..< nk {
		out.kind[i] = p.kind[i]
	}
	out.kind_len = u8(nk)

	ns := min(len(p.severity), len(out.severity))
	for i in 0 ..< ns {
		out.severity[i] = p.severity[i]
	}
	out.severity_len = u8(ns)

	nr := min(len(p.reason), len(out.reason))
	for i in 0 ..< nr {
		out.reason[i] = p.reason[i]
	}
	out.reason_len = u8(nr)

	ng := min(len(regime), len(out.regime))
	for i in 0 ..< ng {
		out.regime[i] = regime[i]
	}
	out.regime_len = u8(ng)

	return out, true
}

// --- Validation helper ---

@(private = "package")
f64_valid :: proc(v: f64) -> bool {
	return !math.is_nan(v) && !math.is_inf(v, 0)
}

// --- Individual payload parsers ---

@(private = "file")
parse_trade :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Trade, bool) {
	frame: util.MR_Trade_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	trade := frame.payload

	if !f64_valid(trade.price) || !f64_valid(trade.size) do return {}, false
	if trade.price < 0 || trade.size < 0 do return {}, false

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

	out_ac := 0
	for i in 0 ..< ac {
		p := bd.asks[i].price
		s := bd.asks[i].size
		if !f64_valid(p) || !f64_valid(s) || p < 0 || s < 0 do continue
		result.ask_prices[out_ac] = p
		result.ask_sizes[out_ac]  = s
		out_ac += 1
	}
	result.ask_count = out_ac

	out_bc := 0
	for i in 0 ..< bc {
		p := bd.bids[i].price
		s := bd.bids[i].size
		if !f64_valid(p) || !f64_valid(s) || p < 0 || s < 0 do continue
		result.bid_prices[out_bc] = p
		result.bid_sizes[out_bc]  = s
		out_bc += 1
	}
	result.bid_count = out_bc
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

	if !f64_valid(s.mark_price_close) do return {}, false

	unix := util.normalize_unix_seconds(s.window_end_ts if s.window_end_ts != 0 else ts)

	return Parsed_Stats{
		mark_price = s.mark_price_close,
		funding    = f64_valid(s.funding_rate_last) ? s.funding_rate_last : 0,
		tbuy       = f64_valid(s.liq_buy_volume) ? s.liq_buy_volume : 0,
		tsell      = f64_valid(s.liq_sell_volume) ? s.liq_sell_volume : 0,
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

	result: Parsed_Heatmap
	out := 0
	prev_low := -math.F64_MAX
	prev_high := -math.F64_MAX
	for i in 0 ..< lc {
		c := hm.cells[i]
		mid := (c.price_bucket_low + c.price_bucket_high) / 2.0
		total := c.bid_liquidity + c.ask_liquidity + c.trade_volume
		if !f64_valid(mid) || !f64_valid(total) do continue

		if out == 0 {
			price_group = c.price_bucket_high - c.price_bucket_low
		}

		// Aggregate size buckets at the same price level. Backend sorts cells
		// by (price_bucket_low, size_bucket), so consecutive cells with identical
		// price ranges represent different size buckets for the same price level.
		if out > 0 && c.price_bucket_low == prev_low && c.price_bucket_high == prev_high {
			result.sizes[out - 1] += total
			if result.sizes[out - 1] > max_s do max_s = result.sizes[out - 1]
			continue
		}

		prev_low = c.price_bucket_low
		prev_high = c.price_bucket_high
		if mid < min_p do min_p = mid
		if mid > max_p do max_p = mid
		if total > max_s do max_s = total
		result.prices[out] = mid
		result.sizes[out]  = total
		out += 1
	}

	result.level_count = out
	result.price_group = price_group
	result.min_price = min_p if out > 0 else 0
	result.max_price = max_p if out > 0 else 0
	result.max_size = max_s
	result.unix = unix
	result.window_start_ms = hm.window_start_ts > util.UNIX_MS_THRESHOLD ? hm.window_start_ts : hm.window_start_ts * 1000
	result.subject_id = subject_id
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

	result: Parsed_VPVR
	out := 0
	for i in 0 ..< lc {
		b := vp.buckets[i]
		mid := (b.price_low + b.price_high) / 2.0
		if !f64_valid(mid) || !f64_valid(b.buy_volume) || !f64_valid(b.sell_volume) do continue

		if out == 0 {
			price_group = b.price_high - b.price_low
		}
		if mid < min_p do min_p = mid
		if mid > max_p do max_p = mid
		result.prices[out] = mid
		result.buys[out]   = b.buy_volume
		result.sells[out]  = b.sell_volume
		out += 1
	}

	result.level_count = out
	result.price_group = price_group
	result.min_price = min_p if out > 0 else 0
	result.max_price = max_p if out > 0 else 0
	result.unix = unix
	result.subject_id = subject_id
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

	// Backend may send proto_ver (legacy) or protocol_version (Terminal_V1).
	pv := frame.payload.proto_ver
	if pv <= 0 do pv = frame.payload.protocol_version

	caps := frame.payload.capabilities
	h := Parsed_Hello{
		proto_ver             = pv,
		server_time           = frame.payload.server_time,
		server_instance_id    = frame.payload.server_instance_id,
		topic_count           = len(caps.topics),
		venue_count           = len(caps.venues),
		symbol_count          = len(caps.symbols),
		valid                 = true,
		reject                = .None,
		max_subscriptions     = caps.max_subscriptions_per_connection,
		max_symbols           = caps.max_symbols_per_connection,
		max_frame_bytes       = caps.max_frame_bytes,
		outbound_queue_size   = caps.outbound_queue_size,
		metrics_cadence_ms    = caps.metrics_cadence_ms,
		keepalive_interval_ms = caps.keepalive_interval_ms,
		rate_limit_enabled    = caps.rate_limit.enabled,
		rate_limit_max_per_sec = caps.rate_limit.max_per_second,
		rate_limit_burst      = caps.rate_limit.burst_capacity,
	}
	// Copy supported features into fixed slots.
	fc := min(len(caps.supported_features), MAX_FEATURE_SLOTS)
	h.supported_feature_count = fc
	for i in 0 ..< fc {
		f := caps.supported_features[i]
		n := min(len(f), len(h.supported_features[i].name))
		for j in 0 ..< n {
			h.supported_features[i].name[j] = f[j]
		}
		h.supported_features[i].len = u8(n)
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
		if !f64_valid(c.Open) || !f64_valid(c.High) || !f64_valid(c.Low) || !f64_valid(c.ClosePrice) || !f64_valid(c.Volume) do continue

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
parse_pong :: proc(raw: []u8) -> (Parsed_Pong, bool) {
	frame: util.MR_Pong_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	rtt := i64(0)
	if frame.ts_client > 0 && frame.ts_server > 0 {
		rtt = frame.ts_server - frame.ts_client
		if rtt < 0 do rtt = 0
	}
	return Parsed_Pong{
		ts_client  = frame.ts_client,
		ts_server  = frame.ts_server,
		rtt_ms     = rtt,
		request_id = frame.request_id,
	}, true
}

@(private = "file")
parse_metrics :: proc(raw: []u8) -> (Parsed_Metrics, bool) {
	frame: util.MR_Metrics_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	p := frame.payload
	m := Parsed_Metrics{
		ws_dropped_total              = p.ws_dropped_total,
		ws_queue_len                  = p.ws_queue_len,
		ws_lag_ms                     = p.ws_lag_ms,
		publish_to_deliver_latency_ms = p.publish_to_deliver_latency_ms,
		serialize_errors_total        = p.serialize_errors_total,
		resync_total                  = p.resync_total,
		active_subscriptions          = p.active_subscriptions,
		messages_out_total            = p.messages_out_total,
		backpressure_level            = p.backpressure_level,
		queue_capacity                = p.queue_capacity,
		queue_high_watermark          = p.queue_high_watermark,
	}
	// Copy recommended_action into fixed buffer.
	ra_n := min(len(p.recommended_action), len(m.recommended_action_buf))
	for i in 0 ..< ra_n {
		m.recommended_action_buf[i] = p.recommended_action[i]
	}
	m.recommended_action_len = u8(ra_n)
	return m, true
}

@(private = "file")
parse_error :: proc(raw: []u8) -> (Parsed_Error, bool) {
	frame: util.MR_Error_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return Parsed_Error{
		op          = frame.op,
		request_id  = frame.request_id,
		code        = frame.problem.code,
		message     = frame.problem.message,
		error_code  = frame.problem.error_code,
		action_hint = frame.problem.action_hint,
	}, true
}

@(private = "file")
MR_Hello_Ack_Envelope :: struct {
	payload: util.MR_Hello_Ack_Frame `json:"payload"`,
}

@(private = "file")
parse_hello_ack :: proc(raw: []u8) -> (Parsed_Hello_Ack, bool) {
	frame: MR_Hello_Ack_Envelope
	if json.unmarshal(raw, &frame) != nil do return {}, false
	result: Parsed_Hello_Ack
	fc := min(len(frame.payload.negotiated_features), MAX_FEATURE_SLOTS)
	result.negotiated_feature_count = fc
	for i in 0 ..< fc {
		f := frame.payload.negotiated_features[i]
		n := min(len(f), len(result.negotiated_features[i].name))
		for j in 0 ..< n {
			result.negotiated_features[i].name[j] = f[j]
		}
		result.negotiated_features[i].len = u8(n)
	}
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
	if !f64_valid(c.Open) || !f64_valid(c.High) || !f64_valid(c.Low) || !f64_valid(c.ClosePrice) || !f64_valid(c.Volume) {
		return {}, false
	}

	return Parsed_Candle{
		open            = c.Open,
		high            = c.High,
		low             = c.Low,
		close           = c.ClosePrice,
		volume          = c.Volume,
		buy_vol         = f64_valid(c.BuyVolume) ? c.BuyVolume : 0,
		sell_vol        = f64_valid(c.SellVolume) ? c.SellVolume : 0,
		trade_count     = c.TradeCount,
		window_start_ts = c.WindowStartTs,
		window_end_ts   = c.WindowEndTs,
		is_closed       = c.IsClosed,
		subject_id      = subject_id,
	}, true
}
