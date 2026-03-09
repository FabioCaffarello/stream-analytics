# Stage 108 — Widget Host & Data Context Contracts

**Date:** 2026-03-09
**Status:** COMPLETE
**Scope:** Standardize widget–pane integration via formal lifecycle and data context contracts

## Objective

Eliminate direct global store access from widgets. All data enters via a resolved
`Widget_Data_Context`. Define a standard widget contract vtable with 7 lifecycle
hooks. Make widgets pluggable into the pane system with zero structural dependency
on the legacy grid.

## Deliverables

### 1. Widget_Data_Context (ADR-0028 formalized)

Bundles all data a widget needs for a single frame:
- **Market identity**: venue, symbol, stream_idx, stream_bound
- **Timeframe**: tf_idx, tf_string, tf_ms (resolved per-pane or workspace default)
- **Compare group**: compare_group (-1 = not in compare mode)
- **Analytics**: analytics_kind, show_history
- **Data stores**: Cell_Stores (pre-resolved pointers to candle, trades, OB, etc.)
- **Composition**: Cell_Surface_View (health, staleness, composition stage)
- **Chart/indicators**: Chart_Component, Indicator_Component, Indicator_Params
- **Identity**: pane_id, cell_idx, focused, rect

### 2. Widget_Contract (ADR-0027 formalized)

Vtable with 7 lifecycle procs:

| Proc | When | Purpose |
|------|------|---------|
| `on_create` | Pane allocated | Initialize widget-specific defaults |
| `on_bind_context` | Data binding resolves | Prepare for rendering with new context |
| `on_update` | Each frame (Active) | Update internal state |
| `on_render` | Each frame (Active) | Render content into pane rect |
| `on_handle_input` | Input in pane rect | Process user interaction |
| `on_serialize` | Persistence save | Capture view state for workspace schema |
| `on_dispose` | Pane deallocated | Release resources |

### 3. WIDGET_CONTRACTS Table

Compile-time `[Widget_Kind]Widget_Contract` table — compiler enforces exhaustive coverage.
Default implementations delegate to existing render pipelines:
- **Candle** → `render_cell_layer_canvas`
- **Generic** (Stats, Counter, Heatmap, VPVR, Trades, OB, DOM) → `render_cell_layer_canvas`
- **Analytics** → `render_cell_layer_canvas_analytics`
- **Session profiles** (SVPVR, TPO) → `render_session_profile_cell_vm`
- **Empty** → no-op

### 4. Lifecycle State Machine

Valid transitions enforced by `widget_lifecycle_valid`:

```
Created → Bound → Active ⇄ Suspended
    ↓        ↓       ↓          ↓
         Disposing (terminal)
```

- `Created → Active` blocked (must bind context first)
- `Active → Bound` allowed (rebind to different stream)
- `Disposing` is terminal — no transitions out

### 5. Resolution Helpers

- `resolve_widget_data_context(state, pane, cell_idx, rect)` — builds context from
  pane-level state (not Entity_World), using existing `resolve_stores_for_cell` and
  `resolve_cell_surface_view_with_stores` for data resolution
- `widget_data_context_from_vm(vm, pane_id, rect)` — migration bridge, builds context
  from existing `Cell_View_Model` during dual-path coexistence

### 6. Dispatch Helpers

Type-safe contract dispatch that handles nil procs:
- `widget_contract_create`, `widget_contract_bind`, `widget_contract_update`
- `widget_contract_render`, `widget_contract_handle_input`
- `widget_contract_serialize`, `widget_contract_dispose`

## Files

| File | Action | Description |
|------|--------|-------------|
| `client/src/core/app/widget_contract.odin` | **NEW** | Widget_Data_Context, Widget_Contract, WIDGET_CONTRACTS, lifecycle SM, dispatch |
| `client/src/core/app/widget_contract_test.odin` | **NEW** | 30 tests covering lifecycle, contracts, context, dispatch |
| `client/src/core/app/stream_slots.odin` | **MOD** | `resolve_cell_surface_view_with_stores` widened to package-private |

## Tests

30 new tests in widget_contract_test.odin:
- **Lifecycle SM** (13): valid/invalid transitions, full sequence, nil host, terminal state
- **Contract table** (5): exhaustiveness, all procs present, Empty widget nil procs
- **Data context** (2): from_vm conversion, default compare_group
- **Dispatch** (6): create, serialize, input, dispose — including nil safety
- **Alignment** (2): contract_for, descriptor↔contract consistency

**Total app package tests: 158 (all passing)**

## Design Decisions

1. **Vtable, not interface**: Odin doesn't have interfaces. Proc pointer table in a
   rodata array gives the same dispatch semantics with zero overhead.

2. **Default implementations delegate**: Rather than rewriting render logic, default
   contracts call the existing `render_cell_layer_canvas` pipeline. This preserves
   all current behavior while establishing the contract boundary.

3. **Pane-level data context**: `resolve_widget_data_context` reads from `Pane` struct
   (analytics, chart, indicators) rather than Entity_World. This is the migration path
   away from the legacy ECS parallel arrays.

4. **Widget_Serialized_State**: Captures all view state needed for workspace persistence.
   Symmetric with the Pane struct fields — serialize captures, deserialize restores.

5. **No grid dependency**: Widget_Data_Context has no reference to grid indices, column
   spans, or legacy layout. Pane_ID + cell_idx are the only identity markers.

## Acceptance Criteria

- [x] Widgets pluggable into pane system via Widget_Contract vtable
- [x] No structural dependency on legacy grid (Widget_Data_Context is grid-free)
- [x] All data enters widgets via Widget_Data_Context (stores, TF, analytics, identity)
- [x] Standard contract: create, bind_context, update, render, handle_input, serialize, dispose
- [x] Lifecycle state machine with validated transitions
- [x] Compile-time exhaustive contract table (all 12 Widget_Kinds covered)
- [x] 158 app tests passing, zero regressions
