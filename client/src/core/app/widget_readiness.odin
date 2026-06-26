package app

import "mr:md_common"

// ═══════════════════════════════════════════════════════════════════════════
// S136: Unified Data Readiness Policy — single source of truth for
// per-widget readiness, replacing scattered inline switch logic.
//
// Design principles:
//   1. Policy-first: compile-time table drives all readiness decisions
//   2. Store-driven: if the widget's store has data, it's renderable
//   3. Composition badges handle transitional state communication
//   4. High-TF optimization: backfilled charts are usable without live
// ═══════════════════════════════════════════════════════════════════════════

// Data_Readiness — unified readiness level for a widget's primary data.
// Ordered from least-ready to most-ready. Pure data availability concept.
// S154: Removed Stale/Desync/Offline_Unreliable — reliability is now checked
// separately in resolve_pane_visual_state, not conflated with data readiness.
Data_Readiness :: enum u8 {
	Not_Ready,          // No stream bound, no data source
	Loading,            // Stream connected, awaiting first data
	Snapshot_Pending,   // Artifact events flowing but store not yet populated
	Seeding,            // Data arriving from stream, store building up
	Partial_Usable,     // Store has data, can render partial view (e.g., backfilled, live-only)
	Live_Usable,        // Fully composed: historical + live, steady state
}

// Widget_Readiness_Policy — compile-time contract per widget kind.
// Defines how a widget determines its readiness from data state.
//
// Fields:
//   primary_artifact        — which artifact kind this widget primarily depends on
//   partial_usable          — can the widget render useful content with partial data?
//   backfill_absent_usable  — is the widget usable with only live data (no history)?
//   uses_artifact_live_flag — use per-artifact live flag to detect Snapshot_Pending?
Widget_Readiness_Policy :: struct {
	primary_artifact:       md_common.Artifact_Kind,
	partial_usable:         bool,
	backfill_absent_usable: bool,
	uses_artifact_live_flag: bool,
}

// S136: Widget readiness policy table — canonical, compile-time.
//
// Summary by widget:
//   Candle       — composition-aware; backfilled = usable (chart shows history)
//   Stats        — live-immediate; partial=true (single update is useful)
//   Counter      — depends on candle store; partial=true (any candle is useful)
//   Heatmap      — accumulation; needs time to build grid
//   VPVR         — accumulation; needs time to build profile
//   Trades       — live-immediate; partial=true (single trade is useful)
//   Orderbook    — snapshot-gated; needs bid/ask data
//   DOM          — snapshot-gated; same as Orderbook
//   Analytics    — TF-gated; partial=true (partial CVD/DV is informative)
//   Session_VPVR — accumulation; needs session data to build
//   TPO          — accumulation; needs period data to build
//   Empty        — placeholder, no data expected
//
// Backfill-absent usability:
//   Stats, Trades, Orderbook, DOM — self-contained live data, no history needed
//   Counter, Heatmap, VPVR — useful with just live accumulation
//   Analytics, Session_VPVR, TPO — useful with live accumulation
//   Candle — backfilled charts are usable; live-only charts are usable
//   Empty — N/A
@(rodata)
widget_readiness_policies : [Widget_Kind]Widget_Readiness_Policy = {
	.Candle       = { primary_artifact = .Candle,                 partial_usable = false, backfill_absent_usable = true,  uses_artifact_live_flag = false },
	.Stats        = { primary_artifact = .Stats,                  partial_usable = true,  backfill_absent_usable = true,  uses_artifact_live_flag = true  },
	.Counter      = { primary_artifact = .Candle,                 partial_usable = true,  backfill_absent_usable = true,  uses_artifact_live_flag = false },
	.Heatmap      = { primary_artifact = .Heatmap,                partial_usable = false, backfill_absent_usable = true,  uses_artifact_live_flag = false },
	.VPVR         = { primary_artifact = .VPVR,                   partial_usable = false, backfill_absent_usable = true,  uses_artifact_live_flag = false },
	.Trades       = { primary_artifact = .Trade,                  partial_usable = true,  backfill_absent_usable = true,  uses_artifact_live_flag = true  },
	.Orderbook    = { primary_artifact = .Orderbook,              partial_usable = false, backfill_absent_usable = true,  uses_artifact_live_flag = true  },
	.DOM          = { primary_artifact = .Orderbook,              partial_usable = false, backfill_absent_usable = true,  uses_artifact_live_flag = true  },
	.Analytics    = { primary_artifact = .CVD,                    partial_usable = true,  backfill_absent_usable = true,  uses_artifact_live_flag = false },
	.Session_VPVR = { primary_artifact = .Session_Volume_Profile, partial_usable = false, backfill_absent_usable = true,  uses_artifact_live_flag = false },
	.TPO          = { primary_artifact = .TPO_Profile,            partial_usable = false, backfill_absent_usable = true,  uses_artifact_live_flag = false },
	.Empty        = { primary_artifact = .Trade,                  partial_usable = false, backfill_absent_usable = false, uses_artifact_live_flag = false },
	.Footprint    = { primary_artifact = .Trade,                  partial_usable = true,  backfill_absent_usable = true,  uses_artifact_live_flag = true  },
}

// widget_readiness_policy returns the readiness policy for a widget kind.
widget_readiness_policy :: proc(wk: Widget_Kind) -> Widget_Readiness_Policy {
	return widget_readiness_policies[wk]
}

// widget_data_readiness — unified, policy-driven readiness assessment for a widget.
// Pure function. All decisions derive from the compile-time policy table.
//
// Decision flow:
//   1. If store has usable data → Partial_Usable or Live_Usable
//   2. For candle-dependent widgets: composition stage indicates data availability
//      (Backfilled/Live_Only → Partial_Usable, Composed → Live_Usable)
//   3. If artifact live flag active → Snapshot_Pending (data flowing, store empty)
//   4. If stream has any live data → Seeding (connected, waiting for this artifact)
//   5. If stream bound or composition non-empty → Loading (connected, awaiting data)
//   6. Otherwise → Not_Ready (no stream)
//
// Key S136 behavioral improvements:
//   - Candle + Backfilled → Active (was Seeding). Chart shows historical data.
//   - Candle + Live_Only → Active (was No_History). Chart shows live data.
//   - Composition badges (BFILL/LIVE) communicate state without blocking render.
// S154: widget_data_readiness is now purely about data availability.
// Stream reliability (trust) is checked separately in resolve_pane_visual_state.
widget_data_readiness :: proc(
	wk: Widget_Kind,
	sv: Cell_Surface_View,
	stores: Cell_Stores,
) -> Data_Readiness {
	has_data := widget_store_has_data(wk, stores)

	// S136: Store has data → widget is usable.
	// Composition badges (PEND/BFILL/LIVE/COMP) communicate transitional state.
	// This eliminates unnecessary overlays on backfilled/live-only charts.
	if has_data {
		if sv.composition == .Composed && sv.has_live_data {
			return .Live_Usable
		}
		return .Partial_Usable
	}

	// S136: Candle-dependent widgets — composition stage indicates data availability.
	// Backfilled means GetRange returned history (store is implicitly populated).
	// Live_Only means live candles arrived (store has recent data).
	// Composed means both historical + live (fully coherent).
	// This avoids long Seeding/No_History overlays on high TFs where GetRange
	// returns quickly but the first live candle takes minutes.
	if wk == .Candle || wk == .Empty {
		#partial switch sv.composition {
		case .Composed:
			if sv.has_live_data do return .Live_Usable
			return .Partial_Usable
		case .Backfilled, .Live_Only:
			return .Partial_Usable
		case .Range_Pending:
			return .Loading
		}
	}

	// No store data — derive from artifact/stream liveness.
	policy := widget_readiness_policies[wk]

	// Per-artifact live flag → data flowing for this specific artifact, store not yet populated.
	if policy.uses_artifact_live_flag && sv.artifact_has_live[policy.primary_artifact] {
		return .Snapshot_Pending
	}

	// Any live data on the stream → connected, waiting for this specific artifact.
	if sv.has_live_data {
		return .Seeding
	}

	// Stream bound but no data yet → Loading (waiting for first events).
	if sv.stream_bound || sv.composition != .Empty {
		return .Loading
	}

	return .Not_Ready
}

// readiness_to_visual_state maps Data_Readiness to the display-facing Pane_Visual_State.
// Partial_Usable and Live_Usable both map to Active — the composition badge
// communicates transitional state without blocking the widget's render.
// S154: Reliability-based degradation is now handled by resolve_pane_visual_state.
readiness_to_visual_state :: proc(r: Data_Readiness) -> Pane_Visual_State {
	switch r {
	case .Not_Ready:          return .Empty
	case .Loading:            return .Loading
	case .Snapshot_Pending:   return .Snapshot_Pending
	case .Seeding:            return .Seeding
	case .Partial_Usable:     return .Active
	case .Live_Usable:        return .Active
	}
	return .Empty
}

// widget_store_has_data checks if the widget's backing store has renderable data.
// Each widget checks the store appropriate to its kind.
// Pure function — reads only from store pointers.
widget_store_has_data :: proc(wk: Widget_Kind, stores: Cell_Stores) -> bool {
	switch wk {
	case .Candle, .Counter:
		return stores.candle != nil && stores.candle.count > 0
	case .Stats:
		return stores.stats != nil && stores.stats.count > 0
	case .Trades:
		return stores.trades != nil && stores.trades.count > 0
	case .Orderbook:
		return stores.orderbook != nil && (stores.orderbook.bid_count > 0 || stores.orderbook.ask_count > 0)
	case .DOM:
		// S149: DOM is usable when orderbook has data OR dom fills have accumulated.
		ob_ok := stores.orderbook != nil && (stores.orderbook.bid_count > 0 || stores.orderbook.ask_count > 0)
		dom_ok := stores.dom != nil && stores.dom.trade_count > 0
		return ob_ok || dom_ok
	case .Footprint:
		return stores.footprint != nil && stores.footprint.count > 0
	case .Heatmap:
		return stores.heatmap != nil && stores.heatmap.count > 0
	case .VPVR:
		return stores.vpvr != nil && stores.vpvr.count > 0
	case .Analytics:
		return stores.analytics != nil && stores.analytics.count > 0
	case .Session_VPVR:
		return stores.session_vpvr != nil && stores.session_vpvr.count > 0
	case .TPO:
		return stores.tpo != nil && stores.tpo.period_count > 0
	case .Empty:
		return false
	}
	return false
}

// S146: TF-aware data expectation for a widget at a given timeframe.
// Delegates to the TF Data Contract via the widget's primary artifact.
// Pure function — no mutation, no allocation.
widget_tf_expectation :: proc(wk: Widget_Kind, tf_ms: i64) -> md_common.TF_Data_Expectation {
	policy := widget_readiness_policies[wk]
	return md_common.tf_data_expectation(policy.primary_artifact, tf_ms)
}

// S146: Backfill criticality for a widget at a given timeframe.
// Convenience wrapper — returns whether backfill is critical for operator utility.
widget_backfill_critical :: proc(wk: Widget_Kind, tf_ms: i64) -> bool {
	exp := widget_tf_expectation(wk, tf_ms)
	return exp.backfill_criticality == .Critical
}

// S152: widget_backfill_concern returns true if the backfill situation warrants
// operator attention (e.g., high-TF chart with no history). Pure function.
// UI uses this to decide whether to show a backfill warning badge.
widget_backfill_concern :: proc(sv: Cell_Surface_View) -> bool {
	be := sv.backfill_expectation
	// No concern if backfill is optional or already succeeded.
	if be.criticality == .Optional do return false
	if be.outcome == .Success do return false
	// Critical TF with no backfill or timeout → concern.
	if be.criticality == .Critical {
		switch be.outcome {
		case .Empty, .Timeout, .Not_Attempted:
			return true
		case .Partial:
			return true
		case .Success:
			return false
		}
	}
	// Recommended TF with timeout → mild concern.
	if be.outcome == .Timeout do return true
	return false
}

// S152: widget_backfill_hint returns a TF-aware hint string for the current
// backfill state. Extends tf_overlay_hint with outcome-specific messaging.
// All returned strings are compile-time literals.
widget_backfill_hint :: proc(sv: Cell_Surface_View) -> string {
	be := sv.backfill_expectation
	switch be.outcome {
	case .Success:
		return "History loaded"
	case .Partial:
		if be.criticality == .Critical {
			return "Partial history — Ctrl+R for more"
		}
		return "Partial history"
	case .Empty:
		if be.criticality == .Critical {
			return "No history available"
		}
		return "No history — using live data"
	case .Timeout:
		if be.criticality == .Critical {
			return "History fetch timed out — Ctrl+R to retry"
		}
		return "History fetch timed out"
	case .Not_Attempted:
		switch be.live_only_util {
		case .Full:
			return "Live data building chart"
		case .Degraded:
			return "Live only — backfill improves view"
		case .Minimal:
			return "Backfill needed — Ctrl+R to fetch history"
		}
	}
	return "Waiting for data"
}

// widget_store_for_readiness returns a human-readable store kind label.
// Used for diagnostics/telemetry only. All returned strings are compile-time literals.
widget_store_label :: proc(wk: Widget_Kind) -> string {
	switch wk {
	case .Candle, .Counter: return "candle"
	case .Stats:            return "stats"
	case .Trades:           return "trades"
	case .Orderbook:        return "orderbook"
	case .DOM:              return "dom"
	case .Heatmap:          return "heatmap"
	case .VPVR:             return "vpvr"
	case .Analytics:        return "analytics"
	case .Session_VPVR:     return "session_vpvr"
	case .TPO:              return "tpo"
	case .Footprint:        return "footprint"
	case .Empty:            return "none"
	}
	return "unknown"
}
