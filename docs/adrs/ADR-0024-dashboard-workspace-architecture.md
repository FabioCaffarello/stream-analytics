# ADR-0024 — Dashboard Workspace Architecture

**Status:** Accepted
**Date:** 2026-03-08
**Owners:** Core maintainers
**Supersedes:** —
**Related:** ADR-0025, ADR-0026, ADR-0027, ADR-0028, ADR-0029

## Context

The current dashboard is an `App_State` god-struct that mixes layout (grid weights, preset index, resize state), mode flags (focus, compare, zen), ECS components (`Entity_World` with 12 parallel arrays), data routing (`Stream_View_Registry`, `Market_Store`), chrome state (sidebar, overlays, routes), and page sub-states into one flat namespace. This works today because the dashboard is the only workspace, but it creates several problems:

1. **No multi-workspace support.** Users cannot save/load/switch between different dashboard configurations (different instruments, layouts, indicator sets) without manually reconfiguring everything.
2. **State entanglement.** Compare mode maintains its own parallel state (`Compare_State` with 23+ fields) that duplicates concepts already in `Entity_World` (bindings, TF overrides, analytics kind).
3. **Persistence fragility.** `WORKSPACE_SCHEMA_VERSION` bumps require full migration paths because layout, cells, indicators, and data context are serialized as one blob.
4. **Mode explosion.** Grid, Focus, and Compare are three rendering paths with significant divergence, yet they share the same state bag.

We need a first-class **Workspace** aggregate that owns everything below it and can be independently persisted, cloned, and swapped.

## Decision

### 1. Workspace as Aggregate Root

Introduce `Workspace` as the primary aggregate for the dashboard domain:

```
Workspace :: struct {
    id:          Workspace_ID,       // u32, monotonic
    label:       [48]u8,             // user-visible name
    label_len:   u8,
    schema_ver:  u16,                // migration gate

    // Layout tree (ADR-0025)
    tree:        Split_Tree,

    // Pane registry (ADR-0026)
    panes:       [PANE_MAX]Pane,     // 16 max
    pane_count:  u8,

    // Data context (ADR-0028)
    data_ctx:    Data_Context,

    // Workspace-level settings
    active_pane: Pane_ID,
    mode:        Workspace_Mode,     // Normal, Zen
}
```

### 2. Workspace Responsibilities

| Responsibility | Current Owner | Moves To |
|---|---|---|
| Layout tree (splits, weights) | `Grid_Def` + `custom_grid_def` + `layout_preset` | `Workspace.tree` |
| Cell registry + components | `Entity_World` (flat arrays) | `Workspace.panes[]` |
| Focus/compare orchestration | `focus_mode` bool + `Compare_State` | Workspace tree variants (single expanded pane, multi-pane compare) |
| Data context (active stream, TF) | `Stream_View_Registry` + global TF | `Workspace.data_ctx` |
| Persistence envelope | `Settings_Store` ad-hoc keys | `Workspace` serialized as unit |
| Mode (zen/normal) | `Zen_State` on `App_State` | `Workspace.mode` |

### 3. Workspace Lifecycle

```
Create  → allocate ID, default tree (2×4 grid equivalent), default panes
Clone   → deep-copy tree + panes + data_ctx, new ID
Switch  → deactivate current (freeze streams), activate target (resume streams)
Persist → serialize Workspace to codec envelope with schema_ver
Restore → deserialize + migrate if schema_ver < current
Destroy → release all pane resources, unsubscribe streams
```

### 4. Workspace Registry

```
Workspace_Registry :: struct {
    workspaces:   [WORKSPACE_MAX]Workspace,  // 8 max
    count:        u8,
    active_idx:   u8,
}
```

- `App_State` holds a `Workspace_Registry` instead of directly embedding layout/cell state.
- Active workspace is the sole source of truth for rendering.
- Inactive workspaces retain their serialized state but release stream subscriptions.

### 5. Invariants

- **Single active workspace.** Only one workspace renders per frame.
- **Pane IDs are workspace-local.** `Pane_ID` is only meaningful within its parent workspace.
- **Workspace owns its tree.** No external mutation of tree nodes — all changes go through workspace actions.
- **Schema versioning per workspace.** Each workspace carries its own `schema_ver` for independent migration.

## Consequences

- Focus and Compare modes become tree-shape variants, not separate code paths.
- `App_State` shrinks significantly — layout, cells, and data context move into `Workspace`.
- Multi-workspace becomes a natural extension (workspace tabs/switcher).
- Persistence becomes per-workspace with independent schema migration.
- Compare mode's 23-field `Compare_State` is eliminated — each compare pane is a regular `Pane` in the tree.

## Alternatives

1. **Keep flat App_State, add workspace_id tag.** Rejected: does not solve state entanglement or mode explosion.
2. **Full ECS with entity IDs.** Rejected: over-engineered for fixed-capacity panes in an immediate-mode UI. Parallel arrays within `Workspace.panes[]` achieve the same data-oriented layout with less indirection.
3. **Workspace as external config only.** Rejected: workspace must own runtime state (active pane, mode) to be a proper aggregate.

## Evidence

- Current `App_State` fields that move: `world`, `layout_preset`, `layout_mode`, `custom_grid_def`, `focus_mode`, `compare`, `grid_col_resize`, `grid_row_resize`, `stream_views`, `chrome.panel_visible`.
- `WORKSPACE_SCHEMA_VERSION` currently at 10 (`components.odin`), `RUNTIME_SNAPSHOT_VERSION` at 3.
- 12 widget kinds, 7 fixed panel indices, 4 layout presets — all migrate into tree + pane model.

## Changelog

- 2026-03-08: Initial acceptance.
