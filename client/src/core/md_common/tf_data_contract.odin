package md_common

// ═══════════════════════════════════════════════════════════════════════════
// S146: TF Data Contract — unified semantics for timeframe × data availability.
//
// Single source of truth for per-TF expectations across all data kinds.
// Answers: "given this timeframe and data kind, what should the operator expect?"
//
// Design principles:
//   1. TF class, not raw ms — decisions key on behavioral class, not exact duration
//   2. Backfill criticality scales with TF — low TFs tolerate live-only; high TFs need history
//   3. Overlay patience scales with TF — don't alarm the operator on normal high-TF waits
//   4. All policy in one file — no scattered TF-conditional logic across widgets
// ═══════════════════════════════════════════════════════════════════════════

// TF_Class — behavioral classification of timeframe durations.
// Maps the continuous ms range into discrete operational classes.
TF_Class :: enum u8 {
	Tick,          // ≤10s  (1s, 5s) — sub-10s candle close, live data arrives fast
	Minute,        // ≤60s  (1m) — standard operational timeframe
	Multi_Minute,  // ≤15m  (5m, 15m) — first live candle takes minutes
	Hourly,        // ≤4h   (30m, 1h, 4h) — first live candle takes 30min+
	Daily,         // >4h   (1d) — first live candle takes hours
}

// tf_class_from_ms classifies a raw timeframe duration (ms) into a TF_Class.
tf_class_from_ms :: proc(tf_ms: i64) -> TF_Class {
	if tf_ms <= 10_000     do return .Tick
	if tf_ms <= 60_000     do return .Minute
	if tf_ms <= 900_000    do return .Multi_Minute
	if tf_ms <= 14_400_000 do return .Hourly
	return .Daily
}

// Backfill_Criticality — how important is historical backfill for operator utility?
Backfill_Criticality :: enum u8 {
	Optional,     // Live-only is fully useful (chart builds in seconds)
	Recommended,  // Live-only works but slow build; backfill improves experience
	Critical,     // Without backfill, operator waits minutes+ for meaningful chart
}

// Live_Only_Utility — how useful is live-only data (no backfill) at this TF?
Live_Only_Utility :: enum u8 {
	Full,         // Fully useful — data arrives fast enough to be immediately useful
	Degraded,     // Usable but degraded — slow build, missing historical context
	Minimal,      // Barely useful — need to wait TF duration for first candle close
}

// TF_Data_Expectation — per-TF-class expectations for a data concept.
// Pure value type — no pointers, no allocation.
TF_Data_Expectation :: struct {
	backfill_criticality: Backfill_Criticality,
	live_only_utility:    Live_Only_Utility,
	first_useful_ms:      i64,  // Expected time to first useful render from this source
	overlay_patience_ms:  i64,  // How long Loading/Seeding overlay is normal before concern
	min_useful_count:     i32,  // Minimum entries for a meaningful view
}

// ═══════════════════════════════════════════════════════════════════════════
// Per-concept expectation tables — pure functions returning compile-time values.
// Each function maps TF_Class → TF_Data_Expectation for a category of data.
// ═══════════════════════════════════════════════════════════════════════════

// Candle expectations by TF class.
// Candles are the primary chart data — backfill criticality scales sharply with TF.
//
// Tick:         Live-only is fine — chart fills in seconds.
// Minute:       Backfill recommended — live-only builds over a minute, usable but slow.
// Multi_Minute: Backfill critical — 5-15 minutes for first live close is too long to wait.
// Hourly/Daily: Backfill essential — without history, the chart is empty for hours.
candle_tf_expectation :: proc(class: TF_Class) -> TF_Data_Expectation {
	switch class {
	case .Tick:
		return { .Optional,    .Full,     2_000,       10_000,       10 }
	case .Minute:
		return { .Recommended, .Degraded, 60_000,      120_000,      5  }
	case .Multi_Minute:
		return { .Critical,    .Minimal,  300_000,     600_000,      3  }
	case .Hourly:
		return { .Critical,    .Minimal,  1_800_000,   3_600_000,    2  }
	case .Daily:
		return { .Critical,    .Minimal,  86_400_000,  172_800_000,  1  }
	}
	return { .Recommended, .Degraded, 60_000, 120_000, 5 }
}

// Analytics TF-gated expectations (CVD, Delta Volume, Bar Stats).
// These are TF-sensitive with HTTP backfill available from the cold reader.
// On high TFs, HTTP backfill is the primary data source — live accumulation is too slow.
analytics_tf_gated_expectation :: proc(class: TF_Class) -> TF_Data_Expectation {
	switch class {
	case .Tick:
		return { .Optional,    .Full,     2_000,       10_000,      5 }
	case .Minute:
		return { .Recommended, .Degraded, 60_000,      120_000,     3 }
	case .Multi_Minute:
		return { .Critical,    .Minimal,  300_000,     600_000,     2 }
	case .Hourly:
		return { .Critical,    .Minimal,  1_800_000,   3_600_000,   1 }
	case .Daily:
		return { .Critical,    .Minimal,  86_400_000,  172_800_000, 1 }
	}
	return { .Recommended, .Degraded, 60_000, 120_000, 3 }
}

// TF-independent expectations (Stats, Trades, Orderbook, OI).
// These data kinds arrive on their own cadence regardless of chart timeframe.
// Always immediate, always partial-usable, no backfill needed.
tf_independent_expectation :: proc() -> TF_Data_Expectation {
	return { .Optional, .Full, 2_000, 10_000, 1 }
}

// Accumulation expectations (Heatmap, VPVR, Session_VPVR, TPO).
// TF-sensitive but no historical backfill — must accumulate from live data.
// Utility scales with time elapsed, not TF directly, but TF affects granularity.
accumulation_tf_expectation :: proc(class: TF_Class) -> TF_Data_Expectation {
	switch class {
	case .Tick:
		return { .Optional, .Full,     5_000,    15_000,     1 }
	case .Minute:
		return { .Optional, .Degraded, 30_000,   90_000,     1 }
	case .Multi_Minute:
		return { .Optional, .Minimal,  120_000,  300_000,    1 }
	case .Hourly:
		return { .Optional, .Minimal,  300_000,  900_000,    1 }
	case .Daily:
		return { .Optional, .Minimal,  600_000,  1_800_000,  1 }
	}
	return { .Optional, .Degraded, 30_000, 90_000, 1 }
}

// ═══════════════════════════════════════════════════════════════════════════
// Unified query — resolve expectation for any artifact at any TF.
// ═══════════════════════════════════════════════════════════════════════════

// tf_data_expectation returns the data availability expectation for a given
// artifact kind at a given timeframe (ms). Pure function.
tf_data_expectation :: proc(kind: Artifact_Kind, tf_ms: i64) -> TF_Data_Expectation {
	class := tf_class_from_ms(tf_ms)
	policy := artifact_policies[kind]

	// TF-independent artifacts: same expectation regardless of TF.
	if !policy.is_tf_sensitive {
		return tf_independent_expectation()
	}

	// TF-gated analytics: CVD, Delta_Volume, Bar_Stats.
	be := bootstrap_expectations[kind]
	if be.source == .Live_TF_Gated {
		return analytics_tf_gated_expectation(class)
	}

	// Historical range: Candles (+ Range_Candle).
	if be.source == .Historical_Range {
		return candle_tf_expectation(class)
	}

	// Accumulation: Heatmap, VPVR, Session_VPVR, TPO.
	if be.source == .Accumulation {
		return accumulation_tf_expectation(class)
	}

	// Fallback: treat as TF-independent.
	return tf_independent_expectation()
}

// ═══════════════════════════════════════════════════════════════════════════
// TF-aware overlay hints — richer messages for operator trust.
// ═══════════════════════════════════════════════════════════════════════════

// tf_overlay_hint returns a TF-class-aware hint string for overlay messages.
// The hint explains *why* the operator is waiting and sets correct expectations.
// All returned strings are compile-time literals.
tf_overlay_hint :: proc(kind: Artifact_Kind, tf_ms: i64, is_live_only: bool) -> string {
	class := tf_class_from_ms(tf_ms)
	policy := artifact_policies[kind]
	exp := tf_data_expectation(kind, tf_ms)

	// TF-independent: always fast.
	if !policy.is_tf_sensitive {
		return "Data arrives within seconds"
	}

	// Live-only state: differentiate by backfill criticality.
	if is_live_only {
		switch exp.backfill_criticality {
		case .Optional:
			return "Live data building chart"
		case .Recommended:
			return "Live only — backfill improves view"
		case .Critical:
			switch class {
			case .Multi_Minute:
				return "Live only — consider Ctrl+R for backfill"
			case .Hourly, .Daily:
				return "Backfill needed — Ctrl+R to fetch history"
			case .Tick, .Minute:
			}
			return "Live only — backfill recommended"
		}
		return "Live data arriving"
	}

	// Loading/Seeding: hint based on TF class.
	be := bootstrap_expectations[kind]
	switch be.source {
	case .Historical_Range:
		return "Fetching historical data"
	case .Live_TF_Gated:
		switch class {
		case .Tick:
			return "First close in seconds"
		case .Minute:
			return "Waiting for candle close (~1m)"
		case .Multi_Minute:
			return "Normal — first close takes minutes"
		case .Hourly:
			return "Long timeframe — first close takes 30m+"
		case .Daily:
			return "Daily TF — chart relies on backfill"
		}
	case .Accumulation:
		switch class {
		case .Tick:
			return "Accumulating data"
		case .Minute:
			return "Building over time"
		case .Multi_Minute, .Hourly, .Daily:
			return "Accumulating — takes time at this TF"
		}
	case .Live_Immediate:
		return "Data arrives within seconds"
	case .Snapshot_Gate:
		return "Awaiting exchange snapshot"
	}
	return "Waiting for data"
}

// tf_class_label returns a human-readable label for a TF class.
// All returned strings are compile-time literals.
tf_class_label :: proc(class: TF_Class) -> string {
	switch class {
	case .Tick:         return "tick"
	case .Minute:       return "minute"
	case .Multi_Minute: return "multi-min"
	case .Hourly:       return "hourly"
	case .Daily:        return "daily"
	}
	return "unknown"
}

// ═══════════════════════════════════════════════════════════════════════════
// S152: Backfill Policy — TF-adaptive retry budget and timeout for GetRange.
//
// Centralizes the scattered retry_count=1 / timeout=300 frames constants
// into a policy-driven table keyed on TF class. High TFs get:
//   - Longer timeouts (server needs more time to scan cold storage)
//   - More retries (cost of failure is higher — empty chart for minutes/hours)
//   - Live_Only fallback classification (what the operator should expect)
//
// Pure functions, no mutation, no allocation.
// ═══════════════════════════════════════════════════════════════════════════

// Backfill_Policy — per-TF-class retry and timeout policy for GetRange.
Backfill_Policy :: struct {
	timeout_frames:     u64,   // Frames before GetRange times out (@ 60fps)
	max_retries:        u8,    // Max auto-retries on timeout before giving up
	live_only_fallback: bool,  // Is Live_Only an acceptable fallback at this TF?
}

// backfill_policy_for_tf returns the GetRange retry/timeout policy for a TF class.
// Pure function — all values are compile-time constants.
//
// Design rationale:
//   Tick:         Short timeout, 1 retry — chart fills fast from live data anyway.
//   Minute:       Medium timeout, 1 retry — fallback to live-only is acceptable.
//   Multi_Minute: Longer timeout, 2 retries — live-only is painful (5-15min wait).
//   Hourly:       Long timeout, 2 retries — live-only means empty chart for 30min+.
//   Daily:        Long timeout, 2 retries — live-only is essentially unusable.
backfill_policy_for_tf :: proc(class: TF_Class) -> Backfill_Policy {
	switch class {
	case .Tick:
		return { timeout_frames = 300,  max_retries = 1, live_only_fallback = true  }
	case .Minute:
		return { timeout_frames = 480,  max_retries = 1, live_only_fallback = true  }
	case .Multi_Minute:
		return { timeout_frames = 600,  max_retries = 2, live_only_fallback = false }
	case .Hourly:
		return { timeout_frames = 900,  max_retries = 2, live_only_fallback = false }
	case .Daily:
		return { timeout_frames = 1200, max_retries = 2, live_only_fallback = false }
	}
	return { timeout_frames = 300, max_retries = 1, live_only_fallback = true }
}

// backfill_policy_for_tf_ms convenience — classifies tf_ms then returns policy.
backfill_policy_for_tf_ms :: proc(tf_ms: i64) -> Backfill_Policy {
	return backfill_policy_for_tf(tf_class_from_ms(tf_ms))
}

// Backfill_Outcome — classification of a GetRange result.
// Used to decide what the operator sees after a GetRange completes or fails.
Backfill_Outcome :: enum u8 {
	Success,        // Got candles, store is populated
	Partial,        // Got some candles but fewer than min_useful_count
	Empty,          // GetRange returned zero candles (no history on server)
	Timeout,        // GetRange timed out (retry budget exhausted)
	Not_Attempted,  // No GetRange was sent (live-only mode or not connected)
}

// classify_backfill_outcome determines the outcome of a GetRange cycle.
// Pure function — reads from apply state and store count.
classify_backfill_outcome :: proc(
	getrange_seeded: bool,
	getrange_pending: bool,
	store_count: int,
	retry_count: u8,
	max_retries: u8,
	min_useful: i32,
) -> Backfill_Outcome {
	if getrange_pending do return .Not_Attempted  // still in flight
	if !getrange_seeded {
		if retry_count > max_retries do return .Timeout
		return .Not_Attempted
	}
	if store_count <= 0 do return .Empty
	if store_count < int(min_useful) do return .Partial
	return .Success
}

// Backfill_Expectation — what the operator should expect for backfill at this TF.
// Derived from TF data contract + current backfill state. Pure value type.
// Passed through Cell_Surface_View for UI consumption.
Backfill_Expectation :: struct {
	criticality:     Backfill_Criticality,  // How important is backfill?
	live_only_util:  Live_Only_Utility,     // How useful is live-only?
	outcome:         Backfill_Outcome,      // Current backfill result classification
	patience_ms:     i64,                   // How long to wait before operator concern
	tf_class:        TF_Class,              // Behavioral TF class
}

// derive_backfill_expectation computes the backfill expectation for a candle
// artifact at a given TF. Combines TF data contract policy with current state.
// Pure function — no mutation.
derive_backfill_expectation :: proc(
	tf_ms: i64,
	getrange_seeded: bool,
	getrange_pending: bool,
	store_count: int,
	retry_count: u8,
) -> Backfill_Expectation {
	class := tf_class_from_ms(tf_ms)
	exp := candle_tf_expectation(class)
	policy := backfill_policy_for_tf(class)
	outcome := classify_backfill_outcome(
		getrange_seeded, getrange_pending, store_count,
		retry_count, policy.max_retries, exp.min_useful_count,
	)
	return Backfill_Expectation{
		criticality    = exp.backfill_criticality,
		live_only_util = exp.live_only_utility,
		outcome        = outcome,
		patience_ms    = exp.overlay_patience_ms,
		tf_class       = class,
	}
}

// backfill_outcome_label returns a display label for the backfill outcome.
backfill_outcome_label :: proc(o: Backfill_Outcome) -> string {
	switch o {
	case .Success:       return "OK"
	case .Partial:       return "PARTIAL"
	case .Empty:         return "EMPTY"
	case .Timeout:       return "TIMEOUT"
	case .Not_Attempted: return "PENDING"
	}
	return "UNKNOWN"
}
