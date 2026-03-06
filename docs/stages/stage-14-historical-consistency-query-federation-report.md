# Stage 14 -- Historical Consistency + Query Federation Readiness

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

---

## Executive Summary

Stage 14 consolidates the backend storage plane before client evolution. After S12 (hot writers + HTTP endpoints) and S13 (cold writers + dual-write + CH readers), the system has two fully populated tiers but **no unified query surface that spans both**. HTTP reads go to ClickHouse only; WS GetRange goes to Timescale only. There is no consistency verification, no backfill tooling, and no deterministic pagination contract.

**Mission:** Introduce query federation (hot+cold routing), hot/cold consistency checks, replay-based backfill, and stable paginated read surfaces -- all as backend-only work that prepares for future client evolution without starting it.

**Constraints:**
- No new trading surfaces
- No new streams without necessity
- No domain logic in cmd/*
- ClickHouse is NOT source of truth for runtime
- No client-side changes

---

## 1. Current-State Audit (Post S12+S13)

### 1.1 Storage Tier Inventory

| Artifact | Domain | Agg | Bus | WS RT | Pg Hot Write | CH Cold Write | CH Cold Read | HTTP Read | Pg Hot Read | Dual-Write | Consistency Check |
|---|---|---|---|---|---|---|---|---|---|---|---|
| candle | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| stats | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| orderbook | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| tape | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| OI | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| delta_volume | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| CVD | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| bar_stats | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE (CH) | NONE | YES | NONE |
| heatmap | DONE | DONE | DONE | DONE | -- | DONE (CH) | NONE | NONE | NONE | NO (CH only) | NONE |
| volume_profile | DONE | DONE | DONE | DONE | -- | partial | NONE | NONE | NONE | NO | NONE |

### 1.2 Read Path Audit

| Consumer | Current Source | Covers |
|---|---|---|
| WS real-time delivery | In-memory ring buffers | Latest N events per topic |
| WS snapshot-before-delta | Timescale (Pg) | Last snapshot for each orderbook key |
| WS GetRange | Timescale delivery_events | Bounded replay window |
| HTTP /api/v1/* | ClickHouse only | Historical queries (cold) |

### 1.3 Key Gaps Identified

| Gap | Impact | Priority |
|---|---|---|
| **G1: No Pg readers for artifacts** | HTTP reads only go to CH; if CH is down, all historical queries fail. No way to query recent hot data via HTTP. | P0 |
| **G2: No federated routing** | Client cannot ask "give me last 4h of candles" and get hot+cold merged result. Must pick one source. | P0 |
| **G3: No consistency verification** | No way to detect if Pg and CH have diverged (dropped CH writes, replay gaps). | P1 |
| **G4: No backfill tooling** | If CH starts empty or loses data, no way to populate from Pg. | P1 |
| **G5: No pagination contract** | LIMIT-only; no cursor; no guaranteed ordering determinism across hot/cold merge. | P1 |
| **G6: Hot retention cleanup incomplete** | `cleanup_aggregation_hot_retention` only covers candle+stats; tape/OI/dv/cvd/bar_stats not cleaned. | P1 |
| **G7: No Pg readers for new S12 types** | Tape/OI/DV/CVD/BarStats have Pg writers but no Pg readers; GetRange only uses delivery_events. | P1 |

---

## 2. Stage 14 Architecture

### 2.1 Target Read Path

```
Client HTTP GET /api/v1/candles?fromMs=X&toMs=Y
   |
   v
FederatedReader (new port + adapter)
   |
   +--- is time range within hot window? ---> Pg Hot Reader
   |
   +--- is time range beyond hot TTL? -----> CH Cold Reader
   |
   +--- spans both? -----------------------> Pg Hot + CH Cold, merge by window_start, dedup by idempotency_key
   |
   v
Deterministic result: ORDER BY window_start ASC, LIMIT N, cursor-ready
```

### 2.2 Federation Strategy: Time-Based Routing

**Decision:** Route by time range relative to a configurable hot window boundary.

- **Hot window**: configurable per-artifact, default = `retention_period - 1h buffer` (e.g., 13d for sub-minute, 89d for minute+)
- **Routing logic**:
  - `toMs < hot_boundary` -> cold only (CH)
  - `fromMs > cold_boundary` -> hot only (Pg)
  - Otherwise -> merge: query both, deduplicate by `(venue, instrument, timeframe, window_start)`
- **Merge semantics**: window_start ASC ordering; hot wins on conflict (fresher data).

### 2.3 Consistency Strategy

**Decision:** Timestamp-count verification, not row-level diff.

A `ConsistencyChecker` port queries both stores for the same `(venue, instrument, timeframe, fromMs, toMs)` range and compares:
1. Row counts
2. First/last window_start timestamps
3. Hash of sorted window_start list (lightweight gap detection)

Results are emitted as Prometheus metrics + optional structured log. No automatic repair; backfill is a separate operational action.

### 2.4 Backfill Strategy

**Decision:** Pg-to-CH unidirectional backfill via batch reader.

A `BackfillJob` reads from Pg Hot Reader in time-ordered batches and writes to CH Cold Writer. Uses existing idempotency (ReplacingMergeTree) to handle reruns. Exposed as an operator command, not automated.

### 2.5 Pagination Contract

**Decision:** Keyset pagination via `after_window_start` parameter.

- All range queries accept optional `after_window_start` (exclusive lower bound)
- Combined with `limit`, this gives deterministic cursor-based pagination
- Response includes `has_more` boolean and `last_window_start` for next page

---

## 3. Prioritized Slices

```
Slice 1: Pg Hot Readers (ports + adapters)                    [COMPLETE]
  - 7 Pg readers implementing existing port interfaces
  - pgQuerier abstraction for test seams
  - 22 unit tests (success + nil + error paths)

Slice 2: FederatedReader composite adapter                    [COMPLETE]
  - 7 federated readers wrapping hot+cold pairs
  - Time-based routing: route(fromMs, toMs, hotWindowMs, nowFn)
  - O(n) merge by window_start, hot wins on duplicate
  - Generic helpers (queryOrFallback, mergeRange) for 5 simple readers
  - Explicit implementations for candle+stats (4-method interfaces)
  - 19 unit tests with stub hot/cold

Slice 3: HTTP endpoint rewiring                               [COMPLETE]
  - cmd/server/bootstrap.go: buildStorageOptions wires federated readers
    when both Pg+CH pools available, CH-only fallback otherwise
  - Config: storage.federation_hot_window_ms (default 24h)
  - Sub-minute filtering wraps federated readers (unchanged API)

Slice 4: Hot retention cleanup extension                      [COMPLETE]
  - Migration 0006: extends cleanup_aggregation_hot_retention to all 7 tables
  - Adds indexes on (timeframe, created_at) for tape/oi/dv/cvd/bar_stats
  - Same retention policy: sub-minute=14d, minute+=90d

Slice 5: ConsistencyChecker                                   [COMPLETE]
  - federation.ConsistencyChecker compares timestamps across tiers
  - CheckCandles/CheckStats: overlap, missing-in-cold, missing-in-hot counts
  - 4 unit tests

Slice 6: Consistency HTTP endpoint                            [COMPLETE]
  - GET /api/v1/consistency?artifact=candle&venue=...&fromMs=...&toMs=...
  - Localhost-only (localhostOnly middleware)
  - Returns ConsistencyReport JSON
```

### Delivery Order Rationale

Slice 1 must come first (Pg readers are prerequisite for federation). Slice 2 depends on 1. Slice 3 depends on 2. Slices 4-6 are independent and can parallelize after Slice 1.

---

## 4. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Pg hot readers add query load to Timescale | Low | Read-only queries with LIMIT; same connection pool; monitoring via existing Pg metrics |
| Merge dedup complexity in federation | Medium | Keep merge simple: sort by window_start, skip duplicates by (venue,instrument,tf,ws). Hot wins on tie. |
| Hot window boundary misconfiguration | Low | Default to conservative boundary (retention - 1h buffer); configurable per-artifact |
| Backfill of large Pg dataset to CH | Low | Batch-oriented with configurable chunk size; idempotent reruns |
| Pagination contract change breaks existing clients | Low | `after_window_start` is optional; existing `fromMs`+`limit` behavior unchanged |

---

## 5. Non-Goals (Explicit)

- Client-side changes
- New domain types or streams
- CBOR encoding
- session_volume_profile implementation
- Automated consistency repair
- Real-time hot/cold failover (CH down -> auto-route to Pg)

---

## 6. Recommended First Slice

**Slice 1: Pg Hot Readers** is the smallest correct increment:
- Zero coupling to federation logic
- Each reader is a standalone adapter implementing an existing port
- Immediately testable with unit tests
- Unblocks all subsequent slices
- Proves Pg can serve the same query interface as CH

### Slice 1 Delivered Files

| File | LOC | Purpose |
|---|---|---|
| `internal/adapters/storage/timescale/querier.go` | 14 | `pgQuerier` interface (multi-row query abstraction) |
| `internal/adapters/storage/timescale/candle_reader.go` | 142 | `PgCandleReader` — implements `CandleReader` (4 methods) |
| `internal/adapters/storage/timescale/stats_reader.go` | 166 | `PgStatsReader` — implements `StatsReader` (4 methods + nullable scan) |
| `internal/adapters/storage/timescale/tape_reader.go` | 75 | `PgTapeReader` — implements `TapeReader` |
| `internal/adapters/storage/timescale/oi_reader.go` | 63 | `PgOIReader` — implements `OIReader` |
| `internal/adapters/storage/timescale/delta_volume_reader.go` | 63 | `PgDeltaVolumeReader` — implements `DeltaVolumeReader` |
| `internal/adapters/storage/timescale/cvd_reader.go` | 61 | `PgCVDReader` — implements `CVDReader` |
| `internal/adapters/storage/timescale/bar_stats_reader.go` | 69 | `PgBarStatsReader` — implements `BarStatsReader` |
| `internal/adapters/storage/timescale/reader_test.go` | 340 | 22 tests (success + nil + error paths for all 7 readers) |

### Slice 1 Validation

```
internal/adapters/storage/timescale:  22 new reader tests PASS
internal/adapters:                    all tests PASS (zero regressions)
cmd/server:                           builds clean
cmd/processor:                        builds clean
```

### Slices 2-6 Delivered Files

| File | Purpose |
|---|---|
| `internal/adapters/storage/federation/merge.go` | Config, route(), mergeByWindowStart, mergeTimestamps, capSlice |
| `internal/adapters/storage/federation/range_helpers.go` | Generic queryOrFallback, mergeRange helpers |
| `internal/adapters/storage/federation/candle_reader.go` | FederatedCandleReader (4 methods, explicit routing) |
| `internal/adapters/storage/federation/stats_reader.go` | FederatedStatsReader (4 methods, explicit routing) |
| `internal/adapters/storage/federation/tape_reader.go` | FederatedTapeReader (generic helpers) |
| `internal/adapters/storage/federation/oi_reader.go` | FederatedOIReader (generic helpers) |
| `internal/adapters/storage/federation/delta_volume_reader.go` | FederatedDeltaVolumeReader (generic helpers) |
| `internal/adapters/storage/federation/cvd_reader.go` | FederatedCVDReader (generic helpers) |
| `internal/adapters/storage/federation/bar_stats_reader.go` | FederatedBarStatsReader (generic helpers) |
| `internal/adapters/storage/federation/consistency.go` | ConsistencyChecker + ConsistencyReport |
| `internal/adapters/storage/federation/federation_test.go` | 19 federation tests |
| `internal/adapters/storage/federation/consistency_test.go` | 4 consistency tests |
| `sql/timescale/migrations/0006_s14_extended_hot_retention.sql` | Extended cleanup function for 7 tables |
| `internal/shared/config/schema.go` | `FederationHotWindowMs` field on StorageConfig |
| `internal/interfaces/http/server.go` | ConsistencyCheckFn type, WithConsistencyChecks option |
| `internal/interfaces/http/cold_reader_handlers.go` | handleConsistencyCheck handler |
| `cmd/server/bootstrap.go` | buildStorageOptions (federation wiring) |

### Final Validation

```
internal/adapters/storage/federation:  23 tests PASS (19 federation + 4 consistency)
internal/adapters/storage/timescale:   22 tests PASS
internal/adapters:                     all tests PASS (zero regressions)
cmd/server:                            builds clean
cmd/processor:                         builds clean
cmd/executor:                          builds clean
```
