# Stage 115 — Timeframe Determinism Closure Pack

**Date:** 2026-03-09
**Status:** COMPLETE
**Branch:** `codex/s9-legacy-removal-cutover`

## Objective

Guarantee deterministic behavior across all timeframe switching paths:
global TF changes (1s ↔ 5s ↔ 1m ↔ 5m ↔ 15m), per-cell TF overrides,
compare mode per-pane TF overrides, rapid switching, multi-pane isolation,
and workspace restore consistency.

## Scope

| Area | Validated |
|------|-----------|
| Global TF switching (all 9 TFs) | Yes |
| Per-cell TF override isolation | Yes |
| Compare mode per-pane TF override | Yes |
| Unsubscribe/resubscribe correctness | Yes |
| Store clearing (candle/heatmap/VPVR/analytics) | Yes |
| Apply state reset on TF change | Yes |
| Scroll/zoom reset on TF change | Yes |
| GetRange state reset on TF change | Yes |
| Candle health reset | Yes |
| Timeline clearing | Yes |
| Rapid TF switching (stress) | Yes |
| Multi-cell isolation | Yes |
| Compare pane isolation | Yes |
| Workspace persistence round-trip | Yes |
| Pane ↔ Entity_World sync (S112 contract) | Yes |
| TF constant structural invariants | Yes |

## Architecture Analysis

### TF Resolution Hierarchy (3-tier)

```
1. Per-entity override (cell.tf_idx >= 0 / pane.tf_override >= 0 / compare.tf_idx[i] >= 0)
2. Workspace default (ws.data_ctx.default_tf_idx, S112)
3. Global fallback (state.active_tf_idx)
```

All render paths use effective resolution procs (`cell_effective_tf_idx`,
`compare_pane_effective_tf_idx`, `pane_effective_tf_idx`) — never raw TF values.

### TF Switch Determinism Contract

Every TF change (global, per-cell, per-pane) follows the same invariant sequence:

1. **State update** — `active_tf_idx` / `timeframes[ci].tf_idx` / `compare.tf_idx[pi]`
2. **Store clearing** — candle, heatmap, VPVR, analytics (via layer_store)
3. **Apply state reset** — `apply_state_on_tf_change()` resets has_live, snapshot_seen, synthetic flags
4. **View reset** — scroll_x=0, zoom=0, getrange pending=false/seeded=false/oldest_ts=0
5. **Timeline clearing** — `state.timeline = {}`
6. **Health reset** — `candle_health = .No_Data`
7. **Subscription reconciliation** — unsubscribe old TF subjects, subscribe new
8. **Historical data fetch** — `request_*_candle_range()`, `request_analytics_range()`
9. **Persistence** — layout V6 / settings flush

### Isolation Guarantees

| Scenario | Guarantee |
|----------|-----------|
| Global TF change | Per-cell TF override cells are SKIPPED |
| Global TF change | Per-pane TF override compare panes are SKIPPED |
| Per-cell TF change | Other cells' TF/stores are UNTOUCHED |
| Compare pane TF change | Other panes' getrange/scroll/zoom are UNTOUCHED |
| Per-cell TF → pane sync | S112: `pane.tf_override` updated, `view.scroll_x=0, zoom_level=1.0` |

### No-Op Guards

All three TF-change procs reject:
- Same TF as current (`tf_idx == current` → return false)
- Out-of-range TF index (`< -1` or `>= len(TF_OPTIONS)`)
- Out-of-range cell/pane index

## Tests Added

**File:** `client/src/core/app/timeframe_determinism_test.odin`

| # | Test | Category |
|---|------|----------|
| 1 | `test_s115_effective_tf_per_cell_wins` | TF resolution |
| 2 | `test_s115_effective_tf_fallback_global` | TF resolution |
| 3 | `test_s115_compare_pane_tf_wins` | TF resolution |
| 4 | `test_s115_compare_pane_tf_out_of_range_fallback` | TF resolution |
| 5 | `test_s115_sequential_global_tf_switch` | Global TF |
| 6 | `test_s115_global_tf_resets_scroll_zoom` | Global TF |
| 7 | `test_s115_global_tf_resets_getrange` | Global TF |
| 8 | `test_s115_global_tf_clears_candle_health` | Global TF |
| 9 | `test_s115_global_tf_clears_timeline` | Global TF |
| 10 | `test_s115_global_tf_noop_same` | Guard |
| 11 | `test_s115_global_tf_rejects_invalid` | Guard |
| 12 | `test_s115_per_cell_tf_resets_getrange` | Per-cell TF |
| 13 | `test_s115_per_cell_tf_resets_candle_health` | Per-cell TF |
| 14 | `test_s115_per_cell_tf_resets_scroll_zoom` | Per-cell TF |
| 15 | `test_s115_per_cell_tf_noop_same` | Guard |
| 16 | `test_s115_per_cell_tf_revert_to_global` | Per-cell TF |
| 17 | `test_s115_per_cell_tf_multi_cell_isolation` | Isolation |
| 18 | `test_s115_compare_pane_tf_resets_state` | Compare TF |
| 19 | `test_s115_compare_pane_tf_noop_same` | Guard |
| 20 | `test_s115_compare_pane_tf_rejects_invalid_pane` | Guard |
| 21 | `test_s115_global_tf_skips_compare_pane_overrides` | Isolation |
| 22 | `test_s115_rapid_global_tf_cycle` | Rapid switch |
| 23 | `test_s115_rapid_per_cell_tf_toggle` | Rapid switch |
| 24 | `test_s115_global_tf_clears_analytics_multi_cell` | Analytics |
| 25 | `test_s115_per_cell_tf_clears_analytics_isolation` | Analytics |
| 26 | `test_s115_global_tf_resets_apply_state_comprehensive` | Apply state |
| 27 | `test_s115_global_tf_clears_heatmap_vpvr` | Store clearing |
| 28 | `test_s115_persistence_all_tf_indices` | Persistence |
| 29 | `test_s115_persistence_global_tf_cycle` | Persistence |
| 30 | `test_s115_tf_arrays_aligned` | Invariant |
| 31 | `test_s115_tf_ms_monotonically_increasing` | Invariant |
| 32 | `test_s115_tf_labels_distinct` | Invariant |
| 33 | `test_s115_tf_index_to_label` | Invariant |
| 34 | `test_s115_pane_effective_tf_hierarchy` | Pane TF |
| 35 | `test_s115_per_cell_tf_syncs_to_pane` | S112 sync |

**Total S115 tests: 35**
**Total app package tests: 230** (all passing)

## Findings

### Zero inconsistencies found

The TF switching implementation is deterministic across all paths:

1. **Global → per-cell isolation**: Per-cell TF override cells are correctly skipped during global TF changes. Stores, getrange, scroll/zoom, and apply_state for those cells are preserved.

2. **Global → compare pane isolation**: Per-pane TF override compare panes are correctly skipped. Only global-following panes have their state reset.

3. **Rapid switching resilience**: Cycling through all 9 TFs forward and backward, and toggling between two TFs 20 times, produces deterministic store clearing every time.

4. **Analytics isolation**: Per-cell TF change clears only that cell's layer_store analytics stream, not others sharing the same state.

5. **Persistence correctness**: All 9 TF indices round-trip through V6 layout format. The TF+1 encoding (0=global, 1-9=per-cell) is invertible.

6. **S112 contract**: Per-cell TF changes correctly sync to the workspace pane (`pane.tf_override`, `view.scroll_x=0`, `view.zoom_level=1.0`).

### TF switching is reliable — zero regressions, zero determinism gaps.

## Files

| File | Action |
|------|--------|
| `client/src/core/app/timeframe_determinism_test.odin` | **NEW** — 35 S115 tests |
| `docs/stages/stage-115-timeframe-determinism-report.md` | **NEW** — this report |
