# Stage 35 — Recovery & Health Control Plane

**Date:** 2026-03-07
**Branch:** codex/s9-legacy-removal-cutover
**Tests:** 242 (up from ~230), 12 new S35 tests

---

## Executive Summary

S35 formalizes the recovery/health system as a **pure control plane** — centralizing per-frame health decisions into a single testable function, adding per-stream health level derivation, and completing the recovery event audit trail by emitting `Reset` events on reconnect/TF change.

**Key principle:** Zero new mutable state. All additions are pure functions deriving from canonical `Stream_Apply_State`.

---

## Operational Audit

Thorough audit of all recovery, health, reconnect, resync, and remediation paths confirmed:

| Component | Status Pre-S35 | Gap Found |
|-----------|---------------|-----------|
| Recovery state (apply_state) | 100% canonical | None |
| Remediation decisions | Pure functions | None |
| Cooldown/backoff | Exponential 15s→30s→60s | None |
| Recovery event log | Ring buffer cap=16 | **`Reset` events never emitted** |
| Per-stream health | Staleness + composition only | **No derived `System_Health_Level`** |
| Aggregate health | Pure function | None |
| Health decision flow | Imperative in health.odin | **No pure control-plane function** |
| Candle_Health dual system | By design (richer semantics) | Documented, convergence deferred |

**Zero parallel/legacy recovery paths found.** All state flows through canonical `Stream_Apply_State`.

---

## Control Plane Architecture

### Pure Functions Added (md_common/stream_apply_state.odin)

1. **`stream_health_level(s, now_ms, tf_ms) → System_Health_Level`**
   - Derives health level for a single stream from staleness + recovery status
   - Matches aggregate health logic but scoped to one stream
   - Returns Healthy for streams with no events yet (not degraded)

2. **`health_tick_evaluate(input) → Health_Tick_Output`**
   - Single pure entry point for all per-frame health decisions
   - Input: `Health_Tick_Input` (apply_state snapshot + connectivity flags)
   - Output: remediation decision, recovery success flag, stream health, counts
   - Deterministic: same input → same output, no mutation
   - Recovery decisions suppressed when disconnected or offline

3. **`Apply_State_Telemetry.stream_health`** — new field populated by `apply_state_telemetry`

### Reset Event Logging (app/store_adapters.odin)

- `reconnect_active_apply_state`: now emits `Recovery_Event{kind=.Reset}` when `recovery_attempts > 0`
- `tf_change_active_apply_state`: same — completes the audit trail
- Guard: only emits when recovery was actually in progress (avoids noise)

### Health.odin Refactor

- `refresh_active_stream_health` now delegates to `health_tick_evaluate` for decisions
- Side effects (resubscribe, log, sync) remain in the imperative caller
- Pure decision logic is now testable independently

---

## Recovery/Health Model (Unchanged)

### Per-Stream Recovery
- **Source of truth:** `Stream_Apply_State.recovery_attempts` + `recovery_last_ms`
- **Isolation:** Per-slot via `sync_recovery_to_active_slot`
- **Policy:** Only Dual_Silence artifacts (OB, Stats) trigger auto-recovery
- **Backoff:** 15s → 30s → 60s exponential, max 3 attempts

### Aggregate Health (Unchanged)
- `aggregate_health_from_slots` — pure function across all slots
- `System_Health_Level`: Healthy → Degraded → Unhealthy → Critical

### Per-Stream Health (NEW)
- `stream_health_level` — same classification logic scoped to one stream
- Consistent thresholds with aggregate (stale + exhausted = Critical, etc.)

---

## Code Changes

| File | Change | LOC |
|------|--------|-----|
| `md_common/stream_apply_state.odin` | `stream_health_level`, `Health_Tick_Input/Output`, `health_tick_evaluate`, telemetry field | +70 |
| `app/store_adapters.odin` | Reset event logging in reconnect + TF change adapters | +16 |
| `app/health.odin` | Refactored to use `health_tick_evaluate` | ~0 net (restructured) |
| `app/build_status.odin` | Per-stream health in panel + diagnostics, tf_ms in telemetry calls | +25 |
| `md_common/store_boundary_test.odin` | 12 new S35 tests | +130 |

**Total:** ~240 LOC added, zero removed, zero wire changes.

---

## Tests (12 New)

| Test | Verifies |
|------|----------|
| `test_s35_stream_health_level_healthy` | Fresh artifacts → Healthy |
| `test_s35_stream_health_level_degraded_aging` | 9s age (Dual_Silence) → Degraded |
| `test_s35_stream_health_level_unhealthy_stale` | 13s age → Unhealthy |
| `test_s35_stream_health_level_critical_exhausted` | Stale + max attempts → Critical |
| `test_s35_stream_health_level_no_data_is_healthy` | No events → Healthy (not degraded) |
| `test_s35_health_tick_evaluate_none_when_fresh` | Fresh → None remediation, Healthy |
| `test_s35_health_tick_evaluate_resubscribe_when_stale` | Stale → Resubscribe, Unhealthy |
| `test_s35_health_tick_evaluate_no_action_when_disconnected` | Disconnected → suppressed |
| `test_s35_health_tick_evaluate_recovery_success` | Fresh after recovery → success flag |
| `test_s35_telemetry_includes_stream_health` | Telemetry struct includes health level |
| `test_s35_recovery_event_reset_kind` | Reset event can be pushed/retrieved |

---

## Risks

1. **`health_tick_evaluate` duplicates staleness check logic** — mirrors `apply_state_check_recovery_success` for the success detection. Acceptable: the pure function needs to evaluate without mutation; the caller still calls the canonical mutating function.
2. **`apply_state_telemetry` gained `tf_ms` parameter** — backward compatible (default=0), but callers should pass it for accurate `stream_health` derivation.

---

## Recommended S36

**Protocol Convergence & Candle Health Unification:**
- Converge `Candle_Health` (4-state, window-aware) into `Artifact_Staleness` framework
- Or formalize the dual system with a `health_surface` that presents both
- Add per-cell health level (cell_health_level) for multi-cell layouts
- Consider surfacing `health_tick_evaluate` output in the HUD status bar badge
