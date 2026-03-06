# Stage 20 -- First Client Vertical Slice on Stable Backend Surfaces

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE (All 3 slices delivered)
**Depends on:** S18 (protocol hardening), S19 (client-readiness surfaces)

## Executive Summary

Stage 20 delivers the first end-to-end client vertical slice consuming stable backend surfaces from S16-S19 -- without introducing new transport semantics, domain logic, or wire contracts. The slice is: **Candle chart + Stats panel + Trades list for one market, bootstrapped via HTTP session endpoint, with freshness-aware connection status and timeline-driven historical seed.**

The client today has a working WS transport, stores, widgets, and reconciliation system. What it lacks is **structured HTTP bootstrap** -- it only calls `GET /api/v1/markets` at startup and derives everything else from WS events. S20 closes this gap by wiring the S19 session/freshness/timeline surfaces into the client bootstrap and per-frame health loop, making the client aware of backend readiness before subscribing.

**Key decisions:**
1. **Slice scope:** Candle + Stats + Trades (smallest set that proves full pipeline)
2. **HTTP bootstrap:** `GET /api/v1/session` replaces `GET /api/v1/markets` as primary bootstrap (session includes markets)
3. **Freshness polling:** `GET /api/v1/freshness` called post-connect to gate subscription readiness
4. **Timeline seed:** `GET /api/v1/timeline` replaces blind GetRange with data-aware historical requests
5. **No client-side composition:** Status normalization stays backend-owned (S19 decision preserved)
6. **No new wire contracts:** All WS message types already handled by existing message parser

## Current-State Audit

### Backend surfaces available (S16-S19)

| Surface | Route | Status | Client usage today |
|---------|-------|--------|-------------------|
| Markets | `GET /api/v1/markets` | Frozen (S15) | YES -- `fetch_markets` port, both web+native |
| Session | `GET /api/v1/session` | Frozen (S15) | NO |
| Catalog | `GET /api/v1/catalog` | Frozen (S16) | NO |
| Timeline | `GET /api/v1/timeline` | Frozen (S16) | NO |
| Freshness | `GET /api/v1/freshness` | Frozen (S16) | NO |
| Instrument Overview | `GET /api/v1/instrument/overview` | Frozen (S19) | NO |
| Session Dashboard | `GET /api/v1/session/dashboard` | Frozen (S19) | NO |
| Artifact Summary | `GET /api/v1/artifacts/summary` | Frozen (S19) | NO |
| WS Subscribe | `subscribe { subject, request_id }` | Frozen (S18) | YES -- full protocol |
| WS Snapshot | `snapshot { payload, snapshot_seq }` | Frozen (S18) | YES -- parsed |
| WS Event | `event { payload, prev_seq }` | Frozen (S18) | YES -- parsed |
| WS GetRange | `get_range { from_ms, to_ms, limit }` | Frozen (S18) | YES -- candle backfill |
| WS Hello | `hello / hello_ack` | Frozen (S18) | YES -- feature negotiation |
| WS Metrics | `metrics { backpressure, lag }` | Frozen (S18) | YES -- server metrics display |

### Client transport layer

**Working:**
- WS connection (native TCP + web JS bridge), Terminal_V1 protocol
- Hello handshake with feature negotiation
- Subscribe/unsubscribe with ack tracking
- Snapshot/event/batch frame parsing (all 10 event kinds)
- GetRange for candle historical backfill
- Reconnect with full reconciliation
- Server-pushed metrics integration
- Desync detection (seq gap, snapshot gap, protocol invalid, stale)

**Gap:** No HTTP calls besides `fetch_markets`. Client cannot query readiness, freshness, or data availability before subscribing.

### Client store layer

**Working (all ring buffers, zero-alloc after init):**
- Candle_Store (750 cap), Stats_Store (64), Trades_Store (256)
- Orderbook_Store (single snapshot), Heatmap_Store (32), VPVR_Store (32)
- DOM_Store, Footprint_Store, Signal_Store
- Market_Store (16 markets, market_id64 aggregation)
- Stream_View_Registry (32 per-subject slots)

**Gap:** No "bootstrap state" store -- client doesn't know if backend is ready, what markets are active, or what data ranges exist.

### Client widget layer

**Working:**
- Candle widget (OHLC, line, Heiken Ashi, footprint modes + 8 indicators)
- Stats widget (mark price, funding, liq buy/sell)
- Trades widget (recent trades list)
- Orderbook, DOM, Heatmap, VPVR, Signal widgets
- Compare mode, focus mode, layout presets

**Gap:** No readiness indicator per-market. No freshness badge. No "data available from X to Y" awareness.

### Client app shell

**Working:**
- `init()` → load defaults → fetch_markets → restore settings → reconcile → prime GetRange
- `update()` → drain_marketdata → UI → render
- Smart subscription reconciliation (channels_for_bundle + diff-based sub/unsub)
- Per-cell stream binding with market_id64 convergence
- Lazy candle loading on scroll

**Gap:** Bootstrap is WS-first. If WS connects but backend isn't ready, client subscribes anyway and gets nothing or errors. No pre-flight readiness check.

## Stage 20 Architecture

### Vertical slice definition

The **minimum viable vertical slice** that proves end-to-end integration:

```
HTTP Bootstrap                    WS Realtime
     |                                |
GET /api/v1/session          subscribe candle/stats/trades
     |                                |
     v                                v
[Session Store]               [Candle/Stats/Trades Stores]
  - server_time                       |
  - ready: bool                       v
  - markets[]               [Candle Widget + Stats Widget + Trades Widget]
  - capabilities                      |
     |                                v
     +-------> gate WS connect -------+
               (only if ready)
```

Plus two enhancers that consume S19 surfaces:

```
GET /api/v1/freshness?venue=X&instrument=Y
     |
     v
[Freshness badge in connection panel / stream status]

GET /api/v1/timeline?venue=X&instrument=Y&timeframe=1m&artifact=candle
     |
     v
[Smart GetRange: request from first_ts to last_ts instead of blind window]
```

### Bootstrap sequence (target)

```
1. App init
2. HTTP: GET /api/v1/session
   - Parse: server_time, ready, markets[], capabilities
   - Populate Markets_Store (replace defaults if backend reachable)
   - Store server_time for clock sync
   - If !ready: show "Backend not ready" in connection panel, skip WS
3. WS: Connect to /ws (only if session.ready == true)
   - Hello handshake (existing)
   - For each cell binding: reconcile_subscriptions (existing)
4. HTTP: GET /api/v1/freshness?venue=X&instrument=Y (for active market)
   - Parse: channels[].flowing, lag_ms
   - Update stream_controller status from server-side perspective
5. HTTP: GET /api/v1/timeline?venue=X&instrument=Y&timeframe=1m&artifact=candle
   - Parse: first_ts, last_ts
   - Use last_ts as GetRange end_ts (instead of now_ms)
   - Use first_ts to know when to stop requesting older candles
6. WS: subscribe → snapshot → live events (existing flow, unchanged)
```

### What changes vs what stays

**STAYS UNCHANGED:**
- All WS message parsing (message_parser.odin, frames)
- All store push/apply logic (marketdata.odin)
- All widget rendering (candle_widget, stats_widget, trades_widget)
- Reconciliation logic (reconcile.odin)
- Stream controller desync detection
- Market_Store / data_source market_id64 routing
- Settings persistence (V5 layout format)

**CHANGES (client):**
- `Marketdata_Port`: add `fetch_session`, `fetch_freshness`, `fetch_timeline` procs
- `app.init()`: call `fetch_session` before WS connect, gate on readiness
- `app.odin`: new `Bootstrap_State` struct (server_time, ready, capabilities)
- `market_discovery.odin`: add `session_parse_json` to extract markets from session response
- Web platform: wire 3 new HTTP foreign calls through `http_get_sync`
- Native platform: wire 3 new HTTP GET calls through TCP
- Connection panel: show readiness status from bootstrap
- GetRange logic: use timeline first_ts/last_ts for smarter seeding

**CHANGES (backend):**
- None. All surfaces are already deployed and frozen.

### Layered integration plan

**Layer 1 -- Transport (HTTP bootstrap port)**
- Extend `Marketdata_Port` with 3 new optional procs
- Implement in web (via `http_get_sync`) and native (via TCP GET)
- Pattern: same as existing `fetch_markets` -- synchronous, buffer-based

**Layer 2 -- Bootstrap state store**
- New `Bootstrap_State` struct in app state
- Populated from `GET /api/v1/session` response
- Fields: `server_time_ms`, `ready`, `markets_loaded`, `capabilities_loaded`

**Layer 3 -- Freshness integration**
- New `Freshness_State` struct per active market
- Populated from `GET /api/v1/freshness` after WS connect
- Feeds into stream_controller health display

**Layer 4 -- Timeline-aware GetRange**
- Parse timeline response into `first_ts` / `last_ts` per artifact
- Modify `request_active_stream_candle_range` to use `last_ts` as end bound
- Modify `check_lazy_candle_loading` to stop at `first_ts`

**Layer 5 -- UI integration**
- Connection panel: show "Ready" / "Not Ready" badge from bootstrap
- Stream status: show freshness lag_ms from server
- Candle scroll: respect timeline bounds (no requests before first_ts)

## Vertical Slice Plan (Ordered Increments)

### Slice 1: Session bootstrap port + Markets from session

**Goal:** Replace `fetch_markets` with `fetch_session` as primary bootstrap. Fall back to `fetch_markets` if session unavailable.

**Changes:**
1. Add to `Marketdata_Port`:
   ```
   fetch_session: proc(buf: [^]u8, cap: i32) -> i32
   ```
2. Add `Bootstrap_State` to `App_State`:
   ```
   Bootstrap_State :: struct {
       server_time_ms: i64,
       ready:          bool,
       has_session:    bool,
   }
   ```
3. Add `session_parse_json` to `market_discovery.odin` -- extracts markets from session response AND populates bootstrap state
4. Modify `app.init()`:
   - Try `fetch_session` first
   - Parse session → populate markets + bootstrap state
   - Fall back to `fetch_markets` if session fails
   - Gate auto-connect on `bootstrap.ready`
5. Web platform: implement `web_fetch_session` (same pattern as `web_fetch_markets`, URL: `/api/v1/session`)
6. Native platform: implement `native_fetch_session` (same TCP pattern, path: `/api/v1/session`)

**Validation:** Markets_Store populated from session endpoint. Backend-not-ready blocks WS connect.

### Slice 2: Freshness-aware stream status

**Goal:** After WS connects, fetch freshness for active market and display in connection panel.

**Changes:**
1. Add to `Marketdata_Port`:
   ```
   fetch_freshness: proc(buf: [^]u8, cap: i32, venue: string, instrument: string) -> i32
   ```
2. Add `Freshness_State` to `App_State`:
   ```
   Freshness_State :: struct {
       active:     bool,
       channels:   [CHANNEL_COUNT]Channel_Freshness,
       checked_at: i64,
   }
   Channel_Freshness :: struct {
       flowing: bool,
       lag_ms:  i64,
   }
   ```
3. Call `fetch_freshness` after first successful WS subscribe ack (or on a 10s cadence)
4. Display freshness status in connection panel (green=flowing, yellow=stale, red=inactive)
5. Feed lag_ms into stream_controller health heuristic

**Validation:** Connection panel shows per-channel flow status from backend.

### Slice 3: Timeline-driven GetRange

**Goal:** Use timeline API to bound historical candle requests with actual data availability.

**Changes:**
1. Add to `Marketdata_Port`:
   ```
   fetch_timeline: proc(buf: [^]u8, cap: i32, venue: string, instrument: string, timeframe: string) -> i32
   ```
2. Add `Timeline_State` to `App_State`:
   ```
   Timeline_State :: struct {
       first_ts: i64,
       last_ts:  i64,
       loaded:   bool,
   }
   ```
3. Call `fetch_timeline` after session bootstrap, for active market + active TF
4. Modify `request_active_stream_candle_range`:
   - Use `timeline.last_ts` as `end_ts` parameter (instead of `now_ms`)
   - Skip request entirely if `timeline.first_ts == 0` (no data available)
5. Modify `check_lazy_candle_loading`:
   - Stop requesting older candles when `getrange.oldest_ts <= timeline.first_ts`

**Validation:** GetRange requests bounded by actual data availability. No wasted requests for empty time ranges.

## Risks

1. **HTTP latency on startup**
   - Risk: Session + timeline HTTP calls add 100-300ms to bootstrap.
   - Mitigation: Fire in parallel where possible. Session is single call (replaces markets). Timeline can be deferred to post-WS-connect.

2. **Session endpoint unavailable (backend not started)**
   - Risk: Client blocks forever waiting for session.
   - Mitigation: 3s timeout on HTTP calls. Fall back to `fetch_markets` defaults. Show "Backend unreachable" status.

3. **Freshness polling overhead**
   - Risk: Frequent HTTP polling wastes resources.
   - Mitigation: 10s cadence minimum. Only poll for active market. Skip if WS metrics already flowing.

4. **Timeline stale data**
   - Risk: Timeline returns old first_ts/last_ts if cold storage lags.
   - Mitigation: Use timeline as hint only. Still accept GetRange responses beyond timeline bounds.

5. **Port explosion**
   - Risk: 3 new procs on Marketdata_Port increases surface area.
   - Mitigation: All optional (nil-check pattern). Same buffer-based API as fetch_markets. No new abstraction layers.

## Validation

All 3 slices compile clean across all targets:

```
make check-core          → all packages OK
make check-wasm-compile  → OK
go build ./cmd/server/   → clean
go test ./internal/interfaces/http/... -count=1 -short → PASS
```

### Delivered files (all slices)

| File | Slice | Change |
|------|-------|--------|
| `core/ports/marketdata.odin` | 1-3 | +3 port procs (fetch_session, fetch_freshness, fetch_timeline) |
| `core/app/app.odin` | 1-3 | +Bootstrap/Freshness/Timeline_State, session bootstrap in init(), poll_freshness in update() |
| `core/app/top_bar.odin` | 1-2 | "NOT READY" badge, "FLOWING"/"STALE" freshness badge |
| `core/app/health.odin` | 2-3 | +poll_freshness (10s cadence), +fetch_timeline_for_active |
| `core/app/stream_views.odin` | 3 | Timeline-bounded GetRange, timeline refetch on TF switch |
| `core/app/marketdata.odin` | 3 | Timeline boundary in per-cell lazy loading |
| `core/services/market_discovery.odin` | 1-3 | +session_parse_json, +freshness_parse_json, +timeline_parse_json |
| `platform/web/marketdata_web.odin` | 1-3 | +web_http_get shared, +3 fetch impls |
| `platform/native/marketdata_native.odin` | 1-3 | +native_http_get shared, +3 fetch impls |

**Zero backend changes.** All S15-S19 surfaces consumed as-is.

## Constraints (Held)

- No new wire contracts created
- No trading surface expansion
- No exchange integration changes
- No client-side federation/readiness/freshness logic duplication
- No behavioral changes to existing WS protocol handling
- All S15-S19 frozen contracts preserved

## Recommended Next Slice (Post-S20)

1. **S20.1:** Catalog-driven widget enablement -- use `GET /api/v1/catalog` to disable widgets for unavailable artifacts
2. **S20.2:** Instrument Overview in stream picker -- show per-instrument readiness status from `GET /api/v1/instrument/overview`
3. **S20.3:** Session Dashboard as app-level health bar -- aggregate readiness/freshness/resync from `GET /api/v1/session/dashboard`
4. **S21:** Second vertical slice -- Orderbook + DOM + Heatmap (richer data types, snapshot-heavy)
