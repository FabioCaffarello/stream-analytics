# Stage 57 — UI Architecture Baseline + Shell Modularization

**Date:** 2026-03-08
**Status:** COMPLETE
**Tests:** 510 (402 md_common + 85 services + 16 streams + 6 layers + 1 app)
**Regressions:** Zero

## Architectural Diagnostic

### Pre-S57 State (Strengths)
- S52 shell decomposition had reduced `build_ui.odin` from 880→190 lines
- ECS Entity_World provides good per-cell component model
- Sub-state decomposition (Chrome, Overlay, Zen, Compare) already extracted
- Action queue pattern (UI_Action → apply_ui_actions) is clean and extensible
- Pure functions in md_common (health, composition, surface views)

### Pre-S57 Problems Identified
1. **No page contract** — Dashboard, Markets, Settings are ad-hoc procs with no shared lifecycle
2. **Route switch has no lifecycle** — `Navigate_Route` just sets `state.chrome.active_route` with no cleanup/init hooks; stale overlay state can leak across route changes
3. **build_ui.odin owns page-level concerns** — zen fade logic, sidebar rendering, detail panel dispatch, and route switching all inlined
4. **Detail panel dispatch is a switch** — `switch state.chrome.active_route` for detail panels; grows linearly with new pages
5. **Page content dispatch is a switch** — same pattern for workspace content
6. **draw_settings_detail has mismatched signature** — missing `pointer` parameter unlike other detail procs

## Changes Implemented

### 1. Page Module Contract (`page_module.odin` — 135 lines, NEW)

Formal page lifecycle via a vtable-style dispatch table:

```
Page_Module :: struct {
    render_page:   Page_Render_Proc,     // draw page content into workspace
    render_detail: Page_Render_Detail_Proc, // draw detail panel sidebar
    on_enter:      Page_Lifecycle_Proc,  // called when route becomes active
    on_leave:      Page_Lifecycle_Proc,  // called when leaving route
}
```

- **Compile-time page table** `PAGE_MODULES :: [Route]Page_Module{...}` — zero allocation, no runtime registration
- **Shell-facing dispatch**: `page_render`, `page_render_detail`, `page_navigate`
- **Lifecycle-aware navigation**: `page_navigate(state, from, to)` calls on_leave → close_all_overlays → set route → on_enter
- Thin adapters wrap existing render procs to satisfy the contract

### 2. Route Lifecycle (`actions.odin`)

`Navigate_Route` action now calls `page_navigate(state, old_route, new_route)` instead of bare assignment. This:
- Closes all modal overlays on route change (prevents stale overlay leaks)
- Provides on_enter/on_leave hooks for future pages (e.g., init analytics aggregation when entering Analytics page)
- Is backwards-compatible: existing pages have nil lifecycle procs

### 3. Shell Extraction (`shell_common.odin` — 134→195 lines)

Extracted from `build_ui.odin`:
- **`zen_update_fade`** — pure state mutation for zen mode edge-triggered fade (top/bottom/left)
- **`update_detail_resize`** — detail panel resize handle interaction + rendering

### 4. Shell Orchestrator (`build_ui.odin` — 190→129 lines, -32%)

- Replaced detail panel switch with `page_render_detail(state, route, rect, pointer)`
- Replaced page content switch with `page_render(state, route, workspace, pointer)` for non-Dashboard routes
- Dashboard retains inline mode dispatch (focus/compare/grid) because these are viewport-layout modes, not separate pages
- Zen fade delegated to `zen_update_fade`
- Resize handle delegated to `update_detail_resize`

### 5. Signature Normalization (`settings.odin`)

`draw_settings_detail` gained a `pointer: ui.Pointer_Input = {}` parameter (default value) to match the `Page_Render_Detail_Proc` contract without breaking existing callers.

## Risk Mitigation

| Risk | Mitigation |
|---|---|
| Page dispatch adds indirection | Compile-time const table, zero runtime cost; Odin inlines through proc pointers on const tables |
| Dashboard mode dispatch outside page contract | Intentional: focus/compare/grid need workspace_input + workspace_pointer which aren't in the page contract. Dashboard's page_render_page is a sentinel. |
| Stale overlay on route change | `page_navigate` calls `close_all_overlays` between on_leave and on_enter |
| Settings detail signature change | Default parameter value preserves backward compatibility |

## What This Enables for S58+

1. **New pages**: add a `Route` enum variant, a `Page_Module` entry, render procs. Zero shell changes.
2. **Page lifecycle**: on_enter can init page-local state (e.g., aggregation views); on_leave can clean up.
3. **Dashboard sub-pages**: the pattern supports nested page modules for future Dashboard tabs.
4. **Page-local overlays**: on_leave cleanup prevents cross-page overlay bleed.
5. **Shell remains thin**: at 129 lines, the shell is a pure layout orchestrator. Page logic is fully decoupled.

## File Summary

| File | Before | After | Delta |
|---|---|---|---|
| `build_ui.odin` | 190 | 129 | -61 (-32%) |
| `page_module.odin` | — | 135 | +135 (new) |
| `shell_common.odin` | 134 | 195 | +61 |
| `actions.odin` | 548 | 548 | 0 (1 line changed) |
| `settings.odin` | 293 | 293 | 0 (signature only) |
| **Net** | | | **+74 lines** |

Zero wire changes. Zero new mutable state. Zero breaking changes.
