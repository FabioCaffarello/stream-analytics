# Stage 143 — Stream Health & Desync Model Hardening

**Date:** 2026-03-09
**ADR:** ADR-0032-stream-reliability-model.md

## Objective

Transform desync, snapshot stale, and stale recovery exhausted into an explicit, robust, and consistent model between backend, client, and UI.

## Changes

### 1. Stream_Reliability Enum (md_common/stream_apply_state.odin)

New canonical enum with 7 states:
- `Reliable` — transport live, data fresh, composition valid
- `Degraded_Aging` — artifacts aging, render with warning
- `Stale_Recovering` — auto-recovery in progress, render with warning
- `Stale_Unrecoverable` — recovery exhausted, block render
- `Desync` — transport desync, block render
- `Offline` — transport disconnected, block render
- `Manual_Resync` — exhausted + desync, manual intervention required

Pure derivation: `stream_reliability(health, recovery, is_desync, is_offline)`

Helper: `stream_reliability_blocks_render(r)` — true for blocking states.

### 2. Cell_Surface_View Enrichment (app/stream_slots.odin)

Added `reliability: Stream_Reliability` field to `Cell_Surface_View`.

Wired in both resolution paths:
- `resolve_cell_surface_view_with_stores` — main cell path
- `resolve_compare_surface_view` — compare pane path

Both derive reliability from health_level + recovery_status + transport state.

### 3. Widget Data Readiness Integration (app/widget_readiness.odin)

`Data_Readiness` gains 3 new variants:
- `Stale_Unreliable`
- `Desync_Unreliable`
- `Offline_Unreliable`

`widget_data_readiness` now checks `stream_reliability_blocks_render(sv.reliability)` before returning usable states. When data IS present but the stream is unreliable, the specific unreliable variant is returned.

### 4. Pane Visual State (app/shell_common.odin)

`Pane_Visual_State` gains `Degraded` variant.

`resolve_pane_visual_state` updated: Offline/Desync/Critical with cached store data now flow through to `widget_data_readiness` instead of blocking unconditionally. Without data, the original blocking behavior is preserved.

`draw_pane_state_overlay` handles `Degraded`: "Unreliable" title, warning color, "Ctrl+R to resync" hint.

### 5. Health Tick Enhancement (md_common/stream_apply_state.odin)

- `Health_Tick_Input` gains `is_desync: bool` field
- `Health_Tick_Output` gains `reliability: Stream_Reliability` field
- `Apply_State_Telemetry` gains `reliability` field
- `apply_state_telemetry` gains `is_desync`/`is_offline` parameters

### 6. Compare Pane Consistency (app/health.odin)

Compare pane exhaustion handler documented to clarify that the reliability model in `resolve_cell_surface_view` handles exhaustion surface consistently without needing transport-level DESYNC marking.

## Behavioral Changes

| Scenario | Before S143 | After S143 |
|----------|------------|------------|
| Offline + cached data | Blank "Offline" overlay | Data visible + "Unreliable" warning |
| Desync + cached data | Blank "Error" overlay | Data visible + "Unreliable" warning |
| Critical + cached data | Blank "Error" overlay | Data visible + "Unreliable" warning |
| Offline + no data | "Offline" overlay | "Offline" overlay (unchanged) |
| Desync + no data | "Error" overlay | "Error" overlay (unchanged) |
| Recovery exhausted | DESYNC marker on active | Reliability = Manual_Resync everywhere |

## Test Summary

- **12 new md_common tests**: Stream_Reliability derivation, blocks_render, labels, health_tick_output, telemetry
- **10 new app tests**: Widget readiness integration (Stale/Desync/Offline unreliable, reliable, degraded_aging, pane visual state)
- **1 updated app test**: S124 universal gates test updated for S143 Degraded behavior
- **All 424 md_common tests pass**
- **All 410 app tests pass**
- **Full WASM build clean**

## Files Changed

| File | Change |
|------|--------|
| `md_common/stream_apply_state.odin` | +Stream_Reliability enum, derivation, labels, blocks_render, Health_Tick enrichment |
| `app/stream_slots.odin` | +Cell_Surface_View.reliability, wired in both resolution paths |
| `app/widget_readiness.odin` | +3 Data_Readiness unreliable variants, reliability check in widget_data_readiness |
| `app/shell_common.odin` | +Pane_Visual_State.Degraded, overlay handler, updated resolve_pane_visual_state |
| `app/health.odin` | +Health_Tick_Input.is_desync, compare pane docs |
| `md_common/md_common_test.odin` | +12 reliability tests |
| `app/marketdata_test.odin` | +10 new tests, 1 updated |
| `docs/adrs/ADR-0032-stream-reliability-model.md` | New ADR |

## Zero Regressions

- No wire-breaking changes
- No new allocations on hot path
- All functions pure and deterministic
- Existing behavior preserved for data-absent cases
