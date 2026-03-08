# Stage 63 — UX / Performance Hardening Report

**Date:** 2026-03-07
**Status:** COMPLETE
**Focus:** Render performance, overlay robustness, empty state consistency, dead code removal

---

## Diagnostic Summary

Full codebase audit across 3 axes: render hot paths, reconnect/recovery robustness, grid/workspace performance. Architecture strengths confirmed: Cell_View_Model (S61), Page_Module contract (S57), modal z-ordering, escape cascade, widget decoupling.

## Changes Implemented

### 1. Detail Panel Store Resolution (PERF — High Impact)

**File:** `build_dashboard.odin:183,192`
**Before:** `resolve_stores_for_cell()` called 2x per analytics/profile cell per frame in the ANALYTICS section of the detail panel. Each call traverses slot registry (O(STREAM_VIEW_CAP)) + binding resolution.
**After:** Direct access to global stores (`&state.stores.session_vpvr`, `&state.stores.tpo`). The detail panel only needs POC label data — full store resolution is unnecessary here.
**Impact:** Eliminates ~14 redundant slot lookups per frame when detail panel analytics section is expanded.

### 2. Compare Pane Double Subject ID Resolution (PERF — Medium Impact)

**File:** `stream_slots.odin:697,714→750`
**Before:** `resolve_compare_surface_view()` called `compare_pane_resolve_subject_id()` at line 697, then `resolve_compare_pane_composition()` at line 714 which called `compare_pane_resolve_subject_id()` again internally at line 750. Each call traverses slot registry + market matching.
**After:** Extracted `resolve_compare_pane_composition_for_sid()` that accepts pre-resolved `eff_sid`. Surface view passes the already-resolved ID, eliminating the second call.
**Impact:** 1 fewer slot registry walk per compare pane per frame (4 panes × 60fps = 240 eliminated lookups/sec).

### 3. Grid Resize Border Detection O(n²) → O(n) (PERF — Medium Impact)

**File:** `grid_resize.odin:39-57`
**Before:** Column border hover detection re-accumulated positions from scratch for each column boundary (`O(n²)` where n = col_count).
**After:** Pre-compute cumulative column positions incrementally — single pass with running accumulator.
**Impact:** Reduced per-frame work during hover from quadratic to linear. Also eliminates redundant `col_weight_sum()` calls inside the inner loop.

### 4. Weight Sum Caching During Resize Drag (PERF — Medium Impact)

**File:** `grid_resize.odin:20-30,73-85`
**Before:** `col_weight_sum()` called 4x per frame during active column drag, `row_weight_sum()` called 4x during row drag.
**After:** Cached in a single `s` variable computed once per frame.
**Impact:** 3 fewer function calls per frame during drag (continuous 60fps path).

### 5. Overlay Close Pattern Consistency (UX — Medium Impact)

**Files:** `overlays.odin`, `actions.odin`
**Before:** Mixed patterns for overlay dismiss:
- Exchange manager: `queue_ui_action(.Toggle_Connection_Modal)` → processed next frame
- Stream picker: `queue_ui_action(.Toggle_Stream_Picker)` without `return` → continued rendering
- Widget catalog: direct mutation `state.overlays.show_widget_catalog = false`
- Cell picker: direct mutation `state.overlays.cell_stream_picker_open = -1`

**After:** All overlay close actions use direct mutation + early return:
- Exchange manager click-outside: direct `show_exchange_manager = false` + return
- Stream picker click-outside: direct `show_stream_picker = false` + return (was missing return!)
- Escape cascade: all overlay closes use direct mutation

**Impact:** Eliminates 1-frame delay on overlay close. Fixes stream picker click-outside bug where rendering continued after dismiss (potential click-through to underlying UI).

### 6. Trades Widget Empty State (UX — Medium Impact)

**File:** `trades_widget.odin:72-79`
**Before:** No empty state handler. When store is nil or has 0 trades, the widget attempted to access `data.store.count` (nil-unsafe) and rendered an empty scroll area with headers but no feedback.
**After:** Added consistent empty state: centered "Waiting for trades..." text with alpha 0.3, matching the pattern used by orderbook, stats, vpvr, dom, and heatmap widgets.
**Impact:** Users see clear feedback instead of an empty table with headers.

### 7. Dead Code Removal (CLEANUP)

**File:** `stream_slots.odin:596-656`
**Removed:** `resolve_cell_surface_view()` — 60 lines of dead code. This proc was superseded by `resolve_cell_view_model()` in S61, which internally uses `resolve_cell_surface_view_with_stores()` instead. No callers remained.
**Impact:** Reduces cognitive load and potential confusion.

---

## Verification

| Check | Result |
|-------|--------|
| `make check-core` | PASS — all 10 packages |
| `make check-wasm-compile` | PASS |
| `make check-core-imports` | PASS — no forbidden imports |
| `make check-widgets` | PASS — offline soak probe clean |

---

## Issues Diagnosed but NOT Fixed (Deferred)

| Issue | Reason | Risk |
|-------|--------|------|
| Recovery marked before resubscribe (health.odin:447) | Functional — recovery attempts increment is intentional to prevent rapid retries | Low |
| GetRange timeout no auto-retry | Already handled by lazy loading on scroll + TF change reset | Low |
| Compare pane resync affects all panes | reconcile_subscriptions is idempotent; cross-pane effect is benign | Low |
| Candle TF_Adaptive staleness not auto-recovered | Design decision — low-volume markets naturally sparse | Expected |

---

## Architecture Strengths Preserved

1. **Cell_View_Model** — single resolution per cell per frame
2. **Page_Module contract** — clean navigation with lifecycle hooks
3. **Modal z-ordering** — proper stack discipline via `current_z_layer`
4. **Escape cascade** — hierarchical, non-overlapping priority
5. **ECS world model** — component arrays with value semantics
6. **Port-driven architecture** — platform abstraction via proc tables

## Files Modified

| File | Lines Changed | Category |
|------|---------------|----------|
| `build_dashboard.odin` | 183-199 | Perf: store resolution |
| `stream_slots.odin` | 596-656 removed, 744-770 refactored | Perf + cleanup |
| `grid_resize.odin` | 15-57, 68-87 | Perf: weight caching + O(n) |
| `overlays.odin` | 86-89, 316-318 | UX: overlay consistency |
| `actions.odin` | 52-60 | UX: escape cascade |
| `trades_widget.odin` | 72-79 | UX: empty state |

**Total:** 6 files, ~30 lines added, ~80 lines removed/refactored
