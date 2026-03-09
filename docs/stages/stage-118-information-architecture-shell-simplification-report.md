# Stage 118 — Information Architecture & Shell Simplification

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Reduce visual clutter in the dashboard shell, improve context hierarchy, and promote the chart as the dominant surface.

## Changes

### Top Bar (global context)
- **Removed** "Market Raccoon" app title text — wasted horizontal space with zero information value for an in-use application.
- Retained: MR logo, connection badge, backend readiness, error indicator, quick action buttons (?, C, F, Z, S).

### Workspace Toolbar (workspace context)
- **Removed candle health badge** — redundant with the status bar's LIVE/LAG/DESYNC health pill which provides the same (and richer) information.
- **Removed freshness badge** (FLOWING/SEEDING/STALE) — redundant with status bar health status. The freshness concept was a mid-layer heuristic that repeated what the status bar already communicates at the transport level.
- **Raised indicator pill viewport threshold** from 450px → 550px — gives the toolbar breathing room on medium viewports, preventing cramped layout where TF selector + layout presets + pills compete for space.
- Retained: hero instrument, price ticker, % change, volume, stream navigation, TF selector, layout presets (D/C/A/K/+), indicator pills, context stack toggle.

### Status Bar (transport/telemetry context)
- No changes — already provides the authoritative health signal (LIVE/LAG/DESYNC pill with background + border), RTT, lag, age, drop, reconnect counts, HUD toggle.

## Rationale

The workspace toolbar was carrying 3 redundant health signals:
1. Candle health badge (toolbar) ≈ status bar LIVE/LAG/DESYNC
2. Freshness badge (toolbar) ≈ status bar LIVE/LAG/DESYNC
3. Status bar health pill (authoritative)

By removing #1 and #2, the toolbar focuses on its core job: instrument context, timeframe selection, and indicator control. The status bar remains the single source of truth for transport health.

## Validation

- `make check-core`: all packages OK
- `odin test app`: 254 tests pass (0 failures)
- Zero regressions, zero wire-breaking changes

## Files Modified

| File | Change |
|------|--------|
| `client/src/core/app/top_bar.odin` | Removed app title text |
| `client/src/core/app/workspace_toolbar.odin` | Removed candle health badge, freshness badge; raised pill threshold |
