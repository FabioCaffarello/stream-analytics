# Stage 105 — Dashboard Workspace Architecture

**Date:** 2026-03-08
**Status:** Complete
**Branch:** codex/s9-legacy-removal-cutover

## Objective

Implement the structural foundation for the tree-based dashboard workspace architecture, replacing the conceptual reliance on grid-based layout with a recursive split tree model. This stage introduces types only — no rendering paths are changed.

## What Changed

### New Files

| File | Purpose |
|------|---------|
| `workspace.odin` | Foundation types: `Split_Tree`, `Split_Node`, `Pane`, `Pane_Pool`, `Widget_Host`, `Widget_Descriptor`, `Workspace`, `Workspace_Registry`, `Focus_State`, `Workspace_Data_Context` |
| `workspace_tree.odin` | Tree factory functions (`build_default_workspace_tree`, `build_chart_focus_workspace_tree`, `build_compact_workspace_tree`, `build_focus_workspace_tree`, `build_compare_workspace_tree`, `build_analysis_workspace_tree`) + `resolve_tree_layout` + tree mutations + validation |
| `workspace_test.odin` | 38 tests covering pane pool, tree builder helpers, layout resolution, all 6 tree factories, tree mutations (swap/rotate/remove/set_ratio), widget host, workspace init, workspace registry, tree validation |

### Architecture (ADR Implementation)

| ADR | Implementation |
|-----|---------------|
| ADR-0024 (Workspace) | `Workspace` aggregate root with tree + pane pool + data context + focus + mode |
| ADR-0025 (Split Tree) | `Split_Tree` with `TREE_NODE_MAX=31` nodes, `Split_Node_Kind` (Split_H/Split_V/Pane/Stack), recursive `resolve_tree_layout` |
| ADR-0026 (Pane) | `Pane` with stable `Pane_ID` (u16 monotonic), `Pane_Pool` (fixed-capacity 16), `Pane_View_State` with frame-transient rect |
| ADR-0027 (Widget Host) | `Widget_Host` with lifecycle state, `Widget_Descriptor` table (`@(rodata)` for Odin indexing), `widget_host_create` populates from descriptor + runtime channel/bundle resolution |

### Tree Factories (Pixel-Exact Grid Equivalents)

| Factory | Replaces | Layout |
|---------|----------|--------|
| `build_default_workspace_tree` | `build_default_grid` | V(candle 40%, V(H(stats,counter) 30%, V(H(heatmap,vpvr) 52.4%, H(trades,ob)))) |
| `build_chart_focus_workspace_tree` | `build_chart_focus_grid` | V(candle 70%, H(trades,ob)) |
| `build_compact_workspace_tree` | `build_compact_grid` | H(candle 65%, ob 35%) |
| `build_focus_workspace_tree` | `build_focus.odin` | H(candle 75%, ob 25%) |
| `build_compare_workspace_tree` | `build_compare_grid` | H of N panes (N=4 → V(H(p0,p1), H(p2,p3))) |
| `build_analysis_workspace_tree` | `build_analysis_grid` | V(candle 50%, V(H(heatmap,vpvr) 50%, H(trades,ob))) |

### Key Design Decisions

1. **Pane_ID is 1-based** — `PANE_ID_NONE = 0` is the sentinel; tree leaf `children[0]` stores the Pane_ID cast as i8
2. **Layout resolution indexes by `pid - 1`** — result array is `[PANE_MAX]Rect` indexed by Pane_ID offset
3. **Widget descriptor uses `@(rodata)` global var** — Odin constants cannot be indexed with variable indices
4. **Channels/bundle resolved at runtime** — `channels_for_widget` / `layer_bundle_for_widget` require context, cannot be called at global scope

### Tree Mutations

| Operation | Status |
|-----------|--------|
| `tree_swap_panes` | Implemented + tested |
| `tree_set_ratio` | Implemented + tested (clamped to min_size) |
| `tree_rotate` | Implemented + tested (Split_H ↔ Split_V) |
| `tree_remove_pane` | Implemented + tested (sibling promotion) |
| `tree_validate` | Implemented + tested (structural invariant check) |

## Test Results

- **38 new tests** in `workspace_test.odin`
- **105 total** app package tests (67 existing + 38 new)
- All pass in 26ms

## What's Next (S106+)

Per ADR-0029 migration plan:
- **S106:** Wire workspace to App_State (dual-path behind `use_workspace_tree` flag)
- **S107–S109:** Pane render path, interactive tree resize, visual regression
- **S110–S111:** Stream slot migration to workspace data context
- **S112–S113:** Legacy removal (Grid_Def, Entity_World, Compare_State)

## Invariants Preserved

- Zero changes to existing rendering paths
- Zero wire-breaking changes
- All existing 67 app tests continue to pass
- `check-core` passes all 10 packages
