# Stage 41 — Per-Pane Historical/Realtime Composition

**Status:** COMPLETE
**Date:** 2026-03-07
**Tests:** 291 (10 new S41, 281 prior)
**Wire changes:** Zero
**New mutable state:** Zero
**New protocol logic:** Zero

## Executive Summary

S41 completes the per-pane composition lifecycle that S39 started. S39 introduced
`Compare_Pane_GetRange` and the request path (`request_compare_pane_candle_range`,
`request_compare_pane_older_candles`), but the **response path** and **lifecycle
auxiliaries** were never wired. S41 closes five gaps:

1. **Range batch routing** — `handle_range_candle_batch` now routes `oldest_ts`
   updates and `pending` clearing to compare panes, not just cells.
2. **GetRange timeout** — compare panes now participate in the 5s timeout guard.
3. **Lazy loading** — `check_lazy_candle_loading` extends to compare pane scroll
   edges, respecting the global concurrency budget.
4. **Initial backfill** — `apply_enter_compare` and `apply_add_compare_stream`
   now call `request_compare_pane_candle_range` so panes start with data.
5. **Reconnect clearing** — compare pane getranges reset on transport reconnect.

## Current-State Audit (Pre-S41)

| Aspect | Cell Path | Compare Pane Path | Gap |
|--------|-----------|-------------------|-----|
| GetRange request | `request_cell_candle_range` | `request_compare_pane_candle_range` (S39) | None |
| GetRange response routing | `handle_range_candle_batch` per-cell loop | **Missing** | **Critical** |
| GetRange timeout | `drain_marketdata` per-cell loop | **Missing** | Medium |
| Lazy loading | `check_lazy_candle_loading` per-cell loop | **Missing** | Medium |
| Initial backfill on enter | N/A (cells get data on subscribe) | **Missing** | High |
| Reconnect clearing | `drain_marketdata` reconnect block | **Missing** | High |
| Composition derivation | `resolve_compare_pane_composition` (S39) | Works, reads getrange | OK |
| Surface view | `resolve_compare_surface_view` (S38) | Works, reads composition | OK |

## Per-Pane Composition Architecture

Each compare pane follows the same lifecycle as cells:

```
Empty → Range_Pending → Backfilled → (live candle arrives) → Composed
         ↑                                                      |
         └──────────── TF change / reconnect ←─────────────────┘
```

**Key principle:** Compare panes reuse the canonical `cell_composition_stage` pure
function. No new composition logic was needed — only wiring the response path so
`Compare_Pane_GetRange.{pending, seeded, oldest_ts}` are properly maintained.

### How historical enters per pane
- `request_compare_pane_candle_range` sends GetRange with per-pane TF
- `handle_range_candle_batch` matches `batch_slot_idx` to pane's effective slot
- `oldest_ts` tracked during batch, `pending` cleared on `is_last`

### How realtime handoff occurs per pane
- Live candle events flow into slot's `candle_store` via existing `handle_candle_event`
- `resolve_compare_pane_composition` reads `slot.apply_state.has_live[.Candle]`
- When both `seeded=true` and `has_live=true` → `Composed`

### No duplication of apply state
- Compare panes do NOT have their own `Stream_Apply_State`
- They read the slot's apply state (shared with any cell bound to the same market)
- Only `Compare_Pane_GetRange` is per-pane (5 fields, same as `GetRange_Component`)

## Minimal Correct Implementation

### Fix 1: Range batch response routing (marketdata.odin)

Before the `is_last` block, added a loop over compare panes to track `oldest_ts`
from batch candles. Inside the `is_last` block, added a second loop to clear
`pending` when the batch completes.

### Fix 2: GetRange timeout (marketdata.odin)

After the per-cell timeout loop, added compare pane timeout loop using the same
`GETRANGE_TIMEOUT_FRAMES` constant (300 frames ≈ 5s at 60fps).

### Fix 3: Lazy loading (marketdata.odin)

Extended `check_lazy_candle_loading` to scan compare panes when in compare candle
mode (widget_idx=2). Uses per-pane `scroll_x`/`zoom` and the slot's `candle_store`
to detect near-edge scroll position. Respects global `MAX_CONCURRENT_GETRANGE`
budget (which now includes compare pane pending count).

### Fix 4: Initial backfill (actions.odin)

- `apply_enter_compare`: calls `request_compare_pane_candle_range` for all initial panes
- `apply_add_compare_stream`: calls `request_compare_pane_candle_range` for the newly added pane

### Fix 5: Reconnect clearing (marketdata.odin)

Added `state.compare.getranges[cpi] = {}` loop in the reconnect detection block,
alongside the existing per-cell clearing.

## Code Changes

| File | Lines | Change |
|------|-------|--------|
| `marketdata.odin` | +42 | Range batch routing, timeout, lazy loading, reconnect clearing |
| `actions.odin` | +6 | Initial backfill on compare enter/add |
| `store_boundary_test.odin` | +115 | 10 new S41 tests |

## Tests (10 new)

| Test | Validates |
|------|-----------|
| `test_s41_pane_lifecycle_empty_to_composed` | Full 4-phase lifecycle: Empty → Pending → Backfilled → Composed |
| `test_s41_range_complete_clears_pending_and_sets_oldest` | `mark_range_complete` transitions |
| `test_s41_range_complete_preserves_older_oldest_ts` | Only updates oldest_ts if smaller |
| `test_s41_getrange_timeout_detected` | Timeout fires at correct frame boundary |
| `test_s41_getrange_timeout_not_fired_when_not_pending` | No false timeout when not pending |
| `test_s41_reconnect_clears_getrange_state` | Reconnect clears pending+request_id, preserves seeded |
| `test_s41_independent_panes_different_lifecycle_stages` | 3 panes at Composed/Backfilled/Pending simultaneously |
| `test_s41_composition_should_extend_guards` | All guard conditions for lazy loading extension |
| `test_s41_tf_change_resets_then_reseeds` | Full TF change → re-request → re-compose cycle |
| `test_s41_composition_intent_lifecycle` | Orchestrator intent through all phases |

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Batch slot_idx mismatch for per-pane TF subscriptions | Low | `compare_pane_resolve_subject_id` resolves to TF-matched slot; `find_market_channel_slot` handles this |
| Concurrent getrange budget exceeded with 4 panes | Low | Compare panes counted in `pending_count` before MAX_CONCURRENT_GETRANGE check |
| Stale pane getrange after TF change race | Low | `apply_set_compare_pane_timeframe` already resets getrange and re-requests |

## Recommended S42

**Compare Pane Timeline Integration** — Each compare pane should fetch its own
timeline for the pane's market + TF, enabling per-pane boundary-aware lazy loading
(`oldest_ts <= timeline.first_ts` guard). Currently the global `state.timeline`
only covers the active stream.
