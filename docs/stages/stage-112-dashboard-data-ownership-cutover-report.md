# Stage 112 — Dashboard Data Ownership Cutover

**Date:** 2026-03-09
**Status:** Complete
**ADR:** ADR-0030 (Pane Data Context Ownership)

## Objective

Ensure the dashboard contract path (`resolve_widget_data_context`) uses
explicit pane-local data ownership. Eliminate implicit dependencies on
Entity_World parallel arrays for the workspace tree render path.

## Problem

The `Pane` struct declared ownership of `binding`, `tf_override`, `indicators`,
`chart`, `subplots`, and `analytics` — but these fields were **never written**
by actions. All data flowed through Entity_World parallel arrays
(`state.world.bindings[ci]`, `state.world.timeframes[ci]`, etc.).

The contract path partially read from pane but fell back to Entity_World for:
- Store resolution (`resolve_stores_for_cell` indexed by cell position)
- TF resolution (`cell_effective_tf_idx` read `state.world.timeframes[ci]`)
- Focus tracking (`state.world.focused` cell index)
- Stream index (`state.world.bindings[cell_idx].stream_idx`)

This created a fragile invariant: DFS pane order must perfectly align with
Entity_World cell indices. Any mismatch broke all bindings.

## Changes

### 1. Pane-Local Resolvers (New)

- **`resolve_stores_for_pane(state, binding, eff_tf_idx)`** — resolves data
  stores from pane's `Stream_Binding` and effective TF index directly. No
  Entity_World lookup. Same resolution logic as `resolve_stores_for_cell` but
  reads from passed binding pointer.
  (`stream_slots.odin`)

- **`pane_effective_tf_idx(pane, ws, global_tf_idx)`** — resolves effective TF
  from `pane.tf_override` → `ws.data_ctx.default_tf_idx` → `global_tf_idx`.
  No Entity_World read.
  (`stream_views.odin`)

### 2. Contract Path Updated

`resolve_widget_data_context` now reads exclusively from pane-local state:
- **TF:** `pane_effective_tf_idx(pane, ws, state.active_tf_idx)` (was `cell_effective_tf_idx`)
- **Stores:** `resolve_stores_for_pane(state, &pane.binding, ctx.tf_idx)` (was `resolve_stores_for_cell`)
- **Focus:** `ws.focus.active == pane.id` (was `cell_idx == state.world.focused`)
- **Stream index:** `pane.binding.stream_idx` (was `state.world.bindings[cell_idx].stream_idx`)
(`widget_contract.odin`)

### 3. Bidirectional Sync

Actions now write to both Entity_World AND pane:
- **`apply_set_cell_timeframe_action`** — syncs `tf_idx` → `pane.tf_override`, resets view
- **`apply_set_cell_stream_action`** — syncs binding → `pane.binding`
- **`apply_set_cell_widget_action`** — syncs widget kind + analytics → pane
- **`apply_add_cell_action`** — syncs binding + analytics on new cell
- **Indicator toggle** — syncs `state.world.indicators[fci]` → `pane.indicators`
(`actions.odin`, `actions_cell_mutations.odin`, `stream_views.odin`)

### 4. Startup/Restore Sync

- **`workspace_sync_panes_from_world`** — copies all Entity_World component data
  (binding, TF, indicators, chart, subplots, analytics) into corresponding panes.
  Called after `workspace_sync_from_world` (tree rebuild).
- **`workspace_sync_pane_to_world`** — writes pane state back to Entity_World
  for legacy paths (persistence, compare mode, reconciliation).
(`build_workspace.odin`)

### 5. Focus Tracking

`build_workspace_dashboard` now maintains `ws.focus.active` (Pane_ID) alongside
`state.world.focused` (cell index). The workspace focus is the source of truth
for the contract path; cell index remains for legacy paths.
(`build_workspace.odin`)

### 6. Pane_Data_Context Struct

New `Pane_Data_Context` struct documents the ownership model:
venue, symbol, stream_idx, stream_bound, tf_idx, analytics_kind, compare_group.
(`workspace.odin`)

## Files Modified

| File | Change |
|------|--------|
| `widget_contract.odin` | Contract path reads from pane exclusively |
| `stream_slots.odin` | New `resolve_stores_for_pane` |
| `stream_views.odin` | New `pane_effective_tf_idx`, TF action syncs to pane |
| `build_workspace.odin` | Sync helpers, focus tracking |
| `actions_cell_mutations.odin` | Stream/widget/add actions sync to pane |
| `actions.odin` | Indicator toggle syncs to pane |
| `workspace.odin` | `Pane_Data_Context` struct |
| `widget_contract_test.odin` | 8 new S112 tests |

## Files Created

| File | Purpose |
|------|---------|
| `docs/adrs/ADR-0030-pane-data-context-ownership.md` | Architecture decision |

## Tests

8 new tests added (186 total in app package, all passing):

| Test | Coverage |
|------|----------|
| `test_pane_effective_tf_idx_override` | Per-pane TF takes precedence |
| `test_pane_effective_tf_idx_inherit_workspace` | Fallback to workspace default |
| `test_pane_effective_tf_idx_fallback_global` | Fallback to global TF |
| `test_sync_panes_from_world_copies_binding` | Entity_World → pane sync |
| `test_sync_pane_to_world_roundtrip` | Pane → Entity_World roundtrip |
| `test_pane_data_context_struct` | Ownership struct validity |
| `test_pane_focus_by_id` | Focus by Pane_ID |
| `test_pane_independent_bindings` | Pane binding independence |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| TF switching consistent per-pane | PASS — `pane_effective_tf_idx` reads from pane.tf_override |
| Panes independent (no shared state) | PASS — each pane owns binding, TF, indicators |
| Widgets without external structural dependency | PASS — `Widget_Data_Context` resolved from pane only |
| Data flow: WS → stream → store → pane context → widget | PASS — `resolve_stores_for_pane` closes the loop |

## Data Flow (Post-S112)

```
WebSocket
  → Stream Registry (slots)
    → Layer Store (Market_Stream per subject)
      → resolve_stores_for_pane(binding, eff_tf)
        → Widget_Data_Context (immutable)
          → Widget Contract (render/update/input)
```

Pane owns: binding, tf_override, indicators, chart, subplots, analytics.
Widget receives: pre-resolved Widget_Data_Context (no mutable refs, no globals).

## Zero Regressions

- 186 tests pass (178 existing + 8 new)
- Native + WASM build clean
- Entity_World parallel arrays maintained for legacy (persistence, compare, reconcile)
- No wire-breaking changes
