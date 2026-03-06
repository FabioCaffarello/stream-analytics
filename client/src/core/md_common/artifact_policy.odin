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
	None,          // No staleness check (trades, evidence, signal, tape)
	TF_Adaptive,   // Candle health: 2x/3x TF thresholds
	Dual_Silence,  // Dual-event silence: stats+orderbook silent >12s
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
		if ak == .VPVR do return degrade_vpvr
		return false
	case .Low:
		return level >= 3
	}
	return false
}
