# Stage 58 — Instrument Overview Page + Adoption of Backend Read Models

**Date:** 2026-03-08
**Status:** COMPLETE

## Mission

Create the first UI surface truly oriented by a backend-owned composed read model,
establishing the pattern for consuming canonical backend state instead of reconstructing
instrument views from scattered client-side sources.

## Architecture Decisions

### Backend Source: Single Composed Endpoint
- **`GET /api/v1/instrument/overview?venue=X&instrument=Y`** — already existed, no backend changes needed
- Returns readiness, freshness (per-channel), resync diagnostics, and artifact timeline summaries
- Response is a composed read model with classified status (`ready|degraded|not_ready|inactive`)

### Client Architecture
- **Route:** `Instrument_Overview` added as 4th route in `Route` enum (contextual drilldown, no nav rail item)
- **Page Module:** Full lifecycle — `on_enter` resolves target instrument, `on_leave` clears state
- **View Model:** `Instrument_Overview_Result` in services layer, parsed from backend JSON
- **No raw payload coupling:** Renderer consumes parsed view model, never touches JSON bytes
- **Periodic polling:** ~10s cadence (600 frames), consistent with freshness polling pattern

### Navigation
- **From Markets page:** ">" button on each active market row → `Navigate_Instrument_Overview` action
- **Back navigation:** "← Markets" link on overview page + Escape key → returns to Markets
- **Nav rail:** 3 items unchanged (D/V/G); overview doesn't appear in nav rail (contextual page)
- **Active stream binding:** Overview shows the active stream's instrument, switching active stream first if needed

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `client/src/core/app/build_instrument_overview.odin` | Page render, detail panel, fetch, navigation |
| `client/src/core/services/instrument_overview.odin` | JSON parser + `Instrument_Overview_Result` view model |
| `client/src/core/services/instrument_overview_test.odin` | 7 tests for JSON parser |

### Modified Files
| File | Change |
|------|--------|
| `client/src/core/app/app.odin` | Route enum +1, `Instrument_Overview_State` struct, poll wiring |
| `client/src/core/app/page_module.odin` | PAGE_MODULES table +1 entry |
| `client/src/core/app/actions.odin` | `Navigate_Instrument_Overview` action + Escape handling |
| `client/src/core/app/build_markets.odin` | ">" overview button on active market rows |
| `client/src/core/ports/marketdata.odin` | `fetch_instrument_overview` port function |
| `client/src/platform/native/marketdata_native.odin` | Native fetch implementation |
| `client/src/platform/web/marketdata_web.odin` | Web fetch implementation |

## Page Sections

The Instrument Overview page renders 5 sections from the backend read model:

1. **STATUS** — Overall classified status badge + checked-at age
2. **READINESS** — Guardian readiness (`ready|not_ready`)
3. **FRESHNESS** — Per-channel flow health with flowing/stale dots and lag metrics
4. **RESYNC DIAGNOSTICS** — Stream count, resync events, drops, max lag
5. **ARTIFACTS** — Per-artifact timeline (span, age, status), available timeframes, endpoints

## State Handling

| State | Rendering |
|-------|-----------|
| **Loading** | "Loading..." placeholder |
| **Error** | Warning message + "Retry" link |
| **Empty** (no instrument) | "No instrument selected" message |
| **Success** | Full 5-section render with backend data |
| **Partial** (no channels/artifacts) | Section renders "no channels" / "no artifacts" gracefully |

## What Moved from Client-Improvised to Backend-Owned

| Before (S57) | After (S58) |
|--------------|-------------|
| Client reconstructed readiness from `bootstrap.ready` + connection state | Backend `readiness.status` from guardian query |
| Client polled `/api/v1/freshness` and mapped channels manually | Backend `freshness.channels` with per-channel flow/lag |
| Client had no resync visibility per instrument | Backend `resync` with stream count, totals, max lag |
| Client had no artifact timeline visibility | Backend `artifacts[].timeline` with first/last ts + status |
| Client assembled overview from 3+ scattered HTTP calls | Single `GET /api/v1/instrument/overview` call |

## Test Results

- **7 new parser tests** — minimal, channels, artifacts, empty, invalid, nil, not_ready/inactive
- **402 md_common tests** — all pass, zero regressions
- **92 services tests** — all pass (85 existing + 7 new)
- **check-core:** all packages OK
- **check-core-imports:** OK
- **check-wasm-compile:** OK
- **build-native:** OK (4.3MB binary)

## Metrics

- **New code:** ~420 lines (page render + parser + tests)
- **Modified code:** ~30 lines across 7 existing files
- **Backend changes:** 0 (endpoint already existed)
- **Shell impact:** 0 lines changed in `build_ui.odin`
- **Wire protocol changes:** 0

## What This Stage Establishes

1. **Pattern for backend read model adoption** — parse once, render from view model
2. **Contextual page navigation** — Route exists without nav rail item
3. **Lifecycle-aware data fetching** — on_enter triggers fetch, periodic poll while active
4. **Graceful multi-state rendering** — loading/error/empty/partial/success gates
5. **No backend coupling in renderer** — page never touches raw JSON or HTTP
