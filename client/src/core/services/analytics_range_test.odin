package services

// S81: Tests for analytics range fetch + store population.

import "core:testing"

@(test)
test_parse_cvd_range_empty :: proc(t: ^testing.T) {
	store: Analytics_Store
	n := parse_analytics_cvd_range(&store, {})
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_cvd_range_single :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"DeltaVolume":-2.5,"CVD":150.0,"WindowStartTs":1700000060000,"WindowEndTs":1700000120000,"Seq":56,"TsIngestMs":1700000120000}]`)
	n := parse_analytics_cvd_range(&store, raw)
	testing.expect_value(t, n, 1)
	testing.expect_value(t, store.count, 1)
	entry, ok := get_analytics_latest(&store, .CVD)
	testing.expect(t, ok)
	testing.expect_value(t, entry.kind, Analytics_Kind.CVD)
	testing.expect(t, entry.values[0] == -2.5)
	testing.expect(t, entry.values[1] == 150.0)
	testing.expect_value(t, entry.window_start_ms, i64(1700000060000))
	testing.expect_value(t, entry.window_end_ms, i64(1700000120000))
	testing.expect_value(t, entry.seq, i64(56))
}

@(test)
test_parse_cvd_range_multiple :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"DeltaVolume":1.0,"CVD":10.0,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200},{"DeltaVolume":2.0,"CVD":12.0,"WindowStartTs":200,"WindowEndTs":300,"Seq":2,"TsIngestMs":300}]`)
	n := parse_analytics_cvd_range(&store, raw)
	testing.expect_value(t, n, 2)
	testing.expect_value(t, store.count, 2)
	// Latest should be the second entry (seq=2).
	entry, ok := get_analytics_latest(&store, .CVD)
	testing.expect(t, ok)
	testing.expect(t, entry.values[1] == 12.0)
	testing.expect_value(t, entry.seq, i64(2))
}

@(test)
test_parse_delta_volume_range :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"BuyVolume":12.0,"SellVolume":9.0,"DeltaVolume":3.0,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}]`)
	n := parse_analytics_delta_volume_range(&store, raw)
	testing.expect_value(t, n, 1)
	entry, ok := get_analytics_latest(&store, .Delta_Volume)
	testing.expect(t, ok)
	testing.expect(t, entry.values[0] == 12.0)
	testing.expect(t, entry.values[1] == 9.0)
	testing.expect(t, entry.values[2] == 3.0)
}

@(test)
test_parse_bar_stats_range :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"TradeCount":30,"BuyCount":18,"SellCount":12,"TotalVolume":22.0,"BuyVolume":13.0,"SellVolume":9.0,"VwapPrice":50001.2,"Imbalance":0.18,"IsBurst":true,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}]`)
	n := parse_analytics_bar_stats_range(&store, raw)
	testing.expect_value(t, n, 1)
	entry, ok := get_analytics_latest(&store, .Bar_Stats)
	testing.expect(t, ok)
	testing.expect(t, entry.values[0] == 30.0)  // trade_count
	testing.expect(t, entry.values[6] == 50001.2) // vwap
	testing.expect(t, entry.values[7] == 0.18)  // imbalance
	testing.expect(t, (entry.flags & 1) != 0)   // is_burst
}

@(test)
test_parse_bar_stats_range_no_burst :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"TradeCount":10,"BuyCount":5,"SellCount":5,"TotalVolume":5.0,"BuyVolume":2.5,"SellVolume":2.5,"VwapPrice":100.0,"Imbalance":0.0,"IsBurst":false,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}]`)
	n := parse_analytics_bar_stats_range(&store, raw)
	testing.expect_value(t, n, 1)
	entry, ok := get_analytics_latest(&store, .Bar_Stats)
	testing.expect(t, ok)
	testing.expect(t, (entry.flags & 1) == 0)  // not burst
}

@(test)
test_parse_cvd_range_budget_limit :: proc(t: ^testing.T) {
	store: Analytics_Store
	// Build a response with 100 entries — should be capped at ANALYTICS_RANGE_BUDGET (64).
	buf: [16384]u8
	off := 0
	buf[off] = '['; off += 1
	for i := 0; i < 100; i += 1 {
		if i > 0 { buf[off] = ','; off += 1 }
		src := `{"DeltaVolume":1.0,"CVD":1.0,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}`
		for c in src { buf[off] = u8(c); off += 1 }
	}
	buf[off] = ']'; off += 1
	n := parse_analytics_cvd_range(&store, buf[:off])
	testing.expect_value(t, n, ANALYTICS_RANGE_BUDGET)
}

@(test)
test_parse_analytics_nil_store :: proc(t: ^testing.T) {
	raw := transmute([]u8)string(`[{"DeltaVolume":1.0,"CVD":1.0,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}]`)
	n := parse_analytics_cvd_range(nil, raw)
	testing.expect_value(t, n, 0)
}

// --- S82: OI range parser tests ---

@(test)
test_parse_oi_range_empty :: proc(t: ^testing.T) {
	store: Analytics_Store
	count := parse_analytics_oi_range(&store, transmute([]u8)string("[]"))
	testing.expect_value(t, count, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_oi_range_single :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"open_interest":50000.5,"delta":100.0,"delta_pct":0.2,"cadence_hint_ms":5000,"confidence":"high","window_start_ts":1000,"window_end_ts":1000,"seq":1,"ts_ingest_ms":1001}]`)
	count := parse_analytics_oi_range(&store, raw)
	testing.expect_value(t, count, 1)
	testing.expect_value(t, store.count, 1)
	entry := get_analytics(&store, 0)
	testing.expect_value(t, entry.kind, Analytics_Kind.Open_Interest)
	testing.expect(t, entry.values[0] > 49999.0, "open_interest should be ~50000.5")
	testing.expect(t, entry.values[1] > 99.0, "delta should be ~100.0")
	testing.expect(t, entry.values[2] > 0.19, "delta_pct should be ~0.2")
	testing.expect_value(t, entry.cadence_hint_ms, i64(5000))
	testing.expect_value(t, entry.confidence, u8(1)) // high
}

@(test)
test_parse_oi_range_confidence_levels :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"open_interest":100,"delta":0,"delta_pct":0,"cadence_hint_ms":2000,"confidence":"high","window_start_ts":1000,"window_end_ts":1000,"seq":1,"ts_ingest_ms":1001},{"open_interest":200,"delta":100,"delta_pct":100,"cadence_hint_ms":15000,"confidence":"medium","window_start_ts":2000,"window_end_ts":2000,"seq":2,"ts_ingest_ms":2001},{"open_interest":300,"delta":100,"delta_pct":50,"cadence_hint_ms":45000,"confidence":"low","window_start_ts":3000,"window_end_ts":3000,"seq":3,"ts_ingest_ms":3001}]`)
	count := parse_analytics_oi_range(&store, raw)
	testing.expect_value(t, count, 3)
	// get_analytics(&store, 0) = most recent = last pushed = low
	e0 := get_analytics(&store, 0)
	testing.expect_value(t, e0.confidence, u8(3)) // low
	e1 := get_analytics(&store, 1)
	testing.expect_value(t, e1.confidence, u8(2)) // medium
	e2 := get_analytics(&store, 2)
	testing.expect_value(t, e2.confidence, u8(1)) // high
}

@(test)
test_parse_oi_range_budget_limit :: proc(t: ^testing.T) {
	store: Analytics_Store
	// Build a response with 70 entries — should be capped at ANALYTICS_RANGE_BUDGET (64).
	buf: [16384]u8
	off := 0
	buf[off] = '['; off += 1
	for i := 0; i < 70; i += 1 {
		if i > 0 { buf[off] = ','; off += 1 }
		src := `{"open_interest":100.0,"delta":1.0,"delta_pct":1.0,"cadence_hint_ms":5000,"confidence":"high","window_start_ts":100,"window_end_ts":200,"seq":1,"ts_ingest_ms":200}`
		for c in src { buf[off] = u8(c); off += 1 }
	}
	buf[off] = ']'; off += 1
	n := parse_analytics_oi_range(&store, buf[:off])
	testing.expect_value(t, n, ANALYTICS_RANGE_BUDGET)
}

@(test)
test_parse_oi_range_unknown_confidence :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"open_interest":100,"delta":0,"delta_pct":0,"cadence_hint_ms":0,"confidence":"unknown","window_start_ts":1000,"window_end_ts":1000,"seq":1,"ts_ingest_ms":1001}]`)
	count := parse_analytics_oi_range(&store, raw)
	testing.expect_value(t, count, 1)
	entry := get_analytics(&store, 0)
	testing.expect_value(t, entry.confidence, u8(0)) // unknown maps to 0
}

// --- S83: Robustness tests ---

@(test)
test_parse_cvd_range_invalid_json :: proc(t: ^testing.T) {
	store: Analytics_Store
	// Garbage bytes — no valid JSON objects.
	n := parse_analytics_cvd_range(&store, transmute([]u8)string("not json at all"))
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_delta_volume_range_empty_array :: proc(t: ^testing.T) {
	store: Analytics_Store
	n := parse_analytics_delta_volume_range(&store, transmute([]u8)string("[]"))
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_bar_stats_range_truncated :: proc(t: ^testing.T) {
	store: Analytics_Store
	// Truncated mid-object — no closing brace.
	raw := transmute([]u8)string(`[{"TradeCount":30,"BuyCount":18`)
	n := parse_analytics_bar_stats_range(&store, raw)
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_cvd_range_missing_fields :: proc(t: ^testing.T) {
	store: Analytics_Store
	// Object present but missing all expected keys — values default to 0.
	raw := transmute([]u8)string(`[{"foo":"bar"}]`)
	n := parse_analytics_cvd_range(&store, raw)
	testing.expect_value(t, n, 1)
	entry, ok := get_analytics_latest(&store, .CVD)
	testing.expect(t, ok)
	testing.expect(t, entry.values[0] == 0.0)
	testing.expect(t, entry.values[1] == 0.0)
	testing.expect_value(t, entry.seq, i64(0))
}

@(test)
test_parse_oi_range_truncated_mid_array :: proc(t: ^testing.T) {
	store: Analytics_Store
	// First object is complete, second is truncated.
	raw := transmute([]u8)string(`[{"open_interest":100,"delta":1,"delta_pct":1,"cadence_hint_ms":5000,"confidence":"high","window_start_ts":100,"window_end_ts":200,"seq":1,"ts_ingest_ms":200},{"open_interest":200,"delta":2`)
	count := parse_analytics_oi_range(&store, raw)
	testing.expect_value(t, count, 1) // only first entry parsed
}

@(test)
test_parse_delta_volume_range_nil_store :: proc(t: ^testing.T) {
	raw := transmute([]u8)string(`[{"BuyVolume":1,"SellVolume":1,"DeltaVolume":0,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}]`)
	n := parse_analytics_delta_volume_range(nil, raw)
	testing.expect_value(t, n, 0)
}

@(test)
test_parse_bar_stats_range_nil_store :: proc(t: ^testing.T) {
	raw := transmute([]u8)string(`[{"TradeCount":10,"BuyCount":5,"SellCount":5,"TotalVolume":5.0,"BuyVolume":2.5,"SellVolume":2.5,"VwapPrice":100.0,"Imbalance":0.0,"IsBurst":false,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}]`)
	n := parse_analytics_bar_stats_range(nil, raw)
	testing.expect_value(t, n, 0)
}

@(test)
test_parse_oi_range_nil_store :: proc(t: ^testing.T) {
	raw := transmute([]u8)string(`[{"open_interest":100,"delta":0,"delta_pct":0,"cadence_hint_ms":0,"confidence":"high","window_start_ts":1000,"window_end_ts":1000,"seq":1,"ts_ingest_ms":1001}]`)
	n := parse_analytics_oi_range(nil, raw)
	testing.expect_value(t, n, 0)
}

@(test)
test_parse_delta_volume_range_budget_limit :: proc(t: ^testing.T) {
	store: Analytics_Store
	buf: [16384]u8
	off := 0
	buf[off] = '['; off += 1
	for i := 0; i < 80; i += 1 {
		if i > 0 { buf[off] = ','; off += 1 }
		src := `{"BuyVolume":1.0,"SellVolume":1.0,"DeltaVolume":0.0,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}`
		for c in src { buf[off] = u8(c); off += 1 }
	}
	buf[off] = ']'; off += 1
	n := parse_analytics_delta_volume_range(&store, buf[:off])
	testing.expect_value(t, n, ANALYTICS_RANGE_BUDGET)
}

@(test)
test_parse_bar_stats_range_budget_limit :: proc(t: ^testing.T) {
	store: Analytics_Store
	buf: [32768]u8
	off := 0
	buf[off] = '['; off += 1
	for i := 0; i < 80; i += 1 {
		if i > 0 { buf[off] = ','; off += 1 }
		src := `{"TradeCount":1,"BuyCount":1,"SellCount":0,"TotalVolume":1.0,"BuyVolume":1.0,"SellVolume":0.0,"VwapPrice":100.0,"Imbalance":0.0,"IsBurst":false,"WindowStartTs":100,"WindowEndTs":200,"Seq":1,"TsIngestMs":200}`
		for c in src { buf[off] = u8(c); off += 1 }
	}
	buf[off] = ']'; off += 1
	n := parse_analytics_bar_stats_range(&store, buf[:off])
	testing.expect_value(t, n, ANALYTICS_RANGE_BUDGET)
}

// --- S93: Missing edge-case tests for DV/BS/OI parity with CVD ---

@(test)
test_parse_delta_volume_range_invalid_json :: proc(t: ^testing.T) {
	store: Analytics_Store
	n := parse_analytics_delta_volume_range(&store, transmute([]u8)string("not json at all"))
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_delta_volume_range_missing_fields :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"foo":"bar"}]`)
	n := parse_analytics_delta_volume_range(&store, raw)
	testing.expect_value(t, n, 1)
	entry, ok := get_analytics_latest(&store, .Delta_Volume)
	testing.expect(t, ok)
	testing.expect(t, entry.values[0] == 0.0)
	testing.expect(t, entry.values[1] == 0.0)
	testing.expect(t, entry.values[2] == 0.0)
	testing.expect_value(t, entry.seq, i64(0))
}

@(test)
test_parse_delta_volume_range_truncated :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"BuyVolume":12.0,"SellVolume":9.0`)
	n := parse_analytics_delta_volume_range(&store, raw)
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_bar_stats_range_invalid_json :: proc(t: ^testing.T) {
	store: Analytics_Store
	n := parse_analytics_bar_stats_range(&store, transmute([]u8)string("garbage bytes"))
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_bar_stats_range_missing_fields :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"unrelated":999}]`)
	n := parse_analytics_bar_stats_range(&store, raw)
	testing.expect_value(t, n, 1)
	entry, ok := get_analytics_latest(&store, .Bar_Stats)
	testing.expect(t, ok)
	testing.expect(t, entry.values[0] == 0.0) // trade_count
	testing.expect(t, entry.values[6] == 0.0) // vwap
	testing.expect(t, entry.values[7] == 0.0) // imbalance
	testing.expect(t, (entry.flags & 1) == 0) // no burst
	testing.expect_value(t, entry.seq, i64(0))
}

@(test)
test_parse_oi_range_invalid_json :: proc(t: ^testing.T) {
	store: Analytics_Store
	n := parse_analytics_oi_range(&store, transmute([]u8)string("not valid json"))
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

@(test)
test_parse_oi_range_missing_fields :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"irrelevant":true}]`)
	n := parse_analytics_oi_range(&store, raw)
	testing.expect_value(t, n, 1)
	entry, ok := get_analytics_latest(&store, .Open_Interest)
	testing.expect(t, ok)
	testing.expect(t, entry.values[0] == 0.0) // open_interest
	testing.expect(t, entry.values[1] == 0.0) // delta
	testing.expect(t, entry.values[2] == 0.0) // delta_pct
	testing.expect_value(t, entry.cadence_hint_ms, i64(0))
	testing.expect_value(t, entry.confidence, u8(0))
	testing.expect_value(t, entry.seq, i64(0))
}

@(test)
test_parse_cvd_range_truncated :: proc(t: ^testing.T) {
	store: Analytics_Store
	raw := transmute([]u8)string(`[{"DeltaVolume":1.5,"CVD":100.0`)
	n := parse_analytics_cvd_range(&store, raw)
	testing.expect_value(t, n, 0)
	testing.expect_value(t, store.count, 0)
}

