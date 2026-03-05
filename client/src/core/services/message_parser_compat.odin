package services

import "core:encoding/json"
import "mr:util"

// Compatibility parsers kept for offline migration/forensics only.
// Runtime hot path must not call these helpers.

parse_stats_flat_compat :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Stats, bool) {
	frame: util.MR_Stats_Frame
	if json.unmarshal(raw, &frame) != nil do return {}, false
	s := frame.payload
	if !stats_payload_has_data(s) do return {}, false
	if !f64_valid(s.mark_price_close) do return {}, false

	ingest_ms := s.ts_ingest_ms
	if ingest_ms <= 0 do ingest_ms = ts
	unix := util.normalize_unix_seconds(s.window_end_ts if s.window_end_ts != 0 else ingest_ms)

	return Parsed_Stats{
		mark_price    = s.mark_price_close,
		funding       = f64_valid(s.funding_rate_last) ? s.funding_rate_last : 0,
		tbuy          = f64_valid(s.liq_buy_volume) ? s.liq_buy_volume : 0,
		tsell         = f64_valid(s.liq_sell_volume) ? s.liq_sell_volume : 0,
		window_ms     = s.window_ms,
		ts_ingest_ms  = ingest_ms,
		quality_flags = s.quality_flags,
		unix          = unix,
		subject_id    = subject_id,
	}, true
}

parse_microstructure_evidence_legacy_compat :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Evidence, bool) {
	legacy: util.MR_Microstructure_Evidence_Frame
	if json.unmarshal(raw, &legacy) != nil do return {}, false
	p := legacy.payload

	out: Parsed_Evidence
	out.subject_id = subject_id
	out.confidence = p.confidence
	out.unix = p.ts_ingest if p.ts_ingest > 0 else ts
	out.seq = p.seq

	nk := min(len(p.kind), len(out.kind))
	for i in 0 ..< nk {
		out.kind[i] = p.kind[i]
	}
	out.kind_len = u8(nk)
	if out.kind_len == 0 do return {}, false

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

parse_signal_legacy_compat :: proc(raw: []u8, ts: i64, subject_id: u64) -> (Parsed_Signal, bool) {
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
