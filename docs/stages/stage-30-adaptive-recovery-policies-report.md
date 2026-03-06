# Stage 30 — Adaptive Recovery Policies & Per-Stream Recovery Isolation

**Date:** 2026-03-06
**Status:** COMPLETE

## Executive Summary

S30 evolves the S29 recovery mechanism from a fixed-cooldown global model to an adaptive,
per-stream-isolated system. Fixed 30s cooldown is replaced with exponential backoff
(15s → 30s → 60s capped), and recovery state is now synced back to the per-stream slot
so stream switches preserve recovery progress. Zero new state fields, zero wire changes.

## Current-State Audit (Pre-S30)

- **S29** provides stale auto-recovery with `Remediation_Decision` enum and 3 pure functions
- **Recovery state** lives in `Stream_Apply_State` (`recovery_attempts`, `recovery_last_ms`)
- **Fixed 30s cooldown** (`RECOVERY_COOLDOWN_MS = 30_000`) regardless of attempt count
- **Global-only tracking**: recovery runs on `active_apply_state` but never synced back
  to the per-slot `apply_state` → stream switch loses recovery progress
- **Telemetry** shows status + attempts but not cooldown window or remaining time

### Isolation Gap

Events write to **both** `slot.apply_state` and `state.active_apply_state` in parallel.
Recovery mutations only write to `state.active_apply_state` in `health.odin`.
`sync_active_apply_state_from_slot` copies slot → active on stream switch, discarding
any recovery state accumulated on the global copy.

## S30 Architecture

### Adaptive Exponential Backoff

Replace fixed constant with pure function:

```
recovery_cooldown_for_attempt(attempts) → i64
  attempt 0: 15s (base)
  attempt 1: 30s (15s << 1)
  attempt 2: 60s (15s << 2, = max)
  attempt 3+: 60s (capped)
```

Constants: `RECOVERY_BASE_COOLDOWN_MS = 15_000`, `RECOVERY_MAX_COOLDOWN_MS = 60_000`

### Per-Stream Recovery Isolation

New adapter `sync_recovery_to_active_slot`: after recovery mutations in `health.odin`,
writes `recovery_attempts` and `recovery_last_ms` from `active_apply_state` back to
the active slot. Stream switch via `sync_active_apply_state_from_slot` then restores
correct recovery state.

### Enhanced Telemetry

`Apply_State_Telemetry` extended with:
- `recovery_cooldown_ms`: current cooldown window for this attempt level
- `recovery_cooldown_remaining_ms`: time remaining before next attempt allowed

`apply_state_telemetry` gains `now_ms` parameter (defaulted to 0) for computing remaining.

## Code Changes

### `md_common/stream_apply_state.odin`
- Replaced `RECOVERY_COOLDOWN_MS` with `RECOVERY_BASE_COOLDOWN_MS` (15s) + `RECOVERY_MAX_COOLDOWN_MS` (60s)
- Added `recovery_cooldown_for_attempt(attempts)` pure function (exponential backoff)
- Updated `apply_state_stale_remediation` to call `recovery_cooldown_for_attempt`
- Extended `Apply_State_Telemetry` with `recovery_cooldown_ms`, `recovery_cooldown_remaining_ms`
- Updated `apply_state_telemetry(s, now_ms)` to compute cooldown diagnostics

### `app/store_adapters.odin`
- Added `sync_recovery_to_active_slot`: writes recovery state back to active slot

### `app/health.odin`
- Tracks `recovery_mutated` flag for both `mark_recovery` and `check_recovery_success`
- Calls `sync_recovery_to_active_slot` when recovery state changed

### `app/build_status.odin`
- Health panel: recovery row shows cooldown window + remaining (`cd:Ns/Ns`)
- Copy diagnostics: recovery line includes `cooldown=Ns remaining=Ns`
- Both `apply_state_telemetry` calls updated with `now_ms` parameter

## Tests

8 new tests in `store_boundary_test.odin` (192 total, up from 184):

| Test | Validates |
|------|-----------|
| `test_s30_recovery_cooldown_exponential_backoff` | Pure backoff: 15s, 30s, 60s, 60s, 60s |
| `test_s30_adaptive_cooldown_first_attempt` | Attempt 1 has 30s cooldown window |
| `test_s30_adaptive_cooldown_second_attempt` | Attempt 2 has 60s cooldown window |
| `test_s30_recovery_preserved_in_slot` | Recovery state survives struct copy (slot pattern) |
| `test_s30_telemetry_cooldown_remaining` | Cooldown remaining computed correctly |
| `test_s30_exhausted_shows_max_cooldown` | Exhausted level uses max cooldown in telemetry |
| `test_s30_no_thrashing_with_backoff` | Rapid checks within cooldown all return Cooldown |
| `test_s30_success_reset_enables_fast_first_recovery` | After success, next episode uses base cooldown |

Updated 1 existing S29 test (`test_s29_stale_remediation_cooldown`) to use adaptive timing.

## Invariants Maintained

- `Stream_Apply_State` remains sole source of truth (S24)
- Zero new state fields — cooldown is derived from `recovery_attempts`
- Zero wire changes, zero new protocol messages
- Widgets/surfaces remain passive readers of telemetry
- Pure functions: `recovery_cooldown_for_attempt`, all decision functions
- Replay-safe: backoff derives deterministically from attempt count
- `apply_state_reset()` / `on_reconnect()` / `on_tf_change()` still clear recovery state

## Risks

- **Slot sync overhead**: `sync_recovery_to_active_slot` does a slot lookup per recovery mutation
  (at most once per frame, negligible)
- **Longer recovery window**: Exponential backoff means 3rd attempt at ~60s instead of 30s.
  Acceptable tradeoff — persistent staleness after 2 failed attempts likely needs more time
- **Backward compat**: `apply_state_telemetry(s)` still works without `now_ms` (defaults to 0,
  remaining = 0) — no existing callers break

## Recommended S31

**Recovery Observability & Aggregate Health Dashboard:**
- Aggregate recovery stats across all slots (how many streams recovering/exhausted)
- Per-stream degradation badges in compare mode cells
- Recovery event log (timestamped history of attempts + outcomes)
- Health score composite: combine composition, staleness, recovery into single metric
