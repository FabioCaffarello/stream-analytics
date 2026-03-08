# Stage 89 — Fresh Boot Connectivity & Bootstrap Resilience

**Date:** 2026-03-08
**Status:** COMPLETE
**Triggered by:** S85 Playwright validation exposed fresh-browser failures

## Problem Statement

A fresh browser (no localStorage) could not auto-connect, Session Health failed on
first load, and page pollers were gated on WebSocket connection status despite using
independent HTTP endpoints.

## Changes

### 1. Auto-connect defaults to ON for fresh browsers

**File:** `app.odin` (init auto-connect gate)

Previously, `SETTING_AUTO_CONNECT` defaulted to `""` (empty) when no localStorage
existed. The gate checked `== "1"`, so fresh browsers never auto-connected.

**Fix:** Invert the logic — treat missing/empty value as ON, only skip when
explicitly `"0"` (set by disconnect action). The settings toggle in `settings.odin`
was updated to match (`!= "0"` instead of `== "1"`).

Also added `jwt_token` pass-through to `reconnect_transport` for profile parity
with `apply_connect_profile_action`.

### 2. Session Health race condition fix

**File:** `build_session_health.odin`

The `poll_session_health` required `current_conn_status == .Connected`, but the
HTTP endpoint `/api/v1/session/dashboard` is independent of WebSocket state.

**Fix:** Removed the `.Connected` guard. Added `HEALTH_RETRY_INTERVAL` (300 frames
≈ 5s) for auto-retry on error, so the page self-heals without manual click.

### 3. HTTP poller connection guards removed

**Files:** `build_instrument_overview.odin`, `build_markets.odin`

Same pattern as Session Health — these pages fetch from HTTP endpoints that don't
require WebSocket connectivity.

**Fix:** Removed `.Connected` guard from `poll_instrument_overview` and
`poll_explorer`. Added retry intervals for error recovery on both pages.

### 4. Console noise reduction

**File:** `websocket.js`

During reconnection cycles (e.g., backend not running), every failed WS attempt
produced `console.error("[ws] error")` + `console.log("[ws] closed code=...")`.

**Fix:**
- Track `consecutiveErrors` counter
- First failure: `console.warn("[ws] connection failed — will retry with backoff")`
- Subsequent: suppress, log every 10th attempt
- On close during reconnect: suppress redundant close logs
- On successful connect after failures: log recovery with attempt count
- Reset counter on successful connection

## Files Modified

| File | Change |
|------|--------|
| `core/app/app.odin` | Auto-connect default ON, jwt_token, explorer retry interval |
| `core/app/settings.odin` | Toggle display consistency (`!= "0"`) |
| `core/app/build_session_health.odin` | Remove Connected guard, add retry interval |
| `core/app/build_instrument_overview.odin` | Remove Connected guard, add retry interval |
| `core/app/build_markets.odin` | Remove Connected guard, add retry interval |
| `web/modules/websocket.js` | Suppress repeated WS error logs |

## Verification

- `make check-core` — all packages OK
- `make check-wasm-compile` — OK
- `make check-core-imports` — OK

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Fresh browser (no localStorage) enters healthy flow | DONE — auto-connect ON by default |
| Session Health loads on first visit | DONE — no WS gate, auto-retry on error |
| Console without recurring error spam | DONE — suppressed after first warning |
| HTTP page pollers work without WS | DONE — 3 pollers decoupled from WS state |
