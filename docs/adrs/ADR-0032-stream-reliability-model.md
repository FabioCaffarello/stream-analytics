# ADR-0032: Stream Reliability Model

**Status:** Accepted
**Date:** 2026-03-09
**Stage:** S143 — Stream Health & Desync Model Hardening

## Context

The client has three orthogonal state systems that contribute to stream health:

1. **Transport** (`Stream_State`): Offline / Live / Lag / Desync
2. **Delivery** (`Composition_Stage`): Empty / Range_Pending / Backfilled / Live_Only / Composed
3. **Health** (`System_Health_Level`): Healthy / Degraded / Unhealthy / Critical

Plus recovery state (`Recovery_Status`): None / Recovering / Exhausted.

**Problem:** No single canonical answer to "is this stream's data trustworthy?" existed. Widgets used `widget_data_readiness` which checked stores and composition but not transport state or recovery exhaustion. A widget could display stale or desynced data as if it were healthy.

Specific failure modes:
- Recovery exhaustion on compare pane slots didn't escalate consistently
- Offline connection with cached store data rendered as fully Active
- Desync with old data in ring buffer showed as healthy chart
- No distinction between "data present + unreliable" vs "data absent + unreliable"

## Decision

Introduce `Stream_Reliability` as the **canonical reliability gate**:

```
Stream_Reliability :: enum u8 {
    Reliable,              // All good
    Degraded_Aging,        // Artifacts aging, render with visual warning
    Stale_Recovering,      // Auto-recovery in progress, render with warning
    Stale_Unrecoverable,   // Recovery exhausted, block render
    Desync,                // Transport desync, block render
    Offline,               // Transport disconnected, block render
    Manual_Resync,         // Exhausted + desync, manual intervention required
}
```

### Derivation

`stream_reliability(health, recovery, is_desync, is_offline) → Stream_Reliability`

Priority order (highest wins):
1. `is_offline` → `.Offline`
2. `is_desync + exhausted` → `.Manual_Resync`
3. `is_desync` → `.Desync`
4. `exhausted` → `.Manual_Resync`
5. `Critical/Unhealthy + not recovering` → `.Stale_Unrecoverable`
6. `Unhealthy/Degraded + recovering` → `.Stale_Recovering`
7. `Degraded` → `.Degraded_Aging`
8. else → `.Reliable`

### Render Policy

`stream_reliability_blocks_render(r)` returns true for:
- `Stale_Unrecoverable`, `Desync`, `Offline`, `Manual_Resync`

For blocked + data-present: widgets show `Degraded` overlay (data visible with warning).
For blocked + no-data: widgets show `Error`/`Offline` overlay (existing behavior).

### Widget Readiness Integration

> **Implementation Note (S154):** The three `Data_Readiness` unreliable variants proposed
> below were **not implemented**. S154 established a clean separation: `Data_Readiness`
> tracks pure data availability (6 variants: `Not_Ready` through `Live_Usable`), while
> reliability is checked separately in `resolve_pane_visual_state`. The reliability gate
> consults `Cell_Surface_View.reliability` directly, mapping blocked+data-present to
> `Pane_Visual_State.Degraded`. This avoids conflating data availability with transport
> trustworthiness in a single enum.

~~`Data_Readiness` gains three new variants:~~
- ~~`Stale_Unreliable` — data present, stream stale/exhausted~~
- ~~`Desync_Unreliable` — data present, stream desynced~~
- ~~`Offline_Unreliable` — data present, transport offline~~

~~These map to `Pane_Visual_State.Degraded` (new variant), which renders the "Unreliable" overlay.~~

**Actual implementation:** `Pane_Visual_State.Degraded` is resolved by `resolve_pane_visual_state`
which checks `stream_reliability_blocks_render(surface.reliability)` when data is present.
No new `Data_Readiness` variants were needed.

## Ownership

| Layer | Owner | State |
|-------|-------|-------|
| Transport | `stream_controller.odin` | `Stream_State`, `Stream_Desync_Reason` |
| Delivery | `stream_apply_state.odin` | `Composition_Stage`, `Artifact_Staleness` |
| Health | `stream_apply_state.odin` | `System_Health_Level`, `Recovery_Status` |
| **Reliability** | `stream_apply_state.odin` | `Stream_Reliability` (derived, pure) |
| Surface | `stream_slots.odin` | `Cell_Surface_View.reliability` |
| Readiness | `widget_readiness.odin` | `Data_Readiness` (pure data availability; reliability checked separately in `resolve_pane_visual_state`) |
| Visual | `shell_common.odin` | `Pane_Visual_State.Degraded` |

## State Transition Flow

```
Backend (WS) → Transport (Stream_Controller)
                    ↓
                Stream_State (Offline/Live/Lag/Desync)
                    ↓
Apply State ← Events/Metrics → Artifact_Staleness → System_Health_Level
                    ↓                                       ↓
              Recovery_Status ←—————————————————→ stream_reliability()
                                                        ↓
                                                Stream_Reliability
                                                        ↓
                                            Cell_Surface_View.reliability
                                                        ↓
                                            widget_data_readiness()
                                                        ↓
                                            Data_Readiness (pure availability)
                                                        ↓
                                            resolve_pane_visual_state()
                                              ↓ (checks reliability separately)
                                            Pane_Visual_State.Degraded
```

## Consequences

### Positive
- Single canonical answer to "is this data trustworthy?"
- Widgets can no longer appear healthy with stale/desynced streams
- Cached data remains visible with warning instead of blank screen
- Pure function: deterministic, testable, no allocations
- Compare pane slots get consistent reliability evaluation

### Negative
- One more enum for developers to understand
- Behavioral change: Offline/Desync/Critical with cached data now shows Degraded instead of blocking

### Trade-offs
- `Degraded_Aging` and `Stale_Recovering` do NOT block render — the data is stale but the system is actively recovering. Blocking would cause unnecessary blank screens during transient staleness.
- `Stale_Unrecoverable` blocks render even though data exists. The reasoning: if recovery is exhausted and health is Unhealthy/Critical, the data may be arbitrarily old and misleading.

## Files Changed

- `client/src/core/md_common/stream_apply_state.odin` — `Stream_Reliability` enum + derivation
- `client/src/core/app/stream_slots.odin` — `Cell_Surface_View.reliability` field + wiring
- `client/src/core/app/widget_readiness.odin` — `Data_Readiness` (unchanged; reliability handled in `resolve_pane_visual_state`)
- `client/src/core/app/shell_common.odin` — `Pane_Visual_State.Degraded` + overlay
- `client/src/core/app/health.odin` — `Health_Tick_Input.is_desync` + compare pane docs
- `client/src/core/md_common/md_common_test.odin` — 12 new reliability tests
- `client/src/core/app/marketdata_test.odin` — 10 new integration tests + 1 updated
