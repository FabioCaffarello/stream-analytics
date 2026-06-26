package md_common

import "core:testing"

// ═══════════════════════════════════════════════════════════════════════════
// S146: TF Data Contract Tests
// ═══════════════════════════════════════════════════════════════════════════

// --- TF Class Classification ---

@(test)
test_tf_class_1s :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(1_000), TF_Class.Tick)
}

@(test)
test_tf_class_5s :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(5_000), TF_Class.Tick)
}

@(test)
test_tf_class_10s_boundary :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(10_000), TF_Class.Tick)
}

@(test)
test_tf_class_1m :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(60_000), TF_Class.Minute)
}

@(test)
test_tf_class_5m :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(300_000), TF_Class.Multi_Minute)
}

@(test)
test_tf_class_15m :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(900_000), TF_Class.Multi_Minute)
}

@(test)
test_tf_class_30m :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(1_800_000), TF_Class.Hourly)
}

@(test)
test_tf_class_1h :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(3_600_000), TF_Class.Hourly)
}

@(test)
test_tf_class_4h :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(14_400_000), TF_Class.Hourly)
}

@(test)
test_tf_class_1d :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(86_400_000), TF_Class.Daily)
}

// --- Backfill Criticality Scaling ---

@(test)
test_candle_backfill_optional_on_tick :: proc(t: ^testing.T) {
	exp := candle_tf_expectation(.Tick)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Optional)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Full)
}

@(test)
test_candle_backfill_recommended_on_minute :: proc(t: ^testing.T) {
	exp := candle_tf_expectation(.Minute)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Recommended)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Degraded)
}

@(test)
test_candle_backfill_critical_on_multi_minute :: proc(t: ^testing.T) {
	exp := candle_tf_expectation(.Multi_Minute)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Critical)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Minimal)
}

@(test)
test_candle_backfill_critical_on_hourly :: proc(t: ^testing.T) {
	exp := candle_tf_expectation(.Hourly)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Critical)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Minimal)
}

@(test)
test_candle_backfill_critical_on_daily :: proc(t: ^testing.T) {
	exp := candle_tf_expectation(.Daily)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Critical)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Minimal)
}

// --- Overlay Patience Scales with TF ---

@(test)
test_overlay_patience_grows_with_tf :: proc(t: ^testing.T) {
	tick := candle_tf_expectation(.Tick)
	minute := candle_tf_expectation(.Minute)
	multi := candle_tf_expectation(.Multi_Minute)
	hourly := candle_tf_expectation(.Hourly)
	daily := candle_tf_expectation(.Daily)

	testing.expect(t, tick.overlay_patience_ms < minute.overlay_patience_ms,
		"tick patience < minute patience")
	testing.expect(t, minute.overlay_patience_ms < multi.overlay_patience_ms,
		"minute patience < multi-minute patience")
	testing.expect(t, multi.overlay_patience_ms < hourly.overlay_patience_ms,
		"multi-minute patience < hourly patience")
	testing.expect(t, hourly.overlay_patience_ms < daily.overlay_patience_ms,
		"hourly patience < daily patience")
}

// --- Min Useful Count Decreases with TF ---

@(test)
test_min_useful_count_decreases_with_tf :: proc(t: ^testing.T) {
	tick := candle_tf_expectation(.Tick)
	minute := candle_tf_expectation(.Minute)
	multi := candle_tf_expectation(.Multi_Minute)

	testing.expect(t, tick.min_useful_count > minute.min_useful_count,
		"tick needs more candles than minute")
	testing.expect(t, minute.min_useful_count > multi.min_useful_count,
		"minute needs more candles than multi-minute")
}

// --- TF-Independent Data Kinds ---

@(test)
test_stats_always_tf_independent :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Stats, 1_000)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Optional)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Full)

	exp_15m := tf_data_expectation(.Stats, 900_000)
	testing.expect_value(t, exp_15m.backfill_criticality, Backfill_Criticality.Optional)
	testing.expect_value(t, exp_15m.live_only_utility, Live_Only_Utility.Full)
}

@(test)
test_trades_always_tf_independent :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Trade, 300_000)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Optional)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Full)
}

@(test)
test_orderbook_always_tf_independent :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Orderbook, 86_400_000)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Optional)
	testing.expect_value(t, exp.live_only_utility, Live_Only_Utility.Full)
}

@(test)
test_open_interest_always_tf_independent :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Open_Interest, 300_000)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Optional)
}

// --- Analytics TF-Gated Scaling ---

@(test)
test_cvd_optional_on_tick :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.CVD, 1_000)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Optional)
}

@(test)
test_cvd_critical_on_15m :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.CVD, 900_000)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Critical)
}

@(test)
test_delta_vol_critical_on_5m :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Delta_Volume, 300_000)
	testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Critical)
}

// --- Accumulation Expectations ---

@(test)
test_heatmap_optional_backfill_all_tfs :: proc(t: ^testing.T) {
	// Accumulation artifacts have no backfill mechanism — always optional.
	for tf_ms in ([5]i64{1_000, 60_000, 300_000, 3_600_000, 86_400_000}) {
		exp := tf_data_expectation(.Heatmap, tf_ms)
		testing.expect_value(t, exp.backfill_criticality, Backfill_Criticality.Optional)
	}
}

@(test)
test_vpvr_accumulation_patience_grows :: proc(t: ^testing.T) {
	tick := tf_data_expectation(.VPVR, 1_000)
	multi := tf_data_expectation(.VPVR, 300_000)
	testing.expect(t, tick.overlay_patience_ms < multi.overlay_patience_ms,
		"vpvr tick patience < multi-minute patience")
}

// --- Unified Query Dispatch ---

@(test)
test_unified_candle_routes_to_candle_table :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Candle, 5_000)
	ref := candle_tf_expectation(.Tick)
	testing.expect_value(t, exp.backfill_criticality, ref.backfill_criticality)
	testing.expect_value(t, exp.overlay_patience_ms, ref.overlay_patience_ms)
}

@(test)
test_unified_bar_stats_routes_to_analytics :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Bar_Stats, 300_000)
	ref := analytics_tf_gated_expectation(.Multi_Minute)
	testing.expect_value(t, exp.backfill_criticality, ref.backfill_criticality)
	testing.expect_value(t, exp.live_only_utility, ref.live_only_utility)
}

@(test)
test_unified_session_vpvr_routes_to_accumulation :: proc(t: ^testing.T) {
	exp := tf_data_expectation(.Session_Volume_Profile, 60_000)
	ref := accumulation_tf_expectation(.Minute)
	testing.expect_value(t, exp.first_useful_ms, ref.first_useful_ms)
}

// --- Overlay Hints ---

@(test)
test_overlay_hint_candle_loading :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.Candle, 1_000, false)
	testing.expect_value(t, hint, "Fetching historical data")
}

@(test)
test_overlay_hint_candle_live_only_tick :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.Candle, 1_000, true)
	testing.expect_value(t, hint, "Live data building chart")
}

@(test)
test_overlay_hint_candle_live_only_15m :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.Candle, 900_000, true)
	testing.expect_value(t, hint, "Live only — consider Ctrl+R for backfill")
}

@(test)
test_overlay_hint_candle_live_only_1h :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.Candle, 3_600_000, true)
	testing.expect_value(t, hint, "Backfill needed — Ctrl+R to fetch history")
}

@(test)
test_overlay_hint_stats_always_fast :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.Stats, 900_000, false)
	testing.expect_value(t, hint, "Data arrives within seconds")
}

@(test)
test_overlay_hint_cvd_tick_loading :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.CVD, 5_000, false)
	testing.expect_value(t, hint, "First close in seconds")
}

@(test)
test_overlay_hint_cvd_15m_loading :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.CVD, 900_000, false)
	testing.expect_value(t, hint, "Normal — first close takes minutes")
}

@(test)
test_overlay_hint_heatmap_tick :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.Heatmap, 1_000, false)
	testing.expect_value(t, hint, "Accumulating data")
}

@(test)
test_overlay_hint_heatmap_high_tf :: proc(t: ^testing.T) {
	hint := tf_overlay_hint(.Heatmap, 300_000, false)
	testing.expect_value(t, hint, "Accumulating — takes time at this TF")
}

// --- TF Class Labels ---

@(test)
test_tf_class_labels :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_label(.Tick), "tick")
	testing.expect_value(t, tf_class_label(.Minute), "minute")
	testing.expect_value(t, tf_class_label(.Multi_Minute), "multi-min")
	testing.expect_value(t, tf_class_label(.Hourly), "hourly")
	testing.expect_value(t, tf_class_label(.Daily), "daily")
}

// --- Edge Cases ---

@(test)
test_tf_class_zero_ms :: proc(t: ^testing.T) {
	// 0ms should classify as Tick (smallest class).
	testing.expect_value(t, tf_class_from_ms(0), TF_Class.Tick)
}

@(test)
test_tf_class_negative_ms :: proc(t: ^testing.T) {
	// Negative ms should classify as Tick (defensive).
	testing.expect_value(t, tf_class_from_ms(-1), TF_Class.Tick)
}

@(test)
test_boundary_11s_is_minute :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(11_000), TF_Class.Minute)
}

@(test)
test_boundary_61s_is_multi_minute :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(61_000), TF_Class.Multi_Minute)
}

@(test)
test_boundary_901s_is_hourly :: proc(t: ^testing.T) {
	testing.expect_value(t, tf_class_from_ms(901_000), TF_Class.Hourly)
}

// ═══════════════════════════════════════════════════════════════════════════
// S152: Backfill Policy Tests
// ═══════════════════════════════════════════════════════════════════════════

// --- Backfill Policy per TF Class ---

@(test)
test_backfill_policy_tick_short_timeout :: proc(t: ^testing.T) {
	p := backfill_policy_for_tf(.Tick)
	testing.expect_value(t, p.timeout_frames, u64(300))
	testing.expect_value(t, p.max_retries, u8(1))
	testing.expect(t, p.live_only_fallback, "tick live_only_fallback should be true")
}

@(test)
test_backfill_policy_minute :: proc(t: ^testing.T) {
	p := backfill_policy_for_tf(.Minute)
	testing.expect_value(t, p.timeout_frames, u64(480))
	testing.expect_value(t, p.max_retries, u8(1))
	testing.expect(t, p.live_only_fallback, "minute live_only_fallback should be true")
}

@(test)
test_backfill_policy_multi_minute_longer :: proc(t: ^testing.T) {
	p := backfill_policy_for_tf(.Multi_Minute)
	testing.expect_value(t, p.timeout_frames, u64(600))
	testing.expect_value(t, p.max_retries, u8(2))
	testing.expect(t, !p.live_only_fallback, "multi-minute live_only_fallback should be false")
}

@(test)
test_backfill_policy_hourly_long_timeout :: proc(t: ^testing.T) {
	p := backfill_policy_for_tf(.Hourly)
	testing.expect_value(t, p.timeout_frames, u64(900))
	testing.expect_value(t, p.max_retries, u8(2))
	testing.expect(t, !p.live_only_fallback, "hourly live_only_fallback should be false")
}

@(test)
test_backfill_policy_daily_longest :: proc(t: ^testing.T) {
	p := backfill_policy_for_tf(.Daily)
	testing.expect_value(t, p.timeout_frames, u64(1200))
	testing.expect_value(t, p.max_retries, u8(2))
	testing.expect(t, !p.live_only_fallback, "daily live_only_fallback should be false")
}

@(test)
test_backfill_policy_timeout_scales_with_tf :: proc(t: ^testing.T) {
	tick := backfill_policy_for_tf(.Tick)
	minute := backfill_policy_for_tf(.Minute)
	multi := backfill_policy_for_tf(.Multi_Minute)
	hourly := backfill_policy_for_tf(.Hourly)
	daily := backfill_policy_for_tf(.Daily)

	testing.expect(t, tick.timeout_frames < minute.timeout_frames,
		"tick timeout < minute timeout")
	testing.expect(t, minute.timeout_frames < multi.timeout_frames,
		"minute timeout < multi-minute timeout")
	testing.expect(t, multi.timeout_frames < hourly.timeout_frames,
		"multi-minute timeout < hourly timeout")
	testing.expect(t, hourly.timeout_frames < daily.timeout_frames,
		"hourly timeout < daily timeout")
}

@(test)
test_backfill_policy_for_tf_ms_delegates :: proc(t: ^testing.T) {
	// 5s → Tick class policy
	p := backfill_policy_for_tf_ms(5_000)
	testing.expect_value(t, p.timeout_frames, u64(300))

	// 300s (5m) → Multi_Minute class policy
	p2 := backfill_policy_for_tf_ms(300_000)
	testing.expect_value(t, p2.timeout_frames, u64(600))
}

// --- Backfill Outcome Classification ---

@(test)
test_backfill_outcome_success :: proc(t: ^testing.T) {
	o := classify_backfill_outcome(true, false, 10, 0, 1, 5)
	testing.expect_value(t, o, Backfill_Outcome.Success)
}

@(test)
test_backfill_outcome_partial :: proc(t: ^testing.T) {
	o := classify_backfill_outcome(true, false, 2, 0, 1, 5)
	testing.expect_value(t, o, Backfill_Outcome.Partial)
}

@(test)
test_backfill_outcome_empty :: proc(t: ^testing.T) {
	o := classify_backfill_outcome(true, false, 0, 0, 1, 5)
	testing.expect_value(t, o, Backfill_Outcome.Empty)
}

@(test)
test_backfill_outcome_timeout :: proc(t: ^testing.T) {
	// Not seeded, retry count exceeds max → timeout.
	o := classify_backfill_outcome(false, false, 0, 2, 1, 5)
	testing.expect_value(t, o, Backfill_Outcome.Timeout)
}

@(test)
test_backfill_outcome_not_attempted :: proc(t: ^testing.T) {
	o := classify_backfill_outcome(false, false, 0, 0, 1, 5)
	testing.expect_value(t, o, Backfill_Outcome.Not_Attempted)
}

@(test)
test_backfill_outcome_pending_is_not_attempted :: proc(t: ^testing.T) {
	// GetRange in flight → not yet classified (still in progress).
	o := classify_backfill_outcome(false, true, 0, 0, 1, 5)
	testing.expect_value(t, o, Backfill_Outcome.Not_Attempted)
}

@(test)
test_backfill_outcome_seeded_zero_count :: proc(t: ^testing.T) {
	// Seeded but zero candles → empty (server has no history).
	o := classify_backfill_outcome(true, false, 0, 0, 2, 3)
	testing.expect_value(t, o, Backfill_Outcome.Empty)
}

// --- Backfill Expectation Derivation ---

@(test)
test_derive_backfill_expectation_tick_success :: proc(t: ^testing.T) {
	be := derive_backfill_expectation(5_000, true, false, 10, 0)
	testing.expect_value(t, be.tf_class, TF_Class.Tick)
	testing.expect_value(t, be.criticality, Backfill_Criticality.Optional)
	testing.expect_value(t, be.live_only_util, Live_Only_Utility.Full)
	testing.expect_value(t, be.outcome, Backfill_Outcome.Success)
}

@(test)
test_derive_backfill_expectation_15m_not_attempted :: proc(t: ^testing.T) {
	be := derive_backfill_expectation(900_000, false, false, 0, 0)
	testing.expect_value(t, be.tf_class, TF_Class.Multi_Minute)
	testing.expect_value(t, be.criticality, Backfill_Criticality.Critical)
	testing.expect_value(t, be.live_only_util, Live_Only_Utility.Minimal)
	testing.expect_value(t, be.outcome, Backfill_Outcome.Not_Attempted)
}

@(test)
test_derive_backfill_expectation_1h_timeout :: proc(t: ^testing.T) {
	// Hourly TF, retry count > max_retries (2) → Timeout.
	be := derive_backfill_expectation(3_600_000, false, false, 0, 3)
	testing.expect_value(t, be.tf_class, TF_Class.Hourly)
	testing.expect_value(t, be.criticality, Backfill_Criticality.Critical)
	testing.expect_value(t, be.outcome, Backfill_Outcome.Timeout)
}

@(test)
test_derive_backfill_expectation_patience_scales :: proc(t: ^testing.T) {
	be_tick := derive_backfill_expectation(1_000, false, false, 0, 0)
	be_hour := derive_backfill_expectation(3_600_000, false, false, 0, 0)
	testing.expect(t, be_tick.patience_ms < be_hour.patience_ms,
		"tick patience < hourly patience")
}

// --- Backfill Outcome Labels ---

@(test)
test_backfill_outcome_labels :: proc(t: ^testing.T) {
	testing.expect_value(t, backfill_outcome_label(.Success), "OK")
	testing.expect_value(t, backfill_outcome_label(.Partial), "PARTIAL")
	testing.expect_value(t, backfill_outcome_label(.Empty), "EMPTY")
	testing.expect_value(t, backfill_outcome_label(.Timeout), "TIMEOUT")
	testing.expect_value(t, backfill_outcome_label(.Not_Attempted), "PENDING")
}
