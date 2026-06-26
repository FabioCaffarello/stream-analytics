package md_common

import "mr:ports"

// Stream_Apply_State tracks what a stream has received and what stores hold.
// One instance per stream view slot. Single source of truth for snapshot gates,
// live data flags, and synthetic fallback state. Active_Stream_Metrics booleans
// are derived from this via apply_state_sync_to_metrics (S24 cutover).
Stream_Apply_State :: struct {
	// Snapshot gates — per artifact, has first snapshot been seen?
	snapshot_seen:      [Artifact_Kind]bool,

	// Live data flags — has this stream received live (non-synthetic) data?
	has_live:           [Artifact_Kind]bool,

	// Synthetic fallback active — is synthetic data being used?
	using_synthetic:    [Artifact_Kind]bool,

	// Timestamps — last event received per artifact (local ms)
	last_recv_ms:       [Artifact_Kind]i64,

	// S25: Per-artifact event counts for observability.
	artifact_event_count: [Artifact_Kind]u64,

	// GetRange state per stream
	getrange_seeded:    bool,
	getrange_pending:   bool,
	getrange_oldest_ts: i64,
	getrange_sent_frame: u64,

	// S25: Candle subject ID for TF-scoped range guard.
	// Set when getrange is requested, used to reject stale batches from wrong TF.
	range_candle_subject_id: u64,

	// S34: Per-request correlation ID for GetRange.
	// Set when a GetRange request is sent, cleared on completion/timeout/invalidation.
	// Moved from GetRange_Global_State.subject_id to canonical apply state.
	getrange_request_id: u64,

	// Heatmap dedup: last synthetic heatmap window applied
	synth_heatmap_last_window: i64,

	// Permanent snapshot latch — set when a snapshot is first seen, never cleared by
	// recovery or reconnect (unlike snapshot_seen). Used to distinguish "never had data"
	// (first connect) from "had data but recovery cleared the gate" (resubscribe during recovery).
	snapshot_ever_seen: [Artifact_Kind]bool,

	// Total events applied (for health tracking)
	event_count:        u64,

	// S138: Bootstrap timing probe — time of very first event on this stream.
	// Set once (latched), never reset except on full apply_state_reset.
	// Used for time-to-first-data telemetry: first_event_ms - subscribe_ms.
	first_event_ms:     i64,

	// S29: Auto-recovery tracking — cooldown + attempt counter.
	recovery_last_ms:   i64,   // Timestamp of last auto-recovery attempt
	recovery_attempts:  u8,    // Consecutive auto-recovery attempts since last success
}

// =========================================================================
// S144: Snapshot Lifecycle — canonical per-stream snapshot status.
// Replaces the ambiguous combination of snapshot_seen + has_live + composition
// with a single ordered enum that UI/health can consume directly.
//
// Lifecycle ordering (worst → best):
//   Absent       — no data ever received (event_count == 0)
//   Pending      — subscribed, events may be flowing, but snapshot-gated
//                  artifacts have not yet seen their first snapshot
//   Degraded     — had snapshot but recovery is active or synthetic fallback in use
//   Stale        — had snapshot but recovery exhausted or data unrecoverably stale
//   Live         — all snapshot gates satisfied, data flowing normally
// =========================================================================

Snapshot_Lifecycle :: enum u8 {
	Absent,      // No data ever received on this stream
	Pending,     // Awaiting first snapshot for gated artifacts (e.g., Orderbook)
	Degraded,    // Snapshot was seen but stream is recovering or using synthetic
	Stale,       // Snapshot was seen but recovery exhausted — data untrustworthy
	Live,        // All gates satisfied, data flowing normally
}

// apply_state_snapshot_lifecycle derives the canonical snapshot lifecycle for
// a stream. Pure function — no mutation, no allocations.
//
// Decision flow:
//   1. event_count == 0 → Absent
//   2. Recovery exhausted → Stale
//   3. Any snapshot-gated artifact unsatisfied AND never seen before → Pending
//      (if gate was cleared by recovery but artifact had data before → fall through to Degraded)
//   4. Recovery in progress or any synthetic fallback active → Degraded
//   5. Otherwise → Live
apply_state_snapshot_lifecycle :: proc(s: Stream_Apply_State) -> Snapshot_Lifecycle {
	if s.event_count == 0 do return .Absent

	// Recovery exhausted → data untrustworthy (takes priority over pending gates,
	// since recovery itself clears snapshot gates for reconnect-sensitive artifacts).
	if s.recovery_attempts >= RECOVERY_MAX_ATTEMPTS do return .Stale

	// Check if any snapshot-gated artifact is still unsatisfied.
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		if policy.needs_snapshot_gate && !s.snapshot_seen[kind] {
			// If recovery cleared this gate but we had data before, fall through to
			// Degraded rather than blocking as Pending (data existed, just needs refresh).
			if s.snapshot_ever_seen[kind] && s.recovery_attempts > 0 {
				continue
			}
			return .Pending
		}
	}

	// Recovery in progress → degraded.
	if s.recovery_attempts > 0 do return .Degraded

	// Any synthetic fallback active → degraded (live data preferred but absent).
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		if policy.has_synthetic_fallback && s.using_synthetic[kind] && !s.has_live[kind] {
			return .Degraded
		}
	}

	return .Live
}

// snapshot_lifecycle_label returns a display label for the snapshot lifecycle.
// All returned strings are compile-time literals.
snapshot_lifecycle_label :: proc(sl: Snapshot_Lifecycle) -> string {
	switch sl {
	case .Absent:   return "ABSENT"
	case .Pending:  return "PENDING"
	case .Degraded: return "DEGRADED"
	case .Stale:    return "STALE"
	case .Live:     return "OK"
	}
	return "UNKNOWN"
}

// snapshot_lifecycle_blocks_render returns true if the snapshot lifecycle
// indicates that widgets should NOT render data (nothing to show).
// .Stale does NOT block render — last-known data is shown with a stale overlay.
snapshot_lifecycle_blocks_render :: proc(sl: Snapshot_Lifecycle) -> bool {
	switch sl {
	case .Absent, .Pending:
		return true
	case .Stale, .Degraded, .Live:
		return false
	}
	return true
}

// apply_state_reset resets all apply state for a fresh stream.
// Called on subscribe or when stream is completely reset.
apply_state_reset :: proc(s: ^Stream_Apply_State) {
	s^ = {}
}

// apply_state_on_reconnect resets reconnect-sensitive state per artifact policy.
apply_state_on_reconnect :: proc(s: ^Stream_Apply_State) {
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		if policy.reset_on_reconnect {
			s.snapshot_seen[kind] = false
		}
	}
	s.getrange_pending = false
	s.getrange_sent_frame = 0
	s.getrange_request_id = 0
	// S29: Clear recovery state — transport reconnect is already a recovery action.
	s.recovery_attempts = 0
	s.recovery_last_ms = 0
}

// apply_state_on_tf_change resets TF-sensitive state per artifact policy.
apply_state_on_tf_change :: proc(s: ^Stream_Apply_State) {
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		if policy.reset_on_tf_change {
			s.snapshot_seen[kind] = false
			s.has_live[kind] = false
			s.using_synthetic[kind] = false
			s.last_recv_ms[kind] = 0
		}
	}
	s.getrange_seeded = false
	s.getrange_pending = false
	s.getrange_oldest_ts = 0
	s.getrange_sent_frame = 0
	s.range_candle_subject_id = 0
	s.getrange_request_id = 0
	s.synth_heatmap_last_window = 0
	// S29: Clear recovery state — TF change resubscribes everything.
	s.recovery_attempts = 0
	s.recovery_last_ms = 0
}

// apply_state_mark_event records that an event of a given kind was received.
apply_state_mark_event :: proc(s: ^Stream_Apply_State, kind: Artifact_Kind, now_ms: i64, is_snapshot: bool) {
	s.last_recv_ms[kind] = now_ms
	s.event_count += 1
	s.artifact_event_count[kind] += 1
	// S138: Latch first event timestamp for bootstrap timing telemetry.
	if s.first_event_ms <= 0 && now_ms > 0 {
		s.first_event_ms = now_ms
	}

	policy := artifact_policies[kind]
	if is_snapshot || !policy.needs_snapshot_gate {
		s.snapshot_seen[kind] = true
		s.snapshot_ever_seen[kind] = true  // permanent latch — never cleared by recovery
	}

	// Live data displaces synthetic fallback.
	if policy.has_synthetic_fallback {
		s.has_live[kind] = true
		s.using_synthetic[kind] = false
	} else {
		s.has_live[kind] = true
	}
}

// apply_state_mark_synthetic records that synthetic data was generated for an artifact.
apply_state_mark_synthetic :: proc(s: ^Stream_Apply_State, kind: Artifact_Kind, now_ms: i64) {
	if s.has_live[kind] do return  // live data takes precedence
	s.using_synthetic[kind] = true
	s.last_recv_ms[kind] = now_ms
}

// apply_state_mark_range_sent records that a GetRange request was sent.
// S34: request_id parameter added for per-request correlation (was getrange.subject_id).
apply_state_mark_range_sent :: proc(s: ^Stream_Apply_State, frame: u64, candle_subject_id: u64 = 0) {
	s.getrange_pending = true
	s.getrange_sent_frame = frame
	if candle_subject_id != 0 {
		s.range_candle_subject_id = candle_subject_id
		s.getrange_request_id = candle_subject_id
	}
}

// apply_state_mark_range_complete records that a GetRange batch completed.
apply_state_mark_range_complete :: proc(s: ^Stream_Apply_State, oldest_ts: i64) {
	s.getrange_seeded = true
	s.getrange_pending = false
	s.getrange_request_id = 0
	if oldest_ts > 0 && (s.getrange_oldest_ts <= 0 || oldest_ts < s.getrange_oldest_ts) {
		s.getrange_oldest_ts = oldest_ts
	}
}

// apply_state_check_getrange_timeout returns true if the pending GetRange has timed out.
apply_state_check_getrange_timeout :: proc(s: Stream_Apply_State, current_frame: u64, timeout_frames: u64) -> bool {
	if !s.getrange_pending do return false
	return current_frame > s.getrange_sent_frame + timeout_frames
}

// apply_state_should_use_synthetic returns true if synthetic fallback should be used.
// True when: artifact supports fallback AND no live data has been received.
apply_state_should_use_synthetic :: proc(s: Stream_Apply_State, kind: Artifact_Kind) -> bool {
	policy := artifact_policies[kind]
	if !policy.has_synthetic_fallback do return false
	return !s.has_live[kind]
}

// apply_state_needs_snapshot returns true if this artifact still needs its first snapshot.
apply_state_needs_snapshot :: proc(s: Stream_Apply_State, kind: Artifact_Kind) -> bool {
	policy := artifact_policies[kind]
	if !policy.needs_snapshot_gate do return false
	return !s.snapshot_seen[kind]
}

// S33: Canonical candle feed timing — returns the most recent candle-related
// timestamp, considering both live candle events and historical range data.
// This converges the former candle_last_recv_local_ms (ad-hoc, 7 write sites)
// into a single derived query from canonical apply state. Pure function.
apply_state_candle_recv_ms :: proc(s: Stream_Apply_State) -> i64 {
	live := s.last_recv_ms[.Candle]
	hist := s.last_recv_ms[.Range_Candle]
	if live >= hist do return live
	return hist
}

// apply_state_is_range_ready returns true when the stream has received historical
// seed data AND at least one live candle event — i.e., the historical/realtime
// composition is complete and the candle store is coherent.
apply_state_is_range_ready :: proc(s: Stream_Apply_State) -> bool {
	return s.getrange_seeded && s.has_live[.Candle]
}

// apply_state_composition_stage returns the current historical/realtime composition
// stage for observability. Pure function.
apply_state_composition_stage :: proc(s: Stream_Apply_State) -> Composition_Stage {
	has_seed := s.getrange_seeded
	has_live := s.has_live[.Candle]
	if has_seed && has_live do return .Composed
	if has_seed && !has_live do return .Backfilled
	if !has_seed && has_live do return .Live_Only
	if s.getrange_pending do return .Range_Pending
	return .Empty
}

Composition_Stage :: enum u8 {
	Empty,          // No data at all
	Range_Pending,  // GetRange in flight
	Backfilled,     // Historical data received, no live yet
	Live_Only,      // Live candles but no historical backfill
	Composed,       // Both historical + live — fully coherent
}

// S26: Per-cell composition stage — derives composition from a cell's getrange
// state combined with its stream slot's live candle status. Pure function.
cell_composition_stage :: proc(
	getrange_pending: bool,
	getrange_seeded: bool,
	has_live_candle: bool,
) -> Composition_Stage {
	if getrange_seeded && has_live_candle do return .Composed
	if getrange_seeded && !has_live_candle do return .Backfilled
	if !getrange_seeded && has_live_candle do return .Live_Only
	if getrange_pending do return .Range_Pending
	return .Empty
}

// --- Derive aggregate flags for compatibility ---
// These match the existing active_metrics booleans for gradual migration.

Apply_State_Summary :: struct {
	has_live_stats:    bool,
	has_live_candle:   bool,
	has_live_heatmap:  bool,
	has_live_vpvr:     bool,
	snapshot_seen:     bool,  // orderbook
	getrange_seeded:   bool,
	composition_stage: Composition_Stage,
	// S27: Per-artifact event counts + total for telemetry.
	artifact_event_count: [Artifact_Kind]u64,
	event_count:          u64,
}

apply_state_summary :: proc(s: Stream_Apply_State) -> Apply_State_Summary {
	return Apply_State_Summary{
		has_live_stats    = s.has_live[.Stats],
		has_live_candle   = s.has_live[.Candle],
		has_live_heatmap  = s.has_live[.Heatmap],
		has_live_vpvr     = s.has_live[.VPVR],
		snapshot_seen     = s.snapshot_seen[.Orderbook],
		getrange_seeded   = s.getrange_seeded,
		composition_stage = apply_state_composition_stage(s),
		artifact_event_count = s.artifact_event_count,
		event_count       = s.event_count,
	}
}

// S27: Telemetry diagnostics view — derived from apply state, zero new state.
// Per-artifact counts, last_recv_ms, live/synthetic flags, composition.
Apply_State_Telemetry :: struct {
	artifact_event_count: [Artifact_Kind]u64,
	last_recv_ms:         [Artifact_Kind]i64,
	has_live:             [Artifact_Kind]bool,
	using_synthetic:      [Artifact_Kind]bool,
	event_count:          u64,
	composition_stage:    Composition_Stage,
	getrange_pending:     bool,
	getrange_seeded:      bool,
	// S29: Recovery diagnostics.
	recovery_status:      Recovery_Status,
	recovery_attempts:    u8,
	// S30: Adaptive cooldown diagnostics.
	recovery_cooldown_ms:          i64,  // Current cooldown window for this attempt level
	recovery_cooldown_remaining_ms: i64, // Time remaining before next attempt allowed
	// S35: Per-stream health level.
	stream_health:                 System_Health_Level,
	// S143: Canonical stream reliability.
	reliability:                   Stream_Reliability,
	// S138: Bootstrap timing probe.
	first_event_ms:                i64,   // Latched timestamp of very first event
	// S144: Canonical snapshot lifecycle.
	snapshot_lifecycle:             Snapshot_Lifecycle,
}

// apply_state_telemetry returns a telemetry snapshot for diagnostics display.
// Pure function — reads apply state, creates no new state.
// S30: now_ms parameter added for cooldown remaining computation.
// S35: tf_ms parameter added for per-stream health level derivation.
// S143: is_desync/is_offline parameters added for reliability derivation.
apply_state_telemetry :: proc(s: Stream_Apply_State, now_ms: i64 = 0, tf_ms: i64 = 0, is_desync: bool = false, is_offline: bool = false) -> Apply_State_Telemetry {
	cooldown := recovery_cooldown_for_attempt(s.recovery_attempts)
	remaining := i64(0)
	if s.recovery_last_ms > 0 && now_ms > 0 {
		elapsed := now_ms - s.recovery_last_ms
		if elapsed < cooldown do remaining = cooldown - elapsed
	}
	health := stream_health_level(s, now_ms, tf_ms)
	rec := apply_state_recovery_status(s)
	return Apply_State_Telemetry{
		artifact_event_count = s.artifact_event_count,
		last_recv_ms         = s.last_recv_ms,
		has_live             = s.has_live,
		using_synthetic      = s.using_synthetic,
		event_count          = s.event_count,
		composition_stage    = apply_state_composition_stage(s),
		getrange_pending     = s.getrange_pending,
		getrange_seeded      = s.getrange_seeded,
		recovery_status      = rec,
		recovery_attempts    = s.recovery_attempts,
		recovery_cooldown_ms = cooldown,
		recovery_cooldown_remaining_ms = remaining,
		stream_health        = health,
		reliability          = stream_reliability(health, rec, is_desync, is_offline),
		first_event_ms       = s.first_event_ms,
		snapshot_lifecycle   = apply_state_snapshot_lifecycle(s),
	}
}

// S27: Count of active artifacts (at least one event received). Pure function.
apply_state_active_artifact_count :: proc(s: Stream_Apply_State) -> int {
	count := 0
	for kind in Artifact_Kind {
		if s.artifact_event_count[kind] > 0 do count += 1
	}
	return count
}

// S28: Per-artifact staleness classification.
Artifact_Staleness :: enum u8 {
	Unknown,   // Never received (last_recv_ms == 0)
	Fresh,     // Within normal thresholds
	Aging,     // Approaching stale (warning zone)
	Stale,     // Exceeded stale threshold
}

// S28: Compute age in ms since last event for an artifact. Returns -1 if never received.
apply_state_artifact_age_ms :: proc(s: Stream_Apply_State, kind: Artifact_Kind, now_ms: i64) -> i64 {
	if s.last_recv_ms[kind] <= 0 do return -1
	if now_ms <= 0 do return -1
	age := now_ms - s.last_recv_ms[kind]
	if age < 0 do age = 0
	return age
}

// S28: Classify artifact staleness using policy-driven thresholds. Pure function.
// tf_ms is only used for TF_Adaptive stale detection (candle); ignored otherwise.
apply_state_artifact_staleness :: proc(s: Stream_Apply_State, kind: Artifact_Kind, now_ms: i64, tf_ms: i64 = 0) -> Artifact_Staleness {
	age := apply_state_artifact_age_ms(s, kind, now_ms)
	if age < 0 do return .Unknown

	policy := artifact_policies[kind]
	switch policy.stale_detection {
	case .None:
		return .Fresh
	case .TF_Adaptive:
		// Consistent with compute_candle_health thresholds.
		tf := tf_ms
		if tf <= 0 do tf = 60_000
		warn_ms := max(2 * tf, 5_000)
		stale_ms := max(3 * tf, 10_000)
		if age >= stale_ms do return .Stale
		if age >= warn_ms do return .Aging
		return .Fresh
	case .Dual_Silence:
		// Consistent with health.odin 12s dual-silence threshold.
		if age >= 12_000 do return .Stale
		if age >= 8_000 do return .Aging
		return .Fresh
	case .Sparse_Adaptive:
		// S47: Sparse/irregular feeds (OI). Updates can be 1/min on some exchanges.
		// 60s aging, 180s stale — avoids false-positive on naturally sparse data.
		if age >= 180_000 do return .Stale
		if age >= 60_000 do return .Aging
		return .Fresh
	}
	return .Fresh
}

// S28: Count artifacts in Stale or Aging state. Pure function.
// TF_Adaptive artifacts (Candle) are intentionally excluded from the stale bucket:
// low-volume markets can go minutes without a new candle — this is expected and
// not actionable via auto-recovery. Counting them as stale would produce
// Stream_Reliability.Stale_Unrecoverable with recovery_attempts==0, which shows
// "Data stale, auto-recovery exhausted" even though no recovery was ever attempted.
// Instead, TF_Adaptive stale is downgraded to aging → health becomes .Degraded →
// reliability becomes .Degraded_Aging → widget renders with a yellow "Aging" hint.
apply_state_stale_artifact_count :: proc(s: Stream_Apply_State, now_ms: i64, tf_ms: i64 = 0) -> (stale: int, aging: int) {
	for kind in Artifact_Kind {
		if s.artifact_event_count[kind] == 0 do continue
		policy := artifact_policies[kind]
		staleness := apply_state_artifact_staleness(s, kind, now_ms, tf_ms)
		switch staleness {
		case .Stale:
			if policy.stale_detection == .TF_Adaptive {
				aging += 1
			} else {
				stale += 1
			}
		case .Aging:
			aging += 1
		case .Fresh, .Unknown:
		}
	}
	return
}

// =========================================================================
// S29: Stale Auto-Recovery & Protocol-Driven Remediation.
// Pure decision functions for auto-recovery based on staleness detection.
// Recovery state lives in Stream_Apply_State (canonical source of truth).
// =========================================================================

RECOVERY_BASE_COOLDOWN_MS :: i64(15_000)  // 15s base cooldown (doubles per attempt)
RECOVERY_MAX_COOLDOWN_MS  :: i64(60_000)  // 60s ceiling for exponential backoff
RECOVERY_MAX_ATTEMPTS     :: u8(3)        // Max attempts before escalating to manual

// S144: Base GetRange timeout budget — 300 frames at 60fps = 5s.
// S152: This is now the base/default for Tick TFs. Higher TFs use longer
// timeouts via backfill_policy_for_tf() in tf_data_contract.odin.
// Callers should prefer backfill_policy_for_tf_ms(tf_ms).timeout_frames.
GETRANGE_TIMEOUT_FRAMES   :: u64(300)

// S30: Compute adaptive cooldown for the next recovery attempt.
// Exponential backoff: 15s, 30s, 60s (capped). Pure function.
recovery_cooldown_for_attempt :: proc(attempts: u8) -> i64 {
	if attempts == 0 do return RECOVERY_BASE_COOLDOWN_MS
	shift := min(u8(2), attempts)  // cap shift to avoid overflow
	cooldown := RECOVERY_BASE_COOLDOWN_MS << uint(shift)
	if cooldown > RECOVERY_MAX_COOLDOWN_MS do cooldown = RECOVERY_MAX_COOLDOWN_MS
	return cooldown
}

// Remediation_Decision is the pure output of the stale remediation check.
Remediation_Decision :: enum u8 {
	None,         // No remediation needed (all fresh or never received)
	Resubscribe,  // Auto-resubscribe: stale Dual_Silence artifacts detected
	Cooldown,     // Needs remediation but within cooldown window
	Exhausted,    // Max attempts reached — escalate to manual intervention
}

// Recovery_Status is the derived recovery state for diagnostics/HUD.
Recovery_Status :: enum u8 {
	None,        // No recovery in progress
	Recovering,  // Auto-recovery attempted, waiting for fresh data
	Exhausted,   // Max attempts reached, manual intervention needed
}

// apply_state_stale_remediation evaluates whether auto-recovery should fire.
// Only considers Dual_Silence artifacts (Orderbook, Stats) that were previously
// active (event_count > 0) and are now Stale. TF_Adaptive (Candle) staleness
// is surfaced but does NOT trigger auto-recovery (low-volume markets).
// Pure function — no mutation.
apply_state_stale_remediation :: proc(s: Stream_Apply_State, now_ms: i64, tf_ms: i64 = 0) -> Remediation_Decision {
	// Count Dual_Silence artifacts that are Stale.
	dual_stale := 0
	for kind in Artifact_Kind {
		if s.artifact_event_count[kind] == 0 do continue
		policy := artifact_policies[kind]
		if policy.stale_detection != .Dual_Silence do continue
		staleness := apply_state_artifact_staleness(s, kind, now_ms, tf_ms)
		if staleness == .Stale do dual_stale += 1
	}
	if dual_stale == 0 do return .None

	// Max attempts exhausted — escalate to manual.
	if s.recovery_attempts >= RECOVERY_MAX_ATTEMPTS do return .Exhausted

	// S30: Adaptive cooldown guard — exponential backoff prevents thrashing.
	cooldown := recovery_cooldown_for_attempt(s.recovery_attempts)
	if s.recovery_last_ms > 0 && now_ms - s.recovery_last_ms < cooldown {
		return .Cooldown
	}

	return .Resubscribe
}

// apply_state_mark_recovery records that an auto-recovery attempt was made.
// S144: Also resets snapshot gates for reconnect-sensitive artifacts, since
// recovery triggers a resubscribe which is semantically equivalent to a
// reconnect for those artifacts. Without this, stale orderbook data could
// be displayed after recovery resubscribe without a fresh snapshot.
apply_state_mark_recovery :: proc(s: ^Stream_Apply_State, now_ms: i64) {
	s.recovery_last_ms = now_ms
	s.recovery_attempts += 1
	// S144: Clear snapshot gates for reconnect-sensitive artifacts.
	for kind in Artifact_Kind {
		policy := artifact_policies[kind]
		if policy.reset_on_reconnect {
			s.snapshot_seen[kind] = false
		}
	}
}

// apply_state_check_recovery_success checks if previously stale Dual_Silence
// artifacts are now fresh, and if so, resets the recovery counter.
// Called per-frame after events are drained. Pure check + conditional mutation.
apply_state_check_recovery_success :: proc(s: ^Stream_Apply_State, now_ms: i64, tf_ms: i64 = 0) {
	if s.recovery_attempts == 0 do return

	// Check if all Dual_Silence artifacts that were active are now Fresh.
	for kind in Artifact_Kind {
		if s.artifact_event_count[kind] == 0 do continue
		policy := artifact_policies[kind]
		if policy.stale_detection != .Dual_Silence do continue
		staleness := apply_state_artifact_staleness(s^, kind, now_ms, tf_ms)
		if staleness == .Stale || staleness == .Aging do return  // still degraded
	}

	// All clear — recovery succeeded.
	s.recovery_attempts = 0
	s.recovery_last_ms = 0
}

// apply_state_recovery_status derives the current recovery status for display.
// Pure function — reads apply state fields only.
apply_state_recovery_status :: proc(s: Stream_Apply_State) -> Recovery_Status {
	if s.recovery_attempts == 0 do return .None
	if s.recovery_attempts >= RECOVERY_MAX_ATTEMPTS do return .Exhausted
	return .Recovering
}

// =========================================================================
// S143: Stream Reliability — unified gate for "can this stream's data be trusted?"
// Combines transport state, delivery health, and recovery status into a
// single canonical enum that widgets consult before rendering.
//
// Ownership hierarchy:
//   Transport   → Stream_State (Offline/Live/Lag/Desync) — stream_controller.odin
//   Delivery    → Composition_Stage (Empty..Composed) — apply state
//   Health      → System_Health_Level (Healthy..Critical) — apply state
//   Reliability → Stream_Reliability (below) — derived from all three
//
// Design: pure function, no mutation, deterministic. Evaluated per-frame
// in resolve_cell_surface_view and passed through to widget_data_readiness.
// =========================================================================

Stream_Reliability :: enum u8 {
	Reliable,              // Transport live, data fresh, composition valid
	Degraded_Aging,        // Transport OK but some artifacts aging — render with warning
	Stale_Recovering,      // Stale detected, auto-recovery in progress — render with warning
	Stale_Unrecoverable,   // Recovery exhausted — data untrustworthy, block render
	Desync,                // Transport desync detected — data untrustworthy, block render
	Offline,               // Transport disconnected — block render
	Manual_Resync,         // Recovery exhausted + manual intervention required
}

// S143: stream_reliability derives the canonical reliability level.
// Pure function. Inputs: transport state, health level, recovery status, connectivity.
// The is_desync/is_offline parameters come from the transport layer (Stream_State/Stream_Status).
stream_reliability :: proc(
	health: System_Health_Level,
	recovery: Recovery_Status,
	is_desync: bool,
	is_offline: bool,
) -> Stream_Reliability {
	if is_offline do return .Offline
	if is_desync {
		if recovery == .Exhausted do return .Manual_Resync
		return .Desync
	}
	if recovery == .Exhausted do return .Manual_Resync
	switch health {
	case .Critical:  return .Stale_Unrecoverable
	case .Unhealthy:
		if recovery == .Recovering do return .Stale_Recovering
		return .Stale_Unrecoverable
	case .Degraded:
		if recovery == .Recovering do return .Stale_Recovering
		return .Degraded_Aging
	case .Healthy:   return .Reliable
	}
	return .Reliable
}

// S143: stream_reliability_blocks_render returns true if the reliability
// level indicates that widget data should NOT be rendered as trustworthy.
// Widgets should show an overlay or degraded state instead.
stream_reliability_blocks_render :: proc(r: Stream_Reliability) -> bool {
	switch r {
	case .Reliable, .Degraded_Aging, .Stale_Recovering:
		return false
	case .Stale_Unrecoverable, .Desync, .Offline, .Manual_Resync:
		return true
	}
	return false
}

// S143: stream_reliability_label returns a display label for the reliability level.
// All returned strings are compile-time literals.
stream_reliability_label :: proc(r: Stream_Reliability) -> string {
	switch r {
	case .Reliable:            return "OK"
	case .Degraded_Aging:      return "AGING"
	case .Stale_Recovering:    return "RECOVERING"
	case .Stale_Unrecoverable: return "STALE"
	case .Desync:              return "DESYNC"
	case .Offline:             return "OFFLINE"
	case .Manual_Resync:       return "RESYNC"
	}
	return "UNKNOWN"
}

// S35: Per-stream health level — derives System_Health_Level for a single stream.
// Pure function — no mutation, no allocations.
stream_health_level :: proc(s: Stream_Apply_State, now_ms: i64, tf_ms: i64 = 0) -> System_Health_Level {
	if s.event_count == 0 do return .Healthy  // no data yet, not degraded

	stale, aging := apply_state_stale_artifact_count(s, now_ms, tf_ms)
	rec := apply_state_recovery_status(s)

	if stale >= 2 && rec == .Exhausted do return .Critical
	if stale > 0 || rec == .Exhausted do return .Unhealthy
	if aging > 0 || rec == .Recovering do return .Degraded
	return .Healthy
}

// S35: Health_Tick_Input — snapshot of state needed for per-frame health evaluation.
// Passed into the pure control-plane function to avoid coupling to App_State.
Health_Tick_Input :: struct {
	apply_state:       Stream_Apply_State,
	now_ms:            i64,
	tf_ms:             i64,
	is_connected:      bool,
	is_offline:        bool,  // active_metrics.state == .Offline
	is_desync:         bool,  // S143: active_metrics.state == .Desync
}

// S35: Health_Tick_Output — all health decisions for one frame tick.
// Pure output of the control-plane evaluation. The caller applies side effects.
Health_Tick_Output :: struct {
	remediation:       Remediation_Decision,
	recovery_success:  bool,   // true if recovery counter was reset (stale cleared)
	stream_health:     System_Health_Level,
	stale_count:       int,
	aging_count:       int,
	// S143: Canonical reliability derived from health + recovery + transport.
	reliability:       Stream_Reliability,
	// S144: Canonical snapshot lifecycle.
	snapshot_lifecycle: Snapshot_Lifecycle,
}

// S35: health_tick_evaluate — pure control-plane function for per-frame health.
// Returns what to do; caller applies side effects (resubscribe, log, sync).
// Deterministic: same input always produces same output. No mutation.
health_tick_evaluate :: proc(input: Health_Tick_Input) -> Health_Tick_Output {
	out: Health_Tick_Output
	s := input.apply_state

	out.stream_health = stream_health_level(s, input.now_ms, input.tf_ms)
	out.stale_count, out.aging_count = apply_state_stale_artifact_count(s, input.now_ms, input.tf_ms)
	out.snapshot_lifecycle = apply_state_snapshot_lifecycle(s)

	// S143: Derive canonical reliability from health + recovery + transport.
	rec := apply_state_recovery_status(s)
	out.reliability = stream_reliability(out.stream_health, rec, input.is_desync, input.is_offline)

	// Recovery decisions only when connected and not offline.
	if input.now_ms > 0 && input.is_connected && !input.is_offline {
		out.remediation = apply_state_stale_remediation(s, input.now_ms, input.tf_ms)

		// Check if recovery succeeded (requires pre-mutation state).
		if s.recovery_attempts > 0 {
			// Simulate check: all active Dual_Silence artifacts must be Fresh.
			all_fresh := true
			for kind in Artifact_Kind {
				if s.artifact_event_count[kind] == 0 do continue
				policy := artifact_policies[kind]
				if policy.stale_detection != .Dual_Silence do continue
				staleness := apply_state_artifact_staleness(s, kind, input.now_ms, input.tf_ms)
				if staleness == .Stale || staleness == .Aging {
					all_fresh = false
					break
				}
			}
			out.recovery_success = all_fresh
		}
	}

	return out
}

// =========================================================================
// S34: Historical/Realtime Composition Orchestrator.
// Pure decision functions for the composition lifecycle:
//   Empty → Seed_Range → Await_Seed → Backfilled/Await_Live → Steady
// These centralize the scattered guard logic for GetRange requests
// and lazy loading into testable, deterministic orchestrator decisions.
// =========================================================================

// Orchestrator_Phase — lifecycle phase of the composition system.
Orchestrator_Phase :: enum u8 {
	None,         // No action possible (no active stream)
	Seed_Range,   // Need initial GetRange request
	Await_Seed,   // GetRange in flight, wait for response
	Await_Live,   // Backfilled, waiting for first live candle
	Steady,       // Composed — historical + live coherent
}

// composition_intent returns what the orchestrator should do next for the
// active stream's composition lifecycle. Pure function — no mutation.
composition_intent :: proc(
	s: Stream_Apply_State,
	store_count: int,
	has_active_stream: bool,
) -> Orchestrator_Phase {
	if !has_active_stream do return .None
	if s.getrange_pending do return .Await_Seed
	has_seed := s.getrange_seeded
	has_live := s.has_live[.Candle]
	if has_seed && has_live do return .Steady
	if has_seed && !has_live do return .Await_Live
	// No seed yet — request one (regardless of store_count, Live_Only should also seed).
	if store_count <= 0 || !has_seed do return .Seed_Range
	return .None
}

// composition_should_extend returns true if the orchestrator should request
// older candles (lazy loading). Encapsulates all guard conditions.
// Pure function — no mutation.
composition_should_extend :: proc(
	s: Stream_Apply_State,
	store_count: int,
	store_cap: int,
	timeline_first_ts: i64,
	timeline_loaded: bool,
) -> bool {
	if s.getrange_pending do return false
	if !s.getrange_seeded do return false
	if s.getrange_oldest_ts <= 0 do return false
	if store_count >= store_cap do return false
	if timeline_loaded && timeline_first_ts > 0 && s.getrange_oldest_ts <= timeline_first_ts {
		return false
	}
	return true
}

// =========================================================================
// S31: Recovery Observability & Aggregate Health Dashboard.
// Aggregate health computation across all active stream slots, plus
// a ring-buffer recovery event log for diagnostics.
// =========================================================================

System_Health_Level :: enum u8 {
	Healthy,     // All slots composed, no stale, no recovery
	Degraded,    // Some aging or recovering
	Unhealthy,   // Any stale or exhausted
	Critical,    // Multiple stale + exhausted
}

AGGREGATE_HEALTH_MAX_SLOTS :: 32

Aggregate_Health_Summary :: struct {
	slot_count:        int,    // total used slots
	slots_composed:    int,    // slots at Composed stage
	slots_live_only:   int,    // slots at Live_Only
	slots_backfilled:  int,    // slots at Backfilled
	slots_pending:     int,    // slots at Range_Pending
	slots_empty:       int,    // slots at Empty
	slots_recovering:  int,    // slots with recovery_attempts > 0 and < max
	slots_exhausted:   int,    // slots with recovery_attempts >= max
	total_stale:       int,    // total stale artifacts across all slots
	total_aging:       int,    // total aging artifacts across all slots
	total_event_count: u64,    // sum of event_count across all slots
	worst_composition: Composition_Stage,   // worst stage across slots
	worst_staleness:   Artifact_Staleness,  // worst staleness across all
	health_level:      System_Health_Level,
	// S144: Snapshot lifecycle counts across slots.
	slots_snapshot_absent:   int,   // slots with Snapshot_Lifecycle == .Absent
	slots_snapshot_pending:  int,   // slots with Snapshot_Lifecycle == .Pending
	slots_snapshot_degraded: int,   // slots with Snapshot_Lifecycle == .Degraded
	slots_snapshot_stale:    int,   // slots with Snapshot_Lifecycle == .Stale
	slots_snapshot_live:     int,   // slots with Snapshot_Lifecycle == .Live
	worst_snapshot:          Snapshot_Lifecycle,  // worst lifecycle across all slots
}

// aggregate_health_from_slots computes an aggregate health summary across all
// active stream slots. Pure function — no mutation, no allocations.
// states and used must be parallel slices of equal length.
aggregate_health_from_slots :: proc(
	states: []Stream_Apply_State,
	used: []bool,
	now_ms: i64,
	tf_ms: i64,
) -> Aggregate_Health_Summary {
	summary: Aggregate_Health_Summary
	summary.worst_composition = .Composed  // start at best, degrade downward
	summary.worst_snapshot = .Live          // S144: start at best, degrade downward

	n := min(len(states), len(used))
	for i := 0; i < n; i += 1 {
		if !used[i] do continue

		s := states[i]
		summary.slot_count += 1
		summary.total_event_count += s.event_count

		// Composition stage
		comp := apply_state_composition_stage(s)
		switch comp {
		case .Empty:        summary.slots_empty += 1
		case .Range_Pending: summary.slots_pending += 1
		case .Backfilled:   summary.slots_backfilled += 1
		case .Live_Only:    summary.slots_live_only += 1
		case .Composed:     summary.slots_composed += 1
		}

		// worst_composition: lowest enum ordinal is worst (Empty < Composed)
		if int(comp) < int(summary.worst_composition) {
			summary.worst_composition = comp
		}

		// Staleness counts
		stale, aging := apply_state_stale_artifact_count(s, now_ms, tf_ms)
		summary.total_stale += stale
		summary.total_aging += aging

		// Per-artifact worst staleness
		for kind in Artifact_Kind {
			if s.artifact_event_count[kind] == 0 do continue
			staleness := apply_state_artifact_staleness(s, kind, now_ms, tf_ms)
			if int(staleness) > int(summary.worst_staleness) {
				summary.worst_staleness = staleness
			}
		}

		// Recovery status
		rec := apply_state_recovery_status(s)
		switch rec {
		case .Recovering: summary.slots_recovering += 1
		case .Exhausted:  summary.slots_exhausted += 1
		case .None:       // nothing
		}

		// S144: Snapshot lifecycle counts
		sl := apply_state_snapshot_lifecycle(s)
		switch sl {
		case .Absent:   summary.slots_snapshot_absent += 1
		case .Pending:  summary.slots_snapshot_pending += 1
		case .Degraded: summary.slots_snapshot_degraded += 1
		case .Stale:    summary.slots_snapshot_stale += 1
		case .Live:     summary.slots_snapshot_live += 1
		}
		// worst_snapshot: lowest enum ordinal is worst (Absent < Live)
		if int(sl) < int(summary.worst_snapshot) {
			summary.worst_snapshot = sl
		}
	}

	// If no slots used, worst_composition should be Empty
	if summary.slot_count == 0 {
		summary.worst_composition = .Empty
		summary.worst_snapshot = .Absent
	}

	// Derive health level
	if summary.total_stale >= 2 && summary.slots_exhausted > 0 {
		summary.health_level = .Critical
	} else if summary.total_stale > 0 || summary.slots_exhausted > 0 {
		summary.health_level = .Unhealthy
	} else if summary.total_aging > 0 || summary.slots_recovering > 0 {
		summary.health_level = .Degraded
	} else {
		summary.health_level = .Healthy
	}

	return summary
}

// =========================================================================
// S31: Recovery Event Log — ring buffer for recovery event diagnostics.
// =========================================================================

RECOVERY_EVENT_LOG_CAP :: 16

Recovery_Event_Kind :: enum u8 {
	Attempt,    // Auto-recovery fired
	Success,    // Recovery succeeded (stale cleared)
	Exhausted,  // Max attempts reached
	Reset,      // Recovery counter cleared (reconnect/TF change)
}

Recovery_Event :: struct {
	kind:      Recovery_Event_Kind,
	timestamp: i64,
	attempts:  u8,
	slot_id:   u8,   // index into slots array for identification
}

Recovery_Event_Log :: struct {
	events: [RECOVERY_EVENT_LOG_CAP]Recovery_Event,
	head:   int,
	count:  int,
}

// recovery_event_log_push appends an event to the ring buffer, overwriting
// the oldest entry when full.
recovery_event_log_push :: proc(log: ^Recovery_Event_Log, evt: Recovery_Event) {
	log.events[log.head] = evt
	log.head = (log.head + 1) % RECOVERY_EVENT_LOG_CAP
	if log.count < RECOVERY_EVENT_LOG_CAP {
		log.count += 1
	}
}

// recovery_event_log_get retrieves an event by index (0 = newest). Returns
// the event and true on success, or a zero event and false if out of range.
recovery_event_log_get :: proc(log: ^Recovery_Event_Log, idx: int) -> (Recovery_Event, bool) {
	if idx < 0 || idx >= log.count do return {}, false
	actual := (log.head - 1 - idx + RECOVERY_EVENT_LOG_CAP) % RECOVERY_EVENT_LOG_CAP
	return log.events[actual], true
}
