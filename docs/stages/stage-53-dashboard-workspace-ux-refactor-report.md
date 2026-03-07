# Stage 53 — Dashboard / Workspace UX Refactor

**Date:** 2026-03-07
**Branch:** `codex/s9-legacy-removal-cutover`
**Predecessor:** S52 (UI Shell Architecture)

## Objective

Refactor the dashboard interior into a coherent workstation page with:
- Separated orchestration logic from render logic
- All state mutations routed through the action queue
- Shared composition badge/health dot rendering
- Fixed focus mode subject resolution
- Enforced modal exclusivity

## Changes

### 1. Extract Context Menu + Grid Resize (cell_context_menu.odin, grid_resize.odin)

| File | Before | After | Δ |
|------|--------|-------|---|
| `build_dashboard.odin` | 586 lines | 392 lines | -194 (33% reduction) |
| `cell_context_menu.odin` | — | 88 lines | NEW |
| `grid_resize.odin` | — | 119 lines | NEW |

- `build_cell_context_menu()`: Right-click menu with widget types, add/remove cell, span controls
- `update_grid_col_resize()`: Column border drag-to-resize with weight persistence
- `update_grid_row_resize()`: Row border drag-to-resize with weight persistence
- `col_weight_sum()` / `row_weight_sum()` visibility widened from `private="file"` to `private="package"`

### 2. Route Span Mutations Through Action Queue

**New action kinds:**
- `Set_Cell_Span` — sets col_span and row_span for a cell
- `Clear_All_Cells` — removes all cells and opens widget catalog

**New UI_Action fields:**
- `col_span: int` — target column span
- `row_span: int` — target row span

**Eliminates 4 direct state mutations in render path:**
1. Expand Right (was: `state.world.spans[cci].col_span = cs + 1`)
2. Expand Down (was: `state.world.spans[cci].row_span = rs + 1`)
3. Reset Size (was: `spans[cci] = {1, 1}`)
4. Clear All (was: `state.world.count = 0`)

All now flow through `queue_ui_action()` → `apply_ui_actions()` → `apply_set_cell_span_action()` / `apply_clear_all_cells_action()`.

### 3. Shared Composition Badge + Health Dot (shell_common.odin)

**New procs:**
- `draw_composition_badge(cmd_buf, x, text_y, composition, measure) -> f32`
  - Renders PEND/BFILL/LIVE/COMP label with canonical colors
  - Returns cursor advance (width + gap)
- `draw_health_dot(cmd_buf, x, center_y, dot_sz, health_level, has_live_data, composition) -> f32`
  - Renders green/yellow/red square indicator
  - Returns cursor advance

**Deduplication:**
- `build_cell.odin`: 17 lines of inline badge → 2 lines (single call each)
- `build_compare.odin`: 20 lines of inline badge → 2 lines (single call each)

### 4. Focus Mode Fix + Modal Exclusivity

**Focus mode fix:**
- `build_focus.odin:25`: Was `resolve_cell_subject_id(state, 0)` (hardcoded cell 0)
- Now: `resolve_cell_subject_id(state, focus_cell)` where `focus_cell = state.world.focused` (falls back to 0)

**Modal exclusivity:**
- `close_all_overlays(state)` helper: resets all 5 overlay booleans + cell picker
- Called before opening any modal (help, exchange manager, stream picker, widget catalog, cell picker)
- Prevents multiple overlapping modals

**Mode exclusivity:**
- Entering Focus mode exits Compare mode
- Entering Compare mode exits Focus mode

## Invariants Preserved

- Zero wire protocol changes
- Zero new mutable state fields
- Zero render output changes (pixel-identical in normal usage)
- Workspace schema version unchanged (V7)
- Layout persistence format unchanged (V6)
- All keyboard shortcuts preserved
- 402 md_common tests pass (unchanged)

## Files Changed

| File | Change |
|------|--------|
| `cell_context_menu.odin` | NEW — context menu extracted |
| `grid_resize.odin` | NEW — resize handles extracted |
| `build_dashboard.odin` | Slimmed orchestrator (586→392) |
| `build_cell.odin` | Badge dedup via shared procs |
| `build_compare.odin` | Badge dedup via shared procs |
| `build_focus.odin` | Use focused cell instead of cell 0 |
| `shell_common.odin` | +draw_composition_badge, +draw_health_dot |
| `app.odin` | +Set_Cell_Span, +Clear_All_Cells action kinds + fields |
| `actions.odin` | +close_all_overlays, modal/mode exclusivity, new action handlers |
| `actions_cell_mutations.odin` | +apply_set_cell_span_action, +apply_clear_all_cells_action |

## Architecture After S53

```
build_ui.odin (190 lines) — shell orchestrator
├── build_dashboard.odin (392 lines) — grid orchestrator
│   ├── cell_context_menu.odin (88 lines) — right-click menu
│   └── grid_resize.odin (119 lines) — column/row resize
├── build_cell.odin (165 lines) — cell header + widget dispatch
├── build_compare.odin (150 lines) — compare pane rendering
├── build_focus.odin (34 lines) — focus mode rendering
├── shell_common.odin (130 lines) — shared procs (conn status, badges, overlays)
├── build_status.odin — status bar + toast/OSD
└── overlays.odin (728 lines) — modals (help, exchange, pickers, catalog)
```

## Bug Fixes

- **FIX-S53-1:** Focus mode now renders the correct focused candle cell (was always cell 0)
- **FIX-S53-2:** Opening any modal closes all other modals (prevents overlap)
- **FIX-S53-3:** Focus/Compare modes are mutually exclusive (entering one exits the other)
- **FIX-S53-4:** Span mutations no longer bypass action queue (predictable, auditable)
