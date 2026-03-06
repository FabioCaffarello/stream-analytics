# Stage 13 -- Cold Analytics Store + Historical Query Plane

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE (All 6 slices delivered)

---

## Executive Summary

Stage 13 closes the ClickHouse cold storage gap for the 5 new aggregation artifact types introduced in Stage 12 (tape, OI, delta_volume, CVD, bar_stats). It also addresses the discovery that candle/stats CH writers exist but are not wired for dual-write in the processor bootstrap.

**Key discovery:** The HTTP cold reader endpoints (`/api/v1/{tape,oi,delta_volume,cvd,bar_stats}`) exist from S12 but return 503 because no ClickHouse tables, writers, or readers exist for these types. Additionally, candle/stats CH writers exist in code but are not wired in the processor, meaning the existing `/api/v1/candles` and `/api/v1/stats` cold reader endpoints backed by CH return empty results.

---

## Current-State Audit (pre-S13)

| Artifact | Pg Hot Writer | CH Cold Table | CH Cold Writer | CH Cold Reader | HTTP Endpoint | Dual-Write |
|---|---|---|---|---|---|---|
| candle | DONE | EXISTS | EXISTS (unwired) | EXISTS (wired) | DONE | NO |
| stats | DONE | EXISTS | EXISTS (unwired) | EXISTS (wired) | DONE | NO |
| orderbook | DONE | EXISTS | EXISTS (wired) | EXISTS (wired) | DONE | YES |
| tape | DONE | MISSING | MISSING | MISSING | 503 | N/A |
| OI | DONE | MISSING | MISSING | MISSING | 503 | N/A |
| delta_volume | DONE | MISSING | MISSING | MISSING | 503 | N/A |
| CVD | DONE | MISSING | MISSING | MISSING | 503 | N/A |
| bar_stats | DONE | MISSING | MISSING | MISSING | 503 | N/A |

---

## Architecture

### Storage Tier Model (ADR-0006 aligned)

```
In-Memory Ring Buffers  -->  WS real-time delivery (hot path)
         |
         | snapshot-before-delta
         v
Timescale (Hot/Warm)    -->  Pg writers (S12), GetRange, operational queries
         |
         | dual-write (processor, Slice 4)
         v
ClickHouse (Cold)       -->  ReplacingMergeTree, HTTP /api/v1/* cold reader endpoints
                             90d retention (min+), 14d retention (sub-minute)
```

### Design Decisions

1. **ReplacingMergeTree + FINAL**: Same engine as candle/stats cold tables. Dedup on `(venue, instrument, timeframe, window_start, idempotency_key)`.
2. **Boolean → UInt8**: ClickHouse has no native BOOLEAN type; `is_burst` stored as UInt8 (0/1), mapped in reader.
3. **TTL tiering**: Sub-minute (250ms, 1s, 5s) → 14 days. Minute+ → 90 days. Matches migration 0007.
4. **Idempotency**: `WindowIdempotencyKey(venue, instrument, timeframe, window_start)` — FNV-1a hash, same as Pg writers.

---

## Slice 1 -- CH Cold Tables (COMPLETE)

### Files Created (2)

| File | Purpose |
|---|---|
| `sql/clickhouse/migrations/0008_s13_analytics_cold_tables.sql` | 5 new CREATE TABLE statements |
| `sql/clickhouse/migrations/0009_s13_subminute_ttl_policy.sql` | Sub-minute TTL tiering for 5 tables |

### Tables Created

| Table | Engine | ORDER BY | Columns |
|---|---|---|---|
| `aggregation_tape_cold` | ReplacingMergeTree | (venue, instrument, timeframe, window_start, idempotency_key) | 24 |
| `aggregation_oi_cold` | ReplacingMergeTree | same | 12 |
| `aggregation_delta_volume_cold` | ReplacingMergeTree | same | 12 |
| `aggregation_cvd_cold` | ReplacingMergeTree | same | 12 |
| `aggregation_bar_stats_cold` | ReplacingMergeTree | same | 21 |

---

## Slice 2 -- CH Cold Writers + Readers (COMPLETE)

### Files Created (15)

**Writers (5):**

| File | LOC | Port Satisfied |
|---|---|---|
| `internal/adapters/storage/clickhouse/tape_writer.go` | 100 | `TapeHotReadModelStore` |
| `internal/adapters/storage/clickhouse/oi_writer.go` | 82 | `OIHotReadModelStore` |
| `internal/adapters/storage/clickhouse/delta_volume_writer.go` | 82 | `DeltaVolumeHotReadModelStore` |
| `internal/adapters/storage/clickhouse/cvd_writer.go` | 79 | `CVDHotReadModelStore` |
| `internal/adapters/storage/clickhouse/bar_stats_writer.go` | 103 | `BarStatsHotReadModelStore` |

**Readers (5):**

| File | LOC | Port Satisfied |
|---|---|---|
| `internal/adapters/storage/clickhouse/tape_reader.go` | 88 | `TapeReader` |
| `internal/adapters/storage/clickhouse/oi_reader.go` | 78 | `OIReader` |
| `internal/adapters/storage/clickhouse/delta_volume_reader.go` | 78 | `DeltaVolumeReader` |
| `internal/adapters/storage/clickhouse/cvd_reader.go` | 76 | `CVDReader` |
| `internal/adapters/storage/clickhouse/bar_stats_reader.go` | 87 | `BarStatsReader` |

**Tests (5):**

| File | Tests |
|---|---|
| `tape_writer_test.go` | 4 tests (success, nil, prepare error, flush error) |
| `oi_writer_test.go` | 3 tests (success, nil, prepare error) |
| `delta_volume_writer_test.go` | 3 tests (success, nil, flush error) |
| `cvd_writer_test.go` | 3 tests (success, nil, flush error) |
| `bar_stats_writer_test.go` | 3 tests (success, nil, flush error) |

### Validation

```
internal/adapters/storage/clickhouse:  16 new tests PASS
internal/adapters:                     all tests PASS (zero regressions)
cmd/processor:                         builds clean
cmd/server:                            builds clean
```

---

## Slice 3 -- Dual-Write Composite Stores (COMPLETE)

### Files Modified (1)

| File | Delta | Change |
|---|---|---|
| `cmd/processor/bootstrap.go` | +150 | 7 dual-write composite store types + wiring in ClickHouse block |

### Design

Each composite store writes to Pg first (fail → return error → NAK to JetStream), then CH (fail → log + metric, no NAK). This ensures CH is never the source of truth and Pg failures are propagated correctly.

Types added: `dualWriteCandleStore`, `dualWriteStatsStore`, `dualWriteTapeStore`, `dualWriteOIStore`, `dualWriteDeltaVolumeStore`, `dualWriteCVDStore`, `dualWriteBarStatsStore`.

All 7 are wired when `storage.clickhouse.enabled=true`. The existing `ChCandleWriter` and `ChStatsWriter` are now wired for the first time (they existed since S4 but were never used in production).

### Error Handling

- Pg write failure → error returned → JetStream NAK → redelivery
- CH write failure → `logger.Warn("cold X write failed (non-fatal)")` + `metrics.IncProcessorCommit("X_cold_err")`
- Each writer already emits `metrics.IncProcessorCommit("X_cold")` on success

---

## Slice 4 -- Cold Reader Wiring + Sub-Minute Wrappers (COMPLETE)

### Files Modified (1)

| File | Delta | Change |
|---|---|---|
| `cmd/server/bootstrap.go` | +100 | 5 sub-minute filtering reader wrappers + full ColdReaders wiring |

### Sub-Minute Reader Wrappers Added

| Type | Port |
|---|---|
| `subMinuteFilteringTapeReader` | `TapeReader` |
| `subMinuteFilteringOIReader` | `OIReader` |
| `subMinuteFilteringDeltaVolumeReader` | `DeltaVolumeReader` |
| `subMinuteFilteringCVDReader` | `CVDReader` |
| `subMinuteFilteringBarStatsReader` | `BarStatsReader` |

### ColdReaders Struct (now fully populated)

All 8 fields of `httpserver.ColdReaders` are now wired when ClickHouse is enabled:

| Field | Reader |
|---|---|
| `Candles` | `subMinuteFilteringCandleReader` → `ChCandleReader` |
| `Stats` | `subMinuteFilteringStatsReader` → `ChStatsReader` |
| `Snapshots` | `ChSnapshotReader` |
| `Tape` | `subMinuteFilteringTapeReader` → `ChTapeReader` |
| `OI` | `subMinuteFilteringOIReader` → `ChOIReader` |
| `DeltaVolume` | `subMinuteFilteringDeltaVolumeReader` → `ChDeltaVolumeReader` |
| `CVD` | `subMinuteFilteringCVDReader` → `ChCVDReader` |
| `BarStats` | `subMinuteFilteringBarStatsReader` → `ChBarStatsReader` |

### Impact

All 8 HTTP cold reader endpoints now return real data when ClickHouse is enabled:
- `/api/v1/candles` — was reading from empty CH → now populated via dual-write
- `/api/v1/stats` — was reading from empty CH → now populated via dual-write
- `/api/v1/tape` — was 503 → now backed by `ChTapeReader`
- `/api/v1/oi` — was 503 → now backed by `ChOIReader`
- `/api/v1/delta_volume` — was 503 → now backed by `ChDeltaVolumeReader`
- `/api/v1/cvd` — was 503 → now backed by `ChCVDReader`
- `/api/v1/bar_stats` — was 503 → now backed by `ChBarStatsReader`
- `/api/v1/snapshots` — was already working

---

## Storage/Routing Strategy

### Write Path

```
Aggregation Use Case
  → Pg Hot Writer (fail → NAK to JetStream)
  → CH Cold Writer (fail → log + metric, event still ACK'd)
```

### Read Path

| Consumer | Source | Latency |
|---|---|---|
| WS real-time delivery | In-memory ring buffers | <1ms |
| WS snapshot-before-delta | Timescale (Pg) | 1-5ms |
| WS GetRange | Timescale (Pg) | 5-20ms |
| HTTP /api/v1/* cold queries | ClickHouse | 10-100ms |

### Idempotency

- All CH tables use `ReplacingMergeTree` with `idempotency_key` in ORDER BY.
- Writers use `WindowIdempotencyKey()` (FNV-1a of venue+instrument+timeframe+window_start).
- Readers use `FINAL` + application-level dedup via `seen` map (defense-in-depth against unmerged parts).

### TTL Retention

| Timeframe Class | Retention |
|---|---|
| Sub-minute (250ms, 1s, 5s) | 14 days |
| Minute+ (1m, 5m, 15m, 1h, 4h, 1d) | 90 days |

---

---

## Validation

```
internal/adapters/storage/clickhouse:  16 new writer tests PASS
internal/adapters:                     all tests PASS (zero regressions)
internal/core/aggregation:             all tests PASS
internal/interfaces/http:              all tests PASS
cmd/processor:                         builds clean
cmd/server:                            builds clean
```

---

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| CH dual-write adds latency to processor hot path | Low | CH write is best-effort, non-blocking for ACK semantics; failure only logs |
| ReplacingMergeTree merge lag | Low | Readers use FINAL + application dedup via seen map |
| 5 new CH tables increase CH storage | Low | TTL policies auto-expire; same as candle/stats |
| No hot/cold query merging in HTTP endpoints | Low | Deferred to S14; CH-only reads sufficient for analytics |
| CH data starts empty on first deploy | Low | Will populate from first dual-write onward; replay can backfill historical |

---

## Deferred to Stage 14+

| Item | Why Deferred |
|---|---|
| Hot/cold query routing (time-range merge) | HTTP endpoints serve from CH only; Pg for WS. Merging adds complexity without immediate user need |
| Backfill from Pg→CH for existing data | CH will populate from dual-write start; historical backfill is optional operational task |
| session_volume_profile (VPSV) | New domain type required |
| CH readers for heatmap/VPVR | Heatmap cold writer exists but no reader; VPVR storage incomplete |
| Pagination cursors for large queries | LIMIT-based pagination sufficient; cursor-based adds complexity |
