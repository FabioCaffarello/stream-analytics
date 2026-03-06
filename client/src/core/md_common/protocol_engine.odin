package md_common

import "mr:ports"

// Stream_Protocol_State is the canonical per-stream protocol state.
// Replaces the implicit state scattered across snapshot_seen bools,
// active_metrics flags, and desync_reason fields.
Stream_Protocol_State :: enum u8 {
	Idle,                // Not subscribed
	Bootstrap_Pending,   // Subscribed, waiting for first data or snapshot
	Seeded,              // Initial data received (snapshot or range), accepting updates
	Live,                // Regular data flowing, all healthy
	Degraded,            // Data flowing but quality issues (seq gaps, stale approaching)
	Resyncing,           // Resync requested, waiting for server ACK
	Reconnecting,        // Transport reconnecting, data paused
	Stale,               // No data received within threshold
}

// Protocol_Transition records a state change for observability/testing.
Protocol_Transition :: struct {
	from:      Stream_Protocol_State,
	to:        Stream_Protocol_State,
	reason:    Protocol_Transition_Reason,
	timestamp: i64,
}

Protocol_Transition_Reason :: enum u8 {
	None,
	Subscribe,
	First_Snapshot,
	First_Event,
	Range_Complete,
	Reconnect,
	Seq_Gap,
	Seq_Gap_Recurring,
	Snapshot_Gap,
	Stale_Timeout,
	Resync_Sent,
	Resync_Ack,
	Desync_Cleared,
	TF_Change,
}

// Stream_Protocol is the per-stream protocol state machine.
// One instance per stream (keyed by subject_id or market_id).
// All transitions go through pure functions below.
Stream_Protocol :: struct {
	state:              Stream_Protocol_State,
	snapshot_seen:      bool,
	last_seq:           i64,
	last_event_ts_ms:   i64,
	seq_gap_streak:     int,
	getrange_seeded:    bool,
	getrange_pending:   bool,
	getrange_oldest_ts: i64,
	desync_reason:      ports.MD_Desync_Reason,
	resync_sent_ms:     i64,
	event_count:        u64,
}

// --- Pure transition functions ---
// Each returns the transition that occurred (or .None reason if no change).
// Caller is responsible for applying side effects (resync messages, store clears, etc.)

// protocol_on_subscribe: called when a subscribe is sent for this stream.
protocol_on_subscribe :: proc(p: ^Stream_Protocol, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.state = .Bootstrap_Pending
	p.snapshot_seen = false
	p.last_seq = 0
	p.seq_gap_streak = 0
	p.desync_reason = .None
	p.event_count = 0
	return Protocol_Transition{from = prev, to = .Bootstrap_Pending, reason = .Subscribe, timestamp = now_ms}
}

// protocol_on_snapshot: called when a snapshot is received (orderbook, stats, etc.)
// For artifacts with needs_snapshot_gate=true, this gates further delta acceptance.
protocol_on_snapshot :: proc(p: ^Stream_Protocol, seq: i64, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.snapshot_seen = true
	p.last_event_ts_ms = now_ms
	p.event_count += 1
	if seq > 0 do p.last_seq = seq

	if prev == .Bootstrap_Pending || prev == .Idle {
		p.state = .Seeded
		return Protocol_Transition{from = prev, to = .Seeded, reason = .First_Snapshot, timestamp = now_ms}
	}
	if prev == .Resyncing || prev == .Stale || prev == .Degraded {
		p.state = .Live
		p.desync_reason = .None
		p.seq_gap_streak = 0
		return Protocol_Transition{from = prev, to = .Live, reason = .Desync_Cleared, timestamp = now_ms}
	}
	return Protocol_Transition{from = prev, to = prev, reason = .None, timestamp = now_ms}
}

// protocol_on_event: called for any non-snapshot event.
// Handles seq gap detection and state advancement.
protocol_on_event :: proc(p: ^Stream_Protocol, seq: i64, now_ms: i64, recurring_threshold: int) -> Protocol_Transition {
	prev := p.state
	p.last_event_ts_ms = now_ms
	p.event_count += 1

	// Seq gap check
	if seq > 0 && p.last_seq > 0 {
		has_gap, next_streak, is_recurring := seq_gap_transition(p.last_seq, seq, p.seq_gap_streak, recurring_threshold)
		if has_gap {
			p.seq_gap_streak = next_streak
			if is_recurring {
				p.state = .Degraded
				p.desync_reason = .Sequence_Gap
				if seq > 0 do p.last_seq = seq
				return Protocol_Transition{from = prev, to = .Degraded, reason = .Seq_Gap_Recurring, timestamp = now_ms}
			}
			if seq > 0 do p.last_seq = seq
			return Protocol_Transition{from = prev, to = prev, reason = .Seq_Gap, timestamp = now_ms}
		}
		p.seq_gap_streak = 0
	}
	if seq > 0 do p.last_seq = seq

	// State advancement
	if prev == .Bootstrap_Pending {
		p.state = .Seeded
		return Protocol_Transition{from = prev, to = .Seeded, reason = .First_Event, timestamp = now_ms}
	}
	if prev == .Seeded {
		p.state = .Live
		return Protocol_Transition{from = prev, to = .Live, reason = .First_Event, timestamp = now_ms}
	}
	if prev == .Stale || prev == .Degraded {
		p.state = .Live
		p.desync_reason = .None
		p.seq_gap_streak = 0
		return Protocol_Transition{from = prev, to = .Live, reason = .Desync_Cleared, timestamp = now_ms}
	}
	return Protocol_Transition{from = prev, to = prev, reason = .None, timestamp = now_ms}
}

// protocol_on_range_complete: called when a GetRange batch with is_last=true arrives.
protocol_on_range_complete :: proc(p: ^Stream_Protocol, oldest_ts: i64, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.getrange_seeded = true
	p.getrange_pending = false
	if oldest_ts > 0 && (p.getrange_oldest_ts <= 0 || oldest_ts < p.getrange_oldest_ts) {
		p.getrange_oldest_ts = oldest_ts
	}
	p.last_event_ts_ms = now_ms

	if prev == .Bootstrap_Pending || prev == .Idle {
		p.state = .Seeded
		return Protocol_Transition{from = prev, to = .Seeded, reason = .Range_Complete, timestamp = now_ms}
	}
	return Protocol_Transition{from = prev, to = prev, reason = .Range_Complete, timestamp = now_ms}
}

// protocol_on_reconnect: called when transport reconnects. Resets per-stream state.
protocol_on_reconnect :: proc(p: ^Stream_Protocol, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.state = .Reconnecting
	p.snapshot_seen = false
	p.last_seq = 0
	p.seq_gap_streak = 0
	p.desync_reason = .None
	p.getrange_pending = false
	p.resync_sent_ms = 0
	return Protocol_Transition{from = prev, to = .Reconnecting, reason = .Reconnect, timestamp = now_ms}
}

// protocol_on_resync_sent: called when a resync message is sent.
protocol_on_resync_sent :: proc(p: ^Stream_Protocol, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.state = .Resyncing
	p.resync_sent_ms = now_ms
	return Protocol_Transition{from = prev, to = .Resyncing, reason = .Resync_Sent, timestamp = now_ms}
}

// protocol_on_resync_ack: called when server ACKs a resync.
protocol_on_resync_ack :: proc(p: ^Stream_Protocol, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.state = .Seeded
	p.resync_sent_ms = 0
	p.snapshot_seen = false
	p.seq_gap_streak = 0
	p.desync_reason = .None
	return Protocol_Transition{from = prev, to = .Seeded, reason = .Resync_Ack, timestamp = now_ms}
}

// protocol_on_snapshot_gap: called when orderbook snapshot gate detects a gap.
protocol_on_snapshot_gap :: proc(p: ^Stream_Protocol, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.state = .Degraded
	p.desync_reason = .Snapshot_Gap
	return Protocol_Transition{from = prev, to = .Degraded, reason = .Snapshot_Gap, timestamp = now_ms}
}

// protocol_on_tf_change: called when the timeframe changes (for TF-sensitive artifacts).
protocol_on_tf_change :: proc(p: ^Stream_Protocol, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.state = .Bootstrap_Pending
	p.getrange_seeded = false
	p.getrange_pending = false
	p.getrange_oldest_ts = 0
	p.snapshot_seen = false
	p.last_seq = 0
	p.seq_gap_streak = 0
	p.desync_reason = .None
	p.event_count = 0
	return Protocol_Transition{from = prev, to = .Bootstrap_Pending, reason = .TF_Change, timestamp = now_ms}
}

// protocol_check_stale: pure check — returns true if stream should transition to Stale.
// Does NOT mutate state. Caller should call protocol_on_stale_timeout if true.
protocol_check_stale :: proc(p: Stream_Protocol, now_ms: i64, threshold_ms: i64) -> bool {
	if p.state == .Idle || p.state == .Stale || p.state == .Reconnecting do return false
	if p.last_event_ts_ms <= 0 do return false
	if threshold_ms <= 0 do return false
	return now_ms - p.last_event_ts_ms > threshold_ms
}

// protocol_on_stale_timeout: transition to Stale.
protocol_on_stale_timeout :: proc(p: ^Stream_Protocol, now_ms: i64) -> Protocol_Transition {
	prev := p.state
	p.state = .Stale
	return Protocol_Transition{from = prev, to = .Stale, reason = .Stale_Timeout, timestamp = now_ms}
}

// protocol_is_accepting_events: returns true if the stream can accept data events.
protocol_is_accepting_events :: proc(state: Stream_Protocol_State) -> bool {
	switch state {
	case .Idle, .Reconnecting:
		return false
	case .Bootstrap_Pending, .Seeded, .Live, .Degraded, .Resyncing, .Stale:
		return true
	}
	return false
}

// protocol_needs_resync: returns true if the stream is in a state that needs resync.
protocol_needs_resync :: proc(p: Stream_Protocol) -> bool {
	return p.state == .Degraded && p.desync_reason != .None
}

// --- Snapshot gate (generic, policy-driven) ---

// snapshot_gate_check evaluates whether an event should be accepted based on
// the artifact's snapshot policy and current protocol state.
// Returns: (accept: bool, snapshot_gap: bool)
snapshot_gate_check :: proc(
	policy: Artifact_Policy,
	snapshot_seen: bool,
	is_snapshot: bool,
	has_data: bool,   // For orderbook: ask_count>0 && bid_count>0
) -> (accept: bool, snapshot_gap: bool) {
	if !policy.needs_snapshot_gate {
		return true, false
	}
	if snapshot_seen {
		return true, false
	}
	if is_snapshot {
		return true, false
	}
	// Accept non-empty bootstrap delta (venues send valid data before explicit snapshot).
	if has_data {
		return true, false
	}
	return false, true
}
