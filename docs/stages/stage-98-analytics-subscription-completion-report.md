# Stage 98 — Analytics Subscription Completion Report

**Date:** 2026-03-08
**Status:** COMPLETE
**Branch:** codex/s9-legacy-removal-cutover

## Problem

S96 validated that analytics rendering works (subplots, analytics cells), but realtime
analytics data never arrives. Root cause: the client had NO subscription channels for
analytics streams. Analytics event kinds (CVD, DeltaVolume, OI, BarStats) existed at
the parser/reducer level but the `MD_Channel` enum only had 9 base channels. The
reconciliation layer could not subscribe to `aggregation.cvd`, `aggregation.delta_volume`,
`aggregation.oi`, or `aggregation.bar_stats` NATS subjects.

## Solution

Added 4 first-class analytics channel variants to `MD_Channel` and wired them through
the entire subscription stack — from enum definition through subject building, endpoint
capabilities, widget channel mapping, TF-sensitive reconciliation, and both platform
adapters (native + web).

## Changes

### Core — Proto-First Channel Definition

| File | Change |
|---|---|
| `ports/marketdata.odin` | Added `Analytics_CVD`, `Analytics_Delta_Volume`, `Analytics_OI`, `Analytics_Bar_Stats` to `MD_Channel` enum |
| `util/subject.odin` | `channel_to_stream_type()` maps to `aggregation.cvd/delta_volume/oi/bar_stats`; `timeframe_for_channel()` marks them TF-aware (default `1m`) |
| `streams/endpoints.odin` | Widened `Endpoint_Capabilities.channel_mask` from `u8` to `u16`; added analytics bits to `ENDPOINT_ALL_CHANNELS` |

### Subscription Layer

| File | Change |
|---|---|
| `app/reconcile.odin` | `CHANNEL_COUNT` 9→13; `CH_TF_SENSITIVE` includes all 4 analytics channels |
| `app/widget_channels.odin` | `CH_ANALYTICS` composite bitmask; `.Candle` widget includes analytics for subplot support; `.Analytics` widget subscribes to dedicated analytics channels (replaces S81 candle piggyback) |
| `md_common/md_common.odin` | `subject_for_channel()` applies TF filter to analytics channels |
| `app/app_util.odin` | `channel_short_label()` and `parse_channel_short_label()` handle analytics channels |

### Platform Adapters

| File | Change |
|---|---|
| `native/marketdata_native.odin` | Analytics poll events now use correct `source.channel` (was `.Stats`); metrics `latest_pending` counts analytics dirty flags |
| `web/marketdata_web.odin` | Added analytics staging fields (`oi/cvd/delta_vol/bar_stats_staging` + dirty flags); staging from parsed results (was deferred/dropped); poll emits analytics events with correct channel; metrics updated |

### Tests

| File | Tests |
|---|---|
| `util/subject_test.odin` | 5 new: `test_build_subject_analytics_cvd/delta_volume/oi/bar_stats/default_tf` |
| `app/marketdata_test.odin` | Updated: Candle widget asserts analytics channels present; compare analytics asserts dedicated channels; analytics widget assertion updated |

## Architecture

```
Widget_Kind           MD_Channel                   NATS Subject
────────────────────────────────────────────────────────────────
.Candle        →  Analytics_CVD              →  aggregation.cvd/<v>/<s>/<tf>
               →  Analytics_Delta_Volume     →  aggregation.delta_volume/<v>/<s>/<tf>
               →  Analytics_OI              →  aggregation.oi/<v>/<s>/<tf>
               →  Analytics_Bar_Stats       →  aggregation.bar_stats/<v>/<s>/<tf>
               →  Candles, Stats, Heatmaps, VPVR, Evidence, Signals (unchanged)

.Analytics     →  Analytics_CVD              →  aggregation.cvd/<v>/<s>/<tf>
               →  Analytics_Delta_Volume     →  aggregation.delta_volume/<v>/<s>/<tf>
               →  Analytics_OI              →  aggregation.oi/<v>/<s>/<tf>
               →  Analytics_Bar_Stats       →  aggregation.bar_stats/<v>/<s>/<tf>
```

## Data Flow (end-to-end)

1. `reconcile_subscriptions()` scans cells → builds `Sub_Want` with analytics bitmask
2. `subscribe_tf(venue, symbol, .Analytics_CVD, "1m")` → `build_subject_with_timeframe` → `"aggregation.cvd/binance/BTCUSDT/1m"`
3. WS adapter sends SUBSCRIBE frame to server
4. Server publishes analytics events on NATS subject
5. Message parser routes to `Parse_Result_Kind.CVD` → staging buffer
6. Poll emits `MD_Event{kind=.CVD, source.channel=.Analytics_CVD}`
7. `market_store_reduce_analytics()` pushes to `Analytics_Store` ring buffer
8. `layer_canvas.odin` subplot renders from `analytics_collect_by_kind()`

## TF Behavior

- Analytics channels are TF-sensitive (same as Candles/Heatmaps/VPVR)
- Changing timeframe triggers unsubscribe old + subscribe new (via `CH_TF_SENSITIVE` mask)
- Per-cell TF overrides work correctly (per-cell analytics on different TFs)
- Compare mode: each pane subscribes analytics at its own effective TF

## No Duplication

- If both a Candle and Analytics cell target the same market+TF, `reconcile_subscriptions()` merges their channel bitmasks via OR — single subscription per subject
- `seed_stream_slot_for_subject()` uses `market_id64(venue, symbol)` so all channels for the same market converge to one `Stream_View_Slot`

## Metrics

- 11 files modified
- 5 new tests, 3 updated tests
- Zero wire-breaking changes (additive enum extension)
- Zero regressions (existing tests pass with updated assertions)
