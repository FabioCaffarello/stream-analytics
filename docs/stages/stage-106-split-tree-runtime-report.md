# Stage 106 — Split Tree Runtime

**Date:** 2026-03-08
**Status:** COMPLETE

## Objective

Implement the split tree runtime that replaces the legacy grid layout with a pane-based split tree system. The tree drives all dashboard layout: computing pane rects, handling split edge resize, and supporting pane splitting/removal/rotation.

## Architecture

### Tree as Layout Authority

The split tree (ADR-0025) is now the sole layout engine for the dashboard. `build_workspace_dashboard` replaces `build_dashboard_grid` in `build_ui.odin`:

1. **Resolve tree** → `resolve_tree_layout_full` produces per-pane rects + per-node bounds
2. **DFS traversal** → `tree_collect_pane_ids` maps panes to Entity_World cell indices
3. **Render cells** → existing `render_cell_widget` receives tree-computed rects
4. **Resize** → `update_split_resize` detects and handles drag on split edges
5. **Mutations** → `tree_split_pane` / `tree_remove_pane` with Entity_World sync

### Bridge: Tree ↔ Entity_World

The tree handles layout; Entity_World remains the rendering data store (S106 bridge pattern, per ADR-0029 Phase 2):

- **DFS order = cell order**: position `i` in DFS traversal = cell index `i` in Entity_World
- **Init sync**: `workspace_sync_from_world` rebuilds tree from Entity_World state after layout restoration
- **Mutation sync**: `workspace_on_cell_added` / `workspace_on_cell_removed` update tree on cell mutations
- **Auto-layout**: `build_auto_workspace_tree` creates balanced binary trees for arbitrary pane counts; 7-panel case uses purpose-built `build_default_workspace_tree` for optimal proportions

## Changes

### New Files

| File | Lines | Purpose |
|------|-------|---------|
| `build_workspace.odin` | ~210 | Workspace tree rendering + resize + sync |

### Modified Files

| File | Change |
|------|--------|
| `workspace.odin` | +165 lines: `tree_collect_pane_ids`, `resolve_tree_layout_full`, `tree_split_pane`, `build_auto_workspace_tree` |
| `workspace_test.odin` | +290 lines: 22 new tests covering collect, full layout, split, auto-tree, DFS stability |
| `app.odin` | +5 lines: `ws_registry: Workspace_Registry` in App_State, init + settings sync |
| `build_ui.odin` | Dashboard dispatch routes to `build_workspace_dashboard` |
| `actions.odin` | +9 lines: `Split_Pane_H/V`, `Rotate_Split` handlers + Ctrl+H/J/R shortcuts |
| `actions_cell_mutations.odin` | +65 lines: `apply_split_pane_action`, `apply_rotate_split_action`, tree sync on add/remove |

### Preserved (Legacy Compat)

| File | Status |
|------|--------|
| `grid_resize.odin` | Kept — used only if no workspace tree is available |
| `build_dashboard.odin` | Kept — `build_dashboard_grid` retained as fallback path |
| `build_compare.odin` | Unchanged — compare mode uses its own layout (future tree integration) |
| `build_focus.odin` | Unchanged — focus mode uses its own layout (future tree integration) |

## Runtime Operations

### Layout Resolution

```
resolve_tree_layout_full(tree, bounds)
  → pane_rects: [PANE_MAX]Rect      // per-pane positioned rects
  → node_bounds: [TREE_NODE_MAX]Rect // per-node bounds (for resize)
```

Recursive descent producing both pane rects and internal node bounds in a single pass.

### Split Edge Resize

- **Hit detection**: 6px tolerance on split edges, scan all Split_H/Split_V nodes
- **Visual feedback**: 2px blue highlight on hover
- **Drag**: updates `tree_set_ratio` with clamped ratio (min_size..1-min_size)
- **State**: `Split_Resize_State.active_node` tracks which node is being resized

### Pane Operations

| Operation | Shortcut | Description |
|-----------|----------|-------------|
| Split H | Ctrl+H | Split focused pane horizontally (left/right) |
| Split V | Ctrl+J | Split focused pane vertically (top/bottom) |
| Rotate | Ctrl+R | Toggle Split_H ↔ Split_V at focused pane's parent |
| Remove | (existing) | Remove cell via close button or context menu |

### Tree Sync

- `workspace_sync_from_world(state)` — full rebuild from Entity_World (init/restore)
- `workspace_on_cell_added(state, ci, widget)` — splits last pane to accommodate new cell
- `workspace_on_cell_removed(state, ci)` — removes pane at DFS position, promotes sibling

### Auto-Layout Algorithm

`build_auto_workspace_tree` produces balanced binary trees:

| Panes | Layout |
|-------|--------|
| 1 | Single pane |
| 2 | H(p0, p1) |
| 3 | V(p0, H(p1, p2)) |
| 4 | V(H(p0,p1), H(p2,p3)) |
| 5 | V(V(p0,p1), V(p2, H(p3,p4))) |
| 7 | Default tree (purpose-built proportions) |

Alternates V/H at each depth level, with H preferred for 2-pane groups.

## Test Coverage

**120 tests total** (49 S105 + 22 S106 + 49 other app tests)

New S106 tests:
- `test_tree_collect_pane_ids_empty` — empty tree returns 0 panes
- `test_tree_collect_pane_ids_single` — single pane tree
- `test_tree_collect_pane_ids_default_tree` — 7-pane DFS order matches factory
- `test_tree_collect_compare_4` — 4-pane compare tree DFS order
- `test_resolve_tree_layout_full` — full resolution matches standard resolution
- `test_tree_split_pane_horizontal` — split produces 50/50 H layout
- `test_tree_split_pane_vertical` — split produces 50/50 V layout
- `test_tree_split_pane_nested` — double-split produces valid 3-pane tree
- `test_tree_split_pane_invalid_direction` — rejects non-split direction
- `test_auto_workspace_tree_1` — single pane auto layout
- `test_auto_workspace_tree_2` — 2-pane H split
- `test_auto_workspace_tree_4` — 4-pane 2x2 grid
- `test_auto_workspace_tree_5` — 5-pane balanced tree
- `test_dfs_order_stable_after_resize` — DFS order unchanged after ratio change
- `test_split_then_remove_restores_original` — split+remove roundtrip

## Validation

- [x] All 120 tests pass
- [x] All 10 core packages compile clean (`make check-core`)
- [x] Native binary builds (`make build-native`)
- [x] Zero regressions
- [x] Zero wire-breaking changes
- [x] Tree-driven layout produces identical visual output to grid for default 7-panel layout
- [x] Split edge resize with 6px hit detection and visual feedback
- [x] Nested splits supported (arbitrary depth, max 31 nodes)
- [x] Layout restore from settings rebuilds workspace tree
- [x] Grid code retained as fallback (no workspace → uses legacy grid)

## Acceptance Criteria

**Dashboard functioning with pane system, not grid** — SATISFIED:
- Tree is sole layout authority for dashboard rendering
- Dynamic resize via split edge drag
- Nested splits supported
- Layout restore from persistence
- Pane split/remove/rotate operations available
