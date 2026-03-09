# Stage 117 — Dashboard Operating Model

**Date:** 2026-03-09
**Status:** Complete
**ADR:** ADR-0031

## Objective

Formalize the dashboard as an explicit operating model with clear ownership tiers, separating Global Context, Workspace Context, and Pane Context. Eliminate implicit coupling between shell, panes, and widgets.

## Deliverables

### 1. Three-Tier Context Hierarchy (`operating_model.odin`)

| Type | Tier | Scope |
|------|------|-------|
| `Global_Context` | 1 | Connection, route, modes, global TF |
| `Resolved_Workspace_Context` | 2 | Active stream, default TF, layout, focus |
| `Resolved_Pane_Context` | 3 | Binding, TF override, widget, indicators |

Resolution procs:
- `resolve_global_context(state)` — pure read from App_State
- `resolve_workspace_context(ws, gctx)` — inherits from Global_Context
- `resolve_pane_context(pane, wctx, gctx)` — inherits from Workspace + Global

### 2. Ownership Classification

| Category | Tier |
|----------|------|
| Connection, Route, Modes, Global TF, Settings | Global |
| Active Stream, Default TF, Layout Tree, Focus, Workspace Mode | Workspace |
| Stream Binding, TF Override, Widget Kind, Indicators, Chart, Analytics, View | Pane |

`ownership_of(State_Category) → Ownership_Tier` provides compile-time auditable ownership.

### 3. TF Resolution Cascade

```
pane.tf_override (>= 0)  →  Pane_Override
workspace.default_tf_idx (>= 0)  →  Workspace
global.active_tf_idx  →  Global
```

`TF_Source` enum tracks which tier provided the resolved TF.

### 4. Stream Binding Resolution

```
pane.binding has venue+symbol  →  explicit binding (stream_bound=true)
pane.binding.stream_idx >= 0   →  explicit stream slot
else  →  follow workspace active stream (follows_active=true)
```

### 5. Invariant Validators

- `pane_context_valid()` — validates Pane_ID, TF range, follows_active/stream_bound consistency
- `workspace_context_valid()` — validates Workspace_ID, default TF range

### 6. Tests

20 tests covering:
- Ownership classification (7 global, 5 workspace, 7 pane categories)
- Global context resolution (nil safety, connected state)
- Workspace context resolution (TF inheritance, own TF)
- Pane TF cascade (workspace inherit, pane override, global fallthrough)
- Pane stream binding (follow-active, explicit binding)
- Pane focus resolution
- Invariant validation (valid, no ID, bad TF, contradictory flags)
- Nil safety for all resolution procs

## Files

| File | Status |
|------|--------|
| `client/src/core/app/operating_model.odin` | New |
| `client/src/core/app/operating_model_test.odin` | New |
| `docs/adrs/ADR-0031-dashboard-operating-model.md` | New |
| `docs/stages/stage-117-dashboard-operating-model-report.md` | New |

## Architectural Decisions

1. **Pure contracts, no runtime changes**: Operating model is additive — existing `resolve_widget_data_context` continues to work. Migration to use `resolve_pane_context` as the foundation can happen incrementally.

2. **mr:layers preserved**: The visual runtime is untouched. Operating model handles state contracts only.

3. **Entity_World not removed**: Legacy sync (S112) remains until all paths migrate. The operating model documents the target state, not the current bridge.

4. **Compare mode exception documented**: Compare mode's parallel arrays are not yet integrated into the pane context model — flagged for future migration.

## Invariants

- No pane writes to Global_Context or Workspace_Context
- Widgets only receive resolved Widget_Data_Context (immutable)
- `follows_active ∧ stream_bound` is always false
- `effective_tf_idx ∈ [0, 9)` always holds
- Every resolved Pane_Context has a valid Pane_ID

Zero regressions. Zero wire-breaking changes.
