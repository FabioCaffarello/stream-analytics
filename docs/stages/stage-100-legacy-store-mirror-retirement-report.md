# Stage 100 — Legacy Store Mirror Retirement

**Date:** 2026-03-08
**Status:** COMPLETE
**Tests:** 627 (40 app + 401 md_common + 186 services) — all green

## Summary

Eliminated the `sync_legacy_stores_from_layer_store` frame-by-frame mirror mechanism and `sync_active_stream_view_to_global_stores` stream-switch mirror. The `layer_store` (Market_Store) is now the **sole canonical data source** for all stream-scoped stores (candle, trades, orderbook, heatmap, vpvr, stats, signals, analytics).

## What Changed

### Removed
- `sync_legacy_stores_from_layer_store()` — frame-by-frame copy from active Market_Stream to Global_Stores
- `sync_active_stream_view_to_global_stores()` — stream-switch copy from slot stores to Global_Stores
- 10 fields from `Global_Stores` struct: trades, orderbook, heatmap, vpvr, stats, candle, signals, analytics, session_vpvr, tpo
- Demo data fill to `state.stores.*` (now seeds layer_store directly via `market_store_seed_demo`)

### Added
- `sync_apply_state_from_active_stream()` — extracts the apply_state + evidence + metrics sync logic (was inside the removed mirror proc)
- `sync_active_stream_view_registry()` — slim replacement that only syncs stream_registry + resets DOM/footprint on switch
- 9 active stream helper procs: `active_candle_store`, `active_trades_store`, `active_orderbook_store`, `active_heatmap_store`, `active_vpvr_store`, `active_stats_store`, `active_signals_store`, `active_analytics_store`, plus count helpers `active_candle_count`, `active_heatmap_count`, `active_vpvr_count`

### Modified
- `resolve_stores_for_cell()` — default fallback now reads from `layers.market_store_active_stream()` instead of Global_Stores; session_vpvr/tpo resolve from active slot
- `Global_Stores` — slimmed to 3 fields: `dom`, `footprint`, `markets` (non-stream-scoped)
- 12 files updated to use active stream helpers instead of Global_Stores fields

## Files Changed

| File | Change |
|------|--------|
| `layer_marketdata.odin` | Replaced mirror with `sync_apply_state_from_active_stream`, added 12 active stream helpers |
| `stream_views.odin` | Replaced mirror with `sync_active_stream_view_registry`, demo path uses `market_store_seed_demo`, candle count via helpers |
| `stream_slots.odin` | `resolve_stores_for_cell` reads layer_store directly, analytics fallbacks use `active_analytics_store` |
| `components.odin` | `Global_Stores` reduced to dom + footprint + markets |
| `app.odin` | Demo init simplified, runtime snapshot reads from active stream, candle count via helper |
| `actions_stream_control.odin` | Candle count + registry sync updated |
| `top_bar.odin` | Price ticker reads from `active_candle_store` |
| `marketdata.odin` | Lazy loading uses `active_candle_store` |
| `health.odin` | Health checks use `active_candle_store` |
| `build_status.odin` | Synth badges use count helpers |
| `build_dashboard.odin` | SVPVR/TPO labels resolve via `resolve_stores_for_cell` |
| `analytics_range.odin` | Fallback uses `active_analytics_store` |
| `session_vpvr_data.odin` | Fallback resolves from active slot |
| `marketdata_test.odin` | Test rewritten: verifies direct layer_store access |
| `cell_view_model_test.odin` | Test rewritten: verifies stores resolve from active stream |

## Architecture After S100

```
Backend (WS) → data_source_poll_and_apply() → layer_store.streams[subject_id]
                                                      ↓
                                            market_store_active_stream()
                                                      ↓
                                            resolve_stores_for_cell()     ← per-cell
                                            active_*_store() helpers      ← non-cell context
                                                      ↓
                                            Renderers, widgets, analytics
```

No intermediate mirror. No frame-by-frame copy. Single source of truth.

## Acceptance Criteria

- [x] `sync_legacy_stores_from_layer_store` removed (0 references)
- [x] `sync_active_stream_view_to_global_stores` removed (0 references)
- [x] Runtime using only canonical layer_store stores
- [x] Renderers, widgets, analytics read from layer_store
- [x] No regressions — 627 tests pass
- [x] `Global_Stores` retains only non-stream-scoped data (dom, footprint, markets)
