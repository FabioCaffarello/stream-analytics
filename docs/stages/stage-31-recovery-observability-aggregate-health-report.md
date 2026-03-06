# Stage 31 — Recovery Observability & Aggregate Health Dashboard

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

## Executive Summary

S31 introduces system-level aggregate health observability by deriving a composite
health score from composition, staleness, and recovery state across all active stream
view slots. A ring-buffer recovery event log captures attempt/success/exhausted/reset
events for diagnostics. All new state is derived or minimal (one ring buffer). Zero
wire changes, zero new protocol logic, zero regressions.

## Current-State Audit

Before S31, the client had:
- Per-stream `Stream_Apply_State` with full artifact tracking (S22-S24)
- Per-artifact staleness detection with `Artifact_Staleness` enum (S28)
- Per-stream auto-recovery with adaptive exponential backoff (S29-S30)
- HUD/health panel/copy diagnostics showing active-stream telemetry
- Per-slot recovery persistence via `sync_recovery_to_active_slot` (S30)

**Gaps identified:**
1. No aggregate view across all slots (only active stream visible)
2. No composite health score combining composition+staleness+recovery
3. No recovery event history (only current state visible)
4. No cross-slot degradation signal in HUD

## S31 Architecture

### Design Decisions

1. **System_Health_Level** enum (Healthy/Degraded/Unhealthy/Critical) — derived,
   never stored. Computed each frame from aggregate slot state.

2. **Aggregate_Health_Summary** — pure struct computed by iterating all used slots.
   No new App_State fields; computed on demand by `compute_aggregate_health` adapter.

3. **Recovery_Event_Log** — minimal ring buffer (16 entries) in App_State.
   Written at recovery decision points in health.odin. Read by HUD/diagnostics.

4. **Health level cascade:**
   - Critical: total_stale >= 2 AND slots_exhausted > 0
   - Unhealthy: total_stale > 0 OR slots_exhausted > 0
   - Degraded: total_aging > 0 OR slots_recovering > 0
   - Healthy: otherwise

## Aggregate Health Plan

| Metric | Level | Source |
|--------|-------|--------|
| Slots by composition stage | Global | All used slot apply_states |
| Slots recovering/exhausted | Global | recovery_status per slot |
| Total stale/aging artifacts | Global | apply_state_stale_artifact_count per slot |
| Worst composition stage | Global | min(composition_stage) across slots |
| Worst staleness | Global | max(artifact_staleness) across all |
| Composite health level | Global | Derived from above |
| Recovery event history | Global | Ring buffer, 4 event kinds |

## Minimal Correct Implementation

### New Types (md_common/stream_apply_state.odin)
- `System_Health_Level` enum (4 values)
- `AGGREGATE_HEALTH_MAX_SLOTS` constant (32)
- `Aggregate_Health_Summary` struct (14 fields)
- `aggregate_health_from_slots` pure function
- `Recovery_Event_Kind` enum (4 values)
- `Recovery_Event` struct (kind, timestamp, attempts, slot_id)
- `Recovery_Event_Log` struct (ring buffer, cap 16)
- `recovery_event_log_push` / `recovery_event_log_get` ring ops

### Adapter Layer (app/store_adapters.odin)
- `compute_aggregate_health` — builds parallel arrays from slots, delegates to pure function

### App State (app/app.odin)
- `recovery_log: md_common.Recovery_Event_Log` field added to App_State
- `agg_buf/agg_len` fields added to Telemetry_HUD_Cache

### Recovery Event Logging (app/health.odin)
- `.Attempt` event logged on Resubscribe decision
- `.Exhausted` event logged on Exhausted decision
- `.Success` event logged when recovery_attempts clears to 0

### HUD Surface (app/build_status.odin, app/build_ui.odin)
- HUD status bar: `HP:xxx S:n/n REC:n EXH:n STL:n AGN:n` badge
- Health panel: AGGREGATE HEALTH section with level, slot counts, recovery/stale
- Health panel: Recovery event log (most recent 4 events with age)
- Copy diagnostics: AGGREGATE HEALTH section with full counts + event log (8 entries)

## Code Changes

| File | Change |
|------|--------|
| `md_common/stream_apply_state.odin` | +153 lines: types, pure functions, ring buffer |
| `md_common/store_boundary_test.odin` | +8 tests (200 total, up from 192) |
| `app/store_adapters.odin` | +25 lines: `compute_aggregate_health` adapter |
| `app/app.odin` | +3 lines: recovery_log field, HUD cache fields |
| `app/health.odin` | +18 lines: 3 recovery event log push calls |
| `app/build_status.odin` | +80 lines: HUD cache, health panel section, copy diagnostics |
| `app/build_ui.odin` | +6 lines: aggregate health badge in HUD bar |

## Tests

200 tests in md_common (up from 192), 8 new S31 tests:
1. `test_s31_aggregate_health_all_healthy` — 3 composed slots = Healthy
2. `test_s31_aggregate_health_degraded_with_aging` — aging artifact = Degraded
3. `test_s31_aggregate_health_unhealthy_stale` — stale OB = Unhealthy
4. `test_s31_aggregate_health_critical` — 2 stale + exhausted = Critical
5. `test_s31_aggregate_health_empty_slots_ignored` — unused slots skipped
6. `test_s31_aggregate_worst_composition` — worst composition tracking
7. `test_s31_recovery_event_log_push_and_get` — ring buffer FIFO
8. `test_s31_recovery_event_log_wraps` — ring wrap + capacity cap

## Risks

- **Low:** `compute_aggregate_health` iterates all 32 slots per HUD refresh (250ms throttled). 32 slots * 10 artifacts = 320 staleness checks — negligible at HUD cadence.
- **Low:** Recovery event log is fixed 16 entries. Long-running sessions with many recovery events will lose old history. Acceptable for diagnostics.
- **None:** Zero wire contract changes. Zero new protocol logic outside md_common.

## Recommended S32

**Compositional Alert Thresholds & Operator Notifications**
- Configurable health level thresholds for notification escalation
- Optional sound/visual alert on Critical health transitions
- Health level persistence for session recovery comparison
- Per-market health disaggregation (group slots by venue+symbol)
