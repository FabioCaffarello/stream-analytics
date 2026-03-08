# Stage S90 — Freshness Semantics & Empty-History UX

**Date**: 2026-03-08
**Branch**: `codex/s9-legacy-removal-cutover`

---

## Problem

On a fresh stack (no historical data in TimescaleDB), the UI showed a persistent "STALE" badge in the top bar and misleading status messages like "LIVE (no data): snapshot pending". This was technically correct (the backend freshness endpoint reports `active: false` when no data has accumulated), but the UX was confusing — users couldn't distinguish "system is seeding and working normally" from "data pipeline is degraded."

**BUG-6 from S85**: STALE badge + "snapshot pending" + CTX:LIVE_ONLY on fresh stack.

---

## Root Cause

The freshness badge (top bar) had a binary model: `active ? "FLOWING" : "STALE"`. On a fresh stack:
- Backend `/api/v1/freshness` returns `active: false` (no accumulated data yet)
- Composition is `Live_Only` (live candles flow, but GetRange finds no historical data)
- The badge displayed "STALE" (yellow) — implying degradation when there was none

Similarly, status messages like `"LIVE (no data): stats pending"` and `"LIVE (no data): snapshot pending"` sounded alarming rather than informational.

---

## Changes

### 1. Three-state freshness badge (top_bar.odin)

The freshness badge now has three states:
- **FLOWING** (green): Backend reports `active: true` — data pipeline healthy
- **SEEDING** (muted white): Backend reports `active: false` but composition is `Empty`, `Range_Pending`, or `Live_Only` — no historical data yet, system is warming up
- **STALE** (yellow): Backend reports `active: false` with `Backfilled` or `Composed` composition — had data, now degraded

### 2. Better status messages (build_status.odin)

| Before | After |
|--------|-------|
| `"LIVE (no data): stats pending"` | `"Awaiting stats..."` |
| `"LIVE (no data): snapshot pending"` | `"Awaiting snapshot..."` |
| `"LIVE (no data): orderbook pending"` | `"Awaiting orderbook..."` |
| `"snapshot pending"` (in LIVE_ONLY context) | `"seeding (no history)"` |

### 3. Channel count tracking (app.odin, health.odin)

`Freshness_State` now stores `channel_count` from the backend response, enabling future use for distinguishing "backend has no channels" from "channels exist but not flowing."

---

## Files Changed

| File | Change |
|------|--------|
| `app.odin` | Added `channel_count` field to `Freshness_State` |
| `health.odin` | Store `result.count` in freshness state |
| `top_bar.odin` | Three-state freshness badge with composition-aware logic |
| `build_status.odin` | Improved wait messages, composition-aware reason_short |

---

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| No false-negative STALE badge on fresh stack | DONE — shows "SEEDING" instead |
| User understands absence of history vs degraded flow | DONE — distinct labels + messages |
| HUD/status bar aligned with real system state | DONE — reason_short reflects composition |
| No regressions on established stacks | DONE — Composed/Backfilled still show "STALE" correctly |

---

## Architecture Notes

The fix leverages the existing `Composition_Stage` enum as the discriminator:
- `Empty` / `Range_Pending` / `Live_Only` → **no historical data** (fresh stack or cold start)
- `Backfilled` / `Composed` → **historical data present** (if backend says not active → real staleness)

This avoids adding new backend endpoints or protocol changes. The composition stage is already the single source of truth for data lifecycle state (established in S22–S26).

---

## Zero Regressions

- All pre-existing compile warnings unchanged
- Health system (`stream_health_level`, `apply_state_stale_artifact_count`) already correctly handles fresh stacks via `event_count == 0` early exits
- Auto-recovery system unaffected (only triggers on `Dual_Silence` artifacts with `event_count > 0`)
