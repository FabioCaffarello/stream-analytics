# ADR-0028 — Data Context Ownership

**Status:** Accepted
**Date:** 2026-03-08
**Owners:** Core maintainers
**Supersedes:** —
**Related:** ADR-0024, ADR-0026, ADR-0027

## Context

Data context — which stream, venue, instrument, timeframe, and analytics kind a widget renders — is currently scattered across multiple owners:

| Data | Current Owner | Location |
|---|---|---|
| Active stream | `Stream_View_Registry.active_subject_id` | `app.odin` |
| Per-cell stream binding | `Entity_World.bindings[cell_idx]` | `components.odin` |
| Global timeframe | `App_State.tf_idx` | `app.odin` |
| Per-cell TF override | `Entity_World.timeframes[cell_idx]` | `components.odin` |
| Compare-pane TF | `Compare_State.tf_idx[pane]` | `components.odin` |
| Analytics kind | `Entity_World.analytics[cell_idx]` | `components.odin` |
| Compare analytics | `Compare_State.analytics_kind[pane]` | `components.odin` |
| Market data | `Market_Store` (per-market) | `layers/market_store.odin` |
| Apply state | `Stream_View_Slot.apply_state` | `stream_apply_state.odin` |

Resolution is done per-frame via multiple procs: `cell_effective_tf_idx()`, `cell_effective_tf_string()`, `cell_effective_tf_ms()`, `compare_pane_effective_tf_idx()`. The TF inheritance chain (pane → workspace → global) is implicit in conditionals rather than declared.

This creates three problems:
1. **No single resolution path.** Dashboard cells and compare panes use different resolution logic for the same concept.
2. **Ownership ambiguity.** `Market_Store` is owned by `App_State` but mutated by `data_source_poll_and_apply()` — which runs outside workspace scope.
3. **Timeframe inheritance is ad-hoc.** The chain `per-cell → global` has no intermediate "workspace-level" concept.

## Decision

### 1. Data Context Hierarchy

Introduce a three-tier context model with explicit inheritance:

```
Global_Data_Context          (app-level: connection, session)
  └─ Workspace_Data_Context  (workspace-level: active stream, default TF, analytics defaults)
       └─ Pane_Data_Context  (pane-level: binding override, TF override, analytics override)
```

### 2. Workspace Data Context

```
Workspace_Data_Context :: struct {
    // Active stream (workspace-scoped, not global)
    active_subject_id: u64,
    active_venue:      [24]u8,
    active_venue_len:  u8,
    active_symbol:     [32]u8,
    active_symbol_len: u8,

    // Default timeframe (workspace-scoped)
    tf_idx:            i8,       // index into TF_OPTIONS

    // Default analytics kind
    analytics_kind:    Analytics_Kind,

    // Stream view slots (workspace-scoped)
    stream_slots:      Stream_View_Registry,
}
```

Moving active stream and default TF from `App_State` to `Workspace_Data_Context` enables multi-workspace with independent instrument/TF focus.

### 3. Pane Data Context (Overrides)

```
Pane_Data_Context :: struct {
    // Stream binding (-1 = inherit workspace active)
    binding:        Stream_Binding,

    // Timeframe override (-1 = inherit workspace default)
    tf_override:    i8,

    // Analytics kind override (None = inherit workspace default)
    analytics_kind: Analytics_Kind,
}
```

Stored on each `Pane` (ADR-0026). Resolution is always:
```
effective_value = pane_override != INHERIT ? pane_override : workspace_default
```

### 4. Unified Resolution

Single resolution procedure replaces the current scattered conditionals:

```
Resolved_Data_Context :: struct {
    subject_id:     u64,
    venue:          string,       // slice into stable storage
    symbol:         string,
    tf_idx:         i8,
    tf_ms:          i64,
    tf_label:       string,
    analytics_kind: Analytics_Kind,
    stream:         ^Market_Stream,   // resolved from Market_Store
    apply_state:    ^Stream_Apply_State,
    capabilities:   Layer_Capabilities,
}

resolve_pane_data_context :: proc(
    ws_ctx:   ^Workspace_Data_Context,
    pane_ctx: ^Pane_Data_Context,
    store:    ^Market_Store,
) -> Resolved_Data_Context
```

- Called once per pane per frame.
- Result is passed to `Widget_Render_Proc` via `Layer_Context`.
- Compare panes use the same resolution path — no special case.

### 5. Market Store Ownership

`Market_Store` remains **app-level** (not per-workspace) because:
- Market data is venue+symbol scoped, not workspace-scoped.
- Multiple workspaces viewing the same instrument share the same `Market_Stream`.
- WS connection and data ingestion are global (one connection, N workspaces).

Ownership chain:
```
App_State
  ├─ Market_Store          (global, shared across workspaces)
  ├─ Stream_Registry       (global, WS subscription management)
  └─ Workspace_Registry
       └─ Workspace
            └─ Data_Context.stream_slots  (workspace-local slot allocation)
                 └─ references Market_Store entries by subject_id
```

### 6. Stream Lifecycle Coordination

When a workspace activates:
1. Walk all panes → collect required `subject_id` set.
2. Ensure `Stream_Registry` has active subscriptions for each.
3. Populate `stream_slots` with slot allocations referencing `Market_Store`.

When a workspace deactivates:
1. Release `stream_slots` (but do NOT unsubscribe from `Stream_Registry` if other workspaces reference the same streams).
2. Reference counting on `Stream_Registry` entries determines actual unsubscription.

### 7. Analytics Context

Analytics data (CVD, DeltaVol, OI, BarStats) flows through the same `Market_Stream.analytics` store. The analytics kind selector determines which kind to render:

- **Workspace default:** `Workspace_Data_Context.analytics_kind` (e.g., OI).
- **Pane override:** `Pane_Data_Context.analytics_kind` (e.g., CVD for one specific pane).
- **Widget filter:** `Widget_Descriptor.supports_analytics` determines if the pane even uses analytics.

### 8. Invariants

- **Market_Store is never owned by a workspace.** It is global shared state.
- **Stream_View_Registry moves into workspace.** Each workspace maintains its own slot allocation.
- **Resolution is deterministic.** Same inputs → same `Resolved_Data_Context`. No hidden global state.
- **Pane overrides default to INHERIT.** Explicit override is opt-in.
- **TF change at workspace level propagates** to all panes that inherit (override=-1).

## Consequences

- Active stream becomes workspace-scoped — switching workspaces switches instrument focus.
- Compare mode panes use the same resolution path as dashboard panes — no duplication.
- TF inheritance is explicit (3-tier) instead of implicit (2-tier with special compare logic).
- `Market_Store` sharing across workspaces avoids redundant data ingestion.
- `cell_effective_tf_idx()`, `cell_effective_tf_string()`, `cell_effective_tf_ms()`, `compare_pane_effective_tf_idx()` are all replaced by `resolve_pane_data_context()`.

## Alternatives

1. **Per-workspace Market_Store.** Rejected: wastes memory and bandwidth duplicating identical market data across workspaces viewing the same instrument.
2. **Flat context (no inheritance).** Rejected: every pane would need explicit TF and binding, making bulk TF changes tedious.
3. **4-tier (global → workspace → group → pane).** Rejected: pane grouping adds complexity without clear user value. Can be added later if needed.

## Evidence

- `stream_views.odin`: `cell_effective_tf_idx()` (line 262), `cell_effective_tf_string()` (line 271), `cell_effective_tf_ms()` (line 281), `compare_pane_effective_tf_idx()` (line 291) — all replaced.
- `data_source.odin`: `data_source_poll_and_apply()` writes to `Market_Store` — remains global.
- `components.odin`: `Stream_Binding` + `Timeframe_Component` + `Analytics_Component` — consolidated into `Pane_Data_Context`.
- `app.odin`: `Stream_View_Registry` (heap-allocated, 32 slots) — moves into `Workspace_Data_Context`.

## Changelog

- 2026-03-08: Initial acceptance.
