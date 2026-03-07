# Stage 42 — Per-Pane Invalidation, Recovery & Diagnostics

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 301 (10 new S42), zero regressions
**Wire changes:** Zero
**New mutable state:** Zero (all derived from existing slot apply_state)

## Executive Summary

S42 closes the last operational gap in compare mode: non-active compare pane slots now receive independent staleness detection, auto-recovery evaluation, and recovery diagnostics — without introducing new mutable state, wire changes, or cross-pane contamination.

Before S42, recovery evaluation only ran for the active stream. Compare panes pointing to different slots had health/staleness *displayed* (via `resolve_compare_surface_view` reading slot apply_state) but no *remediation* — a stale compare pane would stay stale indefinitely unless the user manually intervened.

## Operational Audit

### Already per-pane (S38-S41):
- TF change invalidation (only target pane cleared)
- Composition derivation (per-pane getrange + slot live candle)
- Health/staleness display (derived from slot apply_state)
- GetRange lifecycle (pending/seeded/oldest_ts per pane)
- Lazy loading (per-pane scroll-near-edge detection)

### Gaps closed by S42:
1. **Recovery evaluation** — `evaluate_compare_pane_health` runs `health_tick_evaluate` per pane for non-active slots
2. **Recovery diagnostics** — `Cell_Surface_View.recovery_status` surfaced per-pane in compare headers (RCVR/XHST badges)
3. **Reconnect reseed** — compare pane getranges reseeded after reconnect (S41 only cleared them)
4. **Recovery-triggered reseed** — successful recovery reseeds per-pane getrange if not already seeded

## Per-Pane Control Plan

### Transitions that invalidate only the local pane:
1. **Per-pane TF change** — resets only target pane's getrange/scroll/zoom + slot stores (unchanged from S38)
2. **Per-pane recovery success** — clears recovery counter on the slot, reseeds pane getrange if needed

### Transitions that invalidate all panes (correctly global):
1. **Reconnect** — all slots reset by `apply_state_on_reconnect`, all pane getranges cleared + reseeded
2. **Resubscribe (recovery action)** — `reconcile_subscriptions` is idempotent, affects all subscriptions

### Per-pane evaluation:
- Each compare pane's slot is evaluated independently via `health_tick_evaluate`
- Active stream slot is skipped (already handled by `refresh_active_stream_health`)
- Recovery decisions (mark_recovery, check_success) write directly to slot's apply_state
- Recovery events logged with distinct `slot_id` for audit trail

### Diagnostics per pane:
- `Cell_Surface_View.recovery_status` derived from `apply_state_recovery_status(slot.apply_state)`
- Compare header shows RCVR (yellow, recovering) or XHST (red, exhausted) badge
- Health dot already reflects recovery via `stream_health_level` (Degraded when recovering)

## Minimal Correct Implementation

### Code Changes

**stream_slots.odin:**
- `Cell_Surface_View` +1 field: `recovery_status: md_common.Recovery_Status`
- `resolve_compare_surface_view` derives recovery_status from slot apply_state
- `resolve_cell_surface_view` derives recovery_status (consistency)

**health.odin:**
- New `evaluate_compare_pane_health(state, now_ms)` — per-pane health tick for non-active compare slots
  - Skips panes on active slot (avoids double-evaluation)
  - Applies recovery decisions directly to slot apply_state
  - Logs recovery events with per-slot slot_id
  - Reseeds pane getrange after successful recovery
- Wired into `sample_marketdata_metrics` after active stream health

**build_compare.odin:**
- Recovery badge (RCVR/XHST) rendered in compare header between composition badge and health dot

**marketdata.odin:**
- Reconnect path reseeds compare pane getranges after clearing (S42 addition to S41 clearing)

**store_boundary_test.odin:**
- 10 new tests covering per-pane recovery isolation, success clearing, health tick independence, diagnostics derivation, reconnect/TF-change per-slot clearing, exhaustion guard, cooldown anti-thrashing, event log per-slot tracking

### Design Invariants

1. **Zero new mutable state** — all recovery state lives in existing `slot.apply_state.recovery_*` fields
2. **Derivation over mutation** — `recovery_status` in `Cell_Surface_View` is pure derivation each frame
3. **No cross-pane contamination** — each pane evaluates its own slot independently
4. **No double-evaluation** — active slot panes skip `evaluate_compare_pane_health` (handled by active stream path)
5. **Idempotent reconciliation** — `reconcile_subscriptions` called by recovery is safe to call multiple times

## Tests (10 new, 301 total)

| Test | What it proves |
|------|---------------|
| `test_s42_per_pane_recovery_isolation` | Recovery on pane A does not affect pane B |
| `test_s42_per_pane_recovery_success_clears_only_target` | Success clears only the recovered pane |
| `test_s42_per_pane_health_tick_independent` | Independent tick outputs for healthy vs stale panes |
| `test_s42_recovery_status_derivation` | None/Recovering/Exhausted derived correctly |
| `test_s42_health_level_reflects_recovery` | stream_health_level reflects recovery state |
| `test_s42_reconnect_clears_recovery_per_slot` | Reconnect clears recovery per slot independently |
| `test_s42_tf_change_clears_recovery_per_slot` | TF change clears only target slot's recovery |
| `test_s42_exhausted_pane_does_not_trigger_resubscribe` | Max attempts → Exhausted, not Resubscribe |
| `test_s42_cooldown_prevents_thrashing` | Cooldown window blocks resubscribe, expires correctly |
| `test_s42_recovery_event_log_tracks_per_slot` | Events carry distinct slot_id for audit trail |

## Risks

- **Concurrent reconciliation**: Multiple pane recoveries in the same frame call `reconcile_subscriptions` multiple times. This is safe (idempotent) but slightly wasteful. Could be optimized with a dirty flag if profiling shows impact.
- **Shared slot**: Two compare panes pointing to the same non-active slot would both evaluate it. The second evaluation is a no-op (recovery already marked/cleared by first). No harm, no duplication.

## Recommended S43

**Compare Mode Diagnostics Panel** — dedicated diagnostics overlay for compare mode showing:
- Per-pane telemetry (artifact counts, composition, recovery status, cooldown)
- Per-pane getrange lifecycle (pending/seeded/oldest_ts)
- Cross-pane aggregate health summary
- Recovery event log filtered by compare slot IDs

Alternatively: **Legacy removal sweep** — audit for any remaining global-only patterns that should be per-pane, particularly in the cell/grid path (non-compare).
