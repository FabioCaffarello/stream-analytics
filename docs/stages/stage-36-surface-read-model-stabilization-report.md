# Stage 36 — Surface Read-Model Stabilization

**Date:** 2026-03-07
**Branch:** codex/s9-legacy-removal-cutover
**Tests:** 254 (up from 242), 12 new S36 tests

---

## Executive Summary

S36 stabilizes the read models consumed by cells, compare mode, panels, and widgets so every surface reads from coalesced, derived views rather than reaching into protocol/apply_state internals. A new `Cell_Surface_View` read model bundles composition, health, staleness, and identity into a single per-cell query. Three coupling violations were fixed where surface code read `active_metrics` (a derived copy) instead of the canonical `active_apply_state`.

**Key principle:** Surfaces read derived views. Only adapters and pure queries touch canonical state.

---

## Surface Audit

| Surface | Pre-S36 Read Path | Gap | S36 Fix |
|---------|-------------------|-----|---------|
| Cell widgets | `resolve_cell_subject_id` → layer canvas | None | Clean |
| Cell headers | `build_cell.odin` → slot.stream_info | None | Clean |
| Top bar price | `state.stores.candle` direct | Acceptable (UI display) | — |
| HUD cache | `state.active_apply_state` via pure queries | None | Clean (telemetry queries) |
| Health panel | `state.active_apply_state` via `apply_state_telemetry` | None | Clean |
| Compare mode | `stream_view_find_slot` → slot | No per-pane health | `Cell_Surface_View` available |
| **Store resolution** | `resolve_stores_for_cell` | **Read `active_metrics.has_live_*`** | **Fixed → `active_apply_state`** |
| **Waiting primary data** | `active_stream_waiting_primary_data` | **Read `active_metrics.last_*_ts_ms`** | **Fixed → `active_apply_state`** |
| **Reason short** | `active_stream_reason_short` | **Read `active_metrics.last_stats_ts_ms`** | **Fixed → `active_apply_state`** |
| Status bar badges | `active_metrics.*` | None | Clean (derived view) |
| Runtime probe | `active_metrics.has_live_*` | None | Clean (diagnostics) |
| Overlay controls | `state.stores.*` direct | None | Acceptable (UI state) |

---

## Read-Model Architecture

### Existing Read Models (unchanged)
| Read Model | Scope | Writer | Consumers |
|-----------|-------|--------|-----------|
| `Active_Stream_Metrics` | Global transport/protocol | `health.odin`, `store_adapters.odin` | Status bar, HUD, health panel |
| `Stream_Apply_State` | Per-stream canonical | `layer_marketdata.odin` drain handlers | Adapters, pure queries |
| `Apply_State_Telemetry` | Per-stream diagnostics | Pure query | Health panel |
| `Aggregate_Health_Summary` | Cross-slot health | Pure query | HUD cache |
| `Cell_Stores` | Per-cell store resolution | `resolve_stores_for_cell` | Layer canvas, candle health |
| `GetRange_Global_State` | Derived getrange view | `apply_state_sync_to_getrange` | GetRange orchestrator |
| `Telemetry_HUD_Cache` | Throttled display strings | `refresh_telemetry_hud_cache` | HUD rendering |

### New: `Cell_Surface_View` (S36)
| Field | Type | Source |
|-------|------|--------|
| `composition` | `Composition_Stage` | `resolve_cell_composition` (S26) |
| `candle_health` | `Candle_Health` | `compute_candle_health_for_store` per cell |
| `has_live_data` | `bool` | Any `apply_state.has_live[*]` true |
| `stale_count` | `int` | `apply_state_stale_artifact_count` |
| `aging_count` | `int` | `apply_state_stale_artifact_count` |
| `venue` | `string` | Slot stream info |
| `symbol` | `string` | Slot stream info |
| `stream_bound` | `bool` | Binding check |
| `health_level` | `System_Health_Level` | `stream_health_level` |

### New: `resolve_cell_apply_state` (S36)
Returns a value-copy snapshot of the apply state for a cell's bound slot (or global active). Used by `resolve_cell_surface_view` and available for any surface that needs per-cell apply state queries.

---

## Stabilization Rules

1. **Surfaces read only via:** `Cell_Surface_View`, `Active_Stream_Metrics`, `Telemetry_HUD_Cache`, `Cell_Stores`, or `Apply_State_Telemetry`
2. **`active_apply_state` is never read directly by rendering code** — only by adapters (`store_adapters.odin`) and pure telemetry queries (`build_status.odin` HUD cache)
3. **New surfaces must call `resolve_cell_surface_view`** — not reach into slots/apply_state
4. **`active_metrics.has_live_*` remain as a derived copy** — written solely by `apply_state_sync_to_metrics` — but cell-level code should prefer `resolve_cell_apply_state`

---

## Code Changes

### `client/src/core/app/stream_slots.odin`
- **Added** `Cell_Surface_View` struct — unified per-cell read model (9 fields)
- **Added** `resolve_cell_apply_state` — resolves apply state for a cell (bound slot or global active)
- **Added** `resolve_cell_surface_view` — derives full surface view for a cell in one call
- **Fixed** `resolve_stores_for_cell` — `heatmap_live`/`vpvr_live` now read from `active_apply_state.has_live[*]` instead of `active_metrics.has_live_*` (removes coupling to derived metrics)

### `client/src/core/app/build_status.odin`
- **Fixed** `active_stream_waiting_primary_data` — reads `active_apply_state.last_recv_ms[.Stats/.Orderbook]` instead of `active_metrics.last_stats_ts_ms/.last_orderbook_ts_ms`
- **Fixed** `active_stream_reason_short` — reads `active_apply_state.last_recv_ms[.Stats]` instead of `active_metrics.last_stats_ts_ms`

### `client/src/core/md_common/store_boundary_test.odin`
- **Added** 12 new S36 tests covering composition stages, staleness levels, health derivation, candle recv timing, and artifact counts

---

## Tests

254 total tests (up from 242). 12 new S36 tests:

| Test | Validates |
|------|-----------|
| `test_s36_composition_stage_empty_state` | Empty apply state → Empty + Healthy |
| `test_s36_composition_stage_live_only` | Live candle → Live_Only |
| `test_s36_composition_stage_backfilled` | Range complete → Backfilled |
| `test_s36_composition_stage_composed` | Range + live → Composed |
| `test_s36_cell_composition_mirrors_global` | Cell + global produce matching results |
| `test_s36_staleness_fresh_when_recent` | 1s age → zero stale/aging |
| `test_s36_staleness_aging_at_8s` | 8s Dual_Silence → Aging |
| `test_s36_staleness_stale_at_12s` | 12s Dual_Silence → Stale |
| `test_s36_health_level_degraded_when_aging` | Aging artifact → Degraded |
| `test_s36_summary_has_live_flags` | Summary reflects per-artifact live state |
| `test_s36_candle_recv_ms_max_of_live_and_range` | Returns max of live + range candle timing |
| `test_s36_active_artifact_count` | Counts artifacts with events |

---

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| `resolve_cell_surface_view` called per-cell per-frame | Low | Pure derivation, zero alloc, O(1) per call |
| Surfaces still reading `active_metrics.has_live_*` | Low | Correctly derived by adapter; cell-level code now uses canonical source |
| `Cell_Surface_View.venue/symbol` are string refs to slot memory | Low | Slots outlive frame; callers must not persist across frames |

---

## Recommended Next Stage

**Stage 37 — Compare Mode Surface Enrichment**
- Wire `resolve_cell_surface_view` into compare mode panes (per-pane health/staleness badges)
- Add composition badge to cell headers (LIVE/COMP/PEND/BFILL/EMPTY)
- Consider per-cell staleness indicator in widget chrome
