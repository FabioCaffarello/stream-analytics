# Stage 27 — Telemetry HUD Expansion & Operational Diagnostics

**Date:** 2026-03-06
**Status:** COMPLETE
**Tests:** 162 in md_common (up from 156), 6 new S27 tests

## Executive Summary

S27 makes the client runtime operationally observable by surfacing per-artifact event counts, composition stage, live/synthetic status, and getrange state from the canonical `Stream_Apply_State` into the HUD status bar, health panel, and Copy Diagnostics clipboard output. All new data is derived from existing canonical state — zero new state fields, zero new protocol logic, zero wire contract changes.

## Current-State Audit (Pre-S27)

| Layer | State | Gap |
|---|---|---|
| `Stream_Apply_State` | `artifact_event_count[Kind]u64`, `has_live`, `using_synthetic`, `last_recv_ms` per artifact | Not surfaced in HUD or health panel |
| `Apply_State_Summary` | 7 fields (has_live_*, snapshot_seen, getrange_seeded, composition_stage) | Missing event counts |
| HUD Cache | MPS, BPS, CB, Arena, PM, PR, PB, phase timing | No artifact counts, no composition summary |
| Status bar | HM/VP/CD live badges, CTX stage | Good but no event volume info |
| Health panel | Streams, Transport, M4 Budgets, Server, Evidence, Log | No per-artifact diagnostics |
| Copy Diagnostics | Streams, Layers, Transport, M4, Protocol, Server, Evidence, Log | No apply state section |

## S27 Architecture

### Design Principles
1. **All telemetry is derived** — `Apply_State_Telemetry` and `apply_state_active_artifact_count` are pure functions over `Stream_Apply_State`
2. **Widgets remain passive readers** — HUD reads from cache, health panel reads from `apply_state_telemetry()`
3. **No new canonical state** — all information was already tracked, just not surfaced
4. **Cache throttled at 250ms** — same cadence as existing HUD cache refresh

### Data Flow
```
Stream_Apply_State (canonical, per-stream)
    |
    +-- apply_state_telemetry() --> Apply_State_Telemetry (pure snapshot)
    |       used by: health panel, Copy Diagnostics
    |
    +-- apply_state_active_artifact_count() --> int (pure query)
    |       used by: HUD cache, health panel, Copy Diagnostics
    |
    +-- apply_state_summary() --> Apply_State_Summary (expanded with event counts)
            used by: existing consumers + tests
```

## Changes

### md_common/stream_apply_state.odin
- **Expanded `Apply_State_Summary`**: Added `artifact_event_count: [Artifact_Kind]u64` and `event_count: u64`
- **New `Apply_State_Telemetry` struct**: Full diagnostics view (event counts, last_recv_ms, has_live, using_synthetic, composition_stage, getrange state)
- **New `apply_state_telemetry()` pure function**: Creates telemetry snapshot from apply state
- **New `apply_state_active_artifact_count()` pure function**: Counts artifacts with >0 events

### app/app.odin — Telemetry_HUD_Cache
- Added `artifact_buf[128]/artifact_len`: Per-artifact event count cache line
- Added `apply_buf[96]/apply_len`: Composition + active artifact summary cache line

### app/build_status.odin
- **`refresh_telemetry_hud_cache`**: Populates new artifact count and apply state cache lines
- **Health panel**: New "APPLY STATE" section with 3 rows:
  - Row 1: Composition stage, total events, active artifact count, getrange state
  - Row 2: Per-artifact event counts (T, OB, ST, CD, HM, VP, EV, SG, TP, RC)
  - Row 3: Per-artifact live/synthetic status flags
- **`copy_diagnostics_to_clipboard`**: New "APPLY STATE" section with stage, event counts, live/synthetic flags

### app/build_ui.odin
- HUD status bar: New `COMP:xxx ART:n/10 EVT:nnn` badge after phase timing, color-coded by composition stage

## Tests (6 new, 162 total)

| Test | Validates |
|---|---|
| `test_s27_summary_includes_event_counts` | Summary `artifact_event_count` + `event_count` populated correctly |
| `test_s27_telemetry_mirrors_apply_state` | `Apply_State_Telemetry` mirrors all apply state fields exactly |
| `test_s27_active_artifact_count` | Active artifact count increments/resets correctly |
| `test_s27_telemetry_synthetic_state` | Telemetry reflects synthetic fallback → live transition |
| `test_s27_telemetry_composition_after_tf_change` | Telemetry composition resets on TF change, event counts survive |
| `test_s27_summary_event_count_consistency` | `event_count == sum(artifact_event_count[*])` invariant holds |

## Risks

- **HUD space**: Status bar is dense. New badge only appears when HUD is enabled (T key), so normal users are unaffected.
- **Health panel height**: New APPLY STATE section adds ~3 rows. Panel already scrolls for dense configurations.

## Recommended S28

**Artifact Latency Surface & Per-Cell Diagnostics**
- Surface `last_recv_ms` per artifact as age-since-last in health panel (e.g., "ST: 2.1s ago")
- Per-cell composition badge in cell headers (already have `resolve_cell_composition`)
- Optional per-cell artifact event counts for bound cells
- Stale artifact detection using `last_recv_ms` + policy `stale_detection` field
