package md_common

import "core:testing"
import "mr:ports"

// --- Artifact Policy tests ---

@(test)
test_artifact_policy_table_completeness :: proc(t: ^testing.T) {
	// Every artifact kind must have a policy entry.
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		_ = policy.snapshot_semantics
	}
}

@(test)
test_artifact_policy_orderbook_needs_snapshot :: proc(t: ^testing.T) {
	policy := artifact_policies[.Orderbook]
	testing.expect(t, policy.needs_snapshot_gate, "orderbook must require snapshot gate")
	testing.expect(t, policy.reset_on_reconnect, "orderbook must reset on reconnect")
	testing.expect(t, !policy.is_tf_sensitive, "orderbook is not TF-sensitive")
}

@(test)
test_artifact_policy_candle_tf_sensitive :: proc(t: ^testing.T) {
	policy := artifact_policies[.Candle]
	testing.expect(t, policy.is_tf_sensitive, "candle must be TF-sensitive")
	testing.expect(t, policy.reset_on_tf_change, "candle must reset on TF change")
	testing.expect(t, policy.accepts_range_seed, "candle must accept range seed")
	testing.expect(t, policy.has_synthetic_fallback, "candle must have synthetic fallback")
}

@(test)
test_artifact_policy_heatmap_degradable :: proc(t: ^testing.T) {
	policy := artifact_policies[.Heatmap]
	testing.expect_value(t, policy.backpressure_priority, BP_Priority.Degradable)
	testing.expect(t, policy.is_tf_sensitive, "heatmap must be TF-sensitive")
}

@(test)
test_artifact_policy_evidence_low_priority :: proc(t: ^testing.T) {
	policy := artifact_policies[.Evidence]
	testing.expect_value(t, policy.backpressure_priority, BP_Priority.Low)
}

@(test)
test_artifact_policy_stats_synthetic :: proc(t: ^testing.T) {
	policy := artifact_policies[.Stats]
	testing.expect(t, policy.has_synthetic_fallback, "stats must have synthetic fallback")
	testing.expect_value(t, policy.backpressure_priority, BP_Priority.Critical)
	testing.expect_value(t, policy.stale_detection, Stale_Detection.Dual_Silence)
}

@(test)
test_artifact_policy_vpvr_degradable_tf :: proc(t: ^testing.T) {
	policy := artifact_policies[.VPVR]
	testing.expect_value(t, policy.backpressure_priority, BP_Priority.Degradable)
	testing.expect(t, policy.is_tf_sensitive, "vpvr must be TF-sensitive")
	testing.expect(t, policy.has_synthetic_fallback, "vpvr must have synthetic fallback")
}

@(test)
test_artifact_policy_trade_minimal :: proc(t: ^testing.T) {
	policy := artifact_policies[.Trade]
	testing.expect(t, !policy.needs_snapshot_gate, "trade needs no snapshot gate")
	testing.expect(t, !policy.is_tf_sensitive, "trade is not TF-sensitive")
	testing.expect(t, !policy.reset_on_reconnect, "trade needs no reconnect reset")
	testing.expect(t, !policy.has_synthetic_fallback, "trade has no synthetic fallback")
	testing.expect_value(t, policy.backpressure_priority, BP_Priority.Critical)
}

@(test)
test_artifact_policy_signal_critical :: proc(t: ^testing.T) {
	policy := artifact_policies[.Signal]
	testing.expect_value(t, policy.backpressure_priority, BP_Priority.Critical)
	testing.expect(t, !policy.is_tf_sensitive, "signal is not TF-sensitive")
}

@(test)
test_artifact_kind_from_event_kind :: proc(t: ^testing.T) {
	testing.expect_value(t, artifact_kind_from_event_kind(.Trade), Artifact_Kind.Trade)
	testing.expect_value(t, artifact_kind_from_event_kind(.Orderbook_Snapshot), Artifact_Kind.Orderbook)
	testing.expect_value(t, artifact_kind_from_event_kind(.Range_Candle_Batch), Artifact_Kind.Range_Candle)
	testing.expect_value(t, artifact_kind_from_event_kind(.Stats), Artifact_Kind.Stats)
	testing.expect_value(t, artifact_kind_from_event_kind(.Heatmap), Artifact_Kind.Heatmap)
	testing.expect_value(t, artifact_kind_from_event_kind(.Evidence), Artifact_Kind.Evidence)
}

// --- Backpressure policy tests ---

@(test)
test_should_skip_by_bp_policy_critical_never_skipped :: proc(t: ^testing.T) {
	testing.expect(t, !should_skip_by_bp_policy(.Trade, true, true, true, 5), "trade must never be skipped")
	testing.expect(t, !should_skip_by_bp_policy(.Stats, true, true, true, 5), "stats must never be skipped")
	testing.expect(t, !should_skip_by_bp_policy(.Candle, true, true, true, 5), "candle must never be skipped")
	testing.expect(t, !should_skip_by_bp_policy(.Signal, true, true, true, 5), "signal must never be skipped")
	testing.expect(t, !should_skip_by_bp_policy(.Orderbook_Snapshot, true, true, true, 5), "orderbook must never be skipped")
	testing.expect(t, !should_skip_by_bp_policy(.Tape, true, true, true, 5), "tape must never be skipped")
}

@(test)
test_should_skip_by_bp_policy_degradable :: proc(t: ^testing.T) {
	testing.expect(t, should_skip_by_bp_policy(.Heatmap, true, true, false, 2), "heatmap should be skipped when degraded")
	testing.expect(t, !should_skip_by_bp_policy(.Heatmap, false, true, false, 2), "heatmap not skipped when bp disabled")
	testing.expect(t, !should_skip_by_bp_policy(.Heatmap, true, false, false, 2), "heatmap not skipped when degrade_heatmap=false")
	testing.expect(t, should_skip_by_bp_policy(.VPVR, true, false, true, 3), "vpvr should be skipped when degraded")
	testing.expect(t, !should_skip_by_bp_policy(.VPVR, true, false, false, 3), "vpvr not skipped when degrade_vpvr=false")
}

@(test)
test_should_skip_by_bp_policy_evidence_l3 :: proc(t: ^testing.T) {
	testing.expect(t, !should_skip_by_bp_policy(.Evidence, false, false, false, 2), "evidence not skipped at L2")
	testing.expect(t, should_skip_by_bp_policy(.Evidence, false, false, false, 3), "evidence skipped at L3")
	testing.expect(t, should_skip_by_bp_policy(.Evidence, false, false, false, 5), "evidence skipped at L5")
}

// --- Snapshot gate tests ---

@(test)
test_snapshot_gate_no_gate_required :: proc(t: ^testing.T) {
	policy := artifact_policies[.Trade]
	accept, gap := snapshot_gate_check(policy, false, false, false)
	testing.expect(t, accept, "trade must always be accepted")
	testing.expect(t, !gap, "trade must never report gap")
}

@(test)
test_snapshot_gate_orderbook_snapshot_seen :: proc(t: ^testing.T) {
	policy := artifact_policies[.Orderbook]
	accept, gap := snapshot_gate_check(policy, true, false, false)
	testing.expect(t, accept, "after snapshot seen, all accepted")
	testing.expect(t, !gap, "no gap after snapshot seen")
}

@(test)
test_snapshot_gate_orderbook_first_snapshot :: proc(t: ^testing.T) {
	policy := artifact_policies[.Orderbook]
	accept, gap := snapshot_gate_check(policy, false, true, false)
	testing.expect(t, accept, "explicit snapshot must be accepted")
	testing.expect(t, !gap, "no gap on explicit snapshot")
}

@(test)
test_snapshot_gate_orderbook_bootstrap_delta :: proc(t: ^testing.T) {
	policy := artifact_policies[.Orderbook]
	accept, gap := snapshot_gate_check(policy, false, false, true)
	testing.expect(t, accept, "non-empty bootstrap delta accepted")
	testing.expect(t, !gap, "no gap on bootstrap delta")
}

@(test)
test_snapshot_gate_orderbook_empty_delta_before_snapshot :: proc(t: ^testing.T) {
	policy := artifact_policies[.Orderbook]
	accept, gap := snapshot_gate_check(policy, false, false, false)
	testing.expect(t, !accept, "empty delta before snapshot must be rejected")
	testing.expect(t, gap, "must report snapshot gap")
}

@(test)
test_snapshot_gate_candle_always_accepts :: proc(t: ^testing.T) {
	policy := artifact_policies[.Candle]
	accept, gap := snapshot_gate_check(policy, false, false, false)
	testing.expect(t, accept, "candle has no snapshot gate")
	testing.expect(t, !gap, "candle should never report gap")
}

// --- Protocol Engine state machine tests ---

@(test)
test_protocol_subscribe_to_bootstrap :: proc(t: ^testing.T) {
	p: Stream_Protocol
	tr := protocol_on_subscribe(&p, 1000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Subscribe)
	testing.expect(t, !p.snapshot_seen, "snapshot_seen must be false after subscribe")
	testing.expect_value(t, p.last_seq, i64(0))
	testing.expect_value(t, p.event_count, u64(0))
}

@(test)
test_protocol_snapshot_advances_to_seeded :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	tr := protocol_on_snapshot(&p, 1, 2000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Seeded)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.First_Snapshot)
	testing.expect(t, p.snapshot_seen, "snapshot_seen must be true")
	testing.expect_value(t, p.last_seq, i64(1))
}

@(test)
test_protocol_event_advances_seeded_to_live :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_snapshot(&p, 1, 2000)
	tr := protocol_on_event(&p, 2, 3000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.First_Event)
}

@(test)
test_protocol_event_stays_live :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_snapshot(&p, 1, 2000)
	protocol_on_event(&p, 2, 3000, 3)
	tr := protocol_on_event(&p, 3, 4000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.None)
}

@(test)
test_protocol_seq_gap_non_recurring :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)  // Bootstrap_Pending -> Seeded
	protocol_on_event(&p, 2, 3000, 3)  // Seeded -> Live
	// Gap: expected 3, got 5
	tr := protocol_on_event(&p, 5, 4000, 3)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Seq_Gap)
	// Single gap doesn't trigger Degraded — stays Live
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
	testing.expect_value(t, p.seq_gap_streak, 1)
}

@(test)
test_protocol_seq_gap_recurring :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)
	protocol_on_event(&p, 2, 3000, 3)
	// 3 consecutive gaps -> recurring
	protocol_on_event(&p, 5, 4000, 3)   // gap 1
	protocol_on_event(&p, 10, 5000, 3)  // gap 2
	tr := protocol_on_event(&p, 20, 6000, 3)  // gap 3 -> recurring threshold
	testing.expect_value(t, p.state, Stream_Protocol_State.Degraded)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Seq_Gap_Recurring)
	testing.expect_value(t, p.desync_reason, ports.MD_Desync_Reason.Sequence_Gap)
}

@(test)
test_protocol_seq_gap_streak_reset_on_clean :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)
	protocol_on_event(&p, 2, 3000, 3)
	// One gap
	protocol_on_event(&p, 5, 4000, 3)
	testing.expect_value(t, p.seq_gap_streak, 1)
	// Clean event resets streak
	protocol_on_event(&p, 6, 5000, 3)
	testing.expect_value(t, p.seq_gap_streak, 0)
}

@(test)
test_protocol_reconnect_resets_state :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_snapshot(&p, 1, 2000)
	protocol_on_event(&p, 2, 3000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)

	tr := protocol_on_reconnect(&p, 4000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Reconnecting)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Reconnect)
	testing.expect(t, !p.snapshot_seen, "snapshot_seen must be cleared on reconnect")
	testing.expect_value(t, p.last_seq, i64(0))
	testing.expect_value(t, p.seq_gap_streak, 0)
	testing.expect_value(t, p.desync_reason, ports.MD_Desync_Reason.None)
}

@(test)
test_protocol_stale_check :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)

	// Not stale yet
	testing.expect(t, !protocol_check_stale(p, 5000, 5000), "should not be stale within threshold")
	// Not stale at boundary
	testing.expect(t, !protocol_check_stale(p, 7000, 5000), "should not be stale at boundary")
	// Stale after threshold
	testing.expect(t, protocol_check_stale(p, 8000, 5000), "should be stale after threshold")

	// Stale transition
	tr := protocol_on_stale_timeout(&p, 8000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Stale)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Stale_Timeout)
}

@(test)
test_protocol_stale_not_for_idle :: proc(t: ^testing.T) {
	p: Stream_Protocol
	testing.expect(t, !protocol_check_stale(p, 999999, 1000), "Idle stream must not report stale")
}

@(test)
test_protocol_stale_not_for_reconnecting :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)
	protocol_on_reconnect(&p, 3000)
	testing.expect(t, !protocol_check_stale(p, 999999, 1000), "Reconnecting must not report stale")
}

@(test)
test_protocol_stale_recovery_on_event :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)
	protocol_on_stale_timeout(&p, 8000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Stale)

	// New event clears stale
	tr := protocol_on_event(&p, 2, 9000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Desync_Cleared)
}

@(test)
test_protocol_range_complete_seeds :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	tr := protocol_on_range_complete(&p, 500, 2000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Seeded)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Range_Complete)
	testing.expect(t, p.getrange_seeded, "getrange_seeded must be true")
	testing.expect_value(t, p.getrange_oldest_ts, i64(500))
	testing.expect(t, !p.getrange_pending, "pending must be cleared")
}

@(test)
test_protocol_range_complete_from_live :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)
	protocol_on_event(&p, 2, 3000, 3)
	tr := protocol_on_range_complete(&p, 500, 4000)
	// Already Live, should stay Live
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Range_Complete)
}

@(test)
test_protocol_snapshot_gap :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)
	tr := protocol_on_snapshot_gap(&p, 3000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Degraded)
	testing.expect_value(t, p.desync_reason, ports.MD_Desync_Reason.Snapshot_Gap)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.Snapshot_Gap)
}

@(test)
test_protocol_resync_flow :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)

	tr1 := protocol_on_resync_sent(&p, 3000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Resyncing)
	testing.expect_value(t, tr1.reason, Protocol_Transition_Reason.Resync_Sent)
	testing.expect_value(t, p.resync_sent_ms, i64(3000))

	tr2 := protocol_on_resync_ack(&p, 4000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Seeded)
	testing.expect_value(t, tr2.reason, Protocol_Transition_Reason.Resync_Ack)
	testing.expect(t, !p.snapshot_seen, "snapshot_seen reset after resync ack")
	testing.expect_value(t, p.resync_sent_ms, i64(0))
}

@(test)
test_protocol_tf_change_resets :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_snapshot(&p, 1, 2000)
	protocol_on_event(&p, 2, 3000, 3)
	p.getrange_seeded = true
	p.getrange_oldest_ts = 500

	tr := protocol_on_tf_change(&p, 4000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)
	testing.expect_value(t, tr.reason, Protocol_Transition_Reason.TF_Change)
	testing.expect(t, !p.getrange_seeded, "getrange_seeded must be cleared on TF change")
	testing.expect_value(t, p.getrange_oldest_ts, i64(0))
	testing.expect(t, !p.snapshot_seen, "snapshot_seen must be cleared on TF change")
	testing.expect_value(t, p.last_seq, i64(0))
	testing.expect_value(t, p.event_count, u64(0))
}

@(test)
test_protocol_is_accepting_events :: proc(t: ^testing.T) {
	testing.expect(t, !protocol_is_accepting_events(.Idle), "Idle must not accept events")
	testing.expect(t, !protocol_is_accepting_events(.Reconnecting), "Reconnecting must not accept events")
	testing.expect(t, protocol_is_accepting_events(.Bootstrap_Pending), "Bootstrap_Pending must accept events")
	testing.expect(t, protocol_is_accepting_events(.Live), "Live must accept events")
	testing.expect(t, protocol_is_accepting_events(.Degraded), "Degraded must accept events")
	testing.expect(t, protocol_is_accepting_events(.Stale), "Stale must accept events")
	testing.expect(t, protocol_is_accepting_events(.Seeded), "Seeded must accept events")
	testing.expect(t, protocol_is_accepting_events(.Resyncing), "Resyncing must accept events")
}

@(test)
test_protocol_needs_resync :: proc(t: ^testing.T) {
	p: Stream_Protocol
	p.state = .Degraded
	p.desync_reason = .Sequence_Gap
	testing.expect(t, protocol_needs_resync(p), "Degraded with reason must need resync")

	p.desync_reason = .None
	testing.expect(t, !protocol_needs_resync(p), "Degraded without reason must not need resync")

	p.state = .Live
	p.desync_reason = .Sequence_Gap
	testing.expect(t, !protocol_needs_resync(p), "Live must not need resync")
}

// --- Stream Apply State tests ---

@(test)
test_apply_state_reset :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	s.has_live[.Stats] = true
	s.snapshot_seen[.Orderbook] = true
	s.getrange_seeded = true
	s.event_count = 42
	apply_state_reset(&s)
	testing.expect(t, !s.has_live[.Stats], "has_live must be cleared")
	testing.expect(t, !s.snapshot_seen[.Orderbook], "snapshot_seen must be cleared")
	testing.expect(t, !s.getrange_seeded, "getrange_seeded must be cleared")
	testing.expect_value(t, s.event_count, u64(0))
}

@(test)
test_apply_state_on_reconnect :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	s.snapshot_seen[.Orderbook] = true
	s.snapshot_seen[.Trade] = true
	s.has_live[.Stats] = true
	s.getrange_pending = true
	apply_state_on_reconnect(&s)
	testing.expect(t, !s.snapshot_seen[.Orderbook], "orderbook snapshot must be cleared on reconnect")
	testing.expect(t, s.snapshot_seen[.Trade], "trade snapshot must NOT be cleared (no gate)")
	testing.expect(t, !s.getrange_pending, "getrange_pending must be cleared")
	testing.expect(t, s.has_live[.Stats], "live stats must NOT be cleared on reconnect")
}

@(test)
test_apply_state_on_tf_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	s.has_live[.Candle] = true
	s.has_live[.Heatmap] = true
	s.has_live[.VPVR] = true
	s.has_live[.Trade] = true
	s.has_live[.Stats] = true
	s.getrange_seeded = true
	s.getrange_oldest_ts = 500
	s.synth_heatmap_last_window = 1000
	apply_state_on_tf_change(&s)
	testing.expect(t, !s.has_live[.Candle], "candle live must be cleared on TF change")
	testing.expect(t, !s.has_live[.Heatmap], "heatmap live must be cleared on TF change")
	testing.expect(t, !s.has_live[.VPVR], "vpvr live must be cleared on TF change")
	testing.expect(t, s.has_live[.Trade], "trade live must NOT be cleared on TF change")
	testing.expect(t, s.has_live[.Stats], "stats live must NOT be cleared on TF change")
	testing.expect(t, !s.getrange_seeded, "getrange_seeded must be cleared")
	testing.expect_value(t, s.getrange_oldest_ts, i64(0))
	testing.expect_value(t, s.synth_heatmap_last_window, i64(0))
}

@(test)
test_apply_state_mark_event :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1000, false)
	testing.expect(t, s.has_live[.Stats], "has_live must be set")
	testing.expect(t, !s.using_synthetic[.Stats], "synthetic must be cleared when live received")
	testing.expect_value(t, s.last_recv_ms[.Stats], i64(1000))
	testing.expect_value(t, s.event_count, u64(1))
}

@(test)
test_apply_state_mark_event_snapshot :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Orderbook, 1000, true)
	testing.expect(t, s.snapshot_seen[.Orderbook], "snapshot_seen must be set for snapshot event")
	testing.expect(t, s.has_live[.Orderbook], "has_live must be set")
}

@(test)
test_apply_state_mark_event_non_snapshot_no_gate :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Trade has no snapshot gate, so snapshot_seen is set even for non-snapshot events
	apply_state_mark_event(&s, .Trade, 1000, false)
	testing.expect(t, s.snapshot_seen[.Trade], "no-gate artifact sets snapshot_seen on any event")
}

@(test)
test_apply_state_synthetic_displaced_by_live :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_synthetic(&s, .Stats, 1000)
	testing.expect(t, s.using_synthetic[.Stats], "synthetic must be active")
	testing.expect(t, !s.has_live[.Stats], "live must not be set")

	apply_state_mark_event(&s, .Stats, 2000, false)
	testing.expect(t, s.has_live[.Stats], "live must be set after real event")
	testing.expect(t, !s.using_synthetic[.Stats], "synthetic must be displaced")
}

@(test)
test_apply_state_synthetic_ignored_when_live :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 1000, false)
	apply_state_mark_synthetic(&s, .Candle, 2000)
	testing.expect(t, !s.using_synthetic[.Candle], "synthetic must be ignored when live already set")
}

@(test)
test_apply_state_should_use_synthetic :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect(t, apply_state_should_use_synthetic(s, .Stats), "should use synthetic when no live stats")
	testing.expect(t, apply_state_should_use_synthetic(s, .Candle), "should use synthetic when no live candle")
	testing.expect(t, apply_state_should_use_synthetic(s, .Heatmap), "should use synthetic when no live heatmap")
	testing.expect(t, !apply_state_should_use_synthetic(s, .Trade), "trade has no synthetic fallback")
	testing.expect(t, !apply_state_should_use_synthetic(s, .Signal), "signal has no synthetic fallback")

	apply_state_mark_event(&s, .Stats, 1000, false)
	testing.expect(t, !apply_state_should_use_synthetic(s, .Stats), "should not use synthetic when live")
}

@(test)
test_apply_state_needs_snapshot :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect(t, apply_state_needs_snapshot(s, .Orderbook), "orderbook needs snapshot initially")
	testing.expect(t, !apply_state_needs_snapshot(s, .Trade), "trade never needs snapshot")
	testing.expect(t, !apply_state_needs_snapshot(s, .Candle), "candle never needs snapshot")
	testing.expect(t, !apply_state_needs_snapshot(s, .Stats), "stats never needs snapshot")

	apply_state_mark_event(&s, .Orderbook, 1000, true)
	testing.expect(t, !apply_state_needs_snapshot(s, .Orderbook), "orderbook snapshot satisfied")
}

@(test)
test_apply_state_getrange_timeout :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect(t, !apply_state_check_getrange_timeout(s, 100, 300), "not pending means no timeout")

	apply_state_mark_range_sent(&s, 100)
	testing.expect(t, s.getrange_pending, "must be pending")
	testing.expect(t, !apply_state_check_getrange_timeout(s, 200, 300), "not timed out yet")
	testing.expect(t, !apply_state_check_getrange_timeout(s, 400, 300), "at boundary")
	testing.expect(t, apply_state_check_getrange_timeout(s, 500, 300), "should be timed out")
}

@(test)
test_apply_state_range_complete :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 100)
	apply_state_mark_range_complete(&s, 500)
	testing.expect(t, s.getrange_seeded, "must be seeded")
	testing.expect(t, !s.getrange_pending, "must not be pending")
	testing.expect_value(t, s.getrange_oldest_ts, i64(500))

	// Older ts should update
	apply_state_mark_range_complete(&s, 300)
	testing.expect_value(t, s.getrange_oldest_ts, i64(300))

	// Newer ts should NOT update
	apply_state_mark_range_complete(&s, 400)
	testing.expect_value(t, s.getrange_oldest_ts, i64(300))
}

@(test)
test_apply_state_summary :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 1000, false)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	s.getrange_seeded = true

	sum := apply_state_summary(s)
	testing.expect(t, sum.has_live_stats, "summary must reflect live stats")
	testing.expect(t, !sum.has_live_candle, "summary must reflect no live candle")
	testing.expect(t, sum.snapshot_seen, "summary must reflect orderbook snapshot")
	testing.expect(t, sum.getrange_seeded, "summary must reflect getrange seeded")
	testing.expect(t, !sum.has_live_heatmap, "summary must reflect no live heatmap")
	testing.expect(t, !sum.has_live_vpvr, "summary must reflect no live vpvr")
}

// --- Full lifecycle integration tests ---

@(test)
test_protocol_full_lifecycle :: proc(t: ^testing.T) {
	p: Stream_Protocol

	// 1. Subscribe
	protocol_on_subscribe(&p, 1000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)

	// 2. First event (no snapshot gate for trades)
	protocol_on_event(&p, 1, 2000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Seeded)

	// 3. More events -> Live
	protocol_on_event(&p, 2, 3000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)

	// 4. GetRange seeds historical data
	protocol_on_range_complete(&p, 500, 3500)
	testing.expect(t, p.getrange_seeded, "must be seeded")

	// 5. Stale detection
	testing.expect(t, protocol_check_stale(p, 15000, 10000), "should be stale")
	protocol_on_stale_timeout(&p, 15000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Stale)

	// 6. Recovery
	protocol_on_event(&p, 3, 16000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)

	// 7. Reconnect
	protocol_on_reconnect(&p, 20000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Reconnecting)
	testing.expect(t, !p.snapshot_seen, "snapshot cleared")

	// 8. Re-subscribe after reconnect
	protocol_on_subscribe(&p, 21000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)

	// 9. Snapshot recovery
	protocol_on_snapshot(&p, 100, 22000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Seeded)

	// 10. Back to live
	protocol_on_event(&p, 101, 23000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
}

@(test)
test_protocol_resync_recovery_via_snapshot :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_snapshot(&p, 1, 2000)
	protocol_on_event(&p, 2, 3000, 3)

	// Enter degraded via snapshot gap
	protocol_on_snapshot_gap(&p, 4000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Degraded)

	// Resync sent
	protocol_on_resync_sent(&p, 5000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Resyncing)

	// Snapshot clears resyncing
	protocol_on_snapshot(&p, 10, 6000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
	testing.expect_value(t, p.desync_reason, ports.MD_Desync_Reason.None)
}

@(test)
test_protocol_degraded_recovery_on_snapshot :: proc(t: ^testing.T) {
	p: Stream_Protocol
	protocol_on_subscribe(&p, 1000)
	protocol_on_event(&p, 1, 2000, 3)
	protocol_on_event(&p, 2, 3000, 3)

	// Force degraded via recurring gaps
	protocol_on_event(&p, 5, 4000, 3)
	protocol_on_event(&p, 10, 5000, 3)
	protocol_on_event(&p, 20, 6000, 3)
	testing.expect_value(t, p.state, Stream_Protocol_State.Degraded)

	// Snapshot clears degraded
	protocol_on_snapshot(&p, 50, 7000)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)
	testing.expect_value(t, p.desync_reason, ports.MD_Desync_Reason.None)
}

// --- Combined protocol + apply state integration ---

@(test)
test_protocol_and_apply_state_coordinated :: proc(t: ^testing.T) {
	p: Stream_Protocol
	s: Stream_Apply_State

	// Subscribe
	protocol_on_subscribe(&p, 1000)
	apply_state_reset(&s)

	// Orderbook snapshot
	protocol_on_snapshot(&p, 1, 2000)
	apply_state_mark_event(&s, .Orderbook, 2000, true)
	testing.expect_value(t, p.state, Stream_Protocol_State.Seeded)
	testing.expect(t, !apply_state_needs_snapshot(s, .Orderbook), "snapshot satisfied")

	// Trade event
	protocol_on_event(&p, 2, 3000, 3)
	apply_state_mark_event(&s, .Trade, 3000, false)
	testing.expect_value(t, p.state, Stream_Protocol_State.Live)

	// Synthetic candle (no live candle yet)
	testing.expect(t, apply_state_should_use_synthetic(s, .Candle), "should use synthetic candle")
	apply_state_mark_synthetic(&s, .Candle, 3000)

	// Live candle arrives
	apply_state_mark_event(&s, .Candle, 4000, false)
	testing.expect(t, !apply_state_should_use_synthetic(s, .Candle), "no longer synthetic")

	// TF change
	protocol_on_tf_change(&p, 5000)
	apply_state_on_tf_change(&s)
	testing.expect_value(t, p.state, Stream_Protocol_State.Bootstrap_Pending)
	testing.expect(t, !s.has_live[.Candle], "candle cleared on TF change")
	testing.expect(t, s.has_live[.Trade], "trade survives TF change")
	testing.expect(t, apply_state_should_use_synthetic(s, .Candle), "back to synthetic candle")
}

// =========================================================================
// S33: Runtime Ownership Cutover — Candle recv timing convergence tests.
// =========================================================================

@(test)
test_apply_state_candle_recv_ms_zero :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(0))
}

@(test)
test_apply_state_candle_recv_ms_live_only :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(5000))
}

@(test)
test_apply_state_candle_recv_ms_range_only :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Range_Candle, 3000, false)
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(3000))
}

@(test)
test_apply_state_candle_recv_ms_live_wins :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Range_Candle, 3000, false)
	apply_state_mark_event(&s, .Candle, 5000, false)
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(5000))
}

@(test)
test_apply_state_candle_recv_ms_range_wins :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Range data arrived more recently (e.g., lazy loading after live already flowing).
	apply_state_mark_event(&s, .Candle, 3000, false)
	apply_state_mark_event(&s, .Range_Candle, 5000, false)
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(5000))
}

@(test)
test_apply_state_candle_recv_ms_tf_change_clears_both :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_event(&s, .Range_Candle, 3000, false)
	// Both Candle and Range_Candle are TF-sensitive (reset_on_tf_change = true).
	apply_state_on_tf_change(&s)
	// After TF change, both timestamps are cleared.
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(0))
}

@(test)
test_apply_state_candle_recv_ms_reset_zeros :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_event(&s, .Range_Candle, 3000, false)
	apply_state_reset(&s)
	testing.expect_value(t, apply_state_candle_recv_ms(s), i64(0))
}

// =========================================================================
// S34: getrange_request_id lifecycle tests
// =========================================================================

@(test)
test_getrange_request_id_set_by_mark_range_sent :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABCD)
	testing.expect_value(t, s.getrange_request_id, u64(0xABCD))
	testing.expect(t, s.getrange_pending, "should be pending after mark_range_sent")
}

@(test)
test_getrange_request_id_cleared_by_mark_range_complete :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABCD)
	apply_state_mark_range_complete(&s, 1000)
	testing.expect_value(t, s.getrange_request_id, u64(0))
	testing.expect(t, !s.getrange_pending, "should not be pending after completion")
	testing.expect(t, s.getrange_seeded, "should be seeded after completion")
}

@(test)
test_getrange_request_id_cleared_by_reconnect :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABCD)
	apply_state_on_reconnect(&s)
	testing.expect_value(t, s.getrange_request_id, u64(0))
	testing.expect(t, !s.getrange_pending, "should not be pending after reconnect")
}

@(test)
test_getrange_request_id_cleared_by_tf_change :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABCD)
	apply_state_on_tf_change(&s)
	testing.expect_value(t, s.getrange_request_id, u64(0))
	testing.expect(t, !s.getrange_pending, "should not be pending after TF change")
	testing.expect(t, !s.getrange_seeded, "seeded should be false after TF change")
}

@(test)
test_getrange_request_id_cleared_by_reset :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABCD)
	apply_state_reset(&s)
	testing.expect_value(t, s.getrange_request_id, u64(0))
}

@(test)
test_getrange_request_id_preserved_across_events :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 10, 0xABCD)
	apply_state_mark_event(&s, .Candle, 5000, false)
	apply_state_mark_event(&s, .Trade, 5100, false)
	testing.expect_value(t, s.getrange_request_id, u64(0xABCD))
}

// =========================================================================
// S34: Composition orchestrator tests
// =========================================================================

@(test)
test_composition_intent_no_stream :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, composition_intent(s, 0, false), Orchestrator_Intent.None)
}

@(test)
test_composition_intent_empty_store_seeds :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, composition_intent(s, 0, true), Orchestrator_Intent.Seed_Range)
}

@(test)
test_composition_intent_pending_awaits :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_sent(&s, 1, 0x1)
	testing.expect_value(t, composition_intent(s, 0, true), Orchestrator_Intent.Await_Seed)
}

@(test)
test_composition_intent_seeded_no_live_awaits :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 1000)
	testing.expect_value(t, composition_intent(s, 100, true), Orchestrator_Intent.Await_Live)
}

@(test)
test_composition_intent_seeded_with_live_steady :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 1000)
	apply_state_mark_event(&s, .Candle, 5000, false)
	testing.expect_value(t, composition_intent(s, 100, true), Orchestrator_Intent.Steady)
}

@(test)
test_composition_intent_live_only_seeds :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 5000, false)
	testing.expect_value(t, composition_intent(s, 10, true), Orchestrator_Intent.Seed_Range)
}

@(test)
test_composition_intent_tf_change_resets_to_seed :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 1000)
	apply_state_mark_event(&s, .Candle, 5000, false)
	testing.expect_value(t, composition_intent(s, 100, true), Orchestrator_Intent.Steady)
	apply_state_on_tf_change(&s)
	testing.expect_value(t, composition_intent(s, 0, true), Orchestrator_Intent.Seed_Range)
}

@(test)
test_composition_should_extend_basic :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 5000)
	testing.expect(t, composition_should_extend(s, 100, 500, 0, false),
		"should extend when seeded with room")
}

@(test)
test_composition_should_extend_pending_blocks :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 5000)
	apply_state_mark_range_sent(&s, 10, 0x1)
	testing.expect(t, !composition_should_extend(s, 100, 500, 0, false),
		"should not extend while pending")
}

@(test)
test_composition_should_extend_full_blocks :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 5000)
	testing.expect(t, !composition_should_extend(s, 500, 500, 0, false),
		"should not extend when store full")
}

@(test)
test_composition_should_extend_timeline_boundary :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_range_complete(&s, 5000)
	// oldest_ts <= timeline.first_ts means we reached the boundary
	testing.expect(t, !composition_should_extend(s, 100, 500, 5000, true),
		"should not extend past timeline boundary")
	testing.expect(t, composition_should_extend(s, 100, 500, 4000, true),
		"should extend when not yet at timeline boundary")
}

@(test)
test_composition_should_extend_not_seeded :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect(t, !composition_should_extend(s, 0, 500, 0, false),
		"should not extend when not seeded")
}

// =========================================================================
// S138: Bootstrap timing probe tests.
// =========================================================================

@(test)
test_first_event_ms_latches_on_first_event :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	testing.expect_value(t, s.first_event_ms, i64(0))

	apply_state_mark_event(&s, .Trade, 5000, false)
	testing.expect_value(t, s.first_event_ms, i64(5000))

	// Second event must NOT overwrite the latched value.
	apply_state_mark_event(&s, .Stats, 7000, false)
	testing.expect_value(t, s.first_event_ms, i64(5000))
}

@(test)
test_first_event_ms_survives_reconnect :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Candle, 3000, false)
	testing.expect_value(t, s.first_event_ms, i64(3000))

	// Reconnect should NOT clear first_event_ms — it persists for telemetry.
	apply_state_on_reconnect(&s)
	testing.expect_value(t, s.first_event_ms, i64(3000))
}

@(test)
test_first_event_ms_reset_clears :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Trade, 2000, false)
	testing.expect_value(t, s.first_event_ms, i64(2000))

	// Full reset clears everything including first_event_ms.
	apply_state_reset(&s)
	testing.expect_value(t, s.first_event_ms, i64(0))
}

@(test)
test_first_event_ms_in_telemetry :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	apply_state_mark_event(&s, .Stats, 4200, false)
	telem := apply_state_telemetry(s)
	testing.expect_value(t, telem.first_event_ms, i64(4200))
}

@(test)
test_first_event_ms_ignores_zero_timestamp :: proc(t: ^testing.T) {
	s: Stream_Apply_State
	// Event with now_ms=0 should not set first_event_ms.
	apply_state_mark_event(&s, .Trade, 0, false)
	testing.expect_value(t, s.first_event_ms, i64(0))

	// But a real timestamp should latch.
	apply_state_mark_event(&s, .Trade, 1000, false)
	testing.expect_value(t, s.first_event_ms, i64(1000))
}
