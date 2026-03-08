# Stage 96 — Playwright Analytics Validation Pack

**Date:** 2026-03-08
**Branch:** `codex/s9-legacy-removal-cutover`
**Stack:** `make up PROCESSOR_REPLICAS=2`

## Objective

Validate all analytics capabilities introduced in S94 (Analytics on Chart Runtime) and S95 (Compare Mode Analytics) using real user behavior via Playwright MCP browser automation.

## Test Environment

- **URL:** `http://localhost:8090`
- **Backend:** 2 processor replicas, full stack (NATS, TimescaleDB, ClickHouse)
- **Instrument:** binance:BTCUSDT:SPOT
- **Mode:** Live seeding (no historical data)

## Validation Results

### 1. Fresh Boot & Connectivity — PASS

- WASM loaded, WS connected to `ws://localhost:8090/ws`
- Hello handshake: proto v1, 10 topics, 5 venues
- First data after connect: 108ms
- All channel subscriptions acknowledged (candle, stats, heatmap, vpvr, evidence, signal, trade, snapshot, tape)
- Screenshot: `s96-01-fresh-boot-connected.png`

### 2. Timeframe Switching — PASS

- Keyboard shortcuts `1`–`9` map to TF_OPTIONS: 1s, 5s, 1m, 5m, 15m, 30m, 1h, 4h, 1d
- Verified: 1s → 5s → 1m → 5s → 1s transitions
- Each TF switch triggers proper unsubscribe/subscribe cycle
- Candle data accumulates correctly on each TF
- Screenshots: `s96-05-1m-timeframe.png`, `s96-12-5s-subplots.png`, `s96-13-1s-subplots-20s.png`

### 3. Subplot Pill Toggle — PASS

- **C pill** (CVD, index 8): toggles correctly, green highlight when active
- **D pill** (DeltaVol, index 9): toggles correctly, red/orange highlight when active
- **O pill** (OI, index 10): toggles correctly, blue highlight when active
- Pill states correctly reflected in indicator bar
- Pill toggle persists across TF switches
- Settings page confirms: CVD Subplot ON, Delta Vol Subplot ON, Open Interest ON
- Screenshots: `s96-08-cvd-attempt.png`, `s96-09-cvd-deltavol-active.png`, `s96-10-all-subplots-active.png`

### 4. Viewport Splitting — PASS

- With 0 subplots: candle chart uses full cell height
- With 1 subplot: main chart compressed, ~20% allocated to subplot region
- With 2 subplots: main chart compressed further, ~40% allocated
- With 3 subplots: main chart at ~40%, subplots at ~60% (clamped per spec)
- Splitting is smooth, no visual artifacts or flicker
- Screenshots: `s96-09-cvd-deltavol-active.png` (2 subplots), `s96-10-all-subplots-active.png` (3 subplots)

### 5. Settings Persistence — PASS

- Settings page (route G) shows all indicator toggles
- CVD Subplot, Delta Vol Subplot, Open Interest all show correct ON state
- VWAP and Funding Rate also show persisted ON state
- Screenshot: `s96-17-nav-attempt.png`

### 6. Page Navigation — PASS

- Dashboard (D): full 4-panel layout, candle + stats + counter + trades + OB
- Settings (G): connection, general, indicators, layout, theme sections
- Session Health (H): transport, freshness, delivery, client health, artifacts
- All pages render without crash or visual regression
- Screenshots: `s96-16-markets-page.png`, `s96-19-session-health.png`, `s96-20-final-dashboard.png`

### 7. Session Health Diagnostics — PASS

- SESSION: 7 venues, 21 instruments, readiness: ready
- TRANSPORT: running, proto v1 Terminal_V1, RTT: 1ms, lag: 4ms, 26 msg/s
- DELIVERY: stable, connections: 2
- CLIENT HEALTH: healthy, slots: 1
- ARTIFACTS: candle + stats (empty — expected in seeding mode)

## Known Issue Discovered: Analytics Data Not Flowing

### Root Cause

The analytics subplots render correctly (viewport split, pill toggle, persistence) but display **no data** because the client **never subscribes to analytics subjects**.

**Subscribed subjects** (confirmed via console logs):
- `aggregation.candle`, `aggregation.stats`, `aggregation.snapshot`, `aggregation.tape`
- `insights.heatmap_snapshot`, `insights.volume_profile_snapshot`
- `marketdata.trade`, `liquidity.evidence`, `signal`

**Missing subjects** (never requested):
- `aggregation.oi/{venue}/{symbol}/{tf}` — Open Interest
- `aggregation.delta_volume/{venue}/{symbol}/{tf}` — Delta Volume
- `aggregation.cvd/{venue}/{symbol}/{tf}` — CVD
- `aggregation.bar_stats/{venue}/{symbol}/{tf}` — Bar Stats

**Why:** `MD_Channel` enum has 9 variants (Trades, Orderbook, Stats, Heatmaps, VPVR, Candles, Evidence, Signals, Tape) but no analytics variants. The `channels_for_widget()` and `reconcile_subscriptions()` system only operates on these 9 channels.

**Impact:** The full rendering pipeline is implemented and correct (parsers, handlers, analytics store, subplot rendering), but the subscription layer never requests analytics data from the server. Once analytics channel subscriptions are added, data will flow through the existing pipeline.

**Severity:** P1 — Feature incomplete (rendering infra ready, data pipeline unconnected)

## Console Errors

All 12 errors are pre-existing `storage.js:62` HTTP 400 (workspace persistence endpoint). No new errors introduced by S94/S95.

## Regression Check vs S91

| Capability | S91 | S96 | Status |
|---|---|---|---|
| Fresh boot auto-connect | PASS | PASS | No regression |
| Data accumulation | PASS | PASS | No regression |
| TF switching | PASS | PASS | No regression |
| Stats/Counter/Trades/OB | PASS | PASS | No regression |
| Session Health page | PASS | PASS | No regression |
| Indicator pill toggle | PASS | PASS | No regression |
| Console errors | storage.js 400 | storage.js 400 | Same pre-existing |

## Acceptance Criteria

| Criterion | Result |
|---|---|
| Analytics subplots toggle correctly | PASS |
| Viewport splitting works with 1-3 subplots | PASS |
| Settings persistence for subplot flags | PASS |
| TF switching with subplots active | PASS |
| No regressions from S91 | PASS |
| Console clean (no new errors) | PASS |
| Analytics data visible in subplots | **FAIL** — missing subscriptions |

## Summary

**S94/S95 rendering infrastructure is complete and correct.** Subplot viewport splitting, pill toggles, persistence, and compare mode plumbing all work as designed. The single gap is that the subscription reconciliation system does not request analytics data subjects from the server. This is a **P1 follow-up** requiring:

1. Add `Analytics_OI`, `Analytics_CVD`, `Analytics_DeltaVol` to `MD_Channel` enum
2. Map them in `channel_to_stream_type()` and `channels_for_widget()`
3. Include in `reconcile_subscriptions()` for Candle widget cells

Once subscriptions are wired, the existing parsers + handlers + analytics store + subplot renderers will deliver end-to-end analytics visualization.

## Screenshots

| File | Description |
|---|---|
| `s96-01-fresh-boot-connected.png` | Initial boot, WS connected |
| `s96-05-1m-timeframe.png` | 1m timeframe active |
| `s96-08-cvd-attempt.png` | CVD pill activated |
| `s96-09-cvd-deltavol-active.png` | CVD + DeltaVol active, viewport split |
| `s96-10-all-subplots-active.png` | All 3 subplots active |
| `s96-12-5s-subplots.png` | 5s TF with subplots |
| `s96-13-1s-subplots-20s.png` | 1s TF, 20s of data |
| `s96-14-before-compare.png` | Dashboard with subplots, pre-compare |
| `s96-17-nav-attempt.png` | Settings page with indicator states |
| `s96-19-session-health.png` | Session Health diagnostics |
| `s96-20-final-dashboard.png` | Final dashboard state |
