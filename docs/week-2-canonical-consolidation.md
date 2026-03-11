# Market Raccoon — Week 2 Canonical Consolidation

**Date:** 2026-03-10
**Branch:** codex/s9-legacy-removal-cutover
**Basis:** 7 audits, 36 ADRs, 5 PRDs, 158 stage reports, 161K LOC
**Score:** 8.4/10 architectural maturity

---

## Executive Summary

Market Raccoon exits Week 2 as a **production-grade, multi-exchange market data platform** with 131K Go + 30K Odin LOC, 2,983 tests, 6 exchanges, and validated throughput of 117,697 evt/sec.

**What closed:** Product definition canonical. 12 bounded contexts mapped. Dependency DAG acyclic. Client guard rails holding. Orderflow vertical slice delivered (S157). All 5 critical flows audited end-to-end.

**What remains open:** 1 structural inversion (shared/contracts), 1 god object (App_State), 1 dual-path debt (Entity_World), documentation governance broken (AUTHORITY-MAP blind to 60% of PRDs), 6 naming inconsistencies tracked.

**Week 3-4 mandate:** Fix documentation governance, remove Entity_World, plan contracts extraction.

---

## 1. Canonical Product Vision

**One-line:** Real-time multi-exchange cryptocurrency market data platform with integrated operational cockpit — NOT a trading platform.

**Core pipelines (7):** Ingestion → Aggregation → Insights → Delivery → Health/Reliability → Execution (fail-closed, governance-gated) → Actor Supervision.

**Quantitative identity:**

| Metric | Value |
|--------|-------|
| Backend LOC | 131K Go |
| Client LOC | 30K Odin |
| Go modules | 26 |
| Bounded contexts | 12 |
| Actor subsystems | 10 |
| Exchanges | 6 (Binance spot+futures, Bybit, Coinbase, HyperLiquid, Kraken spot+futures) |
| Backend tests | 1,666 |
| Client tests | 1,317 |
| ADRs | 36 (0000–0035) |
| Stage reports | 158 |
| Throughput | 117,697 evt/sec (p50=7us, p95=13us, p99=56us) |
| Widgets | 13 kinds |
| Indicators | 8 + 3 subplot analytics |
| Keyboard shortcuts | 20+ |

**What the product is NOT:** Trading bot, order router, backtester, social platform, mobile app. Execution defaults to `bootstrap_simulated`; live mode requires explicit governance gate.

---

## 2. Canonical Architecture

### 2.1 Backend — Hexagonal + DDD + Actor Model

```
cmd/* → interfaces/ → actors/ → adapters/ → core/* → shared/
```

**Patterns:** Hexagonal per BC, DDD aggregates, Hollywood v1.0.5 actors, NATS JetStream event bus, versioned envelopes.

**Storage:** TimescaleDB (hot, 7d) + ClickHouse (cold, historical) with cost-based federation.

**Supervision:** Guardian tree, 10 subsystems, backoff 250ms–5s, 5/30s limit, 30s cooldown.

### 2.2 Client — Strict 5-Layer DAG (Odin/WASM)

```
app/ → layers/ → services/ → md_common/ → ports/
```

**State pipeline:** Stream_Apply_State → Cell_Surface_View (≤10 fields) → Data_Readiness (≤6 variants) → Pane_Visual_State (8 fields).

**Health pipeline (ADR-0034):** Transport → Delivery → Snapshot → Health (4 levels) → Reliability (7 states, ADR-0032).

### 2.3 Invariants (enforced, `make invariants-check`)

| ID | Rule |
|----|------|
| INV-LAY-01–06 | Layer isolation (services↛layers↛app, strategies stateless, Layer_Context read-only) |
| INV-DOM-01 | Core + actors protobuf-free |
| INV-DET-01 | No `time.Now()` in core |
| INV-BUS-01 | Subject taxonomy valid |
| INV-ACK-01 | JetStream ACK/NAK/TERM |
| INV-TOPO-01 | Guardian readiness enforcement |
| INV-MEX-01 | Stream identity = venue + instrument + market_type |

---

## 3. Capability Map (Consolidated)

### 3.1 Bounded Contexts

**Backend (10):**

| BC | Responsibility | Key Contracts |
|----|---------------|---------------|
| MarketData | WS ingest, normalize, dedup, seq | TradeTickV1, BookDeltaV1, OISnapshotV1 |
| Aggregation | Candles, stats, orderbook, tape, OI, CVD | CandleV1, StatsV1, OrderBookSnapshotV1, TapeWindowV1 |
| Delivery | Pub/sub routing, backpressure, backfill | DeliveryRing, TranscodeCache (16 shards) |
| Insights | VPVR, heatmap, TPO, cross-venue | VolumeProfile, Heatmap, FootprintCandleV1(P4) |
| Evidence | LEL rules (5 types), deterministic | LELEvidenceV1 |
| Signal | Detection engine, dedup, rate limit | SignalEvent |
| Strategy | Intent planner | StrategyIntentV1 |
| Execution | Governance, simulation, control plane (4 states, 10 cmds) | ExecutionEventV1 |
| Portfolio | Position/balance projection | PortfolioStateV1 |
| Storage | TimescaleDB + ClickHouse adapters | Goose migrations, cold reader API |

**Client (2):**

| BC | Responsibility |
|----|---------------|
| Client Runtime | State pipeline, transport, health, 14 stores |
| Client App | Workspace, widgets, interaction, 50+ actions |

### 3.2 Orderflow (Cross-Cutting, NOT a BC — ADR-0033/0035)

| Tier | Owner | Artifacts |
|------|-------|-----------|
| T0 Raw | MarketData | TradeTickV1, BookDeltaV1 |
| T1 Aggregates | Aggregation | TapeWindow, OrderBookSnapshot, DeltaVolumeWindow |
| T2 Derived | Insights | VolumeProfile, Heatmap, Footprint |
| T3 Evidence | Evidence | 5 LEL rules |

### 3.3 Dependency DAG (acyclic, verified)

```
shared ← marketdata ← aggregation ← delivery
                    ← insights
       ← evidence ← signal ← strategy ← execution → portfolio
                  ← insights
       ← storage (implements ports)
```

Client: `ports → services → layers → app` (zero cyclic deps).

---

## 4. Ubiquitous Language (Official)

### 4.1 Canonical Terms (12)

| Term | Definition | Qualification Rule |
|------|-----------|-------------------|
| **signal** | Deterministic detection output from evidence | Always atomic unless `Composite` prefix |
| **event** | Immutable append-only fact with canonical envelope | Always envelope-wrapped |
| **intent** | Trade directive proposal (strategy → executor) | Backend only; client uses `Phase` |
| **state** | Deterministic aggregate | Always prefix-qualified (`Stream_State`, `Control_State`) |
| **summary** | Rolled-up aggregation (non-authoritative) | Never a source of truth |
| **snapshot** | Complete point-in-time capture with validity gates | Has `Lifecycle` (Absent→Live) |
| **session** | Single connected client lifecycle | Backend = WS connection scope |
| **readiness** | Operational safety gate (binary pass/fail) | `Trading_Readiness` = backend; `Data_Readiness` = client |
| **health** | Observable monitoring metric (non-deterministic) | `Candle_Health` (4 levels), `System_Health_Level` |
| **workspace** | First-class aggregate root for dashboard domain | Aligned cross-stack |
| **artifact** | Data product from aggregation/insights | `Artifact_Kind` enum (16 variants) |
| **reliability** | Composite stream operational quality | `Stream_Reliability` (7 states, ADR-0032) |

### 4.2 Active Inconsistencies (6, tracked)

| ID | Severity | Issue | Resolution |
|----|----------|-------|------------|
| S1 | P1 | `signal/` vs `signals/` package naming | Rename to `detection/` + `composition/` |
| S2 | ~~P1~~ | ~~Client `Orchestrator_Intent` ≠ backend intent~~ | **RESOLVED** (S159): renamed to `Orchestrator_Phase` |
| S3 | P1 | `Session_Health` queries delivery, not session | Rename to `Delivery_Health` |
| S4 | P2 | Backend `StreamState` vs client `Stream_State` | Backend rename to `StreamAnomalyState` |
| S5 | P2 | `Data_Readiness` / `Trading_Readiness` proximity | Document distinction in glossary |
| S6 | P2 | `State` overloaded 8+ ways | Publish qualification table |

---

## 5. Boundary Rules (Official)

### 5.1 Backend Layer Rules

| Layer | May Import | Must Not Import |
|-------|-----------|----------------|
| shared/ | stdlib only | Any internal/* |
| core/* | shared/ only; cross-BC requires ADR | actors, adapters, interfaces, time.Now(), fmt.Sprintf(hot) |
| adapters/ | core/*/ports, shared/ | actors, interfaces, own domain types |
| actors/ | core/app+domain, adapters, shared | interfaces, domain logic, direct DB |
| interfaces/ | actors, core/domain, shared | adapters, business logic |

### 5.2 Client Layer Rules

| Layer | May Import | Must Not Import |
|-------|-----------|----------------|
| ports/ | (leaf) | services, layers, app |
| services/ | ports, md_common | layers, app |
| md_common/ | ports, services | layers, app |
| layers/ | services, md_common, ports | app |
| app/ | all below | (root) |

### 5.3 Naming Rules (N1–N7)

- **N1:** Distinct names for distinct responsibilities (no singular/plural collision)
- **N2:** Backend terms not reused with different semantics in client
- **N3:** `State` always prefix-qualified
- **N4:** `Health` = monitoring; `Readiness` = safety gate
- **N5:** `Summary` = aggregation; `Snapshot` = complete capture
- **N6:** `Event` = immutable fact; `Action` = user input
- **N7:** Cross-stack terms have identical semantics or different names

---

## 6. Authority Map (Documental)

### 6.1 Hierarchy (4 tiers)

| Tier | Purpose | Count | Examples |
|------|---------|-------|---------|
| **T1 Canonical** | Governs code decisions | ~60 | 36 ADRs, 5 PRDs, 7 wire contracts, 8 arch docs, 3 client arch docs |
| **T2 Operational** | Day-to-day guides | ~20 | Dev setup, testing, templates, runbooks |
| **T3 Evolutionary** | Active proposals | 4 | RFC-0012 through RFC-0015 |
| **T4 Historical** | Record only, never governs | ~190 | 158 stage reports, retired audits/RFCs |

### 6.2 Conflict Resolution

- PRD wins on **scope** (what must be true)
- ADR wins on **mechanism** (how it works)
- Stage reports **never** govern
- Audits are snapshots, not authority

### 6.3 Known Governance Defects (CRITICAL)

| Defect | Impact |
|--------|--------|
| AUTHORITY-MAP blind to PRD-0003/0004/0006 | Decisions ignore 60% of PRDs |
| TRUTH-MAP covers ADRs 0000–0023 only | Half of architecture has no truth chain |
| `client-roadmap-6.8-to-8.0.md` describes retired world | New readers get wrong picture |
| `.context/` references in AUTHORITY-MAP | Points to deleted files |
| RFC W-series (0001–0010) in active space | Risk of consulting outdated plans |

---

## 7. Decisions Closed (Week 2)

| Decision | ADR/Stage | Status |
|----------|-----------|--------|
| Stream Reliability = 7-state enum | ADR-0032 | Accepted, implemented |
| Orderflow = cross-cutting (4 tiers), NOT separate BC | ADR-0033 | Accepted, implemented |
| Health pipeline = 5 layers, ownership invariants | ADR-0034 | Accepted, implemented |
| Orderflow contract = 15 capabilities, widget taxonomy | ADR-0035 | Accepted, implemented |
| Guard rails: 10-field CSV, 6-variant DR, pure derivation | S158 | Validated, holding |
| Product definition: NOT a trading platform | product-definition.md | Canonical |
| Boundary rules: 5 backend + 4 client layers | boundary-rules.md | Canonical |
| Naming rules: 7 rules + 12 canonical terms | naming-rules.md | Canonical |

---

## 8. Open Conflicts

### 8.1 Structural (code)

| ID | Severity | Description | Owner |
|----|----------|-------------|-------|
| **P0-1** | CRITICAL | `shared/contracts` imports all core domains (dependency inversion) | Backend |
| **P0-6** | HIGH | Entity_World dual-path rendering (maintenance doubling) | Client |
| **P1-R6** | HIGH | App_State god object (~1000 fields) | Client |
| **P1-R1** | HIGH | NATS single stream for 10 domains (backpressure cascade) | Backend |
| **P1-R3** | MEDIUM | `/healthz` blocks on Guardian snapshot | Backend |

### 8.2 Naming (semantic)

6 active inconsistencies — see Section 4.2.

### 8.3 Documentation (governance)

5 governance defects — see Section 6.3.

---

## 9. Backlog (Derived from Canonical Definition)

### P0 — Blocking (Week 3-4)

| ID | Action | Rationale |
|----|--------|-----------|
| P0-DOC-1 | Fix AUTHORITY-MAP (add PRDs, fix paths, remove .context/) | Governance broken |
| P0-DOC-2 | Expand TRUTH-MAP to ADRs 0024–0035 | Half architecture untraced |
| P0-DOC-3 | Retire `client-roadmap-6.8-to-8.0.md` | Active confusion source |
| P0-DOC-4 | Update PRD-0002→Implemented, PRD-0006→Partially Implemented | Status drift |
| P0-DOC-5 | Archive RFC W-series (0001–0010) | Pollution of active space |
| P0-CODE-1 | Complete Entity_World removal | Dual-path blocks all UI evolution |

### P1 — Hygiene (Month 2)

| ID | Action | Rationale |
|----|--------|-----------|
| P1-CODE-1 | Extract `shared/contracts/` to `internal/contracts/` | Dependency inversion (structural) |
| P1-CODE-2 | Move `shared/proto/gen/` to `internal/contracts/proto/` | Follows contracts extraction |
| P1-CODE-3 | Unify `signal/` + `signals/` | Naming collision (S1) |
| P1-CODE-4 | Fix `/healthz` → unconditional 200; logic → `/readyz` | Restart loop risk |
| P1-CODE-5 | Rename `Session_Health` → `Delivery_Health` | Misnomer (S3) |
| ~~P1-CODE-6~~ | ~~Rename `Orchestrator_Intent` → `Orchestrator_Phase`~~ | **RESOLVED** (S159) |
| P1-CODE-7 | Decompose App_State into subsystem contexts | God object |
| P1-CODE-8 | Split `layer_strategies.odin` (68K, 1,698 lines) | Monolith |

### P2 — Refinement (Month 3+)

| ID | Action | Rationale |
|----|--------|-----------|
| P2-CODE-1 | NATS stream split (3+ streams) | Backpressure isolation |
| P2-CODE-2 | Add DLQ/retry for cross-service NATS | Lost intents |
| P2-CODE-3 | Workspace DB migration with advisory lock | Race condition, no multi-instance |
| P2-CODE-4 | TranscodeCache schema-aware invalidation | Stale serialization |
| P2-CODE-5 | Force explicit execution mode at startup | Silent simulation risk |
| P2-CODE-6 | Backend `StreamState` → `StreamAnomalyState` | Naming (S4) |
| P2-CODE-7 | Publish State qualification table | Naming (S6) |

---

## Artifacts Produced (Week 2)

| Artifact | Path | Tier |
|----------|------|------|
| Product Definition | `docs/product-definition.md` | T1 |
| Architecture README | `docs/architecture/README.md` | T1 |
| Boundary Rules | `docs/architecture/boundary-rules.md` | T1 |
| Naming Rules | `docs/architecture/naming-rules.md` | T1 |
| Capability Map | `docs/audits/capability-map-2026-03-10.md` | T1 |
| Semantic Boundary Audit | `docs/audits/semantic-boundary-audit-2026-03-10.md` | T1 |
| Architectural Audit | `docs/audits/architectural-audit-2026-03-10.md` | T4 |
| Client Architecture Audit | `docs/audits/client-architecture-audit-2026-03-10.md` | T4 |
| Documentation Audit | `docs/audits/documentation-audit-2026-03-10.md` | T4 |
| E2E Critical Flows Audit | `docs/audits/end-to-end-critical-flows-audit-2026-03-10.md` | T4 |
| Week 1 Executive Report | `docs/audits/week-1-executive-report-2026-03-10.md` | T4 |
| ADR-0032 Stream Reliability | `docs/adrs/ADR-0032-stream-reliability-model.md` | T1 |
| ADR-0033 Orderflow Blueprint | `docs/adrs/ADR-0033-orderflow-domain-blueprint.md` | T1 |
| ADR-0034 Health Recovery | `docs/adrs/ADR-0034-stream-health-recovery-completion.md` | T1 |
| ADR-0035 Orderflow Contracts | `docs/adrs/ADR-0035-orderflow-contract-architecture.md` | T1 |
| Stage reports 139–158 | `docs/stages/stage-{139..158}-*-report.md` | T4 |
| **This report** | `docs/week-2-canonical-consolidation.md` | T4 |

---

## Next Steps (Week 3)

1. **Day 1-2:** Execute P0-DOC-1 through P0-DOC-5 (documentation governance fix)
2. **Day 3-5:** Map Entity_World removal scope (P0-CODE-1), identify all dual-path touch points
3. **Day 5-7:** Begin Entity_World removal execution
4. **Parallel:** Plan `shared/contracts` extraction (P1-CODE-1) for Month 2

**Success criteria for Week 3:** AUTHORITY-MAP fixed, TRUTH-MAP complete, Entity_World removal plan with file-level scope, first removal PRs merged.

---

*This report is T4 (historical). It describes the state at 2026-03-10 and does not govern future decisions. Canonical authority remains with the T1 documents listed in Section 6.*
