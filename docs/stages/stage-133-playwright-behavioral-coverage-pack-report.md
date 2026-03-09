# Stage S133 — Playwright Behavioral Coverage Pack

**Date:** 2026-03-09
**Status:** COMPLETE

## Summary

Built comprehensive behavioral E2E coverage on top of the S132 test architecture.
From 30 tests across 5 specs to **118 runtime tests across 14 spec files** — all
probe-driven, behavior-oriented, and reusable.

## Coverage Matrix

| Spec File | Tests | What It Validates |
|-----------|-------|-------------------|
| `boot.spec.ts` | 11 | Canvas, WASM, hello, ACK, candles, stream, transport, layout version |
| `timeframe.spec.ts` | 8 | All 6 TFs individually, rapid cycle desync check, counter |
| `compare-mode.spec.ts` | 9 | Enter/exit, idempotent, TF in compare, pane count, canvas, data flow |
| `ui-modes.spec.ts` | 6 | Zen, indicators, help, detail, picker open/dismiss, picker select |
| `stability.spec.ts` | 3 | 30s/60s sustained, rapid TF stress + settle |
| `workspace.spec.ts` | 8 | Settings persistence, TF survives reload, layout_v6, CRC, clean boot |
| `navigation.spec.ts` | 7 | Markets/Settings/Instrument/Health routes, rapid cycling, route persistence |
| `widgets.spec.ts` | 14 | Candles, trades, stats, orderbook, DOM, data across TF, zero drops |
| `indicators.spec.ts` | 5 | Enabled/rendered states, TF invariance, compare invariance |
| `focus-mode.spec.ts` | 5 | Enter/exit, data flow, TF switch, stream preservation |
| `stream-switching.spec.ts` | 7 | Subject change, candles, counter, ACK, seq gaps, persist |
| `performance.spec.ts` | 13 | Render p95/p99, parse latency, backlog, transport, zero drops |
| `data-states.spec.ts` | 9 | Loading→seeding→live pipeline, monotonic growth, no resync/gaps |
| `multi-tf-widgets.spec.ts` | 13 | 5 TFs × candles + seq gaps, round-trip, 4h validation, trades |
| **Total** | **118** | |

## New Specs Added (S133)

| Spec | Purpose |
|------|---------|
| `workspace.spec.ts` | Validates save/load/reload of layout, settings, CRC integrity |
| `navigation.spec.ts` | Tests all page routes and return-to-dashboard data resumption |
| `widgets.spec.ts` | Widget data pipeline: candles, trades, stats, orderbook, DOM |
| `indicators.spec.ts` | Indicator enabled/rendered probes, TF/compare invariance |
| `focus-mode.spec.ts` | Focus mode lifecycle, data continuity, stream preservation |
| `stream-switching.spec.ts` | Stream change → unsub → sub → ACK → data lifecycle |
| `performance.spec.ts` | Render budgets, parse/apply latency, backlog, drops |
| `data-states.spec.ts` | Loading → seeding → live state transitions |
| `multi-tf-widgets.spec.ts` | Per-TF widget data validation across all major timeframes |

## Helpers Extended

### WasmProbe — 30+ new convenience accessors
- Widget data: `orderbookAsks/Bids`, `statsCount/State`, `heatmapSnaps`, `vpvrLevels`, `domEntries`, `tapeEntries`
- Indicators: `indicatorEnabled(name)`, `indicatorRendered(name)` (5 indicators)
- Layout: `layoutVersion()`, `layoutMigrated()`
- Compare: `compareCount()`, `compareFocusedIdx()`
- Display: `activeLiveStats/Candle/Heatmap/Vpvr()`
- Stream: `streamCount()`, `activeSubjectLo32()`
- Performance: `statsRenderP95Us/P99Us/OverBudget`, `tapeRenderP95Us/OverBudget`, `domRenderP95Us/OverBudget`
- Parse: `mdParseP95Us/P99Us`, `mdApplyP95Us/P99Us`, `mdTransportMode()`
- Backlog: `tradeBacklog()`, `candleBacklog()`, `signalBacklog()`
- Drops: `statsDropTotal()`, `tapeDropTotal()`, `domDropTotal()`

### Wait helpers — 5 new strategies
- `waitForTrades(page)` — probe_widget_trades_count > 0
- `waitForStats(page)` — probe_widget_stats_count > 0
- `waitForOrderbook(page)` — asks + bids > 0
- `waitForLocalStorage(page, key)` — wait for a key to be written
- `waitForProbeValue(page, name, min)` — wait for probe to reach threshold

### DashboardPage — 15+ new methods
- Pages: `goToMarkets()`, `goToSettings()`, `goToInstrumentOverview()`, `goToSessionHealth()`
- Focus: `toggleFocusMode()`, `exitFocusMode()`
- Resync: `triggerResync()`
- Storage: `getLocalStorage()`, `setLocalStorage()`, `clearLocalStorage()`, `getWorkspaceSettings()`

## Design Decisions

### 1. Probe-Driven Everything
Every assertion uses WASM probes — no screenshot-only tests. Screenshots are evidence, not assertions.

### 2. State Transition Testing
`data-states.spec.ts` verifies the hello → ACK → candles → trades → stats pipeline
in order, testing the actual bootstrap sequence the user experiences.

### 3. Performance as Regression Tests
`performance.spec.ts` codifies the performance contracts:
- Render p95 ≤ 2ms, p99 ≤ 5ms
- Parse p95 ≤ 500us, p99 ≤ 1ms
- Zero backlog accumulation, zero drops
If a code change regresses latency, these tests catch it.

### 4. Workspace Round-Trip
`workspace.spec.ts` tests the full persistence lifecycle:
setting → localStorage → reload → restore. Includes CRC integrity validation.

### 5. Multi-TF as Behavioral Matrix
Instead of one test per timeframe, `multi-tf-widgets.spec.ts` generates parameterized tests
for each major TF, catching TF-specific regressions.

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Covers real terminal behavior | DONE — 118 tests across all major flows |
| Regressions detectable early | DONE — performance budgets, state transitions, seq gaps |
| Reliable and reusable tests | DONE — probe-driven, no blind waits, shared fixtures |
| Boot/WS/TF/workspace/focus/zen/nav/widgets/states | DONE — all covered |
| Functional + state assertions | DONE — every test asserts probe values, not just screenshots |
| Readable and standardized | DONE — consistent fixture usage, page object patterns |
