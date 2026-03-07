# Stage 33 -- Runtime Ownership Cutover

**Date:** 2026-03-07
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

## Executive Summary

Stage 33 completes the runtime ownership cutover by converging the last documented gap from S32: the dual `Candle_Health` / `Artifact_Staleness` timing system. The ad-hoc `candle_last_recv_local_ms` field (7 scattered writes across 5 files) is replaced by `apply_state_candle_recv_ms`, a pure query on canonical apply state timing. This eliminates the last non-canonical timing path and establishes `Stream_Apply_State` as the sole timing authority for all artifact health.

**Impact:** 1 field removed, 7 ad-hoc writes eliminated, 1 pure query added, 7 new tests, 0 wire changes, 0 regressions.

## Ownership Audit

### Pre-S33 State (post-S32)

| Category | Status |
|----------|--------|
| `has_live_*` in `Active_Stream_Metrics` | CANONICAL -- sole writer: `apply_state_sync_to_metrics` |
| `context_stage` (Composition_Stage) | CANONICAL -- derived via adapter |
| `last_stats_ts_ms`, `last_orderbook_ts_ms` | CANONICAL -- synced from apply_state (S32) |
| `getrange.*` globals | CANONICAL -- sole writer: `apply_state_sync_to_getrange` |
| `snapshot_seen`, `has_live` per slot | CANONICAL -- via `apply_state_mark_event` + gate |
| Reconnect/TF/reset paths | CANONICAL -- all flow through adapters |
| `actions_stream_state_helpers.odin` | DELETED (S24) |
| **`candle_last_recv_local_ms`** | **LEGACY -- 7 ad-hoc writes, parallel to apply_state** |
| **`Candle_Health` timing** | **LEGACY -- read from ad-hoc field, not canonical source** |

### Post-S33 State

| Category | Status |
|----------|--------|
| All of the above | CANONICAL (unchanged) |
| `candle_last_recv_local_ms` | **REMOVED** -- replaced by `apply_state_candle_recv_ms` |
| `Candle_Health` timing | **CANONICAL** -- reads from apply_state via pure query |

### What Was NOT Changed (intentional)

| Pattern | Rationale |
|---------|-----------|
| `candle_health` field in `App_State` | Derived each frame by `observe_candle_health` -- same pattern as S22-S31 |
| `candle_health = .No_Data` manual resets | Provide immediate feedback on stream switch/TF change; observer confirms on next frame |
| `active_metrics.state` / `desync_reason` | Transport-level concerns from `Stream_Controller`, not artifact-level |
| Direct reads of `slot.apply_state.has_live[*]` in store resolution | Store resolution layer reads canonical source, provides derived data to widgets |
| Direct writes to `apply_state.getrange_oldest_ts` in batch loop | Hot-path optimization (per-candle in batch), immediately synced |
| Direct write to `apply_state.synth_heatmap_last_window` | Single-writer dedup tracking, canonical location |
| `Candle_Health` enum (No_Data/OK/Lagging/Stale) | UI-facing representation stays; semantics unchanged |

## Runtime Cutover Plan

### Decision: Canonical vs Derived

| State | Classification | Rationale |
|-------|---------------|-----------|
| `Stream_Apply_State` | **CANONICAL** | Single source of truth for all artifact state |
| `Active_Stream_Metrics.has_live_*` | **DERIVED** | Synced from apply_state via adapter |
| `Active_Stream_Metrics.context_stage` | **DERIVED** | Computed from apply_state via pure function |
| `GetRange_Global_State` | **DERIVED** | Synced from apply_state via adapter |
| `candle_health` | **DERIVED** | Computed from apply_state + candle store each frame |
| `Candle_Health` timing | **DERIVED** | `apply_state_candle_recv_ms` query on canonical timing |
| `Aggregate_Health_Summary` | **DERIVED** | Pure function over all slot apply_states |
| `Apply_State_Telemetry` | **DERIVED** | Snapshot query for diagnostics |

### Widget Protocol Isolation

No widget directly interprets protocol state. The data flow is:

```
[Protocol Engine] -> Stream_Apply_State (canonical)
                  -> apply_state_sync_to_metrics (adapter)
                  -> Active_Stream_Metrics (derived)
                  -> widgets read metrics

[Store Resolution] reads slot.apply_state.has_live[*] (canonical)
                -> provides resolved stores + liveness to widgets
                -> widgets never see apply_state directly
```

## Code Changes

### New: `apply_state_candle_recv_ms` (stream_apply_state.odin)

Pure query returning `max(last_recv_ms[.Candle], last_recv_ms[.Range_Candle])`.
Converges live candle timing + historical range timing into single canonical source.

### Modified: `compute_candle_health` + `build_candle_health_ui` (health.odin)

Rebased from `state.candle_last_recv_local_ms` to `md_common.apply_state_candle_recv_ms(state.active_apply_state)`.

### Removed: `candle_last_recv_local_ms` (7 write sites)

| File | Line | Context |
|------|------|---------|
| `app.odin:391` | Field declaration in `App_State` | Removed |
| `marketdata.odin:289` | Trade event handler | Removed |
| `marketdata.odin:495` | Candle event handler | Removed |
| `marketdata.odin:619` | GetRange completion | Removed |
| `stream_views.odin:212` | Stream switch (cycle) | Removed |
| `stream_views.odin:345` | TF change (global) | Removed |
| `actions_stream_control.odin:22` | Pick stream | Removed |
| `actions_stream_control.odin:46` | Resync | Removed |
| `layer_marketdata.odin:75` | Layer drain | Removed |

## Tests

7 new tests in `protocol_engine_test.odin`:

| Test | Validates |
|------|-----------|
| `test_apply_state_candle_recv_ms_zero` | Returns 0 for fresh state |
| `test_apply_state_candle_recv_ms_live_only` | Returns live candle timestamp |
| `test_apply_state_candle_recv_ms_range_only` | Returns range candle timestamp |
| `test_apply_state_candle_recv_ms_live_wins` | Returns max when live > range |
| `test_apply_state_candle_recv_ms_range_wins` | Returns max when range > live |
| `test_apply_state_candle_recv_ms_tf_change_clears_both` | Both cleared on TF change (both TF-sensitive) |
| `test_apply_state_candle_recv_ms_reset_zeros` | Full reset zeros both |

**Total tests in md_common: 207** (200 from S32 + 7 new)

## Risks

| Risk | Mitigation |
|------|------------|
| Candle health badge flash on GetRange-to-live transition | `compute_candle_health_for_store` checks `store.count <= 0` first (returns No_Data), and `apply_state_mark_event(.Range_Candle)` updates timing before sync |
| Trade events no longer update candle timing | Correct: if candle pipeline is stale but trades flow, badge correctly shows staleness instead of masking it |
| Manual `candle_health = .No_Data` resets on TF/switch still present | Intentional: provides immediate feedback; observer confirms on next frame render |

## Recommended S34

With all timing, liveness, composition, staleness, recovery, and health now flowing through `Stream_Apply_State`, the protocol ownership cutover is complete. Recommended next stages:

1. **S34: Clean up S33 tombstone comments** -- Remove the `// S33: candle_last_recv_local_ms removed` comments across 9 files (cosmetic)
2. **S34: Candle_Health / Artifact_Staleness enum convergence** -- Map `Candle_Health` values to `Artifact_Staleness` (No_Data=Unknown, OK=Fresh, Lagging=Aging, Stale=Stale), potentially unifying the enum
3. **S34: Per-cell candle health** -- Derive `Candle_Health` per cell using `build_candle_health_ui_for_store` + cell slot's `apply_state_candle_recv_ms`
4. **S35: Apply state write linting** -- Odin doesn't have visibility modifiers, but a CI grep guard can prevent new direct writes to `active_metrics.has_live_*` outside `store_adapters.odin`
