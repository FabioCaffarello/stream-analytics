# Stage 44 — Compare Pane Autonomy Completion

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 320 (8 new S44)
**Wire changes:** Zero
**New mutable state:** Zero

---

## Executive Summary

S44 closes the last coordination gap in compare pane autonomy: **global TF change now properly invalidates compare panes following global TF** (`tf_idx == -1`). Prior to S44, changing the global timeframe via number keys (1-9) invalidated cells and the active slot but left compare pane getrange/scroll/zoom stale, causing panes to display old-TF candles with incorrect composition until timeout or manual interaction.

The fix adds 12 lines of app-layer code (two loops in `apply_set_timeframe_action`) and 8 pure-function tests validating the composition/health/recovery contracts that compare pane autonomy relies on.

---

## Current-State Audit (Pre-S44)

### Already Complete (S38-S43)

| Capability | Stage | Status |
|---|---|---|
| Per-pane TF override (`tf_idx[4]int`) | S38 | DONE |
| Per-pane getrange state (`Compare_Pane_GetRange[4]`) | S39 | DONE |
| Per-pane backfill request/response routing | S39/S41 | DONE |
| Per-pane lazy loading (scroll-near-edge) | S41 | DONE |
| Per-pane getrange timeout | S41 | DONE |
| Per-pane composition derivation | S39 | DONE |
| Per-pane surface view (`resolve_compare_surface_view`) | S38 | DONE |
| Per-pane health evaluation (`evaluate_compare_pane_health`) | S42 | DONE |
| Per-pane recovery (resubscribe + reseed) | S42 | DONE |
| Per-pane TF invalidation (only target pane) | S39 | DONE |
| Reconnect clearing + reseeding | S41/S42 | DONE |
| Zero direct slot access in render path | S40/S43 | DONE |
| Surface contracts tested | S43 | DONE |

### Gap Found

**Global TF change missing compare pane invalidation.**

`apply_set_timeframe_action` (`stream_views.odin:346-420`) changed `state.active_tf_idx` and invalidated:
- Active slot stores (candle/heatmap/vpvr/orderbook cleared)
- Cells following global TF (getrange/scroll/zoom reset)

But did NOT invalidate compare panes with `tf_idx[ci] == -1` (global-followers). After global TF change:
- Per-pane getrange state was stale (pending/seeded/oldest_ts from old TF)
- Scroll/zoom positions were stale
- No fresh backfill was triggered for the new TF
- Composition badge showed incorrect stage until timeout or data arrival

Per-pane TF override panes (`tf_idx[ci] >= 0`) were correctly unaffected.

---

## S44 Architecture

### Design Decision: Getrange-Only Invalidation

Compare panes share stream view slots with each other and with cells. Clearing a slot's stores during global TF change could destroy data needed by other panes with per-pane TF overrides pointing at the old TF. Therefore:

1. **Clear per-pane getrange state** — stale from old TF, no longer relevant
2. **Reset scroll/zoom** — stale visual state for new TF
3. **DO NOT clear slot stores** — shared resource, may serve other panes
4. **Trigger fresh backfill** — new request goes to new-TF subject via `compare_pane_effective_tf_string`

This mirrors the cell pattern: cells following global TF clear their getrange/scroll/zoom but don't clear slot stores (the active slot is cleared separately by the main handler).

### Slot Lifecycle After Global TF Change

1. `reconcile_subscriptions` creates new subscriptions at new TF → new slots
2. `request_compare_pane_candle_range` sends getrange for new TF's subject
3. Response arrives at new slot; compare pane resolves to it via `compare_pane_resolve_subject_id`
4. Old slots remain for panes with per-pane TF overrides at old TF
5. Composition transitions: Empty → Range_Pending → Composed

---

## Code Changes

### `stream_views.odin` — `apply_set_timeframe_action` (+12 lines)

**After cell invalidation (line 401), before timeline refetch:**
```odin
// S44: Invalidate compare panes following global TF (tf_idx == -1).
if state.compare.active {
    for cpi in 0 ..< state.compare.count {
        if state.compare.tf_idx[cpi] >= 0 do continue
        state.compare.getranges[cpi] = {}
        state.compare.scroll_x[cpi] = 0
        state.compare.zoom[cpi] = 0
    }
}
```

**After `reconcile_subscriptions`, before persist:**
```odin
// S44: Trigger fresh backfill for global-following compare panes after reconcile.
if state.compare.active {
    for cpi in 0 ..< state.compare.count {
        if state.compare.tf_idx[cpi] >= 0 do continue
        request_compare_pane_candle_range(state, cpi)
    }
}
```

### `store_boundary_test.odin` — 8 new tests

| Test | Validates |
|---|---|
| `test_s44_composition_after_getrange_reset_with_live_candle` | Composition = Live_Only after getrange reset (live candles persist) |
| `test_s44_composition_after_getrange_reset_without_live_candle` | Composition = Empty after getrange reset (no live data) |
| `test_s44_tf_change_clears_recovery_state` | apply_state_on_tf_change zeros recovery_attempts + recovery_last_ms |
| `test_s44_per_pane_override_survives_global_tf_change` | Override pane (Composed) unaffected while global-follower resets to Empty |
| `test_s44_reseed_lifecycle_after_invalidation` | Full lifecycle: Composed → TF change → Empty → Range_Pending → Composed |
| `test_s44_health_adapts_to_new_tf_after_change` | TF-Adaptive staleness: same gap stale at 1m but fresh at 1h |
| `test_s44_getrange_timeout_uses_reseed_frame` | Timeout uses reseed frame, not stale frame from old TF |
| `test_s44_recovery_resets_on_tf_change` | Post-TF remediation starts fresh (Resubscribe, not Cooldown) |

---

## Per-Pane Autonomy — Final Status

| Dimension | Mechanism | Isolation |
|---|---|---|
| **Timeframe** | `tf_idx[4]int`, -1 = follow global | Per-pane override or global |
| **Getrange/Backfill** | `Compare_Pane_GetRange[4]` | Per-pane pending/seeded/oldest_ts |
| **Composition** | `resolve_compare_pane_composition` | Per-pane getrange + slot.has_live |
| **Invalidation (per-pane TF)** | `apply_set_compare_pane_timeframe` | Only target pane reset |
| **Invalidation (global TF)** | S44: loop in `apply_set_timeframe_action` | Only global-followers reset |
| **Recovery/Health** | `evaluate_compare_pane_health` | Per-pane health_tick_evaluate |
| **Reconnect** | Clear + reseed all panes | All panes reset uniformly |
| **Surface View** | `resolve_compare_surface_view` | Per-pane read model |
| **Lazy Loading** | `check_lazy_candle_loading` compare section | Per-pane scroll detection |
| **Timeout** | `marketdata.odin` compare pane timeout loop | Per-pane sent_frame check |

All 10 dimensions now have complete, isolated per-pane handling. No compare pane depends implicitly on the active stream runtime.

---

## Risks

- **Transient stale display:** Between global TF change and new subscription establishing, a global-following pane may briefly show old-TF candles from the seed slot. This matches cell behavior and resolves within 1-2 frames as new data arrives. Composition badge shows "PEND" during transition.
- **No Remove_Compare_Pane action:** Panes can only be added (Tab) or all removed (Esc). This is a UX feature, not a protocol gap.

---

## Recommended S45

With compare pane autonomy complete, the next stage should focus on **workspace persistence and layout contracts** — ensuring that compare mode state, per-pane TF overrides, and widget selections survive session restarts. This would complete the PRD-0009 workspace foundations.
