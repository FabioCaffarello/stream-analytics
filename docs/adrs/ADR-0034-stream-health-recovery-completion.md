# ADR-0034: Stream Health & Recovery Completion

**Status:** Accepted
**Date:** 2026-03-09
**Supersedes:** Extends ADR-0032 (Stream Reliability Model)

## Context

The stream health model evolved across S29-S145 into a multi-layered system with pure
derivation functions and clear ownership. S151 audited the full pipeline and found one
design issue: recovery exhaustion was escalating to transport DESYNC (polluting transport
state with delivery-layer information). This ADR documents the complete, corrected model.

## Decision

### 5-Layer Health Pipeline

The health model is organized into 5 orthogonal layers, each with explicit ownership:

```
┌──────────────────────────────────────────────────────────────────────┐
│  Layer 1: TRANSPORT           stream_controller.odin                │
│  Owns: Stream_State (Offline/Live/Lag/Desync)                       │
│  Detects: connection loss, message age, clock drift, seq gaps       │
│  Writer: controller_update_health (per-frame)                       │
│  Key thresholds: lag_warn=4s, desync_stale=12s, clock_drift=8s      │
├──────────────────────────────────────────────────────────────────────┤
│  Layer 2: DELIVERY            stream_apply_state.odin               │
│  Owns: per-artifact tracking (snapshot_seen, has_live, last_recv)   │
│  Derives: Composition_Stage (Empty→Composed)                        │
│  Derives: Artifact_Staleness (Unknown/Fresh/Aging/Stale per kind)   │
│  Writer: store_adapters.odin (sole writer)                          │
├──────────────────────────────────────────────────────────────────────┤
│  Layer 3: SNAPSHOT            stream_apply_state.odin               │
│  Owns: Snapshot_Lifecycle (Absent→Pending→Degraded→Stale→Live)      │
│  Derives from: event_count, snapshot gates, recovery_attempts       │
│  Writer: derived (pure function, no state)                          │
├──────────────────────────────────────────────────────────────────────┤
│  Layer 4: HEALTH & RECOVERY   stream_apply_state.odin + health.odin │
│  Owns: System_Health_Level (Healthy→Critical)                       │
│  Owns: Recovery_Status (None/Recovering/Exhausted)                  │
│  Owns: Remediation_Decision (None/Resubscribe/Cooldown/Exhausted)   │
│  Recovery: 3 attempts, 15s→30s→60s exponential backoff              │
│  Writer: health_tick_evaluate (pure), health.odin (side effects)    │
├──────────────────────────────────────────────────────────────────────┤
│  Layer 5: RELIABILITY         stream_apply_state.odin               │
│  Owns: Stream_Reliability (7-state canonical gate)                  │
│  Merges: transport + health + recovery → single trust answer        │
│  Writer: stream_reliability() pure function                         │
│  Consumer: widget_data_readiness → Pane_Visual_State → render       │
└──────────────────────────────────────────────────────────────────────┘
```

### 5 Explicit Failure Scenarios

| Scenario | Transport | Recovery | Reliability | Render |
|---|---|---|---|---|
| **Transport OK + Snapshot Stale** | Live | Exhausted | Manual_Resync | Blocked |
| **Feed Lagging** | Lag or Live | None | Reliable or Degraded_Aging | Allowed |
| **Desync Recoverable** | Desync | None/Recovering | Desync | Blocked |
| **Desync Exhausted** | Desync | Exhausted | Manual_Resync | Blocked |
| **Manual Resync Required** | Any | Exhausted | Manual_Resync | Blocked |

**Feed Lagging** has two independent channels:
1. Transport lag: `Stream_State == .Lag` (message age > 4s) — visible in status bar
2. Artifact aging: per-artifact staleness → `Degraded_Aging` — visible in health badges

These are distinct signals: transport lag is connection-level, artifact aging is per-feed.

### S151 Fix: No DESYNC Escalation from Recovery Exhaustion

**Before (S29-S150):** When `Remediation_Decision == .Exhausted`, the active stream path
called `controller_mark_desync(.Snapshot_Stale)`, forcing `state.active_metrics.state = .Desync`.
This polluted transport state with delivery-layer information, making "transport ok + snapshot stale"
indistinguishable from "real transport desync".

**After (S151):** Recovery exhaustion is handled solely through the reliability model:
`recovery == .Exhausted` → `stream_reliability()` returns `.Manual_Resync` without needing
`is_desync=true`. This aligns active stream behavior with compare pane behavior (which already
avoided DESYNC escalation since S143).

**Impact:**
- Transport state accurately reflects transport health at all times
- Delivery-layer exhaustion flows through the unified reliability model
- Ctrl+R manual resync continues to work (it's not gated on transport state)
- No behavioral change for the operator — Manual_Resync still blocks render and shows "Ctrl+R"

### Stream_Reliability Decision Tree (unchanged)

```
IF is_offline                               → Offline
IF is_desync AND recovery == Exhausted      → Manual_Resync
IF is_desync                                → Desync
IF recovery == Exhausted                    → Manual_Resync
IF health == Critical                       → Stale_Unrecoverable
IF health == Unhealthy AND recovering       → Stale_Recovering
IF health == Unhealthy                      → Stale_Unrecoverable
IF health == Degraded AND recovering        → Stale_Recovering
IF health == Degraded                       → Degraded_Aging
OTHERWISE                                   → Reliable
```

**Render policy:**
- ALLOWS render: `Reliable`, `Degraded_Aging`, `Stale_Recovering`
- BLOCKS render: `Stale_Unrecoverable`, `Desync`, `Offline`, `Manual_Resync`

### Cell_Surface_View — Unified Read Model

All per-cell UI decisions consume `Cell_Surface_View`:

> **Implementation Note (S154):** The field list below was simplified in S154. Fields
> `recovery_status`, `snapshot_lifecycle`, `is_transport_lagging`, `candle_health`,
> `stale_count`, and `aging_count` were removed. `artifact_has_live` (per-artifact live
> flags) and `backfill_expectation` (TF-aware availability, S152) were added. The 10-field
> ceiling guard rail is enforced.

| Field | Source | Purpose |
|---|---|---|
| `composition` | apply_state | Historical/realtime mix stage |
| `has_live_data` | apply_state | Any artifact with live data |
| `artifact_has_live` | apply_state | S125: per-artifact live flags |
| `venue` / `symbol` | stream_info | Resolved display labels |
| `stream_bound` | cell config | Has explicit stream binding |
| `health_level` | apply_state + now_ms | Aggregate artifact health |
| `recovery_attempts` | apply_state | Attempt count for UX |
| `reliability` | transport + health + recovery | Canonical trust gate |
| `backfill_expectation` | TF policy + composition | S152: TF-aware availability |

### Ownership Invariants

1. **Transport layer never reads delivery state** — it only sees connection, seq, timestamps
2. **Delivery layer never writes transport state** — it only tracks per-artifact events
3. **Recovery decisions never force transport transitions** (S151 fix)
4. **All health/reliability values are pure-derived** — no cached state, deterministic
5. **Side effects are applied only by the frame loop** in `health.odin`

## Consequences

- Health model is explicit and consistent across all 5 layers
- Transport and delivery failures are distinguishable at every level
- Compare panes and active stream use identical reliability derivation
- Recovery exhaustion no longer creates synthetic DESYNC states
- 1,188 tests validate the model (493 md_common + 437 app + 54 layers + 204 services)
