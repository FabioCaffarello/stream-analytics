# ADR-0025 — Split Tree Layout Model

**Status:** Accepted
**Date:** 2026-03-08
**Owners:** Core maintainers
**Supersedes:** —
**Related:** ADR-0024, ADR-0026

## Context

The current layout system uses a weighted grid (`Grid_Def`) with fixed cell indices (`PANEL_CANDLE=0..PANEL_ORDERBOOK=6`), column/row weights, and span overrides. This approach has limitations:

1. **Fixed topology.** The grid is always rectangular — you cannot have 3 panes in the top row and 2 in the bottom without span hacks.
2. **No nesting.** A cell cannot contain a sub-split (e.g., candle chart on top, trades+orderbook stacked below).
3. **Layout presets are rigid.** Four hardcoded presets (`build_default_grid`, `build_filtered_grid`) with fixed visibility arrays. Users cannot create arbitrary arrangements.
4. **Compare mode duplicates layout logic.** `build_compare_grid()` is a separate grid builder rather than a natural tree variant.
5. **Focus mode is another special case.** `build_focus.odin` computes a bespoke 75/25 split.

A tree-based layout model eliminates all five problems with a single recursive structure.

## Decision

### 1. Node Types

```
Split_Node_Kind :: enum u8 {
    Split_H,    // horizontal split: children arranged left → right
    Split_V,    // vertical split: children arranged top → bottom
    Pane,       // leaf: hosts a single widget (references Pane_ID)
    Stack,      // leaf: tabbed stack of widgets (references N Pane_IDs)
}
```

### 2. Tree Structure

```
TREE_NODE_MAX :: 31   // 16 panes → at most 15 internal nodes + 16 leaves

Split_Node :: struct {
    kind:        Split_Node_Kind,
    parent:      i8,             // -1 = root
    children:    [2]i8,          // left/right or top/bottom (Split_H, Split_V)
                                 // for Pane/Stack: children[0] = pane_id, children[1] = -1
    ratio:       f32,            // 0.0–1.0, split position (only for Split_H/Split_V)
    min_size:    f32,            // minimum fraction (e.g., 0.08 = 8% of parent)
}

Split_Tree :: struct {
    nodes:       [TREE_NODE_MAX]Split_Node,
    count:       u8,
    root:        i8,             // index of root node (-1 = empty)
}
```

### 3. Layout Resolution

```
resolve_tree_layout :: proc(tree: ^Split_Tree, bounds: Rect) -> [PANE_MAX]Rect
```

Recursive descent from root:
- **Split_H:** Split `bounds` horizontally at `ratio` → recurse left child with left rect, right child with right rect.
- **Split_V:** Split `bounds` vertically at `ratio` → recurse top child with top rect, bottom child with bottom rect.
- **Pane:** Assign `bounds` to pane's resolved rect.
- **Stack:** Assign `bounds` to the active tab's pane rect (tab bar consumes 24px from top).

Minimum size enforcement: clamp each child's extent to `min_size * parent_extent`.

### 4. Built-in Tree Factories

Replace current grid builders with tree constructors:

| Current | Tree Equivalent |
|---|---|
| `build_default_grid()` | `build_default_tree()` — V(H(candle, H(stats, counter)), H(heatmap, vpvr), H(trades, orderbook)) |
| Preset 1 (Chart Focus) | V root with ratio=0.70: candle top, H(trades, orderbook) bottom |
| Preset 2 (Analysis) | V(candle 50%, H(heatmap, vpvr) 25%, H(trades, orderbook) 25%) |
| Preset 3 (Compact) | H(candle 65%, orderbook 35%) |
| `build_compare_grid(n)` | H of N panes with equal ratios (n=2: H(pane, pane) at 0.5) |
| Focus mode | H(candle 75%, orderbook 25%) |

### 5. Interactive Resize

Replace `grid_col_resize` / `grid_row_resize` with tree-edge resize:

```
Split_Resize_State :: struct {
    active_node: i8,    // -1 = idle, else index of Split_H/Split_V node being resized
    start_ratio: f32,   // ratio at drag start
    start_pos:   f32,   // mouse position at drag start
}
```

- Hover detection: walk tree, find nearest split edge within 4px of cursor.
- Drag: adjust `node.ratio` proportionally, clamped by `min_size`.
- Cursor: `↔` for Split_H, `↕` for Split_V.

### 6. Tree Mutations

All mutations produce a new valid tree (no in-place corruption):

| Operation | Description |
|---|---|
| `tree_add_pane(tree, target_pane, direction, new_pane)` | Split `target_pane`'s parent into `direction` (H or V), insert `new_pane` |
| `tree_remove_pane(tree, pane_id)` | Remove leaf, promote sibling to parent's position |
| `tree_swap_panes(tree, a, b)` | Swap two leaf pane_ids (drag-drop reorder) |
| `tree_set_ratio(tree, node_idx, ratio)` | Resize split |
| `tree_rotate(tree, node_idx)` | Toggle Split_H ↔ Split_V |
| `tree_stack_pane(tree, target_stack, pane_id)` | Add pane as tab to existing Stack node |

### 7. Serialization

Tree serializes as a flat array of nodes with parent/child indices — no pointers, no heap allocation. Compatible with `codec.Marshal/Unmarshal` and workspace persistence.

### 8. Invariants

- **Binary splits only.** Each Split_H/Split_V has exactly 2 children. N-way splits are composed from nested binary splits.
- **Leaves are Pane or Stack.** No empty leaves.
- **Root always exists** (even single-pane workspace has root = Pane node).
- **Ratios in [min_size, 1.0 - min_size].** Enforced on every mutation.
- **Tree depth ≤ 6.** Practical limit for 16 panes; prevents degenerate chains.

## Consequences

- Grid presets become tree templates — same concept, more flexibility.
- Compare mode is just a tree with N equal-ratio splits — no special-case code.
- Focus mode is a tree with a 75/25 split — no special-case code.
- Users can create asymmetric layouts (3 top, 2 bottom) naturally.
- Resize becomes per-edge rather than per-column/per-row — more intuitive.
- Tree serialization replaces `custom_grid_def` + `layout_preset` + per-cell spans.

## Alternatives

1. **Flex-box / CSS-like model.** Rejected: too abstract for a fixed-capacity immediate-mode renderer. Binary tree maps directly to `rect_split_h` / `rect_split_v` which already exist in `layout.odin`.
2. **Grid with arbitrary spans.** Rejected: spans create overlap ambiguities and don't support nesting.
3. **Free-form floating panes.** Rejected: adds z-ordering complexity, doesn't match terminal-style tiling UX.

## Evidence

- `core/ui/layout.odin` already provides `rect_split_h`, `rect_split_v` — tree resolution maps 1:1.
- `core/ui/grid.odin` `compute_grid` will be superseded — no external consumers outside `build_dashboard.odin`.
- `grid_resize.odin` column/row resize logic (120 lines) replaced by ~60-line tree-edge resize.

## Changelog

- 2026-03-08: Initial acceptance.
