# Stage 45 — Surface Contract Versioning & Workspace State Schema

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 329 (10 new S45), zero regressions

## Executive Summary

S45 introduces structural discipline for workspace state persistence and evolution. A canonical `Workspace_Schema` contract explicitly separates persisted fields from derived/transient state. The V6 layout format extends V5 with per-cell chart display persistence (show_vol, show_heatmap, show_vpvr, heatmap_intensity, ob/dom group, trade_filter), closing a gap where per-cell chart overrides were lost on restart. Custom presets are upgraded from V1 to V6 format, preserving full cell state. The `SETTING_SETTINGS_VERSION` key is now actively set to track schema evolution.

## Workspace Audit

### Previously Persisted (V5)
- Layout mode (Preset/Custom), preset index, col/row weights
- Per-cell: widget kind, stream binding, indicator flags, col/row span, subplot ratios, per-cell TF
- Signal-evidence link flag
- Active stream, global TF, panel visibility, draw tools, indicator params (global), chart defaults (global)
- Connection profiles, layer registry, assist mode

### Gap Identified
- **Per-cell chart display state** (Chart_Component) was NOT persisted: show_vol, show_heatmap, show_vpvr, heatmap_intensity_idx, ob_group_idx, dom_group_idx, trade_filter_idx were reset to global defaults on every restart.
- **Custom presets** used V1 format (widget kinds only), losing bindings, indicators, spans, TF, and chart display on save/load.
- **Schema version** (`SETTING_SETTINGS_VERSION`) existed but was never written.

### Intentionally Transient (Derived/Runtime)
- `Cell_Surface_View` — derived per-frame from apply_state + bindings
- `Stream_Apply_State` — populated from live protocol events
- `GetRange_Component` — transient backfill (reseeded on connect)
- `Compare_State` — ephemeral comparison session
- `View_Component` — scroll/zoom/crosshair (reset on startup)
- `Overlay_State` — UI modals (transient)
- Telemetry, Connection, Error, Recovery state — runtime-only

## Schema Architecture

### WORKSPACE_SCHEMA_VERSION = 6

New constant in `workspace_schema.odin` that serves as the single source of truth for the persistence format version. Bumped on every format change.

### V6 Layout Format
```
V6|MODE|CW:w0,...|RW:w0,...|K:S:F:CS:RS:SM:SR:TF:CD|...|LK:flag
```

New field per cell:
- **CD** = chart display packed integer (17 bits):
  - bit0: show_vol
  - bit1: show_heatmap
  - bit2: show_vpvr
  - bits3-4: heatmap_intensity_idx (0-3)
  - bits5-8: ob_group_idx (0-15)
  - bits9-12: dom_group_idx (0-15)
  - bits13-16: trade_filter_idx (0-15)

### Versioning & Migration
- **Restore chain:** V6 → V5 → V4 → V3 → V2 → V1 (fallback)
- **Persist:** V6 writes to `SETTING_LAYOUT_V6`, then calls `persist_layout_v4` for V5/V4 rollback
- **Schema marker:** `SETTING_SETTINGS_VERSION` set to "6" on every persist
- **Clipboard:** Export/import now handles V6/V5/V4 strings
- **Custom presets:** Upgraded from V1 to V6 format; load tries V6 → V4 → V1

### Persisted vs Derived Contract
Documented in `workspace_schema.odin` as structured comments. Every field in `Entity_World` and `App_State` is classified as either persisted (survives restart) or derived (reconstructed at runtime).

## Code Changes

### New Files
| File | Lines | Purpose |
|------|-------|---------|
| `core/app/workspace_schema.odin` | 63 | Schema version constant, persist/derive contract, pack/unpack chart display |

### Modified Files
| File | Change |
|------|--------|
| `core/services/settings_store.odin` | Add `SETTING_LAYOUT_V6`, add to known_keys |
| `core/app/layout_persist.odin` | Add `build_layout_v6_string`, `persist_layout_v6`, `restore_layout_v6[_from_string]`; upgrade export/import to V6; upgrade custom presets to V6 |
| `core/app/app.odin` | Restore chain: V6 → V5 → V4 → V3 → V2 → V1 |
| `core/app/build_ui.odin` | 4× `persist_layout_v4` → `persist_layout_v6` |
| `core/app/actions_cell_mutations.odin` | 4× `persist_layout_v4` → `persist_layout_v6` |
| `core/app/actions.odin` | 1× `persist_layout_v4` → `persist_layout_v6` |
| `core/app/stream_views.odin` | 1× `persist_layout_v4` → `persist_layout_v6` |
| `core/md_common/store_boundary_test.odin` | 10 new S45 tests |

### Caller Migration (12 sites)
All 12 external call sites of `persist_layout_v4` updated to `persist_layout_v6`. The V4 function remains as an internal rollback mechanism called by `persist_layout_v6`.

## Tests (10 new)

| Test | Validates |
|------|-----------|
| `test_s45_chart_display_bitfield_roundtrip` | Pack/unpack symmetry with mixed fields |
| `test_s45_chart_display_zero_state` | Zero packs/unpacks cleanly |
| `test_s45_chart_display_max_values` | Maximum valid indices fit in allocated bits |
| `test_s45_chart_display_ob_grp_isolation` | ob_group bits don't bleed into neighbors |
| `test_s45_chart_display_dom_grp_isolation` | dom_group bits don't bleed into neighbors |
| `test_s45_schema_composition_deterministic` | Same inputs → same composition stage |
| `test_s45_schema_recovery_is_transient` | Recovery state resets on TF change (not persisted) |
| `test_s45_schema_reconnect_preserves_persisted_markers` | Reconnect preserves non-gated state |
| `test_s45_schema_composition_lifecycle_coverage` | All 5 stages reachable: Empty→Pending→Backfilled→LiveOnly→Composed |
| (integration) | Full test suite: 329 tests, 0 failures |

## Constraints Verified
- **Zero wire changes** — persistence only, no protocol modifications
- **Zero new mutable runtime state** — pack/unpack are pure functions, schema version is a constant
- **Zero regression** — 329 tests pass, V5/V4 rollback preserved
- **Surfaces unchanged** — Cell_Surface_View remains a derived read model, never persisted

## Risks

| Risk | Mitigation |
|------|------------|
| V6 string slightly larger (~60 bytes for 12 cells) | 2048-byte buffer has ~700 bytes headroom |
| Old presets (V1 format) in custom slots | load_custom_preset falls back V6 → V4 → V1 |
| Future field additions to Chart_Component | CD bitfield has bits 17-31 available for expansion |

## Recommended S46

**Legacy Persistence Cleanup & Indicator Params Per-Cell:**
1. Per-cell indicator params (MA periods, BB sigma, RSI/MACD) — currently global-only, V6 could extend
2. Workspace export/import as self-describing format (JSON envelope wrapping V6 for interop)
3. Compare mode state persistence (optional — currently intentionally ephemeral)
4. Remove V1/V2/V3 restore code paths (dead code after migration period)
