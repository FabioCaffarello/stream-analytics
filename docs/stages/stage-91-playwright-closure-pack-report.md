# Stage S91 — Playwright Closure Pack Report

**Date**: 2026-03-08
**Branch**: `codex/s9-legacy-removal-cutover`
**Stack**: `make up PROCESSOR_REPLICAS=2`
**Tool**: Playwright MCP (headless browser automation)
**Screenshots**: `s91-01` through `s91-14` in project root

---

## Objective

Re-execute the S85 validation flow end-to-end and confirm that all P0/P1 bugs
identified in S85 were resolved by S86–S90 fixes. Emit a closure or reprovation
report with objective evidence.

---

## Validation Flow

| Step | Action | S85 Result | S91 Result | Verdict |
|------|--------|-----------|-----------|---------|
| 1 | Open http://localhost:8090 (fresh browser) | WASM loads but stays OFFLINE | WASM loads, **auto-connects** | PASS |
| 2 | WS connect + subscriptions | Required localStorage seeding | Auto-connect, 6 subs acked | PASS |
| 3 | Live chart (1m BTCUSDT) | Candles render but OB/Stats bleed | Candles render, **zero bleed** | PASS |
| 4 | Switch timeframe (5m) | Works | Works — clean unsub/sub cycle | PASS |
| 5 | Switch back to 1m | Works | Works | PASS |
| 6 | Add 2nd stream (bybit:ETHUSDT) | Works | Works — snapshot received | PASS |
| 7 | Compare mode (C key, 2 streams) | Works with bleed | **2-pane split, zero cross-pane bleed** | PASS |
| 8 | Session Health page | Failed on first load | **Loads on first visit**, full diagnostics | PASS |
| 9 | Market Explorer page | Works | Works — 7 venues, 21 instruments | PASS |
| 10 | Portfolio page | "Failed to load" (expected) | "Failed to load" (expected — no data) | PASS |
| 11 | Console error check | ~14 errors (WS spam + XHR) | **6 errors** (XHR only, no WS spam) | PASS |

---

## S85 Bug Resolution Matrix

| Bug | Severity | S85 Description | Fix Stage | S91 Status | Evidence |
|-----|----------|----------------|-----------|-----------|----------|
| BUG-1 | P0 | Orderbook depth overlay across all cells | S86 | **CLOSED** | s91-03: OB depth confined to OB cell only |
| BUG-2 | P1 | Stats text bleeds across cells | S86 | **CLOSED** | s91-03: Stats text confined to Stats cell |
| BUG-3 | P1 | Bottom widgets show candle charts | S86+S87 | **CLOSED** | s91-03: Stats=mark/funding, Counter=trades/vol/B÷S, OB=spread+depth |
| BUG-4 | P2 | Fresh browser cannot auto-connect | S89 | **CLOSED** | s91-01: Auto-connected on fresh browser, no localStorage seeding |
| BUG-5 | P3 | Session Health fails on first load | S89 | **CLOSED** | s91-11: Loaded on first visit, full diagnostics visible |
| BUG-6 | P3 | Persistent STALE badge on fresh stack | S90 | **PARTIAL** | Status bar shows "seeding (no history)" correctly; badge shows STALE after initial FLOWING period (see Observation 1) |
| BUG-7 | P3 | Recurring console error noise | S89 | **CLOSED** | 6 errors (down from ~14), zero WS reconnect spam |

---

## Observations

### 1. STALE Badge Transition on Fresh Stack

The freshness badge initially shows "FLOWING" (green) when data first arrives, but
later transitions to "STALE" (yellow) as the freshness endpoint reports
`active: false`. The S90 three-state logic (FLOWING/SEEDING/STALE) depends on
composition stage to distinguish SEEDING from STALE:

- `Empty`/`Range_Pending`/`Live_Only` + `active: false` → SEEDING
- `Backfilled`/`Composed` + `active: false` → STALE

The badge may be reading the wrong composition state or the composition transitions
away from `Live_Only` as candle data accumulates. **Impact: cosmetic only** — the
status bar correctly shows "seeding (no history)" throughout. Not a P0/P1 issue.

### 2. Console Errors (storage.js)

All 6 console errors are HTTP 400 from `storage.js:62` — settings save or analytics
fetch hitting endpoints that return 400 (Bad Request). These are non-user-facing and
do not trigger visible retry loops. Down from ~14 in S85.

---

## What Works Well

| Feature | Status | Notes |
|---------|--------|-------|
| WASM load + canvas render | EXCELLENT | Fast boot, clean rendering |
| Fresh-browser auto-connect | EXCELLENT | **New** — no manual setup needed |
| WS protocol (hello/ack/sub) | OK | Terminal_V1, low latency |
| Live candle data | OK | Binance + Bybit flowing |
| Timeframe switching | OK | Clean unsub→sub cycle with snapshot |
| Cell viewport isolation | EXCELLENT | **Fixed** — zero cross-cell bleed |
| Specialized cell renderers | EXCELLENT | **Fixed** — Stats, Counter, OB each render correct content |
| Compare mode (2 panes) | OK | Side-by-side with tab bar |
| Stream management | OK | Add/switch via G modal |
| Session Health page | EXCELLENT | **Fixed** — loads on first visit |
| Market Explorer page | EXCELLENT | Full catalog, venue-grouped, filter tabs |
| Nav rail navigation | OK | All 5 pages accessible (D/V/P/G/H) |
| Status bar telemetry | OK | RTT, LAG, ACK, DROP, VP, CD all accurate |
| Console noise | IMPROVED | 6 errors (was ~14), no WS spam |

---

## Verdict

### S91 PASS — Closure Accepted

**All P0/P1 bugs from S85 are resolved and verified via live Playwright validation.**

| Severity | Total | Closed | Remaining |
|----------|-------|--------|-----------|
| P0 | 1 | 1 | 0 |
| P1 | 2 | 2 | 0 |
| P2 | 1 | 1 | 0 |
| P3 | 3 | 2 | 1 (cosmetic badge — see Observation 1) |

**Acceptance criteria met:**
- [x] P0/P1 zeroed
- [x] No regression of any S85 working flow
- [x] Objective screenshot evidence of stabilization (14 screenshots)
- [x] Fresh-browser experience validated end-to-end
- [x] All 5 pages navigable and functional
- [x] Compare mode operational with 2 streams
- [x] Console noise reduced by >50%

### Residual (non-blocking)

- **P3**: Freshness badge occasionally shows STALE instead of SEEDING on fresh stack (status bar message is correct). Cosmetic — can be addressed in a future UX polish pass.
- **P3**: 6 `storage.js` HTTP 400 errors per session (non-user-facing).
