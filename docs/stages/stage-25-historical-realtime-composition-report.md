# Stage 25 — Historical/Realtime Composition Report

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

## Executive Summary

S25 formalizes the deterministic composition between historical seed data (GetRange), realtime deltas, and the apply-state lifecycle. GetRange tracking is consolidated so `Stream_Apply_State` is the **single source of truth** for range state (pending, seeded, oldest_ts, sent_frame, candle_subject_id). The global `GetRange_Global_State` becomes a **derived view** synced via the `apply_state_sync_to_getrange` adapter — identical to the S24 pattern for `active_metrics.has_live_*`.

## Current-State Audit (Pre-S25)

**Three competing getrange truth sources:**
1. `GetRange_Global_State` (6 fields) — `App_State.getrange`
2. `GetRange_Component` (4 fields per cell) — `Entity_World.getranges`
3. `Stream_Apply_State.getrange_*` (4 fields) — per-stream in md_common

**7 manual reset sites:**
- `apply_cycle_stream_action` (stream switch)
- `apply_pick_stream_action` (pick stream)
- `apply_set_timeframe_action` (TF change)
- `apply_set_cell_timeframe_action` (per-cell TF)
- `apply_resync_active_stream_action` (resync)
- `drain_marketdata` reconnect path
- `handle_range_candle_batch` completion path

Each reset site manually zeroed `state.getrange.{pending,seeded,oldest_ts}` independently of the apply state, creating a dual-truth drift risk.

## S25 Architecture

### Single Source of Truth: `Stream_Apply_State`

All getrange state writes now go through `Stream_Apply_State`:
- `apply_state_mark_range_sent(frame, candle_subject_id)` — request sent
- `apply_state_mark_range_complete(oldest_ts)` — batch completed
- `apply_state_on_tf_change()` — clears all range state
- `apply_state_on_reconnect()` — clears pending only
- `apply_state_reset()` — full reset

### Derived View: `GetRange_Global_State`

Synced via `apply_state_sync_to_getrange()` adapter:
```
apply_state → getrange.pending
apply_state → getrange.seeded
apply_state → getrange.oldest_ts
apply_state → getrange.sent_frame
apply_state → getrange.active_candle_subject_id
```

`getrange.subject_id` remains request-scoped (cleared independently on stream switch/resync since it tracks the in-flight HTTP request, not stream state).

### Composition Stage Model

New pure function `apply_state_composition_stage()` returns:
- **Empty** — no data at all
- **Range_Pending** — GetRange in flight
- **Backfilled** — historical data received, no live candles yet
- **Live_Only** — live candles but no historical backfill
- **Composed** — both historical + live, fully coherent

### Per-Artifact Observability

New `artifact_event_count: [Artifact_Kind]u64` field tracks event counts per artifact, complementing the existing `event_count` (total) and `last_recv_ms` (timestamps).

## Code Changes

### md_common/stream_apply_state.odin
- Added `artifact_event_count: [Artifact_Kind]u64` field
- Added `range_candle_subject_id: u64` field
- Updated `apply_state_mark_event` to increment per-artifact count
- Updated `apply_state_mark_range_sent` with optional `candle_subject_id` param
- Updated `apply_state_on_tf_change` to clear `range_candle_subject_id`
- Added `apply_state_is_range_ready()` — seed + live query
- Added `apply_state_composition_stage()` — 5-state composition model
- Added `Composition_Stage` enum
- Extended `Apply_State_Summary` with `composition_stage`

### app/store_adapters.odin
- Added `apply_state_sync_to_getrange()` — syncs apply_state → GetRange_Global_State
- Added `apply_state_sync_all()` — combined metrics + getrange sync
- Updated `reset_active_apply_state`, `reconnect_active_apply_state`, `tf_change_active_apply_state`, `sync_active_apply_state_from_slot` to use `apply_state_sync_all`

### app/stream_views.odin
- `request_active_stream_candle_range`: writes to apply state via `mark_range_sent`
- `request_older_candles`: writes to apply state via `mark_range_sent`
- `ensure_active_candle_subject_id`: writes `range_candle_subject_id` to apply state
- `apply_cycle_stream_action`: removed 4 manual getrange resets
- `apply_set_timeframe_action`: removed 4 manual getrange resets
- `apply_set_cell_timeframe_action`: updated sync call

### app/actions_stream_control.odin
- `apply_pick_stream_action`: removed 4 manual getrange resets
- `apply_resync_active_stream_action`: removed 4 manual getrange resets

### app/marketdata.odin
- `drain_marketdata` reconnect: removed manual getrange.pending reset (handled by apply_state_on_reconnect)
- `handle_range_candle_batch`: writes oldest_ts to apply state during batch; completion via `mark_range_complete`
- Getrange timeout: uses `apply_state_check_getrange_timeout` instead of direct field comparison
- End-of-frame: `apply_state_sync_all` replaces `apply_state_sync_to_metrics`

### app/layer_marketdata.odin
- `sync_legacy_stores_from_layer_store`: `apply_state_sync_all` replaces `apply_state_sync_to_metrics`

## Tests

**10 new tests** (148 total in md_common, up from 138):

1. `test_s25_composition_stage_full_lifecycle` — Empty → Range_Pending → Backfilled → Composed
2. `test_s25_composition_live_only_no_seed` — Live_Only without getrange
3. `test_s25_composition_tf_swap_resets` — TF change returns to Empty
4. `test_s25_composition_reconnect_preserves_seed` — Reconnect keeps Composed
5. `test_s25_per_artifact_event_count` — Independent per-artifact counters
6. `test_s25_range_candle_subject_id_lifecycle` — Set/persist/clear through lifecycle
7. `test_s25_summary_includes_composition_stage` — Summary adapter reflects stage
8. `test_s25_composition_resync_full_reset` — Full reset + recomposition

## Risks

- **Per-cell getrange (`GetRange_Component`)** still operates independently for bound cells. This is correct: per-cell getrange is scoped to the cell, not the active stream. S26 could unify this if needed.
- **`getrange.subject_id`** remains outside apply state — it's a request-scoped ID that doesn't belong to stream lifecycle. This is intentional.

## Recommended S26

- **Per-cell composition state**: Extend `GetRange_Component` with a cell-level composition stage, enabling bound cells to report their own historical/realtime coherence.
- **Telemetry HUD integration**: Display composition stage + per-artifact event counts in the debug overlay.
- **Composition-driven context_stage**: Replace the manual `context_stage` writes in event handlers with a derivation from `apply_state_composition_stage`.
