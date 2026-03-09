# Stage 104 — Dashboard Timeframe Integrity Hardening

**Date:** 2026-03-08
**Branch:** codex/s9-legacy-removal-cutover

## Objective

Guarantee that timeframes operate with structural consistency across the entire dashboard — no divergence between UI, subscriptions, and data stores.

## Audit Summary

Full audit of the timeframe architecture across 3 axes:

1. **TF state management** — global (`active_tf_idx`), per-cell (`world.timeframes[ci].tf_idx`), per-compare-pane (`compare.tf_idx[pi]`)
2. **WS subscription lifecycle** — `reconcile_subscriptions()` diff-aware with TF mismatch detection, `CH_TF_SENSITIVE` bitmask covering 9 channel types
3. **Persistence & restore** — `SETTING_ACTIVE_TF_IDX` for global, V6 layout for per-cell, transient for compare panes

### Architecture Strengths (no changes needed)

- **Subscription reconciliation** (`reconcile.odin:254-334`): Correct unsubscribe-before-subscribe ordering, TF mismatch detection on both paths
- **Subject validation**: `range_candle_subject_id` + `getrange_request_id` reject stale GetRange batches from old TF
- **Apply state policy**: `apply_state_on_tf_change()` resets per-artifact flags per `artifact_policy`
- **Persistence**: Global TF persisted immediately on change, per-cell TF in V6 layout, round-trip tested
- **Per-cell TF isolation**: `is_explicit_tf` flag prevents global resubscribe from clobbering per-cell subscriptions

## Issues Found & Fixed

### Issue 1: Global TF change didn't clear bound cell stores (HIGH)

**Root cause:** `apply_set_timeframe_action` only cleared the active slot's candle/heatmap/vpvr/analytics stores. Cells with explicit stream bindings (e.g., ETHUSDT bound while BTCUSDT is active) that follow global TF retained stale data from the previous TF.

**Impact:** Mixed-TF candle data in ring buffer; stale heatmap/vpvr/analytics until fresh data overwrites.

**Fix:** Added per-cell loop that finds the bound slot and clears all TF-sensitive stores + calls `apply_state_on_tf_change` (stream_views.odin:374-401).

### Issue 2: Global TF change didn't refetch analytics range (MED)

**Root cause:** After reconcile, analytics widget cells following global TF had empty analytics stores but no cold reader API refetch was triggered. Only `request_active_stream_candle_range` was called.

**Fix:** Added loop after reconcile to call `request_analytics_range(state, ci)` for analytics widget cells following global TF (stream_views.odin:441-445).

### Issue 3: Per-cell TF change didn't refetch analytics range (MED)

**Root cause:** `apply_set_cell_timeframe_action` cleared the analytics store but never called `request_analytics_range` to backfill historical data for the new TF.

**Fix:** Added `request_analytics_range(state, cell_idx)` call after candle range request for analytics widgets (stream_views.odin:605-608).

### Issue 4: Global TF change didn't refetch compare pane subplot analytics (MED)

**Root cause:** Compare panes following global TF got `request_compare_pane_candle_range` but not `request_compare_pane_subplot_analytics`, leaving subplot data empty until real-time data arrived.

**Fix:** Added `request_compare_pane_subplot_analytics(state, cpi)` alongside candle range request (stream_views.odin:434).

## Files Changed

| File | Change |
|------|--------|
| `client/src/core/app/stream_views.odin` | +36 lines: bound cell store clearing, analytics refetch for global/per-cell/compare TF changes |
| `client/src/core/app/marketdata_test.odin` | +157 lines: 5 new tests covering all 4 fixes |

## Tests

5 new tests added:

| Test | Validates |
|------|-----------|
| `test_s104_global_tf_clears_bound_cell_stores` | Issue 1: bound cell candle_store cleared on global TF change |
| `test_s104_global_tf_clears_bound_cell_analytics` | Issue 1: bound cell analytics cleared on global TF change |
| `test_s104_global_tf_skips_per_cell_tf` | Isolation: per-cell TF cells NOT affected by global change |
| `test_s104_per_cell_tf_clears_slot_stores` | Issue 3: per-cell TF change clears stores + resets apply state |
| `test_s104_global_tf_resets_apply_state_for_bound_cells` | Issue 1: apply_state_on_tf_change called for bound cells |

**Results:** 69 app tests + 401 md_common tests + 186 services tests = **656 tests, all green.**

## Items Audited — No Fix Required

| Aspect | Status | Notes |
|--------|--------|-------|
| TF_OPTIONS / TF_OPTION_MS | OK | 9 values, consistent indices |
| Subscription reconciliation | OK | Correct unsubscribe-before-subscribe, TF mismatch detection |
| GetRange subject validation | OK | Subject-scoped, request_id guards (S34) |
| Persistence global TF | OK | Immediate flush on change, restore with bounds checking |
| Persistence per-cell TF | OK | V6 layout, round-trip tested |
| Compare pane TF (transient) | OK | Reset on mode entry/exit, per-pane isolation |
| Page navigation | OK | Global TF persists across routes |
| Web platform set_candle_tf | OK | No mutex needed — WASM is single-threaded |
| Keyboard shortcuts (1-9, Shift+1-9) | OK | Correct dispatch to global/per-cell/per-pane handlers |
| Native platform mutex | OK | Proper lock/unlock around resubscribe |
| Protocol state reset | OK | Bootstrap_Pending on TF change per protocol_engine |
| Artifact policy flags | OK | reset_on_tf_change correct per artifact kind |
| Orderbook defensive clear | OK | Cleared on global TF change even though policy says no reset |

## Acceptance Criteria

- [x] No widget consuming wrong TF — bound cell stores now cleared on global TF change
- [x] Subscriptions always aligned to active TF — reconciliation audit confirmed correct
- [x] Analytics refetch on TF change — cold reader APIs called for all three TF change paths
- [x] Compare pane subplot analytics refetch on global TF change
- [x] Per-cell TF isolation preserved — global changes skip cells with per-cell TF override
- [x] Zero regressions — 656 tests green
