# Stage 59 — Session Health / Diagnostics Page

**Date:** 2026-03-08
**Status:** COMPLETE
**Scope:** Client UI + port layer (no backend changes needed)

## Objective

Create a dedicated Session Health / Diagnostics page that consolidates dispersed
observability signals into a single, actionable diagnostic surface. Transforms
observability from hidden HUD overlays into a first-class product page.

## Architecture

### Data Source Strategy

The page follows a **hybrid ownership** model:

| Section | Owner | Source | Rationale |
|---------|-------|--------|-----------|
| Session status | Backend | `GET /api/v1/session/dashboard` | Backend already composes readiness, freshness, resync, artifacts |
| Connection/Transport | Client | `MD_Runtime_Metrics` | Only the client observes RTT, parse timings, protocol state, desync |
| Data freshness | Backend | `GET /api/v1/session/dashboard` | Backend tracks per-instrument flow across all channels |
| Delivery | Backend | `GET /api/v1/session/dashboard` | Backend owns stream-level resync/drop counters |
| Client health | Client | `Aggregate_Health_Summary` | Client-side composition, staleness, recovery status |
| Recovery log | Client | `Recovery_Event_Log` | Client-side auto-recovery ring buffer |
| Artifacts | Backend | `GET /api/v1/session/dashboard` | Backend owns artifact coverage matrix |

**Key decision:** No new backend endpoints needed. `GET /api/v1/session/dashboard`
already provides a composed session-level read model with all the backend-owned
diagnostics. Client-side signals (`MD_Runtime_Metrics`, `Aggregate_Health_Summary`,
`Recovery_Event_Log`) are derived locally at render time.

### Information Hierarchy

The page is organized top-to-bottom in order of diagnostic priority:

1. **SESSION** — Overall session status + readiness + venue/instrument summary
2. **TRANSPORT** — Connection state, protocol version, RTT, lag, throughput, desync, drops, parse timings, server backpressure
3. **FRESHNESS** — Active vs stale instruments, flowing vs stale channels
4. **DELIVERY** — Connections, streams, resyncs, drops, max lag
5. **CLIENT HEALTH** — Slot composition breakdown, recovery/staleness, recovery event log
6. **ARTIFACTS** — Coverage matrix per artifact type (available/empty/unavailable)

### Page Module Contract

Follows the S57 page module pattern exactly:

- **Route:** `Session_Health` (5th route, nav rail item #4 "H")
- **Lifecycle:** `on_enter` clears state + triggers initial fetch; `on_leave` clears state
- **Polling:** ~10s interval (`HEALTH_POLL_INTERVAL = 600 frames`)
- **Fetch:** `fetch_session_dashboard` port proc → 16KB buffer → `session_health_parse_json`
- **Detail panel:** Compact summary (status, venues, instruments, freshness, delivery, client health)
- **Navigation:** Nav rail "H" icon, Escape returns to Dashboard

## Files

### New Files

| File | Purpose |
|------|---------|
| `services/session_health.odin` | View model + JSON parser for `/api/v1/session/dashboard` |
| `services/session_health_test.odin` | 7 parser tests (ready, degraded, not_ready, coverage, edge cases) |
| `app/build_session_health.odin` | Page render, lifecycle, fetch, polling, detail panel, color helpers |

### Modified Files

| File | Change |
|------|--------|
| `app/app.odin` | Added `Session_Health` route, `Session_Health_State` struct, field in `App_State`, poll calls |
| `app/page_module.odin` | Registered `.Session_Health` in `PAGE_MODULES` dispatch table |
| `app/build_ui.odin` | Added "H" nav rail item, route mapping table (skips Instrument_Overview) |
| `app/actions.odin` | Escape handler for Session_Health → Dashboard |
| `ports/marketdata.odin` | Added `fetch_session_dashboard` proc to `Marketdata_Port` |
| `platform/native/marketdata_native.odin` | Implemented `native_fetch_session_dashboard` |
| `platform/web/marketdata_web.odin` | Implemented `web_fetch_session_dashboard` |

## Design Decisions

### Why not separate backend endpoints?

The session dashboard endpoint already provides a composed read model that covers
status, readiness, freshness, resync, artifacts, and summary. Using multiple
endpoints would:
- Increase HTTP round-trips
- Create timing inconsistencies between sections
- Risk the client reconstructing backend state from fragments

One fetch, one parse, one source of truth.

### Why hybrid (backend + client)?

Transport metrics (RTT, lag, parse timings, desync reason, protocol state) are
inherently client-local observations. The backend cannot know the client's
WebSocket RTT or parse performance. These signals complement the backend's
view of data flow health.

### Nav rail route mapping

With `Instrument_Overview` as a contextual page (no nav rail item), the nav rail
index no longer maps 1:1 to Route enum ordinals. A `NAV_ROUTES` mapping array
resolves this cleanly — nav rail index → Route value.

### Buffer size

Session dashboard responses can be larger than instrument overview (multiple
artifacts with coverage matrices), so the fetch buffer is 16KB vs 8KB.

## Test Coverage

- **7 new tests** in `services/session_health_test.odin`:
  - `test_session_health_parse_ready` — full happy path with all fields
  - `test_session_health_parse_degraded` — partial freshness, recovering resync
  - `test_session_health_parse_not_ready` — not_ready status, inactive freshness
  - `test_session_health_parse_artifact_coverage_partial` — partial coverage matrix
  - `test_session_health_parse_empty_data` — nil data rejection
  - `test_session_health_parse_invalid_json` — malformed JSON rejection
  - `test_session_health_parse_nil_result` — nil output rejection

## Observability: From Hidden to Product Surface

### Before S59

Operational diagnostics were scattered across:
- **Telemetry HUD** (Ctrl+D): floating panel with raw metrics, requires manual toggle
- **Health dots**: tiny colored squares on cell headers, no detail
- **Composition badges**: PEND/BFILL/LIVE/COMP labels, contextless
- **Status bar**: single connection badge
- **Runtime snapshot** (Ctrl+Shift+D): clipboard dump, requires external viewer

Diagnosing issues required combining signals from 5+ different surfaces, with no
unified view of session health.

### After S59

A single page answers:
- **Is the session healthy?** → SESSION status badge
- **Is my connection stable?** → TRANSPORT state, RTT, desync status
- **Is data flowing?** → FRESHNESS active/stale instruments and channels
- **Are there delivery issues?** → DELIVERY resyncs, drops, lag
- **Are my local streams healthy?** → CLIENT HEALTH composition, recovery log
- **What data is available?** → ARTIFACTS coverage matrix

No overlays to toggle. No clipboard dumps to parse. No mental assembly required.
