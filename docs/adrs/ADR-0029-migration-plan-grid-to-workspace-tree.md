# ADR-0029 — Migration Plan: Grid → Workspace Tree

**Status:** Accepted
**Date:** 2026-03-08
**Owners:** Core maintainers
**Supersedes:** —
**Related:** ADR-0024, ADR-0025, ADR-0026, ADR-0027, ADR-0028

## Context

ADRs 0024–0028 define the target architecture: Workspace aggregate, Split Tree layout, Pane runtime, Widget Host contract, and Data Context ownership. The current codebase uses a flat grid model (`Grid_Def`, `Entity_World`, `Compare_State`, panel indices) that must be migrated incrementally without breaking the working dashboard.

This ADR defines the migration strategy: ordering, compatibility gates, and cutover plan.

## Decision

### Phase 1 — Foundation Types (S105–S106)

**Goal:** Introduce new types alongside existing code. Zero behavioral change.

**S105 — Core Types:**
- Add `split_tree.odin`: `Split_Node`, `Split_Tree`, `resolve_tree_layout()`.
- Add `pane.odin`: `Pane`, `Pane_ID`, `Pane_Pool`, lifecycle procs.
- Add `widget_host.odin`: `Widget_Host`, `Widget_Descriptor`, `WIDGET_DESCRIPTORS` table.
- Add `workspace.odin`: `Workspace`, `Workspace_Registry` (shell only, not wired).
- Add `data_context.odin`: `Workspace_Data_Context`, `Pane_Data_Context`, `resolve_pane_data_context()`.
- **Test gate:** Unit tests for tree resolution, pane pool alloc/free, descriptor completeness.

**S106 — Tree Factories:**
- Implement `build_default_tree()`, `build_chart_focus_tree()`, `build_analysis_tree()`, `build_compact_tree()`, `build_compare_tree(n)`.
- Verify pixel-exact equivalence with current grid output via snapshot tests.
- **Test gate:** Each tree factory resolves to identical rects as its grid counterpart (±1px tolerance).

### Phase 2 — Dual-Path Rendering (S107–S109)

**Goal:** Wire new types into the render path behind a feature flag. Old path remains default.

**S107 — Workspace Shell:**
- `App_State` gets `workspace_registry: Workspace_Registry` (alongside existing `world`, `custom_grid_def`, etc.).
- `build_dashboard_grid()` checks `use_workspace_tree` flag:
  - `false` (default): existing grid path.
  - `true`: workspace tree path.
- Active workspace populated from existing `Entity_World` state via `migrate_world_to_workspace()` shim.
- **Invariant:** Flag off → zero behavioral change. Flag on → visually identical output.

**S108 — Pane Render Path:**
- `render_cell_widget()` replaced by `render_pane()` in tree path.
- `render_pane()` resolves `Resolved_Data_Context`, creates `Layer_Context`, calls `Widget_Render_Proc`.
- Compare mode: tree path uses `build_compare_tree(n)` — compare panes are regular panes.
- Focus mode: tree path uses `build_chart_focus_tree()`.
- **Test gate:** Visual regression tests (screenshot diff) for all 4 presets + compare + focus.

**S109 — Interactive Resize:**
- `Split_Resize_State` replaces `grid_col_resize` / `grid_row_resize`.
- Tree-edge hover detection and drag implemented.
- Persistence: tree serialization replaces grid weight arrays.
- **Test gate:** Resize drag produces correct ratio updates. Min-size constraints enforced.

### Phase 3 — Data Context Migration (S110–S111)

**Goal:** Move stream ownership and TF resolution into workspace.

**S110 — Stream Slot Migration:**
- `Stream_View_Registry` reference moves from `App_State` to `Workspace_Data_Context`.
- `App_State` retains a pointer to active workspace's registry (for compatibility).
- `cell_effective_tf_*` procs delegate to `resolve_pane_data_context()` in tree path.
- **Invariant:** External behavior unchanged — same streams, same TF resolution.

**S111 — Analytics Context Consolidation:**
- `Analytics_Component` and `Compare_State.analytics_kind[]` consolidated into `Pane_Data_Context.analytics_kind`.
- `Subplot_Config` consolidated into pane.
- **Test gate:** Analytics rendering identical in both paths.

### Phase 4 — Legacy Removal (S112–S113)

**Goal:** Remove old grid path. Workspace tree is sole layout model.

**S112 — Grid Path Removal:**
- Remove `use_workspace_tree` flag — tree path is default and only path.
- Remove `Grid_Def`, `Grid_Cell`, `Grid_Result`, `compute_grid()` from `grid.odin`.
- Remove `build_default_grid()`, `build_auto_grid()`, `build_mobile_grid()`, `build_compare_grid()`, `build_filtered_grid()`.
- Remove `grid_resize.odin` (column/row resize handlers).
- Remove `Entity_World` parallel arrays — all state lives in `Pane_Pool`.
- Remove `Compare_State` — compare panes are regular panes in tree.
- Remove `PANEL_CANDLE..PANEL_ORDERBOOK` fixed indices.
- Remove `layout_preset` / `layout_mode` — replaced by tree templates.
- **Gate:** All tests pass, no grid references remain.

**S113 — Persistence Migration:**
- Bump `WORKSPACE_SCHEMA_VERSION` to 11.
- Migration proc: load old grid-based workspace → convert to tree-based workspace.
- Old format: grid weights + cell indices + panel visibility.
- New format: serialized `Split_Tree` + `Pane_Pool` + `Workspace_Data_Context`.
- Forward-only migration (no downgrade path).
- **Gate:** Load old workspace file → verify identical visual output after migration.

### Compatibility Constraints

| Constraint | How Addressed |
|---|---|
| **mr:layers runtime preserved** | `Layer_Strategy`, `Layer_Context`, `Market_Store` unchanged. Widget Host composes layer strategies — does not replace them. |
| **Proto-first data flow preserved** | `Market_Store` remains global. WS → `MD_Event` → `data_source_poll_and_apply()` → `Market_Store` unchanged. Workspace tree only affects layout and rendering, not data ingestion. |
| **Grid structural dependency eliminated** | Phase 4 removes `Grid_Def`, `compute_grid()`, panel indices. Tree is sole layout model. |
| **No big-bang cutover** | Dual-path (Phase 2) allows incremental validation. Feature flag enables A/B testing. |
| **Persistence backward-compatible** | Schema version gate. Old workspaces auto-migrate on load. |
| **Compare mode preserved** | Compare is a tree variant (N equal-ratio splits). All compare features (per-pane TF, analytics, subplots) preserved via pane overrides. |
| **Focus mode preserved** | Focus is a tree variant (75/25 H-split). No special-case code. |
| **Mobile layout preserved** | `build_mobile_tree()`: single-column V-split chain. Equivalent to `build_mobile_grid()`. |
| **Drag-drop panel swap preserved** | `tree_swap_panes()` in tree model. Same UX, different implementation. |
| **Keyboard navigation preserved** | Pane focus via `Ctrl+Arrow` traverses tree in spatial order (left/right/up/down from current pane's rect position). |

### Risk Mitigation

| Risk | Mitigation |
|---|---|
| Visual regression | Screenshot diff tests at each phase gate |
| Performance regression | Tree resolution is O(nodes) = O(31 max). Current grid is O(cells) = O(12). Negligible difference at 60fps. |
| Persistence corruption | Schema version gate + migration test suite |
| Scope creep | Each phase is self-contained with explicit test gates. No phase depends on features beyond its scope. |

### File Impact Summary

| Action | Files |
|---|---|
| **New files** | `split_tree.odin`, `pane.odin`, `widget_host.odin`, `workspace.odin`, `data_context.odin`, `split_tree_test.odin`, `pane_test.odin`, `workspace_test.odin` |
| **Major refactor** | `build_dashboard.odin`, `build_cell.odin`, `build_compare.odin`, `build_focus.odin`, `app.odin`, `components.odin` |
| **Delete (Phase 4)** | `grid_resize.odin`, portions of `grid.odin` (compute_grid, builders), `Compare_State` from `components.odin` |
| **Unchanged** | `layer_api.odin`, `layer_strategies.odin`, `market_store.odin`, `data_source.odin`, `stream_apply_state.odin`, `artifact_policy.odin`, `protocol_engine.odin` |

## Consequences

- 4-phase migration over ~9 stages (S105–S113).
- Zero behavioral change until Phase 4 legacy removal.
- Layers runtime (`mr:layers`) completely untouched — widget host composes on top.
- Proto-first data flow (`WS → MD_Event → Market_Store`) completely untouched.
- Grid structural dependency fully eliminated by S113.
- Workspace becomes the natural unit of persistence, cloning, and sharing.

## Alternatives

1. **Big-bang rewrite.** Rejected: high risk of regression in a 131K+ LOC codebase with 1600+ tests. Incremental migration with dual-path is safer.
2. **Grid-only improvements (spans, nesting).** Rejected: does not solve mode explosion (focus/compare/grid) or state entanglement. A tree model is fundamentally more expressive.
3. **Defer to post-1.0.** Rejected: grid coupling is already causing friction in S100+ stages. Earlier migration reduces compound technical debt.

## Evidence

- Files to delete: `grid_resize.odin` (120 lines), grid builders in `grid.odin` (~200 lines), `Compare_State` (~80 lines).
- Files to refactor: `build_dashboard.odin` (~300 lines), `build_compare.odin` (~250 lines), `build_focus.odin` (~100 lines), `components.odin` (~200 lines of `Entity_World`).
- Preserved unchanged: `layer_api.odin` (216 lines), `market_store.odin`, `data_source.odin`, `stream_apply_state.odin` — the entire `mr:layers` and proto pipeline.
- Test baseline: 598 client tests (402 md_common + 171 services + 25 app).

## Changelog

- 2026-03-08: Initial acceptance.
