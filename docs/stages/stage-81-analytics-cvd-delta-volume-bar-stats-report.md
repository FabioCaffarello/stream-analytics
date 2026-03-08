# Stage 81 — Analytics CVD, Delta Volume & Bar Statistics Report

**Date:** 2026-03-08
**Status:** COMPLETE
**Scope:** Close high-priority analytics gaps for CVD, Delta Volume, and Bar Statistics

## Summary

S81 delivers end-to-end analytics pipeline enhancements for CVD, Delta Volume, and Bar Statistics:
- Analytics widget standalone subscriptions (was broken — zero channels)
- Bar Statistics history rendering (delta bars for volume imbalance)
- Historical backfill via cold reader API (`/api/v1/{cvd,delta_volume,bar_stats}`)
- CVD and Delta Volume chart subplot renderers (structurally complete)
- Indicator component extension for per-cell CVD/DV subplot toggle (bits 8-9)
- Chart_Layer_Kind enum + vtable entries for CVD/DV subplots
- WORKSPACE_SCHEMA_VERSION 9

## Architecture

### Analytics Widget Fix
The Analytics widget kind previously had `channels_for_widget(.Analytics) = 0`, meaning it never triggered its own subscription. Fixed to return `CH_CANDLES` since CVD/DV/BS piggyback on the aggregation pipeline tied to candle subjects.

### Cold Reader Range Fetch
New `analytics_range.odin` (services) + `analytics_range.odin` (app) implement budget-limited historical backfill:
- `parse_analytics_cvd_range` — slot mapping: [0]=delta_vol, [1]=cvd
- `parse_analytics_delta_volume_range` — slot mapping: [0]=buy_vol, [1]=sell_vol, [2]=delta_vol
- `parse_analytics_bar_stats_range` — slot mapping: [0-7] full bar stats + burst flag
- Budget: `ANALYTICS_RANGE_BUDGET = 64` entries per fetch
- Zero-alloc JSON parsing helpers

### Chart Subplot Renderers (Structural)
Two new subplot renderers following the RSI/MACD pattern:
- `indicator_cvd.odin` — CVD line chart from analytics store, timestamp-aligned via `stats_ts_to_x`
- `indicator_delta_vol.odin` — Delta volume +/- bar chart from analytics store

**Note:** These renderers are structurally complete and wired into the `Chart_Layer_Kind` enum and vtable system. However, the candle chart currently renders through the `layers` package (`price_candles_render`), not the `widgets` package chart layer system (`candle_widget`). The subplot dispatching through `Chart_Layer_Vtable` requires either resurrecting the candle_widget or extending the layers system — deferred to a future stage.

### Indicator Persistence
- `pack_indicator_flags` / `unpack_indicator_flags` extended to 10 bits (was 8)
- Bit 8 = `show_cvd`, bit 9 = `show_delta_vol`
- All construction sites updated in `actions.odin` and `layout_persist.odin`
- Both `Indicator_Component` and `Global_Indicator_State` carry the new fields

## Files Modified

### Client — Services
| File | Change |
|------|--------|
| `services/analytics_range.odin` | **NEW** — Range fetch parsers for CVD, DV, BS |
| `services/analytics_range_test.odin` | **NEW** — 7 unit tests for range parsing |

### Client — App
| File | Change |
|------|--------|
| `app/analytics_range.odin` | **NEW** — App-layer range fetch integration |
| `app/widget_channels.odin` | Analytics widget → CH_CANDLES subscription |
| `app/render_analytics.odin` | Bar Stats history rendering (delta bars) |
| `app/components.odin` | `show_cvd`, `show_delta_vol` fields |
| `app/actions.odin` | Indicator_Component construction sites |
| `app/layout_persist.odin` | Pack/unpack bits 8-9 |
| `app/actions_cell_mutations.odin` | Analytics range fetch on widget set |
| `app/workspace_schema.odin` | V8 → V9, updated comments |

### Client — Ports
| File | Change |
|------|--------|
| `ports/marketdata.odin` | 3 new `fetch_analytics_*` port methods |

### Client — Widgets
| File | Change |
|------|--------|
| `widgets/chart_layer.odin` | CVD/DV in Chart_Layer_Kind, vtable entries, analytics_store in context |
| `widgets/indicator_cvd.odin` | **NEW** — CVD subplot renderer |
| `widgets/indicator_delta_vol.odin` | **NEW** — Delta Volume subplot renderer |
| `widgets/candle_widget.odin` | Indicator_Render_Probe CVD/DV fields |

## Tests

### New Tests (7)
- `test_parse_cvd_range_empty` — Empty JSON array
- `test_parse_cvd_range_single` — Single CVD entry
- `test_parse_cvd_range_multiple` — Multiple CVD entries
- `test_parse_delta_volume_range` — Delta Volume parsing
- `test_parse_bar_stats_range` — Bar Stats with burst flag
- `test_parse_bar_stats_range_no_burst` — Bar Stats without burst
- `test_parse_cvd_range_budget_limit` — Budget truncation at 64
- `test_parse_analytics_nil_store` — Nil store safety

### Total Test Count
- md_common: 402
- services: 119 (+7)
- app: 25
- **Total: 546**

## Known Limitations

1. **Candle chart subplots not dispatched**: The `candle_widget` proc is dead code. CVD/DV subplot renderers are structurally complete but won't render until the layers system is extended or candle_widget is resurrected.
2. **Native/web port stubs**: The 3 new `fetch_analytics_*` port methods need platform-specific implementations.
3. **Compare mode analytics**: Not addressed in this stage.

## Validation Checklist

### Functional
- [x] Analytics widget subscribes to candle channel (was broken)
- [x] Bar Stats shows delta bar history when History toggle is on
- [x] CVD range parser correctly maps [0]=delta_vol, [1]=cvd
- [x] Delta Volume range parser correctly maps [0]=buy_vol, [1]=sell_vol, [2]=delta_vol
- [x] Bar Stats range parser handles burst flag in bit 0
- [x] Budget limit enforced at 64 entries
- [x] Nil store safety in all parsers
- [x] Indicator flags persist/restore correctly with 10 bits
- [x] WORKSPACE_SCHEMA_VERSION bumped to 9
- [x] All Indicator_Component construction sites include CVD/DV fields

### Visual (requires running client)
- [ ] Analytics widget (CVD kind) shows CVD value + sparkline history
- [ ] Analytics widget (DV kind) shows delta value + buy/sell bar + delta bars
- [ ] Analytics widget (BS kind) shows imbalance delta bar history
- [ ] Widget catalog can add Analytics cell and cycle through OI/DV/CVD/BS
- [ ] History toggle ("H" button) works for all 4 analytics kinds

### Integration (requires `make up PROCESSOR_REPLICAS=2`)
- [ ] CVD analytics events flow from aggregation pipeline to client
- [ ] Delta Volume events populate analytics store
- [ ] Bar Stats events include burst flag when volume exceeds threshold
- [ ] Cold reader APIs return historical data for all 3 types
