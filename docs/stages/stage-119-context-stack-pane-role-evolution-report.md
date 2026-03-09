# Stage 119 — Context Stack & Pane Role Evolution

**Date:** 2026-03-09
**Status:** COMPLETE
**Tests:** 270 (all pass), 16 new S119 tests

## Summary

Transforms the dashboard from a grid of fixed boxes into a role-aware operational workstation. Panes now have explicit operational roles, the context stack adapts its tab set based on the focused pane's role, and a new "Workstation" layout preset provides a streamlined 1-chart + context-stack experience.

## Changes

### 1. Pane_Role Enum (`workspace.odin`)
- **`Pane_Role`**: `Primary_Chart`, `Auxiliary`, `Context`
- **`infer_pane_role(Widget_Kind)`**: auto-assigns role from widget kind (Candle → Primary_Chart, all others → Auxiliary)
- **`role` field** added to `Pane` struct
- **`pane_pool_alloc`**: sets safe default role (`.Auxiliary`)
- **`workspace_alloc_pane`**: auto-infers role from widget kind

### 2. Operating Model Extension (`operating_model.odin`)
- **`Pane_Role_Cat`** added to `State_Category` enum, owned by `Pane` tier
- **`role: Pane_Role`** added to `Resolved_Pane_Context`
- **`resolve_pane_context`** now includes `pane.role` in resolved context

### 3. Context_Tab Extension (`components.odin`)
- **`Context_Tab`** extended with `DOM` and `Analytics` (7 tabs total)
- **`CONTEXT_TAB_COUNT`** updated from 5 → 7
- **`follow_focus: bool`** added to `Context_Stack_State`

### 4. Role-Aware Context Stack (`context_stack.odin`)
- **Role badge strip** above tab bar showing "CHART" / "AUX" / "CTX"
- **Tab filtering**: `context_tab_available_for_role()` — Primary_Chart gets all tabs, Auxiliary gets only Instrument
- **Auto-snap**: if active tab unavailable for current role, snaps to first available
- **`context_tab_next_available()`**: role-aware tab cycling
- **Extended tab labels/widgets**: Stats, Trades, OB, Ctr, Info, DOM, Ana
- **Instrument info tab** now shows pane role

### 5. Workstation Layout (`workspace_tree.odin`)
- **`build_workstation_workspace_tree()`**: H(Candle @ 0.70, V(Stats @ 0.5, Trades))
- 3-pane streamlined layout designed to work with expanded context stack

### 6. Workstation Preset (`actions_cell_mutations.odin`, `workspace_toolbar.odin`)
- **Preset "W"** (index 4) added to toolbar preset buttons
- **`apply_workstation_preset()`**: rebuilds Entity_World with 3 cells, builds workstation tree, auto-expands context stack
- Toolbar shows 5 presets: D, C, A, K, W

### 7. Cycle_Context_Tab Action (`app.odin`, `actions.odin`)
- **`Cycle_Context_Tab`** action kind: cycles to next available tab respecting focused pane's role
- Auto-expands context stack if collapsed

### 8. Focus Logic Update (`build_workspace.odin`)
- Focus validation now accepts `Pane_Role.Primary_Chart` OR `Widget_Kind.Candle`
- Focus scan prioritizes Primary_Chart role, falls back to Candle kind
- **`workspace_sync_panes_from_world`** now infers role during sync

### 9. Schema Version
- **`WORKSPACE_SCHEMA_VERSION`** bumped from 10 → 11

## Files Modified
| File | Change |
|------|--------|
| `workspace.odin` | Pane_Role enum, infer_pane_role, role field on Pane, alloc updates |
| `operating_model.odin` | State_Category.Pane_Role_Cat, Resolved_Pane_Context.role |
| `components.odin` | Context_Tab extended (DOM, Analytics), CONTEXT_TAB_COUNT=7, follow_focus |
| `context_stack.odin` | Role-aware rendering, tab filtering, role badge, extended tabs |
| `workspace_tree.odin` | build_workstation_workspace_tree factory |
| `actions_cell_mutations.odin` | apply_workstation_preset, preset 4 handling |
| `workspace_toolbar.odin` | "W" preset button added |
| `app.odin` | Cycle_Context_Tab action kind |
| `actions.odin` | Cycle_Context_Tab handler, preset 4 delegation |
| `build_workspace.odin` | Role-aware focus logic, role inference in sync |
| `workspace_schema.odin` | WORKSPACE_SCHEMA_VERSION 11 |
| `persistence_test.odin` | Schema version assertion updated |
| `operating_model_test.odin` | Pane_Role_Cat ownership, role resolution tests |

## Files Created
| File | Purpose |
|------|---------|
| `pane_role_test.odin` | 16 tests: infer_pane_role, alloc role, tab availability, tab cycling, workstation tree, schema version, ownership |

## Test Summary
- **16 new tests**: role inference, pane allocation, tab availability per role, tab cycling, workstation tree validity, workstation tree roles, schema version, ownership
- **270 total tests** in app package, all pass
- **All core packages** compile clean (`check-core: all packages OK`)

## Backward Compatibility
- Existing workspaces load cleanly: `infer_pane_role()` auto-assigns roles from widget kind during sync
- Existing presets (D, C, A, K) unchanged
- Context stack defaults to showing all tabs (Primary_Chart role) for existing focused panes
- Zero wire-breaking changes
