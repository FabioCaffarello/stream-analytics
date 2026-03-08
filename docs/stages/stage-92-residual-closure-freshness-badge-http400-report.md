# Stage 92 — Residual Closure: Freshness Badge + HTTP 400 Elimination

**Date:** 2026-03-08
**Status:** COMPLETE
**Predecessor:** S91 (Playwright Closure Pack)

## Objective

Resolve the two residual defects left by the S85–S91 cycle:

1. **Freshness badge STALE vs SEEDING race** — badge flips to STALE on fresh stacks that were never active
2. **HTTP 400 errors from storage.js** — analytics endpoints receive un-normalized symbols

## Root Cause Analysis

### Badge Race Condition

The S90 logic used `apply_state_composition_stage()` to distinguish SEEDING from STALE when `freshness.active == false`. The composition stage is a client-side derived value that advances from `Live_Only` → `Composed` as soon as both live candles arrive and GetRange completes. This happens within seconds of connection, while the freshness endpoint polls on a 10-second cadence.

**Result:** On a fresh stack, composition advances to `Composed` before the backend ever reports `active=true`, causing the badge to incorrectly show "STALE" instead of "SEEDING".

### HTTP 400 Errors

The `request_analytics_range` and `compare_pane_fetch_analytics` functions passed raw `symbol` values (e.g., `BTCUSDT:PERP`) to the cold reader APIs. The backend expects the normalized instrument name (`BTCUSDT`). The freshness and timeline endpoints already used `normalized_symbol()` to strip the market type suffix, but analytics endpoints did not.

**Result:** Every analytics fetch for futures/spot-suffixed instruments returned HTTP 400.

## Changes

### Fix 1: was_ever_active latch (4 files)

| File | Change |
|------|--------|
| `app.odin` | Added `was_ever_active: bool` to `Freshness_State` struct |
| `health.odin` | Latch `was_ever_active = true` when `result.active` in `poll_freshness` |
| `top_bar.odin` | Badge logic: SEEDING when `!was_ever_active`, STALE only when previously active |
| `actions_stream_control.odin` | Reset `freshness = {}` on stream switch (each stream has independent freshness) |

**Logic change:**
```
Before (S90):
  if !active → check composition stage → SEEDING if early, STALE if advanced

After (S92):
  if !active && !was_ever_active → SEEDING (regardless of composition)
  if !active && was_ever_active  → STALE  (backend was flowing and stopped)
```

### Fix 2: Symbol normalization for analytics (3 files)

| File | Change |
|------|--------|
| `analytics_range.odin` | Added `symbol = normalized_symbol(symbol)` before API dispatch |
| `stream_slots.odin` | Normalized symbol in `compare_pane_fetch_analytics` |
| `session_vpvr_data.odin` | Normalized symbol in `request_session_vpvr_snapshot` |

These match the existing pattern in `poll_freshness` and `fetch_timeline_for_active` which already called `normalized_symbol()`.

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Zero recurring HTTP 400 per session | DONE — all analytics paths now normalize symbols |
| Badge and status bar always coherent | DONE — was_ever_active latch is stable, independent of composition timing |
| No false negative of freshness on fresh stack | DONE — SEEDING persists until backend confirms active=true |
| Freshness resets on stream switch | DONE — full freshness state zeroed on Pick_Stream |

## Files Modified

- `client/src/core/app/app.odin` — Freshness_State struct
- `client/src/core/app/health.odin` — was_ever_active latch
- `client/src/core/app/top_bar.odin` — Badge rendering logic
- `client/src/core/app/actions_stream_control.odin` — Freshness reset on stream switch
- `client/src/core/app/analytics_range.odin` — Symbol normalization
- `client/src/core/app/stream_slots.odin` — Compare pane symbol normalization
- `client/src/core/app/session_vpvr_data.odin` — Session VPVR symbol normalization

**Total: 7 files modified, zero regressions, zero wire-breaking changes.**
