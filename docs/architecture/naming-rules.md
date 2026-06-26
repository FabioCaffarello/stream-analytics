# Naming Rules — Canonical Ubiquitous Language

> **Normative.** All code, docs, and ADRs MUST align to these definitions.
> Authority: ADR-0023, ADR-0032–0035, semantic-boundary-audit-2026-03-10, week-2-canonical-consolidation.

---

## Terms

### signal vs signals

**Definition:** `signal` = atomic detection output from evidence evaluation. `signals` (composition) = derived output combining multiple atomic signals via regime/cross-venue rules (`CompositeSignalV1`). Both non-execution, non-order.

**When to use:** `signal` for the detection engine and its outputs (`SignalEvent`, `Signal_Store`). `composite signal` for composition engine outputs only.

**When NOT to use:** As synonym for "notification", "message", or "indicator". Never use `signal` for composed outputs or vice versa.

**Consistency:** `internal/core/signal/` (detection) vs `internal/core/signals/` (composition) differ only by plural — violates N1. Rename to `detection/` + `composition/` tracked as **P1-S1**.

---

### event

**Definition:** Immutable, append-only fact with canonical envelope (type, version, venue, instrument, ts_exchange, ts_ingest, seq, idempotency_key, payload). Never retroactively modified; only superseded.

**When to use:** For any domain fact flowing through the event pipeline. Always prefix-qualify: `MarketEvent`, `ExecutionEventV1`, `Recovery_Event`.

**When NOT to use:** For user actions (use `action` in client), transient state transitions, or mutable data.

**Consistency:** Clean. Context-qualified consistently across both stacks.

---

### intent

**Definition:** Explicit trade-directive proposal from strategy to executor. Contains IntentID, Side, Sizing, Constraints, Provenance with ParentSignalIDs.

**When to use:** Exclusively for `StrategyIntentV1` and related backend types.

**When NOT to use:** For UI purposes, pane roles, or lifecycle phases in client code. Client MUST use `role`, `purpose`, or `phase` instead.

**Consistency:** Client `Orchestrator_Intent` renamed to `Orchestrator_Phase` — **P1-S2 RESOLVED** (S159).

---

### state

**Definition:** Deterministic aggregate guarded by invariants. Bare `State` is **prohibited** — MUST always be prefix-qualified.

**When to use:** For domain-owned mutable aggregates: `ControlState`, `PortfolioStateV1`, `Stream_Apply_State`, `Chart_Pan_State`.

**When NOT to use:** Without a domain prefix. Never as generic variable name.

**Consistency:** 8+ `State` types across stacks. Prefix qualification resolves code-level ambiguity. Backend `StreamState` (anomaly) vs client `Stream_State` (transport) = same name, different semantics — tracked as **P2-S4**. Full qualification table in Appendix A.

---

### summary

**Definition:** Rolled-up aggregation across multiple entities or time windows. Lightweight read model. **Never source of truth.**

**When to use:** For aggregated metrics derived from events or snapshots: `PortfolioSummaryV1`, `Session_Health_Summary`.

**When NOT to use:** Interchangeably with `snapshot`. Summary = aggregated metrics. Snapshot = complete state capture.

**Consistency:** Clean. No cross-stack conflicts.

---

### snapshot

**Definition:** Immutable point-in-time capture of **complete** state. Snapshot gates track validity and freshness via `Snapshot_Lifecycle` (Absent → Pending → Degraded → Stale → Live).

**When to use:** For full state captures: `AccountSnapshotV1`, `BookSnapshot`, `Runtime_Snapshot`.

**When NOT to use:** Interchangeably with `summary`. Snapshot = complete capture. Summary = aggregated view.

**Consistency:** Clean. Both stacks use consistently.

---

### session

**Definition:** Single connected client WS lifecycle with subscription management and backpressure policy. Owned by the delivery layer.

**When to use:** For delivery-layer WS connection scope (`SessionActor`, `SessionID`) or session-scoped analytics (`SessionVolumeProfileV1`).

**When NOT to use:** For backend health queries displayed on the client dashboard. That concept is **delivery health**, not session health.

**Consistency:** Client `Session_Health_Result` queries delivery-layer health, not session health. Rename to `Delivery_Health` tracked as **P1-S3**.

---

### readiness

**Definition:** Operational gate answering "is X ready to proceed?" Two distinct, namespaced concepts:

| Scope | Type | Question |
|-------|------|----------|
| Backend | `TradingReadinessV1` | Is it safe to execute trades? |
| Client | `Data_Readiness` | Does this widget have enough data to render? |

**When to use:** For binary precondition assessment. Always namespace-qualify.

**When NOT to use:** Interchangeably with `health`. Readiness = binary gate. Health = graduated monitoring level.

**Consistency:** Resolved via qualification. `Data_Readiness` (client rendering) and `Trading_Readiness` (backend safety) are distinct, valid concepts.

---

### health

**Definition:** Observable monitoring layer based on counters and thresholds. Non-deterministic (depends on wall clock). Reports graduated degradation levels.

**When to use:** For staleness, freshness, or operational degradation assessment: `System_Health_Level`, `Candle_Health`, `HealthState`.

**When NOT to use:** As synonym for `readiness`. Health = monitoring. Readiness = safety gate.

**Consistency:** Clean. Client health pipeline (ADR-0034): Transport → Delivery → Snapshot → Health → Reliability.

---

### workspace

**Definition:** First-class aggregate root for the dashboard domain. Owns layout tree, pane registry, data context, persistence envelope. Schema-versioned (`WORKSPACE_SCHEMA_VERSION`).

**When to use:** For the dashboard aggregate in either stack.

**When NOT to use:** As synonym for "layout" (layout is a child of workspace) or "session" (session is delivery-layer).

**Consistency:** Clean. Aligned cross-stack. Backend validates and persists; client owns runtime state.

---

### execution

**Definition:** Bounded context owning the lifecycle of intent execution. Immutable event record. Status machine: unspecified → accepted → placed → partially_filled → filled/canceled/expired/failed. Governance-gated via `ControlState`.

**When to use:** For the execution control plane, governed executor, simulation engine, or execution events.

**When NOT to use:** In client code as a first-class concept. Client reflects execution state via `Trading_Readiness` only.

**Consistency:** Clean. Backend-only domain, correctly bridged to client.

---

### portfolio

**Definition:** Deterministic projected state derived from execution events. Read-model projection — **not source of truth** for actual exchange balances. Scoped: global / account / venue_account.

**When to use:** For projected positions, balances, equity, PnL, or risk aggregations.

**When NOT to use:** As source of truth for exchange balances. Portfolio is always a projection.

**Consistency:** Clean. Cross-stack alignment confirmed. Backend projects; client displays.

---

### artifact

**Definition:** Publishable data product from the aggregation or insights pipeline. Classified into 4 tiers:

| Tier | Owner | Lifespan |
|------|-------|----------|
| T0 Raw | marketdata | Per-event |
| T1 Aggregates | aggregation | Per-window |
| T2 Derived | insights | Session-scoped |
| T3 Evidence | evidence | Stateful detection |

**When to use:** For any data product flowing through delivery: `Artifact_Kind` (16 variants), `Artifact_Staleness`.

**When NOT to use:** As synonym for "type" or "kind". Artifact is the publishable unit; kind is its classification.

**Consistency:** Clean. No cross-stack conflicts.

---

### insight

**Definition:** Conversion of market structure into actionable clarity **without execution directives**. Evidence-based informational output. Must expose what, why, and invalidation conditions.

**When to use:** For VPVR, heatmaps, TPO profiles, cross-venue fusion, or any informational product from the insights layer.

**When NOT to use:** For anything implying execution. Insights NEVER issue buy/sell/entry/stop directives (ADR-0008).

**Consistency:** Clean. Term not used in client — insights surface via the artifact pipeline.

---

## Cross-Cutting Naming Rules

| # | Rule | Rationale |
|---|------|-----------|
| **N1** | Packages with distinct responsibilities MUST have distinct names — never differ only by singular/plural | `signal` vs `signals` confusion |
| **N2** | Backend domain terms MUST NOT be reused with different semantics in the client | `intent` divergence |
| **N3** | `State` MUST always be prefix-qualified in type names | 8+ overloaded usages |
| **N4** | `Health` = observable monitoring; `Readiness` = operational safety gate — never interchange | Semantic precision |
| **N5** | `Summary` = rolled-up aggregation; `Snapshot` = complete point-in-time capture — never interchange | Semantic precision |
| **N6** | `Event` = immutable append-only fact; `Action` = user input in client — never interchange | Domain boundary |
| **N7** | Cross-stack terms must have identical semantics or different names | Ubiquitous language |

---

## Active Inconsistency Registry

| Priority | ID | Conflict | Proposed Fix |
|----------|----|----------|--------------|
| P1 | S1 | `signal/` vs `signals/` differ only by plural | Rename to `detection/` + `composition/` |
| ~~P1~~ | S2 | ~~Client `Orchestrator_Intent` ≠ backend trade-directive `intent`~~ | **RESOLVED** (S159): renamed to `Orchestrator_Phase` |
| P1 | S3 | Client `Session_Health` queries delivery health, not session health | Rename to `Delivery_Health` |
| P2 | S4 | Backend `StreamState` (anomaly) vs Client `Stream_State` (transport) | Backend: rename to `StreamAnomalyState` |
| P2 | S5 | `Data_Readiness` vs `Trading_Readiness` proximity | Maintain namespace qualification; document here |
| P2 | S6 | `State` overloaded 8+ ways | Publish qualification table (Appendix A) |

---

## Appendix A — State Qualification Table

| Qualified Name | Domain | Meaning |
|----------------|--------|---------|
| `StreamState` (backend) | marketdata | Anomaly classification (HEALTHY / NEEDS_ATTENTION) |
| `Stream_State` (client) | streams | Transport lifecycle (Connected / Running / Desync / Backoff) |
| `ControlState` | execution | Governance (Active / Paused / Drained / Halted) |
| `PortfolioStateV1` | portfolio | Account projection (balances, positions, exposures) |
| `HealthState` | aggregation | Book consistency (Healthy / NeedsResync) |
| `Composition_Stage` | md_common | Data assembly (Empty → Composed) |
| `System_Health_Level` | md_common | Artifact staleness (Healthy → Critical) |
| `Stream_Reliability` | md_common | Combined trust gate (7 states, ADR-0032) |
| `Stream_Apply_State` | md_common | Per-stream composite (54 fields) |
| `App_State` | app | Root application aggregate |
