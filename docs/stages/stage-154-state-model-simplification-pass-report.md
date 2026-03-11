# Stage 154 — State Model Simplification Pass

**Date:** 2026-03-10
**Status:** COMPLETE
**Tests:** 1,217 total (447 app + 512 md_common + 54 layers + 204 services), all green

## Objective

Simplify and consolidate the client's operational state model, reducing coupling and overlap between health, readiness, data availability, and UI overlay states.

## Changes

### Phase 1: Remove Dead Fields from Cell_Surface_View

| Field | Reason |
|-------|--------|
| `candle_health` | Written but never read from struct; global `state.candle_health` is what the render loop uses |
| `snapshot_lifecycle` | Written but never read; only consumed via `Apply_State_Telemetry` for diagnostics |
| `is_transport_lagging` | Written but never read; consumers query `active_metrics.state` directly |

### Phase 1b: Remove Dead Enum Variant

- **`Pane_Visual_State.No_History`**: Defined and handled in `draw_pane_state_overlay` but never produced by any readiness-to-visual mapping. Dead code path.
- Removed `_state_sub_label_no_history` proc (sole consumer of `No_History`).

### Phase 2: Internalize Intermediate Fields

| Field | Reason |
|-------|--------|
| `recovery_status` | Only used to derive `reliability`; now a local variable in resolve functions |
| `stale_count` / `aging_count` | Only used to derive `health_level`; now implicit in `stream_health_level()` call |

### Phase 3: Separate Data Readiness from Stream Reliability

**Before:** `Data_Readiness` had 3 unreliable variants (`Stale_Unreliable`, `Desync_Unreliable`, `Offline_Unreliable`) that duplicated `Stream_Reliability` semantics. `widget_data_readiness` checked reliability and returned readiness-encoded reliability states — conflating two concerns.

**After:** Clean separation:
- **`Data_Readiness`** (6 variants) = pure data availability: "how much data does this widget have?"
- **`Stream_Reliability`** = stream trust: "can this stream's data be trusted?"
- **`resolve_pane_visual_state`** checks reliability *after* readiness, producing `Degraded` when data is present but stream is untrustworthy.

This means:
- `widget_data_readiness` no longer checks reliability — it purely answers data availability.
- `readiness_to_visual_state` is a simple 1:1 mapping (6 → 6).
- The reliability gate lives in `resolve_pane_visual_state` where it belongs.

## Result: Cell_Surface_View Before/After

**Before (17 fields):**
```
composition, candle_health, has_live_data, artifact_has_live,
stale_count, aging_count, venue, symbol, stream_bound,
health_level, recovery_status, recovery_attempts, reliability,
snapshot_lifecycle, is_transport_lagging, backfill_expectation
```

**After (10 fields):**
```
composition, has_live_data, artifact_has_live, venue, symbol,
stream_bound, health_level, recovery_attempts, reliability,
backfill_expectation
```

## Data_Readiness Before/After

**Before (9 variants):**
```
Not_Ready, Loading, Snapshot_Pending, Seeding,
Partial_Usable, Live_Usable,
Stale_Unreliable, Desync_Unreliable, Offline_Unreliable
```

**After (6 variants):**
```
Not_Ready, Loading, Snapshot_Pending, Seeding,
Partial_Usable, Live_Usable
```

## Pane_Visual_State Before/After

**Before (9 variants):**
```
Active, Loading, Seeding, Snapshot_Pending, Empty,
No_History, Offline, Error, Degraded
```

**After (8 variants):**
```
Active, Loading, Seeding, Snapshot_Pending, Empty,
Offline, Error, Degraded
```

## Architectural Decision

**Kept separate (no unification):**
- `Composition_Stage` — "where is this cell in its historical+live data assembly?"
- `Snapshot_Lifecycle` — "are snapshot-gated artifacts satisfied?"

These serve genuinely different purposes, read different fields, and share no enum values.

## Files Changed

| File | Changes |
|------|---------|
| `stream_slots.odin` | Cell_Surface_View: removed 6 fields, updated both resolve functions |
| `widget_readiness.odin` | Data_Readiness: removed 3 variants, simplified widget_data_readiness and readiness_to_visual_state |
| `shell_common.odin` | Pane_Visual_State: removed No_History, moved reliability check to resolve_pane_visual_state |
| `interaction_test.odin` | Removed recovery_status from test Cell_Surface_View literals |
| `marketdata_test.odin` | Rewrote S143 unreliable tests as S154 separation tests, removed No_History test |

## Deferred

- **Phase 4 (health_level → reliability color mapping):** `health_level` remains on Cell_Surface_View (1 byte, 3 genuine consumers). Could be replaced with reliability-derived color in `draw_health_dot`, but the cost of keeping it is negligible.
