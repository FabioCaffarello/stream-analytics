# Stage 40 — Compare Branch Truth Alignment

**Date:** 2026-03-07
**Status:** VALIDATED (no code changes required)
**Tests:** 281 (unchanged — no new mutations)
**Wire changes:** Zero
**New mutable state:** Zero

---

## Executive Summary

A rigorous truth-alignment audit of the compare mode layer confirms that **all compare code paths already consume derived views exclusively**. Stages 36–39 progressively built the canonical read-model architecture (`Cell_Surface_View`, per-pane composition, per-pane TF isolation, per-pane backfill), and no legacy direct-read paths remain.

**Result:** The compare branch is architecturally sound. No code changes are required. This stage documents the verified truth alignment.

---

## Truth Audit

### Methodology

Every file touching compare mode was audited for:
1. Direct reads of `slot.apply_state.*` from render paths
2. Direct reads of `slot.candle_store/heatmap_store/vpvr_store` from render paths
3. Health/composition/staleness calculations outside derived-view resolvers
4. Protocol interpretation outside `md_common` canonical functions

### Files Audited

| File | Lines | Role |
|------|-------|------|
| `build_compare.odin` | 9–152 | Render path |
| `stream_slots.odin` | 535–743 | Derived views + mutations |
| `stream_views.odin` | 316–344 | Per-pane TF resolvers |
| `actions.odin` | 52–504 | Enter/add/focus/TF actions |
| `reconcile.odin` | 232–270 | Subscription management |
| `components.odin` | 194–212 | Compare_State struct |
| `store_adapters.odin` | 1–143 | Adapter layer |
| `health.odin` | full | Health control plane |
| `build_status.odin` | full | Status panel |
| `build_ui.odin` | 197 | Compare dispatch |
| `top_bar.odin` | 241 | Compare indicator |
| `app.odin` | 504–508, 734–738, 906–908 | Init + persistence |

### Violations Found: **ZERO**

---

## Canonical Compare Model

### Read Model: `Cell_Surface_View` (shared with cells)

```
Cell_Surface_View :: struct {
    composition:   Composition_Stage,      // .Empty | .Range_Pending | .Backfilled | .Live_Only | .Composed
    candle_health: Candle_Health,           // .No_Data | .OK | .Lagging | .Stale
    has_live_data: bool,
    stale_count:   int,
    aging_count:   int,
    venue:         string,
    symbol:        string,
    stream_bound:  bool,
    health_level:  System_Health_Level,     // .Healthy | .Degraded | .Unhealthy | .Critical
}
```

**Resolver:** `resolve_compare_surface_view(state, pane_idx) -> Cell_Surface_View`

### Derivation Chain

```
build_compare_mode()
  for pane in 0..<count:
    compare_pane_resolve_subject_id(state, ci)     -- TF-aware slot lookup
      compare_pane_effective_tf_string(state, ci)   -- per-pane or global TF
      find_market_channel_slot(reg, venue, symbol, tf)
    resolve_compare_surface_view(state, ci)         -- derived read model
      resolve_compare_pane_composition(state, ci)   -- per-pane getrange + live candle
        cell_composition_stage(pending, seeded, has_live)
      compute_candle_health_for_store(...)
      apply_state_stale_artifact_count(apply, ...)
      stream_health_level(apply, ...)
    [render using sv.venue, sv.symbol, sv.composition, sv.health_level only]
    render_subject_layer_canvas(state, sid, kind, rect)
```

### Compare_State (owned mutable state)

All compare-local state lives in `Compare_State`:
- `slots[4]u64` — pane subject IDs
- `tf_idx[4]int` — per-pane TF override (-1 = global)
- `getranges[4]Compare_Pane_GetRange` — per-pane backfill tracking
- `focused_pane: int` — focus management
- Display toggles: `scroll_x`, `zoom`, `show_vol`, `show_heatmap`, `show_vpvr`, etc.

No protocol state, no apply_state copies, no health caches.

---

## Access Pattern Classification

| Access Type | Count | Location | Verdict |
|-------------|-------|----------|---------|
| Derived view reads (`Cell_Surface_View`) | 6 | `build_compare.odin` | Correct |
| Pure TF derivation | 3 | `stream_views.odin` | Correct |
| Compare_State field reads | ~30 | `actions/build_compare/reconcile` | Correct (owned state) |
| Slot identity for subscription | 2 | `reconcile.odin:245-246` | Correct (infra, not render) |
| Slot identity for resolution | 2 | `stream_slots.odin:554-555` | Correct (inside resolver) |
| `apply_state` reads wrapped in resolver | 4 | `stream_slots.odin:580-604` | Correct (encapsulated) |
| Store mutation on TF change | 1 | `stream_slots.odin:726-731` | Correct (via `apply_state_on_tf_change`) |
| Direct render-path slot reads | **0** | — | Clean |

---

## Legacy Readers Removed: N/A

No legacy readers exist. The progressive build through S36–S39 never left behind direct-read paths:

- **S36** introduced `Cell_Surface_View` + `resolve_cell_surface_view`
- **S37** added `resolve_compare_surface_view` (compare consumed view from day one)
- **S38** added per-pane TF isolation via pure functions
- **S39** added per-pane composition + backfill via `Compare_Pane_GetRange`

Each stage built on the prior canonical layer. No compat shims were needed.

---

## Compat Layers: None

The compare mode was built after the canonical architecture was established (S36+). Unlike cells (which required migration from legacy patterns), compare was designed view-first.

---

## Protocol Isolation Verification

Compare mode **never** interprets protocol/health/composition directly:

1. **Composition** — derived via `resolve_compare_pane_composition()` → `cell_composition_stage()` (shared pure fn)
2. **Health** — derived via `stream_health_level()` (md_common canonical fn, called inside resolver)
3. **Staleness** — derived via `apply_state_stale_artifact_count()` (md_common canonical fn, called inside resolver)
4. **Candle health** — derived via `compute_candle_health_for_store()` (called inside resolver)

All four are encapsulated within `resolve_compare_surface_view()`. The render path sees only the `Cell_Surface_View` struct.

---

## Pure Function Inventory (Compare)

| Function | File | Lines | Pure | Tested |
|----------|------|-------|------|--------|
| `compare_pane_effective_tf_idx` | stream_views.odin | 316–324 | Yes | Yes |
| `compare_pane_effective_tf_string` | stream_views.odin | 327–334 | Yes | Yes |
| `compare_pane_effective_tf_ms` | stream_views.odin | 337–344 | Yes | Yes |
| `compare_pane_resolve_subject_id` | stream_slots.odin | 535–559 | Yes | Yes |
| `resolve_compare_surface_view` | stream_slots.odin | 565–614 | Yes | Yes |
| `resolve_compare_pane_composition` | stream_slots.odin | 618–633 | Yes | Yes |

---

## Risks

**None identified.** The architecture is clean.

Minor observation: `reconcile.odin:245-246` reads `slot.stream_info.venue/symbol` for subscription management. This is correct — reconcile is infrastructure, not a render path, and needs raw identity to build subscription requests. No change needed.

---

## Recommended S41

With compare mode truth-aligned, the next high-value target is one of:

1. **Cell legacy removal** — audit cell render paths (`build_cell.odin`, `build_status.odin`) for any remaining direct slot reads that bypass `Cell_Surface_View`
2. **Diagnostic panel consolidation** — ensure HUD/diagnostics consume only derived views
3. **Test hardening** — property-based tests for `resolve_compare_surface_view` edge cases (empty slots, unsubscribed TFs, 0-count panes)
4. **Compare mode scroll/zoom sync** — per-pane scroll isolation (currently basic)

Recommended: **Option 1 (Cell legacy removal)** if any direct reads remain, else **Option 3 (test hardening)**.
