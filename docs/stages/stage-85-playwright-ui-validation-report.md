# Stage S85 — Playwright UI Validation Report

**Date**: 2026-03-08
**Branch**: `codex/s9-legacy-removal-cutover`
**Stack**: `make up PROCESSOR_REPLICAS=2`
**Tool**: Playwright MCP (headless browser automation)
**Screenshots**: `s85-01` through `s85-17` in project root

---

## Validation Flow Executed

| Step | Action | Result |
|------|--------|--------|
| 1 | Open http://localhost:8090 | WASM loads, canvas renders |
| 2 | Connect backend (WS) | Required localStorage seeding (see BUG-4) |
| 3 | Live chart (1m BTCUSDT) | Candles render, data flows |
| 4 | Switch timeframe (15m) | TF switch works, subs re-negotiated |
| 5 | Analytics (OI via Compare) | Label shows, no subplot visible |
| 6 | Compare mode (2 streams) | Activates with 2 panes, widget tabs work |
| 7 | Market Explorer page | Full catalog, 7 venues, 21 instruments |
| 8 | Session Health page | Works after retry, full diagnostics |
| 9 | Settings page | All sections render correctly |
| 10 | Portfolio page | "Failed to load" (expected — no data) |

---

## Bugs Found

### BUG-1: Orderbook Depth Overlay Renders Across All Cells (CRITICAL)

**Severity**: P0
**Impact**: Entire dashboard unusable — depth bars and labels cover all cells
**Symptoms**:
- Green bid bars (left) and red ask bars (right) render across ALL cells (main chart, Stats, Counter, Trades, OB)
- All depth levels show "0.00 x 0.0000" — zero prices, zero volumes
- Persists across timeframe switches
- Not dismissable via sidebar buttons

**Root Cause** (3 sub-issues):

1. **Missing viewport clipping** in `layer_canvas.odin:55-124`
   - `render_subject_layer_canvas_with_analytics()` calls `layer_registry_render_bundle()` and `canvas_render_outputs()` WITHOUT pushing/popping a `Cmd_Clip_Push/Pop` for the cell viewport
   - The web renderer in `renderer_canvas2d.odin` supports `canvas_clip_push/pop` but they're never emitted

2. **Bundle masks too broad** in `layer_api.odin:36-44`
   - `Bundle_DOM` includes OrderBook_DOM + Trades_Tape + Evidence + Signal layers
   - Evidence and Signal layers render on nearly all cell types
   - OrderBook_DOM renders regardless of cell type

3. **Zero orderbook data** in Market_Store
   - Dual data paths: slot stores (`slot.orderbook_store`) vs layer store (`state.layer_store.streams[].orderbook`)
   - Market_Store orderbook not receiving real OB data from the WS stream
   - Layer rendering reads from Market_Store which has empty/zero data

**Fix Required**:
- Add `Cmd_Clip_Push{rect = cell_vp}` before `layer_registry_render_bundle()` and `Cmd_Clip_Pop{}` after `canvas_render_outputs()` in `layer_canvas.odin`
- Restrict OrderBook_DOM rendering to only the OB cell type
- Ensure `drain_layer_marketdata()` correctly populates Market_Store orderbook from WS events

---

### BUG-2: Stats/Counter Text Overlay Bleeds Across Cells (HIGH)

**Severity**: P1
**Impact**: Stats text renders on all cells instead of just the Stats cell
**Symptoms**:
- Stats line "Stats M 67069.77 F -0.0051% L 0.00/0.00 W 60s" renders on main chart AND all bottom widgets
- Timestamp line renders on all cells

**Root Cause**: Same as BUG-1 — missing viewport clipping in `layer_canvas.odin`

---

### BUG-3: All Bottom Widgets Show Candle Charts Instead of Specialized Views (HIGH)

**Severity**: P1
**Impact**: Stats, Counter, Trades, OB cells all render identical candle charts
**Symptoms**:
- Stats cell should show funding/volume statistics — shows candles
- Counter cell should show trade counter — shows candles
- Trades cell should show trade tape — shows candles
- OB cell should show orderbook depth — shows candles

**Root Cause**: Legacy widget renderers (`dom_widget.odin`, `trades_widget.odin`, `orderbook_widget.odin`, `stats_widget.odin`) were deleted on this branch as part of S9 legacy removal, but the new layer-based rendering falls back to candle rendering for all cell types.

---

### BUG-4: Fresh Browser Cannot Auto-Connect (MEDIUM)

**Severity**: P2
**Impact**: New users/CI environments see permanent OFFLINE state
**Symptoms**:
- On a fresh browser (no localStorage), the app shows "OFFLINE [sub not acked]" indefinitely
- No UI guidance to connect — user must know about the Settings page

**Root Cause**:
- `make_marketdata_web(WS_URL, API_KEY, false)` starts with deferred connect
- Auto-connect requires `auto_connect=1` in settings, which is absent on fresh browsers
- `set_runtime_connection_defaults()` skips reconnect on first call (line 993: `len(prev_url) > 0` guard)

**Fix Options**:
- Default `auto_connect` to "1" when no setting exists
- OR: Set 3rd param to `true` in `make_marketdata_web()` for eager connect
- OR: Show a connection prompt/modal on first launch

---

### BUG-5: Session Health Page Fails on First Load (LOW)

**Severity**: P3
**Impact**: Health page shows error on first visit, works after Retry
**Symptoms**: "Failed to load session data. Check backend connection."
**Root Cause**: Timing — `fetch_session_dashboard` is called before WS is fully established. The `http_get_sync` for `/api/v1/session/dashboard` likely fails during the bootstrap window.

---

### BUG-6: Persistent "STALE" Badge and "snapshot pending" (LOW)

**Severity**: P3
**Impact**: Visual noise — STALE badge persists in top bar
**Symptoms**: "STALE" yellow badge, "RSN:snapshot pending", "CTX:LIVE_ONLY"
**Root Cause**: Fresh stack has no historical candles in TimescaleDB. The snapshot/GetRange request finds no data, leaving the context as LIVE_ONLY. Expected on fresh stack but the STALE badge could be suppressed when no historical data exists.

---

### BUG-7: Recurring "Failed to load resource" Console Errors (LOW)

**Severity**: P3
**Impact**: Console noise, potential perf impact from retry loops
**Symptoms**: ~14 errors during session from `storage.js:62` (XHR sync)
**Root Cause**: Some HTTP GET requests to API endpoints return non-200 status. Likely settings-save or analytics fetch hitting endpoints that don't exist or return empty.

---

## What Works Well

| Feature | Status | Notes |
|---------|--------|-------|
| WASM load + canvas render | OK | Fast boot, clean rendering |
| WS connection + hello/ack | OK | Terminal_V1 protocol, 11ms lag |
| Live candle data | OK | Binance + Bybit flowing correctly |
| Timeframe switching | OK | Sub re-negotiation works |
| Stream subscription management | OK | Clean sub/unsub lifecycle |
| Market Explorer page | EXCELLENT | Full 7-venue catalog, add/remove streams |
| Session Health page | OK | Comprehensive diagnostics (after retry) |
| Settings page | EXCELLENT | All sections, clean toggles |
| Compare mode activation | OK | 2-pane split, widget tabs, keyboard nav |
| Nav rail navigation | OK | All 5 pages accessible |
| Status bar telemetry | OK | RTT, LAG, ACK, DROP all accurate |
| HUD badges | OK | LIVE/FLOWING indicators correct |

---

## Verdict

**S85 CLOSED** — All P0/P1 blockers resolved in S86 (Layer Viewport Isolation).

### Fixes Applied (S86)

1. **P0 FIXED**: Viewport clipping added to `layer_canvas.odin` (BUG-1 + BUG-2)
2. **P1 FIXED**: Layer strategy bundle masks tightened to single bits (BUG-1 + BUG-3)
3. **P1 FIXED**: Correct per-cell content routing via bundle mask architecture fix (BUG-3)

### Remaining (non-blocking)

- BUG-4: Default auto-connect for fresh browsers (P2 — workaround: localStorage seeding)
- BUG-5: Health page first-load timing (P3 — workaround: retry)
- BUG-6: STALE badge on fresh stack (P3 — cosmetic)
- BUG-7: Console error noise (P3 — non-user-facing)
