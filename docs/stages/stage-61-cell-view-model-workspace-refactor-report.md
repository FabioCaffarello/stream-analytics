# Stage 61 — Cell View-Models + Reduction of UI Coupling

**Date:** 2026-03-08
**Status:** COMPLETE
**Tests:** 528 (402 md_common + 112 services + 14 app)

## Diagnosis

### Pre-S61 Architecture Assessment

The workspace architecture was already more mature than expected, with three existing read-model patterns:

1. **Cell_Surface_View (S36)** — per-cell read model for composition, health, staleness, identity
2. **Cell_Stores** — resolved per-cell data store pointers (global vs per-slot)
3. **Layer_Context** — fully decoupled context for layer-based widget renders

### Identified Coupling Issues

| Issue | Impact | Location |
|-------|--------|----------|
| **Double store resolution** | `resolve_cell_surface_view()` calls `resolve_stores_for_cell()` internally, then analytics/session-profile widgets call it AGAIN | `stream_slots.odin:498` + `render_analytics.odin:21` |
| **No unified cell contract** | Surface view, stores, and config (analytics_kind, effective TF) resolved independently by different call sites | `build_cell.odin` + widget renders |
| **Widget renders take ^App_State** | Analytics/session-profile procs receive full App_State when they only need `cmd_buf` + resolved data | `render_analytics.odin:14`, `render_session_profile.odin:12` |
| **Scattered TF resolution** | `cell_effective_tf_idx()` called from header, surface view, and store resolution — up to 3 times per cell per frame | Multiple files |

## Solution: Cell_View_Model

### New Struct (`stream_slots.odin`)

```odin
Cell_View_Model :: struct {
    surface:        Cell_Surface_View,      // composition, health, identity
    stores:         Cell_Stores,            // resolved store pointers
    widget_kind:    Widget_Kind,            // from ECS component
    tf_idx:         int,                    // effective TF (resolved once)
    tf_string:      string,                 // "1m", "5m", etc.
    tf_ms:          i64,                    // TF in milliseconds
    analytics_kind: services.Analytics_Kind, // for Analytics widgets
    show_history:   bool,                   // for Analytics widgets
    cell_idx:       int,                    // cell index
    focused:        bool,                   // is focused cell
}
```

### Resolution Pattern

```
resolve_cell_view_model(state, ci) → Cell_View_Model
  ├── Effective TF resolved ONCE (was 3x)
  ├── Stores resolved ONCE via resolve_stores_for_cell (was 2x for analytics cells)
  ├── Surface view resolved via resolve_cell_surface_view_with_stores (reuses pre-resolved stores)
  ├── Widget config extracted from ECS components
  └── Cell identity (idx, focused) extracted
```

### Decoupled Widget Dispatch

**Before:**
```odin
render_analytics_cell(state: ^App_State, ci: int, cell_vp: ui.Rect)
  → calls resolve_stores_for_cell(state, ci)  // REDUNDANT
  → reads state.world.analytics[ci]           // DIRECT ECS ACCESS
  → passes state to sub-renderers             // FULL STATE LEAKED
```

**After:**
```odin
render_analytics_cell_vm(cmd_buf: ^ui.Command_Buffer, vm: Cell_View_Model, cell_vp: ui.Rect)
  → uses vm.stores.analytics                  // PRE-RESOLVED
  → uses vm.analytics_kind, vm.show_history   // FROM VIEW MODEL
  → passes cmd_buf to sub-renderers           // MINIMAL COUPLING
```

## Changes

### Files Modified

| File | Change | Lines |
|------|--------|-------|
| `stream_slots.odin` | Added `Cell_View_Model` struct, `resolve_cell_view_model()`, `resolve_cell_surface_view_with_stores()` | +110 |
| `build_cell.odin` | Resolve view model once, consume throughout header + widget dispatch | Rewritten (202 lines) |
| `render_analytics.odin` | All procs take `^Command_Buffer` instead of `^App_State` | Rewritten (373 lines) |
| `render_session_profile.odin` | All procs take `^Command_Buffer` instead of `^App_State` | Rewritten (282 lines) |

### Files Created

| File | Purpose |
|------|---------|
| `cell_view_model_test.odin` | 13 tests for Cell_View_Model resolution |

## Coupling Reduction

### Per-Cell-Per-Frame Resolution Count

| Resolution | Before S61 | After S61 |
|-----------|-----------|----------|
| `resolve_stores_for_cell()` | 2x (surface_view + widget render) | 1x (view model) |
| `cell_effective_tf_idx()` | 3x (header + surface_view + stores) | 1x (view model) |
| `resolve_cell_surface_view()` | 1x | 1x (via `_with_stores`, reuses pre-resolved stores) |
| Direct `state.world.*` reads in render | 5+ (widget kind, analytics, TF) | 0 (all via view model) |

### ^App_State Dependency

| Render Proc | Before | After |
|------------|--------|-------|
| `render_analytics_cell` | `^App_State` (13.4MB) | `^Command_Buffer` (~2KB) |
| `render_analytics_oi` | `^App_State` | `^Command_Buffer` |
| `render_analytics_delta_volume` | `^App_State` | `^Command_Buffer` |
| `render_analytics_cvd` | `^App_State` | `^Command_Buffer` |
| `render_analytics_bar_stats` | `^App_State` | `^Command_Buffer` |
| `render_analytics_sparkline` | `^App_State` | `^Command_Buffer` |
| `render_analytics_delta_bars` | `^App_State` | `^Command_Buffer` |
| `render_session_profile_cell` | `^App_State` | `^Command_Buffer` |
| `render_session_vpvr` | `^App_State` | `^Command_Buffer` |
| `render_tpo_profile` | `^App_State` | `^Command_Buffer` |

**10 render procs decoupled from App_State.**

## Tests

13 new tests covering:

- Nil state handling
- Out-of-bounds cell index
- Widget kind resolution
- Global TF resolution
- Per-cell TF override resolution
- Analytics config (kind + show_history) resolution
- Focus tracking
- Cell index correctness
- Store default-to-global for follow-active cells
- Surface composition (empty default)
- Surface live data flag (false default)
- Follow-active not stream_bound
- Bound cell is stream_bound

## Architectural Impact

1. **Widget renders are pure** — they take a command buffer + pre-resolved data. No side effects, no state reads.
2. **Single resolution point** — `resolve_cell_view_model()` is the canonical place where all per-cell state is assembled. Future cell state additions go here.
3. **Layer-canvas widgets are unaffected** — they already had good decoupling via `Layer_Context`. The view model pattern mirrors that contract for analytics/profile widgets.
4. **Compare mode preserved** — `resolve_compare_surface_view()` path is unchanged (separate lifecycle).
5. **Zero wire changes** — no protocol, persistence, or backend changes.

## What This Enables

- Adding new per-cell derived state (e.g., cell alerts, performance badges) goes into `Cell_View_Model` — widgets automatically get it
- Widget renders can be tested in isolation with synthetic `Cell_View_Model` values
- Future widget types receive the same contract — no need to understand App_State internals
- The view model is the right place to add per-frame caching if profiling shows resolve overhead
