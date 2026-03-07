# Stage 39 — Compare Pane Backfill, Focus & Local Invalidation

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 281 (10 new S39), zero regressions
**Wire changes:** ZERO
**New mutable state:** 2 fields (`focused_pane`, `getranges[4]`)

---

## Executive Summary

S39 transforms compare mode into an interactive, per-pane surface with explicit focus, local getrange/backfill, isolated TF invalidation, and pane-independent composition tracking. All new behavior derives from the canonical `Stream_Apply_State` model — zero protocol duplication, zero parallel stores.

## Current-State Audit (Pre-S39)

| Aspect | S38 State | Gap |
|--------|-----------|-----|
| Per-pane TF | `tf_idx[4]` ✓ | No way to direct TF change to focused pane |
| Focus | None | No focused pane concept in compare mode |
| Backfill | Global getrange only | Compare panes have no per-pane getrange |
| Invalidation | Global TF change invalidates everything | No per-pane isolation |
| Composition | Derived from slot apply_state | Missing per-pane getrange tracking |

## S39 Architecture

### New State (minimal)

1. **`Compare_State.focused_pane: int`** — focused pane index (-1 = none). Set on enter (default 0), click, or explicit action.

2. **`Compare_Pane_GetRange`** — per-pane getrange tracking:
   ```
   pending:    bool
   seeded:     bool
   oldest_ts:  i64
   sent_frame: u64
   ```

3. **`Compare_State.getranges: [4]Compare_Pane_GetRange`** — parallel array, one per pane.

### New Actions

- **`Set_Compare_Pane_Timeframe`** — TF change directed at a specific compare pane
- **`Focus_Compare_Pane`** — explicitly set focused pane

### Per-Pane Composition (reuses canonical model)

`resolve_compare_pane_composition` derives `Composition_Stage` from:
- Per-pane `getranges[pane_idx].pending/seeded`
- Slot's `apply_state.has_live[.Candle]`

Uses the same `cell_composition_stage` pure function from S26 — zero new composition logic.

### Per-Pane Backfill

- `request_compare_pane_candle_range` — seeds historical data for a pane
- `request_compare_pane_older_candles` — lazy-loads older data on scroll-left
- Both resolve venue/symbol from the pane's seed slot, TF from per-pane TF

### Isolated Invalidation

`apply_set_compare_pane_timeframe` invalidates only the target pane:
1. Resets `getranges[pane_idx]` to zero
2. Clears scroll/zoom for the pane
3. Clears TF-sensitive stores in the pane's slot via `apply_state_on_tf_change`
4. Reconciles subscriptions (new TF subjects)
5. Requests backfill for the new TF

Other panes are completely unaffected.

### Keyboard Integration

- **Shift+N** in compare mode targets the focused pane (was: targeted focused cell)
- **Click** on a compare pane sets focus
- Focus is visualized with a blue border highlight

## Code Changes

| File | Change |
|------|--------|
| `components.odin` | Added `Compare_Pane_GetRange`, `focused_pane`, `getranges[4]` to `Compare_State` |
| `app.odin` | Added `Set_Compare_Pane_Timeframe`, `Focus_Compare_Pane` actions, `pane_idx` to `UI_Action` |
| `stream_slots.odin` | Added `resolve_compare_pane_composition`, `request_compare_pane_candle_range`, `request_compare_pane_older_candles`, `apply_set_compare_pane_timeframe`; updated `resolve_compare_surface_view` to use per-pane composition |
| `stream_views.odin` | Changed `adaptive_getrange_limit` from `private="file"` to `private="package"` |
| `actions.odin` | Wired `Set_Compare_Pane_Timeframe` and `Focus_Compare_Pane` handlers; Shift+N in compare mode targets focused pane; init `focused_pane`/`getranges` on enter/add |
| `build_compare.odin` | Click-to-focus, focused pane border highlight |
| `store_boundary_test.odin` | 10 new S39 tests |

## Tests (10 new)

1. `test_s39_pane_composition_empty_when_no_getrange` — Empty state
2. `test_s39_pane_composition_range_pending` — Pending state
3. `test_s39_pane_composition_backfilled` — Backfilled state
4. `test_s39_pane_composition_live_only` — Live_Only state
5. `test_s39_pane_composition_composed` — Composed state
6. `test_s39_tf_change_clears_getrange_not_other_artifacts` — Selective invalidation
7. `test_s39_pane_composition_after_tf_change_is_empty` — Composition reset
8. `test_s39_independent_panes_isolated_composition` — Cross-pane isolation
9. `test_s39_pane_backfill_seed_marks_seeded` — GetRange lifecycle
10. `test_s39_pane_recovery_isolated_per_slot` — Recovery isolation

## Risks

1. **Slot sharing:** Two panes targeting the same market but different TFs share a slot. TF change on one pane's slot clears the other's candle data. Mitigation: per-TF slot resolution via `compare_pane_resolve_subject_id` already returns different subjects for different TFs (S38). Each TF gets its own subscription + slot.

2. **GetRange response routing:** Compare pane getrange responses arrive via the global drain path and are applied to slots, not directly to pane getrange state. The per-pane `seeded/pending` flags must be synchronized. Current implementation: `seeded=true` is set optimistically on send; the actual data arrives in the slot's candle store. This is consistent with the cell ECS pattern.

## Recommended S40

**Compare Pane Scroll-to-Load & GetRange Response Routing:**
- Wire `request_compare_pane_older_candles` to scroll-left detection in compare renderer
- Route GetRange batch completions to per-pane getrange state (clear pending, update oldest_ts)
- Add per-pane candle health computation
- Consider per-pane timeline fetch for scroll boundary detection
