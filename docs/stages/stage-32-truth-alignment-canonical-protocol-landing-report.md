# Stage 32 — Truth Alignment & Canonical Protocol Landing

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

## Executive Summary

Stage 32 reconciles the gap between the architecture described in S22-S31 stage reports and the actual codebase, establishing a single canonical truth for protocol state ownership. The audit identified 3 ad-hoc timing field writes that bypassed the `apply_state → adapter → active_metrics` pipeline established in S24. These were eliminated, making `apply_state_sync_to_metrics` the sole writer of per-artifact timing fields in `Active_Stream_Metrics`, completing the ownership contract that S24 began.

**Impact:** 4 direct metric writes removed, 0 new state fields, 0 wire changes, 206 tests passing (6 new).

## Truth Audit

### Fully Aligned (confirmed correct)

| Claim | Status |
|-------|--------|
| `actions_stream_state_helpers.odin` deleted (S24) | CONFIRMED — file does not exist |
| `Stream_View_Slot` has no legacy booleans | CONFIRMED — only `apply_state` field |
| `apply_state_sync_to_metrics` sole writer of `has_live_*` | CONFIRMED |
| `Composition_Stage` replaces `Context_Stage` | CONFIRMED |
| `synth_heatmap_last_window` moved to `Stream_Apply_State` | CONFIRMED |
| Recovery/staleness/aggregate health — pure functions | CONFIRMED |
| All 10 event handlers call `apply_state_mark_event` | CONFIRMED |

### Gaps Fixed in S32

| Gap | Location | Resolution |
|-----|----------|------------|
| Direct write `active_metrics.last_orderbook_ts_ms = now_ms` | `marketdata.odin:364` | Removed — adapter syncs from `last_recv_ms[.Orderbook]` |
| Direct write `active_metrics.last_stats_ts_ms = now_ms` | `marketdata.odin:404` | Removed — adapter syncs from `last_recv_ms[.Stats]` |
| Manual zeroing of timing in `reset_active_apply_state` | `store_adapters.odin:46-47` | Removed — `apply_state_reset` zeros apply_state, adapter propagates |
| Manual zeroing of timing in `tf_change_active_apply_state` | `store_adapters.odin:62-63` | Removed — `apply_state_on_tf_change` zeros TF-sensitive, adapter propagates |
| Layer path direct metric writes | `layer_marketdata.odin:65-66` | Replaced — timing now flows through `apply_state.last_recv_ms` before sync |

### Documented Divergences (intentional, not gaps)

| Pattern | Rationale | Owner |
|---------|-----------|-------|
| `candle_last_recv_local_ms` in `App_State` | Tracks feed liveness (trades + candles), distinct from `last_recv_ms[.Candle]` which tracks candle events only. Used by `Candle_Health` for "is anything flowing?" detection | `App_State` (direct writes) |
| `Candle_Health` enum parallel to `Artifact_Staleness` | `Candle_Health` (No_Data/OK/Lagging/Stale) serves UI badge display with TF-adaptive lag thresholds. `Artifact_Staleness` serves protocol-level recovery decisions. Same thresholds, different consumers | Convergence deferred to S33 |
| `active_metrics.state` / `desync_reason` from stream controller | Stream state comes from `Stream_Controller`, not from `apply_state`. These are transport-level concerns, not artifact-level | `Stream_Controller` → `active_metrics` (intentional) |

## Canonical Target Architecture

### State Ownership Map (S32 final)

```
[Transport Layer]
  └─ MD_Runtime_Metrics ──────────────┐
                                      ▼
[Stream Controller]             [active_metrics]
  └─ Stream_State ─────────────►  .state
  └─ desync_reason ─────────────►  .desync_reason
  └─ rtt_ms, lag_ms ───────────►  .rtt_ms, .lag_ms
                                      ▲
[Protocol Engine]                     │
  └─ Stream_Apply_State ──────────────┤ (via apply_state_sync_to_metrics)
       .has_live[*]          ────────►  .has_live_*
       .last_recv_ms[.Stats] ────────►  .last_stats_ts_ms
       .last_recv_ms[.OB]   ────────►  .last_orderbook_ts_ms
       .composition_stage()  ────────►  .context_stage
       .getrange_*           ────────►  GetRange_Global_State (via sync_to_getrange)
```

### Write Contracts

| Field | Sole Writer | When |
|-------|-------------|------|
| `has_live_{stats,candle,heatmap,vpvr}` | `apply_state_sync_to_metrics` | End of each frame |
| `last_stats_ts_ms` | `apply_state_sync_to_metrics` | End of each frame |
| `last_orderbook_ts_ms` | `apply_state_sync_to_metrics` | End of each frame |
| `context_stage` | `apply_state_sync_to_metrics` | End of each frame |
| `getrange.{pending,seeded,oldest_ts,...}` | `apply_state_sync_to_getrange` | End of each frame |
| `active_metrics.state` | `health.odin` + `record_stream_event` | Per-event + per-metrics-sample |
| `candle_last_recv_local_ms` | `handle_{trade,candle}_event` + stream switch + TF change | Per-event |

## Code Changes

### Modified Files

1. **`store_adapters.odin`** — Extended `apply_state_sync_to_metrics` to sync `last_recv_ms[.Stats]` → `last_stats_ts_ms` and `last_recv_ms[.Orderbook]` → `last_orderbook_ts_ms`. Removed manual zeroing from `reset_active_apply_state` and `tf_change_active_apply_state`.

2. **`marketdata.odin`** — Removed 2 direct writes: `active_metrics.last_orderbook_ts_ms` in `handle_orderbook_event` and `active_metrics.last_stats_ts_ms` in `handle_stats_event`.

3. **`layer_marketdata.odin`** — Replaced direct metric writes with `apply_state.last_recv_ms` population before `sync_all`, letting the adapter propagate timing uniformly.

4. **`store_boundary_test.odin`** — 6 new S32 tests proving timing flows through apply_state, resets clear properly, TF change affects only TF-sensitive artifacts, and telemetry snapshot includes timing.

### Lines Changed
- 4 direct metric writes removed
- 2 adapter sync lines added
- 4 manual reset lines removed
- 6 tests added (206 total in md_common)

## Tests

| Test | Proves |
|------|--------|
| `test_s32_apply_state_timing_flows_through_mark_event` | `mark_event` populates `last_recv_ms` for Stats and Orderbook |
| `test_s32_apply_state_reset_clears_timing` | `apply_state_reset` zeros all timing |
| `test_s32_apply_state_reconnect_preserves_timing` | Reconnect does NOT clear timing (per policy) |
| `test_s32_apply_state_tf_change_clears_tf_sensitive_timing` | TF change clears Candle/Heatmap/VPVR timing, preserves Stats/Trade |
| `test_s32_summary_includes_timing` | Telemetry snapshot includes all timing |
| `test_s32_timing_monotonic_updates` | Multiple events update to latest timestamp |

**Result:** 206/206 tests passing.

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Layer path timing changed from direct-write to apply_state flow | Low | Same values, just routed through canonical pipeline; behavior-preserving |
| `candle_last_recv_local_ms` remains direct-write | None | Intentional — tracks feed liveness, not artifact timing |
| `Candle_Health` / `Artifact_Staleness` dual system persists | Low | Same thresholds, different consumers. Convergence is S33 scope |

## Recommended S33

**Candle Health Convergence & Final Compat Layer Removal**

1. **Converge `Candle_Health` → `Artifact_Staleness`** — Replace the separate `Candle_Health` enum and `candle_last_recv_local_ms` tracking with a derived view from `Artifact_Staleness[.Candle]`. This requires:
   - Making `last_recv_ms[.Candle]` also update on trade events (when synthetic candles are active)
   - Updating all `Candle_Health` UI consumers to use `Artifact_Staleness`
   - Removing `candle_last_recv_local_ms` from `App_State`

2. **Activate `Stream_Protocol` in the drain path** — The `Stream_Protocol` state machine (protocol_engine.odin) is fully tested but not yet wired into the event drain. S33 should integrate it as the canonical per-stream protocol state, replacing the ad-hoc state tracking in `record_stream_event`.

3. **Remove `GetRange_Global_State` compat struct** — After S32, this is a pure derived view from `apply_state`. S33 can inline its fields or remove the struct entirely, reading directly from `active_apply_state.getrange_*`.
