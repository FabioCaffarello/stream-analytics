# Stage 28 -- Artifact Latency Surface & Per-Cell Diagnostics

**Status:** COMPLETE
**Date:** 2026-03-06

## Executive Summary

S28 surfaces per-artifact latency/staleness diagnostics using the canonical
`Stream_Apply_State.last_recv_ms` array and policy-driven stale detection
thresholds from `Artifact_Policy.stale_detection`. All new code is pure
queries -- zero new canonical state, zero wire changes, zero protocol logic
outside `md_common`.

## Current-State Audit (Pre-S28)

- `last_recv_ms: [Artifact_Kind]i64` tracked per artifact since S22 -- never
  surfaced in HUD, health panel, or copy diagnostics.
- `Stale_Detection` enum (None, TF_Adaptive, Dual_Silence) defined in policy
  table since S22 -- used only indirectly by `compute_candle_health` and the
  12s dual-silence check in `health.odin`.
- No pure query to compute per-artifact age or classify staleness.
- Health panel showed event counts and live/synthetic flags but not recency.
- HUD status bar showed composition + artifact count but no stale indicator.

## S28 Architecture

All additions follow the S22-S27 pattern: pure functions in `md_common`,
adapter/display in `app`, zero new canonical state.

### New Pure Queries (md_common/stream_apply_state.odin)

| Function | Signature | Purpose |
|----------|-----------|---------|
| `apply_state_artifact_age_ms` | `(s, kind, now_ms) -> i64` | Age in ms since last event (-1 if never received) |
| `apply_state_artifact_staleness` | `(s, kind, now_ms, tf_ms?) -> Artifact_Staleness` | Policy-driven classification |
| `apply_state_stale_artifact_count` | `(s, now_ms, tf_ms?) -> (stale, aging)` | Count artifacts in stale/aging state |

### New Enum

```
Artifact_Staleness :: enum u8 {
    Unknown,   // Never received
    Fresh,     // Within thresholds
    Aging,     // Warning zone
    Stale,     // Exceeded threshold
}
```

### Staleness Thresholds (Policy-Driven)

| Stale_Detection | Aging Threshold | Stale Threshold | Artifacts |
|-----------------|-----------------|-----------------|-----------|
| None | N/A | N/A | Trade, Evidence, Signal, Tape, Heatmap, VPVR, Range_Candle |
| TF_Adaptive | max(2*tf_ms, 5s) | max(3*tf_ms, 10s) | Candle |
| Dual_Silence | 8s | 12s | Orderbook, Stats |

Thresholds are consistent with existing `compute_candle_health` and the 12s
dual-silence check in `health.odin`.

## Display Changes

### HUD Status Bar
- New `AGE T:Ns OB:Ns ST:Ns CD:Ns` badge (250ms throttled cache).
- Apply state summary badge now includes `STALE!` or `aging` suffix when
  any artifact crosses threshold.

### Health Panel (APPLY STATE section)
- New Row 4: `age: T=Ns OB=Ns(ok) ST=Ns(ok) CD=Ns(ok)` with staleness
  labels in parentheses.
- Row color: red if any stale, yellow if any aging, default otherwise.

### Copy Diagnostics
- New line: `age T=Ns OB=Ns(ok) ST=Ns(ok) CD=Ns(ok)`.
- Conditional `staleness: stale=N aging=N` line when non-zero.

## Code Changes

| File | Change |
|------|--------|
| `md_common/stream_apply_state.odin` | +`Artifact_Staleness` enum, +3 pure queries |
| `app/app.odin` | +`age_buf/age_len` to `Telemetry_HUD_Cache` |
| `app/build_status.odin` | +age cache in HUD refresh, +age row in health panel, +age in copy diagnostics, +`tf_ms_for_staleness`, `age_ms_short`, `staleness_label` helpers |
| `app/build_ui.odin` | +age badge in HUD overlay |
| `md_common/store_boundary_test.odin` | +9 S28 tests |

## Tests

171 tests in md_common (up from 162). 9 new S28 tests:

1. `test_s28_artifact_age_never_received` -- returns -1 for unseen artifacts
2. `test_s28_artifact_age_after_event` -- correct age computation
3. `test_s28_staleness_none_always_fresh` -- Trade stays Fresh regardless of age
4. `test_s28_staleness_dual_silence` -- Orderbook/Stats: Fresh -> Aging -> Stale
5. `test_s28_staleness_tf_adaptive` -- Candle: TF-scaled thresholds
6. `test_s28_staleness_unknown_when_never_received` -- Unknown for unseen
7. `test_s28_stale_artifact_count` -- counts stale/aging correctly
8. `test_s28_age_resets_on_new_event` -- new event refreshes age
9. `test_s28_age_tf_change_resets_tf_sensitive` -- TF change clears TF-sensitive age

## Constraints Verified

- Zero wire contract changes
- Zero new protocol logic outside md_common
- Zero new canonical state (all derived from existing `last_recv_ms` + policy)
- Zero UI expansion beyond diagnostic surfaces
- All existing 162 tests still pass

## Risks

- **Low:** Staleness thresholds are hardcoded in pure queries rather than
  configurable. This matches the existing pattern (compute_candle_health
  hardcodes thresholds). If needed, thresholds can be parameterized later.

## Recommended S29

- **Per-cell latency indicators:** Surface staleness as cell header badges
  using `resolve_cell_composition` + `apply_state_artifact_staleness`.
- **Stale auto-recovery:** When an artifact crosses Stale, auto-trigger
  resync or resub using existing protocol engine transitions.
- **Latency histogram:** Track `last_recv_ms` delta distribution for
  per-artifact jitter analysis (wire-visible via METRICS frame).
