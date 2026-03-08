# Stage 67 — Insights Subsystem Materialization

**Date:** 2026-03-08
**Status:** COMPLETE
**Scope:** Backend — insights bounded context formalization

## Objective

Materialize the insights domain as an explicit operational bounded context with clear event contracts, formalized artifacts, complete query surface, and delivery governance — without breaking existing boundaries.

## Changes

### 1. Centralized Event Catalog (`domain/events.go`)

Created `EventContract` struct and `EventCatalog` registry consolidating all 10 insights event types previously scattered across 6 domain files:

| Event Type | Version | Owner BC | Producer BC |
|---|---|---|---|
| `insights.volume_profile_snapshot` | 1 | insights | aggregation |
| `insights.volume_profile_delta` | 1 | insights | aggregation |
| `insights.heatmap_snapshot` | 1 | insights | aggregation |
| `insights.heatmap_delta` | 1 | insights | aggregation |
| `insights.crossvenue.trade_snapshot` | 1 | insights | aggregation |
| `insights.crossvenue.spread_signal` | 1 | insights | aggregation |
| `insights.session_volume_profile` | 1 | insights | aggregation |
| `insights.tpo_profile` | 1 | insights | aggregation |
| `insights.fused_volume_profile_snapshot` | 1 | insights | aggregation |
| `insights.fused_heatmap_snapshot` | 1 | insights | aggregation |

Helper: `EventCatalogByType()` returns O(1) lookup map.

### 2. Complete InsightsService Query Surface

Previously only 2 of 6 artifact types were queryable via `InsightsService`. Added:

- **`SnapshotSessionVolumeProfile(ctx, key)`** → `result.Result[SessionVolumeProfileV1]`
- **`SnapshotTPOProfile(ctx, key)`** → `result.Result[TPOProfileV1]`
- New key types: `SessionVolumeProfileSnapshotKey`, `TPOProfileSnapshotKey`

### 3. Public TPO Snapshot Method

`BuildTPOProfile.Snapshot(venue, instrument, anchorLabel)` was previously private (`buildSnapshot`). Added public `Snapshot()` method mirroring `BuildSessionVolumeProfile.Snapshot()` pattern — resolves anchor from presets or constructs custom anchor.

### 4. Formalized HeatmapHotWriter Port

Added `HeatmapHotWriter` interface to `ports/ports.go`:
```go
type HeatmapHotWriter interface {
    UpsertHeatmapSnapshot(ctx context.Context, snapshot domain.HeatmapArtifactV1) *problem.Problem
}
```

### 5. Read Model Ports (`ports/readers.go`)

New file with 4 read model interfaces for querying persisted artifacts from storage:

- `VolumeProfileReader` — range query for VPVR snapshots
- `HeatmapReader` — range query for heatmap artifacts
- `SessionVolumeProfileReader` — query for SVP snapshots
- `TPOProfileReader` — query for TPO profiles

Each with corresponding query struct supporting venue/instrument/timeframe/anchor filters plus time range and limit.

### 6. Delivery Governance Completion

**Envelope Policy** — registered 6 previously missing insights event types:
- `insights.heatmap_delta`
- `insights.volume_profile_delta`
- `insights.session_volume_profile`
- `insights.tpo_profile`
- `insights.fused_volume_profile_snapshot`
- `insights.fused_heatmap_snapshot`

**Backpressure Policy** — assigned priorities for new events:
- Heatmap delta: 55 (matches snapshot)
- Session VP / TPO: 18 (below VPVR, session-scoped = lower urgency)
- Fused artifacts: 15 (derived, lowest priority)

### 7. Tests

- **5 new domain tests** — catalog field validation, uniqueness, lookup, ownership, count
- **2 new service snapshot tests** — SVP and TPO round-trip (execute → snapshot → validate)
- **Extended not-found test** — covers all 4 snapshot query types
- **Extended delivery test** — validates all new event types pass envelope policy

## Files Modified

| File | Change |
|---|---|
| `internal/core/insights/domain/events.go` | **NEW** — centralized event catalog |
| `internal/core/insights/domain/events_test.go` | **NEW** — 5 catalog tests |
| `internal/core/insights/app/build_tpo_profile.go` | Added public `Snapshot()` method |
| `internal/core/insights/app/service.go` | Added SVP/TPO snapshot methods + key types |
| `internal/core/insights/app/service_snapshot_test.go` | Added SVP/TPO snapshot tests + extended not-found |
| `internal/core/insights/ports/ports.go` | Added `HeatmapHotWriter` interface |
| `internal/core/insights/ports/readers.go` | **NEW** — 4 read model interfaces |
| `internal/core/delivery/domain/envelope_policy.go` | Registered 6 new insights event types |
| `internal/core/delivery/domain/envelope_policy_test.go` | Extended to cover new types |
| `internal/core/delivery/domain/backpressure_policy.go` | Added priorities for new events |

## Constraints Honored

- No aggregation logic duplicated
- No logic moved to evidence
- No strategy or execution logic introduced
- No wire protocol changes (events use existing envelope format)
- All existing tests pass without modification

## Test Results

```
ok  github.com/market-raccoon/internal/core/insights/app      0.371s
ok  github.com/market-raccoon/internal/core/insights/domain    0.535s
ok  github.com/market-raccoon/internal/core/delivery/app       0.353s
ok  github.com/market-raccoon/internal/core/delivery/domain    0.194s
ok  github.com/market-raccoon/internal/actors/insights/runtime 0.249s
```

## Architecture Summary

Insights is now a fully materialized bounded context:

```
                    ┌─────────────────────────────────┐
                    │         INSIGHTS BC              │
                    │                                   │
  marketdata.trade ─┤  Use Cases:                       │
                    │    BuildVolumeProfile              │─── insights.volume_profile_*
                    │    BuildHeatmap                    │─── insights.heatmap_*
                    │    JoinCrossVenueTrades            │─── insights.crossvenue.*
                    │    BuildSessionVolumeProfile       │─── insights.session_volume_profile
                    │    BuildTPOProfile                 │─── insights.tpo_profile
                    │                                   │
                    │  Fusion:                           │
                    │    FuseVolumeProfiles              │─── insights.fused_volume_profile_snapshot
                    │    FuseHeatmaps                    │─── insights.fused_heatmap_snapshot
                    │                                   │
                    │  Query Surface:                    │
                    │    SnapshotHeatmap                 │    (in-memory)
                    │    SnapshotVolumeProfile           │    (in-memory)
                    │    SnapshotSessionVolumeProfile    │    (in-memory)
                    │    SnapshotTPOProfile              │    (in-memory)
                    │                                   │
                    │  Ports:                            │
                    │    VolumeProfileHotWriter          │    (write)
                    │    HeatmapHotWriter                │    (write)
                    │    SessionVolumeProfileWriter      │    (write)
                    │    TPOProfileWriter                │    (write)
                    │    VolumeProfileReader             │    (read)
                    │    HeatmapReader                   │    (read)
                    │    SessionVolumeProfileReader      │    (read)
                    │    TPOProfileReader                │    (read)
                    │                                   │
                    │  Governance:                       │
                    │    EventCatalog (10 events)        │
                    │    VPVREmitPolicy (overload)       │
                    │    SessionEmitPolicy (cadence)     │
                    └─────────────────────────────────┘
```
