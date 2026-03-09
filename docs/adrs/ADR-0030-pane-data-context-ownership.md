# ADR-0030 — Pane Data Context Ownership

**Status:** Accepted
**Date:** 2026-03-08
**Owners:** Core maintainers
**Supersedes:** —
**Related:** ADR-0024, ADR-0026, ADR-0027, ADR-0028

## Context

The dashboard uses a split ownership model where the `Pane` struct declares binding, `tf_override`, indicators, chart settings, and subplot flags, but the actual data flows through `Entity_World` parallel arrays (`state.world.bindings[ci]`, `state.world.timeframes[ci]`). The contract path (`resolve_widget_data_context` in `widget_contract.odin`) partially reads from the pane but falls back to `Entity_World` for store resolution, TF resolution, and focus tracking. This creates implicit coupling where DFS pane order must align with `Entity_World` cell indices.

| Data Concept | Declared In | Actually Resolved From |
|---|---|---|
| Stream binding | `Pane.binding` | `Entity_World.bindings[cell_idx]` |
| Timeframe override | `Pane.tf_override` | `Entity_World.timeframes[cell_idx]` |
| Indicators / chart | `Pane.indicators`, `Pane.chart` | Pane (direct) |
| Subplot flags | `Pane.subplots` | Pane (direct) |
| Analytics kind | `Pane.analytics_kind` | `Entity_World.analytics[cell_idx]` |
| Focus tracking | Workspace `active_pane` | `cell_idx` comparison |
| Store lookup | — | `Market_Store` via `Entity_World` binding |

This split creates three problems:

1. **Index coupling.** DFS traversal of the workspace tree must produce cell indices that align 1:1 with `Entity_World` parallel arrays. If the tree mutates (split, swap, remove), the index mapping can silently drift.
2. **Dual-write hazard.** Actions like TF switching must write to both `Pane.tf_override` and `Entity_World.timeframes[ci]`, and if either write is missed the pane renders with stale state.
3. **Testing friction.** Unit-testing `resolve_widget_data_context` requires constructing both a `Pane` and a full `Entity_World` with correctly aligned indices, even though the pane already holds all necessary data.

## Decision

### 1. Pane Is the Sole Owner of Its Data Context

`Pane` is the authoritative source for all per-cell data context fields:

```
Pane :: struct {
    // ... existing fields ...
    binding:        Stream_Binding,     // sole owner
    tf_override:    i8,                 // sole owner (-1 = inherit workspace)
    indicators:     Indicator_Flags,    // sole owner
    chart:          Chart_Settings,     // sole owner
    subplots:       Subplot_Flags,      // sole owner
    analytics_kind: Analytics_Kind,     // sole owner
}
```

No `Entity_World` read is permitted in the contract resolution path.

### 2. Bidirectional Sync During Migration

Actions that mutate cell state must sync to the corresponding pane:

```
Action writes Pane field  -->  sync to Entity_World (legacy consumers)
Action writes Entity_World -->  sync to Pane (during migration only)
```

Once all legacy consumers migrate to pane-direct reads, the `Entity_World` sync is removed.

### 3. resolve_widget_data_context Reads Exclusively From Pane

The `resolve_widget_data_context` proc (`widget_contract.odin:174`) must source all data context from the `Pane` struct. No `state.world.*[cell_idx]` reads are permitted in this path.

| Field | Source | Fallback |
|---|---|---|
| Binding | `pane.binding` | Workspace `data_ctx.active_subject_id` |
| Timeframe | `pane.tf_override` | Workspace `data_ctx.default_tf_idx` |
| Analytics kind | `pane.analytics_kind` | Workspace `data_ctx.analytics_kind` |
| Indicators | `pane.indicators` | None (pane-local only) |
| Chart settings | `pane.chart` | None (pane-local only) |
| Subplot flags | `pane.subplots` | None (pane-local only) |

### 4. New Pane-Direct Resolution Procs

Two new procs replace scattered `Entity_World` lookups:

```
resolve_stores_for_pane :: proc(
    pane:   ^Pane,
    store:  ^Market_Store,
    ws_ctx: ^Workspace_Data_Context,
) -> (stream: ^Market_Stream, apply_state: ^Stream_Apply_State)
```

Accepts binding and TF from `pane` directly. No cell index parameter.

```
pane_effective_tf_idx :: proc(
    pane:   ^Pane,
    ws_ctx: ^Workspace_Data_Context,
) -> i8
```

Resolves TF from `pane.tf_override`; falls back to `ws_ctx.default_tf_idx` when override is -1.

### 5. Focus Tracked by Pane_ID

Focus is tracked by `Pane_ID` (on `Workspace.active_pane`) rather than cell index. This decouples focus from tree traversal order:

```
// Before (index-coupled):
is_focused := cell_idx == state.focused_cell_idx

// After (ID-stable):
is_focused := pane.id == workspace.active_pane
```

### 6. Entity_World Retained for Legacy Paths

`Entity_World` remains as parallel storage for legacy paths during migration:

| Legacy Path | Entity_World Usage | Migration Target |
|---|---|---|
| Persistence (save/load) | Reads `Entity_World` arrays | Serialize from `Pane` directly |
| Compare mode | Reads `Entity_World` bindings | Compare panes are regular `Pane` entries |
| Stream slot allocation | Uses cell index for slot mapping | Use `Pane_ID` for slot mapping |

`Entity_World` is not the source of truth for the contract path.

## Consequences

- Panes are independently testable without `Entity_World` setup --- construct a `Pane` and a `Workspace_Data_Context`, call `resolve_widget_data_context`, assert the result.
- TF switching is consistent per-pane: a single write to `pane.tf_override` is sufficient; no second write to `Entity_World.timeframes[ci]` required after migration.
- Store resolution is deterministic from pane-local state: `binding` + `tf_override` fully determine which `Market_Stream` and `Stream_Apply_State` to use.
- Legacy paths (persistence, compare mode) still sync via `Entity_World` during migration but are not on the critical render path.
- Tree mutations (split, swap, collapse) no longer risk index misalignment because the contract path does not depend on cell indices.

## Alternatives

1. **Remove Entity_World immediately.** Rejected: persistence and compare mode depend on index-aligned arrays. Incremental migration reduces risk.
2. **Add an index-stabilization layer.** Rejected: mapping table from `Pane_ID` to cell index adds indirection without solving the dual-write problem.
3. **Keep Entity_World as source of truth, remove Pane fields.** Rejected: panes are the natural aggregate boundary (ADR-0024, ADR-0026) and must own their configuration to support workspace clone/serialize.

## Evidence

- `widget_contract.odin:174`: `resolve_widget_data_context` --- current mixed-source resolution proc.
- `build_cell.odin`: DFS traversal maps tree leaves to `Entity_World` cell indices.
- `actions_cell_mutations.odin`: cell mutation actions write to `Entity_World` parallel arrays.
- `workspace.odin`: `Pane` struct with binding, tf_override, indicators, chart, subplots.
- `workspace_test.odin`: pane-level tests that validate workspace tree operations.
- `widget_contract_test.odin`: contract resolution tests.

## Changelog

- 2026-03-08: Initial acceptance.
