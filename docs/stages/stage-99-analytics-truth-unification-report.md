# Stage 99 — Analytics Truth Unification

**Date:** 2026-03-08
**Status:** COMPLETE

## Problem

The client had three possible analytics stores:

1. **`layer_store.streams[i].analytics`** (Market_Stream) — written by WS realtime events via `market_store_reduce_analytics()`
2. **`stream_views.slots[i].analytics_store`** (Stream_View_Slot) — written by HTTP range fetch (`request_analytics_range`)
3. **`state.stores.analytics`** (Global_Stores) — fallback for follow-active cells, **never written to**

Historical (HTTP) and realtime (WS) analytics landed in completely separate stores. Subplot renderers read from `ctx.stream.analytics` (layer_store — realtime only), while the Analytics widget read from `Cell_Stores.analytics` (slot — historical only). Follow-active cells read from the global store which was always empty.

## Solution

Unified all analytics to a single canonical source: **`layer_store.streams[subject_id].analytics`** (Market_Stream.analytics).

### Changes

| File | Change |
|------|--------|
| `app/layer_marketdata.odin` | Added `state.stores.analytics = active.analytics` to `sync_legacy_stores_from_layer_store()` — syncs active stream analytics to global store |
| `app/analytics_range.odin` | HTTP range fetch now writes to `layer_store` Market_Stream (via `market_store_stream_get_or_alloc`) instead of slot.analytics_store |
| `app/stream_slots.odin` | `resolve_stores_for_cell()` resolves analytics from layer_store Market_Stream (not slot); `request_compare_pane_analytics_range()` and `request_compare_pane_subplot_analytics_kind()` write to layer_store; added `resolve_analytics_store_for_subject()` helper; analytics/session_vpvr/tpo resolution moved before venue/symbol early-return gate |
| `app/stream_views.odin` | TF-change clear paths clear `ms.analytics` on layer_store stream instead of slot.analytics_store |
| `app/app.odin` | Removed `analytics_store` field from `Stream_View_Slot` |
| `app/marketdata_test.odin` | 6 new S99 tests |

### Data Flow (After)

```
┌───────────────────────────────────────────┐
│ Backend (WS realtime)                     │
│ Open_Interest, Delta_Volume, CVD, Bar_Stats│
└──────────────┬────────────────────────────┘
               │ data_source_poll_and_apply()
               ▼
┌──────────────────────────────────────────────┐
│ layer_store.streams[subject_id].analytics    │  ← SINGLE CANONICAL STORE
│ (Market_Stream.analytics — ring buffer, 64)  │
└──────────────┬───────────────────────────────┘
               │                        ▲
               │ sync_legacy_stores     │ HTTP range fetch
               ▼                        │ (request_analytics_range)
┌─────────────────────────┐  ┌──────────┴──────────────┐
│ state.stores.analytics  │  │ Cold Reader API          │
│ (global fallback for    │  │ /api/v1/candles etc.     │
│  follow-active cells)   │  └─────────────────────────┘
└─────────────────────────┘
```

### Resolution Paths

- **Subplot renderers**: `ctx.stream.analytics` → layer_store stream ✓
- **Analytics widget (bound cell)**: `Cell_Stores.analytics` → layer_store stream via `resolve_stores_for_cell()` ✓
- **Analytics widget (follow-active)**: `state.stores.analytics` → synced from active layer_store stream ✓
- **Compare mode**: `resolve_analytics_store_for_subject()` → layer_store stream ✓

### Removed

- `Stream_View_Slot.analytics_store` field — no longer needed
- All `slot.analytics_store` write/clear paths replaced with layer_store equivalents

## Tests

6 new tests added to `app/marketdata_test.odin`:

| Test | Validates |
|------|-----------|
| `test_s99_sync_analytics_to_global_stores` | drain_layer_marketdata syncs analytics to global stores |
| `test_s99_resolve_stores_analytics_from_layer_store` | resolve_stores_for_cell returns layer_store analytics pointer |
| `test_s99_historical_and_realtime_compose` | Historical + realtime entries compose in same ring buffer |
| `test_s99_resolve_analytics_store_for_subject` | Helper returns correct store, nil for invalid inputs |
| `test_s99_tf_change_clears_layer_store_analytics` | TF change clears layer_store stream analytics |
| `test_s99_slot_no_analytics_store` | Stream_View_Slot works without analytics_store field |

## Test Results

- **md_common:** 401 tests ✓
- **services:** 186 tests ✓
- **layers:** 22 tests ✓
- **app:** 40 tests ✓ (6 new)
- **util:** 14 tests ✓
- **Total:** 663 tests, all passing

## Acceptance Criteria

- ✅ Analytics historical and realtime always consistent (single store)
- ✅ Only one analytics store active per stream in client
- ✅ Renderer consumes canonical source (layer_store Market_Stream)
- ✅ No secondary store writes
- ✅ Zero regressions
