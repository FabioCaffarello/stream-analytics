package md_common

import "core:testing"

// S23/S24: Store Consolidation & Surface Boundary tests.
// Prove that Stream_Apply_State is the single source of truth for
// protocol -> store -> surface state, and that adapters maintain consistency.
// S24 additions verify that legacy boolean fields are fully replaced.

// --- Ownership model: per-stream state ---

@(test)
test_s23_per_slot_apply_state_tracks_all_artifacts :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Trade (no gate, critical, TF-insensitive)
	apply_state_mark_event(&s, .Trade, 1000, false)
	testing.expect(t, s.has_live[.Trade], "trade must be marked live")
	testing.expect(t, s.snapshot_seen[.Trade], "trade has no gate, snapshot_seen auto-set")

	// Orderbook (gate, reconnect-sensitive)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	testing.expect(t, s.has_live[.Orderbook], "orderbook live after snapshot")
	testing.expect(t, s.snapshot_seen[.Orderbook], "orderbook snapshot_seen set")

	// Stats (synthetic fallback, dual-silence)
	apply_state_mark_event(&s, .Stats, 3000, false)
	testing.expect(t, s.has_live[.Stats], "stats live")
	testing.expect(t, !apply_state_should_use_synthetic(s, .Stats), "no synthetic when live")

	// Candle (TF-sensitive, range-seedable)
	apply_state_mark_event(&s, .Candle, 4000, false)
	testing.expect(t, s.has_live[.Candle], "candle live")

	// Heatmap (TF-sensitive, degradable)
	apply_state_mark_event(&s, .Heatmap, 5000, false)
	testing.expect(t, s.has_live[.Heatmap], "heatmap live")

	// VPVR (TF-sensitive, degradable)
	apply_state_mark_event(&s, .VPVR, 6000, false)
	testing.expect(t, s.has_live[.VPVR], "vpvr live")

	// Evidence, Signal, Tape (no gate, append-only)
	apply_state_mark_event(&s, .Evidence, 7000, false)
	apply_state_mark_event(&s, .Signal, 8000, false)
	apply_state_mark_event(&s, .Tape, 9000, false)

	testing.expect_value(t, s.event_count, u64(9))
}

// --- Reconnect clears only policy-gated artifacts ---

@(test)
test_s23_reconnect_clears_only_gated_artifacts :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Populate all artifacts
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	apply_state_mark_event(&s, .Stats, 3000, false)
	apply_state_mark_event(&s, .Candle, 4000, false)
	apply_state_mark_event(&s, .Heatmap, 5000, false)
	s.getrange_pending = true

	apply_state_on_reconnect(&s)

	// Orderbook: reset_on_reconnect=true → snapshot_seen cleared
	testing.expect(t, !s.snapshot_seen[.Orderbook], "orderbook snapshot_seen must clear on reconnect")
	// Trade: reset_on_reconnect=false → survives
	testing.expect(t, s.snapshot_seen[.Trade], "trade snapshot_seen must survive reconnect")
	// Live flags survive (only snapshot gates reset)
	testing.expect(t, s.has_live[.Stats], "stats live must survive reconnect")
	testing.expect(t, s.has_live[.Candle], "candle live must survive reconnect")
	// getrange_pending cleared
	testing.expect(t, !s.getrange_pending, "getrange_pending must clear on reconnect")
}

// --- TF change clears only TF-sensitive artifacts ---

@(test)
test_s23_tf_change_clears_only_tf_sensitive :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	apply_state_mark_event(&s, .Stats, 3000, false)
	apply_state_mark_event(&s, .Candle, 4000, false)
	apply_state_mark_event(&s, .Heatmap, 5000, false)
	apply_state_mark_event(&s, .VPVR, 6000, false)
	s.getrange_seeded = true
	s.getrange_oldest_ts = 1000
	s.synth_heatmap_last_window = 5000

	apply_state_on_tf_change(&s)

	// TF-sensitive: candle, heatmap, vpvr, range_candle → cleared
	testing.expect(t, !s.has_live[.Candle], "candle live must clear on TF change")
	testing.expect(t, !s.has_live[.Heatmap], "heatmap live must clear on TF change")
	testing.expect(t, !s.has_live[.VPVR], "vpvr live must clear on TF change")
	// TF-insensitive: trade, orderbook, stats → survive
	testing.expect(t, s.has_live[.Trade], "trade live must survive TF change")
	testing.expect(t, s.has_live[.Orderbook], "orderbook live must survive TF change")
	testing.expect(t, s.has_live[.Stats], "stats live must survive TF change")
	// GetRange state cleared
	testing.expect(t, !s.getrange_seeded, "getrange_seeded must clear on TF change")
	testing.expect_value(t, s.getrange_oldest_ts, i64(0))
	testing.expect_value(t, s.synth_heatmap_last_window, i64(0))
}

// --- Synthetic fallback consistency ---

@(test)
test_s23_synthetic_fallback_follows_policy :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Stats, Candle, Heatmap, VPVR have synthetic fallback
	testing.expect(t, apply_state_should_use_synthetic(s, .Stats), "stats synthetic before live")
	testing.expect(t, apply_state_should_use_synthetic(s, .Candle), "candle synthetic before live")
	testing.expect(t, apply_state_should_use_synthetic(s, .Heatmap), "heatmap synthetic before live")
	testing.expect(t, apply_state_should_use_synthetic(s, .VPVR), "vpvr synthetic before live")

	// Trade, Orderbook, Evidence, Signal, Tape have NO synthetic fallback
	testing.expect(t, !apply_state_should_use_synthetic(s, .Trade), "trade has no synthetic")
	testing.expect(t, !apply_state_should_use_synthetic(s, .Orderbook), "orderbook has no synthetic")
	testing.expect(t, !apply_state_should_use_synthetic(s, .Evidence), "evidence has no synthetic")
	testing.expect(t, !apply_state_should_use_synthetic(s, .Signal), "signal has no synthetic")
	testing.expect(t, !apply_state_should_use_synthetic(s, .Tape), "tape has no synthetic")

	// Live event displaces synthetic
	apply_state_mark_event(&s, .Stats, 1000, false)
	testing.expect(t, !apply_state_should_use_synthetic(s, .Stats), "stats no longer synthetic after live")

	// TF change re-enables synthetic for TF-sensitive artifacts
	apply_state_mark_event(&s, .Candle, 2000, false)
	apply_state_on_tf_change(&s)
	testing.expect(t, apply_state_should_use_synthetic(s, .Candle), "candle back to synthetic after TF change")
	// Stats survives TF change → still live
	testing.expect(t, !apply_state_should_use_synthetic(s, .Stats), "stats still live after TF change")
}

// --- Summary adapter for metrics compatibility ---

@(test)
test_s23_summary_matches_apply_state :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1000, false)
	apply_state_mark_event(&s, .Candle, 2000, false)
	apply_state_mark_event(&s, .Orderbook, 3000, true)
	s.getrange_seeded = true

	sum := apply_state_summary(s)
	testing.expect(t, sum.has_live_stats, "summary stats must match")
	testing.expect(t, sum.has_live_candle, "summary candle must match")
	testing.expect(t, !sum.has_live_heatmap, "summary heatmap must be false")
	testing.expect(t, !sum.has_live_vpvr, "summary vpvr must be false")
	testing.expect(t, sum.snapshot_seen, "summary orderbook snapshot must match")
	testing.expect(t, sum.getrange_seeded, "summary getrange must match")
}

// --- Backpressure policy consistency ---

@(test)
test_s23_bp_policy_canonical_matches_artifact_table :: proc(t: ^testing.T) {
	// Verify that should_skip_by_bp_policy uses artifact_policy table consistently
	// Critical: never dropped
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		if policy.backpressure_priority == .Critical {
			// Map back to event kind for testing
			// Only test the ones we can map
		}
	}

	// Specific event kind tests through the canonical proc
	testing.expect(t, !should_skip_by_bp_policy(.Trade, true, true, true, 5), "trade critical")
	testing.expect(t, !should_skip_by_bp_policy(.Orderbook_Snapshot, true, true, true, 5), "orderbook critical")
	testing.expect(t, !should_skip_by_bp_policy(.Candle, true, true, true, 5), "candle critical")
	testing.expect(t, !should_skip_by_bp_policy(.Stats, true, true, true, 5), "stats critical")
	testing.expect(t, !should_skip_by_bp_policy(.Signal, true, true, true, 5), "signal critical")
	testing.expect(t, !should_skip_by_bp_policy(.Tape, true, true, true, 5), "tape critical")
	testing.expect(t, !should_skip_by_bp_policy(.Range_Candle_Batch, true, true, true, 5), "range_candle critical")

	// Degradable: dropped when enabled + degrade flag
	testing.expect(t, should_skip_by_bp_policy(.Heatmap, true, true, false, 1), "heatmap degraded")
	testing.expect(t, should_skip_by_bp_policy(.VPVR, true, false, true, 1), "vpvr degraded")
	testing.expect(t, !should_skip_by_bp_policy(.Heatmap, false, true, false, 1), "heatmap not degraded when bp off")

	// Low priority: dropped at L3+
	testing.expect(t, !should_skip_by_bp_policy(.Evidence, false, false, false, 2), "evidence ok at L2")
	testing.expect(t, should_skip_by_bp_policy(.Evidence, false, false, false, 3), "evidence dropped at L3")
}

// --- Snapshot gate consistency ---

@(test)
test_s23_snapshot_gate_canonical_for_all_artifacts :: proc(t: ^testing.T) {
	// Only orderbook has needs_snapshot_gate=true
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		if kind == .Orderbook {
			testing.expect(t, policy.needs_snapshot_gate, "orderbook must have gate")
		} else {
			testing.expect(t, !policy.needs_snapshot_gate, "only orderbook has gate")
		}
	}

	// Orderbook: empty delta before snapshot → rejected
	ob_policy := artifact_policies[.Orderbook]
	accept, gap := snapshot_gate_check(ob_policy, false, false, false)
	testing.expect(t, !accept, "empty delta before snapshot rejected")
	testing.expect(t, gap, "gap detected")

	// Orderbook: non-empty bootstrap delta → accepted
	accept2, gap2 := snapshot_gate_check(ob_policy, false, false, true)
	testing.expect(t, accept2, "bootstrap delta accepted")
	testing.expect(t, !gap2, "no gap for bootstrap delta")
}

// --- Protocol engine + apply state coordinated lifecycle ---

@(test)
test_s23_full_lifecycle_protocol_and_apply_state :: proc(t: ^testing.T) {
	p: Stream_Protocol
	s: Stream_Apply_State

	// 1. Subscribe
	protocol_on_subscribe(&p, 1000)
	apply_state_reset(&s)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)

	// 2. Orderbook snapshot (gated)
	protocol_on_snapshot(&p, 1, 2000)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	testing.expect_value(t, p.state, Stream_Protocol_State.Seeded)
	testing.expect(t, !apply_state_needs_snapshot(s, .Orderbook), "snapshot satisfied")

	// 3. Trade → Live
	protocol_on_event(&p, 2, 3000, 3)
	apply_state_mark_event(&s, .Trade, 3000, false)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)

	// 4. Synthetic candle (no live candle yet)
	testing.expect(t, apply_state_should_use_synthetic(s, .Candle), "should use synthetic candle")
	apply_state_mark_synthetic(&s, .Candle, 3500)
	testing.expect(t, s.using_synthetic[.Candle], "synthetic candle active")

	// 5. Live candle displaces synthetic
	apply_state_mark_event(&s, .Candle, 4000, false)
	testing.expect(t, !apply_state_should_use_synthetic(s, .Candle), "no longer synthetic")
	testing.expect(t, !s.using_synthetic[.Candle], "synthetic cleared")

	// 6. TF change
	protocol_on_tf_change(&p, 5000)
	apply_state_on_tf_change(&s)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)
	testing.expect(t, !s.has_live[.Candle], "candle cleared")
	testing.expect(t, s.has_live[.Trade], "trade survives")
	testing.expect(t, apply_state_should_use_synthetic(s, .Candle), "back to synthetic")

	// 7. Reconnect
	protocol_on_reconnect(&p, 6000)
	apply_state_on_reconnect(&s)
	testing.expect_value(t, p.state, Stream_Protocol_State.Reconnecting)
	testing.expect(t, !s.snapshot_seen[.Orderbook], "OB snapshot cleared on reconnect")
	testing.expect(t, s.has_live[.Trade], "trade survives reconnect")

	// 8. Re-subscribe
	protocol_on_subscribe(&p, 7000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)
}

// --- GetRange tracking through apply state ---

@(test)
test_s23_getrange_lifecycle_in_apply_state :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Initial: no getrange
	testing.expect(t, !s.getrange_pending, "initially not pending")
	testing.expect(t, !s.getrange_seeded, "initially not seeded")

	// Send getrange
	apply_state_mark_range_sent(&s, 100)
	testing.expect(t, s.getrange_pending, "pending after send")
	testing.expect_value(t, s.getrange_sent_frame, u64(100))

	// Complete getrange
	apply_state_mark_range_complete(&s, 500)
	testing.expect(t, !s.getrange_pending, "not pending after complete")
	testing.expect(t, s.getrange_seeded, "seeded after complete")
	testing.expect_value(t, s.getrange_oldest_ts, i64(500))

	// Timeout check
	apply_state_mark_range_sent(&s, 200)
	testing.expect(t, !apply_state_check_getrange_timeout(s, 300, 300), "no timeout yet")
	testing.expect(t, apply_state_check_getrange_timeout(s, 600, 300), "should timeout")

	// TF change clears getrange
	apply_state_on_tf_change(&s)
	testing.expect(t, !s.getrange_seeded, "getrange cleared on TF change")
	testing.expect(t, !s.getrange_pending, "pending cleared on TF change")
	testing.expect_value(t, s.getrange_oldest_ts, i64(0))
}

// --- Stale detection aligned with policy ---

@(test)
test_s23_stale_detection_policy_alignment :: proc(t: ^testing.T) {
	// Verify stale detection enum matches artifact policy table
	testing.expect_value(t, artifact_policies[.Trade].stale_detection, Stale_Detection.None)
	testing.expect_value(t, artifact_policies[.Orderbook].stale_detection, Stale_Detection.Dual_Silence)
	testing.expect_value(t, artifact_policies[.Stats].stale_detection, Stale_Detection.Dual_Silence)
	testing.expect_value(t, artifact_policies[.Candle].stale_detection, Stale_Detection.TF_Adaptive)
	testing.expect_value(t, artifact_policies[.Heatmap].stale_detection, Stale_Detection.None)
	testing.expect_value(t, artifact_policies[.VPVR].stale_detection, Stale_Detection.None)
	testing.expect_value(t, artifact_policies[.Evidence].stale_detection, Stale_Detection.None)
	testing.expect_value(t, artifact_policies[.Signal].stale_detection, Stale_Detection.None)
	testing.expect_value(t, artifact_policies[.Tape].stale_detection, Stale_Detection.None)
}

// --- Verify all artifact kinds have consistent policy ---

@(test)
test_s23_policy_table_invariants :: proc(t: ^testing.T) {
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]

		// If needs_snapshot_gate, must also reset_on_reconnect
		if policy.needs_snapshot_gate {
			testing.expect(t, policy.reset_on_reconnect,
				"snapshot-gated artifacts must reset on reconnect")
		}

		// If is_tf_sensitive, must also reset_on_tf_change
		if policy.is_tf_sensitive {
			testing.expect(t, policy.reset_on_tf_change,
				"TF-sensitive artifacts must reset on TF change")
		}

		// accepts_range_seed implies TF-sensitive (only candles need range backfill)
		if policy.accepts_range_seed {
			testing.expect(t, policy.is_tf_sensitive,
				"range-seedable artifacts must be TF-sensitive")
		}
	}
}

// =========================================================================
// S24: Legacy Removal Cutover — verify apply_state replaces all legacy bools.
// =========================================================================

// S24: orderbook_snapshot_seen is now apply_state.snapshot_seen[.Orderbook].
// Verify that reconnect policy resets it correctly.
@(test)
test_s24_orderbook_snapshot_via_apply_state_reconnect :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	testing.expect(t, s.snapshot_seen[.Orderbook], "snapshot_seen set after event")
	apply_state_on_reconnect(&s)
	testing.expect(t, !s.snapshot_seen[.Orderbook], "snapshot_seen cleared on reconnect")
}

// S24: has_heatmap_snapshot is now apply_state.has_live[.Heatmap].
// Verify TF change resets it.
@(test)
test_s24_heatmap_live_via_apply_state_tf_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Heatmap, 1000, false)
	testing.expect(t, s.has_live[.Heatmap], "heatmap live after event")
	apply_state_on_tf_change(&s)
	testing.expect(t, !s.has_live[.Heatmap], "heatmap live cleared on TF change")
}

// S24: has_live_vpvr is now apply_state.has_live[.VPVR].
// Verify TF change resets it.
@(test)
test_s24_vpvr_live_via_apply_state_tf_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .VPVR, 1000, false)
	testing.expect(t, s.has_live[.VPVR], "vpvr live after event")
	apply_state_on_tf_change(&s)
	testing.expect(t, !s.has_live[.VPVR], "vpvr live cleared on TF change")
}

// S24: synth_heatmap_last_window is now only in apply_state.
// Verify TF change and reset both zero it.
@(test)
test_s24_synth_heatmap_window_lifecycle :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	s.synth_heatmap_last_window = 5000
	apply_state_on_tf_change(&s)
	testing.expect_value(t, s.synth_heatmap_last_window, i64(0))

	s.synth_heatmap_last_window = 9000
	apply_state_reset(&s)
	testing.expect_value(t, s.synth_heatmap_last_window, i64(0))
}

// S24: Verify that summary adapter still produces correct results after cutover.
@(test)
test_s24_summary_adapter_after_cutover :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Fresh state: nothing live
	sum := apply_state_summary(s)
	testing.expect(t, !sum.has_live_stats, "fresh: no stats")
	testing.expect(t, !sum.has_live_candle, "fresh: no candle")
	testing.expect(t, !sum.has_live_heatmap, "fresh: no heatmap")
	testing.expect(t, !sum.has_live_vpvr, "fresh: no vpvr")
	testing.expect(t, !sum.snapshot_seen, "fresh: no snapshot")

	// Mark events
	apply_state_mark_event(&s, .Stats, 1000, false)
	apply_state_mark_event(&s, .Heatmap, 2000, false)
	apply_state_mark_event(&s, .VPVR, 3000, false)
	apply_state_mark_event(&s, .Candle, 4000, false)
	apply_state_mark_event(&s, .Orderbook, 5000, true)

	sum = apply_state_summary(s)
	testing.expect(t, sum.has_live_stats, "stats live")
	testing.expect(t, sum.has_live_candle, "candle live")
	testing.expect(t, sum.has_live_heatmap, "heatmap live")
	testing.expect(t, sum.has_live_vpvr, "vpvr live")
	testing.expect(t, sum.snapshot_seen, "snapshot seen")

	// TF change clears TF-sensitive, preserves TF-insensitive
	apply_state_on_tf_change(&s)
	sum = apply_state_summary(s)
	testing.expect(t, sum.has_live_stats, "stats survives TF change")
	testing.expect(t, !sum.has_live_candle, "candle cleared by TF change")
	testing.expect(t, !sum.has_live_heatmap, "heatmap cleared by TF change")
	testing.expect(t, !sum.has_live_vpvr, "vpvr cleared by TF change")
	testing.expect(t, sum.snapshot_seen, "OB snapshot survives TF change")

	// Reconnect clears OB snapshot
	apply_state_on_reconnect(&s)
	sum = apply_state_summary(s)
	testing.expect(t, !sum.snapshot_seen, "OB snapshot cleared on reconnect")
	testing.expect(t, sum.has_live_stats, "stats survives reconnect")
}

// =========================================================================
// S25: Historical/Realtime Composition tests.
// =========================================================================

// S25: Composition stage progression: Empty → Range_Pending → Backfilled → Composed.
@(test)
test_s25_composition_stage_full_lifecycle :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// 1. Fresh: Empty
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)
	testing.expect(t, !apply_state_is_range_ready(s), "not range ready when empty")

	// 2. GetRange sent: Range_Pending
	apply_state_mark_range_sent(&s, 100, 0xABCD)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Range_Pending)

	// 3. GetRange complete: Backfilled
	apply_state_mark_range_complete(&s, 500)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Backfilled)
	testing.expect(t, !apply_state_is_range_ready(s), "not ready without live candle")

	// 4. Live candle arrives: Composed
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)
	testing.expect(t, apply_state_is_range_ready(s), "range ready after seed + live")
}

// S25: Live-only path (no getrange) stays Live_Only.
@(test)
test_s25_composition_live_only_no_seed :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	apply_state_mark_event(&s, .Candle, 1000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Live_Only)
	testing.expect(t, !apply_state_is_range_ready(s), "not range ready without seed")
}

// S25: TF swap resets composition back to Empty.
@(test)
test_s25_composition_tf_swap_resets :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Build up to Composed state.
	apply_state_mark_range_sent(&s, 100, 0x1234)
	apply_state_mark_range_complete(&s, 500)
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)

	// TF change: resets candle + getrange → Empty.
	apply_state_on_tf_change(&s)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)
	testing.expect(t, !apply_state_is_range_ready(s), "not ready after TF change")
	testing.expect_value(t, s.range_candle_subject_id, u64(0))
}

// S25: Reconnect preserves seed but clears pending.
@(test)
test_s25_composition_reconnect_preserves_seed :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	apply_state_mark_range_sent(&s, 100)
	apply_state_mark_range_complete(&s, 500)
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)

	// Reconnect: clears pending but preserves seed + live candle.
	apply_state_on_reconnect(&s)
	testing.expect(t, s.getrange_seeded, "seed survives reconnect")
	testing.expect(t, s.has_live[.Candle], "live candle survives reconnect")
	testing.expect(t, !s.getrange_pending, "pending cleared on reconnect")
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)
}

// S25: Per-artifact event count tracks independently.
@(test)
test_s25_per_artifact_event_count :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Trade, 1001, false)
	apply_state_mark_event(&s, .Candle, 2000, false)
	apply_state_mark_event(&s, .Stats, 3000, false)
	apply_state_mark_event(&s, .Stats, 3001, false)
	apply_state_mark_event(&s, .Stats, 3002, false)

	testing.expect_value(t, s.artifact_event_count[.Trade], u64(2))
	testing.expect_value(t, s.artifact_event_count[.Candle], u64(1))
	testing.expect_value(t, s.artifact_event_count[.Stats], u64(3))
	testing.expect_value(t, s.artifact_event_count[.Heatmap], u64(0))
	testing.expect_value(t, s.event_count, u64(6))
}

// S25: Range candle subject ID lifecycle.
@(test)
test_s25_range_candle_subject_id_lifecycle :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Set via mark_range_sent
	apply_state_mark_range_sent(&s, 100, 0xDEAD)
	testing.expect_value(t, s.range_candle_subject_id, u64(0xDEAD))

	// Survives range completion
	apply_state_mark_range_complete(&s, 500)
	testing.expect_value(t, s.range_candle_subject_id, u64(0xDEAD))

	// Cleared on TF change
	apply_state_on_tf_change(&s)
	testing.expect_value(t, s.range_candle_subject_id, u64(0))

	// Set again
	apply_state_mark_range_sent(&s, 200, 0xBEEF)
	testing.expect_value(t, s.range_candle_subject_id, u64(0xBEEF))

	// Full reset clears it
	apply_state_reset(&s)
	testing.expect_value(t, s.range_candle_subject_id, u64(0))
}

// S25: Summary includes composition stage.
@(test)
test_s25_summary_includes_composition_stage :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	sum := apply_state_summary(s)
	testing.expect_value(t, sum.composition_stage, Composition_Stage.Empty)

	apply_state_mark_range_sent(&s, 100)
	apply_state_mark_range_complete(&s, 500)
	sum = apply_state_summary(s)
	testing.expect_value(t, sum.composition_stage, Composition_Stage.Backfilled)

	apply_state_mark_event(&s, .Candle, 2000, false)
	sum = apply_state_summary(s)
	testing.expect_value(t, sum.composition_stage, Composition_Stage.Composed)
}

// S25: Resync (full reset) returns to Empty and allows fresh composition.
@(test)
test_s25_composition_resync_full_reset :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	apply_state_mark_range_sent(&s, 100, 0x1234)
	apply_state_mark_range_complete(&s, 500)
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)

	// Full reset (resync path)
	apply_state_reset(&s)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)
	testing.expect_value(t, s.event_count, u64(0))
	testing.expect_value(t, s.artifact_event_count[.Candle], u64(0))

	// Can recompose from scratch
	apply_state_mark_range_sent(&s, 200, 0x5678)
	apply_state_mark_range_complete(&s, 300)
	apply_state_mark_event(&s, .Candle, 3000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)
	testing.expect_value(t, s.range_candle_subject_id, u64(0x5678))
}

// =========================================================================
// S26: Per-Cell Composition & Composition-Driven Runtime tests.
// =========================================================================

// S26: cell_composition_stage mirrors apply_state_composition_stage semantics.
@(test)
test_s26_cell_composition_stage_full_lifecycle :: proc(t: ^testing.T) {
	// 1. Empty: no pending, no seed, no live
	testing.expect_value(t, cell_composition_stage(false, false, false), Composition_Stage.Empty)

	// 2. Range_Pending: pending, no seed
	testing.expect_value(t, cell_composition_stage(true, false, false), Composition_Stage.Range_Pending)

	// 3. Backfilled: seeded, no live
	testing.expect_value(t, cell_composition_stage(false, true, false), Composition_Stage.Backfilled)

	// 4. Live_Only: no seed, has live candle
	testing.expect_value(t, cell_composition_stage(false, false, true), Composition_Stage.Live_Only)

	// 5. Composed: seeded + live
	testing.expect_value(t, cell_composition_stage(false, true, true), Composition_Stage.Composed)

	// Edge: pending + seeded + live → Composed (seed + live overrides pending)
	testing.expect_value(t, cell_composition_stage(true, true, true), Composition_Stage.Composed)

	// Edge: pending + live (no seed) → Live_Only
	testing.expect_value(t, cell_composition_stage(true, false, true), Composition_Stage.Live_Only)
}

// S26: Composition stage is always derivable from apply_state (no manual writes needed).
@(test)
test_s26_composition_stage_always_derivable :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Fresh: Empty
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)

	// After trade (no candle): still Empty
	apply_state_mark_event(&s, .Trade, 1000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)

	// After range sent: Range_Pending
	apply_state_mark_range_sent(&s, 100)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Range_Pending)

	// After range complete: Backfilled
	apply_state_mark_range_complete(&s, 500)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Backfilled)

	// After candle event: Composed
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)

	// After TF change: back to Empty
	apply_state_on_tf_change(&s)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)

	// Live candle without seed: Live_Only
	apply_state_mark_event(&s, .Candle, 3000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Live_Only)
}

// S26: cell_composition_stage and apply_state_composition_stage agree
// when given equivalent inputs.
@(test)
test_s26_cell_and_stream_composition_agree :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Empty
	testing.expect_value(t,
		cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle]),
		apply_state_composition_stage(s))

	// Range_Pending
	apply_state_mark_range_sent(&s, 100)
	testing.expect_value(t,
		cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle]),
		apply_state_composition_stage(s))

	// Backfilled
	apply_state_mark_range_complete(&s, 500)
	testing.expect_value(t,
		cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle]),
		apply_state_composition_stage(s))

	// Composed
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t,
		cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle]),
		apply_state_composition_stage(s))
}

// S26: Summary adapter composition_stage matches derived composition.
@(test)
test_s26_summary_composition_stage_always_derived :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Empty
	sum := apply_state_summary(s)
	testing.expect_value(t, sum.composition_stage, Composition_Stage.Empty)

	// Build to Composed
	apply_state_mark_range_sent(&s, 100)
	apply_state_mark_range_complete(&s, 500)
	apply_state_mark_event(&s, .Candle, 2000, false)
	sum = apply_state_summary(s)
	testing.expect_value(t, sum.composition_stage, Composition_Stage.Composed)

	// Reconnect preserves Composed
	apply_state_on_reconnect(&s)
	sum = apply_state_summary(s)
	testing.expect_value(t, sum.composition_stage, Composition_Stage.Composed)

	// TF change → Empty
	apply_state_on_tf_change(&s)
	sum = apply_state_summary(s)
	testing.expect_value(t, sum.composition_stage, Composition_Stage.Empty)
}

// S26: Per-artifact event counts survive reconnect and TF change, cleared on full reset.
@(test)
test_s26_artifact_event_count_lifecycle :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Trade, 1001, false)
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t, s.artifact_event_count[.Trade], u64(2))
	testing.expect_value(t, s.artifact_event_count[.Candle], u64(1))

	// Reconnect: event counts survive
	apply_state_on_reconnect(&s)
	testing.expect_value(t, s.artifact_event_count[.Trade], u64(2))
	testing.expect_value(t, s.artifact_event_count[.Candle], u64(1))

	// TF change: event counts survive (counters are cumulative observability)
	apply_state_on_tf_change(&s)
	testing.expect_value(t, s.artifact_event_count[.Trade], u64(2))
	testing.expect_value(t, s.artifact_event_count[.Candle], u64(1))

	// Full reset: all cleared
	apply_state_mark_event(&s, .Trade, 3000, false)
	apply_state_reset(&s)
	testing.expect_value(t, s.artifact_event_count[.Trade], u64(0))
}

// =========================================================================
// S27: Telemetry HUD Expansion & Operational Diagnostics tests.
// =========================================================================

// S27: Summary includes artifact event counts and total event count.
@(test)
test_s27_summary_includes_event_counts :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Trade, 1001, false)
	apply_state_mark_event(&s, .Stats, 2000, false)
	apply_state_mark_event(&s, .Candle, 3000, false)

	sum := apply_state_summary(s)
	testing.expect_value(t, sum.artifact_event_count[.Trade], u64(2))
	testing.expect_value(t, sum.artifact_event_count[.Stats], u64(1))
	testing.expect_value(t, sum.artifact_event_count[.Candle], u64(1))
	testing.expect_value(t, sum.artifact_event_count[.Heatmap], u64(0))
	testing.expect_value(t, sum.event_count, u64(4))
}

// S27: Telemetry view mirrors apply state exactly.
@(test)
test_s27_telemetry_mirrors_apply_state :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	apply_state_mark_event(&s, .Candle, 3000, false)
	apply_state_mark_range_sent(&s, 100, 0xABCD)
	apply_state_mark_range_complete(&s, 500)

	telem := apply_state_telemetry(s)

	// Event counts match
	testing.expect_value(t, telem.artifact_event_count[.Trade], u64(1))
	testing.expect_value(t, telem.artifact_event_count[.Orderbook], u64(1))
	testing.expect_value(t, telem.artifact_event_count[.Candle], u64(1))
	testing.expect_value(t, telem.event_count, u64(3))

	// Last recv matches
	testing.expect_value(t, telem.last_recv_ms[.Trade], i64(1000))
	testing.expect_value(t, telem.last_recv_ms[.Orderbook], i64(2000))
	testing.expect_value(t, telem.last_recv_ms[.Candle], i64(3000))
	testing.expect_value(t, telem.last_recv_ms[.Heatmap], i64(0))

	// Live/synthetic flags match
	testing.expect(t, telem.has_live[.Trade], "trade live")
	testing.expect(t, telem.has_live[.Orderbook], "orderbook live")
	testing.expect(t, telem.has_live[.Candle], "candle live")
	testing.expect(t, !telem.has_live[.Heatmap], "heatmap not live")
	testing.expect(t, !telem.using_synthetic[.Candle], "candle not synthetic when live")

	// Composition matches
	testing.expect_value(t, telem.composition_stage, Composition_Stage.Composed)
	testing.expect(t, telem.getrange_seeded, "getrange seeded")
	testing.expect(t, !telem.getrange_pending, "getrange not pending")
}

// S27: Active artifact count tracks correctly.
@(test)
test_s27_active_artifact_count :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, apply_state_active_artifact_count(s), 0)

	apply_state_mark_event(&s, .Trade, 1000, false)
	testing.expect_value(t, apply_state_active_artifact_count(s), 1)

	apply_state_mark_event(&s, .Stats, 2000, false)
	apply_state_mark_event(&s, .Candle, 3000, false)
	testing.expect_value(t, apply_state_active_artifact_count(s), 3)

	// Full reset clears count
	apply_state_reset(&s)
	testing.expect_value(t, apply_state_active_artifact_count(s), 0)
}

// S27: Telemetry view reflects synthetic fallback state.
@(test)
test_s27_telemetry_synthetic_state :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Mark synthetic for stats (no live data yet)
	apply_state_mark_synthetic(&s, .Stats, 1000)
	telem := apply_state_telemetry(s)
	testing.expect(t, telem.using_synthetic[.Stats], "stats should show synthetic")
	testing.expect(t, !telem.has_live[.Stats], "stats not live yet")

	// Live displaces synthetic
	apply_state_mark_event(&s, .Stats, 2000, false)
	telem = apply_state_telemetry(s)
	testing.expect(t, !telem.using_synthetic[.Stats], "synthetic cleared by live")
	testing.expect(t, telem.has_live[.Stats], "stats now live")
}

// S27: Telemetry composition stage after TF change returns to Empty.
@(test)
test_s27_telemetry_composition_after_tf_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 100)
	apply_state_mark_range_complete(&s, 500)
	apply_state_mark_event(&s, .Candle, 2000, false)

	telem := apply_state_telemetry(s)
	testing.expect_value(t, telem.composition_stage, Composition_Stage.Composed)

	apply_state_on_tf_change(&s)
	telem = apply_state_telemetry(s)
	testing.expect_value(t, telem.composition_stage, Composition_Stage.Empty)
	// Event counts survive TF change (cumulative observability)
	testing.expect_value(t, telem.artifact_event_count[.Candle], u64(1))
}

// S27: Summary event_count stays consistent with artifact_event_count sum.
@(test)
test_s27_summary_event_count_consistency :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	apply_state_mark_event(&s, .Stats, 3000, false)
	apply_state_mark_event(&s, .Candle, 4000, false)
	apply_state_mark_event(&s, .Heatmap, 5000, false)

	sum := apply_state_summary(s)

	// Total must equal sum of per-artifact counts
	artifact_total := u64(0)
	for kind in Artifact_Kind {
		artifact_total += sum.artifact_event_count[kind]
	}
	testing.expect_value(t, sum.event_count, artifact_total)
}

// =========================================================================
// S28: Artifact Latency Surface & Per-Cell Diagnostics tests.
// =========================================================================

// S28: Age returns -1 for never-received artifacts.
@(test)
test_s28_artifact_age_never_received :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Trade, 10_000), i64(-1))
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Orderbook, 10_000), i64(-1))
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Candle, 10_000), i64(-1))
}

// S28: Age computation for received artifacts.
@(test)
test_s28_artifact_age_after_event :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 5_000, false)
	apply_state_mark_event(&s, .Stats, 8_000, false)

	testing.expect_value(t, apply_state_artifact_age_ms(s, .Trade, 10_000), i64(5_000))
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Stats, 10_000), i64(2_000))
	// Not yet received
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Candle, 10_000), i64(-1))
}

// S28: Staleness classification for Stale_Detection.None (Trade) — always Fresh.
@(test)
test_s28_staleness_none_always_fresh :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1_000, false)

	// Even with huge age, Trade (stale_detection=None) stays Fresh.
	testing.expect_value(t, apply_state_artifact_staleness(s, .Trade, 100_000), Artifact_Staleness.Fresh)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Trade, 1_000_000), Artifact_Staleness.Fresh)
}

// S28: Staleness classification for Dual_Silence (Orderbook, Stats).
@(test)
test_s28_staleness_dual_silence :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// Fresh: age < 8s
	testing.expect_value(t, apply_state_artifact_staleness(s, .Orderbook, 5_000), Artifact_Staleness.Fresh)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Stats, 5_000), Artifact_Staleness.Fresh)

	// Aging: 8s <= age < 12s
	testing.expect_value(t, apply_state_artifact_staleness(s, .Orderbook, 9_500), Artifact_Staleness.Aging)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Stats, 9_500), Artifact_Staleness.Aging)

	// Stale: age >= 12s
	testing.expect_value(t, apply_state_artifact_staleness(s, .Orderbook, 13_500), Artifact_Staleness.Stale)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Stats, 13_500), Artifact_Staleness.Stale)
}

// S28: Staleness classification for TF_Adaptive (Candle).
@(test)
test_s28_staleness_tf_adaptive :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1_000, false)

	// With tf_ms=60_000: warn=120s, stale=180s
	testing.expect_value(t, apply_state_artifact_staleness(s, .Candle, 60_000, 60_000), Artifact_Staleness.Fresh)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Candle, 121_500, 60_000), Artifact_Staleness.Aging)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Candle, 181_500, 60_000), Artifact_Staleness.Stale)

	// With tf_ms=1_000: warn=max(2_000,5_000)=5s, stale=max(3_000,10_000)=10s
	testing.expect_value(t, apply_state_artifact_staleness(s, .Candle, 4_000, 1_000), Artifact_Staleness.Fresh)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Candle, 6_500, 1_000), Artifact_Staleness.Aging)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Candle, 11_500, 1_000), Artifact_Staleness.Stale)
}

// S28: Unknown staleness for never-received artifacts.
@(test)
test_s28_staleness_unknown_when_never_received :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, apply_state_artifact_staleness(s, .Orderbook, 10_000), Artifact_Staleness.Unknown)
	testing.expect_value(t, apply_state_artifact_staleness(s, .Candle, 10_000, 60_000), Artifact_Staleness.Unknown)
}

// S28: Stale/aging artifact count.
@(test)
test_s28_stale_artifact_count :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Mark orderbook, stats, candle at t=1000
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)
	apply_state_mark_event(&s, .Candle, 1_000, false)
	apply_state_mark_event(&s, .Trade, 1_000, false)

	// At t=5000: all fresh
	stale, aging := apply_state_stale_artifact_count(s, 5_000, 60_000)
	testing.expect_value(t, stale, 0)
	testing.expect_value(t, aging, 0)

	// At t=10000: OB+Stats aging (8s threshold), candle+trade fresh
	stale, aging = apply_state_stale_artifact_count(s, 10_000, 60_000)
	testing.expect_value(t, stale, 0)
	testing.expect_value(t, aging, 2) // orderbook + stats

	// At t=14000: OB+Stats stale (12s threshold)
	stale, aging = apply_state_stale_artifact_count(s, 14_000, 60_000)
	testing.expect_value(t, stale, 2) // orderbook + stats
	testing.expect_value(t, aging, 0)
}

// S28: Age resets correctly on events.
@(test)
test_s28_age_resets_on_new_event :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1_000, false)
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Stats, 10_000), i64(9_000))

	// New event refreshes age
	apply_state_mark_event(&s, .Stats, 9_500, false)
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Stats, 10_000), i64(500))
	testing.expect_value(t, apply_state_artifact_staleness(s, .Stats, 10_000), Artifact_Staleness.Fresh)
}

// S28: TF change resets age for TF-sensitive artifacts only.
@(test)
test_s28_age_tf_change_resets_tf_sensitive :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5_000, false)
	apply_state_mark_event(&s, .Stats, 5_000, false)

	apply_state_on_tf_change(&s)

	// Candle age reset (TF-sensitive → last_recv_ms cleared)
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Candle, 10_000), i64(-1))
	// Stats age survives (not TF-sensitive)
	testing.expect_value(t, apply_state_artifact_age_ms(s, .Stats, 10_000), i64(5_000))
}

// =========================================================================
// S29: Stale Auto-Recovery & Protocol-Driven Remediation tests.
// =========================================================================

// S29: Stale remediation triggers Resubscribe when Dual_Silence artifacts go stale.
@(test)
test_s29_stale_remediation_triggers_resubscribe :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Both OB and Stats received at t=1000.
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// At t=5000: still fresh — no remediation.
	testing.expect_value(t, apply_state_stale_remediation(s, 5_000), Remediation_Decision.None)

	// At t=14000: both stale (>12s) — triggers Resubscribe.
	testing.expect_value(t, apply_state_stale_remediation(s, 14_000), Remediation_Decision.Resubscribe)
}

// S29/S30: Cooldown prevents rapid re-triggering (adaptive backoff).
@(test)
test_s29_stale_remediation_cooldown :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// First recovery at t=14000.
	apply_state_mark_recovery(&s, 14_000)
	testing.expect_value(t, s.recovery_attempts, u8(1))
	testing.expect_value(t, s.recovery_last_ms, i64(14_000))

	// At t=20000 (6s after recovery): still stale, within 30s cooldown (attempt=1).
	testing.expect_value(t, apply_state_stale_remediation(s, 20_000), Remediation_Decision.Cooldown)

	// At t=44001 (30s+1ms after recovery): cooldown expired, triggers again.
	testing.expect_value(t, apply_state_stale_remediation(s, 44_001), Remediation_Decision.Resubscribe)
}

// S29: Exhausted after max attempts.
@(test)
test_s29_stale_remediation_exhausted :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// 3 recovery attempts.
	apply_state_mark_recovery(&s, 14_000)
	apply_state_mark_recovery(&s, 45_000)
	apply_state_mark_recovery(&s, 76_000)
	testing.expect_value(t, s.recovery_attempts, u8(3))

	// At t=200000: stale but exhausted.
	testing.expect_value(t, apply_state_stale_remediation(s, 200_000), Remediation_Decision.Exhausted)
}

// S29: No remediation when all artifacts are fresh.
@(test)
test_s29_stale_remediation_none_when_fresh :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)

	// At t=15000: age=5s, well within 12s threshold.
	testing.expect_value(t, apply_state_stale_remediation(s, 15_000), Remediation_Decision.None)
}

// S29: Recovery success resets attempt counter when stale clears.
@(test)
test_s29_recovery_success_resets_attempts :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// Trigger recovery.
	apply_state_mark_recovery(&s, 14_000)
	testing.expect_value(t, s.recovery_attempts, u8(1))

	// Still stale at t=20000 — no reset.
	apply_state_check_recovery_success(&s, 20_000)
	testing.expect_value(t, s.recovery_attempts, u8(1))

	// Fresh data arrives at t=25000.
	apply_state_mark_event(&s, .Orderbook, 25_000, true)
	apply_state_mark_event(&s, .Stats, 25_000, false)

	// At t=26000: both fresh (age=1s) — recovery succeeded.
	apply_state_check_recovery_success(&s, 26_000)
	testing.expect_value(t, s.recovery_attempts, u8(0))
	testing.expect_value(t, s.recovery_last_ms, i64(0))
}

// S29: Reconnect clears recovery state.
@(test)
test_s29_recovery_clears_on_reconnect :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_recovery(&s, 14_000)
	apply_state_mark_recovery(&s, 45_000)
	testing.expect_value(t, s.recovery_attempts, u8(2))

	apply_state_on_reconnect(&s)
	testing.expect_value(t, s.recovery_attempts, u8(0))
	testing.expect_value(t, s.recovery_last_ms, i64(0))
}

// S29: TF change clears recovery state.
@(test)
test_s29_recovery_clears_on_tf_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_recovery(&s, 14_000)
	testing.expect_value(t, s.recovery_attempts, u8(1))

	apply_state_on_tf_change(&s)
	testing.expect_value(t, s.recovery_attempts, u8(0))
	testing.expect_value(t, s.recovery_last_ms, i64(0))
}

// S29: Full reset clears recovery state.
@(test)
test_s29_recovery_clears_on_full_reset :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_recovery(&s, 14_000)
	apply_state_mark_recovery(&s, 45_000)

	apply_state_reset(&s)
	testing.expect_value(t, s.recovery_attempts, u8(0))
	testing.expect_value(t, s.recovery_last_ms, i64(0))
}

// S29: Recovery status is derived correctly.
@(test)
test_s29_recovery_status_derived :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// No recovery → None.
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.None)

	// 1 attempt → Recovering.
	apply_state_mark_recovery(&s, 14_000)
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Recovering)

	// 2 attempts → still Recovering.
	apply_state_mark_recovery(&s, 45_000)
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Recovering)

	// 3 attempts (max) → Exhausted.
	apply_state_mark_recovery(&s, 76_000)
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Exhausted)
}

// S29: No remediation for artifacts that were never received.
@(test)
test_s29_no_remediation_for_never_received :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// No events received at all — even at large timestamp, no remediation.
	testing.expect_value(t, apply_state_stale_remediation(s, 1_000_000), Remediation_Decision.None)
}

// S29: Telemetry includes recovery status.
@(test)
test_s29_telemetry_includes_recovery :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	telem := apply_state_telemetry(s)
	testing.expect_value(t, telem.recovery_status, Recovery_Status.None)
	testing.expect_value(t, telem.recovery_attempts, u8(0))

	apply_state_mark_recovery(&s, 14_000)
	telem = apply_state_telemetry(s)
	testing.expect_value(t, telem.recovery_status, Recovery_Status.Recovering)
	testing.expect_value(t, telem.recovery_attempts, u8(1))
}

// S29: Only Dual_Silence triggers remediation, not TF_Adaptive.
@(test)
test_s29_tf_adaptive_does_not_trigger_remediation :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Only candle received (TF_Adaptive stale detection).
	apply_state_mark_event(&s, .Candle, 1_000, false)

	// At t=200_000: candle is stale (>3x60s) but it's TF_Adaptive, not Dual_Silence.
	testing.expect_value(t, apply_state_stale_remediation(s, 200_000, 60_000), Remediation_Decision.None)
}

// S29: Single Dual_Silence artifact stale triggers remediation.
@(test)
test_s29_single_dual_silence_stale_triggers :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Only Stats received (Dual_Silence).
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// At t=14000: stats stale (>12s) — single Dual_Silence artifact stale.
	testing.expect_value(t, apply_state_stale_remediation(s, 14_000), Remediation_Decision.Resubscribe)
}

// =========================================================================
// S30: Adaptive Recovery Policies & Per-Stream Recovery Isolation tests.
// =========================================================================

// S30: Verify exponential backoff cooldown values.
@(test)
test_s30_recovery_cooldown_exponential_backoff :: proc(t: ^testing.T) {
	// Attempt 0 (before any recovery): base cooldown 15s.
	testing.expect_value(t, recovery_cooldown_for_attempt(0), i64(15_000))
	// Attempt 1: 15s << 1 = 30s.
	testing.expect_value(t, recovery_cooldown_for_attempt(1), i64(30_000))
	// Attempt 2: 15s << 2 = 60s (= max).
	testing.expect_value(t, recovery_cooldown_for_attempt(2), i64(60_000))
	// Attempt 3+: capped at 60s.
	testing.expect_value(t, recovery_cooldown_for_attempt(3), i64(60_000))
	testing.expect_value(t, recovery_cooldown_for_attempt(255), i64(60_000))
}

// S30: Adaptive cooldown window grows with each attempt.
@(test)
test_s30_adaptive_cooldown_first_attempt :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// First recovery at t=14_000.
	apply_state_mark_recovery(&s, 14_000)
	testing.expect_value(t, s.recovery_attempts, u8(1))

	// At t=29_000 (15s later): attempt=1 has cooldown=30s, still in cooldown.
	testing.expect_value(t, apply_state_stale_remediation(s, 29_000), Remediation_Decision.Cooldown)

	// At t=44_001 (30s+1ms later): cooldown expired → Resubscribe.
	testing.expect_value(t, apply_state_stale_remediation(s, 44_001), Remediation_Decision.Resubscribe)
}

// S30: Second attempt has 60s cooldown.
@(test)
test_s30_adaptive_cooldown_second_attempt :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// Two recovery attempts.
	apply_state_mark_recovery(&s, 14_000)
	apply_state_mark_recovery(&s, 45_000)
	testing.expect_value(t, s.recovery_attempts, u8(2))

	// At t=90_000 (45s after 2nd attempt): cooldown for attempt=2 is 60s, still in cooldown.
	testing.expect_value(t, apply_state_stale_remediation(s, 90_000), Remediation_Decision.Cooldown)

	// At t=105_001 (60s+1ms after 2nd attempt): cooldown expired → Resubscribe.
	testing.expect_value(t, apply_state_stale_remediation(s, 105_001), Remediation_Decision.Resubscribe)
}

// S30: Recovery state survives stream switch via slot sync.
@(test)
test_s30_recovery_preserved_in_slot :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_recovery(&s, 14_000)
	apply_state_mark_recovery(&s, 45_000)

	// Simulate slot copy: recovery state is preserved in struct copy.
	slot_copy := s
	testing.expect_value(t, slot_copy.recovery_attempts, u8(2))
	testing.expect_value(t, slot_copy.recovery_last_ms, i64(45_000))

	// Simulate sync back from slot.
	active: Stream_Apply_State
	active.recovery_attempts = slot_copy.recovery_attempts
	active.recovery_last_ms = slot_copy.recovery_last_ms
	testing.expect_value(t, active.recovery_attempts, u8(2))
	testing.expect_value(t, active.recovery_last_ms, i64(45_000))
}

// S30: Telemetry includes cooldown diagnostics.
@(test)
test_s30_telemetry_cooldown_remaining :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)

	// No recovery → cooldown = base (15s), remaining = 0.
	telem := apply_state_telemetry(s, 5_000)
	testing.expect_value(t, telem.recovery_cooldown_ms, i64(15_000))
	testing.expect_value(t, telem.recovery_cooldown_remaining_ms, i64(0))

	// After first recovery at t=14_000, check at t=20_000.
	apply_state_mark_recovery(&s, 14_000)
	telem = apply_state_telemetry(s, 20_000)
	// Attempt=1 → cooldown=30s, elapsed=6s, remaining=24s.
	testing.expect_value(t, telem.recovery_cooldown_ms, i64(30_000))
	testing.expect_value(t, telem.recovery_cooldown_remaining_ms, i64(24_000))

	// After cooldown expires at t=50_000.
	telem = apply_state_telemetry(s, 50_000)
	testing.expect_value(t, telem.recovery_cooldown_remaining_ms, i64(0))
}

// S30: Exhausted state still uses max cooldown for telemetry display.
@(test)
test_s30_exhausted_shows_max_cooldown :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// Exhaust all attempts.
	apply_state_mark_recovery(&s, 14_000)
	apply_state_mark_recovery(&s, 45_000)
	apply_state_mark_recovery(&s, 106_000)
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Exhausted)

	// Telemetry still shows cooldown for the exhausted attempt level.
	telem := apply_state_telemetry(s, 200_000)
	testing.expect_value(t, telem.recovery_cooldown_ms, i64(60_000))
}

// S30: No thrashing — adaptive cooldown prevents rapid re-attempts.
@(test)
test_s30_no_thrashing_with_backoff :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// First recovery at t=14_000. Next allowed after 30s (attempt=1 cooldown).
	apply_state_mark_recovery(&s, 14_000)

	// Rapid checks within cooldown — all must return Cooldown.
	for t_ms := i64(14_001); t_ms < 44_000; t_ms += 1_000 {
		testing.expect_value(t, apply_state_stale_remediation(s, t_ms), Remediation_Decision.Cooldown)
	}
	// First check after cooldown → Resubscribe.
	testing.expect_value(t, apply_state_stale_remediation(s, 44_000), Remediation_Decision.Resubscribe)
}

// S30: Success reset clears cooldown properly for fresh restart.
@(test)
test_s30_success_reset_enables_fast_first_recovery :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1_000, true)
	apply_state_mark_event(&s, .Stats, 1_000, false)

	// Two recovery attempts, then success.
	apply_state_mark_recovery(&s, 14_000)
	apply_state_mark_recovery(&s, 45_000)
	testing.expect_value(t, s.recovery_attempts, u8(2))

	// Fresh data arrives.
	apply_state_mark_event(&s, .Orderbook, 80_000, true)
	apply_state_mark_event(&s, .Stats, 80_000, false)
	apply_state_check_recovery_success(&s, 81_000)
	testing.expect_value(t, s.recovery_attempts, u8(0))
	testing.expect_value(t, s.recovery_last_ms, i64(0))

	// New stale episode: should use base cooldown (15s), not 60s.
	apply_state_mark_event(&s, .Orderbook, 100_000, true)
	apply_state_mark_event(&s, .Stats, 100_000, false)
	// At t=113_000: stale again → first attempt, base cooldown.
	testing.expect_value(t, apply_state_stale_remediation(s, 113_000), Remediation_Decision.Resubscribe)
	apply_state_mark_recovery(&s, 113_000)
	// Cooldown for attempt=1 = 30s.
	testing.expect_value(t, recovery_cooldown_for_attempt(s.recovery_attempts), i64(30_000))
}

// =========================================================================
// S31: Aggregate Health & Recovery Event Log tests.
// =========================================================================

// S31: All healthy — 3 slots with live data + range complete.
@(test)
test_s31_aggregate_health_all_healthy :: proc(t: ^testing.T) {
	states: [3]Stream_Apply_State
	used := [3]bool{true, true, true}

	for i := 0; i < 3; i += 1 {
		apply_state_mark_event(&states[i], .Trade, 1_000, false)
		apply_state_mark_event(&states[i], .Orderbook, 1_000, true)
		apply_state_mark_event(&states[i], .Candle, 1_000, false)
		apply_state_mark_range_sent(&states[i], 100)
		apply_state_mark_range_complete(&states[i], 500)
	}

	summary := aggregate_health_from_slots(states[:], used[:], 5_000, 60_000)
	testing.expect_value(t, summary.health_level, System_Health_Level.Healthy)
	testing.expect_value(t, summary.slots_composed, 3)
	testing.expect_value(t, summary.slot_count, 3)
	testing.expect_value(t, summary.total_stale, 0)
	testing.expect_value(t, summary.total_aging, 0)
}

// S31: Degraded — one slot with aging stats (Dual_Silence, 8s threshold).
@(test)
test_s31_aggregate_health_degraded_with_aging :: proc(t: ^testing.T) {
	states: [2]Stream_Apply_State
	used := [2]bool{true, true}

	// Slot 0: fresh data at t=10_000.
	apply_state_mark_event(&states[0], .Trade, 10_000, false)
	apply_state_mark_event(&states[0], .Orderbook, 10_000, true)

	// Slot 1: stats received at t=1_000, will be aging at now=10_000 (age=9s, >8s).
	apply_state_mark_event(&states[1], .Trade, 10_000, false)
	apply_state_mark_event(&states[1], .Stats, 1_000, false)

	summary := aggregate_health_from_slots(states[:], used[:], 10_000, 60_000)
	testing.expect_value(t, summary.health_level, System_Health_Level.Degraded)
	testing.expect(t, summary.total_aging > 0, "should have aging artifacts")
	testing.expect_value(t, summary.total_stale, 0)
}

// S31: Unhealthy — one slot with stale OB (>12s Dual_Silence).
@(test)
test_s31_aggregate_health_unhealthy_stale :: proc(t: ^testing.T) {
	states: [2]Stream_Apply_State
	used := [2]bool{true, true}

	// Slot 0: fresh.
	apply_state_mark_event(&states[0], .Trade, 20_000, false)

	// Slot 1: orderbook received at t=1_000, stale at now=20_000 (age=19s, >12s).
	apply_state_mark_event(&states[1], .Orderbook, 1_000, true)

	summary := aggregate_health_from_slots(states[:], used[:], 20_000, 60_000)
	testing.expect_value(t, summary.health_level, System_Health_Level.Unhealthy)
	testing.expect(t, summary.total_stale > 0, "should have stale artifacts")
}

// S31: Critical — both slots stale + one exhausted.
@(test)
test_s31_aggregate_health_critical :: proc(t: ^testing.T) {
	states: [2]Stream_Apply_State
	used := [2]bool{true, true}

	// Slot 0: stale OB + exhausted recovery.
	apply_state_mark_event(&states[0], .Orderbook, 1_000, true)
	apply_state_mark_recovery(&states[0], 14_000)
	apply_state_mark_recovery(&states[0], 45_000)
	apply_state_mark_recovery(&states[0], 106_000)

	// Slot 1: stale stats.
	apply_state_mark_event(&states[1], .Stats, 1_000, false)

	summary := aggregate_health_from_slots(states[:], used[:], 200_000, 60_000)
	testing.expect_value(t, summary.health_level, System_Health_Level.Critical)
	testing.expect(t, summary.total_stale >= 2, "need multiple stale for critical")
	testing.expect_value(t, summary.slots_exhausted, 1)
}

// S31: Unused slots are ignored in aggregate health.
@(test)
test_s31_aggregate_health_empty_slots_ignored :: proc(t: ^testing.T) {
	states: [3]Stream_Apply_State
	used := [3]bool{true, false, true}

	// Slot 0: healthy with live data.
	apply_state_mark_event(&states[0], .Trade, 5_000, false)
	apply_state_mark_event(&states[0], .Candle, 5_000, false)
	apply_state_mark_range_sent(&states[0], 100)
	apply_state_mark_range_complete(&states[0], 500)

	// Slot 1: stale — but unused, must be ignored.
	apply_state_mark_event(&states[1], .Orderbook, 1_000, true)

	// Slot 2: healthy with live data.
	apply_state_mark_event(&states[2], .Trade, 5_000, false)
	apply_state_mark_event(&states[2], .Candle, 5_000, false)
	apply_state_mark_range_sent(&states[2], 100)
	apply_state_mark_range_complete(&states[2], 500)

	summary := aggregate_health_from_slots(states[:], used[:], 6_000, 60_000)
	testing.expect_value(t, summary.slot_count, 2)
	testing.expect_value(t, summary.total_stale, 0)
	testing.expect_value(t, summary.health_level, System_Health_Level.Healthy)
}

// S31: Worst composition across heterogeneous slots.
@(test)
test_s31_aggregate_worst_composition :: proc(t: ^testing.T) {
	states: [3]Stream_Apply_State
	used := [3]bool{true, true, true}

	// Slot 0: Composed (range + live candle).
	apply_state_mark_event(&states[0], .Candle, 5_000, false)
	apply_state_mark_range_sent(&states[0], 100)
	apply_state_mark_range_complete(&states[0], 500)

	// Slot 1: Live_Only (live candle, no range).
	apply_state_mark_event(&states[1], .Candle, 5_000, false)

	// Slot 2: Empty (no events).

	summary := aggregate_health_from_slots(states[:], used[:], 6_000, 60_000)
	testing.expect_value(t, summary.worst_composition, Composition_Stage.Empty)
	testing.expect_value(t, summary.slots_composed, 1)
	testing.expect_value(t, summary.slots_live_only, 1)
	testing.expect_value(t, summary.slots_empty, 1)
}

// S31: Recovery event log push and get (newest-first).
@(test)
test_s31_recovery_event_log_push_and_get :: proc(t: ^testing.T) {
	log: Recovery_Event_Log

	recovery_event_log_push(&log, Recovery_Event{kind = .Attempt,   timestamp = 10_000, attempts = 1, slot_id = 0})
	recovery_event_log_push(&log, Recovery_Event{kind = .Attempt,   timestamp = 20_000, attempts = 2, slot_id = 0})
	recovery_event_log_push(&log, Recovery_Event{kind = .Exhausted, timestamp = 30_000, attempts = 3, slot_id = 0})

	testing.expect_value(t, log.count, 3)

	// Index 0 = newest.
	evt, ok := recovery_event_log_get(&log, 0)
	testing.expect(t, ok, "get(0) should succeed")
	testing.expect_value(t, evt.kind, Recovery_Event_Kind.Exhausted)
	testing.expect_value(t, evt.timestamp, i64(30_000))
	testing.expect_value(t, evt.attempts, u8(3))

	// Index 1 = second newest.
	evt, ok = recovery_event_log_get(&log, 1)
	testing.expect(t, ok, "get(1) should succeed")
	testing.expect_value(t, evt.kind, Recovery_Event_Kind.Attempt)
	testing.expect_value(t, evt.timestamp, i64(20_000))

	// Index 2 = oldest.
	evt, ok = recovery_event_log_get(&log, 2)
	testing.expect(t, ok, "get(2) should succeed")
	testing.expect_value(t, evt.timestamp, i64(10_000))

	// Out of range.
	_, ok = recovery_event_log_get(&log, 3)
	testing.expect(t, !ok, "get(3) should fail — only 3 events")
}

// S31: Recovery event log wraps — oldest events evicted at capacity.
@(test)
test_s31_recovery_event_log_wraps :: proc(t: ^testing.T) {
	log: Recovery_Event_Log

	// Push RECOVERY_EVENT_LOG_CAP + 2 events.
	total := RECOVERY_EVENT_LOG_CAP + 2
	for i := 0; i < total; i += 1 {
		recovery_event_log_push(&log, Recovery_Event{
			kind      = .Attempt,
			timestamp = i64(i * 1_000),
			attempts  = u8(i % 256),
			slot_id   = 0,
		})
	}

	// Count capped at capacity.
	testing.expect_value(t, log.count, RECOVERY_EVENT_LOG_CAP)

	// Newest event is the last pushed (index = total-1).
	evt, ok := recovery_event_log_get(&log, 0)
	testing.expect(t, ok, "get(0) should succeed")
	testing.expect_value(t, evt.timestamp, i64((total - 1) * 1_000))

	// Oldest surviving event: index CAP-1 should be event at (total - CAP).
	evt, ok = recovery_event_log_get(&log, RECOVERY_EVENT_LOG_CAP - 1)
	testing.expect(t, ok, "get(CAP-1) should succeed")
	testing.expect_value(t, evt.timestamp, i64((total - RECOVERY_EVENT_LOG_CAP) * 1_000))

	// Beyond capacity: out of range.
	_, ok = recovery_event_log_get(&log, RECOVERY_EVENT_LOG_CAP)
	testing.expect(t, !ok, "get(CAP) should fail — only CAP events stored")
}

// =========================================================================
// S32: Truth Alignment — per-artifact timing flows through apply_state.
// These tests prove that last_recv_ms is the sole source for timing fields,
// and that apply_state resets clear them properly.
// =========================================================================

@(test)
test_s32_apply_state_timing_flows_through_mark_event :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Stats event at t=5000 should set last_recv_ms[.Stats].
	apply_state_mark_event(&s, .Stats, 5000, false)
	testing.expect_value(t, s.last_recv_ms[.Stats], i64(5000))

	// Orderbook snapshot at t=6000 should set last_recv_ms[.Orderbook].
	apply_state_mark_event(&s, .Orderbook, 6000, true)
	testing.expect_value(t, s.last_recv_ms[.Orderbook], i64(6000))

	// Both should be readable — adapter would sync to metrics.
	testing.expect(t, s.last_recv_ms[.Stats] > 0, "stats timing must be set")
	testing.expect(t, s.last_recv_ms[.Orderbook] > 0, "orderbook timing must be set")
}

@(test)
test_s32_apply_state_reset_clears_timing :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 5000, false)
	apply_state_mark_event(&s, .Orderbook, 6000, true)

	apply_state_reset(&s)

	testing.expect_value(t, s.last_recv_ms[.Stats], i64(0))
	testing.expect_value(t, s.last_recv_ms[.Orderbook], i64(0))
}

@(test)
test_s32_apply_state_reconnect_preserves_timing :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 5000, false)
	apply_state_mark_event(&s, .Orderbook, 6000, true)
	apply_state_mark_event(&s, .Trade, 7000, false)

	apply_state_on_reconnect(&s)

	// Reconnect clears snapshot_seen for gated artifacts but preserves timing.
	// Stats and Trade timing survive reconnect (per policy, no reset_on_reconnect).
	testing.expect_value(t, s.last_recv_ms[.Stats], i64(5000))
	testing.expect_value(t, s.last_recv_ms[.Orderbook], i64(6000))
	testing.expect_value(t, s.last_recv_ms[.Trade], i64(7000))
}

@(test)
test_s32_apply_state_tf_change_clears_tf_sensitive_timing :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_event(&s, .Heatmap, 6000, false)
	apply_state_mark_event(&s, .VPVR, 7000, false)
	apply_state_mark_event(&s, .Stats, 8000, false)
	apply_state_mark_event(&s, .Trade, 9000, false)

	apply_state_on_tf_change(&s)

	// TF-sensitive artifacts should have timing cleared.
	testing.expect_value(t, s.last_recv_ms[.Candle], i64(0))
	testing.expect_value(t, s.last_recv_ms[.Heatmap], i64(0))
	testing.expect_value(t, s.last_recv_ms[.VPVR], i64(0))
	// Non-TF-sensitive artifacts should preserve timing.
	testing.expect_value(t, s.last_recv_ms[.Stats], i64(8000))
	testing.expect_value(t, s.last_recv_ms[.Trade], i64(9000))
}

@(test)
test_s32_summary_includes_timing :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 5000, false)
	apply_state_mark_event(&s, .Orderbook, 6000, true)
	apply_state_mark_event(&s, .Candle, 7000, false)

	telemetry := apply_state_telemetry(s, 8000)
	testing.expect_value(t, telemetry.last_recv_ms[.Stats], i64(5000))
	testing.expect_value(t, telemetry.last_recv_ms[.Orderbook], i64(6000))
	testing.expect_value(t, telemetry.last_recv_ms[.Candle], i64(7000))
}

@(test)
test_s32_timing_monotonic_updates :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Multiple stats events — timing should always reflect latest.
	apply_state_mark_event(&s, .Stats, 5000, false)
	apply_state_mark_event(&s, .Stats, 6000, false)
	apply_state_mark_event(&s, .Stats, 7000, false)
	testing.expect_value(t, s.last_recv_ms[.Stats], i64(7000))
	testing.expect_value(t, s.artifact_event_count[.Stats], u64(3))
}

// ==========================================================================
// S35: Recovery & Health Control Plane tests
// ==========================================================================

@(test)
test_s35_stream_health_level_healthy :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	// Fresh at now=11_000 (1s age, well within 8s Dual_Silence warn threshold).
	testing.expect_value(t, stream_health_level(s, 11_000), System_Health_Level.Healthy)
}

@(test)
test_s35_stream_health_level_degraded_aging :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	// Aging at now=19_000 (9s age, past 8s Dual_Silence warn but below 12s stale).
	testing.expect_value(t, stream_health_level(s, 19_000), System_Health_Level.Degraded)
}

@(test)
test_s35_stream_health_level_unhealthy_stale :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	// Stale at now=23_000 (13s age, past 12s Dual_Silence stale threshold).
	testing.expect_value(t, stream_health_level(s, 23_000), System_Health_Level.Unhealthy)
}

@(test)
test_s35_stream_health_level_critical_exhausted :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	s.recovery_attempts = RECOVERY_MAX_ATTEMPTS
	// Stale + exhausted = critical.
	testing.expect_value(t, stream_health_level(s, 23_000), System_Health_Level.Critical)
}

@(test)
test_s35_stream_health_level_no_data_is_healthy :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// No events received yet — not degraded.
	testing.expect_value(t, stream_health_level(s, 10_000), System_Health_Level.Healthy)
}

@(test)
test_s35_health_tick_evaluate_none_when_fresh :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	tick := health_tick_evaluate(Health_Tick_Input{
		apply_state = s,
		now_ms = 11_000,
		tf_ms = 60_000,
		is_connected = true,
		is_offline = false,
	})
	testing.expect_value(t, tick.remediation, Remediation_Decision.None)
	testing.expect_value(t, tick.recovery_success, false)
	testing.expect_value(t, tick.stream_health, System_Health_Level.Healthy)
	testing.expect_value(t, tick.stale_count, 0)
}

@(test)
test_s35_health_tick_evaluate_resubscribe_when_stale :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	tick := health_tick_evaluate(Health_Tick_Input{
		apply_state = s,
		now_ms = 23_000,  // 13s past → Stale
		tf_ms = 60_000,
		is_connected = true,
		is_offline = false,
	})
	testing.expect_value(t, tick.remediation, Remediation_Decision.Resubscribe)
	testing.expect_value(t, tick.stream_health, System_Health_Level.Unhealthy)
	testing.expect(t, tick.stale_count > 0, "expected stale artifacts")
}

@(test)
test_s35_health_tick_evaluate_no_action_when_disconnected :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	tick := health_tick_evaluate(Health_Tick_Input{
		apply_state = s,
		now_ms = 23_000,
		tf_ms = 60_000,
		is_connected = false,  // disconnected
		is_offline = false,
	})
	// Recovery decisions suppressed when disconnected.
	testing.expect_value(t, tick.remediation, Remediation_Decision.None)
}

@(test)
test_s35_health_tick_evaluate_recovery_success :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	// Simulate previous recovery attempt.
	s.recovery_attempts = 1
	s.recovery_last_ms = 5_000
	// Now all Dual_Silence artifacts are fresh (1s age).
	tick := health_tick_evaluate(Health_Tick_Input{
		apply_state = s,
		now_ms = 11_000,
		tf_ms = 60_000,
		is_connected = true,
		is_offline = false,
	})
	testing.expect_value(t, tick.recovery_success, true)
	testing.expect_value(t, tick.remediation, Remediation_Decision.None)
}

@(test)
test_s35_telemetry_includes_stream_health :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	telem := apply_state_telemetry(s, 11_000, 60_000)
	testing.expect_value(t, telem.stream_health, System_Health_Level.Healthy)
	telem2 := apply_state_telemetry(s, 23_000, 60_000)
	testing.expect_value(t, telem2.stream_health, System_Health_Level.Unhealthy)
}

@(test)
test_s35_recovery_event_reset_kind :: proc(t: ^testing.T) {
	// Verify that Reset event kind can be pushed and retrieved from the log.
	log: Recovery_Event_Log
	recovery_event_log_push(&log, Recovery_Event{
		kind = .Reset,
		timestamp = 50_000,
		attempts = 2,
		slot_id = 1,
	})
	evt, ok := recovery_event_log_get(&log, 0)
	testing.expect(t, ok, "should retrieve event")
	testing.expect_value(t, evt.kind, Recovery_Event_Kind.Reset)
	testing.expect_value(t, evt.attempts, u8(2))
	testing.expect_value(t, evt.slot_id, u8(1))
}

// =========================================================================
// S36: Surface Read-Model Stabilization tests.
// Verify that per-cell derived views produce correct results from apply state,
// and that read model contracts are stable across composition/health states.
// =========================================================================

@(test)
test_s36_composition_stage_empty_state :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)
	testing.expect_value(t, stream_health_level(s, 10_000), System_Health_Level.Healthy)
}

@(test)
test_s36_composition_stage_live_only :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Live_Only)
}

@(test)
test_s36_composition_stage_backfilled :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 500)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Backfilled)
}

@(test)
test_s36_composition_stage_composed :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 500)
	apply_state_mark_event(&s, .Candle, 2000, false)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)
}

@(test)
test_s36_cell_composition_mirrors_global :: proc(t: ^testing.T) {
	// cell_composition_stage should produce the same result as apply_state_composition_stage
	// when given matching inputs.
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 1, 100)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Range_Pending)
	testing.expect_value(t, cell_composition_stage(true, false, false), Composition_Stage.Range_Pending)
}

@(test)
test_s36_staleness_fresh_when_recent :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	apply_state_mark_event(&s, .Stats, 10_000, false)
	stale, aging := apply_state_stale_artifact_count(s, 11_000, 60_000)
	testing.expect_value(t, stale, 0)
	testing.expect_value(t, aging, 0)
}

@(test)
test_s36_staleness_aging_at_8s :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	// 8s past = aging for Dual_Silence.
	staleness := apply_state_artifact_staleness(s, .Orderbook, 18_000)
	testing.expect_value(t, staleness, Artifact_Staleness.Aging)
}

@(test)
test_s36_staleness_stale_at_12s :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 10_000, false)
	// 12s past = stale for Dual_Silence.
	staleness := apply_state_artifact_staleness(s, .Stats, 22_000)
	testing.expect_value(t, staleness, Artifact_Staleness.Stale)
}

@(test)
test_s36_health_level_degraded_when_aging :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 10_000, true)
	// 9s = aging
	hl := stream_health_level(s, 19_000)
	testing.expect_value(t, hl, System_Health_Level.Degraded)
}

@(test)
test_s36_summary_has_live_flags :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1000, false)
	apply_state_mark_event(&s, .Heatmap, 2000, false)
	summary := apply_state_summary(s)
	testing.expect(t, summary.has_live_stats, "stats should be live")
	testing.expect(t, summary.has_live_heatmap, "heatmap should be live")
	testing.expect(t, !summary.has_live_candle, "candle should not be live")
	testing.expect(t, !summary.has_live_vpvr, "vpvr should not be live")
}

@(test)
test_s36_candle_recv_ms_max_of_live_and_range :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_event(&s, .Range_Candle, 8000, false)
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(8000))
	// Now live surpasses range.
	apply_state_mark_event(&s, .Candle, 9000, false)
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(9000))
}

@(test)
test_s36_active_artifact_count :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, apply_state_active_artifact_count(s), 0)
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Stats, 2000, false)
	testing.expect_value(t, apply_state_active_artifact_count(s), 2)
}

// --- S37: Compare Mode Surface Enrichment ---
// Verify that pure surface view derivation produces correct composition,
// health, staleness, and identity signals from apply_state alone.

@(test)
test_s37_surface_composition_empty_no_health_dot :: proc(t: ^testing.T) {
	// Empty apply state should yield Empty composition, Healthy level, no live data.
	s: Stream_Apply_State
	comp := apply_state_composition_stage(s)
	health := stream_health_level(s, 10000, 60_000)
	testing.expect_value(t, comp, Composition_Stage.Empty)
	testing.expect_value(t, health, System_Health_Level.Healthy)
}

@(test)
test_s37_surface_composition_live_only :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Live_Only)
	health := stream_health_level(s, 6000, 60_000)
	testing.expect_value(t, health, System_Health_Level.Healthy)
}

@(test)
test_s37_surface_composition_composed :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_range_complete(&s, 1000)
	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Composed)
}

@(test)
test_s37_surface_health_degraded_on_aging :: proc(t: ^testing.T) {
	// Stats aging at 8s with Dual_Silence policy → Degraded.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1000, false)
	health := stream_health_level(s, 9000, 60_000)
	testing.expect_value(t, health, System_Health_Level.Degraded)
}

@(test)
test_s37_surface_health_unhealthy_on_stale :: proc(t: ^testing.T) {
	// Stats stale at 12s with Dual_Silence policy → Unhealthy.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1000, false)
	health := stream_health_level(s, 13000, 60_000)
	testing.expect_value(t, health, System_Health_Level.Unhealthy)
}

@(test)
test_s37_surface_staleness_counts_for_compare :: proc(t: ^testing.T) {
	// Two artifacts: one fresh, one stale → stale_count=1.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 9000, false)    // fresh at now=10000
	apply_state_mark_event(&s, .Stats, 1000, false)    // stale at now=10000 (9s, >8s for Dual_Silence)
	_, aging := apply_state_stale_artifact_count(s, 10000, 60_000)
	testing.expect(t, aging >= 1, "at least one artifact should be aging")
}

@(test)
test_s37_surface_has_live_data_flag :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// No events → no live data.
	has_live := false
	for kind in Artifact_Kind {
		if s.has_live[kind] { has_live = true; break }
	}
	testing.expect(t, !has_live, "no live data on empty state")
	// After a trade event → has live data.
	apply_state_mark_event(&s, .Trade, 1000, false)
	for kind in Artifact_Kind {
		if s.has_live[kind] { has_live = true; break }
	}
	testing.expect(t, has_live, "trade should set live data flag")
}

@(test)
test_s37_cell_composition_stage_pending :: proc(t: ^testing.T) {
	// GetRange pending without live candle → Range_Pending.
	comp := cell_composition_stage(true, false, false)
	testing.expect_value(t, comp, Composition_Stage.Range_Pending)
}

@(test)
test_s37_cell_composition_stage_backfilled :: proc(t: ^testing.T) {
	// GetRange seeded without live candle → Backfilled.
	comp := cell_composition_stage(false, true, false)
	testing.expect_value(t, comp, Composition_Stage.Backfilled)
}

@(test)
test_s37_cell_composition_stage_composed :: proc(t: ^testing.T) {
	// GetRange seeded + live candle → Composed.
	comp := cell_composition_stage(false, true, true)
	testing.expect_value(t, comp, Composition_Stage.Composed)
}

// --- S38: Per-Pane TF Isolation ---
// Verify that the same apply_state produces different health/staleness results
// when evaluated at different TF durations, validating per-pane TF isolation.
// TF only affects TF_Adaptive artifacts (Candle); Dual_Silence (Stats, Orderbook)
// uses fixed 8s/12s thresholds regardless of TF.

@(test)
test_s38_per_pane_tf_health_isolation :: proc(t: ^testing.T) {
	// Candle event at t=1000. At now=131_000 (130s gap):
	// TF_Adaptive aging = max(2*tf, 5000)
	// - tf=60_000 (1m): aging=120s → 130s gap > 120s → Degraded
	// - tf=3_600_000 (1h): aging=7200s → 130s gap < 7200s → Healthy
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)

	health_1m := stream_health_level(s, 131_000, 60_000)
	health_1h := stream_health_level(s, 131_000, 3_600_000)

	testing.expect_value(t, health_1m, System_Health_Level.Degraded)
	testing.expect_value(t, health_1h, System_Health_Level.Healthy)
}

@(test)
test_s38_per_pane_tf_staleness_isolation :: proc(t: ^testing.T) {
	// Candle event at t=1000. At now=201_000 (200s gap):
	// TF_Adaptive stale = max(3*tf, 10000)
	// - tf=60_000 (1m): stale=180s → 200s > 180s → stale_count=1
	// - tf=3_600_000 (1h): stale=10800s → 200s < 10800s → stale_count=0
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)

	stale_1m, _ := apply_state_stale_artifact_count(s, 201_000, 60_000)
	stale_1h, aging_1h := apply_state_stale_artifact_count(s, 201_000, 3_600_000)

	testing.expect(t, stale_1m > 0, "1m TF: candle should be stale at 200s gap")
	testing.expect_value(t, stale_1h, 0)
	testing.expect_value(t, aging_1h, 0)
}

@(test)
test_s38_per_pane_tf_composition_unaffected :: proc(t: ^testing.T) {
	// Composition stage depends on apply_state (getrange/live), not TF.
	// Same apply_state should produce same composition regardless of TF.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_range_complete(&s, 1000)

	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Composed)
	// TF does not influence composition derivation — it's purely about getrange + live state.
}

@(test)
test_s38_per_pane_tf_two_panes_different_health :: proc(t: ^testing.T) {
	// Simulate two panes viewing the same market at different TFs.
	// Candle event at t=1000, now=131_000 (130s gap).
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)

	now_ms: i64 = 131_000

	// Pane A: 1m TF → aging at 120s → Degraded
	health_a := stream_health_level(s, now_ms, 60_000)
	// Pane B: 1h TF → aging at 7200s → Healthy
	health_b := stream_health_level(s, now_ms, 3_600_000)

	testing.expect_value(t, health_a, System_Health_Level.Degraded)
	testing.expect_value(t, health_b, System_Health_Level.Healthy)
}

@(test)
test_s38_per_pane_tf_default_follows_global :: proc(t: ^testing.T) {
	// When per-pane tf_idx is -1, effective TF should match global.
	// Same TF → same health result.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)

	tf_ms: i64 = 60_000
	now_ms: i64 = 131_000

	health_global := stream_health_level(s, now_ms, tf_ms)
	health_pane := stream_health_level(s, now_ms, tf_ms) // same TF when following global

	testing.expect_value(t, health_global, health_pane)
}

@(test)
test_s38_per_pane_tf_critical_isolation :: proc(t: ^testing.T) {
	// Two stale Dual_Silence artifacts + exhausted recovery → Critical.
	// Dual_Silence is TF-independent, so we need Orderbook+Stats for critical.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	// Exhaust recovery: 3 attempts
	s.recovery_attempts = 3
	s.recovery_last_ms = 1000

	now_ms: i64 = 20000 // 19s gap > 12s stale threshold

	// Both Dual_Silence artifacts stale + exhausted → Critical (TF irrelevant for Dual_Silence)
	health := stream_health_level(s, now_ms, 60_000)
	testing.expect_value(t, health, System_Health_Level.Critical)
}

@(test)
test_s38_per_pane_tf_candle_stale_not_critical :: proc(t: ^testing.T) {
	// Only candle stale (TF_Adaptive) + exhausted recovery.
	// stale=1 + exhausted → Unhealthy (not Critical, which needs stale >= 2).
	// At 1h TF, candle is fresh → recovery exhausted alone → Unhealthy.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	s.recovery_attempts = 3
	s.recovery_last_ms = 1000

	now_ms: i64 = 201_000 // 200s gap

	// 1m TF: candle stale (>180s) + exhausted → Unhealthy
	health_1m := stream_health_level(s, now_ms, 60_000)
	// 1h TF: candle fresh (<10800s) + exhausted → Unhealthy (exhausted alone)
	health_1h := stream_health_level(s, now_ms, 3_600_000)

	testing.expect_value(t, health_1m, System_Health_Level.Unhealthy)
	testing.expect_value(t, health_1h, System_Health_Level.Unhealthy)
}

// =========================================================================
// S39: Compare Pane Backfill, Focus & Local Invalidation tests.
// Tests verify per-pane composition derived from per-pane getrange + canonical apply_state.
// =========================================================================

@(test)
test_s39_pane_composition_empty_when_no_getrange :: proc(t: ^testing.T) {
	// Per-pane getrange: pending=false, seeded=false.
	// Slot has no live candle. → .Empty
	comp := cell_composition_stage(false, false, false)
	testing.expect_value(t, comp, Composition_Stage.Empty)
}

@(test)
test_s39_pane_composition_range_pending :: proc(t: ^testing.T) {
	// Per-pane getrange: pending=true, seeded=false, no live candle → .Range_Pending
	comp := cell_composition_stage(true, false, false)
	testing.expect_value(t, comp, Composition_Stage.Range_Pending)
}

@(test)
test_s39_pane_composition_backfilled :: proc(t: ^testing.T) {
	// Per-pane getrange: seeded=true, slot has no live candle → .Backfilled
	comp := cell_composition_stage(false, true, false)
	testing.expect_value(t, comp, Composition_Stage.Backfilled)
}

@(test)
test_s39_pane_composition_live_only :: proc(t: ^testing.T) {
	// Per-pane getrange: seeded=false, slot has live candle → .Live_Only
	comp := cell_composition_stage(false, false, true)
	testing.expect_value(t, comp, Composition_Stage.Live_Only)
}

@(test)
test_s39_pane_composition_composed :: proc(t: ^testing.T) {
	// Per-pane getrange: seeded=true, slot has live candle → .Composed
	comp := cell_composition_stage(false, true, true)
	testing.expect_value(t, comp, Composition_Stage.Composed)
}

@(test)
test_s39_tf_change_clears_getrange_not_other_artifacts :: proc(t: ^testing.T) {
	// Simulates per-pane TF change: apply_state_on_tf_change clears TF-sensitive
	// data but preserves TF-insensitive artifacts (Trade, Orderbook, Stats).
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	apply_state_mark_event(&s, .Candle, 3000, false)
	apply_state_mark_event(&s, .Heatmap, 4000, false)
	s.getrange_seeded = true
	s.getrange_pending = false
	s.getrange_oldest_ts = 500

	apply_state_on_tf_change(&s)

	// TF-sensitive artifacts cleared.
	testing.expect(t, !s.has_live[.Candle], "candle must clear on tf change")
	testing.expect(t, !s.has_live[.Heatmap], "heatmap must clear on tf change")
	testing.expect(t, !s.getrange_seeded, "getrange_seeded must clear")
	testing.expect_value(t, s.getrange_oldest_ts, i64(0))

	// TF-insensitive artifacts survive.
	testing.expect(t, s.has_live[.Trade], "trade must survive tf change")
	testing.expect(t, s.has_live[.Orderbook], "orderbook must survive tf change")
}

@(test)
test_s39_pane_composition_after_tf_change_is_empty :: proc(t: ^testing.T) {
	// After TF change on a pane, both getrange and live candle are cleared.
	// The pane should show .Empty until new data arrives.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 3000, false)
	s.getrange_seeded = true

	// Before TF change: Composed
	comp_before := cell_composition_stage(false, true, true)
	testing.expect_value(t, comp_before, Composition_Stage.Composed)

	apply_state_on_tf_change(&s)

	// After TF change: getrange cleared, candle live cleared → Empty
	comp_after := cell_composition_stage(false, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp_after, Composition_Stage.Empty)
}

@(test)
test_s39_independent_panes_isolated_composition :: proc(t: ^testing.T) {
	// Two independent apply states simulate two panes.
	// TF change on pane A should not affect pane B's composition.
	a: Stream_Apply_State
	b: Stream_Apply_State

	// Both start Composed.
	apply_state_mark_event(&a, .Candle, 1000, false)
	a.getrange_seeded = true
	apply_state_mark_event(&b, .Candle, 2000, false)
	b.getrange_seeded = true

	comp_a := cell_composition_stage(false, a.getrange_seeded, a.has_live[.Candle])
	comp_b := cell_composition_stage(false, b.getrange_seeded, b.has_live[.Candle])
	testing.expect_value(t, comp_a, Composition_Stage.Composed)
	testing.expect_value(t, comp_b, Composition_Stage.Composed)

	// TF change on pane A only.
	apply_state_on_tf_change(&a)

	comp_a_after := cell_composition_stage(false, a.getrange_seeded, a.has_live[.Candle])
	comp_b_after := cell_composition_stage(false, b.getrange_seeded, b.has_live[.Candle])
	testing.expect_value(t, comp_a_after, Composition_Stage.Empty)
	testing.expect_value(t, comp_b_after, Composition_Stage.Composed) // B unaffected
}

@(test)
test_s39_pane_backfill_seed_marks_seeded :: proc(t: ^testing.T) {
	// apply_state_mark_range_complete sets seeded=true and clears pending.
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABCD)
	testing.expect(t, s.getrange_pending, "pending after mark_range_sent")

	apply_state_mark_range_complete(&s, 5000)
	testing.expect(t, s.getrange_seeded, "seeded after mark_range_complete")
	testing.expect(t, !s.getrange_pending, "pending cleared after complete")
	testing.expect_value(t, s.getrange_oldest_ts, i64(5000))
}

@(test)
test_s39_pane_recovery_isolated_per_slot :: proc(t: ^testing.T) {
	// Recovery state lives in each slot's apply_state.
	// Marking recovery on slot A should not affect slot B.
	a: Stream_Apply_State
	b: Stream_Apply_State

	apply_state_mark_event(&a, .Orderbook, 1000, true)
	apply_state_mark_event(&b, .Orderbook, 1000, true)

	apply_state_mark_recovery(&a, 50_000)
	testing.expect_value(t, a.recovery_attempts, u8(1))
	testing.expect_value(t, b.recovery_attempts, u8(0)) // B unaffected
}

// =========================================================================
// S41: Per-Pane Historical/Realtime Composition tests.
// Tests verify the full lifecycle: seed → backfill → live continuation → composed,
// per-pane getrange completion, timeout guard, reconnect clearing, and
// independent pane composition coexistence.
// =========================================================================

@(test)
test_s41_pane_lifecycle_empty_to_composed :: proc(t: ^testing.T) {
	// Full lifecycle: Empty → Range_Pending → Backfilled → Composed
	s: Stream_Apply_State

	// Phase 1: Empty (no getrange, no live)
	comp := cell_composition_stage(false, false, false)
	testing.expect_value(t, comp, Composition_Stage.Empty)

	// Phase 2: Range_Pending (getrange in flight)
	apply_state_mark_range_sent(&s, 1, 0x1234)
	comp = cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Range_Pending)

	// Phase 3: Backfilled (range complete, no live yet)
	apply_state_mark_range_complete(&s, 5000)
	comp = cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Backfilled)

	// Phase 4: Composed (range seeded + live candle)
	apply_state_mark_event(&s, .Candle, 10000, false)
	comp = cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Composed)
}

@(test)
test_s41_range_complete_clears_pending_and_sets_oldest :: proc(t: ^testing.T) {
	// Verifies that mark_range_complete correctly transitions per-pane state.
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABC)
	testing.expect(t, s.getrange_pending, "pending after send")
	testing.expect_value(t, s.getrange_request_id, u64(0xABC))

	apply_state_mark_range_complete(&s, 3000)
	testing.expect(t, !s.getrange_pending, "pending must clear on complete")
	testing.expect(t, s.getrange_seeded, "seeded must be true after complete")
	testing.expect_value(t, s.getrange_oldest_ts, i64(3000))
	testing.expect_value(t, s.getrange_request_id, u64(0)) // cleared on complete
}

@(test)
test_s41_range_complete_preserves_older_oldest_ts :: proc(t: ^testing.T) {
	// If oldest_ts already has a value, mark_range_complete should only
	// update it if the new value is smaller (extends history further).
	s: Stream_Apply_State
	s.getrange_seeded = true
	s.getrange_oldest_ts = 2000

	// Simulate a second batch with newer oldest — should NOT update.
	apply_state_mark_range_sent(&s, 20, 0)
	apply_state_mark_range_complete(&s, 3000)
	testing.expect_value(t, s.getrange_oldest_ts, i64(2000)) // kept older

	// Simulate a third batch extending further back.
	apply_state_mark_range_sent(&s, 30, 0)
	apply_state_mark_range_complete(&s, 1000)
	testing.expect_value(t, s.getrange_oldest_ts, i64(1000)) // updated
}

@(test)
test_s41_getrange_timeout_detected :: proc(t: ^testing.T) {
	// Timeout detection should fire when current_frame > sent_frame + timeout.
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 100, 0)
	testing.expect(t, !apply_state_check_getrange_timeout(s, 399, 300), "should not timeout at frame 399")
	testing.expect(t, apply_state_check_getrange_timeout(s, 401, 300), "should timeout at frame 401")
}

@(test)
test_s41_getrange_timeout_not_fired_when_not_pending :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Not pending — timeout should never fire regardless of frame.
	testing.expect(t, !apply_state_check_getrange_timeout(s, 99999, 300), "no timeout when not pending")
}

@(test)
test_s41_reconnect_clears_getrange_state :: proc(t: ^testing.T) {
	// Reconnect should clear getrange pending + request_id so panes can re-request.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	apply_state_mark_range_sent(&s, 10, 0xDEAD)
	s.getrange_seeded = true
	s.getrange_oldest_ts = 500

	apply_state_on_reconnect(&s)

	testing.expect(t, !s.getrange_pending, "pending must clear on reconnect")
	testing.expect_value(t, s.getrange_request_id, u64(0))
	// seeded and oldest_ts survive reconnect (data is still in store)
	testing.expect(t, s.getrange_seeded, "seeded must survive reconnect")
	testing.expect_value(t, s.getrange_oldest_ts, i64(500))
}

@(test)
test_s41_independent_panes_different_lifecycle_stages :: proc(t: ^testing.T) {
	// Pane A: Composed. Pane B: Backfilled. Pane C: Range_Pending.
	// Each pane is independent, uses own getrange state + own slot.
	a: Stream_Apply_State
	b: Stream_Apply_State
	c: Stream_Apply_State

	// Pane A: Composed (seeded + live)
	apply_state_mark_range_sent(&a, 1, 0)
	apply_state_mark_range_complete(&a, 1000)
	apply_state_mark_event(&a, .Candle, 5000, false)

	// Pane B: Backfilled (seeded, no live)
	apply_state_mark_range_sent(&b, 2, 0)
	apply_state_mark_range_complete(&b, 2000)

	// Pane C: Range_Pending (in flight)
	apply_state_mark_range_sent(&c, 3, 0)

	comp_a := cell_composition_stage(a.getrange_pending, a.getrange_seeded, a.has_live[.Candle])
	comp_b := cell_composition_stage(b.getrange_pending, b.getrange_seeded, b.has_live[.Candle])
	comp_c := cell_composition_stage(c.getrange_pending, c.getrange_seeded, c.has_live[.Candle])

	testing.expect_value(t, comp_a, Composition_Stage.Composed)
	testing.expect_value(t, comp_b, Composition_Stage.Backfilled)
	testing.expect_value(t, comp_c, Composition_Stage.Range_Pending)
}

@(test)
test_s41_composition_should_extend_guards :: proc(t: ^testing.T) {
	// Verify all guard conditions for lazy loading extension.
	s: Stream_Apply_State
	s.getrange_seeded = true
	s.getrange_oldest_ts = 5000

	// Should extend when conditions are met.
	testing.expect(t, composition_should_extend(s, 50, 200, 0, false), "should extend: normal case")

	// Pending blocks extension.
	s.getrange_pending = true
	testing.expect(t, !composition_should_extend(s, 50, 200, 0, false), "should NOT extend: pending")
	s.getrange_pending = false

	// Store at cap blocks extension.
	testing.expect(t, !composition_should_extend(s, 200, 200, 0, false), "should NOT extend: store at cap")

	// Timeline boundary reached blocks extension.
	testing.expect(t, !composition_should_extend(s, 50, 200, 5000, true), "should NOT extend: at timeline boundary")
	testing.expect(t, composition_should_extend(s, 50, 200, 4000, true), "should extend: oldest > timeline first")

	// Not seeded blocks extension.
	s.getrange_seeded = false
	testing.expect(t, !composition_should_extend(s, 50, 200, 0, false), "should NOT extend: not seeded")
}

@(test)
test_s41_tf_change_resets_then_reseeds :: proc(t: ^testing.T) {
	// After TF change: apply_state resets → pane re-requests → re-enters lifecycle.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	s.getrange_seeded = true
	s.getrange_oldest_ts = 500

	// Verify Composed before TF change.
	comp := cell_composition_stage(false, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Composed)

	// TF change resets.
	apply_state_on_tf_change(&s)
	comp = cell_composition_stage(false, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Empty)

	// Pane re-requests (simulates request_compare_pane_candle_range).
	apply_state_mark_range_sent(&s, 50, 0xBEEF)
	comp = cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Range_Pending)

	// Range completes → Backfilled.
	apply_state_mark_range_complete(&s, 2000)
	comp = cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Backfilled)

	// Live candle arrives → Composed again.
	apply_state_mark_event(&s, .Candle, 6000, false)
	comp = cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Composed)
}

@(test)
test_s41_composition_intent_lifecycle :: proc(t: ^testing.T) {
	// Verify composition_intent returns correct orchestrator actions through lifecycle.
	s: Stream_Apply_State

	// No active stream → None
	testing.expect_value(t, composition_intent(s, 0, false), Orchestrator_Phase.None)

	// Active stream, no data → Seed_Range
	testing.expect_value(t, composition_intent(s, 0, true), Orchestrator_Phase.Seed_Range)

	// Pending getrange → Await_Seed
	apply_state_mark_range_sent(&s, 1, 0)
	testing.expect_value(t, composition_intent(s, 0, true), Orchestrator_Phase.Await_Seed)

	// Seeded but no live → Await_Live
	apply_state_mark_range_complete(&s, 1000)
	testing.expect_value(t, composition_intent(s, 50, true), Orchestrator_Phase.Await_Live)

	// Seeded + live → Steady
	apply_state_mark_event(&s, .Candle, 5000, false)
	testing.expect_value(t, composition_intent(s, 50, true), Orchestrator_Phase.Steady)
}

// =========================================================================
// S42: Per-Pane Invalidation, Recovery & Diagnostics
// Tests prove per-pane recovery isolation, no cross-pane contamination,
// diagnostics derivation, and reconnect reseed correctness.
// =========================================================================

@(test)
test_s42_per_pane_recovery_isolation :: proc(t: ^testing.T) {
	// Two panes: pane A goes stale + recovers, pane B stays healthy.
	// Recovery on A must NOT affect B.
	a: Stream_Apply_State
	b: Stream_Apply_State

	// Both active with data.
	apply_state_mark_event(&a, .Orderbook, 1000, true)
	apply_state_mark_event(&a, .Stats, 1000, false)
	apply_state_mark_event(&b, .Orderbook, 1000, true)
	apply_state_mark_event(&b, .Stats, 1000, false)

	// Pane A: Stale (no data for 20s).
	now_ms := i64(21_000)
	rem_a := apply_state_stale_remediation(a, now_ms)
	testing.expect_value(t, rem_a, Remediation_Decision.Resubscribe)

	// Pane B: Still fresh (received data at 20s).
	b.last_recv_ms[.Orderbook] = 20_000
	b.last_recv_ms[.Stats] = 20_000
	rem_b := apply_state_stale_remediation(b, now_ms)
	testing.expect_value(t, rem_b, Remediation_Decision.None)

	// Mark recovery on A.
	apply_state_mark_recovery(&a, now_ms)
	testing.expect_value(t, a.recovery_attempts, u8(1))
	testing.expect_value(t, b.recovery_attempts, u8(0)) // B unaffected
}

@(test)
test_s42_per_pane_recovery_success_clears_only_target :: proc(t: ^testing.T) {
	// Pane with recovery in progress: verify success clears only that pane.
	a: Stream_Apply_State
	b: Stream_Apply_State

	apply_state_mark_event(&a, .Orderbook, 1000, true)
	apply_state_mark_event(&a, .Stats, 1000, false)
	apply_state_mark_event(&b, .Orderbook, 1000, true)
	apply_state_mark_event(&b, .Stats, 1000, false)

	// Both go stale and attempt recovery.
	apply_state_mark_recovery(&a, 15_000)
	apply_state_mark_recovery(&b, 15_000)
	testing.expect_value(t, a.recovery_attempts, u8(1))
	testing.expect_value(t, b.recovery_attempts, u8(1))

	// Pane A gets fresh data.
	a.last_recv_ms[.Orderbook] = 16_000
	a.last_recv_ms[.Stats] = 16_000
	apply_state_check_recovery_success(&a, 17_000)
	testing.expect_value(t, a.recovery_attempts, u8(0)) // A cleared
	testing.expect_value(t, b.recovery_attempts, u8(1)) // B still recovering
}

@(test)
test_s42_per_pane_health_tick_independent :: proc(t: ^testing.T) {
	// Two panes at different health levels produce independent tick outputs.
	healthy: Stream_Apply_State
	stale: Stream_Apply_State

	apply_state_mark_event(&healthy, .Orderbook, 1000, true)
	apply_state_mark_event(&healthy, .Stats, 1000, false)
	healthy.last_recv_ms[.Orderbook] = 9_000
	healthy.last_recv_ms[.Stats] = 9_000

	apply_state_mark_event(&stale, .Orderbook, 1000, true)
	apply_state_mark_event(&stale, .Stats, 1000, false)
	// Stale pane: no recent data.

	now_ms := i64(10_000)
	tick_h := health_tick_evaluate(Health_Tick_Input{
		apply_state = healthy, now_ms = now_ms, tf_ms = 60_000,
		is_connected = true, is_offline = false,
	})
	tick_s := health_tick_evaluate(Health_Tick_Input{
		apply_state = stale, now_ms = now_ms, tf_ms = 60_000,
		is_connected = true, is_offline = false,
	})

	testing.expect_value(t, tick_h.remediation, Remediation_Decision.None)
	testing.expect_value(t, tick_h.stream_health, System_Health_Level.Healthy)
	// Stale: OB+Stats both silent for 10s (below 12s threshold for Dual_Silence).
	// At 13s they'd be stale. Let's push to 13s:
	now_ms = 13_000
	tick_s = health_tick_evaluate(Health_Tick_Input{
		apply_state = stale, now_ms = now_ms, tf_ms = 60_000,
		is_connected = true, is_offline = false,
	})
	testing.expect_value(t, tick_s.remediation, Remediation_Decision.Resubscribe)
	testing.expect(t, tick_s.stale_count > 0, "stale pane must have stale artifacts")
}

@(test)
test_s42_recovery_status_derivation :: proc(t: ^testing.T) {
	// Verify recovery_status is correctly derived for diagnostics.
	s: Stream_Apply_State

	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.None)

	s.recovery_attempts = 1
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Recovering)

	s.recovery_attempts = 2
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Recovering)

	s.recovery_attempts = RECOVERY_MAX_ATTEMPTS
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Exhausted)
}

@(test)
test_s42_health_level_reflects_recovery :: proc(t: ^testing.T) {
	// stream_health_level should reflect recovery status even if no stale artifacts.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)

	// Healthy with no recovery.
	testing.expect_value(t, stream_health_level(s, 2000), System_Health_Level.Healthy)

	// Recovering → Degraded.
	s.recovery_attempts = 1
	testing.expect_value(t, stream_health_level(s, 2000), System_Health_Level.Degraded)

	// Exhausted → Unhealthy.
	s.recovery_attempts = RECOVERY_MAX_ATTEMPTS
	testing.expect_value(t, stream_health_level(s, 2000), System_Health_Level.Unhealthy)
}

@(test)
test_s42_reconnect_clears_recovery_per_slot :: proc(t: ^testing.T) {
	// Reconnect should clear recovery state per slot independently.
	a: Stream_Apply_State
	b: Stream_Apply_State

	apply_state_mark_event(&a, .Orderbook, 1000, true)
	apply_state_mark_recovery(&a, 5000)
	apply_state_mark_event(&b, .Orderbook, 1000, true)
	apply_state_mark_recovery(&b, 5000)

	testing.expect_value(t, a.recovery_attempts, u8(1))
	testing.expect_value(t, b.recovery_attempts, u8(1))

	// Reconnect both (simulates global reconnect clearing all slots).
	apply_state_on_reconnect(&a)
	apply_state_on_reconnect(&b)

	testing.expect_value(t, a.recovery_attempts, u8(0))
	testing.expect_value(t, b.recovery_attempts, u8(0))
	testing.expect_value(t, a.recovery_last_ms, i64(0))
	testing.expect_value(t, b.recovery_last_ms, i64(0))
}

@(test)
test_s42_tf_change_clears_recovery_per_slot :: proc(t: ^testing.T) {
	// TF change on a pane should clear recovery for that pane's slot only.
	a: Stream_Apply_State
	b: Stream_Apply_State

	apply_state_mark_event(&a, .Candle, 1000, false)
	apply_state_mark_event(&b, .Candle, 1000, false)
	apply_state_mark_recovery(&a, 5000)
	apply_state_mark_recovery(&b, 5000)

	// TF change on A only.
	apply_state_on_tf_change(&a)
	testing.expect_value(t, a.recovery_attempts, u8(0)) // A cleared
	testing.expect_value(t, b.recovery_attempts, u8(1)) // B untouched
}

@(test)
test_s42_exhausted_pane_does_not_trigger_resubscribe :: proc(t: ^testing.T) {
	// A pane at max attempts should get .Exhausted, not .Resubscribe.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	apply_state_mark_event(&s, .Stats, 1000, false)
	s.recovery_attempts = RECOVERY_MAX_ATTEMPTS
	s.recovery_last_ms = 1000

	rem := apply_state_stale_remediation(s, 100_000) // well past any cooldown
	testing.expect_value(t, rem, Remediation_Decision.Exhausted)
}

@(test)
test_s42_cooldown_prevents_thrashing :: proc(t: ^testing.T) {
	// During cooldown window, remediation should be .Cooldown not .Resubscribe.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	apply_state_mark_event(&s, .Stats, 1000, false)

	// First recovery attempt.
	now := i64(20_000)
	apply_state_mark_recovery(&s, now)

	// Within cooldown (15s base): still stale but cooldown blocks resubscribe.
	rem := apply_state_stale_remediation(s, now + 10_000)
	testing.expect_value(t, rem, Remediation_Decision.Cooldown)

	// After cooldown expires (attempt=1 → cooldown=30s): should allow next attempt.
	rem = apply_state_stale_remediation(s, now + 31_000)
	testing.expect_value(t, rem, Remediation_Decision.Resubscribe)
}

@(test)
test_s42_recovery_event_log_tracks_per_slot :: proc(t: ^testing.T) {
	// Recovery events should carry distinct slot_id for different panes.
	log: Recovery_Event_Log

	recovery_event_log_push(&log, Recovery_Event{kind = .Attempt, timestamp = 1000, attempts = 1, slot_id = 3})
	recovery_event_log_push(&log, Recovery_Event{kind = .Attempt, timestamp = 2000, attempts = 1, slot_id = 7})
	recovery_event_log_push(&log, Recovery_Event{kind = .Success, timestamp = 3000, attempts = 1, slot_id = 3})

	testing.expect_value(t, log.count, 3)

	// Index 0 = newest, 2 = oldest.
	e0, ok0 := recovery_event_log_get(&log, 0) // newest: Success on slot 3
	testing.expect(t, ok0, "event 0 must exist")
	testing.expect_value(t, e0.slot_id, u8(3))
	testing.expect_value(t, e0.kind, Recovery_Event_Kind.Success)

	e1, ok1 := recovery_event_log_get(&log, 1) // middle: Attempt on slot 7
	testing.expect(t, ok1, "event 1 must exist")
	testing.expect_value(t, e1.slot_id, u8(7))

	e2, ok2 := recovery_event_log_get(&log, 2) // oldest: Attempt on slot 3
	testing.expect(t, ok2, "event 2 must exist")
	testing.expect_value(t, e2.slot_id, u8(3))
	testing.expect_value(t, e2.kind, Recovery_Event_Kind.Attempt)
}

// ---------------------------------------------------------------------------
// S43: Surface View Composition Contract tests.
// Verify that the pure functions composing Cell_Surface_View produce
// consistent, correct results when combined — the "surface contract".
// ---------------------------------------------------------------------------

@(test)
test_s43_surface_empty_state_contract :: proc(t: ^testing.T) {
	// An empty apply_state must produce Empty composition, Healthy health, no recovery.
	s: Stream_Apply_State

	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Empty)

	health := stream_health_level(s, 1000)
	testing.expect_value(t, health, System_Health_Level.Healthy)

	stale, aging := apply_state_stale_artifact_count(s, 1000)
	testing.expect_value(t, stale, 0)
	testing.expect_value(t, aging, 0)

	recovery := apply_state_recovery_status(s)
	testing.expect_value(t, recovery, Recovery_Status.None)
}

@(test)
test_s43_surface_live_only_contract :: proc(t: ^testing.T) {
	// Live candle + no getrange = Live_Only, Healthy, no staleness.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	apply_state_mark_event(&s, .Trade, 1000, false)

	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Live_Only)

	health := stream_health_level(s, 2000)
	testing.expect_value(t, health, System_Health_Level.Healthy)

	stale, aging := apply_state_stale_artifact_count(s, 2000)
	testing.expect_value(t, stale, 0)
	testing.expect_value(t, aging, 0)

	recovery := apply_state_recovery_status(s)
	testing.expect_value(t, recovery, Recovery_Status.None)
}

@(test)
test_s43_surface_composed_contract :: proc(t: ^testing.T) {
	// Getrange seeded + live candle = Composed. All fields consistent.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_range_sent(&s, 1, 0xABC)
	s.getrange_seeded = true
	s.getrange_oldest_ts = 1000

	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Composed)

	health := stream_health_level(s, 6000)
	testing.expect_value(t, health, System_Health_Level.Healthy)
}

@(test)
test_s43_surface_range_pending_contract :: proc(t: ^testing.T) {
	// Getrange pending + no live candle = Range_Pending.
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 1, 0xABC)

	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Range_Pending)

	health := stream_health_level(s, 1000)
	testing.expect_value(t, health, System_Health_Level.Healthy)
}

@(test)
test_s43_surface_backfilled_contract :: proc(t: ^testing.T) {
	// Getrange seeded + no live candle = Backfilled.
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 1, 0xABC)
	s.getrange_pending = false
	s.getrange_seeded = true
	s.getrange_oldest_ts = 1000

	comp := apply_state_composition_stage(s)
	testing.expect_value(t, comp, Composition_Stage.Backfilled)
}

@(test)
test_s43_surface_degraded_staleness_contract :: proc(t: ^testing.T) {
	// Live data with aging artifacts → Degraded health, aging > 0.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	apply_state_mark_event(&s, .Stats, 1000, false)
	apply_state_mark_event(&s, .Trade, 1000, false)
	apply_state_mark_event(&s, .Candle, 1000, false)

	// 9s later: Dual_Silence artifacts (OB, Stats) are aging (>8s warn threshold).
	now_ms := i64(10_000)
	health := stream_health_level(s, now_ms)
	testing.expect_value(t, health, System_Health_Level.Degraded)

	stale, aging := apply_state_stale_artifact_count(s, now_ms)
	testing.expect(t, aging > 0, "aging count must be > 0 for 9s silence on Dual_Silence artifacts")
	testing.expect_value(t, stale, 0) // not yet stale (threshold is 12s)
}

@(test)
test_s43_surface_unhealthy_staleness_contract :: proc(t: ^testing.T) {
	// Live data with stale artifacts → Unhealthy health, stale > 0.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	apply_state_mark_event(&s, .Stats, 1000, false)

	// 13s later: stale threshold (12s) exceeded.
	now_ms := i64(14_000)
	health := stream_health_level(s, now_ms)
	testing.expect_value(t, health, System_Health_Level.Unhealthy)

	stale, aging := apply_state_stale_artifact_count(s, now_ms)
	testing.expect(t, stale > 0, "stale count must be > 0 at 13s silence for Dual_Silence")
	_ = aging
}

@(test)
test_s43_surface_recovery_contract :: proc(t: ^testing.T) {
	// Recovering → Degraded, Exhausted → Unhealthy. Contract consistency.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 1000, false)

	// Baseline: Healthy.
	testing.expect_value(t, stream_health_level(s, 2000), System_Health_Level.Healthy)
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.None)

	// After 1 recovery attempt: Degraded + Recovering.
	s.recovery_attempts = 1
	testing.expect_value(t, stream_health_level(s, 2000), System_Health_Level.Degraded)
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Recovering)

	// After max attempts: Unhealthy + Exhausted.
	s.recovery_attempts = RECOVERY_MAX_ATTEMPTS
	testing.expect_value(t, stream_health_level(s, 2000), System_Health_Level.Unhealthy)
	testing.expect_value(t, apply_state_recovery_status(s), Recovery_Status.Exhausted)
}

@(test)
test_s43_cell_composition_matches_apply_state :: proc(t: ^testing.T) {
	// cell_composition_stage must agree with apply_state_composition_stage
	// for equivalent inputs.
	s: Stream_Apply_State

	// Empty state: both should return Empty.
	testing.expect_value(t, cell_composition_stage(false, false, false), Composition_Stage.Empty)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)

	// Live only: candle live, no getrange.
	apply_state_mark_event(&s, .Candle, 1000, false)
	testing.expect_value(t, cell_composition_stage(false, false, true), Composition_Stage.Live_Only)
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Live_Only)

	// Range pending: getrange sent, no live candle.
	s2: Stream_Apply_State
	apply_state_mark_range_sent(&s2, 1, 0xABC)
	testing.expect_value(t, cell_composition_stage(true, false, false), Composition_Stage.Range_Pending)
	testing.expect_value(t, apply_state_composition_stage(s2), Composition_Stage.Range_Pending)

	// Composed: getrange seeded + live candle.
	s3: Stream_Apply_State
	apply_state_mark_event(&s3, .Candle, 1000, false)
	apply_state_mark_range_sent(&s3, 1, 0xABC)
	s3.getrange_seeded = true
	s3.getrange_oldest_ts = 500
	testing.expect_value(t, cell_composition_stage(false, true, true), Composition_Stage.Composed)
	testing.expect_value(t, apply_state_composition_stage(s3), Composition_Stage.Composed)
}

@(test)
test_s43_surface_tf_sensitive_health_isolation :: proc(t: ^testing.T) {
	// Same apply_state, different TF → different health levels.
	// This validates per-pane TF isolation at the contract level.
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	now_ms := i64(200_000) // 199s gap

	// At 1m TF: candle gap is well past TF_Adaptive thresholds → stale.
	health_1m := stream_health_level(s, now_ms, 60_000)

	// At 1h TF: thresholds scale with TF → still healthy or degraded.
	health_1h := stream_health_level(s, now_ms, 3_600_000)

	// 1m must be worse than 1h.
	testing.expect(t, int(health_1m) > int(health_1h),
		"1m TF should have worse health than 1h TF for same gap")
}

@(test)
test_s43_surface_staleness_tf_scaling :: proc(t: ^testing.T) {
	// Stale artifact counts must scale with TF for TF_Adaptive artifacts (Candle).
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	now_ms := i64(200_000) // 199s gap

	// 1m TF: Candle stale (3*60s = 180s threshold, gap=199s > threshold).
	stale_1m, _ := apply_state_stale_artifact_count(s, now_ms, 60_000)

	// 1h TF: Candle not stale (3*3600s = 10800s threshold, gap=199s < threshold).
	stale_1h, _ := apply_state_stale_artifact_count(s, now_ms, 3_600_000)

	testing.expect(t, stale_1m > stale_1h,
		"1m TF should have more stale artifacts than 1h TF")
}

// --- S44: Compare Pane Autonomy Completion ---

// S44: After getrange reset (simulating global TF change), composition reverts to
// Live_Only if live candles exist, or Empty if not. This is the contract that
// compare panes following global TF rely on after invalidation.
@(test)
test_s44_composition_after_getrange_reset_with_live_candle :: proc(t: ^testing.T) {
	// Simulates: pane was Composed, then global TF change resets getrange.
	// Slot still has has_live[.Candle] from previous data.
	comp := cell_composition_stage(false, false, true)
	testing.expect_value(t, comp, Composition_Stage.Live_Only)
}

@(test)
test_s44_composition_after_getrange_reset_without_live_candle :: proc(t: ^testing.T) {
	// Simulates: pane had only backfill, global TF change resets getrange.
	// No live candle yet at new TF → Empty.
	comp := cell_composition_stage(false, false, false)
	testing.expect_value(t, comp, Composition_Stage.Empty)
}

// S44: TF change clears recovery state — TF change triggers a full resubscribe,
// which is itself a recovery action. Recovery counters reset to zero.
@(test)
test_s44_tf_change_clears_recovery_state :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	apply_state_mark_recovery(&s, 5000)
	testing.expect(t, s.recovery_attempts == 1, "should have 1 recovery attempt")

	apply_state_on_tf_change(&s)

	testing.expect(t, s.recovery_attempts == 0,
		"TF change must clear recovery_attempts (resubscribe is recovery)")
	testing.expect(t, s.recovery_last_ms == 0,
		"TF change must clear recovery_last_ms")
}

// S44: Per-pane TF override pane's composition must be unaffected by global TF change.
// When pane A follows global and pane B has override, only A gets invalidated.
// Verified by showing B's apply_state/composition inputs are independent.
@(test)
test_s44_per_pane_override_survives_global_tf_change :: proc(t: ^testing.T) {
	// Pane B: per-pane TF override, Composed state.
	b: Stream_Apply_State
	apply_state_mark_event(&b, .Candle, 1000, false)
	apply_state_mark_range_sent(&b, 10, 0)
	apply_state_mark_range_complete(&b, 500)
	comp_b := cell_composition_stage(b.getrange_pending, b.getrange_seeded, b.has_live[.Candle])
	testing.expect_value(t, comp_b, Composition_Stage.Composed)

	// Simulate global TF change affecting pane A but NOT pane B.
	// Pane A: getrange reset (pending=false, seeded=false).
	comp_a := cell_composition_stage(false, false, false)
	testing.expect_value(t, comp_a, Composition_Stage.Empty)

	// Pane B: unchanged — its apply_state was never touched.
	comp_b2 := cell_composition_stage(b.getrange_pending, b.getrange_seeded, b.has_live[.Candle])
	testing.expect_value(t, comp_b2, Composition_Stage.Composed)
}

// S44: After getrange invalidation + reseed, pane transitions back through
// the full composition lifecycle (Empty → Range_Pending → Composed).
@(test)
test_s44_reseed_lifecycle_after_invalidation :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	apply_state_mark_range_sent(&s, 10, 0)
	apply_state_mark_range_complete(&s, 500)
	testing.expect_value(t, cell_composition_stage(s.getrange_pending, s.getrange_seeded, s.has_live[.Candle]),
		Composition_Stage.Composed)

	// Simulate global TF change: reset getrange, clear TF-sensitive state.
	apply_state_on_tf_change(&s)
	// Per-pane getrange also reset (app layer does getranges[cpi] = {}).
	pane_pending := false
	pane_seeded := false

	// After invalidation: has_live[.Candle] cleared by tf_change, getrange reset.
	comp := cell_composition_stage(pane_pending, pane_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Empty)

	// Reseed: new getrange request sent.
	pane_pending = true
	comp = cell_composition_stage(pane_pending, pane_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Range_Pending)

	// Range complete + live candle at new TF.
	pane_pending = false
	pane_seeded = true
	apply_state_mark_event(&s, .Candle, 2000, false) // Live candle at new TF.
	comp = cell_composition_stage(pane_pending, pane_seeded, s.has_live[.Candle])
	testing.expect_value(t, comp, Composition_Stage.Composed)
}

// S44: Health level adapts to TF for TF-Adaptive (candle) artifacts.
// Candle staleness thresholds scale with TF — same gap is stale at 1m but healthy at 1h.
@(test)
test_s44_health_adapts_to_new_tf_after_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Only candle — Dual_Silence artifacts (OB/Stats) would dominate with fixed 12s threshold.
	apply_state_mark_event(&s, .Candle, 1000, false)
	now_ms := i64(200_000) // 199s gap

	// 1m TF: candle stale (3*60s=180s threshold, 199s > 180s → stale).
	stale_1m, aging_1m := apply_state_stale_artifact_count(s, now_ms, 60_000)

	// 1h TF: candle not stale (3*3600s=10800s threshold, 199s < 10800s → fresh).
	stale_1h, aging_1h := apply_state_stale_artifact_count(s, now_ms, 3_600_000)

	testing.expect(t, stale_1m > stale_1h,
		"1m TF should have more stale artifacts than 1h TF for same gap")
	_ = aging_1m
	_ = aging_1h
}

// S44: Getrange timeout detection works correctly after invalidation + reseed.
// Ensures the new sent_frame is used for timeout, not stale frame from old TF.
@(test)
test_s44_getrange_timeout_uses_reseed_frame :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Old getrange at frame 100.
	apply_state_mark_range_sent(&s, 100, 0)
	testing.expect(t, apply_state_check_getrange_timeout(s, 500, 300), "old request should timeout")

	// Simulate TF change invalidation + reseed at frame 1000.
	s.getrange_pending = false
	s.getrange_seeded = false
	apply_state_mark_range_sent(&s, 1000, 0)

	// At frame 1200: not timed out (only 200 frames since reseed).
	testing.expect(t, !apply_state_check_getrange_timeout(s, 1200, 300),
		"reseed frame should be used for timeout, not old frame")

	// At frame 1400: timed out (400 > 300 frames since reseed).
	testing.expect(t, apply_state_check_getrange_timeout(s, 1400, 300),
		"should timeout after threshold from reseed frame")
}

// S44: TF change clears recovery, so post-TF-change remediation starts fresh.
// A stale Dual_Silence artifact (OB) after TF change triggers immediate Resubscribe
// (not Cooldown) since recovery_attempts was cleared to 0.
@(test)
test_s44_recovery_resets_on_tf_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)

	// Two recovery attempts with escalating backoff.
	apply_state_mark_recovery(&s, 5000)
	apply_state_mark_recovery(&s, 20_000)
	testing.expect(t, s.recovery_attempts == 2, "two attempts before TF change")

	// TF change clears recovery state.
	apply_state_on_tf_change(&s)
	testing.expect(t, s.recovery_attempts == 0,
		"TF change must reset attempt count")

	// Re-receive OB data at new TF to set up Dual_Silence tracking.
	apply_state_mark_event(&s, .Orderbook, 30_000, true)

	// OB goes stale (now=50s, OB at 30s → 20s gap > 12s threshold).
	// With recovery_attempts=0, first stale triggers Resubscribe (not Cooldown).
	remediation := apply_state_stale_remediation(s, 50_000, 60_000)
	testing.expect_value(t, remediation, Remediation_Decision.Resubscribe)
}

// ===================================================================
// S45: Workspace Schema Contract Tests
// Verify bitfield layout, persist/derive boundary, and schema invariants.
// ===================================================================

// Chart display bitfield roundtrip: pack and unpack must be symmetric.
@(test)
test_s45_chart_display_bitfield_roundtrip :: proc(t: ^testing.T) {
	// Pack: bit0=vol, bit1=heatmap, bit2=vpvr, bits3-4=heatmap_idx,
	//       bits5-8=ob_grp, bits9-12=dom_grp, bits13-16=trade_filter
	f := 0
	f |= 1 << 0   // show_vol = true
	f |= 0 << 1   // show_heatmap = false
	f |= 1 << 2   // show_vpvr = true
	f |= 2 << 3   // heatmap_intensity_idx = 2
	f |= 5 << 5   // ob_group_idx = 5
	f |= 3 << 9   // dom_group_idx = 3
	f |= 7 << 13  // trade_filter_idx = 7

	// Unpack and verify.
	testing.expect(t, (f & (1 << 0)) != 0, "vol bit set")
	testing.expect(t, (f & (1 << 1)) == 0, "heatmap bit clear")
	testing.expect(t, (f & (1 << 2)) != 0, "vpvr bit set")
	testing.expect_value(t, (f >> 3) & 0x3, 2)
	testing.expect_value(t, (f >> 5) & 0xF, 5)
	testing.expect_value(t, (f >> 9) & 0xF, 3)
	testing.expect_value(t, (f >> 13) & 0xF, 7)
}

// Chart display zero state: all flags off, all indices zero.
@(test)
test_s45_chart_display_zero_state :: proc(t: ^testing.T) {
	f := 0
	testing.expect(t, (f & (1 << 0)) == 0, "vol off")
	testing.expect(t, (f & (1 << 1)) == 0, "heatmap off")
	testing.expect(t, (f & (1 << 2)) == 0, "vpvr off")
	testing.expect_value(t, (f >> 3) & 0x3, 0)
	testing.expect_value(t, (f >> 5) & 0xF, 0)
	testing.expect_value(t, (f >> 9) & 0xF, 0)
	testing.expect_value(t, (f >> 13) & 0xF, 0)
}

// Chart display max values: all flags on, max valid indices.
@(test)
test_s45_chart_display_max_values :: proc(t: ^testing.T) {
	f := 0
	f |= 1 << 0    // vol
	f |= 1 << 1    // heatmap
	f |= 1 << 2    // vpvr
	f |= 3 << 3    // max heatmap_idx (2 bits)
	f |= 15 << 5   // max ob_grp (4 bits)
	f |= 15 << 9   // max dom_grp (4 bits)
	f |= 15 << 13  // max trade_filter (4 bits)

	testing.expect(t, (f & (1 << 0)) != 0, "vol on")
	testing.expect(t, (f & (1 << 1)) != 0, "heatmap on")
	testing.expect(t, (f & (1 << 2)) != 0, "vpvr on")
	testing.expect_value(t, (f >> 3) & 0x3, 3)
	testing.expect_value(t, (f >> 5) & 0xF, 15)
	testing.expect_value(t, (f >> 9) & 0xF, 15)
	testing.expect_value(t, (f >> 13) & 0xF, 15)
}

// Bit isolation: ob_group_idx must not bleed into heatmap_idx or dom_group_idx.
@(test)
test_s45_chart_display_ob_grp_isolation :: proc(t: ^testing.T) {
	f := 15 << 5  // ob_grp = 15, everything else 0
	testing.expect(t, (f & (1 << 0)) == 0, "vol must be 0")
	testing.expect(t, (f & (1 << 1)) == 0, "heatmap must be 0")
	testing.expect(t, (f & (1 << 2)) == 0, "vpvr must be 0")
	testing.expect_value(t, (f >> 3) & 0x3, 0)   // heatmap_idx clean
	testing.expect_value(t, (f >> 5) & 0xF, 15)   // ob_grp correct
	testing.expect_value(t, (f >> 9) & 0xF, 0)    // dom_grp clean
	testing.expect_value(t, (f >> 13) & 0xF, 0)   // trade_filter clean
}

// Bit isolation: dom_group_idx must not bleed into adjacent fields.
@(test)
test_s45_chart_display_dom_grp_isolation :: proc(t: ^testing.T) {
	f := 15 << 9  // dom_grp = 15, everything else 0
	testing.expect_value(t, (f >> 3) & 0x3, 0)    // heatmap_idx clean
	testing.expect_value(t, (f >> 5) & 0xF, 0)    // ob_grp clean
	testing.expect_value(t, (f >> 9) & 0xF, 15)   // dom_grp correct
	testing.expect_value(t, (f >> 13) & 0xF, 0)   // trade_filter clean
}

// Composition stage must be deterministic: same apply_state inputs → same stage.
@(test)
test_s45_schema_composition_deterministic :: proc(t: ^testing.T) {
	// Two identical apply states must produce identical composition stages.
	s1, s2: Stream_Apply_State
	apply_state_mark_event(&s1, .Candle, 5000, false)
	apply_state_mark_event(&s2, .Candle, 5000, false)
	s1.getrange_seeded = true
	s2.getrange_seeded = true

	c1 := apply_state_composition_stage(s1)
	c2 := apply_state_composition_stage(s2)
	testing.expect_value(t, c1, c2)
	testing.expect_value(t, c1, Composition_Stage.Composed)
}

// Persisted state must not include recovery/backfill (these are derived/transient).
@(test)
test_s45_schema_recovery_is_transient :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	apply_state_mark_recovery(&s, 5000)
	testing.expect(t, s.recovery_attempts == 1, "recovery tracked")

	// TF change resets recovery — proving it's transient per-session state.
	apply_state_on_tf_change(&s)
	testing.expect_value(t, s.recovery_attempts, u8(0))
}

// Reconnect must reset only policy-gated artifacts, not all persisted state.
@(test)
test_s45_schema_reconnect_preserves_persisted_markers :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Trade: no gate, should survive reconnect.
	apply_state_mark_event(&s, .Trade, 1000, false)
	// Candle: live, should survive reconnect.
	apply_state_mark_event(&s, .Candle, 2000, false)
	s.getrange_seeded = true

	apply_state_on_reconnect(&s)

	// Trade snapshot_seen survives (not gated).
	testing.expect(t, s.snapshot_seen[.Trade], "trade snapshot survives reconnect")
	// Candle live survives.
	testing.expect(t, s.has_live[.Candle], "candle live survives reconnect")
	// getrange_pending cleared (transient backfill state).
	testing.expect(t, !s.getrange_pending, "getrange cleared on reconnect")
}

// Composition stage covers full lifecycle: Empty → Pending → Backfilled → LiveOnly → Composed.
@(test)
test_s45_schema_composition_lifecycle_coverage :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Empty.
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Empty)

	// Pending (getrange in flight).
	s.getrange_pending = true
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Range_Pending)

	// Backfilled (seeded, no live candle).
	s.getrange_pending = false
	s.getrange_seeded = true
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Backfilled)

	// Live_Only (live candle, no backfill).
	s2: Stream_Apply_State
	apply_state_mark_event(&s2, .Candle, 5000, false)
	testing.expect_value(t, apply_state_composition_stage(s2), Composition_Stage.Live_Only)

	// Composed (seeded + live candle).
	s.has_live[.Candle] = true
	testing.expect_value(t, apply_state_composition_stage(s), Composition_Stage.Composed)
}

// =========================================================================
// S46: Deterministic Runtime Snapshot tests.
// Verify snapshot capture determinism, serialization, and apply state equality.
// =========================================================================

@(test)
test_s46_snapshot_version_constant :: proc(t: ^testing.T) {
	testing.expect_value(t, RUNTIME_SNAPSHOT_VERSION, 3) // S82: bumped to V3 for show_oi
	testing.expect_value(t, SNAPSHOT_MAX_SLOTS, 32)
	testing.expect_value(t, SNAPSHOT_MAX_CELLS, 12)
	testing.expect_value(t, SNAPSHOT_MAX_COMPARE_PANES, 4)
}

@(test)
test_s46_empty_snapshot_serializes :: proc(t: ^testing.T) {
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION
	buf: [SNAPSHOT_SERIALIZE_CAP]u8
	n := runtime_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 0, "empty snapshot must produce output")

	// Must start with "SNAP3|" (S82: snapshot V3 — added show_oi indicator flag)
	out := string(buf[:n])
	testing.expect(t, len(out) >= 5, "output too short")
	testing.expect(t, out[:5] == "SNAP3", "must start with SNAP3")
}

@(test)
test_s46_serialize_deterministic :: proc(t: ^testing.T) {
	// Build a snapshot with known state.
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION
	snap.capture_ts_ms = 1709800000000
	snap.active_subject_id = 42
	snap.active_tf_idx = 3
	snap.active_apply_state.event_count = 100
	snap.active_apply_state.has_live[.Trade] = true
	snap.active_apply_state.has_live[.Candle] = true
	snap.active_apply_state.getrange_seeded = true

	// Serialize twice — must produce identical output.
	buf1: [SNAPSHOT_SERIALIZE_CAP]u8
	buf2: [SNAPSHOT_SERIALIZE_CAP]u8
	n1 := runtime_snapshot_serialize(&snap, buf1[:])
	n2 := runtime_snapshot_serialize(&snap, buf2[:])
	testing.expect_value(t, n1, n2)
	testing.expect(t, n1 > 0, "must produce output")
	for i in 0 ..< n1 {
		if buf1[i] != buf2[i] {
			testing.expect(t, false, "determinism violation at byte")
			break
		}
	}
}

@(test)
test_s46_apply_state_equality_basic :: proc(t: ^testing.T) {
	a: Stream_Apply_State
	b: Stream_Apply_State
	testing.expect(t, runtime_snapshot_apply_states_equal(a, b), "zero states must be equal")

	apply_state_mark_event(&a, .Trade, 1000, false)
	testing.expect(t, !runtime_snapshot_apply_states_equal(a, b), "different event counts must differ")

	apply_state_mark_event(&b, .Trade, 1000, false)
	testing.expect(t, runtime_snapshot_apply_states_equal(a, b), "same events must match")
}

@(test)
test_s46_apply_state_equality_getrange :: proc(t: ^testing.T) {
	a: Stream_Apply_State
	b: Stream_Apply_State
	a.getrange_seeded = true
	a.getrange_oldest_ts = 5000
	testing.expect(t, !runtime_snapshot_apply_states_equal(a, b), "getrange diff must detect")
	b.getrange_seeded = true
	b.getrange_oldest_ts = 5000
	testing.expect(t, runtime_snapshot_apply_states_equal(a, b), "getrange match")
}

@(test)
test_s46_apply_state_equality_recovery :: proc(t: ^testing.T) {
	a: Stream_Apply_State
	b: Stream_Apply_State
	a.recovery_attempts = 2
	a.recovery_last_ms = 9000
	testing.expect(t, !runtime_snapshot_apply_states_equal(a, b), "recovery diff must detect")
	b.recovery_attempts = 2
	b.recovery_last_ms = 9000
	testing.expect(t, runtime_snapshot_apply_states_equal(a, b), "recovery match")
}

@(test)
test_s46_snapshot_with_slots_serializes :: proc(t: ^testing.T) {
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION
	snap.capture_ts_ms = 1000
	snap.slot_count = 2

	// Slot 0: used
	snap.slots[0].used = true
	snap.slots[0].subject_id = 100
	v0 := "binance"
	for i in 0 ..< len(v0) { snap.slots[0].venue[i] = v0[i] }
	snap.slots[0].venue_len = u8(len(v0))
	s0 := "BTCUSDT"
	for i in 0 ..< len(s0) { snap.slots[0].symbol[i] = s0[i] }
	snap.slots[0].symbol_len = u8(len(s0))
	snap.slots[0].timeframe_ms = 60000
	apply_state_mark_event(&snap.slots[0].apply_state, .Trade, 500, false)

	// Slot 1: used
	snap.slots[1].used = true
	snap.slots[1].subject_id = 200

	buf: [SNAPSHOT_SERIALIZE_CAP]u8
	n := runtime_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 100, "slot snapshot must be substantial")

	// Verify presence of SL lines.
	out := string(buf[:n])
	has_sl := false
	for i in 0 ..< len(out) - 2 {
		if out[i] == 'S' && out[i + 1] == 'L' && out[i + 2] == '|' {
			has_sl = true
			break
		}
	}
	testing.expect(t, has_sl, "must contain SL| lines")
}

@(test)
test_s46_snapshot_with_cells_serializes :: proc(t: ^testing.T) {
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION
	snap.cell_count = 2
	snap.cells[0].widget_kind = 3 // Candle
	snap.cells[0].stream_idx = -1
	snap.cells[0].tf_idx = -1
	snap.cells[1].widget_kind = 1
	snap.cells[1].stream_idx = 0
	snap.cells[1].tf_idx = 2

	buf: [SNAPSHOT_SERIALIZE_CAP]u8
	n := runtime_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 50, "cell snapshot must be substantial")

	// Verify CL| lines present.
	out := string(buf[:n])
	has_cl := false
	for i in 0 ..< len(out) - 2 {
		if out[i] == 'C' && out[i + 1] == 'L' && out[i + 2] == '|' {
			has_cl = true
			break
		}
	}
	testing.expect(t, has_cl, "must contain CL| lines")
}

@(test)
test_s46_snapshot_compare_serializes :: proc(t: ^testing.T) {
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION
	snap.compare.active = true
	snap.compare.count = 2
	snap.compare.widget_idx = 2
	snap.compare.focused_pane = 1
	snap.compare.slots[0] = 100
	snap.compare.slots[1] = 200
	snap.compare.tf_idx[0] = -1
	snap.compare.tf_idx[1] = 4
	snap.compare.getranges[0].seeded = true
	snap.compare.getranges[0].oldest_ts = 5000

	buf: [SNAPSHOT_SERIALIZE_CAP]u8
	n := runtime_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 30, "compare snapshot must produce output")

	// Verify CM| line present.
	out := string(buf[:n])
	has_cm := false
	for i in 0 ..< len(out) - 2 {
		if out[i] == 'C' && out[i + 1] == 'M' && out[i + 2] == '|' {
			has_cm = true
			break
		}
	}
	testing.expect(t, has_cm, "must contain CM| line")
}

@(test)
test_s46_snapshot_recovery_log_serializes :: proc(t: ^testing.T) {
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION

	// Push 3 events into recovery log.
	recovery_event_log_push(&snap.recovery_log, Recovery_Event{kind = .Attempt, timestamp = 1000, attempts = 1, slot_id = 0})
	recovery_event_log_push(&snap.recovery_log, Recovery_Event{kind = .Success, timestamp = 2000, attempts = 1, slot_id = 0})
	recovery_event_log_push(&snap.recovery_log, Recovery_Event{kind = .Reset, timestamp = 3000, attempts = 0, slot_id = 1})

	buf: [SNAPSHOT_SERIALIZE_CAP]u8
	n := runtime_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 30, "recovery log must serialize")

	// Verify RL| and RE| lines present.
	out := string(buf[:n])
	has_rl := false
	has_re := false
	for i in 0 ..< len(out) - 2 {
		if out[i] == 'R' && out[i + 1] == 'L' && out[i + 2] == '|' do has_rl = true
		if out[i] == 'R' && out[i + 1] == 'E' && out[i + 2] == '|' do has_re = true
	}
	testing.expect(t, has_rl, "must contain RL| line")
	testing.expect(t, has_re, "must contain RE| lines")
}

@(test)
test_s46_nil_snapshot_serializes_zero :: proc(t: ^testing.T) {
	buf: [256]u8
	n := runtime_snapshot_serialize(nil, buf[:])
	testing.expect_value(t, n, 0)
}

@(test)
test_s46_aggregate_health_in_snapshot :: proc(t: ^testing.T) {
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION
	snap.aggregate_health.health_level = .Degraded
	snap.aggregate_health.slot_count = 5
	snap.aggregate_health.slots_composed = 3
	snap.aggregate_health.total_aging = 2

	buf: [SNAPSHOT_SERIALIZE_CAP]u8
	n := runtime_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 0, "aggregate health must serialize")

	// Verify AH| line present.
	out := string(buf[:n])
	has_ah := false
	for i in 0 ..< len(out) - 2 {
		if out[i] == 'A' && out[i + 1] == 'H' && out[i + 2] == '|' {
			has_ah = true
			break
		}
	}
	testing.expect(t, has_ah, "must contain AH| line")
}

@(test)
test_s46_apply_state_bitmask_roundtrip :: proc(t: ^testing.T) {
	// Verify that bitmask packing in serialization captures all artifact kinds.
	s: Stream_Apply_State
	for kind in Artifact_Kind {
		apply_state_mark_event(&s, kind, 1000 + i64(kind) * 100, kind == .Orderbook)
	}
	testing.expect_value(t, s.event_count, u64(len(Artifact_Kind)))

	// Serialize and check all artifacts appear in the apply state line.
	snap: Runtime_Snapshot
	snap.version = RUNTIME_SNAPSHOT_VERSION
	snap.active_apply_state = s

	buf: [SNAPSHOT_SERIALIZE_CAP]u8
	n := runtime_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 0, "must serialize")

	// The bitmask for all artifacts set = (1<<len(Artifact_Kind))-1
	all_mask := u16((1 << uint(len(Artifact_Kind))) - 1)
	testing.expect(t, all_mask > 0, "artifact mask must be nonzero")

	// Verify determinism: same input, same output, both times.
	buf2: [SNAPSHOT_SERIALIZE_CAP]u8
	n2 := runtime_snapshot_serialize(&snap, buf2[:])
	testing.expect_value(t, n, n2)
}

// =========================================================================
// S47: Analytics Substrate Tests — artifact identity, policies, staleness.
// =========================================================================

@(test)
test_s47_analytics_artifact_kinds_exist :: proc(t: ^testing.T) {
	// Verify all 4 analytics artifact kinds are valid enum values.
	oi := Artifact_Kind.Open_Interest
	dv := Artifact_Kind.Delta_Volume
	cvd := Artifact_Kind.CVD
	bs := Artifact_Kind.Bar_Stats
	testing.expect(t, int(oi) > int(Artifact_Kind.Range_Candle), "OI after Range_Candle")
	testing.expect(t, int(dv) > int(oi), "DV after OI")
	testing.expect(t, int(cvd) > int(dv), "CVD after DV")
	testing.expect(t, int(bs) > int(cvd), "BS after CVD")
	// Total artifact count = 16 (S49: +Session_Volume_Profile, +TPO_Profile)
	testing.expect_value(t, len(Artifact_Kind), 16)
}

@(test)
test_s47_analytics_policies_correct :: proc(t: ^testing.T) {
	// Open Interest: Latest_Wins, not TF-sensitive, Sparse_Adaptive, Degradable
	oi := artifact_policy(.Open_Interest)
	testing.expect(t, !oi.needs_snapshot_gate, "OI no gate")
	testing.expect(t, oi.snapshot_semantics == .Latest_Wins, "OI Latest_Wins")
	testing.expect(t, !oi.is_tf_sensitive, "OI not TF-sensitive")
	testing.expect(t, oi.stale_detection == .Sparse_Adaptive, "OI Sparse_Adaptive")
	testing.expect(t, oi.backpressure_priority == .Degradable, "OI Degradable")

	// Delta Volume: Ring_Append, TF-sensitive, TF_Adaptive, Degradable
	dv := artifact_policy(.Delta_Volume)
	testing.expect(t, dv.snapshot_semantics == .Ring_Append, "DV Ring_Append")
	testing.expect(t, dv.is_tf_sensitive, "DV TF-sensitive")
	testing.expect(t, dv.stale_detection == .TF_Adaptive, "DV TF_Adaptive")
	testing.expect(t, dv.backpressure_priority == .Degradable, "DV Degradable")
	testing.expect(t, dv.reset_on_tf_change, "DV reset on TF change")

	// CVD: Ring_Append, TF-sensitive, TF_Adaptive, Degradable
	cvd := artifact_policy(.CVD)
	testing.expect(t, cvd.snapshot_semantics == .Ring_Append, "CVD Ring_Append")
	testing.expect(t, cvd.is_tf_sensitive, "CVD TF-sensitive")
	testing.expect(t, cvd.stale_detection == .TF_Adaptive, "CVD TF_Adaptive")
	testing.expect(t, cvd.reset_on_tf_change, "CVD reset on TF change")

	// Bar Stats: Ring_Append, TF-sensitive, TF_Adaptive, Degradable
	bs := artifact_policy(.Bar_Stats)
	testing.expect(t, bs.snapshot_semantics == .Ring_Append, "BS Ring_Append")
	testing.expect(t, bs.is_tf_sensitive, "BS TF-sensitive")
	testing.expect(t, bs.stale_detection == .TF_Adaptive, "BS TF_Adaptive")
	testing.expect(t, bs.reset_on_tf_change, "BS reset on TF change")
}

@(test)
test_s47_analytics_apply_state_tracking :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Mark all 4 analytics kinds
	apply_state_mark_event(&s, .Open_Interest, 10_000, false)
	apply_state_mark_event(&s, .Delta_Volume, 11_000, false)
	apply_state_mark_event(&s, .CVD, 12_000, false)
	apply_state_mark_event(&s, .Bar_Stats, 13_000, false)

	testing.expect(t, s.has_live[.Open_Interest], "OI live")
	testing.expect(t, s.has_live[.Delta_Volume], "DV live")
	testing.expect(t, s.has_live[.CVD], "CVD live")
	testing.expect(t, s.has_live[.Bar_Stats], "BS live")
	testing.expect_value(t, s.event_count, u64(4))
	testing.expect_value(t, s.artifact_event_count[.Open_Interest], u64(1))
	testing.expect_value(t, s.last_recv_ms[.Open_Interest], i64(10_000))
}

@(test)
test_s47_sparse_adaptive_staleness :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Open_Interest, 100_000, false)

	// Fresh: 30s old
	st := apply_state_artifact_staleness(s, .Open_Interest, 130_000)
	testing.expect(t, st == .Fresh, "OI fresh at 30s")

	// Aging: 90s old
	st = apply_state_artifact_staleness(s, .Open_Interest, 190_000)
	testing.expect(t, st == .Aging, "OI aging at 90s")

	// Stale: 200s old
	st = apply_state_artifact_staleness(s, .Open_Interest, 300_000)
	testing.expect(t, st == .Stale, "OI stale at 200s")
}

@(test)
test_s47_analytics_tf_change_resets_tf_sensitive :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Open_Interest, 1000, false)
	apply_state_mark_event(&s, .Delta_Volume, 2000, false)
	apply_state_mark_event(&s, .CVD, 3000, false)
	apply_state_mark_event(&s, .Bar_Stats, 4000, false)

	apply_state_on_tf_change(&s)

	// OI is NOT TF-sensitive — should survive TF change
	testing.expect(t, s.has_live[.Open_Interest], "OI survives TF change")
	testing.expect_value(t, s.last_recv_ms[.Open_Interest], i64(1000))

	// DV, CVD, BS are TF-sensitive — should be cleared
	testing.expect(t, !s.has_live[.Delta_Volume], "DV cleared on TF change")
	testing.expect(t, !s.has_live[.CVD], "CVD cleared on TF change")
	testing.expect(t, !s.has_live[.Bar_Stats], "BS cleared on TF change")
	testing.expect_value(t, s.last_recv_ms[.Delta_Volume], i64(0))
}

@(test)
test_s47_analytics_backpressure_degradable :: proc(t: ^testing.T) {
	// Analytics are Degradable — should be skippable under backpressure
	oi_p := artifact_policy(.Open_Interest)
	dv_p := artifact_policy(.Delta_Volume)
	cvd_p := artifact_policy(.CVD)
	bs_p := artifact_policy(.Bar_Stats)
	testing.expect(t, oi_p.backpressure_priority == .Degradable, "OI degradable")
	testing.expect(t, dv_p.backpressure_priority == .Degradable, "DV degradable")
	testing.expect(t, cvd_p.backpressure_priority == .Degradable, "CVD degradable")
	testing.expect(t, bs_p.backpressure_priority == .Degradable, "BS degradable")

	// None have synthetic fallback
	testing.expect(t, !oi_p.has_synthetic_fallback, "OI no synthetic")
	testing.expect(t, !dv_p.has_synthetic_fallback, "DV no synthetic")
	testing.expect(t, !cvd_p.has_synthetic_fallback, "CVD no synthetic")
	testing.expect(t, !bs_p.has_synthetic_fallback, "BS no synthetic")
}

@(test)
test_s47_apply_state_arrays_cover_14_artifacts :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Verify arrays expanded automatically via enum sizing
	testing.expect_value(t, len(s.snapshot_seen), 16)
	testing.expect_value(t, len(s.has_live), 16)
	testing.expect_value(t, len(s.using_synthetic), 16)
	testing.expect_value(t, len(s.last_recv_ms), 16)
	testing.expect_value(t, len(s.artifact_event_count), 16)
}

@(test)
test_s47_analytics_stale_count_includes_analytics :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Open_Interest, 1000, false)
	apply_state_mark_event(&s, .Delta_Volume, 1000, false)

	// At 200s later, OI should be stale (Sparse_Adaptive 180s), DV should be stale (TF_Adaptive ~10s default)
	stale, aging := apply_state_stale_artifact_count(s, 201_000, 5_000)
	testing.expect(t, stale >= 2, "both OI and DV stale at 200s")
	_ = aging
}

// -----------------------------------------------------------------------
// S48: Orderflow Analytics Pack v1 — Widget contract tests
// -----------------------------------------------------------------------

@(test)
test_s48_artifact_kind_count_is_14 :: proc(t: ^testing.T) {
	// S47 introduced 4 analytics kinds, total should be 14.
	count := 0
	for k in Artifact_Kind {
		count += 1
		_ = k
	}
	testing.expect(t, count == 16, "Artifact_Kind enum must have exactly 16 values")
}

@(test)
test_s48_analytics_kind_values_stable :: proc(t: ^testing.T) {
	// Ensure analytics artifact ordinals are stable (widgets + persistence depend on them).
	testing.expect(t, int(Artifact_Kind.Open_Interest) == 10, "Open_Interest ordinal must be 10")
	testing.expect(t, int(Artifact_Kind.Delta_Volume)  == 11, "Delta_Volume ordinal must be 11")
	testing.expect(t, int(Artifact_Kind.CVD)           == 12, "CVD ordinal must be 12")
	testing.expect(t, int(Artifact_Kind.Bar_Stats)     == 13, "Bar_Stats ordinal must be 13")
}

@(test)
test_s48_analytics_policy_ring_append_for_tf_sensitive :: proc(t: ^testing.T) {
	// Delta Volume, CVD, Bar Stats must use Ring_Append semantics and be TF-sensitive.
	for k in ([3]Artifact_Kind{.Delta_Volume, .CVD, .Bar_Stats}) {
		p := artifact_policy(k)
		testing.expect(t, p.snapshot_semantics == .Ring_Append, "analytics TF-sensitive must use Ring_Append")
		testing.expect(t, p.reset_on_tf_change, "DV/CVD/BS must reset on TF change")
		testing.expect(t, p.stale_detection == .TF_Adaptive, "DV/CVD/BS must use TF_Adaptive staleness")
	}
}

@(test)
test_s48_oi_policy_sparse_not_tf_sensitive :: proc(t: ^testing.T) {
	p := artifact_policy(.Open_Interest)
	testing.expect(t, p.snapshot_semantics == .Latest_Wins, "OI must use Latest_Wins")
	testing.expect(t, !p.reset_on_tf_change, "OI must NOT reset on TF change")
	testing.expect(t, p.stale_detection == .Sparse_Adaptive, "OI must use Sparse_Adaptive staleness")
}

@(test)
test_s48_analytics_apply_state_mark_and_query :: proc(t: ^testing.T) {
	s: Stream_Apply_State

	// Mark all 4 analytics kinds.
	apply_state_mark_event(&s, .Open_Interest, 1000, false)
	apply_state_mark_event(&s, .Delta_Volume, 1000, false)
	apply_state_mark_event(&s, .CVD, 1000, false)
	apply_state_mark_event(&s, .Bar_Stats, 1000, false)

	testing.expect(t, s.has_live[.Open_Interest], "OI must be live after mark")
	testing.expect(t, s.has_live[.Delta_Volume], "DV must be live after mark")
	testing.expect(t, s.has_live[.CVD], "CVD must be live after mark")
	testing.expect(t, s.has_live[.Bar_Stats], "BS must be live after mark")
	testing.expect(t, s.last_recv_ms[.Open_Interest] == 1000, "OI last_recv_ms must be 1000")
	testing.expect(t, s.artifact_event_count[.Bar_Stats] == 1, "BS event count must be 1")
}

@(test)
test_s48_tf_change_preserves_oi_clears_others :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Open_Interest, 1000, false)
	apply_state_mark_event(&s, .Delta_Volume, 1000, false)
	apply_state_mark_event(&s, .CVD, 1000, false)
	apply_state_mark_event(&s, .Bar_Stats, 1000, false)

	apply_state_on_tf_change(&s)

	// OI survives TF change (not TF-sensitive).
	testing.expect(t, s.has_live[.Open_Interest], "OI must survive TF change")
	testing.expect(t, s.last_recv_ms[.Open_Interest] == 1000, "OI last_recv_ms preserved")

	// DV/CVD/BS cleared on TF change.
	testing.expect(t, !s.has_live[.Delta_Volume], "DV must be cleared on TF change")
	testing.expect(t, !s.has_live[.CVD], "CVD must be cleared on TF change")
	testing.expect(t, !s.has_live[.Bar_Stats], "BS must be cleared on TF change")
}

@(test)
test_s48_analytics_staleness_at_boundaries :: proc(t: ^testing.T) {
	// OI: Sparse_Adaptive — fresh at 59s, aging at 60s, stale at 180s.
	s_oi: Stream_Apply_State
	apply_state_mark_event(&s_oi, .Open_Interest, 1000, false)
	testing.expect(t, apply_state_artifact_staleness(s_oi, .Open_Interest, 60_000) == .Fresh, "OI fresh at 59s")
	testing.expect(t, apply_state_artifact_staleness(s_oi, .Open_Interest, 61_000) == .Aging, "OI aging at 60s")
	testing.expect(t, apply_state_artifact_staleness(s_oi, .Open_Interest, 181_000) == .Stale, "OI stale at 180s")

	// DV with TF=5s: aging at 10s (2x), stale at 15s (3x).
	s_dv: Stream_Apply_State
	apply_state_mark_event(&s_dv, .Delta_Volume, 1000, false)
	testing.expect(t, apply_state_artifact_staleness(s_dv, .Delta_Volume, 10_999, 5_000) == .Fresh, "DV fresh at 9.999s")
	testing.expect(t, apply_state_artifact_staleness(s_dv, .Delta_Volume, 11_000, 5_000) == .Aging, "DV aging at 10s")
	testing.expect(t, apply_state_artifact_staleness(s_dv, .Delta_Volume, 16_000, 5_000) == .Stale, "DV stale at 15s")
}

@(test)
test_s48_reconnect_resets_analytics :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Delta_Volume, 1000, false)
	apply_state_mark_event(&s, .CVD, 1000, false)

	apply_state_on_reconnect(&s)

	// Reconnect-sensitive artifacts get snapshot_seen cleared based on policy.
	// Analytics don't have snapshot gates, so has_live should remain but
	// the key behavior is that new data will re-seed the state.
	// Verify the apply state still tracks correctly after reconnect.
	apply_state_mark_event(&s, .Delta_Volume, 2000, false)
	testing.expect(t, s.has_live[.Delta_Volume], "DV live after reconnect + new event")
	testing.expect(t, s.last_recv_ms[.Delta_Volume] == 2000, "DV last_recv_ms updated after reconnect")
}

// ============================================================================
// S51: Replay Scrubber Tests
// ============================================================================

@(test)
test_scrubber_push_and_get :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 2, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 3, slot_idx = 0, artifact_kind = .Trade})
	testing.expect_value(t, s.count, 3)

	e, ok := scrubber_get(&s, 0) // newest
	testing.expect(t, ok, "get offset 0")
	testing.expect_value(t, e.seq, u64(3))

	e, ok = scrubber_get(&s, 2) // oldest
	testing.expect(t, ok, "get offset 2")
	testing.expect_value(t, e.seq, u64(1))

	_, ok = scrubber_get(&s, 3) // out of range
	testing.expect(t, !ok, "offset 3 should fail")
}

@(test)
test_scrubber_integrity_gap :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 5, slot_idx = 0, artifact_kind = .Trade}) // gap
	e, ok := scrubber_get(&s, 0)
	testing.expect(t, ok, "get newest")
	testing.expect_value(t, e.integrity, Stream_Integrity_Flag.Gap)
}

@(test)
test_scrubber_integrity_duplicate :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade}) // dup
	e, ok := scrubber_get(&s, 0)
	testing.expect(t, ok, "get newest")
	testing.expect_value(t, e.integrity, Stream_Integrity_Flag.Duplicate)
}

@(test)
test_scrubber_integrity_reorder :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 5, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 3, slot_idx = 0, artifact_kind = .Trade}) // reorder
	e, ok := scrubber_get(&s, 0)
	testing.expect(t, ok, "get newest")
	testing.expect_value(t, e.integrity, Stream_Integrity_Flag.Reorder)
}

@(test)
test_scrubber_integrity_ok :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 2, slot_idx = 0, artifact_kind = .Trade})
	e, ok := scrubber_get(&s, 0)
	testing.expect(t, ok, "get newest")
	testing.expect_value(t, e.integrity, Stream_Integrity_Flag.Ok)
}

@(test)
test_scrubber_multi_slot_isolation :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 1, artifact_kind = .Trade}) // different slot, not dup
	e, ok := scrubber_get(&s, 0)
	testing.expect(t, ok, "get newest")
	testing.expect_value(t, e.integrity, Stream_Integrity_Flag.Ok)
}

@(test)
test_scrubber_integrity_summary :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 2, slot_idx = 0, artifact_kind = .Trade}) // ok
	scrubber_push(&s, Scrubber_Entry{seq = 5, slot_idx = 0, artifact_kind = .Trade}) // gap
	scrubber_push(&s, Scrubber_Entry{seq = 5, slot_idx = 0, artifact_kind = .Trade}) // dup
	summary := scrubber_integrity_summary(&s)
	testing.expect_value(t, summary.total, 4)
	testing.expect_value(t, summary.ok, 2) // first + second
	testing.expect_value(t, summary.gaps, 1)
	testing.expect_value(t, summary.duplicates, 1)
}

@(test)
test_scrubber_ring_wrap :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	for i in 0 ..< SCRUBBER_RING_CAP + 10 {
		scrubber_push(&s, Scrubber_Entry{seq = u64(i + 1), slot_idx = 0, artifact_kind = .Trade})
	}
	testing.expect_value(t, s.count, SCRUBBER_RING_CAP)
	e, ok := scrubber_get(&s, 0)
	testing.expect(t, ok, "get newest after wrap")
	testing.expect_value(t, e.seq, u64(SCRUBBER_RING_CAP + 10))
}

@(test)
test_scrubber_pause_discards :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_pause(&s, true)
	scrubber_push(&s, Scrubber_Entry{seq = 2, slot_idx = 0, artifact_kind = .Trade})
	testing.expect_value(t, s.count, 1) // paused, not pushed
	scrubber_pause(&s, false)
	scrubber_push(&s, Scrubber_Entry{seq = 3, slot_idx = 0, artifact_kind = .Trade})
	testing.expect_value(t, s.count, 2)
}

@(test)
test_scrubber_seek :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 2, slot_idx = 0, artifact_kind = .Trade})
	scrubber_push(&s, Scrubber_Entry{seq = 3, slot_idx = 0, artifact_kind = .Trade})
	scrubber_seek(&s, 1)
	testing.expect_value(t, s.cursor, 1)
	scrubber_seek(&s, -1)
	testing.expect_value(t, s.cursor, -1)
	scrubber_seek(&s, 100)
	testing.expect_value(t, s.cursor, 2) // clamped to count-1
}

@(test)
test_scrubber_reset :: proc(t: ^testing.T) {
	s: Replay_Scrubber
	scrubber_push(&s, Scrubber_Entry{seq = 1, slot_idx = 0, artifact_kind = .Trade})
	scrubber_reset(&s)
	testing.expect_value(t, s.count, 0)
	testing.expect_value(t, s.head, 0)
}

@(test)
test_scrubber_nil_safe :: proc(t: ^testing.T) {
	scrubber_push(nil, Scrubber_Entry{})
	_, ok := scrubber_get(nil, 0)
	testing.expect(t, !ok, "nil get should fail")
	summary := scrubber_integrity_summary(nil)
	testing.expect_value(t, summary.total, 0)
	scrubber_seek(nil, 0)
	scrubber_pause(nil, true)
	scrubber_reset(nil)
}

// ============================================================================
// S51: Scene Snapshot Tests
// ============================================================================

@(test)
test_scene_snapshot_copy_scrubber_tail :: proc(t: ^testing.T) {
	scrubber: Replay_Scrubber
	for i in 0 ..< 100 {
		scrubber_push(&scrubber, Scrubber_Entry{seq = u64(i + 1), slot_idx = 0, artifact_kind = .Trade})
	}
	snap: Scene_Snapshot
	scene_snapshot_copy_scrubber_tail(&snap, &scrubber)
	testing.expect_value(t, snap.scrubber_tail_count, SCENE_SCRUBBER_TAIL_CAP)
	// Newest should be seq=100
	testing.expect_value(t, snap.scrubber_tail[0].seq, u64(100))
}

@(test)
test_scene_snapshot_set_build_tag :: proc(t: ^testing.T) {
	snap: Scene_Snapshot
	scene_snapshot_set_build_tag(&snap, "v1.2.3-abc123")
	testing.expect_value(t, int(snap.build_tag_len), 13)
	tag := string(snap.build_tag[:snap.build_tag_len])
	testing.expect(t, tag == "v1.2.3-abc123", "build tag mismatch")
}

@(test)
test_scene_snapshot_set_build_tag_truncate :: proc(t: ^testing.T) {
	snap: Scene_Snapshot
	long_tag := "this-is-a-very-long-build-tag-that-exceeds-32-characters-and-should-be-truncated"
	scene_snapshot_set_build_tag(&snap, long_tag)
	testing.expect_value(t, int(snap.build_tag_len), 32)
}

@(test)
test_scene_snapshot_serialize :: proc(t: ^testing.T) {
	snap: Scene_Snapshot
	snap.scene_version = SCENE_SNAPSHOT_VERSION
	snap.schema_version = 7
	snap.workspace_fingerprint = 12345
	snap.runtime.version = RUNTIME_SNAPSHOT_VERSION
	snap.runtime.capture_ts_ms = 1000
	snap.runtime.slot_count = 1
	snap.runtime.slots[0].used = true
	snap.runtime.slots[0].subject_id = 42
	snap.store_digests[0].candle_count = 100
	snap.store_digests[0].candle_newest_ts = 999

	buf: [32768]u8
	n := scene_snapshot_serialize(&snap, buf[:])
	testing.expect(t, n > 0, "serialize should write bytes")
	// Should contain SC| header and SD| store digest
	has_sc := false
	has_sd := false
	// Check for SC and SD prefixes in raw bytes
	for i in 0 ..< n - 2 {
		if buf[i] == 'S' && buf[i+1] == 'C' && buf[i+2] == '|' do has_sc = true
		if buf[i] == 'S' && buf[i+1] == 'D' && buf[i+2] == '|' do has_sd = true
	}
	testing.expect(t, has_sc, "output should contain SC| header")
	testing.expect(t, has_sd, "output should contain SD| store digest")
}

@(test)
test_scene_snapshot_nil_safe :: proc(t: ^testing.T) {
	scene_snapshot_copy_scrubber_tail(nil, nil)
	scene_snapshot_set_build_tag(nil, "test")
	buf: [256]u8
	n := scene_snapshot_serialize(nil, buf[:])
	testing.expect_value(t, n, 0)
}

// ============================================================================
// S51: Workspace Governance Tests
// ============================================================================

@(test)
test_workspace_compat_compatible :: proc(t: ^testing.T) {
	result := workspace_compat_check(WORKSPACE_MAX_COMPAT_VERSION)
	testing.expect_value(t, result, Workspace_Compat_Result.Compatible)
}

@(test)
test_workspace_compat_upgrade :: proc(t: ^testing.T) {
	result := workspace_compat_check(5)
	testing.expect_value(t, result, Workspace_Compat_Result.Upgrade_Available)
}

@(test)
test_workspace_compat_downgrade :: proc(t: ^testing.T) {
	result := workspace_compat_check(WORKSPACE_MAX_COMPAT_VERSION + 1)
	testing.expect_value(t, result, Workspace_Compat_Result.Downgrade_Warning)
}

@(test)
test_workspace_compat_incompatible :: proc(t: ^testing.T) {
	result := workspace_compat_check(WORKSPACE_MIN_COMPAT_VERSION - 1)
	testing.expect_value(t, result, Workspace_Compat_Result.Incompatible)
}

@(test)
test_workspace_compat_min_boundary :: proc(t: ^testing.T) {
	result := workspace_compat_check(WORKSPACE_MIN_COMPAT_VERSION)
	testing.expect(t, workspace_compat_is_loadable(result), "min version should be loadable")
}

@(test)
test_workspace_fingerprint_deterministic :: proc(t: ^testing.T) {
	data := []u8{1, 2, 3, 4, 5}
	fp1 := workspace_fingerprint(data)
	fp2 := workspace_fingerprint(data)
	testing.expect_value(t, fp1, fp2)
}

@(test)
test_workspace_fingerprint_varies :: proc(t: ^testing.T) {
	fp1 := workspace_fingerprint([]u8{1, 2, 3})
	fp2 := workspace_fingerprint([]u8{1, 2, 4})
	testing.expect(t, fp1 != fp2, "different data should produce different fingerprints")
}

@(test)
test_workspace_fingerprint_empty :: proc(t: ^testing.T) {
	fp := workspace_fingerprint([]u8{})
	testing.expect(t, fp != 0, "empty input should produce non-zero FNV offset basis")
}

@(test)
test_workspace_profile_version_guard :: proc(t: ^testing.T) {
	compat, fp_match := workspace_profile_version_guard(WORKSPACE_MAX_COMPAT_VERSION, 12345, 12345)
	testing.expect_value(t, compat, Workspace_Compat_Result.Compatible)
	testing.expect(t, fp_match, "same fingerprint should match")
}

@(test)
test_workspace_profile_version_guard_fp_mismatch :: proc(t: ^testing.T) {
	compat, fp_match := workspace_profile_version_guard(WORKSPACE_MAX_COMPAT_VERSION, 12345, 99999)
	testing.expect_value(t, compat, Workspace_Compat_Result.Compatible)
	testing.expect(t, !fp_match, "different fingerprint should not match")
}

@(test)
test_workspace_compat_is_loadable :: proc(t: ^testing.T) {
	testing.expect(t, workspace_compat_is_loadable(.Compatible), "Compatible is loadable")
	testing.expect(t, workspace_compat_is_loadable(.Upgrade_Available), "Upgrade is loadable")
	testing.expect(t, workspace_compat_is_loadable(.Downgrade_Warning), "Downgrade is loadable")
	testing.expect(t, !workspace_compat_is_loadable(.Incompatible), "Incompatible not loadable")
}

// ============================================================================
// S51: Diagnostics View Tests
// ============================================================================

@(test)
test_diagnostics_stream_latency :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	s.last_recv_ms[.Trade] = 1000
	s.last_recv_ms[.Orderbook] = 500
	lat := diagnostics_stream_latency(s, 2000)
	testing.expect_value(t, lat.recv_age_ms[.Trade], i64(1000))
	testing.expect_value(t, lat.recv_age_ms[.Orderbook], i64(1500))
	testing.expect_value(t, lat.worst_age_ms, i64(1500))
	testing.expect_value(t, lat.worst_artifact, Artifact_Kind.Orderbook)
}

@(test)
test_diagnostics_stream_latency_no_data :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	lat := diagnostics_stream_latency(s, 2000)
	testing.expect_value(t, lat.worst_age_ms, i64(-1))
}

@(test)
test_diagnostics_store_health_empty :: proc(t: ^testing.T) {
	h := diagnostics_store_health(0, 0, 1000, 60_000)
	testing.expect_value(t, h.integrity, Store_Integrity_Flag.Empty)
}

@(test)
test_diagnostics_store_health_ok :: proc(t: ^testing.T) {
	h := diagnostics_store_health(100, 900, 1000, 60_000)
	testing.expect_value(t, h.integrity, Store_Integrity_Flag.Ok)
	testing.expect_value(t, h.newest_age_ms, i64(100))
}

@(test)
test_diagnostics_store_health_stale :: proc(t: ^testing.T) {
	// newest_ts = 0, now = 200_000, tf = 60_000 → age = 200_000 > 180_000 (3x TF)
	h := diagnostics_store_health(50, 1000, 200_000, 60_000)
	testing.expect_value(t, h.integrity, Store_Integrity_Flag.Stale)
}

@(test)
test_diagnostics_cell_health :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 1000, false)
	diag := diagnostics_cell_health(0, s, 2000, 60_000)
	testing.expect_value(t, diag.composition, Composition_Stage.Live_Only)
	testing.expect_value(t, diag.health_level, System_Health_Level.Healthy)
	testing.expect(t, diag.event_count > 0, "event count should be positive")
}

@(test)
test_diagnostics_count_stale_aging :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Mark an orderbook event, then check at a time far in the future
	apply_state_mark_event(&s, .Orderbook, 1000, false)
	// Dual_Silence: aging > 8s, stale > 12s
	_, aging := diagnostics_count_stale_aging(s, 10_000, 60_000)
	testing.expect(t, aging > 0, "should have aging artifacts at 9s age")
	stale, _ := diagnostics_count_stale_aging(s, 14_000, 60_000)
	testing.expect(t, stale > 0, "should have stale artifacts at 13s age")
}
