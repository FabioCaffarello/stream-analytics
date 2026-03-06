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
