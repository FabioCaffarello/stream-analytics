package services

// Shared MR protocol message parser — pure function, zero platform imports.
// Both native and web adapters call parse_mr_message, then write results
// into their own staging buffers under their own threading model.
//
// Eliminates ~400 LOC of duplicated parsing logic.

import "core:encoding/json"
import "core:strings"
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
	window_ms:  i64,
	ts_ingest_ms: i64,
	quality_flags: u32,
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

Parsed_Tape :: struct {
	last_price:      f64,
	total_volume:    f64,
	buy_volume:      f64,
	sell_volume:     f64,
	trade_count:     i64,
	rate_per_sec:    f64,
	imbalance:       f64,
	is_burst:        bool,
	window_start_ts: i64,
	window_end_ts:   i64,
	unix:            i64,
	subject_id:      u64,
	seq:             i64,
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
	Tape,
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
	tape:           Parsed_Tape,
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
	legacy_subject:   bool,
	parse_fallback:   bool,
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


// --- Telemetry counters (caller accumulates into their own state) ---

Parse_Telemetry :: struct {
	parse_errors:    int,
	envelope_errors: int,
	unknown_streams: int,
	canonical_stats_frames: int,
	stats_fallback_frames:  int,
	canonical_evidence_frames: int,
	legacy_evidence_frames:    int,
	evidence_fallback_frames:  int,
	canonical_signal_frames:   int,
	legacy_signal_frames:      int,
	signal_fallback_frames:    int,
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
	legacyEvidenceSubject := strings.has_prefix(env.subject, "insights.microstructure_evidence/")
	legacySignalSubject := strings.has_prefix(env.subject, "signal/composite/")
	if legacyEvidenceSubject || legacySignalSubject {
		result.meta.legacy_subject = true
	}

	switch stream {
	case "marketdata.trade":
		if r, ok := parse_trade(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Trade
			result.data.trade = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "aggregation.tape":
		if r, ok := parse_tape(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Tape
			result.data.tape = r
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
	case "aggregation.snapshot":
		if r, ok := parse_aggregation_snapshot(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			r.is_snapshot = true
			result.meta.is_snapshot = true
			result.kind = .Orderbook
			result.data.ob = r
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "aggregation.stats":
		if r, ok, used_fallback := parse_stats(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Stats
			result.data.stats = r
			result.meta.parse_fallback = used_fallback
			if telemetry != nil {
				telemetry.canonical_stats_frames += 1
			}
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
	case "liquidity.evidence":
		if r, ok, _, used_fallback := parse_microstructure_evidence(raw, env.ts_ingest, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Evidence
			result.data.evidence = r
			result.meta.parse_fallback = used_fallback
			if telemetry != nil {
				telemetry.canonical_evidence_frames += 1
			}
		} else if telemetry != nil {
			telemetry.parse_errors += 1
		}
	case "insights.microstructure_evidence":
		if telemetry != nil {
			telemetry.legacy_evidence_frames += 1
		}
		return result
	case "signal", "signal.event":
		if legacySignalSubject {
			if telemetry != nil {
				telemetry.legacy_signal_frames += 1
			}
			return result
		}
		if r, ok, _, used_fallback := parse_signal(raw, result.meta.server_ts_ms, subject_id); ok {
			r.seq = result.meta.seq
			result.kind = .Signal
			result.data.signal = r
			result.meta.parse_fallback = used_fallback
			if telemetry != nil {
				telemetry.canonical_signal_frames += 1
			}
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
