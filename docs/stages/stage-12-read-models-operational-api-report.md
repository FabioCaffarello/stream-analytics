# Stage 12 -- Read Models + Operational API / Delivery Readiness

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE (All 5 slices delivered)

---

## Executive Summary

Stage 12 evolves the backend toward client-ready delivery by adding operational API surfaces, read model persistence, and snapshot-before-delta support for new aggregation streams. This is a backend-first stage: no client changes, no new domain types, no exchange integration.

**Key discovery:** All 5 MMT "missing" parity payloads (open_interest, delta_volume, bar_stats, cvd, session_volume_profile) are already implemented except session_volume_profile. The gap is **not contracts or pipelines** but **persistence, cold readers, and snapshot support**.

**Slices 1-4 delivered:**
- 5 Timescale writers + migration (5 new hypertables)
- 5 reader port interfaces + 5 writer port interfaces
- 5 cold reader HTTP endpoints (`/api/v1/{tape,oi,delta_volume,cvd,bar_stats}`)
- Control Plane HTTP API (Slice 1)
- Full bootstrap wiring with sub-minute rollout gates
- Zero regressions across full test suite

---

## Architecture

### Current-State Audit Matrix

| Stream | Domain | Aggregation | Bus | WS Delivery | Hot Store | Cold Store | HTTP Read | Snapshot |
|---|---|---|---|---|---|---|---|---|
| aggregation.candle | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE |
| aggregation.stats | DONE | DONE | DONE | DONE | DONE | DONE | DONE | DONE |
| aggregation.snapshot | DONE | DONE | DONE | DONE | partial | DONE | DONE | DONE |
| aggregation.tape | DONE | DONE | DONE | DONE | DONE | -- | DONE | DONE |
| aggregation.oi | DONE | DONE | DONE | DONE | DONE | -- | DONE | DONE |
| aggregation.delta_volume | DONE | DONE | DONE | DONE | DONE | -- | DONE | DONE |
| aggregation.cvd | DONE | DONE | DONE | DONE | DONE | -- | DONE | DONE |
| aggregation.bar_stats | DONE | DONE | DONE | DONE | DONE | -- | DONE | DONE |
| insights.heatmap_snapshot | DONE | DONE | DONE | DONE | -- | DONE | -- | partial |
| insights.volume_profile_snapshot | DONE | DONE | DONE | DONE | -- | -- | -- | partial |

### Stage 12 Slices (Delivery Order)

```
Slice 1: Control Plane HTTP API          [DONE]
Slice 2: Hot Read Model Ports            [DONE]
Slice 3: Timescale Hot Writers           [DONE]
Slice 4: Cold Reader HTTP Endpoints      [DONE]
Slice 5: HotSnapshotProvider Extension   [DONE]
```

---

## Slice 1 -- Control Plane HTTP API (COMPLETE)

### Files Created (2)

| File | LOC | Purpose |
|---|---|---|
| `internal/interfaces/http/control_plane_handlers.go` | 170 | POST /api/v1/control + GET /api/v1/control/snapshot handlers + DTOs |
| `internal/interfaces/http/control_plane_handlers_test.go` | 230 | 10 tests covering all command types, error paths, nil safety |

### Files Modified (2)

| File | Delta | Change |
|---|---|---|
| `internal/interfaces/http/server.go` | +8 | Added controlPlane field, executionports import, route registration, critical route guard |
| `cmd/executor/bootstrap.go` | +1 | Pass controlPlane to httpserver.WithControlPlane() |

### API Contract

**POST /api/v1/control** (localhost-only)

Request:
```json
{
  "command": "pause|resume|drain|halt|disable_strategy|enable_strategy|disable_adapter|enable_adapter|set_simulation_profile|update_allowlist",
  "target_id": "strategy-id-or-adapter-id",
  "parameters": {"venues": "binance,bybit", "symbols": "BTCUSDT"},
  "reason": "maintenance window",
  "issuer": "operator-name",
  "issued_at_ms": 1709769600000
}
```

Response (200 OK): ControlSnapshot JSON (same as GET endpoint).
Error (400): validation failure. Error (409): state transition conflict.

**GET /api/v1/control/snapshot** (localhost-only)

Response:
```json
{
  "state": "active|paused|drained|halted",
  "disabled_strategies": ["momentum-v1"],
  "disabled_adapters": ["binance.spot"],
  "simulation_profile": "",
  "allowlist_overrides": null,
  "last_directive": {
    "command": "pause",
    "issuer": "operator",
    "issued_at_ms": 1709769600000
  },
  "updated_at_ms": 1709769600000
}
```

### Design Decisions

1. **Localhost-only:** Matches existing `/runtime/*` pattern. External exposure deferred until auth middleware is hardened for write operations.
2. **Issuer required:** Every directive must identify the operator for audit trail.
3. **IssuedAtMs auto-fill:** If omitted, defaults to `time.Now().UnixMilli()`. Prevents validation failure for simple curl commands.
4. **Returns snapshot after apply:** The response always includes the updated state, eliminating need for a follow-up GET.
5. **409 for state conflicts:** Distinguishes "invalid command" (400) from "command valid but state won't allow it" (409).

### Validation

```
internal/interfaces/http:  all tests PASS (10 new control plane + existing)
cmd/executor:              all tests PASS
```

---

## Slice 2 -- Hot Read Model Ports (COMPLETE)

### Files Modified (2)

| File | Delta | Change |
|---|---|---|
| `internal/core/aggregation/ports/ports.go` | +20 | Added OIHotReadModelStore, DeltaVolumeHotReadModelStore, CVDHotReadModelStore, BarStatsHotReadModelStore interfaces |
| `internal/core/aggregation/ports/readers.go` | +25 | Added TapeReader, OIReader, DeltaVolumeReader, CVDReader, BarStatsReader interfaces |

### Port Interfaces Added

**Writer ports** (in `ports.go`):
- `OIHotReadModelStore.SaveOI(ctx, evt) *problem.Problem`
- `DeltaVolumeHotReadModelStore.SaveDeltaVolume(ctx, evt) *problem.Problem`
- `CVDHotReadModelStore.SaveCVD(ctx, evt) *problem.Problem`
- `BarStatsHotReadModelStore.SaveBarStats(ctx, evt) *problem.Problem`

**Reader ports** (in `readers.go`):
- `TapeReader.GetTapeRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit) ([]domain.TapeWindowV1, *problem.Problem)`
- `OIReader.GetOIRange(...)` — returns `[]domain.OpenInterestWindowV1`
- `DeltaVolumeReader.GetDeltaVolumeRange(...)` — returns `[]domain.DeltaVolumeWindowV1`
- `CVDReader.GetCVDRange(...)` — returns `[]domain.CVDWindowV1`
- `BarStatsReader.GetBarStatsRange(...)` — returns `[]domain.BarStatsWindowV1`

---

## Slice 3 -- Timescale Hot Writers + Migrations (COMPLETE)

### Files Created (6)

| File | LOC | Purpose |
|---|---|---|
| `internal/adapters/storage/timescale/tape_writer.go` | ~90 | PgTapeWriter: upsert closed tape windows |
| `internal/adapters/storage/timescale/oi_writer.go` | ~70 | PgOIWriter: upsert closed OI windows |
| `internal/adapters/storage/timescale/delta_volume_writer.go` | ~70 | PgDeltaVolumeWriter: upsert closed delta-volume windows |
| `internal/adapters/storage/timescale/cvd_writer.go` | ~70 | PgCVDWriter: upsert closed CVD windows |
| `internal/adapters/storage/timescale/bar_stats_writer.go` | ~85 | PgBarStatsWriter: upsert closed bar-stats windows |
| `sql/timescale/migrations/0005_s12_analytics_hot_tables.sql` | ~100 | CREATE TABLE for 5 new hypertables + goose down |

### Files Modified (4)

| File | Delta | Change |
|---|---|---|
| `internal/core/aggregation/app/service.go` | +5 | Added OIStore, DeltaVolumeStore, CVDStore, BarStatsStore to AggregationServiceConfig |
| `internal/core/aggregation/app/build_tape.go` | +30 | Added deltaVolumeStore, cvdStore, barStatsStore fields; persist before publish in publishDerivedAnalytics |
| `internal/core/aggregation/app/build_open_interest.go` | +10 | Added store field; persist before publish in Execute |
| `cmd/processor/bootstrap.go` | +120 | Log stubs for 5 new stores, Timescale writer creation, sub-minute filtering wrappers, service config wiring |

### Schema (5 tables)

All tables follow the established pattern: `(venue, instrument, timeframe, window_start)` primary key + `idempotency_key` for dedup + `ON CONFLICT DO NOTHING`.

- `aggregation_tape` — 23 columns (full tape window fields + is_burst)
- `aggregation_oi` — 11 columns (OI + delta + delta_pct)
- `aggregation_delta_volume` — 11 columns (buy/sell/delta volume)
- `aggregation_cvd` — 10 columns (delta_volume + cumulative CVD)
- `aggregation_bar_stats` — 20 columns (full bar stats + imbalance + is_burst)

### Wiring

Each writer:
1. Defaults to a `logXxxHotStore` stub (debug-level logging, no DB)
2. Upgraded to `PgXxxWriter(tsPool)` when `storage.timescale.enabled=true`
3. Wrapped in `subMinuteFilteringXxxStore` for rollout gating

Persistence order: store → publish (fail-fast on store error before bus publish).

---

## Slice 4 -- Cold Reader HTTP Endpoints (COMPLETE)

### Files Modified (2)

| File | Delta | Change |
|---|---|---|
| `internal/interfaces/http/cold_reader_handlers.go` | +130 | 5 new handlers + ColdReaders struct extended + parseWindowRangeParams helper |
| `internal/interfaces/http/server.go` | +5 | Route registration for 5 new endpoints |

### New API Endpoints

All follow the established candle/stats pattern with identical query parameters.

| Endpoint | Reader Interface | Response Type |
|---|---|---|
| `GET /api/v1/tape` | `TapeReader.GetTapeRange` | `[]TapeWindowV1` |
| `GET /api/v1/oi` | `OIReader.GetOIRange` | `[]OpenInterestWindowV1` |
| `GET /api/v1/delta_volume` | `DeltaVolumeReader.GetDeltaVolumeRange` | `[]DeltaVolumeWindowV1` |
| `GET /api/v1/cvd` | `CVDReader.GetCVDRange` | `[]CVDWindowV1` |
| `GET /api/v1/bar_stats` | `BarStatsReader.GetBarStatsRange` | `[]BarStatsWindowV1` |

Query parameters: `venue`, `instrument`, `timeframe`, `fromMs`, `toMs`, `limit` (default 1000).

### Design Decision: parseWindowRangeParams

Extracted common parameter parsing into a shared helper to eliminate the duplicated validation logic across candle/stats/tape/OI/DV/CVD/BarStats handlers.

---

## Validation

```
internal/core/aggregation:  all tests PASS (tape + OI + candle + stats + domain)
internal/adapters:           all tests PASS (storage + exchange + jetstream)
internal/interfaces/http:    all tests PASS (control plane + cold readers)
cmd/processor:               builds clean
cmd/server:                  builds clean
cmd/executor:                builds clean
Full test suite (make test): PASS — zero regressions
```

---

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Control plane API is localhost-only | Low | Sufficient for operator tooling; external exposure requires auth hardening (Stage 13) |
| 5 new Timescale tables increase storage overhead | Low | Same retention policies as candle/stats; bounded by window count |
| Snapshot provider coupling grows | Low | Composite provider pattern isolates per-type logic |
| session_volume_profile (VPSV) still missing | Medium | Deferred to Stage 13 (requires new domain type, not just plumbing) |
| No ClickHouse cold writers for new types | Low | Pg hot store sufficient for operational use; cold path additive |
| Cold reader endpoints return 503 until readers wired | Low | Same pattern as existing candle/stats; readers wired when ClickHouse adapters added |

---

## Slice 5 -- HotSnapshotProvider Extension (COMPLETE)

### Files Modified (1)

| File | Delta | Change |
|---|---|---|
| `cmd/server/bootstrap.go` | +180 | Extended `timescaleAggregateHotSnapshotProvider.GetLatest` with 5 new stream type cases + 5 query methods |

### Stream Types Added

| Stream Type | Default TF | Query Table |
|---|---|---|
| `aggregation.tape` | `1s` | `aggregation_tape` |
| `aggregation.oi` | `raw` | `aggregation_oi` |
| `aggregation.delta_volume` | `1s` | `aggregation_delta_volume` |
| `aggregation.cvd` | `1s` | `aggregation_cvd` |
| `aggregation.bar_stats` | `1s` | `aggregation_bar_stats` |

Each method follows the established candle/stats pattern:
1. Resolve timeframe (default to sensible value if raw/empty)
2. Apply sub-minute rollout gate
3. Try symbol candidates (exact match + base without market type suffix)
4. Query `ORDER BY window_end DESC LIMIT 1`
5. Marshal to JSON and return

### Impact

WS subscribers to any of the 5 new stream types now receive a **snapshot-before-delta** on subscribe, matching the existing candle/stats behavior. This eliminates the "cold start" gap where clients would see no data until the first window closes.

---

## Deferred to Stage 13+

| Item | Why Deferred |
|---|---|
| session_volume_profile (VPSV) | New domain type + builder needed |
| ClickHouse cold writers for new types | Additive; Pg hot store sufficient |
| Portfolio state HTTP read model | Not needed by client yet |
| Execution/strategy storage persistence | Draft status in event-bus contract |
| Control plane auth for external access | Requires auth middleware evolution |
| Proto rollout for new aggregation types | JSON sufficient for now |
| Cold reader implementations (ClickHouse) | Port interfaces ready; adapters additive |
