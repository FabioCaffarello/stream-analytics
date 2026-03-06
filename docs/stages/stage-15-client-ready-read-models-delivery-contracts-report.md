# Stage 15 -- Client-Ready Operational Read Models + Delivery Contracts

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** IN PROGRESS (Slice 1 complete)

---

## Executive Summary

Stage 15 consolidates the backend read surface for future client evolution. After S14 (query federation), the system has unified hot/cold readers behind stable port interfaces, but the client-facing surfaces remain fragmented: HTTP serves federated domain types, WS getrange serves raw envelopes from `delivery_events`, there is no timeline/availability discovery API, and domain types lack explicit JSON tags (wire format depends on Go field naming).

**Mission:** Create client-ready read models, stable delivery/query contracts, timeline/inspection/diagnostics surfaces, and formalize snapshot-before-delta semantics — all as backend-only preparation without starting client evolution.

**Constraints:**
- No new trading surfaces
- No new domain types or streams without necessity
- No domain logic in cmd/*
- No storage/federation details leaked to client
- No client-side changes

---

## 1. Current-State Audit (Post S14)

### 1.1 Read Surface Inventory

| Surface | Source | Data Shape | Coverage |
|---|---|---|---|
| HTTP `/api/v1/candles` | Federated (Pg+CH) | `[]CandleV1` (PascalCase, no json tags) | 8 artifact types |
| HTTP `/api/v1/stats` | Federated (Pg+CH) | `[]StatsWindowV1` (PascalCase) | 8 artifact types |
| HTTP `/api/v1/tape,oi,delta_volume,cvd,bar_stats` | Federated (Pg+CH) | Domain types | 5 simple artifacts |
| HTTP `/api/v1/snapshots` | CH only | `[]int64` timestamps | Orderbook only |
| HTTP `/api/v1/markets` | Config-derived | Exchange+symbol list | All configured |
| HTTP `/api/v1/consistency` | Pg+CH raw | ConsistencyReport | candle, stats |
| WS real-time | In-memory ring buffers | JSON frames (PascalCase) | All streams |
| WS getrange | `delivery_events` (Pg) or in-memory | `RangeItem{Seq,TsIngest,Payload}` | All streams |
| WS snapshot | HotSnapshotProvider or session last event | JSON snapshot frame | Subscribe/resync |

### 1.2 JSON Tag Inventory

| Domain Type | JSON Tags | Wire Format | Exposed Via |
|---|---|---|---|
| CandleV1 | NONE | PascalCase default | WS + HTTP |
| StatsWindowV1 | NONE | PascalCase default | WS + HTTP |
| TapeWindowV1 | NONE | PascalCase default | HTTP only |
| DeltaVolumeWindowV1 | NONE | PascalCase default | HTTP only |
| CVDWindowV1 | NONE | PascalCase default | HTTP only |
| BarStatsWindowV1 | NONE | PascalCase default | HTTP only |
| OpenInterestWindowV1 | NONE | PascalCase default | HTTP only |
| SnapshotProduced | NONE | PascalCase default | WS only |
| HeatmapArtifactV1 | **explicit snake_case** | Stable | WS + CH |
| VolumeProfileSnapshotV1 | **explicit snake_case** | Stable | WS (planned) |
| CrossVenueTradeSnapshotV1 | **explicit snake_case** | Stable | WS |

**Risk:** The 8 aggregation domain types without json tags depend on Go field naming. Any Go field rename silently breaks all clients.

### 1.3 WS vs HTTP Read Path Divergence

| Aspect | WS GetRange | HTTP `/api/v1/*` |
|---|---|---|
| Data source | `delivery_events` table (Pg) or in-memory ring | Federated (Pg hot + CH cold merged) |
| Time coverage | Recent only (~4096 items per subject) | Full historical depth |
| Data shape | `RangeItem{Seq, TsIngest, Payload}` (raw JSON) | Typed domain structs |
| Federation | None | Time-based routing + merge |
| Snapshot-on-subscribe | HotSnapshotProvider → session last event | N/A |

The WS getrange path and HTTP federated reader path are **completely separate**. A client using WS getrange cannot access full historical data that the HTTP API serves.

### 1.4 Key Gaps

| Gap | Impact | Priority |
|---|---|---|
| **G1: No timeline/availability API** | Client cannot discover what time range is available for a market without fetching data. No way to populate scrollbars, time pickers, or detect data existence. | P0 |
| **G2: Two disconnected read paths** | HTTP uses federated readers (Pg+CH merged), WS getrange uses `delivery_events` table (raw envelopes). Different data shapes, different time coverage. | P1 |
| **G3: No explicit JSON tags on domain types** | Wire format depends on Go PascalCase defaults. A Go field rename silently breaks all clients. | P1 |
| **G4: No stream catalog API** | Client must hardcode available artifacts/timeframes. No data-driven stream picker support. | P1 |
| **G5: Snapshot source inconsistency** | On subscribe, snapshot comes from HotSnapshotProvider (in-memory) or session last event. No federated historical snapshot. For orderbook, this means snapshot quality depends on process uptime. | P2 |
| **G6: No diagnostics/inspection surface** | No way for client to ask "what streams exist?", "what's the lag?", "is data flowing?" | P2 |

### 1.3 What Works Well (No Change Needed)

| Surface | Assessment |
|---|---|
| Federation routing | S14 time-based routing + merge-by-window-start is correct and tested (23 federation tests) |
| Federated readers | 7 Pg hot + 7 CH cold + 7 federated wired in bootstrap — complete |
| WS delivery contract | hello/ack/event/snapshot/error/range frames are stable (10 invariants, 10+ tests) |
| WS backpressure | 3 policies, threshold disconnect, metrics — production-ready |
| Sub-minute filtering | Wraps all read surfaces transparently |
| Consistency checker | Candle+stats timestamp comparison, localhost-only endpoint |

---

## 2. Stage 15 Architecture

### 2.1 Design Decisions

**D1: Backend vs Client Responsibility**

| Responsibility | Backend (S15) | Client (future) |
|---|---|---|
| Data availability discovery | Timeline API (first_ts/last_ts) | Scrollbar positioning, time picker bounds |
| Stream catalog | Markets + artifacts endpoint | Stream picker UI |
| Historical data | Federated readers (hot+cold merged) | Local caching, pagination |
| Real-time delivery | WS events + snapshot-on-subscribe | Gap detection (prev_seq), retry |
| Snapshot consistency | HotSnapshotProvider + session fallback | Orderbook rebuild from snapshot+deltas |
| Diagnostics | Health/readyz/stream-info endpoints | Connection status UI |

**D2: No Read-Model Mapping Layer (Yet)**

The domain types (`CandleV1`, `StatsWindowV1`, etc.) are already the HTTP response types. Adding a separate read-model struct that maps 1:1 would be premature abstraction. Instead:
- Freeze the wire format by adding explicit `json` tags to domain types (Slice 2)
- This prevents Go field renames from breaking clients
- A true read-model layer should only be introduced when the wire shape needs to diverge from the domain shape

**D3: Hide Federation Behind Stable APIs**

Federation is already hidden — the `ColdReaders` struct in `httpserver` accepts any `CandleReader`/`StatsReader` etc. via ports. The client sees `/api/v1/candles` and gets data regardless of whether it came from Pg, CH, or both. The new timeline API follows the same pattern.

**D4: WS GetRange Federation Bridge (Deferred)**

Bridging WS getrange to use federated readers requires significant refactoring of the session actor's range store. This is Slice 4 and should only be done if the client actually needs federated historical data via WS (vs HTTP). For now, WS getrange serves recent data from `delivery_events`, which is adequate for "scroll back a few pages" use cases.

**D5: Snapshot-Before-Delta Is Already Correct**

Current implementation:
1. On subscribe → `emitSnapshot()` checks `HotSnapshotProvider` first (in-memory latest), then `lastSnapshot` (session's last seen event)
2. Only then does the session start forwarding live events
3. Resync follows the same pattern

This is the correct ordering. What's missing is documentation (formalized in this report) and a snapshot diagnostic endpoint (Slice 5).

### 2.2 New API Surface

```
GET /api/v1/timeline?venue=X&instrument=Y&timeframe=Z&artifact=candle|stats
→ {venue, instrument, timeframe, artifact, first_ts, last_ts}
```

Characteristics:
- Uses existing federated readers (GetFirstCandle/GetLastCandle)
- Returns unified time range (hot+cold merged — federation is invisible)
- Zero new domain types, zero new storage queries
- Registered only when coldReaders is configured

---

## 3. Prioritized Slices

```
Slice 1: Timeline API                                            [COMPLETE]
  - GET /api/v1/timeline endpoint
  - TimelineResponse read-model struct with explicit json tags
  - Supports candle and stats artifacts (the two with GetFirst/GetLast)
  - 10 unit tests (happy path, default artifact, no data, error,
    stats, reader unavailable, missing params, unsupported artifact)
  - Wired in server.go, route protected by coldReaders != nil guard

Slice 2: JSON Tag Stabilization                                  [PLANNED]
  - Add explicit json tags to CandleV1, StatsWindowV1, TapeWindowV1,
    OpenInterestWindowV1, DeltaVolumeWindowV1, CVDWindowV1, BarStatsWindowV1
  - PascalCase tags to preserve current wire behavior
  - Golden test asserting JSON shape stability
  - Zero behavioral change, pure contract freeze

Slice 3: Stream Catalog API                                      [PLANNED]
  - GET /api/v1/catalog?venue=X&instrument=Y
  - Returns available artifacts, timeframes, and their timeline bounds
  - Combines markets config + timeline data
  - Enables future client stream picker to be data-driven

Slice 4: WS GetRange Federation Bridge                           [PLANNED]
  - For aggregation.candle and aggregation.stats, fall back to federated
    readers when delivery_events has no results
  - Requires new port on SessionActor config for typed readers
  - Only if client needs historical WS data beyond delivery_events window

Slice 5: Snapshot Diagnostics Endpoint                           [PLANNED]
  - GET /api/v1/diagnostics/snapshots (localhost-only)
  - Returns per-stream snapshot state: source, age, hash, watermark_seq
  - Enables operators to verify snapshot health without WS connection

Slice 6: Delivery Contract Documentation                         [PLANNED]
  - Formalize snapshot-before-delta ordering guarantee in delivery-ws.md
  - Document timeline API contract in event-bus.md
  - Add Stage 15 SSoT references to TRUTH-MAP
```

### Delivery Order Rationale

Slice 1 is the smallest correct increment: zero existing code modification (new file + 1-line route registration), immediately useful, uses existing ports. Slice 2 is the highest-value safety net (wire format freeze). Slice 3 depends on 1 (uses timeline data). Slices 4-6 are independent.

---

## 4. Slice 1 Implementation

### 4.1 Delivered Files

| File | LOC | Purpose |
|---|---|---|
| `internal/interfaces/http/timeline_handlers.go` | 108 | `handleGetTimeline` + `TimelineResponse` read-model type |
| `internal/interfaces/http/timeline_handlers_test.go` | 217 | 10 tests covering all paths |
| `internal/interfaces/http/server.go` | +3 | Route registration for `/api/v1/timeline` |

### 4.2 Design

```
Client GET /api/v1/timeline?venue=binance&instrument=BTCUSDT&timeframe=1m
   |
   v
handleGetTimeline (validates params, routes by artifact)
   |
   +--- artifact=candle → CandleReader.GetFirstCandle + GetLastCandle
   |
   +--- artifact=stats  → StatsReader.GetFirstStats + GetLastStats
   |
   v
TimelineResponse{venue, instrument, timeframe, artifact, first_ts, last_ts}
```

The handler delegates to the same federated readers used by `/api/v1/candles`. When federation is configured (Pg+CH), `GetFirstCandle` queries both tiers and returns the earliest; `GetLastCandle` returns the latest. The client sees a single unified time range.

### 4.3 Validation

```
internal/interfaces/http:  10 new timeline tests PASS
internal/interfaces/http:  all tests PASS (zero regressions)
cmd/server:                builds clean
cmd/processor:             builds clean
cmd/executor:              builds clean
```

---

## 5. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Timeline API adds query load (2 queries per call) | Low | GetFirst/GetLast are index-only scans; same connection pool |
| JSON tag freeze (Slice 2) could expose unintended fields | Low | Tags match current PascalCase behavior; golden test catches drift |
| WS getrange federation bridge (Slice 4) complexity | Medium | Deferred — only implement if client needs it; HTTP path is already federated |
| Stream catalog (Slice 3) becomes stale if config changes | Low | Reads live config; no cache to invalidate |

---

## 6. Non-Goals (Explicit)

- Client-side changes
- New domain types or streams
- CBOR encoding
- Read-model mapping layer (premature — domain types serve directly)
- Automated snapshot repair
- WS protocol version bump
- Trading surface expansion
- Moving domain logic to cmd/*

---

## 7. Recommended Next Slice

**Slice 2: JSON Tag Stabilization** is the next smallest correct increment:

- Adds explicit `json:"FieldName"` tags to all 7 aggregation domain types
- Preserves current PascalCase wire format (zero behavioral change)
- Prevents accidental Go field renames from breaking client parsers
- Golden test ensures wire format stability across releases
- Unblocks Slice 3 (catalog API) with confidence that response shapes are frozen
