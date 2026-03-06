# Stage 17 -- Operational Session Surfaces

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE (Slices 1-2 delivered)

---

## Executive Summary

Stage 17 creates the first composed operational surfaces oriented toward client consumption, preparing the backend for future client evolution without starting it. After S16 (wire contract stabilization, catalog/timeline discovery), the backend has rich internal observability and 8+ HTTP endpoints — but the client must call 3+ endpoints to bootstrap and has no way to check if data is actually flowing for a given instrument.

**Mission:** Create composed read surfaces that give the client everything it needs in minimal round trips, without leaking internal details (federation, hot/cold tiers, subsystem topology, stream IDs).

**Constraints (all met):**
- No trading surface expansion
- No new domain types or streams
- No domain logic in cmd/*
- No storage/federation details leaked to client
- No client-side changes
- No breaking changes to frozen contracts

---

## 1. Current-State Audit (Post S16)

### 1.1 Existing API Surface

| Endpoint | Purpose | Client-facing? |
|---|---|---|
| GET /healthz | Liveness probe + WS age | Yes (ops) |
| GET /readyz | Subsystem readiness | Yes (ops) |
| GET /api/v1/markets | Market discovery | Yes |
| GET /api/v1/catalog | Artifact catalog | Yes |
| GET /api/v1/timeline | Time range discovery | Yes |
| GET /api/v1/candles,stats,... | Data readers (8 endpoints) | Yes |
| GET /runtime/snapshot | Full subsystem state | No (localhost) |
| GET /runtime/terminal | Per-stream lag/delivery | No (localhost) |
| GET /runtime/overload | PolicyKit backpressure | No (localhost) |
| GET /runtime/storage | Hot/cold/committer health | No (localhost) |
| GET /runtime/ws | WS session counts | No (localhost) |
| GET /api/v1/consistency | Hot/cold consistency | No (localhost) |
| POST /api/v1/control | Execution control plane | No (localhost) |

### 1.2 Key Gaps

| Gap | Impact | Resolution |
|---|---|---|
| **G1: No single bootstrap endpoint** | Client must call /readyz + /markets + /catalog separately at session init | Session Overview (Slice 1) |
| **G2: No per-instrument flow health** | Client cannot show "is data flowing?" indicators without subscribing to WS | Instrument Freshness (Slice 2) |
| **G3: Internal observability is localhost-only** | Terminal WS state has per-stream lag, but client cannot access it | Freshness composes and exposes relevant subset |

### 1.3 What Works Well (No Change Needed)

| Surface | Assessment |
|---|---|
| Wire format | Frozen in S16 (8 golden tests, explicit json tags) |
| Catalog API | Config-derived, 8 artifact types, sorted, deduplicated |
| Timeline API | Federated first_ts/last_ts, hides hot/cold |
| WS delivery | 11 invariants, snapshot-before-delta guaranteed |
| Federation | Time-based routing, O(n) merge, 23 tests |
| Consistency checker | Hot/cold timestamp comparison, localhost-only |

---

## 2. Stage 17 Architecture

### 2.1 Design Decisions

**D1: Session Overview composes existing data, adds zero storage queries**

The session overview combines:
- Server time (for clock skew detection)
- Guardian readiness (engine.Request to existing handler)
- Markets config (already loaded in memory)
- Artifact catalog (domain constants)

No new ports, no new storage queries, no new domain types. Pure composition.

**D2: Freshness composes terminal WS state, hides internals**

The freshness endpoint reads from the existing `observability.SnapshotTerminalWSState()` which tracks per-stream delivery state (venue, symbol, channel, last_ts, lag_ms). The handler:
- Filters by venue/instrument
- Groups by channel
- Computes "flowing" from a 30-second staleness threshold
- Returns a client-friendly view without stream IDs or internal metrics

**D3: No new domain types, no new ports**

Both surfaces compose existing data through existing infrastructure. No changes to core domain, aggregation, or storage layers.

**D4: Backend vs Client responsibility**

| Responsibility | Backend (S17) | Client (future) |
|---|---|---|
| Session bootstrap | Single /api/v1/session call | Parse and configure from response |
| Data flow indicators | /api/v1/freshness per instrument | Show green/yellow/red per market |
| Historical data | Existing federated readers | Local caching, pagination |
| Real-time delivery | Existing WS contract | Gap detection, retry |

**D5: Freshness is public (not localhost-only)**

Unlike `/runtime/terminal` which exposes raw stream IDs and internal counters, `/api/v1/freshness` only exposes per-channel flow health (last_event_ts, lag_ms, flowing). This is safe for client consumption.

### 2.2 New API Surface

```
GET /api/v1/session
-> {
     "server_time_ms": 1710000000000,
     "ready": true,
     "markets": [
       {"venue": "binance", "instruments": ["BTCUSDT", "ETHUSDT"]}
     ],
     "capabilities": {
       "artifacts": [
         {"name": "candle", "endpoint": "/api/v1/candles", "timeframes": ["1s","5s","1m",...]},
         ...
       ]
     }
   }

GET /api/v1/freshness?venue=binance&instrument=BTCUSDT
-> {
     "venue": "binance",
     "instrument": "BTCUSDT",
     "active": true,
     "channels": {
       "candle": {"last_event_ts": 1710000000000, "lag_ms": 50, "flowing": true},
       "stats": {"last_event_ts": 1710000000000, "lag_ms": 30, "flowing": true}
     },
     "checked_at": 1710000000100
   }
```

### 2.3 What Does NOT Change

- No new domain types or streams
- No federation changes
- No storage changes
- No WS protocol changes
- No client changes
- No frozen contract modifications

---

## 3. Prioritized Slices

```
Slice 1: Session Overview                                         [COMPLETE]
  - GET /api/v1/session composed read model
  - Combines server time, guardian readiness, markets, artifact capabilities
  - Single call replaces /readyz + /markets + /catalog at bootstrap
  - 6 unit tests (happy path, no markets, dedup, sorted, empty name, shape)
  - Wired in server.go, route gated by markets config

Slice 2: Instrument Freshness                                     [COMPLETE]
  - GET /api/v1/freshness?venue=X&instrument=Y
  - Composes terminal WS stream state into per-channel flow health
  - 30-second staleness threshold for "flowing" indicator
  - Case-insensitive venue/instrument matching
  - 6 unit tests (missing params, no streams, flowing, case-insensitive,
    no match, shape)
  - Wired in server.go, always available (no config gate)
```

---

## 4. Slice 1 Implementation -- Session Overview

### 4.1 Delivered Files

| File | LOC | Purpose |
|---|---|---|
| `internal/interfaces/http/session_handlers.go` | 112 | `handleGetSession` + composed read model types |
| `internal/interfaces/http/session_handlers_test.go` | 163 | 6 tests covering all paths |
| `internal/interfaces/http/server.go` | +2 | Route registration |

### 4.2 Design

```
Client GET /api/v1/session
   |
   v
handleGetSession
   |
   +--- time.Now().UnixMilli()          -> server_time_ms
   +--- engine.Request(guardian, ReadyQuery) -> ready (bool)
   +--- s.markets config                -> markets[]
   +--- domain constants                -> capabilities.artifacts[]
   |
   v
SessionOverviewResponse (JSON)
```

Markets are deduplicated by venue, instruments deduplicated within venue, all sorted alphabetically. Readiness query is best-effort — returns false on timeout or nil engine.

---

## 5. Slice 2 Implementation -- Instrument Freshness

### 5.1 Delivered Files

| File | LOC | Purpose |
|---|---|---|
| `internal/interfaces/http/freshness_handlers.go` | 75 | `handleGetFreshness` + freshness types |
| `internal/interfaces/http/freshness_handlers_test.go` | 144 | 6 tests covering all paths |
| `internal/interfaces/http/server.go` | +1 | Route registration |

### 5.2 Design

```
Client GET /api/v1/freshness?venue=binance&instrument=BTCUSDT
   |
   v
handleGetFreshness
   |
   +--- observability.SnapshotTerminalWSState(1024)
   +--- filter by venue+instrument (case-insensitive)
   +--- group by channel, keep most recent per channel
   +--- compute flowing = (now - last_ts) < 30s
   |
   v
FreshnessResponse{venue, instrument, active, channels, checked_at}
```

The handler reads from the same global terminal WS store used by `/runtime/terminal`, but filters and reshapes the data for client consumption. No stream IDs, no internal counters, no observability internals are leaked.

---

## 6. Validation

```
internal/interfaces/http:  12 new tests PASS (6 session + 6 freshness)
internal/interfaces/http:  all existing tests PASS (zero regressions)
cmd/server:                builds clean
cmd/processor:             builds clean
cmd/executor:              builds clean
```

---

## 7. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Session overview readiness query adds latency | Low | Same timeout as /readyz (5s); best-effort (returns false on timeout) |
| Freshness depends on terminal WS store (global mutable state) | Low | Same store used by /runtime/terminal in production; snapshot is a copy |
| Freshness 30s threshold may not suit all use cases | Low | Constant is documented; can be made configurable later if needed |
| Session overview returns all markets (no pagination) | Low | Market configs are small (6 exchanges × ~10 symbols); pagination unnecessary |

---

## 8. Non-Goals (Explicit)

- Client-side changes
- New domain types or streams
- Per-instrument timeline in session response (use /api/v1/timeline separately)
- Historical freshness or trend data
- Trading surface expansion
- Localhost-only restriction on session/freshness (these are client-facing)
- WS protocol version bump

---

## 9. Recommended Next Slice

Stage 17 is **COMPLETE**. The backend now has composed operational surfaces for client bootstrap and data flow indicators.

Recommended next priorities:
1. **Client evolution** -- the backend is formally ready. The client can:
   - Call `GET /api/v1/session` once at connect to get markets, capabilities, readiness
   - Call `GET /api/v1/freshness` per instrument for flow indicators
   - Use `/api/v1/catalog` for stream picker data
   - Use `/api/v1/timeline` for scrollbar bounds
2. **WS GetRange Federation Bridge** (S15 Slice 4, deferred) -- only if client needs federated historical data via WS
3. **System Readiness surface** (public /api/v1/readiness) -- lower priority, /readyz already serves ops
