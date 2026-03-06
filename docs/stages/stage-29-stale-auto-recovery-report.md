# Stage 29 — Stale Auto-Recovery & Protocol-Driven Remediation

**Date:** 2026-03-06
**Status:** COMPLETE

## Executive Summary

S29 closes the operational loop between stale detection (S28) and recovery by adding
policy-driven auto-remediation. When Dual_Silence artifacts (Orderbook, Stats) go stale
(>12s silence), the system auto-resubscribes with cooldown/backoff guards. After 3
failed attempts, it escalates to DESYNC for manual intervention. Recovery state lives in
the canonical `Stream_Apply_State` — zero new runtime state, zero wire changes.

## Current-State Audit (Pre-S29)

- **S28** provides `apply_state_artifact_staleness()` and `apply_state_stale_artifact_count()`
  as pure queries over `last_recv_ms` + `Artifact_Policy.stale_detection`
- **health.odin:420-438** had ad-hoc 12s DESYNC detection: if stats AND orderbook AND
  stream events all silent >12s, hard-mark DESYNC — no auto-recovery
- **Manual resync** (`apply_resync_active_stream_action`) was the only recovery path
- **Platform reconnect** handles transport-level recovery (exponential backoff) but not
  application-level stale detection

## Architecture

### Recovery State (in `Stream_Apply_State`)

Two new fields — zero-initialized, cleared on reset/reconnect/TF-change:

```
recovery_last_ms:  i64   // Timestamp of last auto-recovery attempt
recovery_attempts: u8    // Consecutive attempts since last success
```

### Decision Enums

- **`Remediation_Decision`**: `None | Resubscribe | Cooldown | Exhausted`
- **`Recovery_Status`**: `None | Recovering | Exhausted` (derived for display)

### Pure Functions (md_common)

| Function | Purpose |
|----------|---------|
| `apply_state_stale_remediation(s, now_ms, tf_ms)` | Decides if auto-recovery should fire |
| `apply_state_mark_recovery(s, now_ms)` | Records a recovery attempt |
| `apply_state_check_recovery_success(s, now_ms, tf_ms)` | Resets counter when stale clears |
| `apply_state_recovery_status(s)` | Derived status for display |

### Recovery Flow

```
Frame N: Dual_Silence artifacts go Stale (age > 12s)
  ↓
apply_state_stale_remediation() → .Resubscribe
  ↓
apply_state_mark_recovery() → attempts=1, last_ms=now
  ↓
reconcile_subscriptions() → server gets fresh SUBSCRIBEs
  ↓
Frame N+k: Fresh data arrives → staleness clears
  ↓
apply_state_check_recovery_success() → attempts=0, last_ms=0
```

**Guard Rails:**
- **Cooldown:** 30s between attempts (prevents thrashing)
- **Max attempts:** 3 before escalating to DESYNC (prevents infinite loops)
- **Cleared on:** reconnect, TF change, full reset (transport handles these)
- **Only Dual_Silence:** TF_Adaptive (Candle) does NOT trigger auto-recovery

## Code Changes

### `md_common/stream_apply_state.odin`
- Added `recovery_last_ms`, `recovery_attempts` to `Stream_Apply_State`
- Added `Remediation_Decision`, `Recovery_Status` enums
- Added `RECOVERY_COOLDOWN_MS` (30s), `RECOVERY_MAX_ATTEMPTS` (3) constants
- Added 4 pure functions: `stale_remediation`, `mark_recovery`, `check_recovery_success`, `recovery_status`
- Updated `apply_state_on_reconnect`, `apply_state_on_tf_change` to clear recovery state
- Updated `Apply_State_Telemetry` with `recovery_status`, `recovery_attempts`
- Updated `apply_state_telemetry()` to include recovery fields

### `app/health.odin`
- Replaced ad-hoc 12s DESYNC detection (lines 420-438) with policy-driven remediation
- `.Resubscribe` → `mark_recovery` + `reconcile_subscriptions`
- `.Exhausted` → `controller_mark_desync(.Snapshot_Stale)` (existing DESYNC path)
- Added `apply_state_check_recovery_success` per-frame call
- Added `tf_ms_for_health` helper

### `app/build_status.odin`
- HUD apply summary: `REC` badge (recovering) / `REC!` badge (exhausted)
- Health panel: recovery row (status + attempts/max) when active
- Copy diagnostics: recovery line in APPLY STATE section

## Tests

13 new tests in `store_boundary_test.odin` (184 total, up from 171):

| Test | Validates |
|------|-----------|
| `test_s29_stale_remediation_triggers_resubscribe` | Dual_Silence stale → Resubscribe |
| `test_s29_stale_remediation_cooldown` | 30s cooldown between attempts |
| `test_s29_stale_remediation_exhausted` | 3 attempts → Exhausted |
| `test_s29_stale_remediation_none_when_fresh` | Fresh → None |
| `test_s29_recovery_success_resets_attempts` | Fresh data clears counter |
| `test_s29_recovery_clears_on_reconnect` | Reconnect clears recovery |
| `test_s29_recovery_clears_on_tf_change` | TF change clears recovery |
| `test_s29_recovery_clears_on_full_reset` | Full reset clears recovery |
| `test_s29_recovery_status_derived` | Status enum derivation |
| `test_s29_no_remediation_for_never_received` | Never-active → no remediation |
| `test_s29_telemetry_includes_recovery` | Telemetry struct reflects recovery |
| `test_s29_tf_adaptive_does_not_trigger_remediation` | Candle stale → no auto-recovery |
| `test_s29_single_dual_silence_stale_triggers` | Single stale artifact triggers |

## Risks

- **False positive remediation:** Mitigated by requiring `event_count > 0` (only recover
  artifacts that were previously active) and 30s cooldown
- **Reconcile during high load:** Mitigated by max 3 attempts before escalating
- **Layer path sequencing:** `health.odin` calls `reconcile_subscriptions` directly
  (same pattern as backpressure assist), works in both drain and layer paths

## Invariants Maintained

- `Stream_Apply_State` remains sole source of truth (S24)
- `apply_state_reset()` zeros all fields including recovery (S22 contract)
- Zero wire changes, zero new protocol messages
- Widgets/surfaces remain passive readers
- Deterministic: pure decision functions, no randomness
- Replay-safe: recovery decisions derive from timestamps in apply state

## Recommended S30

**Adaptive Recovery Policies & Per-Stream Recovery Isolation:**
- Per-stream recovery tracking (currently global via `active_apply_state`)
- Adaptive cooldown (exponential backoff instead of fixed 30s)
- Recovery telemetry metrics (attempts/success rate over time)
- Per-cell degradation badges when individual streams are recovering
