package services

import "core:encoding/json"
import "core:math"
import "mr:util"

parse_microstructure_evidence :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Evidence, bool, bool, bool) {
	out: Parsed_Evidence
	out.subject_id = subject_id

	// Runtime path accepts only canonical evidence frame.
	v2: util.MR_Microstructure_Evidence_Frame_V2
	if json.unmarshal(raw, &v2) != nil do return {}, false, false, false
	p2 := v2.payload
	out.confidence = p2.confidence
	if p2.ts_ingest > 0 {
		out.unix = p2.ts_ingest
	} else if p2.ts_server > 0 {
		out.unix = p2.ts_server
	} else {
		out.unix = ts
	}
	out.seq = p2.seq

	kind := p2.kind
	if len(kind) == 0 {
		kind = p2.type_str
	}
	nk := min(len(kind), len(out.kind))
	for i in 0 ..< nk {
		out.kind[i] = kind[i]
	}
	out.kind_len = u8(nk)
	if out.kind_len == 0 do return {}, false, false, false

	reason := p2.reason
	if len(reason) == 0 {
		reason = p2.explanation
	}
	nr := min(len(reason), len(out.reason))
	for i in 0 ..< nr {
		out.reason[i] = reason[i]
	}
	out.reason_len = u8(nr)

	fc := min(len(p2.features), len(out.feature_tags))
	for fi in 0 ..< fc {
		tag := p2.features[fi].key
		tn := min(len(tag), len(out.feature_tags[fi]))
		for tj in 0 ..< tn {
			out.feature_tags[fi][tj] = tag[tj]
		}
		out.feature_vals[fi] = p2.features[fi].value
	}
	out.feature_count = fc
	return out, true, false, false
}

parse_signal :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Signal, bool, bool, bool) {
	frame_v2: util.MR_Signal_Frame_V2
	if json.unmarshal(raw, &frame_v2) != nil do return {}, false, false, false
	p2 := frame_v2.payload
	kind := p2.kind
	if len(kind) == 0 {
		kind = p2.type_str
	}
	reason := p2.reason
	if len(reason) == 0 {
		reason = p2.explanation
	}
	if !f64_valid(p2.confidence) || p2.confidence < 0 || p2.confidence > 1 do return {}, false, false, false
	if !f64_valid(p2.regime_strength) do return {}, false, false, false
	if len(kind) == 0 || len(p2.severity) == 0 do return {}, false, false, false

	regime := p2.regime_kind
	if len(regime) == 0 {
		if p2.regime_strength != 0 do return {}, false, false, false
	} else if p2.regime_strength < 0 || p2.regime_strength > 1 {
		return {}, false, false, false
	}

	ts_source := ts
	if ts_source <= 0 do ts_source = frame_v2.ts_server
	unix := util.normalize_unix_seconds(ts_source)

	out: Parsed_Signal
	out.confidence = p2.confidence
	out.regime_strength = p2.regime_strength
	out.unix = unix
	out.subject_id = subject_id
	out.seq = frame_v2.seq

	nk := min(len(kind), len(out.kind))
	for i in 0 ..< nk {
		out.kind[i] = kind[i]
	}
	out.kind_len = u8(nk)

	ns := min(len(p2.severity), len(out.severity))
	for i in 0 ..< ns {
		out.severity[i] = p2.severity[i]
	}
	out.severity_len = u8(ns)

	nr := min(len(reason), len(out.reason))
	for i in 0 ..< nr {
		out.reason[i] = reason[i]
	}
	out.reason_len = u8(nr)

	ng := min(len(regime), len(out.regime))
	for i in 0 ..< ng {
		out.regime[i] = regime[i]
	}
	out.regime_len = u8(ng)

	return out, true, false, false
}

// --- Validation helper ---

@(private = "package")
f64_valid :: proc(v: f64) -> bool {
	return !math.is_nan(v) && !math.is_inf(v, 0)
}

// --- Individual payload parsers ---

@(private = "package")
parse_trade :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Trade, bool) {
	frame: util.MR_Trade_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_trade_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_trade_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Trade, bool) {
	trade: util.MR_Trade
	if json.unmarshal(payload_raw, &trade) != nil do return {}, false
	return parse_trade_from_payload(trade, ts, subject_id)
}

@(private = "package")
parse_trade_from_payload :: proc(trade: util.MR_Trade, ts: i64, subject_id: u64) -> (Parsed_Trade, bool) {
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

@(private = "package")
parse_tape :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Tape, bool) {
	frame: util.MR_Tape_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_tape_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_tape_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Tape, bool) {
	tape: util.MR_Tape_Payload
	if json.unmarshal(payload_raw, &tape) != nil do return {}, false
	return parse_tape_from_payload(tape, ts, subject_id)
}

@(private = "package")
parse_tape_from_payload :: proc(tape: util.MR_Tape_Payload, ts: i64, subject_id: u64) -> (Parsed_Tape, bool) {
	if tape.TradeCount < 0 do return {}, false
	if !f64_valid(tape.LastPrice) || !f64_valid(tape.TotalVolume) do return {}, false
	if !f64_valid(tape.BuyVolume) || !f64_valid(tape.SellVolume) do return {}, false
	if !f64_valid(tape.Rate) || !f64_valid(tape.Imbalance) do return {}, false
	if tape.TotalVolume < 0 || tape.BuyVolume < 0 || tape.SellVolume < 0 do return {}, false
	if tape.Imbalance < -1 || tape.Imbalance > 1 do return {}, false

	unix := util.normalize_unix_seconds(tape.WindowEndTs if tape.WindowEndTs != 0 else ts)
	return Parsed_Tape{
		last_price      = tape.LastPrice,
		total_volume    = tape.TotalVolume,
		buy_volume      = tape.BuyVolume,
		sell_volume     = tape.SellVolume,
		trade_count     = tape.TradeCount,
		rate_per_sec    = tape.Rate,
		imbalance       = tape.Imbalance,
		is_burst        = tape.IsBurst,
		window_start_ts = tape.WindowStartTs,
		window_end_ts   = tape.WindowEndTs,
		unix            = unix,
		subject_id      = subject_id,
		seq             = tape.Seq,
	}, true
}

@(private = "package")
parse_open_interest_tick :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Stats, bool) {
	frame: util.MR_Open_Interest_Tick_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_open_interest_tick_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_open_interest_tick_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Stats, bool) {
	payload: util.MR_Open_Interest_Tick_Payload
	if json.unmarshal(payload_raw, &payload) != nil do return {}, false
	return parse_open_interest_tick_from_payload(payload, ts, subject_id)
}

@(private = "package")
parse_open_interest_tick_from_payload :: proc(payload: util.MR_Open_Interest_Tick_Payload, ts: i64, subject_id: u64) -> (Parsed_Stats, bool) {
	if !f64_valid(payload.open_interest) || payload.open_interest < 0 do return {}, false
	ingest_ms := ts
	if ingest_ms <= 0 do ingest_ms = payload.timestamp
	unix := util.normalize_unix_seconds(payload.timestamp if payload.timestamp != 0 else ingest_ms)
	return Parsed_Stats{
		mark_price   = payload.open_interest,
		funding      = 0,
		tbuy         = 0,
		tsell        = 0,
		window_ms    = 0,
		ts_ingest_ms = ingest_ms,
		quality_flags = 0,
		unix         = unix,
		subject_id   = subject_id,
	}, true
}

// S47: Returns Parsed_Open_Interest (was Parsed_Stats — identity collapse fix).
@(private = "package")
parse_open_interest_window :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Open_Interest, bool) {
	frame: util.MR_Open_Interest_Window_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_open_interest_window_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_open_interest_window_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Open_Interest, bool) {
	payload: util.MR_Open_Interest_Window_Payload
	if json.unmarshal(payload_raw, &payload) != nil do return {}, false
	return parse_open_interest_window_from_payload(payload, ts, subject_id)
}

@(private = "package")
parse_open_interest_window_from_payload :: proc(payload: util.MR_Open_Interest_Window_Payload, ts: i64, subject_id: u64) -> (Parsed_Open_Interest, bool) {
	if !f64_valid(payload.OpenInterest) || payload.OpenInterest < 0 do return {}, false
	if !f64_valid(payload.Delta) || !f64_valid(payload.DeltaPct) do return {}, false
	ingest_ms := payload.TsIngestMs
	if ingest_ms <= 0 do ingest_ms = ts
	unix := util.normalize_unix_seconds(payload.WindowEndTs if payload.WindowEndTs != 0 else ingest_ms)
	return Parsed_Open_Interest{
		open_interest   = payload.OpenInterest,
		delta           = payload.Delta,
		delta_pct       = payload.DeltaPct,
		window_start_ts = payload.WindowStartTs,
		window_end_ts   = payload.WindowEndTs,
		unix            = unix,
		subject_id      = subject_id,
		seq             = payload.Seq,
	}, true
}

// S47: Returns Parsed_Delta_Volume (was Parsed_Stats — identity collapse fix).
@(private = "package")
parse_delta_volume :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Delta_Volume, bool) {
	frame: util.MR_Delta_Volume_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_delta_volume_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_delta_volume_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Delta_Volume, bool) {
	payload: util.MR_Delta_Volume_Payload
	if json.unmarshal(payload_raw, &payload) != nil do return {}, false
	return parse_delta_volume_from_payload(payload, ts, subject_id)
}

@(private = "package")
parse_delta_volume_from_payload :: proc(payload: util.MR_Delta_Volume_Payload, ts: i64, subject_id: u64) -> (Parsed_Delta_Volume, bool) {
	if !f64_valid(payload.BuyVolume) || !f64_valid(payload.SellVolume) || !f64_valid(payload.DeltaVolume) do return {}, false
	if payload.BuyVolume < 0 || payload.SellVolume < 0 do return {}, false
	ingest_ms := payload.TsIngestMs
	if ingest_ms <= 0 do ingest_ms = ts
	unix := util.normalize_unix_seconds(payload.WindowEndTs if payload.WindowEndTs != 0 else ingest_ms)
	return Parsed_Delta_Volume{
		buy_volume      = payload.BuyVolume,
		sell_volume     = payload.SellVolume,
		delta_volume    = payload.DeltaVolume,
		window_start_ts = payload.WindowStartTs,
		window_end_ts   = payload.WindowEndTs,
		unix            = unix,
		subject_id      = subject_id,
		seq             = payload.Seq,
	}, true
}

// S47: Returns Parsed_CVD (was Parsed_Stats — identity collapse fix).
@(private = "package")
parse_cvd :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_CVD, bool) {
	frame: util.MR_CVD_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_cvd_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_cvd_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_CVD, bool) {
	payload: util.MR_CVD_Payload
	if json.unmarshal(payload_raw, &payload) != nil do return {}, false
	return parse_cvd_from_payload(payload, ts, subject_id)
}

@(private = "package")
parse_cvd_from_payload :: proc(payload: util.MR_CVD_Payload, ts: i64, subject_id: u64) -> (Parsed_CVD, bool) {
	if !f64_valid(payload.DeltaVolume) || !f64_valid(payload.CVD) do return {}, false
	ingest_ms := payload.TsIngestMs
	if ingest_ms <= 0 do ingest_ms = ts
	unix := util.normalize_unix_seconds(payload.WindowEndTs if payload.WindowEndTs != 0 else ingest_ms)
	return Parsed_CVD{
		delta_volume    = payload.DeltaVolume,
		cvd             = payload.CVD,
		window_start_ts = payload.WindowStartTs,
		window_end_ts   = payload.WindowEndTs,
		unix            = unix,
		subject_id      = subject_id,
		seq             = payload.Seq,
	}, true
}

// S47: Returns Parsed_Bar_Stats (was Parsed_Tape — identity collapse fix).
@(private = "package")
parse_bar_stats :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Bar_Stats, bool) {
	frame: util.MR_Bar_Stats_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_bar_stats_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_bar_stats_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Bar_Stats, bool) {
	payload: util.MR_Bar_Stats_Payload
	if json.unmarshal(payload_raw, &payload) != nil do return {}, false
	return parse_bar_stats_from_payload(payload, ts, subject_id)
}

@(private = "package")
parse_bar_stats_from_payload :: proc(payload: util.MR_Bar_Stats_Payload, ts: i64, subject_id: u64) -> (Parsed_Bar_Stats, bool) {
	if payload.TradeCount < 0 do return {}, false
	if !f64_valid(payload.LastPrice) || !f64_valid(payload.TotalVolume) do return {}, false
	if !f64_valid(payload.BuyVolume) || !f64_valid(payload.SellVolume) do return {}, false
	if !f64_valid(payload.Imbalance) do return {}, false
	if payload.TotalVolume < 0 || payload.BuyVolume < 0 || payload.SellVolume < 0 do return {}, false
	if payload.Imbalance < -1 || payload.Imbalance > 1 do return {}, false
	ingest_ms := payload.TsIngestMs
	if ingest_ms <= 0 do ingest_ms = ts
	unix := util.normalize_unix_seconds(payload.WindowEndTs if payload.WindowEndTs != 0 else ingest_ms)
	return Parsed_Bar_Stats{
		trade_count     = payload.TradeCount,
		buy_count       = payload.BuyCount,
		sell_count      = payload.SellCount,
		total_volume    = payload.TotalVolume,
		buy_volume      = payload.BuyVolume,
		sell_volume     = payload.SellVolume,
		vwap_price      = payload.VwapPrice,
		imbalance       = payload.Imbalance,
		is_burst        = payload.IsBurst,
		window_start_ts = payload.WindowStartTs,
		window_end_ts   = payload.WindowEndTs,
		unix            = unix,
		subject_id      = subject_id,
		seq             = payload.Seq,
	}, true
}

@(private = "package")
parse_book_delta :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_OB, bool) {
	frame: util.MR_Book_Delta_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_book_delta_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_aggregation_snapshot :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_OB, bool) {
	frame: util.MR_Aggregation_Snapshot_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	return parse_aggregation_snapshot_from_payload(frame.payload, ts, subject_id)
}

@(private = "package")
parse_book_delta_payload :: proc(payload_raw: []u8, ts: i64, subject_id: u64) -> (Parsed_OB, bool) {
	bd: util.MR_Book_Delta
	if json.unmarshal(payload_raw, &bd) != nil do return {}, false
	return parse_book_delta_from_payload(bd, ts, subject_id)
}

@(private = "package")
parse_book_delta_from_payload :: proc(bd: util.MR_Book_Delta, ts: i64, subject_id: u64) -> (Parsed_OB, bool) {
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

@(private = "package")
parse_aggregation_snapshot_from_payload :: proc(snap: util.MR_Aggregation_Snapshot, ts: i64, subject_id: u64) -> (Parsed_OB, bool) {
	unix := util.normalize_unix_seconds(snap.ts_ingest_ms if snap.ts_ingest_ms != 0 else ts)

	result: Parsed_OB
	ac := min(len(snap.asks), OB_STAGING_DEPTH)
	bc := min(len(snap.bids), OB_STAGING_DEPTH)
	result.ask_count = ac
	result.bid_count = bc
	result.is_snapshot = true
	result.unix = unix
	result.subject_id = subject_id

	if f64_valid(snap.best_ask_price) && f64_valid(snap.best_bid_price) &&
		snap.best_ask_price > 0 && snap.best_bid_price > 0 {
		result.last_price = (snap.best_ask_price + snap.best_bid_price) / 2.0
	}

	out_ac := 0
	for i in 0 ..< ac {
		p := snap.asks[i].price
		s := snap.asks[i].quantity
		if !f64_valid(p) || !f64_valid(s) || p < 0 || s < 0 do continue
		result.ask_prices[out_ac] = p
		result.ask_sizes[out_ac]  = s
		out_ac += 1
	}
	result.ask_count = out_ac

	out_bc := 0
	for i in 0 ..< bc {
		p := snap.bids[i].price
		s := snap.bids[i].quantity
		if !f64_valid(p) || !f64_valid(s) || p < 0 || s < 0 do continue
		result.bid_prices[out_bc] = p
		result.bid_sizes[out_bc]  = s
		out_bc += 1
	}
	result.bid_count = out_bc
	if result.last_price <= 0 && result.ask_count > 0 && result.bid_count > 0 {
		result.last_price = (result.ask_prices[0] + result.bid_prices[0]) / 2.0
	}
	return result, true
}

@(private = "package")
stats_payload_has_data :: proc(s: util.MR_Stats_Payload) -> bool {
	return s.window_start_ts != 0 ||
		s.window_end_ts != 0 ||
		s.window_ms != 0 ||
		s.ts_ingest_ms != 0 ||
		s.quality_flags != 0 ||
		s.mark_price_close != 0 ||
		s.funding_rate_last != 0 ||
		s.liq_buy_volume != 0 ||
		s.liq_sell_volume != 0
}

@(private = "package")
parse_stats :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Stats, bool, bool) {
	// Runtime path accepts only canonical wrapped payload.Stats frame.
	s: util.MR_Stats_Payload
	wrapped: util.MR_Stats_Frame_Wrapped
	if json.unmarshal(raw, &wrapped) == nil && stats_payload_has_data(wrapped.payload.stats) {
		s = wrapped.payload.stats
	} else {
		return {}, false, false
	}

	if !f64_valid(s.mark_price_close) do return {}, false, false

	ingest_ms := s.ts_ingest_ms
	if ingest_ms <= 0 do ingest_ms = ts
	unix := util.normalize_unix_seconds(s.window_end_ts if s.window_end_ts != 0 else ingest_ms)

	return Parsed_Stats{
		mark_price = s.mark_price_close,
		funding    = f64_valid(s.funding_rate_last) ? s.funding_rate_last : 0,
		tbuy       = f64_valid(s.liq_buy_volume) ? s.liq_buy_volume : 0,
		tsell      = f64_valid(s.liq_sell_volume) ? s.liq_sell_volume : 0,
		window_ms  = s.window_ms,
		ts_ingest_ms = ingest_ms,
		quality_flags = s.quality_flags,
		unix       = unix,
		subject_id = subject_id,
	}, true, false
}

@(private = "package")
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

@(private = "package")
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

@(private = "package")
Control_Payload :: struct {
	rtt_ms:  i64 `json:"rtt_ms"`,
	backlog: int `json:"backlog"`,
	dropped: int `json:"dropped"`,
}

@(private = "package")
Control_Frame :: struct {
	payload: Control_Payload `json:"payload"`,
}

@(private = "package")
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

@(private = "package")
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

@(private = "package")
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

@(private = "package")
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

@(private = "package")
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

@(private = "package")
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

@(private = "package")
MR_Hello_Ack_Envelope :: struct {
	payload: util.MR_Hello_Ack_Frame `json:"payload"`,
}

@(private = "package")
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

@(private = "package")
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
