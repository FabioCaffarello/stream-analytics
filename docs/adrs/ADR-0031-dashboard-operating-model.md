# ADR-0031: Dashboard Operating Model

**Status:** Accepted
**Date:** 2026-03-09
**Stage:** S117 — Dashboard Operating Model
**Supersedes:** Extends ADR-0024 (Workspace), ADR-0026 (Pane), ADR-0027 (Widget Host), ADR-0028 (Data Context), ADR-0030 (Pane Data Context)

## Context

The dashboard has evolved through 100+ stages, accumulating three concurrent state management approaches:

1. **Entity_World** (legacy ECS) — parallel arrays of components per cell
2. **Pane Pool** (S105+) — per-pane structs with widget host contracts
3. **Sync Protocols** (S112) — bidirectional bridges between the two

This creates implicit coupling: global timeframe affects all follow-active cells, focus tracking maintains dual indices (cell_idx + Pane_ID), compare mode shadows global state with per-pane copies, and context stack reads Entity_World directly.

Without an explicit operating model, adding new features (multi-workspace, workspace tabs, pane-level subscriptions) risks subtle invariant violations.

## Decision

We adopt a **three-tier context hierarchy** with explicit ownership, inheritance, and resolution contracts.

### Tier 1: Global Context (`Global_Context`)

**Owned by:** `App_State`
**Scope:** App-wide singleton state
**Read-only for:** Workspaces, Panes, Widgets

| State | Owner | Readers |
|-------|-------|---------|
| Connection status | `conn.last_conn` | Top bar, status bar, context stack |
| Route | `chrome.active_route` | Shell, page dispatch |
| Zen mode | `zen.active` | Shell chrome visibility |
| Focus mode | `focus_mode` | Workspace rendering path |
| Compare mode | `compare.active` | Workspace rendering path |
| Global TF | `active_tf_idx` | Fallback for panes that inherit |
| Settings | `settings` | All tiers (read-only) |

**Rule:** No workspace or pane may write to Tier 1 state. Mutations flow through `UI_Action` queue only.

### Tier 2: Workspace Context (`Resolved_Workspace_Context`)

**Owned by:** `Workspace`
**Scope:** Per-workspace
**Inherits from:** Global Context (for TF fallback)

| State | Owner | Readers |
|-------|-------|---------|
| Active stream | `data_ctx.active_stream_idx` | Follow-active panes |
| Default TF | `data_ctx.default_tf_idx` | Panes without tf_override |
| Layout tree | `tree` | Pane rect resolution |
| Focus pane | `focus.active` (Pane_ID) | Pane focus determination |
| Mode | `mode` (Normal/Zen) | Chrome visibility |

**Rule:** Workspace reads Global Context for defaults but never writes to it. Panes read Workspace Context for inheritance.

### Tier 3: Pane Context (`Resolved_Pane_Context`)

**Owned by:** `Pane`
**Scope:** Per-pane
**Inherits from:** Workspace Context (for stream + TF defaults)

| State | Owner | Readers |
|-------|-------|---------|
| Stream binding | `pane.binding` | Store resolution, subscription reconciliation |
| TF override | `pane.tf_override` | Effective TF resolution |
| Widget kind | `pane.widget.kind` | Contract dispatch |
| Indicators | `pane.indicators` + `pane.ind_params` | Layer rendering |
| Chart config | `pane.chart` | Layer rendering |
| Analytics | `pane.analytics` | Analytics widget dispatch |
| View state | `pane.view` | Scroll, zoom, crosshair |

**Rule:** Widgets receive `Widget_Data_Context` (resolved from `Resolved_Pane_Context` + stores). Widgets NEVER read `App_State`, `Workspace`, or `Pane` directly.

### Resolution Cascade

```
Timeframe:    pane.tf_override → workspace.default_tf_idx → global.active_tf_idx
Stream:       pane.binding     → workspace.active_stream  (follow-active fallback)
Focus:        workspace.focus.active == pane.id
Indicators:   pane.indicators  (no inheritance — pane owns entirely)
Chart:        pane.chart       (no inheritance — pane owns entirely)
```

### Invariants

1. `follows_active ∧ stream_bound` is always false (mutually exclusive)
2. `effective_tf_idx ∈ [0, len(TF_OPTIONS))` always holds
3. `Pane_ID ≠ PANE_ID_NONE` for any resolved context
4. Widgets receive immutable `Widget_Data_Context` — no back-references to mutable state

## Consequences

### Positive
- **Explicit ownership**: Every piece of state has exactly one owning tier
- **Auditable inheritance**: TF/stream resolution is a pure function with documented cascade
- **Widget isolation**: Widgets receive resolved data, never raw state — safe for future async rendering
- **Multi-workspace ready**: Workspace Context is per-workspace, not global

### Negative
- **Entity_World still exists**: Legacy sync protocols (S112) remain until full cutover
- **Resolution cost**: Three-tier resolution adds ~20 lines per frame per pane — negligible at 16 panes
- **Compare mode exception**: Compare mode bypasses the standard pane model (uses parallel arrays), not yet integrated into this contract

### Migration Path
1. **S117 (this stage)**: Define contracts, tests, ADR. No runtime changes.
2. **Future**: Replace `resolve_widget_data_context` with `resolve_pane_context` + store resolution.
3. **Future**: Migrate compare mode to use workspace pane pool instead of parallel arrays.
4. **Future**: Remove Entity_World entirely once all paths use pane contracts.

## Implementation

- `operating_model.odin`: Type definitions + resolution procs
- `operating_model_test.odin`: 20 tests covering ownership, resolution, invariants
- `mr:layers` remains the sole visual runtime — operating model is pure state contracts
