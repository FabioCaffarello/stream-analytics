# Stage 26 — Per-Cell Composition & Composition-Driven Runtime

**Date:** 2026-03-06
**Status:** COMPLETE

## Executive Summary

Replaced manual `Context_Stage` writes (3 values, 7 locations) with derived `Composition_Stage` (5 values, single writer in `apply_state_sync_to_metrics`). Added `cell_composition_stage` pure function for per-cell composition queries. Updated HUD to display all 5 composition states. Zero wire contract changes, zero UI expansion, zero regressions.

## Current-State Audit (Pre-S26)

| Issue | Location | Severity |
|---|---|---|
| `Context_Stage` (3 values) manually written in 7 places | marketdata, stream_views, store_adapters | Medium — truth drift risk |
| `Composition_Stage` (5 values) exists but unused in active_metrics | md_common | Design gap |
| No per-cell composition semantics | Entity_World has GetRange_Component but no stage | Feature gap |
| HUD shows impoverished 3-value stage | build_ui.odin:688-699 | Observability gap |

## Architecture

### Design Decisions

1. **`Context_Stage` → `Composition_Stage`:** The legacy 3-value enum was a subset of the richer 5-value enum. Replace, don't extend.

2. **Derivation, not mutation:** `context_stage` is now written exclusively by `apply_state_sync_to_metrics`, derived from `apply_state_composition_stage()`. All 7 manual writes removed.

3. **Per-cell composition is a pure query:** `cell_composition_stage(pending, seeded, has_live_candle)` — no per-cell state tracking needed. `resolve_cell_composition(state, ci)` resolves the inputs from ECS components + slot state.

4. **No per-cell `Stream_Apply_State`:** Would be over-engineering. Cells already have `GetRange_Component` for getrange tracking + slot `apply_state` for live data flags. The composition query combines these.

## Code Changes

### md_common (pure domain)
- `stream_apply_state.odin`: Added `cell_composition_stage` pure function
- `store_boundary_test.odin`: +8 S26 tests (156 total, up from 148)

### app (integration layer)
- `components.odin`: `context_stage` type changed from `Context_Stage` to `md_common.Composition_Stage`; `Context_Stage` enum removed
- `store_adapters.odin`: `apply_state_sync_to_metrics` now derives `context_stage` from apply state (sole writer); removed manual `.Empty` writes from `reset_active_apply_state` and `tf_change_active_apply_state`
- `marketdata.odin`: Removed 3 manual `context_stage` writes (`.Live` in handle_candle_event, `.Backfilled` in handle_range_candle_batch, `.Empty` in pending_active_subject resolution)
- `stream_views.odin`: Removed 2 manual `context_stage` writes (`.Empty` in apply_cycle_stream_action and apply_set_cell_timeframe_action)
- `stream_slots.odin`: Added `resolve_cell_composition` query function; added `md_common` import
- `build_ui.odin`: Updated HUD to display all 5 composition stages (Empty, Pending, Backfilled, Live_Only, Composed)

## Tests

| Suite | Count | Status |
|---|---|---|
| md_common (total) | 156 | ALL PASS |
| S26 new tests | 8 | ALL PASS |
| app tests | 1 | ALL PASS |

### S26 Test Coverage
- `test_s26_cell_composition_stage_full_lifecycle` — all 5 stages + edge cases
- `test_s26_composition_stage_always_derivable` — proves derivation replaces manual writes
- `test_s26_cell_and_stream_composition_agree` — cell and stream stage equivalence
- `test_s26_summary_composition_stage_always_derived` — summary adapter consistency
- `test_s26_artifact_event_count_lifecycle` — event count persistence across lifecycle events

## Risks

- **Low:** Widgets that previously read `context_stage` with 3-value switch now see 5 values. All switches use explicit case matching (no default fallback), so the Odin compiler would catch missing cases. The only consumer (build_ui.odin) was updated.

## Metrics

- Manual `context_stage` writes: 7 → 0 (100% reduction)
- Composition stage values: 3 → 5 (67% richer observability)
- New pure functions: 2 (`cell_composition_stage`, `resolve_cell_composition`)
- Test count: 148 → 156 (+8)
- LOC changed: ~40 removed, ~120 added (net +80, mostly tests)

## Recommended S27

**HUD Telemetry Expansion:** Surface per-artifact event counts (`artifact_event_count[kind]`) in the telemetry overlay. Currently tracked in apply state but not rendered. Could also add per-cell composition badge to cell headers for multi-stream layouts.
