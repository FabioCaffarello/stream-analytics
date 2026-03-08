# Stage 65 — Backend Capability Audit & Domain Convergence

**Date:** 2026-03-07
**Status:** COMPLETE
**Scope:** Architectural audit of all backend domains, dependency analysis, convergence recommendations

---

## 1. Domain Map — Current State

### 11 Core Domains

| # | Domain | Module | Purpose | Maturity |
|---|--------|--------|---------|----------|
| 1 | **marketdata** | `core/marketdata` | Raw event ingestion, normalization, dedup | Production |
| 2 | **aggregation** | `core/aggregation` | Candles, stats, tape, orderbooks, fusion | Production |
| 3 | **insights** | `core/insights` | Volume profiles, heatmaps, TPO, cross-venue | Production |
| 4 | **delivery** | `core/delivery` | Session management, subscriptions, WebSocket fanout | Production |
| 5 | **evidence** | `core/evidence` | Microstructure observation (spreads, imbalance, sweeps) | Production |
| 6 | **signal** | `core/signal` | Raw signal detection from evidence (single engine) | Production |
| 7 | **signals** | `core/signals` | Composite signal composition (multi-replica) | Production |
| 8 | **strategy** | `core/strategy` | Intent planning from composed signals | Production |
| 9 | **execution** | `core/execution` | Governance + control plane + order submission | Production |
| 10 | **portfolio** | `core/portfolio` | Position/PnL projection from execution events | Production |
| 11 | **marketmodel** | `core/marketmodel` | Canonical types, state store, value objects | Foundation |

### 6 Infrastructure Layers

| Layer | Module | Purpose |
|-------|--------|---------|
| **shared** | `internal/shared` | Foundation: problem, result, validation, ids, clock, envelope, codec, hash, naming |
| **adapters** | `internal/adapters` | Bus (inmemory/jetstream), exchange (6x), storage (timescale+clickhouse), execution (binance) |
| **actors** | `internal/actors` | Hollywood runtime: Guardian → 10 subsystem actors |
| **interfaces** | `internal/interfaces` | HTTP REST API + WebSocket streaming |
| **tools** | `internal/tools` | Auxiliary utilities |
| **storage** | `core/storage` | (go.mod exists, SubsystemStorage defined in Guardian — NOT IMPLEMENTED) |

### 10 Binaries

| Binary | Pipeline Role |
|--------|--------------|
| `consumer` | WebSocket ingestion → event bus |
| `processor` | Event bus → aggregation/orderbook |
| `store` | Cold-path ClickHouse persistence |
| `server` | HTTP API + observability gateway |
| `signals` | Evidence → canonical signal events |
| `strategist` | Signals → strategy intents |
| `executor` | Intents → execution events |
| `portfolio` | Execution events → portfolio state |
| `migrate` | Goose schema migrations |
| `backfill` | Historical data download + gap analysis |

---

## 2. Data Flow — Canonical Pipeline

```
Exchange WebSockets (6 exchanges)
    ↓
[consumer] → marketdata.{trade,bookdelta,liquidation,markprice,open_interest}
    ↓
[processor] → aggregation.{candle,tape,snapshot,stats,crossvenue_book}
    ↓                              ↓
[store] (cold)              [evidence] → insights.{microstructure_evidence,regime_evidence}
                                   ↓
                            [signals] → signal.composite
                                   ↓
                            [strategist] → strategy.intent
                                   ↓
                            [executor] ↔ [control plane] → execution.event
                                   ↓
                            [portfolio] → portfolio.state
                                   ↓
                            [delivery/router] → [session] → Client WebSocket
```

---

## 3. Findings — Domain Health Assessment

### 3.1 CLEAN DOMAINS (no action required)

**marketdata** — Well-bounded. Ingests, normalizes, deduplicates. No downstream knowledge.

**aggregation** — Rich but coherent. Candles, stats, tape, orderbooks, fusion, rollup. All derive from raw events. Dependency fan is wide (see §4.1) but justified: it's the central processing hub.

**delivery** — Clean session/subscription model. Backpressure policies, sequence enforcement, sharded transcode cache. No domain leakage.

**marketmodel** — Pure value objects + canonical types. Foundation dependency for all domains. Appropriately thin.

**strategy** — Minimal and focused. `StrategyIntentV1` + `IntentPlanner`. Clean input (CompositeSignalV1) → clean output (intent). No bloat.

**execution** — Well-structured governance layer. 4-state control plane, 10 commands, GovernedExecutor wrapping adapters. Clean separation of domain (control.go, event.go) from app (governed_executor, control_plane).

**portfolio** — Clean projector. Event-sourced from execution events. BootstrapProjector handles all status transitions deterministically.

### 3.2 DOMAINS REQUIRING ATTENTION

#### 3.2.1 `signal` vs `signals` — Naming Collision (MODERATE CONCERN)

**Current state:**
- `signal` (singular): Raw signal engine. Consumes evidence, evaluates rules (RegimeChange, LiquidityCollapse, PersistentImbalance, VenueDivergence), emits `marketmodel.SignalEvent`. Flat package structure (no domain/app/ports split).
- `signals` (plural): Composite signal composer. Consumes raw signals + regime context, applies 3 composition rules (confidence threshold, regime boost, cross-venue correlation), emits `CompositeSignalV1`. Standard DDD structure.

**Assessment:**
- **Functionally distinct**: `signal` = detection, `signals` = composition. Pipeline ordering is clear.
- **Naming is confusing**: The singular/plural distinction doesn't convey the actual semantic difference. New engineers will conflate them.
- **Structural inconsistency**: `signal` uses flat package layout; `signals` uses domain/app/ports. This is the only core domain without DDD structure.

**Verdict:** The separation is architecturally sound. The naming and structural inconsistency is a documentation/onboarding debt, not a design flaw. See §5 for recommendation.

#### 3.2.2 `insights` vs `evidence` — Scope Overlap (LOW CONCERN)

**Current state:**
- `insights`: Volume profiles, heatmaps, TPO profiles, cross-venue trade snapshots, session management. Builds **analytical artifacts** from aggregated data.
- `evidence`: Microstructure **observations** (spread explosion, thinning, imbalance, absorption, sweep). Builds **evidence events** from raw market events.

**Assessment:**
- **Different inputs**: insights consumes aggregation artifacts; evidence consumes raw trades/books/candles.
- **Different outputs**: insights produces visual/analytical artifacts (profiles, heatmaps); evidence produces typed observation events.
- **Envelope namespacing bleed**: Evidence publishes under `insights.microstructure_evidence` and `insights.regime_evidence` — using the `insights.*` namespace despite being a separate domain.

**Verdict:** Domains are correctly separated. The envelope namespace collision (`insights.*` for evidence events) should be corrected. See §5.

#### 3.2.3 `storage` — Reserved Guardian Slot (NO CONCERN)

**Current state:**
- `SubsystemStorage` is defined in the Guardian's subsystem enum.
- `core/storage` does NOT exist on disk — correctly never materialized.
- All actual storage is in `adapters/storage/` (timescale + clickhouse + federation).

**Assessment:** Storage is correctly an adapter concern (infrastructure), not a domain. The Guardian subsystem slot exists as a reserved placeholder for future hot-path write coordination.

**Verdict:** Clean. No action needed.

---

## 4. Dependency Analysis

### 4.1 Aggregation Dependency Fan — VERIFIED CLEAN

`aggregation/go.mod` direct requires: shared, marketdata, adapters, btree, prometheus.

The strategy, execution, portfolio, signals, evidence, insights, delivery, signal dependencies are all `// indirect` — pulled transitively through `adapters`. Grep confirms aggregation source code does NOT import any of these domains directly.

**Verdict:** No coupling violation. The indirect deps are go.mod noise from transitive resolution. The actual import graph is clean: aggregation → {shared, marketdata, adapters, marketmodel}.

### 4.2 Dependency DAG (Clean)

```
shared ← marketmodel ← marketdata
                     ← evidence
                     ← insights
                     ← delivery
                     ← signal ← evidence
                     ← signals ← evidence
                     ← strategy
                     ← execution ← strategy
                     ← portfolio ← execution
```

No cycles detected. Dependency direction is consistently upstream→downstream. This is healthy.

---

## 5. Convergence Recommendations

### 5.1 RENAME: `signal` → `detection` (RECOMMENDED, DEFERRED)

| Current | Proposed | Rationale |
|---------|----------|-----------|
| `core/signal` | `core/detection` | Clarifies role as raw evidence→signal detection |
| `actors/signal/runtime` | `actors/detection/runtime` | Consistent naming |
| `cmd/signals` | (keep) | Binary name is fine |

**Impact:** Module rename across go.mod, actors, cmd. Non-trivial but mechanical.
**Priority:** LOW — defer until next major refactor window. Document the naming convention now.

### 5.2 FIX: Evidence Envelope Namespace (RECOMMENDED, MINIMAL)

| Current | Proposed | Rationale |
|---------|----------|-----------|
| `insights.microstructure_evidence` | `evidence.microstructure` | Evidence domain owns its envelope namespace |
| `insights.regime_evidence` | `evidence.regime` | Same |

**Impact:** Wire protocol change. Requires client-side envelope type update.
**Priority:** MEDIUM — should align before adding more evidence types. Can be done with config-gated dual-publish for migration.

### 5.3 AUDIT: Aggregation go.mod Dependencies (RECOMMENDED, QUICK)

Verify whether `core/aggregation` actually imports `strategy`, `execution`, `portfolio`. If not, remove from go.mod. If yes, extract the coupling.

**Priority:** HIGH — 30-minute task, prevents future coupling drift.

### 5.4 RESTRUCTURE: `signal` Package Layout (OPTIONAL)

Align `core/signal` to DDD structure (domain/app/ports) matching all other core domains. Currently flat.

**Priority:** LOW — cosmetic consistency. No functional impact.

### 5.5 CLEAN: `core/storage` Ghost Module (OPTIONAL)

If `core/storage` contains no meaningful code, remove it. Storage is correctly an adapter concern.

**Priority:** LOW — avoids confusion for new contributors.

---

## 6. Canonical Domain Registry

### First-Class Domains (Production, Stable API)

| Domain | Canonical Events | Owner Binary |
|--------|-----------------|--------------|
| **marketdata** | `marketdata.{trade,bookdelta,liquidation,markprice,open_interest}` | `consumer` |
| **aggregation** | `aggregation.{candle,tape,snapshot,stats,crossvenue_book}` | `processor` |
| **insights** | `insights.{volume_profile,heatmap,tpo_profile,session_profile}` | `processor` |
| **evidence** | `insights.{microstructure_evidence,regime_evidence}` → proposed: `evidence.{microstructure,regime}` | `processor` |
| **delivery** | (internal: session/subscription management, no external events) | `server` |
| **signal** | `signal.event` (raw detection) | `signals` |
| **signals** | `signal.composite` (composed) | `signals` |
| **strategy** | `strategy.intent` | `strategist` |
| **execution** | `execution.event` | `executor` |
| **portfolio** | `portfolio.state` | `portfolio` |

### Foundation (No Events)

| Module | Role |
|--------|------|
| **shared** | Error handling, codecs, clock, hashing, naming, validation |
| **marketmodel** | Canonical types, value objects, state store |

### Reserved

| Module | Status | Notes |
|--------|--------|-------|
| `SubsystemStorage` (Guardian) | Reserved | Slot exists for future hot-path write coordination (no core/storage on disk) |

---

## 7. Architectural Verdict

### Strengths

1. **Clean bounded contexts**: Each domain has its own go.mod, clear responsibilities, no circular dependencies
2. **Deterministic pipeline**: evidence → signal → signals → strategy → execution → portfolio is a clean DAG
3. **ADR-0008 compliance**: Evidence/signals NEVER issue buy/sell directives — clean separation of observation from action
4. **Governance layer**: Control plane with 4-state lifecycle, allowlist overrides, credential boundary
5. **Hot-path optimization**: FNV-1a hashing, sharded LRU, zero-alloc in critical paths
6. **Multi-replica support**: Signals, strategy, execution, portfolio all support replica-aware ownership filtering
7. **Dual-store architecture**: TimescaleDB (hot) + ClickHouse (cold) with federation layer

### Weaknesses (all minor)

1. **signal/signals naming**: Confusing singular/plural distinction for functionally different domains
2. **Evidence envelope namespace**: Uses `insights.*` instead of `evidence.*`
3. **signal package structure**: Only core domain without DDD layout

### Overall Score: **4.8/5**

The backend is architecturally sound with no dangerous overlaps or structural debt. The five issues identified are all cosmetic/naming concerns, not design flaws. The domain boundaries are well-drawn, the pipeline is deterministic, and the dependency direction is consistently clean.

---

## 8. Minimum Structural Adjustments (Priority Order)

| # | Action | Effort | Impact | Priority |
|---|--------|--------|--------|----------|
| 1 | ~~Audit aggregation go.mod imports~~ | — | **VERIFIED CLEAN** (indirect only) | ~~HIGH~~ DONE |
| 2 | Fix evidence envelope namespace | 2-4 hrs | Wire protocol alignment | **MEDIUM** |
| 3 | Document signal vs signals convention | 30 min | Onboarding clarity | **MEDIUM** |
| 4 | ~~Remove core/storage ghost module~~ | — | **Already clean** (never existed) | ~~LOW~~ DONE |
| 5 | Rename signal→detection | 4-8 hrs | Naming convergence | **LOW (defer)** |
| 6 | Restructure signal to DDD layout | 2-4 hrs | Structural consistency | **LOW (defer)** |

---

## 9. Success Criteria Assessment

| Criterion | Status |
|-----------|--------|
| Domains clearly defined | ✅ 11 domains, all with distinct responsibilities |
| No dangerous overlap | ✅ signal/signals overlap is naming, not functional |
| Events canonical | ✅ with one namespace fix needed (evidence) |
| Dependencies acyclic | ✅ clean DAG verified |
| First-class vs transitional identified | ✅ see §6 |

**Stage 65: COMPLETE**
