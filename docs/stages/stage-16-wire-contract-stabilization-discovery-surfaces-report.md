# Stage 16 -- Wire Contract Stabilization + Discovery Surfaces

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE (All 4 slices delivered)

---

## Executive Summary

Stage 16 freezes the backend wire format and adds discovery surfaces so the system is formally ready for future client evolution. After S15 Slice 1 (timeline API), the backend serves 8 artifact types via HTTP and WS — but 8 aggregation domain types lack explicit JSON tags (wire format depends on Go field naming), there is no stream catalog API, and delivery contracts are under-documented.

**Mission:** Stabilize wire contracts with explicit JSON tags, add a catalog/discovery API, and formalize delivery guarantees — all as backend-only preparation without starting client evolution.

**Constraints:**
- No new trading surfaces
- No new domain types or streams
- No domain logic in cmd/*
- No storage/federation details leaked to client
- No client-side changes
- Preserve current PascalCase wire format (zero behavioral change for Odin)

---

## 1. Current-State Audit (Post S15 Slice 1)

### 1.1 JSON Tag Inventory

| Domain Type | File | JSON Tags | Wire Format | Exposed Via | Decision |
|---|---|---|---|---|---|
| CandleV1 | aggregation/domain/candle.go | **NONE** | PascalCase default | WS + HTTP | ADD explicit PascalCase tags |
| StatsWindowV1 | aggregation/domain/stats.go | **NONE** | PascalCase default | WS + HTTP | ADD explicit PascalCase tags |
| TapeWindowV1 | aggregation/domain/tape.go | **NONE** | PascalCase default | HTTP only | ADD explicit PascalCase tags |
| DeltaVolumeWindowV1 | aggregation/domain/analytics_primitives.go | **NONE** | PascalCase default | HTTP only | ADD explicit PascalCase tags |
| CVDWindowV1 | aggregation/domain/analytics_primitives.go | **NONE** | PascalCase default | HTTP only | ADD explicit PascalCase tags |
| BarStatsWindowV1 | aggregation/domain/analytics_primitives.go | **NONE** | PascalCase default | HTTP only | ADD explicit PascalCase tags |
| OpenInterestWindowV1 | aggregation/domain/analytics_primitives.go | **NONE** | PascalCase default | HTTP only | ADD explicit PascalCase tags |
| SnapshotProduced | aggregation/domain/events.go | **NONE** | PascalCase default | WS only | ADD explicit PascalCase tags |
| HeatmapArtifactV1 | insights/domain/ | **explicit snake_case** | Stable | WS + CH | NO CHANGE |
| VolumeProfileSnapshotV1 | insights/domain/ | **explicit snake_case** | Stable | WS | NO CHANGE |
| CrossVenueTradeSnapshotV1 | insights/domain/ | **explicit snake_case** | Stable | WS | NO CHANGE |
| TimelineResponse | interfaces/http/ | **explicit snake_case** | Stable | HTTP | NO CHANGE |
| MarketsConfig/Exchange/Symbol | shared/config/ | **explicit snake_case** | Stable | HTTP | NO CHANGE |

**Risk:** The 8 aggregation types without json tags depend on Go field naming. Any Go field rename silently breaks all clients. The Odin client already parses PascalCase fields.

### 1.2 API Surface Inventory

| Endpoint | Source | Data Shape | Tags Stable? |
|---|---|---|---|
| GET /api/v1/candles | Federated (Pg+CH) | `[]CandleV1` | **NO** |
| GET /api/v1/stats | Federated (Pg+CH) | `[]StatsWindowV1` | **NO** |
| GET /api/v1/tape | Federated (Pg+CH) | `[]TapeWindowV1` | **NO** |
| GET /api/v1/oi | Federated (Pg+CH) | `[]OpenInterestWindowV1` | **NO** |
| GET /api/v1/delta_volume | Federated (Pg+CH) | `[]DeltaVolumeWindowV1` | **NO** |
| GET /api/v1/cvd | Federated (Pg+CH) | `[]CVDWindowV1` | **NO** |
| GET /api/v1/bar_stats | Federated (Pg+CH) | `[]BarStatsWindowV1` | **NO** |
| GET /api/v1/snapshots | CH only | `[]int64` | YES (primitive) |
| GET /api/v1/timeline | Federated | `TimelineResponse` | YES |
| GET /api/v1/markets | Config-derived | `MarketsConfig` | YES |
| GET /api/v1/consistency | Pg+CH raw | ConsistencyReport | YES (localhost) |
| GET /api/v1/control/* | In-memory | ControlPlane | YES (localhost) |
| WS real-time | In-memory ring | JSON frames | **NO** (CandleV1, StatsWindowV1) |
| WS getrange | delivery_events | RangeItem{} | YES (envelope) |

### 1.3 Key Decisions

**D1: PascalCase tags to preserve current wire format**

The Odin client parses PascalCase field names (`WindowStartTs`, `ClosePrice`, `BuyVolume`, etc.). Adding `json:"WindowStartTs"` (not `json:"window_start_ts"`) preserves the current wire format with zero behavioral change. This is a contract freeze, not a format migration.

Rationale: Insights types already use snake_case because they were designed with explicit tags from the start. Aggregation types were not. Changing to snake_case now would break the Odin client. The correct action is to freeze what exists.

**D2: Unexported fields excluded from wire**

`CandleV1` has unexported `*Fixed` fields (e.g., `openFixed`, `highFixed`). These are already excluded by Go's JSON encoder. No action needed — tags are only added to exported fields.

**D3: Catalog API combines markets + artifacts**

A new `GET /api/v1/catalog` endpoint will combine the markets config with available artifact types and timeframes. This enables future client stream pickers to be data-driven rather than hardcoded.

**D4: HTTP and WS share domain types but not read paths**

HTTP serves typed domain structs from federated readers. WS serves raw JSON envelopes from delivery_events/in-memory. Both paths serialize the same domain types (CandleV1, StatsWindowV1) — so stabilizing JSON tags on the domain types stabilizes both HTTP and WS wire formats simultaneously.

**D5: Snapshot/range/timeline guarantees frozen**

- Timeline API: `first_ts`/`last_ts` from federated readers (hot+cold merged)
- Snapshot-on-subscribe: HotSnapshotProvider → session last event → live events (ordering guaranteed)
- GetRange: delivery_events table with seq-ordered results
- These are documented and will not change without a version bump

---

## 2. Stage 16 Architecture

### 2.1 What Changes

```
Domain types (aggregation/domain/*.go)
  └── Add explicit json tags to 8 types (exported fields only)
  └── PascalCase to match current wire format
  └── Zero behavioral change

New golden test (aggregation/domain/*_test.go)
  └── Asserts JSON shape stability for all 8 types
  └── Catches accidental field additions/removals/renames

New catalog endpoint (interfaces/http/catalog_handlers.go)
  └── GET /api/v1/catalog?venue=X&instrument=Y
  └── Returns available artifacts, timeframes per market
  └── Config-derived, no storage queries

Documentation updates
  └── delivery-ws.md: snapshot-before-delta ordering
  └── TRUTH-MAP: Stage 16 references
```

### 2.2 What Does NOT Change

- No new domain types or streams
- No read-model mapping layer (domain types serve directly)
- No WS protocol changes
- No federation changes
- No storage changes
- No client changes

---

## 3. Prioritized Slices

```
Slice 1: JSON Tag Stabilization                                  [COMPLETE]
  - Add explicit json tags to CandleV1, StatsWindowV1, TapeWindowV1,
    OpenInterestWindowV1, DeltaVolumeWindowV1, CVDWindowV1, BarStatsWindowV1,
    SnapshotProduced
  - PascalCase tags to preserve current wire behavior
  - Golden test asserting JSON shape stability
  - Zero behavioral change, pure contract freeze

Slice 2: Stream Catalog API                                      [COMPLETE]
  - GET /api/v1/catalog?venue=X&instrument=Y endpoint
  - Returns 8 artifact types with supported timeframes and HTTP endpoints
  - Config-derived from markets + domain constants, zero storage queries
  - Optional venue/instrument filters, sorted results, dedup
  - 9 unit tests (happy path, venue filter, instrument filter,
    both filters, no match, not configured, dedup, shape, multi-exchange sort)
  - Wired in server.go, route gated by markets config

Slice 3: Delivery Contract Documentation                         [COMPLETE]
  - Formalized snapshot-before-delta ordering as WS-11 in delivery-ws.md
  - Documented snapshot source hierarchy (HotSnapshotProvider → session last → none)
  - Documented subscribe and resync flows with guarantees
  - Added Timeline API and Catalog API documentation to event-bus.md
  - Added wire format contract documentation (PascalCase vs snake_case)

Slice 4: Wire Contract Regression Suite                          [COMPLETE]
  - 8 golden JSON key-set tests for all aggregation types
  - Tests assert exact key set match — any field add/remove/rename fails
  - Delivered as part of Slice 1 (wire_format_golden_test.go)
```

### Delivery Order Rationale

Slice 1 is the highest-value safety net: adds explicit json tags to 8 domain types, preventing Go field renames from silently breaking clients. Zero behavioral change. Slice 2 depends on markets config already being wired. Slices 3-4 are independent documentation/testing.

---

## 4. Slice 1 Implementation

### 4.1 Design

Add explicit `json:"FieldName"` struct tags to all exported fields on 8 aggregation domain types. Tags use PascalCase to exactly match the current Go default encoding, ensuring zero wire format change.

**Types modified:**
1. `CandleV1` — 16 exported fields
2. `StatsWindowV1` — 19 exported fields
3. `TapeWindowV1` — 18 exported fields
4. `DeltaVolumeWindowV1` — 10 exported fields
5. `CVDWindowV1` — 9 exported fields
6. `BarStatsWindowV1` — 18 exported fields
7. `OpenInterestWindowV1` — 10 exported fields
8. `SnapshotProduced` — 13 exported fields (including nested Level slices)

**Golden test:** Marshals each type to JSON and asserts the key set matches the frozen contract.

### 4.2 Validation

```
internal/core/aggregation:  golden wire format test PASS
internal/core/aggregation:  all existing tests PASS (zero regressions)
cmd/server:                 builds clean
cmd/processor:              builds clean
cmd/executor:               builds clean
```

---

## 5. Slice 2 Implementation — Stream Catalog API

### 5.1 Delivered Files

| File | LOC | Purpose |
|---|---|---|
| `internal/interfaces/http/catalog_handlers.go` | 101 | `handleGetCatalog` + response types |
| `internal/interfaces/http/catalog_handlers_test.go` | 215 | 9 tests covering all paths |
| `internal/interfaces/http/server.go` | +1 | Route registration for `/api/v1/catalog` |

### 5.2 Design

```
Client GET /api/v1/catalog?venue=binance&instrument=BTCUSDT
   |
   v
handleGetCatalog (filters by venue/instrument from markets config)
   |
   v
CatalogResponse{entries: [{venue, instrument, artifacts: [{name, endpoint, timeframes}]}]}
```

Artifact definitions are derived from domain constants:
- `candle` → `AllowedCandleTimeframes` (9 TFs)
- `stats` → `AllowedStatsTimeframes` (9 TFs)
- `tape`, `delta_volume`, `cvd`, `bar_stats` → `AllowedTapeTimeframes` (3 TFs)
- `oi`, `snapshots` → `["raw"]`

Each artifact includes its HTTP endpoint path for data-driven client wiring.

### 5.3 Slice 3 — Delivery Contract Documentation

Updated `docs/contracts/delivery-ws.md`:
- Added `WS-11`: snapshot-before-delta ordering invariant
- Formalized subscribe and resync snapshot delivery flows
- Documented snapshot source hierarchy (HotSnapshotProvider → session last → none)
- Specified `prev_seq == 0` semantics for first event after snapshot

Updated `docs/contracts/event-bus.md`:
- Added Timeline API contract documentation
- Added Stream Catalog API contract documentation
- Added Wire Format Contract section documenting PascalCase vs snake_case conventions

### 5.4 Validation

```
internal/interfaces/http:  9 catalog tests PASS
internal/interfaces/http:  all tests PASS (zero regressions)
internal/core/aggregation: all tests PASS (zero regressions)
cmd/server:                builds clean
cmd/processor:             builds clean
cmd/executor:              builds clean
```

---

## 6. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| PascalCase tags diverge from insights snake_case convention | Low | Documented decision — aggregation types freeze existing format; insights types already have their own convention |
| Adding tags to SnapshotProduced changes Level serialization | None | Level type has no tags, PascalCase default preserved |
| Catalog API (Slice 2) returns stale data if config changes | Low | Reads live config; no cache |
| Golden test maintenance burden | Low | Only fails when wire format actually changes (desired behavior) |

---

## 6. Non-Goals (Explicit)

- Client-side changes
- snake_case migration for aggregation types
- New domain types or streams
- CBOR encoding
- Read-model mapping layer
- WS protocol version bump
- Trading surface expansion
- WS getrange federation bridge (S15 Slice 4, deferred)

---

## 8. Recommended Next Stage

Stage 16 is **COMPLETE**. All wire contracts are frozen, discovery surfaces are live, and delivery guarantees are formalized.

Recommended next priorities:
1. **Client evolution** — the backend is now formally ready. The client can use `/api/v1/catalog` for data-driven stream pickers and `/api/v1/timeline` for scrollbar bounds.
2. **WS GetRange Federation Bridge** (S15 Slice 4, deferred) — only if the client needs federated historical data via WS rather than HTTP.
3. **Snapshot Diagnostics** (S15 Slice 5) — operator-facing, lower priority.
