# Stage 23 -- Store Consolidation & Surface Boundaries

**Date:** 2026-03-06
**Status:** COMPLETE
**Tests:** 138 pass (127 existing + 11 new S23)
**Breaking changes:** Zero

---

## Executive Summary

Stage 23 wires the S22 canonical contracts (`artifact_policy`, `protocol_engine`, `Stream_Apply_State`) into the live runtime, establishing them as the single source of truth for protocol -> store -> surface state. The implementation adds `Stream_Apply_State` to every `Stream_View_Slot` and to `App_State` (as `active_apply_state`), replaces duplicate logic in `drain_marketdata` handlers with policy-driven calls, and introduces an adapter layer (`store_adapters.odin`) that syncs canonical state to the legacy `active_metrics` booleans each frame. Zero behavioral changes -- widgets continue reading `active_metrics` unchanged.

---

## Current-State Audit (Pre-S23)

### Duplicated State (75 occurrences across 9 files)

| State | Location | Canonical Source (S22) |
|---|---|---|
| `has_live_stats` | `Active_Stream_Metrics` | `Stream_Apply_State.has_live[.Stats]` |
| `has_live_candle` | `Active_Stream_Metrics` | `Stream_Apply_State.has_live[.Candle]` |
| `has_live_heatmap` | `Active_Stream_Metrics` | `Stream_Apply_State.has_live[.Heatmap]` |
| `has_live_vpvr` | `Active_Stream_Metrics` | `Stream_Apply_State.has_live[.VPVR]` |
| `orderbook_snapshot_seen` | `Stream_View_Slot` | `Stream_Apply_State.snapshot_seen[.Orderbook]` |
| `has_heatmap_snapshot` | `Stream_View_Slot` | `Stream_Apply_State.has_live[.Heatmap]` |
| `has_live_vpvr` | `Stream_View_Slot` | `Stream_Apply_State.has_live[.VPVR]` |
| `synth_heatmap_last_window` | `App_State` | `Stream_Apply_State.synth_heatmap_last_window` |

### Duplicated Logic

| Logic | Location | Canonical Source (S22) |
|---|---|---|
| Orderbook snapshot gate | `orderbook_snapshot_gate` in `marketdata.odin` | `snapshot_gate_check` in `md_common` |
| Backpressure skip check | `should_skip_event_by_backpressure_policy` | `should_skip_by_bp_policy` in `md_common` |
| Synthetic fallback check | `!state.active_metrics.has_live_stats` | `apply_state_should_use_synthetic(s, .Stats)` |

### Ad-Hoc Reset Blocks (6 locations)

1. `drain_marketdata` reconnect block
2. `apply_cycle_stream_action` (Tab)
3. `apply_set_timeframe_action` (TF change)
4. `apply_pick_stream_action` (Pick_Stream)
5. `apply_resync_active_stream_action` (manual resync)
6. `drain_marketdata` pending_active_subject

All 6 manually clear the same 4-8 boolean fields.

---

## S23 Target Boundaries

### Ownership Model

| State Category | Owner | Access Pattern |
|---|---|---|
| **Global market state** | `App_State.active_apply_state` | Written by `drain_marketdata` handlers; read via `apply_state_sync_to_metrics` |
| **Per-stream state** | `Stream_View_Slot.apply_state` | Written by `drain_marketdata` handlers per event |
| **Per-surface/widget state** | `active_metrics` (read-only for widgets) | Synced from `active_apply_state` each frame |
| **Derived/transitional** | `candle_health`, `lifecycle`, `context_stage` | Computed from canonical state |

### Boundary Rules

1. **Widgets NEVER read `Stream_Apply_State` directly** -- they read `active_metrics`
2. **`active_metrics.has_live_*` are derived**, not primary -- set only by `apply_state_sync_to_metrics`
3. **Reset/reconnect/TF-change logic is policy-driven** -- calls `apply_state_on_{reconnect,tf_change}`, not ad-hoc field clearing
4. **Snapshot gate is canonical** -- `snapshot_gate_check` from `md_common`, not `orderbook_snapshot_gate`
5. **Backpressure is canonical** -- `should_skip_by_bp_policy` from `md_common`

---

## Minimal Correct Implementation

### New Files

- `client/src/core/app/store_adapters.odin` -- Adapter layer: `apply_state_sync_to_metrics`, `reset_active_apply_state`, `reconnect_active_apply_state`, `tf_change_active_apply_state`, `sync_active_apply_state_from_slot`
- `client/src/core/md_common/store_boundary_test.odin` -- 11 S23-specific integration tests

### Modified Files

| File | Changes |
|---|---|
| `app.odin` | Added `apply_state` to `Stream_View_Slot`, `active_apply_state` to `App_State` |
| `marketdata.odin` | Wired `apply_state_mark_event` into all 10 event handlers; replaced `orderbook_snapshot_gate` with `snapshot_gate_check`; replaced ad-hoc backpressure check with `should_skip_by_bp_policy`; added `apply_state_sync_to_metrics` at end of `drain_marketdata`; added `reconnect_active_apply_state` in reconnect block |
| `stream_views.odin` | Replaced manual `has_live_*` resets with `sync_active_apply_state_from_slot` / `reset_active_apply_state` / `tf_change_active_apply_state`; added per-slot `apply_state_on_tf_change` |
| `actions_stream_control.odin` | Added `sync_active_apply_state_from_slot` on Pick_Stream; `reset_active_apply_state` on resync |
| `layer_marketdata.odin` | Syncs `active_apply_state.has_live[]` from layer store counts |

### What Was NOT Changed (intentionally)

- `Stream_View_Slot.orderbook_snapshot_seen` -- kept for now; `apply_state.snapshot_seen[.Orderbook]` shadows it. Remove in S24.
- `Stream_View_Slot.has_heatmap_snapshot` / `has_live_vpvr` -- kept for now; shadowed by `apply_state`. Remove in S24.
- `App_State.synth_heatmap_last_window` -- kept as legacy sync target. Remove in S24.
- `Active_Stream_Metrics.has_live_*` -- kept as read targets for widgets. Replace with `apply_state_summary` in S24.
- Widget code in `build_ui.odin` -- reads `active_metrics` unchanged. Widgets never interpret protocol.

---

## Tests

### New S23 Tests (11)

| Test | What it proves |
|---|---|
| `test_s23_per_slot_apply_state_tracks_all_artifacts` | All 9 artifact kinds tracked correctly |
| `test_s23_reconnect_clears_only_gated_artifacts` | Policy-driven reconnect (only OB gate clears) |
| `test_s23_tf_change_clears_only_tf_sensitive` | Policy-driven TF change (only candle/heatmap/vpvr clear) |
| `test_s23_synthetic_fallback_follows_policy` | Synthetic fallback exactly matches `has_synthetic_fallback` policy |
| `test_s23_summary_matches_apply_state` | Summary adapter produces correct booleans |
| `test_s23_bp_policy_canonical_matches_artifact_table` | Backpressure skip matches `BP_Priority` policy |
| `test_s23_snapshot_gate_canonical_for_all_artifacts` | Only orderbook has gate; accepts bootstrap delta |
| `test_s23_full_lifecycle_protocol_and_apply_state` | Full subscribe->snapshot->live->TF->reconnect->re-sub |
| `test_s23_getrange_lifecycle_in_apply_state` | GetRange send/complete/timeout/TF-clear cycle |
| `test_s23_stale_detection_policy_alignment` | Stale detection enum matches policy table |
| `test_s23_policy_table_invariants` | Cross-field consistency (gate->reconnect, TF->reset, range->TF) |

### Existing Tests

All 127 existing md_common tests pass unchanged (protocol_engine, artifact_policy, snapshot_gate, stream_apply_state, backpressure).

---

## Risks

| Risk | Mitigation |
|---|---|
| Dual writes (legacy booleans + apply_state) | Additive: apply_state writes shadow, don't replace. `apply_state_sync_to_metrics` is the canonical bridge. |
| Legacy `has_live_*` readers see stale data | Sync runs every frame at end of `drain_marketdata`. No frame delay possible. |
| Per-slot `apply_state` doubles slot memory | 10 * (bool + bool + bool + i64) + getrange fields = ~200 bytes per slot. 32 slots = ~6.4KB. Negligible. |

---

## Recommended S24: Legacy Removal Cutover

1. **Remove `Active_Stream_Metrics.has_live_{stats,candle,heatmap,vpvr}`** -- replace all readers with `apply_state_summary()` or direct `active_apply_state.has_live[]` access.
2. **Remove `Stream_View_Slot.orderbook_snapshot_seen`** -- use `slot.apply_state.snapshot_seen[.Orderbook]`.
3. **Remove `Stream_View_Slot.has_heatmap_snapshot` / `has_live_vpvr`** -- use `slot.apply_state.has_live[.Heatmap/.VPVR]`.
4. **Remove `App_State.synth_heatmap_last_window`** -- use `active_apply_state.synth_heatmap_last_window`.
5. **Remove `orderbook_snapshot_gate` proc** -- fully replaced by `snapshot_gate_check`.
6. **Remove `reset_active_stream_live_metrics`** -- fully replaced by `reset_active_apply_state`.
7. **Remove `actions_stream_state_helpers.odin`** -- empty file after removal.
8. **Replace `build_ui.odin` LIVE/SYNTH badges** with `apply_state_summary()`.
