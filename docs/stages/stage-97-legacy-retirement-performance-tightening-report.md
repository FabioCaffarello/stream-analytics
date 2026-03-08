# Stage 97 — Legacy Retirement & Performance Tightening

**Date:** 2026-03-08
**Status:** COMPLETE

## Objective

Consolidate the Market Raccoon client runtime by eliminating legacy code paths,
removing dead data pipelines, and adding performance instrumentation for dense
dashboard scenarios.

## Changes

### 1. Dead Drain Path Removal (~1,030 lines deleted)

**Discovery:** `drain_marketdata` (marketdata.odin) was completely dead code — never
called from app.odin. Only `drain_layer_marketdata` (layer_marketdata.odin) is the
active drain path. However, `drain_marketdata` contained essential side-effects
(reconnection, lifecycle, getrange timeout, lazy loading) that had never been
migrated to the layer path.

**Actions:**
- **Migrated 6 side-effects** to `drain_layer_marketdata`:
  1. Reconnection detection (connection state transitions, subscription reconciliation)
  2. GetRange timeout (300-frame timeout for global, per-cell, per-compare-pane)
  3. Lazy candle loading (scroll-triggered older candle fetching)
  4. Lifecycle derivation (per-frame `derive_lifecycle` state machine)
  5. Lazy re-resolution (unresolved cell bindings → stream view slots)
  6. Slot repair + seeding (invariant repair, initial candle range request)

- **Removed 30 dead procs** from marketdata.odin:
  - `drain_marketdata` (main dead function)
  - 16 `handle_*_event` / `push_*` procs
  - 12 `apply_*_to_store` / `build_*` helper procs
  - 2 file-private helpers (`event_unix_to_ms`, `record_stream_event`)

- **File**: `marketdata.odin` reduced from 1,123 → 95 lines (only `check_lazy_candle_loading` remains)

### 2. Legacy Transport & Compat Parser Removal

- **Deleted** `md_common/transport_legacy.odin` — `ALLOW_LEGACY_WS_DEFAULT` config + `legacy_switch_from_text` proc
- **Deleted** `services/message_parser_compat.odin` — 3 compat parsers (`parse_stats_flat_compat`, `parse_microstructure_evidence_legacy_compat`, `parse_signal_legacy_compat`)
- **Removed** associated tests from `md_common_test.odin` and `message_parser_test.odin`
- **Removed** `legacy_evidence_frames` and `legacy_signal_frames` from `Parse_Telemetry` struct + their increment lines

### 3. Legacy Metric Fields Removal (~50 lines across 8 files)

Removed 6 legacy tracking fields from the metrics pipeline:

| Field | Removed From |
|-------|-------------|
| `legacy_evidence_frames` | components.odin, ports/marketdata.odin, health.odin, app.odin |
| `legacy_signal_frames` | components.odin, ports/marketdata.odin, health.odin, app.odin |
| `legacy_evidence_rejected` | components.odin, ports/marketdata.odin, health.odin, app.odin |
| `legacy_signal_rejected` | components.odin, ports/marketdata.odin, health.odin, app.odin |
| `legacy_downgrade_count` | components.odin, ports/marketdata.odin, health.odin, build_status.odin |
| `legacy_connected_since_ms` | components.odin, ports/marketdata.odin, health.odin |

Also removed:
- 2 HUD display blocks in `build_status.odin` (legacy downgrade warnings)
- 5 exported web probe functions (`probe_md_legacy_*`)
- Platform metric builder assignments (web + native)

### 4. Analytics Collection Optimization

Replaced two-pass `analytics_collect_by_kind` with single-pass + in-place reverse:

| Metric | Before | After |
|--------|--------|-------|
| Ring buffer traversals | 2 (count + fill) | 1 (collect + reverse) |
| Compare mode (4 panes × 3 subplots) | 24 × 128 iterations | 24 × 64 iterations |
| Additional work | — | n/2 swaps (negligible) |

### 5. Frame Cost Probes

Added 3 per-frame counters to `Telemetry_State`:
- `subplot_count` — active subplots rendered
- `compare_pane_count` — compare panes rendered
- `layer_render_count` — total layer bundle render calls

Counters are:
- Reset to 0 at frame start (both `update` and `update_web`)
- Incremented in `layer_canvas.odin` (subplots + layer dispatch) and `build_compare.odin`
- Displayed in telemetry HUD as `SUB:N CMP:N LYR:N`

## Test Results

| Package | Tests | Status |
|---------|-------|--------|
| app | 34 | PASS |
| services | 186 | PASS |
| layers | 22 | PASS |
| md_common | 401 | PASS |
| streams | 16 | PASS |
| **Total** | **659** | **ALL PASS** |

Compile check: all 10 core packages OK.

## Impact Summary

| Metric | Before | After |
|--------|--------|-------|
| Dead code (marketdata.odin) | 1,028 lines | 0 lines |
| Legacy files | 2 files | 0 files |
| Legacy metric fields | 6 fields × 8 files | 0 |
| Analytics ring scans (worst case) | 2× per collect | 1× per collect |
| Frame cost visibility | drain/actions/render only | + subplot/compare/layer counters |
| Active drain path side-effects | Partial (missing reconnect, lifecycle, etc.) | Complete |

## Architecture Notes

### Data Flow (Post-S97)

```
WebSocket Events
    ↓
data_source_poll_and_apply()
    ↓
Market_Store (layer store)
    ├── Market_Stream.trades/orderbook/stats/heatmap/vpvr/candles/signals/analytics
    ↓
sync_legacy_stores_from_layer_store()
    ↓
Global_Stores (app-level mirrors for non-layer consumers)
    ├── trades, orderbook, stats, heatmap, vpvr, candle, signals
    └── (analytics NOT synced — layer store is sole source for analytics rendering)
```

### Known Remaining Legacy

1. **`sync_legacy_stores_from_layer_store`** — copies 7 store types each frame from layer store to `Global_Stores`. This exists because 18 files still read from `Global_Stores`. Full removal requires redirecting all readers to the layer store.

2. **`Global_Stores.analytics` / `slot.analytics_store`** — written by HTTP analytics range fetcher but never read for rendering (subplots read from `Market_Stream.analytics`). Potential data loss for historical analytics data.

3. **V1/V4 layout restore chain** — 3-tier migration in `layout_persist.odin`. Safe to remove once all users have migrated to V6 format.

4. **Platform-internal legacy counters** — `MD_Web_State` and `MD_Native_State` retain local `legacy_*` bookkeeping fields. No longer flows to port contract.

## Files Changed

### Deleted (2)
- `client/src/core/md_common/transport_legacy.odin`
- `client/src/core/services/message_parser_compat.odin`

### Modified (15)
- `client/src/core/app/layer_marketdata.odin` — migrated 6 side-effects
- `client/src/core/app/marketdata.odin` — removed 1,028 lines of dead code
- `client/src/core/app/components.odin` — removed 6 legacy fields, added 3 probe counters
- `client/src/core/app/app.odin` — removed legacy telemetry fields, added counter resets
- `client/src/core/app/health.odin` — removed legacy metric sync
- `client/src/core/app/build_status.odin` — removed legacy HUD blocks, added probe display
- `client/src/core/app/layer_canvas.odin` — added subplot + layer counter increments
- `client/src/core/app/build_compare.odin` — added compare pane counter increment
- `client/src/core/app/marketdata_test.odin` — unchanged (tests unaffected)
- `client/src/core/ports/marketdata.odin` — removed 6 legacy port fields
- `client/src/core/services/analytics_store.odin` — single-pass collection optimization
- `client/src/core/services/message_parser.odin` — removed legacy telemetry fields
- `client/src/core/services/message_parser_test.odin` — removed compat parser tests
- `client/src/core/md_common/md_common_test.odin` — removed legacy switch test
- `client/src/core/app/store_adapters.odin` — updated stale comments
- `client/src/core/app/stream_views.odin` — updated stale comments
- `client/src/core/md_common/lifecycle.odin` — updated stale comment
- `client/src/platform/web/marketdata_web.odin` — removed legacy metric assignments
- `client/src/platform/native/marketdata_native.odin` — removed legacy metric assignments
- `client/src/platform/web/main.odin` — removed legacy probe exports
