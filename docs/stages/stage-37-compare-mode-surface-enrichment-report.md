# Stage 37 — Compare Mode Surface Enrichment

**Date:** 2026-03-07
**Branch:** `codex/s9-legacy-removal-cutover`
**Status:** COMPLETE

---

## Executive Summary

Stage 37 wires `Cell_Surface_View` — the S36 per-cell read model — into compare mode panes and main cell headers. Both surfaces now display composition badges (PEND/BFILL/LIVE/COMP) and health dots (green/yellow/red) derived entirely from the canonical read model. Compare mode no longer accesses stream view slots directly for identity resolution. Zero wire changes, zero new mutable state, zero protocol logic introduced outside `md_common`.

---

## Surface Audit (Pre-S37)

### Compare Mode (`build_compare.odin`)
- **Direct slot access:** Lines 63-74 reached into `reg.slots[slot_idx]` for venue/symbol
- **No composition/health/staleness display** — headers showed only venue:symbol label
- **No Cell_Surface_View usage** — slots accessed directly bypassing read model

### Cell Headers (`build_cell.odin`)
- **Direct slot access:** Lines 45-53 reached into `reg.slots[bind.stream_idx]` for venue/symbol
- **No composition badge** — no indication of data pipeline stage
- **No health indicator** — no per-cell health visibility
- Identity resolution duplicated between cell header and surface view

### Findings
- `resolve_cell_surface_view` (S36) was defined but had **zero callers**
- `resolve_compare_surface_view` did not exist — compare mode had no equivalent
- All rendering surfaces (widgets, layers) correctly abstained from protocol reads
- Only the header/chrome layer had direct slot access for identity

---

## S37 Architecture

### Design Decisions

1. **Composition badges:** EMPTY (hidden), PEND (yellow), BFILL (yellow), LIVE (accent), COMP (green)
   - Empty composition hides the badge entirely to avoid noise on startup
   - Color scheme matches existing build_status.odin HUD convention

2. **Health dots:** Green (Healthy), Yellow (Degraded), Red (Unhealthy/Critical)
   - 6x6 pixel filled rect — minimal footprint, high signal
   - Only shown when `has_live_data` or composition is non-Empty (avoids phantom dots on startup)

3. **Staleness surfacing:** Through health level color, not separate indicator
   - Health level already incorporates staleness (Degraded = aging, Unhealthy = stale)
   - Separate staleness badge would duplicate information and clutter compact headers

4. **Widget read model contract:** All widgets consume only `Cell_Surface_View`
   - `build_cell.odin` calls `resolve_cell_surface_view(state, ci)` once per cell
   - `build_compare.odin` calls `resolve_compare_surface_view(state, sid)` once per pane
   - No rendering code touches `slot.apply_state` or `active_apply_state`

### New Functions

| Function | File | Purpose |
|----------|------|---------|
| `resolve_compare_surface_view` | `stream_slots.odin` | Per-subject Cell_Surface_View for compare panes |
| `global_tf_ms` | `stream_slots.odin` | Global TF in ms (compare mode has no per-cell TF) |

### Modified Surfaces

| File | Change |
|------|--------|
| `build_compare.odin` | Replace direct slot access with `resolve_compare_surface_view`; add composition badge + health dot |
| `build_cell.odin` | Add `resolve_cell_surface_view` call; render composition badge + health dot after venue badge |

---

## Compare Mode Plan

**Before S37:**
```
[Binance:BTCUSDT]              ← venue:symbol only, raw slot access
```

**After S37:**
```
[Binance:BTCUSDT] COMP ●       ← venue:symbol + composition badge + health dot
```

- Compare pane headers now show the same operational signals as the diagnostics HUD
- Colors are consistent: green=healthy/composed, yellow=warning/pending, red=unhealthy/stale
- Health dot only appears when there's data (avoids misleading indicators on empty cells)

---

## Minimal Correct Implementation

### stream_slots.odin — `resolve_compare_surface_view`
- Takes `subject_id` (not cell index) since compare slots aren't world cells
- Finds the slot via `stream_view_find_slot`, reads `slot.apply_state` snapshot
- Uses `global_tf_ms` (compare mode has no per-cell TF overrides)
- Derives composition, candle_health, live_data, staleness, health, identity
- Pure function — no mutation, no allocations

### build_compare.odin — Surface view integration
- Replaced 12-line direct slot access block with 3-line `resolve_compare_surface_view` call
- Identity (venue:symbol) now read from `sv.venue`/`sv.symbol`
- Added composition badge after venue label (5-case switch, color-coded)
- Added health dot after composition badge (4-case switch, conditional on data presence)

### build_cell.odin — Surface view integration
- Added `resolve_cell_surface_view(state, ci)` call after venue badge
- Composition badge rendered right of venue badge
- Health dot rendered right of composition badge
- Layout flows left-to-right: `[venue:symbol] [COMP] [●] ... [widget] [TF] [×]`

---

## Code Changes

| File | Lines Changed | Type |
|------|--------------|------|
| `client/src/core/app/stream_slots.odin` | +62 | New `resolve_compare_surface_view` + `global_tf_ms` |
| `client/src/core/app/build_compare.odin` | +33 / -12 | Surface view integration, badge + dot rendering |
| `client/src/core/app/build_cell.odin` | +32 | Composition badge + health dot in cell header |
| `client/src/core/md_common/store_boundary_test.odin` | +80 | 10 new S37 tests |

**Total:** +207 / -12 lines

---

## Tests

10 new S37 tests added to `store_boundary_test.odin` (264 total):

| Test | Validates |
|------|-----------|
| `test_s37_surface_composition_empty_no_health_dot` | Empty state → Empty + Healthy |
| `test_s37_surface_composition_live_only` | Live candle → Live_Only + Healthy |
| `test_s37_surface_composition_composed` | Range + live → Composed |
| `test_s37_surface_health_degraded_on_aging` | 8s Stats → Degraded |
| `test_s37_surface_health_unhealthy_on_stale` | 12s Stats → Unhealthy |
| `test_s37_surface_staleness_counts_for_compare` | Mixed artifacts → correct aging count |
| `test_s37_surface_has_live_data_flag` | Empty=false, after trade=true |
| `test_s37_cell_composition_stage_pending` | Pending getrange → Range_Pending |
| `test_s37_cell_composition_stage_backfilled` | Seeded getrange → Backfilled |
| `test_s37_cell_composition_stage_composed` | Seeded + live → Composed |

---

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| `resolve_compare_surface_view` per-pane per-frame | Low | Pure derivation, zero alloc, max 4 compare panes |
| `resolve_cell_surface_view` called in addition to existing cell header work | Low | O(1) per call, pure query |
| Venue/symbol string refs from slot memory | Low | Slots outlive frame; callers do not persist refs |
| Health dot hidden for Empty composition | None | Intentional — avoids phantom indicators on startup |

---

## Recommended S38

**Stage 38 — Compare Mode Per-Pane TF Override & Store Isolation**
- Add per-pane TF selector in compare headers (currently compare uses global TF only)
- Resolve per-pane stores via `resolve_stores_for_cell` equivalent for compare slots
- Enable independent historical backfill per compare pane
