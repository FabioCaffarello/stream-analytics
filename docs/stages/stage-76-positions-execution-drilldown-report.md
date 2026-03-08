# Stage 76 — Positions & Execution Drilldown

**Date:** 2026-03-08
**Status:** COMPLETE

## Summary

Added operational drilldown views to the Portfolio page with tabbed navigation
(Positions / Exposure / Fill Stats), venue filtering, stale position detection,
and exposure divergence warnings.

## Changes

### App State (`app.odin`)
- Added `Portfolio_Tab` enum: `Positions`, `Exposure`, `Fill_Stats`
- Added `venue_filter` bounded buffer (24 bytes) + `venue_filter_len` to `Portfolio_Data_State`
- Added `active_tab` field to `Portfolio_Data_State`

### Service Layer (`portfolio_store.odin`)
- `Portfolio_Symbol_Exposure` struct — aggregated exposure per symbol across venues
- `portfolio_compute_symbol_exposures()` — computes cross-venue symbol exposure from account snapshot
- `portfolio_position_is_stale()` — checks if position's last fill exceeds threshold
- `portfolio_has_exposure_divergence()` — detects opposing positions on same symbol across venues

### UI (`build_portfolio.odin`)
- **Tab bar**: Positions / Exposure / Fill Stats with active state, hover, click handling
- **Venue filter**: Click venue header to filter; clear button in tab bar; filter indicator
- **Positions tab**: Enhanced with Status column (STALE/LIVE), venue margin display, stale position highlighting (warning color on symbol)
- **Exposure tab**: Two sections — "Exposure by Venue" (equity, margin, PnL, position count) + "Exposure by Symbol" (net qty, gross notional, venue count with multi-venue warning)
- **Fill Stats tab**: 9 metric cards in 3x3 grid (trades, wins, losses, volume, largest win/loss, turnover, win rate, win/loss ratio) + "Recent Fills by Venue" table (per-position trade count + volume)
- **Detail panel**: Added DIVERGENT indicator when exposure divergence detected
- **Stale threshold**: 5 minutes (`STALE_THRESHOLD_MS = 300,000`)

### Tests (`portfolio_store_test.odin`) — 12 new tests
- Symbol exposure computation: basic (2 venues, 3 positions), nil, empty
- Stale detection: true, false, nil, zero-fill-ms
- Exposure divergence: opposing sides (true), different symbols (false), nil, same side (false)

## Metrics
- **137 total service tests**, all passing
- **0 backend changes** — client-only, renders from existing stores
- **0 wire changes** — no new endpoints or protocols
- **0 regressions** — full check-core clean

## Architecture Decisions
- **No PnL recalculation in client** — all values rendered directly from backend stores
- **Stale detection uses projected_at_ms** as reference timestamp (backend-owned clock)
- **Exposure divergence** is a cross-venue check (same symbol, opposing sides)
- **Tab state persists** across re-navigation (not cleared on page leave)
- **Venue filter** is per-session, cleared on new navigation or explicit click
