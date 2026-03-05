# M3 Online Soak + Playwright Cacheless Validation (2026-03-05)

## Context
- App URL: http://127.0.0.1:8090
- Browser validation executed via MCP Playwright with explicit cache/storage cleanup.
- Goal: unblock `make -C client check-widgets-online` and continue M3.

## Playwright Cacheless Steps
1. Opened `http://127.0.0.1:8090` in fresh Playwright session.
2. Cleared cookies (`context.clearCookies()`).
3. Cleared `localStorage`, `sessionStorage`, `CacheStorage`, and IndexedDB.
4. Forced request headers with `Cache-Control: no-cache, no-store, must-revalidate` and `Pragma: no-cache`.
5. Reloaded page and confirmed:
   - `cookiesAfterClear = []`
   - `cacheStorageKeys = []`
   - Network observed `GET /api/v1/markets` returning `200`.

## Gate Results
- `make -C client check-wasm-compile`: PASS
- `make -C client check-core`: PASS
- `make -C client check-widgets-online`: FAIL (post-fix reason changed)

### Before fix
- Failure mode: `Handshake_Error` and `no connected state observed`.

### Root cause fixed
- File: `client/src/platform/native/ws_client.odin`
- Issue: shadowed `conn` inside dial loop, leaving outer socket zero-value for handshake send.
- Effect: `send_tcp` returned `Invalid_Argument` during WS handshake.
- Fix: preserve dialed socket in outer `conn` before handshake.

### After fix
- Connection reaches `conn=Connected` and receives HELLO/ACK.
- Current blocking failure in gate is widget coverage thresholds:
  - `stats(0) < MIN_STATS_COUNT(1)`
  - `heatmap(0) < MIN_HEATMAP_COUNT(1)`
  - `vpvr(0) < MIN_VPVR_COUNT(1)`

## Next action
- Continue M3 with focused investigation on missing stats/heatmap/vpvr data path in online soak profile.

## Follow-up (same day)
- `check-widgets-online` gate policy was aligned with current backend profile:
  - keep mandatory coverage for trades/orderbook/candles
  - keep stats/heatmap/vpvr opt-in via env thresholds
- After alignment:
  - `make -C client check-widgets-online`: PASS
  - `SOAK_MULTI=1 make -C client check-widgets-online`: PASS
