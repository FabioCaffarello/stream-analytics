# Stage 24 -- Legacy Removal Cutover Report

**Date:** 2026-03-06
**Branch:** `codex/s9-legacy-removal-cutover`
**Status:** COMPLETE

---

## Executive Summary

S24 completes the cutover from scattered legacy boolean fields to the canonical
`Stream_Apply_State` architecture introduced in S22/S23. All legacy state
duplication is eliminated. `artifact_policy`, `protocol_engine`, and
`Stream_Apply_State` are now the **sole source of truth** for snapshot gates,
live data flags, synthetic fallback, and synth heatmap dedup.

**Impact:** 4 struct fields removed, 1 proc deleted, 1 file deleted, 13 legacy
writes eliminated, 5 cutover tests added. Zero behavioral regression, zero wire
contract change.

---

## Legacy Audit

| Legacy Field / Proc | Writers (S23) | Readers | Canonical Replacement | S24 Action |
|---|---|---|---|---|
| `Stream_View_Slot.orderbook_snapshot_seen` | handle_orderbook_event, reconnect, TF change | snapshot_gate_check | `slot.apply_state.snapshot_seen[.Orderbook]` | **REMOVED** |
| `Stream_View_Slot.has_heatmap_snapshot` | handle_heatmap_event, TF change | synth heatmap guard, sync_active, resolve_stores | `slot.apply_state.has_live[.Heatmap]` | **REMOVED** |
| `Stream_View_Slot.has_live_vpvr` | handle_vpvr_event, TF change | resolve_stores | `slot.apply_state.has_live[.VPVR]` | **REMOVED** |
| `App_State.synth_heatmap_last_window` | handle_orderbook (legacy sync), cycle, TF, resync | (only writers/mirror) | `active_apply_state.synth_heatmap_last_window` | **REMOVED** |
| `Active_Stream_Metrics.has_live_{stats,heatmap,vpvr,candle}` | event handlers (4 writes) | build_ui, app perf export, resolve_stores | `apply_state_sync_to_metrics` (adapter) | Legacy writes removed; fields kept for readers |
| `reset_active_stream_live_metrics` proc | pick_stream, resync, disconnect | -- | `reset_active_apply_state` | **DELETED** |
| `orderbook_snapshot_gate` proc | (dead -- replaced by `md_common.snapshot_gate_check` in S23) | 3 tests | -- | **DELETED** + tests removed |
| `actions_stream_state_helpers.odin` | contained only `reset_active_stream_live_metrics` | -- | -- | **FILE DELETED** |

---

## S24 Target Cutover

### Before S24

```
event handler -> writes legacy bool on slot (has_heatmap_snapshot, etc.)
              -> writes legacy bool on active_metrics (has_live_heatmap, etc.)
              -> writes apply_state (canonical)
end-of-frame  -> apply_state_sync_to_metrics overwrites active_metrics
```

Two writers racing: event handlers AND adapter. Legacy booleans on slots existed
alongside `apply_state` arrays.

### After S24

```
event handler -> writes apply_state ONLY (canonical)
end-of-frame  -> apply_state_sync_to_metrics -> active_metrics (sole writer)
```

Single writer per field. Slot booleans eliminated. `Active_Stream_Metrics`
has_live fields are now **derived** (read-only from widget perspective).

---

## Minimal Correct Implementation

### Struct Changes

**`Stream_View_Slot`** -- 3 fields removed:
- `orderbook_snapshot_seen: bool` -- replaced by `apply_state.snapshot_seen[.Orderbook]`
- `has_heatmap_snapshot: bool` -- replaced by `apply_state.has_live[.Heatmap]`
- `has_live_vpvr: bool` -- replaced by `apply_state.has_live[.VPVR]`
- `heatmap_snapshot: Heatmap_Snapshot` -- **KEPT** (actual data, not a flag)

**`App_State`** -- 1 field removed:
- `synth_heatmap_last_window: i64` -- replaced by `active_apply_state.synth_heatmap_last_window`

### Code Changes

| File | Change | Lines |
|---|---|---|
| `marketdata.odin` | Remove 4 legacy `has_live_*` writes in event handlers | -4 |
| `marketdata.odin` | Migrate `orderbook_snapshot_seen` -> `apply_state.snapshot_seen[.Orderbook]` | ~6 |
| `marketdata.odin` | Migrate `has_heatmap_snapshot` -> `apply_state.has_live[.Heatmap]` | ~3 |
| `marketdata.odin` | Remove `has_live_vpvr` slot write | -1 |
| `marketdata.odin` | Remove `synth_heatmap_last_window` legacy sync | -1 |
| `marketdata.odin` | Remove reconnect `orderbook_snapshot_seen = false` | -1 |
| `marketdata.odin` | Delete `orderbook_snapshot_gate` dead proc | -15 |
| `stream_views.odin` | Remove `has_heatmap_snapshot`, `has_live_vpvr`, `orderbook_snapshot_seen` writes in TF paths | -6 |
| `stream_views.odin` | Remove `synth_heatmap_last_window = 0` writes | -2 |
| `stream_views.odin` | Migrate `has_heatmap_snapshot` read in sync_active | ~1 |
| `stream_slots.odin` | Migrate `has_heatmap_snapshot` -> `apply_state.has_live[.Heatmap]` | ~1 |
| `stream_slots.odin` | Migrate `has_live_vpvr` -> `apply_state.has_live[.VPVR]` | ~1 |
| `actions_stream_control.odin` | Remove `reset_active_stream_live_metrics` + `synth_heatmap_last_window = 0` | -3 |
| `actions_profiles.odin` | Replace `reset_active_stream_live_metrics` -> `reset_active_apply_state` | ~1 |
| `layer_marketdata.odin` | Replace direct metrics writes with apply_state + sync | ~6 |
| `app.odin` | Remove 4 struct fields | -4 |
| `store_adapters.odin` | Update comments (S23 -> S24) | ~6 |
| `actions_stream_state_helpers.odin` | **DELETED** | -13 |
| `marketdata_test.odin` | Remove 3 `orderbook_snapshot_gate` tests | -21 |
| `store_boundary_test.odin` | Add 5 S24 cutover tests | +73 |

### Tests

- **143 md_common tests** (up from 138): all pass
- **1 app test**: passes
- **10 core packages**: all type-check clean
- **5 new S24 tests:**
  - `test_s24_orderbook_snapshot_via_apply_state_reconnect`
  - `test_s24_heatmap_live_via_apply_state_tf_change`
  - `test_s24_vpvr_live_via_apply_state_tf_change`
  - `test_s24_synth_heatmap_window_lifecycle`
  - `test_s24_summary_adapter_after_cutover`

---

## Risks

| Risk | Mitigation |
|---|---|
| Widget reads stale `active_metrics.has_live_*` for 1 frame | `apply_state_sync_to_metrics` runs at end of every drain -- max 1-frame lag, same as S23 |
| Layer path had direct metric writes | Migrated to apply_state + sync; behaviorally identical |
| `heatmap_snapshot` data field still on slot | Intentional: it holds actual snapshot data, not a boolean flag |
| `Active_Stream_Metrics.has_live_*` fields still exist | Intentional: they are the **read** interface for widgets; only the adapter writes them |

---

## Recommended S25

1. **Observability dashboard:** Add apply_state event_count and per-artifact last_recv_ms to telemetry HUD
2. **Cell-level apply state:** Extend per-cell getrange tracking to use `Stream_Apply_State` instead of ad-hoc `Cell_GetRange_State`
3. **Layer path full integration:** Move layer_marketdata to use `apply_state_mark_event` per artifact instead of count-based inference
4. **Dead code sweep:** Audit `Active_Stream_Metrics` for fields that are now only written by the adapter and could be inlined
