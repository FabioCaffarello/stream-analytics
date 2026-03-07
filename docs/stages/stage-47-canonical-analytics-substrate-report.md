# Stage 47 — Canonical Analytics Substrate

**Date:** 2026-03-07
**Status:** COMPLETE
**Tests:** 430 (350 md_common + 80 services)

## Summary

Hardened the backend→client contract for analytics streams (open_interest, delta_volume, cvd, bar_stats) by giving each stream its own semantic identity throughout the entire pipeline. Previously, all 4 analytics streams were collapsed into generic `.Stats` or `.Tape` parse results, losing field fidelity. S47 eliminates this identity collapse.

## Changes

### 1. Artifact Identity (artifact_policy.odin)
- Extended `Artifact_Kind` enum from 10 → 14 values: `Open_Interest`, `Delta_Volume`, `CVD`, `Bar_Stats`
- Added `Sparse_Adaptive` stale detection (60s aging / 180s stale) for irregular feeds like OI
- 4 new artifact policies with correct semantics:
  - OI: Latest_Wins, not TF-sensitive, Sparse_Adaptive, Degradable
  - DV/CVD/BS: Ring_Append, TF-sensitive, TF_Adaptive, Degradable, reset_on_tf_change

### 2. Analytics Store (analytics_store.odin — NEW)
- Ring buffer store (cap 64) with flat `Analytics_Entry` struct
- `[8]f64` value slots per kind, documented slot indices
- Zero-allocation push/get/latest/count/clear operations
- Per-slot + global stores wired into App_State

### 3. Parser Routing (message_parser.odin, message_parser_frames.odin, message_parser_batch.odin)
- 4 new `Parse_Result_Kind` variants: `Open_Interest`, `Delta_Volume`, `CVD`, `Bar_Stats`
- 4 new parsed structs with full field fidelity (no more field cramming)
- `aggregation.oi` → `.Open_Interest`, `aggregation.delta_volume` → `.Delta_Volume`, etc.
- `marketdata.open_interest` (raw tick) intentionally kept as `.Stats`
- Batch parser updated with same routing

### 4. Port Events (ports/marketdata.odin)
- 4 new `MD_Event_Kind` variants + event structs + `MD_Event_Data` union members

### 5. Drain Handlers (app/marketdata.odin)
- 4 new handler functions: `handle_open_interest_event`, `handle_delta_volume_event`, `handle_cvd_event`, `handle_bar_stats_event`
- Each: record_stream_event → apply_state_mark_event → push_analytics (slot + global)

### 6. Platform Staging
- Native: 8 staging fields (4 dirty flags + 4 staging buffers), poll emissions wired
- Web: passthrough cases added (staging deferred)

### 7. Surface Contract + Snapshot
- `Cell_Stores` extended with `analytics` pointer
- `resolve_stores_for_cell` wires analytics from slot or global
- All `[Artifact_Kind]` arrays auto-expand to 14 elements
- Snapshot serialization uses `u16` bitmasks (supports 16 bits, using 14)
- `parse_result_has_data` and `parse_result_requires_ts_server` updated

### 8. Apply State Integration
- `Sparse_Adaptive` case added to `apply_state_artifact_staleness`
- TF-change store clearing added to all 3 code paths
- All existing surface/health queries automatically cover analytics via enum iteration

### 9. Ancillary
- `market_store.odin` switch extended with analytics passthrough
- Pre-existing tape test seq assertion fixed (envelope seq overrides payload Seq)

## Design Decisions

1. **Sparse_Adaptive vs Dual_Silence for OI**: OI updates can be 1/min; Dual_Silence (12s) would false-positive. 60s/180s thresholds match actual OI update cadence.
2. **Raw OI tick kept as Stats**: `marketdata.open_interest` is a raw exchange tick (mark_price-like), distinct from the windowed `aggregation.oi` which gets the new `.Open_Interest` kind.
3. **Analytics not channel-specific**: Analytics events arrive on various channel slots. `Cell_Stores.analytics` resolves from the bound slot, not from a specific MD_Channel.
4. **Web platform deferred**: Web staging added as passthrough; full poll/emit can be added when web analytics widgets are needed.

## Test Coverage (8 new S47 tests in md_common)
- `test_s47_analytics_artifact_kinds_exist` — 14 enum values
- `test_s47_analytics_policies_correct` — all 4 policy contracts
- `test_s47_analytics_apply_state_tracking` — mark + live + count
- `test_s47_sparse_adaptive_staleness` — 30s/90s/200s thresholds
- `test_s47_analytics_tf_change_resets_tf_sensitive` — OI survives, DV/CVD/BS cleared
- `test_s47_analytics_backpressure_degradable` — all 4 Degradable
- `test_s47_apply_state_arrays_cover_14_artifacts` — array size
- `test_s47_analytics_stale_count_includes_analytics` — stale count coverage

## Files Modified (17)
- `client/src/core/md_common/artifact_policy.odin`
- `client/src/core/md_common/stream_apply_state.odin`
- `client/src/core/md_common/md_common.odin`
- `client/src/core/md_common/store_boundary_test.odin`
- `client/src/core/ports/marketdata.odin`
- `client/src/core/services/message_parser.odin`
- `client/src/core/services/message_parser_frames.odin`
- `client/src/core/services/message_parser_batch.odin`
- `client/src/core/services/message_parser_test.odin`
- `client/src/core/app/app.odin`
- `client/src/core/app/components.odin`
- `client/src/core/app/marketdata.odin`
- `client/src/core/app/stream_views.odin`
- `client/src/core/app/stream_slots.odin`
- `client/src/core/layers/market_store.odin`
- `client/src/platform/native/marketdata_native.odin`
- `client/src/platform/web/marketdata_web.odin`

## Files Created (1)
- `client/src/core/services/analytics_store.odin`

## Wire Protocol Changes
None. All changes are client-side parse/store/surface.

## Breaking Changes
None. Existing `.Stats` and `.Tape` flows unchanged for their original channels.
