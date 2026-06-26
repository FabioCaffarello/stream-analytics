package md_common

import "mr:ports"

// Artifact_Kind enumerates every data artifact the client handles.
// Maps 1:1 to MD_Event_Kind but exists in md_common to avoid circular deps.
Artifact_Kind :: enum u8 {
	Trade,
	Orderbook,
	Stats,
	Candle,
	Heatmap,
	VPVR,
	Evidence,
	Signal,
	Tape,
	Range_Candle,
	// S47: Analytics substrate — first-class identity for analytics streams.
	Open_Interest,
	Delta_Volume,
	CVD,
	Bar_Stats,
	// S49: Session & Profile Engine.
	Session_Volume_Profile,
	TPO_Profile,
}

// Snapshot_Semantics defines how an artifact's state is replaced or accumulated.
Snapshot_Semantics :: enum u8 {
	None,          // No snapshot concept — append-only (trades, evidence, signal)
	Latest_Wins,   // Each message replaces entire state (orderbook, stats, vpvr)
	Ring_Append,   // Append to ring, in-place update for same window (candle)
	Window_Dedup,  // Replace within same time window, ring across windows (heatmap)
}

// BP_Priority defines backpressure drop priority.
BP_Priority :: enum u8 {
	Critical,    // Never drop (trades, orderbook, candles, stats, signal, tape)
	Degradable,  // Drop under assist mode (heatmap, vpvr)
	Low,         // Drop at L3+ backpressure (evidence)
}

// Stale_Detection defines the staleness detection strategy for an artifact.
Stale_Detection :: enum u8 {
	None,              // No staleness check (trades, evidence, signal, tape)
	TF_Adaptive,       // Candle health: 2x/3x TF thresholds
	Dual_Silence,      // Dual-event silence: stats+orderbook silent >12s
	Sparse_Adaptive,   // S47: Sparse/irregular feeds (OI): 60s/180s thresholds
}

// S130: Bootstrap source — how an artifact first becomes usable.
Bootstrap_Source :: enum u8 {
	Live_Immediate,    // Trades, OB, Stats — arrive on subscribe, no TF dependency
	Live_TF_Gated,     // Delta Vol, CVD, Bar Stats — first close = TF duration
	Historical_Range,  // Candles — GetRange backfill
	Snapshot_Gate,     // Needs explicit snapshot (OB depth)
	Accumulation,      // Heatmap, VPVR, SVP, TPO — needs time to build
}

// S130: Bootstrap expectation per artifact — TF-independent base contract.
Bootstrap_Expectation :: struct {
	source:         Bootstrap_Source,
	min_seed_ms:    i64,   // Minimum expected time to first usable data
	partial_usable: bool,  // Can render partial data before full bootstrap
}

// Artifact_Policy is the compile-time contract for each artifact kind.
// Defines what the stream engine must enforce per artifact.
Artifact_Policy :: struct {
	needs_snapshot_gate:          bool,  // Must see snapshot before accepting deltas
	accepts_range_seed:          bool,  // Can be seeded via GetRange historical backfill
	accepts_delta_without_snapshot: bool, // Process deltas without prior snapshot
	snapshot_semantics:          Snapshot_Semantics,
	reset_on_reconnect:          bool,  // Clear snapshot gate / apply state on reconnect
	reset_on_tf_change:          bool,  // Clear store on timeframe change
	is_tf_sensitive:             bool,  // Subject includes timeframe component
	has_synthetic_fallback:      bool,  // Can be synthesized from other artifact data
	backpressure_priority:       BP_Priority,
	stale_detection:             Stale_Detection,
}

// artifact_policies is the canonical policy table — single source of truth.
// Use artifact_policy(kind) to look up the policy for any artifact.
@(rodata)
artifact_policies : [Artifact_Kind]Artifact_Policy = {
	.Trade = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .None,
		reset_on_reconnect          = false,
		reset_on_tf_change          = false,
		is_tf_sensitive             = false,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Critical,
		stale_detection             = .None,
	},
	.Orderbook = {
		needs_snapshot_gate          = true,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = false,
		snapshot_semantics          = .Latest_Wins,
		reset_on_reconnect          = true,
		reset_on_tf_change          = false,
		is_tf_sensitive             = false,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Critical,
		stale_detection             = .Dual_Silence,
	},
	.Stats = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Latest_Wins,
		reset_on_reconnect          = false,
		reset_on_tf_change          = false,
		is_tf_sensitive             = false,
		has_synthetic_fallback      = true,   // synthetic from trades
		backpressure_priority       = .Critical,
		stale_detection             = .Dual_Silence,
	},
	.Candle = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = true,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Ring_Append,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = true,   // synthetic from trades
		backpressure_priority       = .Critical,
		stale_detection             = .TF_Adaptive,
	},
	.Heatmap = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Window_Dedup,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = true,   // synthetic from orderbook
		backpressure_priority       = .Degradable,
		stale_detection             = .None,
	},
	.VPVR = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Latest_Wins,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = true,   // synthetic from orderbook
		backpressure_priority       = .Degradable,
		stale_detection             = .None,
	},
	.Evidence = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .None,
		reset_on_reconnect          = false,
		reset_on_tf_change          = false,
		is_tf_sensitive             = false,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Low,
		stale_detection             = .None,
	},
	.Signal = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .None,
		reset_on_reconnect          = false,
		reset_on_tf_change          = false,
		is_tf_sensitive             = false,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Critical,
		stale_detection             = .None,
	},
	.Tape = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .None,
		reset_on_reconnect          = false,
		reset_on_tf_change          = false,
		is_tf_sensitive             = false,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Critical,
		stale_detection             = .None,
	},
	.Range_Candle = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,  // IS the range seed
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Ring_Append,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Critical,
		stale_detection             = .None,
	},
	// S47: Analytics substrate policies.
	.Open_Interest = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Latest_Wins,
		reset_on_reconnect          = false,
		reset_on_tf_change          = false,
		is_tf_sensitive             = false,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Degradable,
		stale_detection             = .Sparse_Adaptive,
	},
	.Delta_Volume = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Ring_Append,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Degradable,
		stale_detection             = .TF_Adaptive,
	},
	.CVD = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Ring_Append,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Degradable,
		stale_detection             = .TF_Adaptive,
	},
	.Bar_Stats = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Ring_Append,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Degradable,
		stale_detection             = .TF_Adaptive,
	},
	// S49: Session & Profile Engine policies.
	.Session_Volume_Profile = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Latest_Wins,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Degradable,
		stale_detection             = .None,
	},
	.TPO_Profile = {
		needs_snapshot_gate          = false,
		accepts_range_seed          = false,
		accepts_delta_without_snapshot = true,
		snapshot_semantics          = .Latest_Wins,
		reset_on_reconnect          = false,
		reset_on_tf_change          = true,
		is_tf_sensitive             = true,
		has_synthetic_fallback      = false,
		backpressure_priority       = .Degradable,
		stale_detection             = .None,
	},
}

// S130: Bootstrap expectation table — single source of truth for how each artifact bootstraps.
@(rodata)
bootstrap_expectations : [Artifact_Kind]Bootstrap_Expectation = {
	.Trade              = { source = .Live_Immediate,   min_seed_ms = 500,    partial_usable = true  },
	.Orderbook          = { source = .Snapshot_Gate,    min_seed_ms = 2_000,  partial_usable = false },
	.Stats              = { source = .Live_Immediate,   min_seed_ms = 1_000,  partial_usable = true  },
	.Candle             = { source = .Historical_Range, min_seed_ms = 1_000,  partial_usable = false },
	.Heatmap            = { source = .Accumulation,     min_seed_ms = 5_000,  partial_usable = false },
	.VPVR               = { source = .Accumulation,     min_seed_ms = 5_000,  partial_usable = false },
	.Evidence           = { source = .Live_Immediate,   min_seed_ms = 500,    partial_usable = true  },
	.Signal             = { source = .Live_Immediate,   min_seed_ms = 500,    partial_usable = true  },
	.Tape               = { source = .Live_Immediate,   min_seed_ms = 500,    partial_usable = true  },
	.Range_Candle       = { source = .Historical_Range, min_seed_ms = 1_000,  partial_usable = false },
	.Open_Interest      = { source = .Live_Immediate,   min_seed_ms = 2_000,  partial_usable = true  },
	.Delta_Volume       = { source = .Live_TF_Gated,    min_seed_ms = 1_000,  partial_usable = false },
	.CVD                = { source = .Live_TF_Gated,    min_seed_ms = 1_000,  partial_usable = false },
	.Bar_Stats          = { source = .Live_TF_Gated,    min_seed_ms = 1_000,  partial_usable = false },
	.Session_Volume_Profile = { source = .Accumulation, min_seed_ms = 5_000,  partial_usable = false },
	.TPO_Profile        = { source = .Accumulation,     min_seed_ms = 10_000, partial_usable = false },
}

// S130: Bootstrap hint — TF-aware output for UX display.
Bootstrap_Hint :: struct {
	expected_ms: i64,    // Expected time to first useful data for this TF
	partial_ok:  bool,   // Can render partial data while waiting
	hint_label:  string, // Human-readable hint (string literal, no alloc)
}

// S130: bootstrap_hint_for_artifact returns a TF-aware bootstrap hint.
// Pure function. All returned strings are compile-time literals.
bootstrap_hint_for_artifact :: proc(kind: Artifact_Kind, tf_ms: i64) -> Bootstrap_Hint {
	be := bootstrap_expectations[kind]
	hint: Bootstrap_Hint
	hint.partial_ok = be.partial_usable

	switch be.source {
	case .Live_Immediate:
		hint.expected_ms = be.min_seed_ms
		hint.hint_label = "Data arrives within seconds"
	case .Live_TF_Gated:
		hint.expected_ms = max(be.min_seed_ms, tf_ms)
		if tf_ms <= 5_000 {
			hint.hint_label = "First close in seconds"
		} else if tf_ms <= 60_000 {
			hint.hint_label = "Waiting for candle close"
		} else if tf_ms <= 900_000 {
			hint.hint_label = "First close takes minutes"
		} else {
			hint.hint_label = "Long timeframe — first close may take a while"
		}
	case .Historical_Range:
		hint.expected_ms = be.min_seed_ms + 1_000
		hint.hint_label = "Fetching historical data"
	case .Snapshot_Gate:
		hint.expected_ms = be.min_seed_ms
		hint.hint_label = "Awaiting exchange snapshot"
	case .Accumulation:
		hint.expected_ms = be.min_seed_ms + tf_ms
		if tf_ms <= 5_000 {
			hint.hint_label = "Accumulating data"
		} else {
			hint.hint_label = "Building over time"
		}
	}
	return hint
}

// S130: artifact_bootstrap_expectation returns the bootstrap expectation for a given artifact.
artifact_bootstrap_expectation :: proc(kind: Artifact_Kind) -> Bootstrap_Expectation {
	return bootstrap_expectations[kind]
}

// artifact_kind_from_event_kind maps the port event kind to artifact kind.
artifact_kind_from_event_kind :: proc(kind: ports.MD_Event_Kind) -> Artifact_Kind {
	switch kind {
	case .Trade:              return .Trade
	case .Orderbook_Snapshot: return .Orderbook
	case .Stats:              return .Stats
	case .Candle:             return .Candle
	case .Heatmap:            return .Heatmap
	case .VPVR:               return .VPVR
	case .Evidence:           return .Evidence
	case .Signal:             return .Signal
	case .Tape:               return .Tape
	case .Range_Candle_Batch: return .Range_Candle
	// S47: Analytics event kinds → artifact kinds.
	case .Open_Interest:      return .Open_Interest
	case .Delta_Volume:       return .Delta_Volume
	case .CVD:                return .CVD
	case .Bar_Stats:          return .Bar_Stats
	// S49: Session & Profile Engine.
	case .Session_Volume_Profile: return .Session_Volume_Profile
	case .TPO_Profile:            return .TPO_Profile
	}
	return .Trade
}

// artifact_policy returns the policy for a given artifact kind.
artifact_policy :: proc(kind: Artifact_Kind) -> Artifact_Policy {
	return artifact_policies[kind]
}

// artifact_policy_for_event returns the policy for a given event kind.
artifact_policy_for_event :: proc(kind: ports.MD_Event_Kind) -> Artifact_Policy {
	return artifact_policies[artifact_kind_from_event_kind(kind)]
}

// should_skip_by_bp_policy evaluates whether an event should be dropped under backpressure.
// Pure function — caller provides current state.
should_skip_by_bp_policy :: proc(
	kind: ports.MD_Event_Kind,
	bp_enabled: bool,
	degrade_heatmap: bool,
	degrade_vpvr: bool,
	level: int,
) -> bool {
	policy := artifact_policy_for_event(kind)
	switch policy.backpressure_priority {
	case .Critical:
		return false
	case .Degradable:
		if !bp_enabled do return false
		ak := artifact_kind_from_event_kind(kind)
		if ak == .Heatmap do return degrade_heatmap
		if ak == .VPVR || ak == .Session_Volume_Profile || ak == .TPO_Profile do return degrade_vpvr
		return false
	case .Low:
		return level >= 3
	}
	return false
}
