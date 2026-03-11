# Stage 144 — Snapshot Lifecycle & Recovery Policy

**Date:** 2026-03-09
**Status:** COMPLETE
**Tests:** 438 md_common (16 new) + 410 app — all green

## Objective

Harden the snapshot lifecycle and recovery policy, making behavior predictable
and unambiguous across bootstrap, stale, retry, and fallback scenarios.

## Changes

### 1. `Snapshot_Lifecycle` Enum (stream_apply_state.odin)

New canonical per-stream enum replacing the ambiguous combination of
`snapshot_seen` + `has_live` + `using_synthetic` + `recovery_attempts`:

| State | Meaning |
|---|---|
| **Absent** | No events received (`event_count == 0`) |
| **Pending** | Subscribed, but snapshot-gated artifacts unsatisfied (e.g., Orderbook) |
| **Degraded** | Snapshot was seen, but recovery in progress or synthetic fallback active |
| **Stale** | Recovery exhausted (3 attempts) — data untrustworthy |
| **Live** | All gates satisfied, data flowing normally |

**Decision flow** (ordered by priority):
1. `event_count == 0` → Absent
2. `recovery_attempts >= 3` → Stale (takes priority over pending gates)
3. Any `needs_snapshot_gate && !snapshot_seen` → Pending
4. `recovery_attempts > 0` → Degraded
5. Any `has_synthetic_fallback && using_synthetic && !has_live` → Degraded
6. Otherwise → Live

### 2. Recovery Resets Snapshot Gates (stream_apply_state.odin)

`apply_state_mark_recovery` now clears `snapshot_seen` for all
`reset_on_reconnect` artifacts (currently: Orderbook, Range_Candle).

**Rationale:** Recovery triggers a resubscribe, which is semantically
equivalent to a reconnect. Without this, stale orderbook data could persist
after recovery without a fresh snapshot being required.

### 3. `GETRANGE_TIMEOUT_FRAMES` Constant (stream_apply_state.odin)

Explicit budget: `300` frames (5 seconds at 60fps). Replaces ad-hoc
`timeout_frames` values passed by callers.

### 4. Lifecycle Integration Points

- **`Cell_Surface_View.snapshot_lifecycle`** — populated by
  `resolve_cell_surface_view_with_stores`, available to all widget renderers
- **`Health_Tick_Output.snapshot_lifecycle`** — derived per-frame in
  `health_tick_evaluate`, available to health side-effect layer
- **`Apply_State_Telemetry.snapshot_lifecycle`** — available in HUD diagnostics
- **`Aggregate_Health_Summary`** — new per-lifecycle-state slot counts
  (`slots_snapshot_absent/pending/degraded/stale/live`) + `worst_snapshot`

### 5. Helper Functions

- `snapshot_lifecycle_label(sl) -> string` — display labels (ABSENT/PENDING/DEGRADED/STALE/OK)
- `snapshot_lifecycle_blocks_render(sl) -> bool` — Absent, Pending, Stale block; Degraded, Live allow

## Files Modified

| File | Change |
|---|---|
| `md_common/stream_apply_state.odin` | +Snapshot_Lifecycle enum, derivation, helpers; recovery gate reset; GETRANGE_TIMEOUT_FRAMES |
| `app/stream_slots.odin` | +snapshot_lifecycle on Cell_Surface_View, populated in resolution |
| `md_common/protocol_engine_test.odin` | +16 new tests |

## Tests Added (16)

| Test | Validates |
|---|---|
| `test_snapshot_lifecycle_absent_when_no_events` | Absent state |
| `test_snapshot_lifecycle_pending_when_ob_unsatisfied` | Pending when OB gate open |
| `test_snapshot_lifecycle_live_when_all_gates_satisfied` | Live after OB snapshot |
| `test_snapshot_lifecycle_degraded_during_recovery` | Recovery clears OB → Pending |
| `test_snapshot_lifecycle_stale_when_exhausted` | Stale after 3 recovery attempts |
| `test_snapshot_lifecycle_degraded_when_synthetic_active` | Synthetic fallback → Degraded |
| `test_snapshot_lifecycle_live_after_synthetic_displaced` | Live data displaces synthetic |
| `test_recovery_resets_snapshot_gates` | Recovery clears OB, preserves Trade |
| `test_snapshot_lifecycle_label` | All 5 label strings |
| `test_snapshot_lifecycle_blocks_render` | Render blocking policy |
| `test_getrange_timeout_constant` | GETRANGE_TIMEOUT_FRAMES == 300 |
| `test_health_tick_includes_snapshot_lifecycle` | Pending → Live transition in tick |
| `test_aggregate_health_snapshot_counts` | Per-lifecycle slot counts |
| `test_telemetry_includes_snapshot_lifecycle` | Telemetry struct populated |

## Architectural Decisions

1. **Stale takes priority over Pending** — When recovery is exhausted, the
   stream is fundamentally untrustworthy regardless of individual gate state.
   Recovery itself clears gates, so checking gates after exhaustion would
   always return Pending (masking the real problem).

2. **Recovery clears snapshot gates** — Resubscribe ≈ reconnect for
   snapshot-gated artifacts. This closes the gap where orderbook data from
   a previous connection could be rendered without validation after recovery.

3. **No new state fields** — `Snapshot_Lifecycle` is fully derived from
   existing `Stream_Apply_State` fields. Zero new mutable state.

4. **`snapshot_lifecycle_blocks_render` is independent of `stream_reliability_blocks_render`** —
   They check orthogonal concerns (snapshot validity vs. transport/delivery health).
   Widgets can consult either or both.

## Zero Regressions

- All 438 md_common tests pass
- All 410 app tests pass
- No wire-breaking changes
- No schema version bump needed (no persisted state changed)
