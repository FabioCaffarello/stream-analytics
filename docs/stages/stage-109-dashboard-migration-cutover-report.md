# Stage 109 — Dashboard Migration (Cutover) Report

**Date:** 2026-03-08
**Status:** COMPLETE

## Objective

Migrate the dashboard from grid-based layout to Workspace + Split Tree Runtime, routing all widget rendering through the Widget Host Contract.

## Changes

### build_workspace.odin — Grid Fallback Removed

- **Before:** `build_workspace_dashboard` fell back to `build_dashboard_grid()` when no workspace existed
- **After:** Auto-initializes workspace via `workspace_registry_alloc` + `workspace_sync_from_world` — grid path is never reached from the dashboard
- Focus detection now scans the pane pool (widget kind from `pane.widget.kind`) instead of Entity_World
- Crosshair sync still reads from Entity_World views (chart interaction writes there — bridge preserved)
- Render loop calls `render_pane_via_contract` instead of `render_cell_widget`

### build_cell.odin — Contract-Based Pane Renderer

New proc `render_pane_via_contract`:
- Resolves `Widget_Data_Context` from pane state via `resolve_widget_data_context`
- Renders cell header (border, accent, stream badge, composition, health, close, switcher, TF, analytics badges)
- **TF badge** reads from `pane.tf_override` (not Entity_World timeframes)
- **Analytics kind/history** reads from `pane.analytics` (not Entity_World analytics)
- **Close button** uses `pane_count` (not `world.count`)
- **Body** dispatched through `widget_contract_render(state, pane, ctx, body_rect)`
- Pane state overlay rendered via existing `resolve_pane_visual_state` / `draw_pane_state_overlay`
- Legacy `render_cell_widget` retained for compare/focus mode paths

### Bridge: Entity_World Still Provides Data Stores

The DFS traversal order of panes = Entity_World cell index mapping is preserved. Data stores (candles, trades, orderbook, apply_state, etc.) are still accessed via `cell_idx` through `resolve_stores_for_cell` and `resolve_cell_surface_view_with_stores`. This bridge will be removed when data stores move to Workspace_Data_Context.

### Widget_Contract Dispatch Path

All 12 widget kinds now render through `WIDGET_CONTRACTS[kind].on_render`:
- Candle → `render_candle_contract` → `render_cell_layer_canvas`
- Analytics → `render_analytics_contract` → `render_cell_layer_canvas_analytics`
- Session_VPVR, TPO → `render_profile_contract` → `render_session_profile_cell_vm`
- Stats, Counter, Heatmap, VPVR, Trades, Orderbook, DOM → `render_generic_contract` → `render_cell_layer_canvas`
- Empty → `render_empty_contract` (no-op)

## Files Modified

| File | Change |
|------|--------|
| `build_workspace.odin` | Remove grid fallback, pane-pool focus detection, contract render dispatch |
| `build_cell.odin` | Add `render_pane_via_contract` proc (S109 contract path) |
| `workspace_test.odin` | 5 new tests: auto-init, pane lookup, focus detection, TF independence, analytics independence |
| `widget_contract_test.odin` | 6 new tests: render coverage, lifecycle round-trip, analytics isolation, TF override |

## Tests

- **168 tests** — all pass (was ~157 pre-S109)
- 11 new S109 tests covering:
  - Workspace auto-initialization on nil
  - Pane pool widget kind lookup via DFS
  - Focused candle detection from pane pool
  - Per-pane TF override independence
  - Per-pane analytics config independence
  - Contract render coverage (all 12 kinds)
  - Contract lifecycle round-trip (create → bind → serialize)
  - All-kinds lifecycle (create → bind → dispose)
  - Analytics write isolation (pane, not Entity_World)
  - TF override from pane

## Criteria Verification

| Criterion | Status |
|-----------|--------|
| Dashboard functional on new runtime | PASS — workspace tree is the sole render path |
| Grid not used on main path | PASS — `build_dashboard_grid` no longer called from dashboard |
| No widget dependent on old layout | PASS — all 12 kinds route through Widget_Contract |
| Panes host widgets via Widget Host Contract | PASS — `render_pane_via_contract` dispatches through contract table |
| Data Context connected to widgets | PASS — `resolve_widget_data_context` produces `Widget_Data_Context` per pane |
| Layout compatibility | PASS — `workspace_sync_from_world` rebuilds tree from existing panel state |

## Architecture Notes

- `build_dashboard_grid` and `render_cell_widget` are **not deleted** — they are retained for compare mode and focus mode, which still use legacy paths. A future stage will migrate those to the workspace tree.
- The Entity_World → pane pool data bridge is intentional. Moving data stores to Workspace_Data_Context is a separate concern (data ownership migration).
- Zero wire-breaking changes, zero regressions.
