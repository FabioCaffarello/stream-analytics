# Stage 151 — Stream Health & Recovery Completion

**Date:** 2026-03-09
**Status:** Complete

## Objective

Close the stream health/desync/recovery model by making it explicit, consistent, and
unambiguous across transport, delivery, snapshot, health, and reliability layers.

## Changes

### 1. Fix: Recovery Exhaustion No Longer Escalates to Transport DESYNC

**File:** `app/health.odin`

The active stream path previously escalated `Remediation_Decision.Exhausted` to
`controller_mark_desync(.Snapshot_Stale)`, contaminating transport state with
delivery-layer exhaustion. The compare pane path (S143) already avoided this correctly.

**Before:** Exhausted → `controller_mark_desync` → `state.active_metrics.state = .Desync`
**After:** Exhausted → log event only → `stream_reliability()` handles via `recovery == .Exhausted`

This makes "transport ok + snapshot stale" distinguishable from "real transport desync".

### 2. Transport Lag Surfaced in Cell_Surface_View

**File:** `app/stream_slots.odin`

Added `is_transport_lagging: bool` to `Cell_Surface_View`, set from
`state.active_metrics.state == .Lag`. This surfaces transport-level lag (message age > 4s)
as an explicit field distinct from per-artifact aging (`Degraded_Aging`).

Wired in both `resolve_cell_surface_view_with_stores` and `resolve_compare_surface_view`.

### 3. ADR-0034: Stream Health & Recovery Completion

**File:** `docs/adrs/ADR-0034-stream-health-recovery-completion.md`

Documents the complete 5-layer health pipeline (transport → delivery → snapshot →
health/recovery → reliability), the 5 explicit failure scenarios, ownership invariants,
and the S151 fix rationale.

### 4. Tests: 8 New Scenario Tests

**File:** `md_common/md_common_test.odin`

| Test | Scenario |
|---|---|
| `test_s151_transport_ok_snapshot_stale` | Recovery exhausted without transport desync → Manual_Resync |
| `test_s151_feed_lagging_via_artifact_aging` | Aging artifacts → Degraded_Aging, render allowed |
| `test_s151_desync_recoverable` | Transport desync, no exhaustion → Desync |
| `test_s151_desync_exhausted` | Transport desync + exhausted → Manual_Resync |
| `test_s151_manual_resync_required_clears_on_reconnect` | Reconnect resets recovery |
| `test_s151_exhausted_without_desync_differs_from_with_desync` | Distinguishes delivery vs transport causes |
| `test_s151_health_tick_exhausted_preserves_transport` | Pure function doesn't produce synthetic desync |
| `test_s151_recovery_success_resets_to_reliable` | Fresh data after recovery → Reliable |

## Test Results

- **md_common:** 493 tests, all pass (was 485, +8 new)
- **app:** 437 tests, all pass
- **layers:** 54 tests, all pass
- **services:** 204 tests, all pass
- **Total:** 1,188 tests, zero failures, zero regressions

## Files Modified

| File | Change |
|---|---|
| `app/health.odin` | Remove DESYNC escalation from Exhausted path |
| `app/stream_slots.odin` | Add `is_transport_lagging` to Cell_Surface_View |
| `md_common/md_common_test.odin` | 8 new S151 scenario tests |
| `docs/adrs/ADR-0034-stream-health-recovery-completion.md` | New ADR |

## 5 Explicit Failure Scenarios (Now Documented & Tested)

| Scenario | Transport | Recovery | Reliability | Render |
|---|---|---|---|---|
| Transport OK + Snapshot Stale | Live | Exhausted | Manual_Resync | Blocked |
| Feed Lagging | Lag/Live | None | Reliable/Degraded_Aging | Allowed |
| Desync Recoverable | Desync | None/Recovering | Desync | Blocked |
| Desync Exhausted | Desync | Exhausted | Manual_Resync | Blocked |
| Manual Resync Required | Any | Exhausted | Manual_Resync | Blocked |

## Deferred

- **Transport Lag badge in cell headers** — `is_transport_lagging` is surfaced but not yet
  rendered as a badge. Can be added when UI polish pass occurs.
- **Per-slot transport state** — Currently all cells share the global transport state.
  Future multi-connection architecture would need per-slot transport tracking.
