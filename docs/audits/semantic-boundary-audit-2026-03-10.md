# Semantic & Boundary Audit — Market Raccoon

**Date:** 2026-03-10
**Scope:** Backend (Go), Client (Odin), Documentation (ADRs, stage reports)
**Method:** Cross-stack semantic analysis + boundary dependency verification
**Status:** Complete — no code changes

---

## 1. Canonical Semantics

### 1.1 Signal

| Layer | Type | Semantic |
|-------|------|----------|
| Backend (detection) | `SignalEvent` (`internal/core/signal/`) | Deterministic detection output from evidence evaluation |
| Backend (composition) | `CompositeSignalV1` (`internal/core/signals/`) | Composed signal after regime/cross-venue rules |
| Client | `MD_Channel.Signals`, `Signal_Store` | Data channel/artifact kind for market signals |
| Docs | ADR-0023 | Actionable alert derived from evidence; non-execution, non-order |

**Canonical definition:** Actionable alert derived from evidence. Never contains execution semantics. Feeds strategy layer but does not issue trade directives.

**Boundary:** Signal detection (`core/signal`) produces `SignalEvent`. Signal composition (`core/signals`) consumes `SignalEvent` and produces `CompositeSignalV1`. Strategy consumes `IntentInput` derived from signals via `SignalToIntentMapping` (`signal/handoff.go:57`).

---

### 1.2 Event

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `MarketEvent`, `ExecutionEventV1`, `PortfolioEventContract`, `DeliveryEvent` | Immutable fact, append-only, envelope-wrapped (ADR-0002) |
| Client | `Recovery_Event` | Transport/protocol state transition during auto-recovery |
| Docs | ADR-0002 | Atomic unit of domain communication with canonical envelope |

**Canonical definition:** Immutable, append-only fact with canonical envelope (type, version, venue, instrument, ts_exchange, ts_ingest, seq, idempotency_key, payload). Never retroactively modified; only superseded by new events.

**Envelope fields (ADR-0002):** `type`, `version`, `venue`, `instrument`, `ts_exchange`, `ts_ingest`, `seq`, `idempotency_key`, `payload`.

---

### 1.3 Intent

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `StrategyIntentV1` (`strategy/domain/intent.go:84`) | Trade directive: side, sizing, constraints, provenance |
| Client | Comment in `workspace.odin:58` | UI pane operational purpose (Primary_Chart, Auxiliary, Context) |
| Docs | ADR-0023 | Decision proposal to take/adjust risk; derived from signals and policy |

**Canonical definition:** Explicit domain decision proposal to take/adjust risk. Produced by strategy, consumed by executor. Contains IntentID, Side, Sizing, Constraints, Provenance with ParentSignalIDs.

**Note:** Client usage of "intent" for UI purpose diverges from canonical definition. See inconsistency I2.

---

### 1.4 State

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `StreamState` (marketdata) | HEALTHY / NEEDS_ATTENTION (anomaly counters) |
| Backend | `ControlState` (execution) | Active / Paused / Drained / Halted |
| Backend | `PortfolioStateV1` (portfolio) | Projected account-level balances, positions, exposures |
| Backend | `HealthState` (aggregation) | Healthy / NeedsResync (order book consistency) |
| Client | `Stream_State` (streams) | Connected / Hello_Pending / Running / Desync / Backoff |
| Client | `Composition_Stage` (md_common) | Empty / Range_Pending / Backfilled / Live_Only / Composed |
| Client | `System_Health_Level` (md_common) | Healthy / Degraded / Unhealthy / Critical |
| Client | `Stream_Reliability` (md_common) | Reliable / Degraded_Aging / Stale_Recovering / ... / Manual_Resync |
| Client | `Recovery_Status` (md_common) | None / Recovering / Exhausted |

**Canonical definition:** Deterministic aggregate state guarded by invariants. Always qualified by prefix to distinguish domain.

**Qualification rule:** Bare `State` is prohibited. Must be prefixed: `Stream_State`, `Control_State`, `Portfolio_State`, `Composition_Stage`, etc.

---

### 1.5 Summary

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `PortfolioSummaryV1` (`portfolio/domain/snapshot.go:56`) | Global roll-up across all accounts |
| Backend | `AccountSummaryV1` | Lightweight per-account aggregate |
| Backend | `FillSummaryV1` | Cumulative trade statistics |
| Client | `Portfolio_Summary_Result` | Global portfolio aggregation for display |
| Client | `Session_Health_Summary` | Venue/instrument counts from backend |
| Client | `Apply_State_Summary` | Stream-level summary for health dashboard |

**Canonical definition:** Rolled-up aggregation across multiple entities or time windows. Lightweight read model. Not source of truth — derived from events or snapshots.

**Distinction from Snapshot:** Summary = aggregated metrics. Snapshot = complete state at moment T.

---

### 1.6 Snapshot

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `AccountSnapshotV1` | Point-in-time account state (positions, balances, equity) |
| Backend | `BookSnapshot` | Full LOB depth at point in time |
| Backend | `StreamSnapshot` (signal) | Signal engine evidence history + watermarks |
| Backend | `HotSnapshotProvider` (delivery) | Latest state per subject for backfill |
| Client | `Snapshot_Lifecycle` (md_common) | Absent / Pending / Degraded / Stale / Live |
| Client | `Runtime_Snapshot` (md_common) | Deterministic capture for incident reproduction |

**Canonical definition:** Immutable point-in-time capture of complete state. Always includes full detail (vs. Summary which is aggregated). Snapshot gates track validity and freshness.

---

### 1.7 Session

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `Session` (`delivery/domain/session.go:28`) | Connected client WS lifecycle + subscriptions |
| Backend | `SessionActor` (`actors/delivery/runtime/session.go:116`) | Actor owning WS connection, buffering, outbound queue |
| Client | `Session_Health` (`services/session_health.odin`) | Backend health query result from `/api/v1/session/dashboard` |
| Docs | ADR-0007 | Per-connection isolation with subscription management |
| Docs | ADR-0033 | Session-scoped orderflow artifacts (SessionVolumeProfileV1, TPOProfileV1) |

**Canonical definition:** Single connected client lifecycle with subscription management and backpressure policy. Owned by delivery layer.

**Note:** Client usage refers to backend health query, not a client-side session concept. See inconsistency I3.

---

### 1.8 Readiness

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `TradingReadinessV1` (`execution/domain/readiness.go:48`) | Operational safety: control plane + portfolio staleness |
| Client | `Data_Readiness` (`app/widget_readiness.odin:20`) | Widget data availability: Not_Ready → Live_Usable |
| Client | `Trading_Readiness` (`services/trading_readiness.odin:60`) | Backend control plane state reflection |

**Canonical definition (backend):** Composed trading readiness surface integrating control plane state with portfolio staleness assessment. Answers: "Is it safe to execute trades?"

**Canonical definition (client):** Widget data completeness and freshness for rendering. Answers: "Does this widget have enough data to display?"

**Resolution:** Namespace-qualified: `Data_Readiness` (client rendering) vs `Trading_Readiness` (backend safety gate). Both are valid, distinct concepts.

---

### 1.9 Health

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `StreamHealth` (marketdata) | Observable anomalies: LastSeq, OutOfOrderCount, DuplicateCount |
| Backend | `HealthState` (aggregation) | Healthy / NeedsResync (order book consistency) |
| Backend | `SubsystemState` (actors/runtime) | Actor lifecycle health (Running, Degraded, Connected) |
| Client | `Candle_Health` (app) | No_Data / OK / Lagging / Stale |
| Client | `System_Health_Level` (md_common) | Healthy / Degraded / Unhealthy / Critical |
| Client | 5-layer pipeline (ADR-0034) | Transport → Delivery → Snapshot → Health+Recovery → Reliability |

**Canonical definition:** Observable monitoring layer based on counters and thresholds. Non-deterministic (depends on wall clock). Distinct from Readiness (operational safety) and Reliability (combined trust gate).

**Pipeline (client, ADR-0034):**
1. Transport: `Stream_State` (connection, seq, timestamps)
2. Delivery: per-artifact tracking (snapshot_seen, has_live, last_recv)
3. Snapshot: `Snapshot_Lifecycle` (Absent → Live)
4. Health+Recovery: `System_Health_Level` + `Recovery_Status` + `Remediation_Decision`
5. Reliability: `Stream_Reliability` (canonical trust gate for render decisions)

**Ownership invariants (ADR-0034):**
- Transport never reads delivery state
- Delivery never writes transport state
- Recovery decisions never force transport transitions (S151 fix)
- All health/reliability values are pure-derived, no cached state

---

### 1.10 Workspace

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `Workspace` (`workspace/domain/workspace.go:33`) | Root aggregate: schemaVersion, layoutV6, fingerprint, settings, savedAtMs |
| Client | `Workspace` (`app/workspace.odin`) | Root aggregate: split tree, panes, data context, mode |
| Docs | ADR-0024 through ADR-0031 | Dashboard domain aggregate root |

**Canonical definition:** First-class aggregate root for the dashboard domain. Owns layout tree, pane registry, data context, and persistence envelope. Single active workspace renders per frame. Schema-versioned for independent migration.

**Aligned cross-stack.** Backend validates and persists; client owns runtime state.

---

### 1.11 Portfolio

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `PortfolioStateV1` (`portfolio/domain/state.go:77`) | Projected account-level balances, positions, exposures, risk |
| Backend | `PortfolioSummaryV1` | Global roll-up across all accounts |
| Client | `Portfolio_Store` (`services/portfolio_store.odin`) | Read-only dashboard view of positions, equity, PnL, fills |
| Docs | ADR-0023 | Deterministic projected state derived from execution events |

**Canonical definition:** Read-model projection from execution events. NOT source of truth for positions (execution events are). Scoped: global / account / venue_account.

**Aligned cross-stack.** Backend projects; client displays.

---

### 1.12 Execution

| Layer | Type | Semantic |
|-------|------|----------|
| Backend | `ExecutionEventV1` (`execution/domain/event.go:53`) | Immutable order lifecycle record |
| Backend | `GovernedExecutor` | Authorization + simulation + routing |
| Backend | `ControlState` (Active/Paused/Drained/Halted) | Governance state machine |
| Client | Not first-class | Reflected via `Trading_Readiness` |
| Docs | ADR-0023 | Immutable event describing lifecycle of intent execution |

**Canonical definition:** Order lifecycle as immutable, append-only events. Status machine: unspecified → accepted → placed → partially_filled → filled/canceled/expired/failed. Provenance chains intent → execution → fills.

**Backend-only domain.** Client reflects control plane state via Trading_Readiness, does not manage execution.

---

## 2. Inconsistencies

### I1 — `signal` vs `signals` package naming (P1)

**Problem:** Two distinct subsystems with near-identical names.

| Package | Role |
|---------|------|
| `internal/core/signal/` | Detection engine (emitter, rules, state store, features) |
| `internal/core/signals/` | Composition engine (regimes, cross-venue, emitter protocol) |
| `internal/actors/signal/runtime/` | Detection actor runtime |
| `internal/actors/signals/runtime/` | Composition actor runtime |

**Evidence:** Filesystem listing confirms both exist. `signal/emitter.go` vs `signals/domain/composite_signal.go`. The singular/plural distinction does not communicate the architectural difference between detection and composition.

**Risk:** Import confusion; cognitive friction for contributors.

**Proposed resolution:** Rename to `detection` / `composition` within the signal bounded context, or consolidate under `signals/detection/` and `signals/composition/`.

---

### I2 — `intent` semantic divergence (P1)

**Problem:** Same term, incompatible meanings across stacks.

**Backend** (`internal/core/strategy/domain/intent.go:84`):
```go
type StrategyIntentV1 struct {
    IntentID    ids.IntentID
    Side        string        // buy|sell
    Sizing      IntentSizing
    Constraints ExecutionConstraints
    Provenance  IntentProvenance
}
```
Meaning: Trade directive from strategy to execution.

**Client** (`client/src/core/app/workspace.odin:58`):
```odin
// S119: Pane role — classifies pane operational intent.
```
Meaning: UI element operational purpose.

**Evidence:** The backend `intent` is a first-class domain type (ADR-0023 frozen contract). The client usage is a comment/documentation term, not a type name. No type conflict exists, but the ubiquitous language is violated.

**Proposed resolution:** Client should use `purpose` or `role` consistently instead of `intent` for UI concepts.

---

### I3 — `Session_Health` misnomer in client (P1)

**Problem:** `Session_Health` in the client does not represent session health.

**Backend** (`internal/core/delivery/domain/session.go:28`):
```go
type Session struct {
    SessionID     ids.SessionID
    Subscriptions map[string]Subscription
}
```
Meaning: WS connection lifecycle.

**Client** (`client/src/core/services/session_health.odin`):
```odin
Session_Health_Result :: struct { ... }
// Queried from /api/v1/session/dashboard
```
Meaning: Backend delivery health query — instrument freshness, resync coverage, per-venue counts.

**Evidence:** The client `Session_Health` queries a backend API about overall delivery health. It does not represent the health of the WS session itself.

**Proposed resolution:** Rename to `Delivery_Health` or `Backend_Health` in the client.

---

### I4 — `StreamState` (backend) vs `Stream_State` (client) (P2)

**Problem:** Same name, incompatible semantics.

**Backend** (`internal/core/marketdata/domain/instrument_stream.go:10`):
```go
type StreamState string // HEALTHY | NEEDS_ATTENTION
```
Meaning: Anomaly detection (duplicates, out-of-order events).

**Client** (`client/src/core/streams/stream_types.odin`):
```odin
Stream_State :: enum { Connected, Hello_Pending, Running, Desync, Backoff }
```
Meaning: Transport connection lifecycle.

**Evidence:** Both named `StreamState` / `Stream_State` but measure completely different things. No runtime conflict (different languages), but violates ubiquitous language.

**Proposed resolution:** Backend should use `StreamAnomalyState` or `StreamIntegrityState`. Document distinction in glossary.

---

### I5 — `Readiness` dual meaning (P2)

**Problem:** Two readiness concepts coexist in the client.

- `Data_Readiness` (`app/widget_readiness.odin:20`): Widget data availability (Not_Ready → Live_Usable)
- `Trading_Readiness` (`services/trading_readiness.odin:60`): Backend control plane authorization

**Evidence:** Both types exist in client codebase. The namespace qualification resolves the ambiguity at code level, but documentation should explicitly distinguish them.

**Status:** Resolved via qualification. Document in glossary.

---

### I6 — `State` overloading (P2)

**Problem:** 8+ types use `State` with distinct meanings.

| Type | Domain | Meaning |
|------|--------|---------|
| `StreamState` (backend) | marketdata | Anomaly classification |
| `Stream_State` (client) | streams | Transport lifecycle |
| `ControlState` | execution | Governance state |
| `PortfolioStateV1` | portfolio | Account projection |
| `HealthState` | aggregation | Book consistency |
| `Composition_Stage` | md_common | Data assembly progress |
| `System_Health_Level` | md_common | Artifact staleness |
| `Stream_Reliability` | md_common | Combined trust gate |

**Evidence:** Each type is prefix-qualified, which prevents code confusion. However, the term `State` alone is ambiguous in cross-team communication.

**Status:** Resolved via mandatory prefix qualification. Document qualification table in glossary.

---

## 3. Naming Rules

| # | Rule | Rationale |
|---|------|-----------|
| **N1** | Packages with distinct responsibilities MUST have distinct names — never differ only by singular/plural | `signal` vs `signals` confusion |
| **N2** | Backend domain terms MUST NOT be reused with different semantics in the client | `intent` (trade directive) != `intent` (UI purpose) |
| **N3** | `State` MUST always be prefix-qualified in type names | Prevents ambiguity across 8+ state types |
| **N4** | `Health` = observable monitoring (counters, thresholds); `Readiness` = operational safety gate | Prevents conflation |
| **N5** | `Summary` = rolled-up aggregation; `Snapshot` = complete point-in-time capture | Never interchange |
| **N6** | `Event` = immutable append-only fact; `Action` = user input in client | `Recovery_Event` (ok), `UI_Action` (ok) |
| **N7** | Cross-stack terms must have identical semantics or different names | Enforces ubiquitous language |

---

## 4. Boundary Rules

### 4.1 Backend Boundaries

| # | Rule | Status | Enforcement |
|---|------|--------|-------------|
| **B1** | `shared` has zero internal dependencies | PASS | `import_guard_test.go` |
| **B2** | Core domains never import each other (except `signals→evidence`, declared) | PASS | go.mod isolation |
| **B3** | Adapters import only `domain/` and `ports/` from core | PASS | go.mod + review |
| **B4** | Actors orchestrate; never imported by core or adapters | PASS | go.mod dependency flow |
| **B5** | Interfaces defined on consumer side (`ports/` packages) | PASS | Package structure |

**Dependency flow (verified):**
```
shared ← core/* ← adapters ← actors
              ↑                   ↑
              └───────────────────┘ (actors depend on core directly too)
```

No circular dependencies. No cross-domain imports in core (except `signals→evidence`).

### 4.2 Client Boundaries

| # | Rule | Status | Enforcement |
|---|------|--------|-------------|
| **B6** | `ports/` defines only contracts (enums, structs, proc signatures) | PASS | Code review |
| **B7** | `services/` never imports from `layers/` or `app/` | PASS | Zero violations found |
| **B8** | `layers/` never imports from `app/` | PASS | Zero violations found |
| **B9** | `md_common/` never imports from `layers/` or `app/` | PASS | Zero violations found |
| **B10** | State pipeline is unidirectional | PASS | Guard rails enforced |

**State pipeline (verified):**
```
Stream_Apply_State (md_common, 54 fields)
  → Cell_Surface_View (app, 10 fields max)
    → Data_Readiness (app, 6 variants max)
      → Pane_Visual_State (app, 8 variants max)
```

**Guard rails (verified):**
- Cell_Surface_View ceiling: 10 fields — PASS
- Data_Readiness: 6 variants — PASS
- Pure derivation only, no cached health/reliability state — PASS
- Per-stream store isolation (Market_Stream) — PASS
- md_common stores widget_kind as `u8` ordinal, not enum — PASS (prevents app coupling)

### 4.3 Cross-Stack Boundaries

| # | Rule | Status |
|---|------|--------|
| **B11** | Workspace semantics aligned backend↔client | PASS |
| **B12** | Portfolio is read-model in both stacks | PASS |
| **B13** | Execution is backend-only; client reflects via Trading_Readiness | PASS |
| **B14** | Event envelope contract (ADR-0002) consistent across wire format | PASS |

---

## 5. Conflict Backlog

### P0 — Blocking

None. Zero boundary violations, zero circular dependencies, zero semantic conflicts that cause bugs.

### P1 — Ubiquitous Language Conflicts

| ID | Type | Description | Evidence | Proposed Action |
|----|------|-------------|----------|-----------------|
| **S1** | Naming | `internal/core/signal/` vs `internal/core/signals/` — near-identical names for detection vs composition | Filesystem; go.mod | Rename to `detection` / `composition` |
| **S2** | Semantics | `intent` used as UI purpose in client vs trade directive in backend | `workspace.odin:58` vs `strategy/domain/intent.go:84` | Client: replace with `role` or `purpose` |
| **S3** | Semantics | `Session_Health` in client represents backend health, not session health | `services/session_health.odin` vs `delivery/domain/session.go:28` | Client: rename to `Delivery_Health` or `Backend_Health` |

### P2 — Documentation / Clarity

| ID | Type | Description | Evidence | Proposed Action |
|----|------|-------------|----------|-----------------|
| **S4** | Polysemy | `StreamState` (backend anomaly) vs `Stream_State` (client transport) | `marketdata/domain/instrument_stream.go:10` vs `streams/stream_types.odin` | Backend: rename to `StreamAnomalyState`; document in glossary |
| **S5** | Clarity | `Data_Readiness` vs `Trading_Readiness` — close terms, distinct concepts | `widget_readiness.odin:20` vs `trading_readiness.odin:60` | Document distinction in glossary |
| **S6** | Clarity | `State` overloaded in 8+ types across stacks | Multiple domains | Publish qualification table in glossary |

### Deferred

| ID | Type | Description | Priority |
|----|------|-------------|----------|
| **S7** | Glossary | Create canonical glossary ADR formalizing all 12 term definitions | P2 |
| **S8** | Rules | Add naming rules N1-N7 to CLAUDE.md | P2 |

---

## Appendix A — Backend Package Dependency Matrix

| Module | Requires | Cross-domain? |
|--------|----------|---------------|
| `internal/shared` | (external only) | No |
| `internal/core/marketdata` | shared, marketmodel | No |
| `internal/core/aggregation` | shared, marketdata | No (input dependency) |
| `internal/core/delivery` | shared | No |
| `internal/core/insights` | shared | No |
| `internal/core/evidence` | shared | No |
| `internal/core/signals` | shared, evidence | Yes (declared, acyclic) |
| `internal/core/marketmodel` | shared | No |
| `internal/core/workspace` | shared | No |
| `internal/core/execution` | shared | No |
| `internal/core/portfolio` | shared | No |
| `internal/core/strategy` | shared | No |
| `internal/core/signal` | shared | No |
| `internal/adapters` | shared, all core ports | No (implements ports) |
| `internal/actors` | shared, adapters, all core | No (orchestration) |

## Appendix B — Client Layer Import Matrix

| Layer | Imports | Never Imports |
|-------|---------|---------------|
| `ports/` | `mr:ui` | services, layers, app, md_common |
| `services/` | `core:*`, `mr:ports`, `mr:util` | layers, app, md_common |
| `md_common/` | `mr:ports`, `mr:services`, `mr:util` | layers, app |
| `layers/` | `mr:ports`, `mr:services`, `mr:ui` | app, md_common |
| `app/` | `mr:layers`, `mr:md_common`, `mr:ports`, `mr:services`, `mr:streams`, `mr:ui`, `mr:util`, `mr:widgets` | (top layer) |

## Appendix C — State Pipeline Diagram

```
┌─────────────────────────────────────────────────────────────┐
│ md_common/stream_apply_state.odin                          │
│                                                             │
│  Stream_Apply_State (54 fields)                            │
│  ├── snapshot gates [7 artifact kinds]                      │
│  ├── live data flags [7 artifact kinds]                     │
│  ├── Composition_Stage (5 variants)                         │
│  ├── Recovery_Status (3 variants)                           │
│  ├── System_Health_Level (4 variants)                       │
│  └── Stream_Reliability (7 variants)                        │
└──────────────────────────┬──────────────────────────────────┘
                           │ pure derivation
                           ▼
┌─────────────────────────────────────────────────────────────┐
│ app/stream_slots.odin                                       │
│                                                             │
│  Cell_Surface_View (10 fields, ceiling enforced)            │
│  ├── composition: Composition_Stage                         │
│  ├── has_live_data: bool                                    │
│  ├── artifact_has_live: [Artifact_Kind]bool                 │
│  ├── venue, symbol: string                                  │
│  ├── stream_bound: bool                                     │
│  ├── health_level: System_Health_Level                      │
│  ├── recovery_attempts: u8                                  │
│  ├── reliability: Stream_Reliability                        │
│  └── backfill_expectation: Backfill_Expectation             │
└──────────────────────────┬──────────────────────────────────┘
                           │ policy-driven
                           ▼
┌─────────────────────────────────────────────────────────────┐
│ app/widget_readiness.odin                                   │
│                                                             │
│  Data_Readiness (6 variants, ceiling enforced)              │
│  Not_Ready → Loading → Snapshot_Pending → Seeding           │
│  → Partial_Usable → Live_Usable                             │
└──────────────────────────┬──────────────────────────────────┘
                           │ visual mapping
                           ▼
┌─────────────────────────────────────────────────────────────┐
│ app/shell_common.odin                                       │
│                                                             │
│  Pane_Visual_State (8 variants)                             │
│  Active | Loading | Seeding | Snapshot_Pending              │
│  | Empty | Offline | Error | Degraded                       │
└─────────────────────────────────────────────────────────────┘
```

## Appendix D — Health Pipeline Diagram (ADR-0034)

```
Layer 1: TRANSPORT (stream_controller.odin)
  │ Stream_State: Offline / Live / Lag / Desync
  │ Thresholds: lag_warn=4s, desync_stale=12s, clock_drift=8s
  ▼
Layer 2: DELIVERY (stream_apply_state.odin)
  │ Per-artifact: snapshot_seen, has_live, last_recv
  │ Derives: Composition_Stage, Artifact_Staleness
  ▼
Layer 3: SNAPSHOT (stream_apply_state.odin)
  │ Snapshot_Lifecycle: Absent → Pending → Degraded → Stale → Live
  ▼
Layer 4: HEALTH + RECOVERY (stream_apply_state.odin + health.odin)
  │ System_Health_Level: Healthy → Critical
  │ Recovery_Status: None / Recovering / Exhausted
  │ Recovery: 3 attempts, 15s→30s→60s exponential backoff
  ▼
Layer 5: RELIABILITY (stream_apply_state.odin)
  │ Stream_Reliability: 7-state canonical trust gate
  │ Render blocks: Stale_Unrecoverable, Desync, Offline, Manual_Resync
  │ Render allows: Reliable, Degraded_Aging, Stale_Recovering
  ▼
  Consumer: widget_data_readiness → Pane_Visual_State → render
```
