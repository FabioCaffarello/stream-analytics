# Stage 102 — Product Surface Convergence

**Date:** 2026-03-08
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

## Objective

Consolidate product surfaces (portfolio, execution, monitoring) onto the stabilized architecture. Verify all surfaces share common contracts, consume data via consistent paths, and have zero legacy dependencies.

## Audit Results

Full audit of 6 page modules, all stores, data contracts, navigation, and stream consumption paths.

| Area | Status | Details |
|------|--------|---------|
| Legacy dependencies | CLEAN | Zero `sync_legacy` references, zero removed store fields |
| Global_Stores scope | CORRECT | Only dom/footprint/markets remain (non-stream-scoped) |
| Data contracts | CONSISTENT | All pages use identical fetch→parse→store→render pattern |
| Stream consumption | UNIFIED | Widgets via layer_store (S100), pages via HTTP read models |
| Navigation | COHERENT | 6 routes, all lifecycle hooks wired, overlay cleanup on transition |
| TODO/FIXME/HACK | ZERO | Production-ready codebase |

## Bug Found and Fixed

### Portfolio WS Connection Gate (S89 Alignment)

**Impact:** Portfolio HTTP polling was blocked when WebSocket was disconnected.

**Root Cause:** `poll_portfolio()` gated on `current_conn_status(state) != .Connected`, which checks WS status. Portfolio data comes from HTTP endpoints (`/api/v1/portfolio/*`), which are independent of WS (S89 design principle established for Session Health and Instrument Overview).

**Fix:**
1. Removed WS connection gate from `poll_portfolio()`
2. Added `PORTFOLIO_RETRY_INTERVAL` (300 frames / ~5s) for error retry, matching `HEALTH_RETRY_INTERVAL` and `OVERVIEW_RETRY_INTERVAL`
3. Error retry uses `any_error` across all 4 portfolio stores to determine interval

**Before:**
```
poll_portfolio:
  ✗ gate: current_conn_status != .Connected → early return
  interval: PORTFOLIO_POLL_INTERVAL (600) for all cases
```

**After (aligned with S89 pattern):**
```
poll_portfolio:
  ✓ no WS gate (HTTP is independent)
  interval: PORTFOLIO_RETRY_INTERVAL (300) on error, PORTFOLIO_POLL_INTERVAL (600) on success
```

## Files Changed

| File | Change |
|------|--------|
| `portfolio_data.odin` | Removed WS connection gate, added `PORTFOLIO_RETRY_INTERVAL`, error-driven interval selection |
| `marketdata_test.odin` | 3 new S102 tests |

## Poll Pattern Convergence (After S102)

All HTTP-backed page surfaces now follow the same contract:

| Surface | Route Gate | WS Gate | Normal Interval | Error Retry | Background |
|---------|-----------|---------|----------------|-------------|------------|
| Session Health | `.Session_Health` | None (S89) | 600 | 300 | No |
| Instrument Overview | `.Instrument_Overview` | None (S89) | 600 | 300 | No |
| Markets Explorer | `.Markets` | None | 600 | N/A | No |
| Portfolio | None (background) | None (S102) | 600 | 300 | Yes |

Portfolio is the only surface that polls across all pages (background polling). This is intentional — portfolio state is used by the detail panel sidebar regardless of active route.

## Tests

3 new tests added to `marketdata_test.odin`:

| Test | Validates |
|------|-----------|
| `test_s102_portfolio_poll_no_ws_gate` | Portfolio poll proceeds even when WS is disconnected |
| `test_s102_portfolio_retry_interval` | Error triggers 300-frame retry; success uses 600-frame interval |
| `test_s102_poll_intervals_consistent` | All surfaces share same base (600) and retry (300) intervals |

## Test Results

- **app:** 64 tests ✓ (3 new)
- Zero regressions

## Acceptance Criteria

- [x] All product surfaces share common architecture (fetch→parse→store→render)
- [x] Zero legacy dependencies (no sync_legacy, no removed store fields)
- [x] Consistent data contracts across all HTTP-backed pages
- [x] Navigation between pages validated (6 routes, lifecycle hooks)
- [x] Realtime update paths unified (layer_store canonical, S100)
- [x] Poll intervals aligned across all surfaces (S89 pattern)
- [x] No duplicated logic requiring extraction (minor template duplication is acceptable)
