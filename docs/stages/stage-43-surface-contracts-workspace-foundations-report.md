# Stage 43 — Surface Contracts & Workspace Foundations

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 312 (11 new S43)
**Wire changes:** Zero
**New mutable state:** Zero

---

## Executive Summary

Stage 43 stabilizes the surface read model contracts established in S36-S42, eliminates the last direct slot bypass in the cell render path, and adds comprehensive contract tests for the composed `Cell_Surface_View`. The architecture is confirmed clean: all render paths consume unified surface views, diagnostics panels use apply_state directly (by design), and no violations exist.

---

## Surface Audit

### Architecture Status: CLEAN

| Surface | Reader | Source | Status |
|---------|--------|--------|--------|
| Cell header (identity) | `build_cell.odin` | ~~direct slot~~ → `sv.venue`/`sv.symbol` | **S43: Fixed** |
| Cell header (composition) | `build_cell.odin` | `sv.composition` | Clean |
| Cell header (health) | `build_cell.odin` | `sv.health_level` | Clean |
| Compare header (identity) | `build_compare.odin` | `sv.venue`/`sv.symbol` | Clean |
| Compare header (composition) | `build_compare.odin` | `sv.composition` | Clean |
| Compare header (recovery) | `build_compare.odin` | `sv.recovery_status` | Clean |
| Compare header (health) | `build_compare.odin` | `sv.health_level` | Clean |
| Diagnostics/HUD | `build_status.odin` | `active_apply_state` direct | Acceptable by design |
| Layer canvas | `layer_canvas.odin` | `subject_id` resolution | Correct separation |

### Cell_Surface_View Struct (10 fields, stable)

```odin
Cell_Surface_View :: struct {
    composition:     Composition_Stage,
    candle_health:   Candle_Health,
    has_live_data:   bool,
    stale_count:     int,
    aging_count:     int,
    venue:           string,
    symbol:          string,
    stream_bound:    bool,
    health_level:    System_Health_Level,
    recovery_status: Recovery_Status,
}
```

### Resolvers (2 primary, 5 intermediate)

| Resolver | Scope | Pure |
|----------|-------|------|
| `resolve_cell_surface_view` | Per-cell | Yes |
| `resolve_compare_surface_view` | Per-pane | Yes |
| `resolve_cell_composition` | Per-cell | Yes |
| `resolve_compare_pane_composition` | Per-pane | Yes |
| `resolve_cell_apply_state` | Per-cell | Yes |
| `compare_pane_resolve_subject_id` | Per-pane | Yes |
| `resolve_stores_for_cell` | Per-cell | Yes (side-effect: lazy binding) |

---

## Contract Architecture

### Global Contracts (mode-independent)

1. **Composition derivation**: `apply_state_composition_stage(s)` and `cell_composition_stage(pending, seeded, live)` — derive `Composition_Stage` from apply_state or cell getrange inputs. Agreement tested in S43.
2. **Health derivation**: `stream_health_level(s, now_ms, tf_ms)` — derives `System_Health_Level` from apply_state + timing. TF-sensitive.
3. **Staleness counts**: `apply_state_stale_artifact_count(s, now_ms, tf_ms)` — TF-adaptive thresholds.
4. **Recovery status**: `apply_state_recovery_status(s)` — pure derivation from `recovery_attempts`.
5. **Candle health**: `compute_candle_health_for_store(store, recv_ms, tf_ms, now_ms)` — parameterized, used by both resolvers.

### Per-Cell/Per-Pane Contracts

1. **Cell surface view**: Composes global contracts with cell-specific binding/TF/getrange state.
2. **Compare surface view**: Composes global contracts with per-pane TF/getrange/slot resolution.
3. **Store resolution**: `resolve_stores_for_cell` maps cell binding → channel-specific stores.

### Versioning Strategy

- `Cell_Surface_View` struct is the **stable read API** for all surfaces.
- Fields may be added (additive evolution) but never removed or retyped.
- New surfaces must consume `Cell_Surface_View` via resolvers — never reach into apply_state, slots, or stores directly.
- Diagnostics panels are explicitly exempt (they surface internal state for debugging).

---

## Stabilization Plan (Executed)

### Fix: Cell Header Identity Bypass (build_cell.odin)

**Before (S37-S42):**
```odin
// Lines 45-53: Direct slot access for venue/symbol badge
if bind.stream_idx >= 0 && bind.stream_idx < STREAM_VIEW_CAP {
    slot := &reg.slots[bind.stream_idx]
    badge_label = fmt.bprintf(badge_buf[:], "%s:%s", slot.stream_info.venue, ...)
}
// Line 67: Surface view resolved AFTER badge
sv := resolve_cell_surface_view(state, ci)
```

**After (S43):**
```odin
// Surface view resolved ONCE, BEFORE all header elements
sv := resolve_cell_surface_view(state, ci)
// Badge uses sv.venue/sv.symbol — no direct slot access
if sv.stream_bound && len(sv.venue) > 0 {
    badge_label = fmt.bprintf(badge_buf[:], "%s:%s", sv.venue, sv.symbol)
}
```

**Behavior improvement:** Intent-bound cells (PRD-0009) now show their target market name instead of "~ Active" — more correct since they ARE bound.

### Tests: Surface View Composition Contracts (11 new)

| Test | Validates |
|------|-----------|
| `test_s43_surface_empty_state_contract` | Empty → Empty comp, Healthy, no stale, no recovery |
| `test_s43_surface_live_only_contract` | Live candle → Live_Only, Healthy |
| `test_s43_surface_composed_contract` | Getrange seeded + live → Composed, Healthy |
| `test_s43_surface_range_pending_contract` | Getrange pending → Range_Pending |
| `test_s43_surface_backfilled_contract` | Getrange seeded, no live → Backfilled |
| `test_s43_surface_degraded_staleness_contract` | Aging artifacts → Degraded, aging > 0 |
| `test_s43_surface_unhealthy_staleness_contract` | Stale artifacts → Unhealthy, stale > 0 |
| `test_s43_surface_recovery_contract` | Recovery progression: None→Degraded→Unhealthy |
| `test_s43_cell_composition_matches_apply_state` | cell_composition_stage agrees with apply_state_composition_stage |
| `test_s43_surface_tf_sensitive_health_isolation` | Same state, different TF → different health |
| `test_s43_surface_staleness_tf_scaling` | Stale counts scale with TF (1m vs 1h) |

---

## Code Changes

| File | Change | Lines |
|------|--------|-------|
| `build_cell.odin` | Move surface view before badge; use `sv.venue`/`sv.symbol` | -10/+5 |
| `store_boundary_test.odin` | 11 new S43 surface contract tests | +145 |

---

## Risks

- **None identified.** Changes are additive tests + a minor render-order fix.
- Cell header badge now shows market name for intent-bound cells — strictly an improvement.
- Zero wire contract changes, zero new mutable state, zero protocol logic changes.

---

## Recommended Next Stage

**Stage 44 — Widget Catalog Isolation & Surface Adapter Layer**

Candidates:
1. **Widget data adapter**: Formalize how widgets (Candle, Trades, OB, Heatmap, VPVR, Stats, DOM) receive their data stores. Currently `resolve_stores_for_cell` returns `Cell_Stores` — this could become the stable "widget data contract" preventing widgets from reaching into app state.
2. **Workspace layout model**: Extract cell grid layout + compare mode layout into a workspace read model, preparing for multi-workspace support.
3. **Surface registry**: If multiple dashboards/workspaces need independent surface views, a registry pattern could version and isolate them.

Priority recommendation: Widget data adapter (highest coupling reduction per effort).
