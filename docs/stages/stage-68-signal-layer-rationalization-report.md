# Stage 68 — Signal Layer Rationalization

**Date:** 2026-03-08
**Status:** COMPLETE
**Predecessors:** S65 (capability audit), S66 (evidence namespace alignment), S67 (insights materialization)

## Objective

Rationalize the signal layer semantically and structurally — clarify boundaries with evidence, insights, and strategy without breaking the existing pipeline.

## Conceptual Model

### Two-Tier Signal Pipeline

```
Evidence (BC: evidence)
    │
    ├──→ [signal/] Tier 1: Detection Engine
    │        Input:  EvidenceEvent
    │        Rules:  RegimeChange, LiquidityCollapse, PersistentImbalance, VenueDivergence
    │        Output: marketmodel.SignalEvent via SignalEmitter
    │        State:  SignalStateStore (ring buffers, TTL, dedup, tenant rate limit)
    │
    ├──→ [signals/] Tier 2: Composition Layer
    │        Input:  EvidenceEvent + RegimeSignal (optional)
    │        Rules:  ConfidenceThreshold, RegimeBoost, CrossVenueConfirmation
    │        Output: CompositeSignalV1 via SignalPublisher
    │        State:  SignalComposer (correlation window) + SignalRateLimiter (dedup + global rate)
    │
    └──→ [Orchestrator (actor layer)] merges outputs
              │
              ▼
         [strategy/] IntentPlanner
              Input:  IntentInput { SignalID, CorrelationID, Kind, Confidence, ... }
              Output: StrategyIntentV1 with IntentProvenance.ParentSignalIDs
```

### Key Definitions

| Concept | Definition | Owner BC | Module |
|---------|-----------|----------|--------|
| **Signal detection** | Deterministic rule evaluation over windowed evidence history. Pure function: same input → same output. | `signal` | `internal/core/signal/` |
| **Signal composition** | Enrichment of evidence with regime context and cross-venue correlation. Boosts confidence, applies thresholds. | `signals` | `internal/core/signals/` |
| **Signal event/stream** | `marketmodel.SignalEvent` (Tier 1 wire contract) or `CompositeSignalV1` (Tier 2 domain contract). Both carry `SignalID` for lineage. | `signal` / `signals` | respective modules |
| **Strategy intent handoff** | Mapping from signal outputs to `IntentInput`. Orchestrator responsibility — signal modules never issue buy/sell. | `strategy` | `internal/core/strategy/` |
| **Evidence** | Deterministic microstructure observations. Input to both signal tiers. Never issues directives (ADR-0008). | `evidence` | `internal/core/evidence/` |
| **Insights** | Market understanding (volume profiles, heatmaps, TPO). Orthogonal to signal pipeline — no direct interaction. | `insights` | `internal/core/insights/` |

### Parallel vs Serial

Tier 1 and Tier 2 are **parallel evaluation paths** — both consume `EvidenceEvent` directly. This is intentional:

- Tier 1 detects compound anomalies (e.g., thinning + spread explosion = liquidity collapse)
- Tier 2 enriches individual evidence with regime/cross-venue context

A downstream orchestrator merges outputs before strategy handoff.

## Ambiguities Identified & Resolved

### 1. Missing Strategy Handoff Identity (RESOLVED)

**Problem:** `CompositeSignalV1` lacked `SignalID` and `CorrelationID`, making strategy handoff require synthetic ID generation at the orchestrator layer.

**Resolution:** Added `SignalID` (prefixed `csig_`) and `CorrelationID` to `CompositeSignalV1`. Computed deterministically in `SignalComposer.Compose()` using FNV-1a hash. Strategy can now use `CompositeSignalV1.SignalID` directly for `IntentInput.SignalID`.

### 2. No Event Catalog for Signal Layer (RESOLVED)

**Problem:** Insights had a governed event catalog (S67) but signal/signals did not.

**Resolution:** Created `signal/event_catalog.go` (4 detection event contracts) and `signals/domain/event_catalog.go` (1 composition event contract). Both follow the same `EventContract` pattern as insights.

### 3. Feature Type Divergence (DOCUMENTED)

**Problem:** `marketmodel.SignalFeature{Key string, Value float64}` vs `signals/domain.SignalFeature{Label string, Value string}`.

**Resolution:** Documented as intentional in `signals/domain/event_catalog.go`. Composite signals carry pre-formatted string features for transport safety. Strategy accesses confidence/severity directly, not through features.

### 4. Port Naming Inconsistency (DOCUMENTED)

**Problem:** `SignalEmitter` (signal/) vs `SignalPublisher` (signals/).

**Resolution:** Documented in handoff contract. Both are transport-neutral output ports. Naming reflects semantic difference: "emitter" for low-level emission, "publisher" for composed publication.

### 5. Flat vs Structured Layout (ACCEPTED)

**Problem:** `signal/` is flat, `signals/` has `app/domain/ports/`.

**Resolution:** Accepted as-is. `signal/` is a focused engine with no app/port separation needed — all files are implementation. Restructuring would be disruptive with no functional benefit.

## Changes

### New Files

| File | Purpose |
|------|---------|
| `signal/event_catalog.go` | Detection event catalog (4 entries) + package-level conceptual model doc |
| `signal/event_catalog_test.go` | 3 tests: required fields, no duplicates, by-type lookup |
| `signal/handoff.go` | Strategy handoff contract documentation + field mapping reference |
| `signals/domain/event_catalog.go` | Composition event catalog (1 entry) + package-level conceptual model doc |
| `signals/domain/event_catalog_test.go` | 3 tests: required fields, no duplicates, by-type lookup |

### Modified Files

| File | Change |
|------|--------|
| `signals/domain/composite_signal.go` | Added `SignalID` and `CorrelationID` fields (omitempty, backward compatible) |
| `signals/app/compose_signal.go` | Added `compositeSignalID()` and `compositeCorrelationID()` (FNV-1a, deterministic) |
| `signals/app/compose_signal_test.go` | Added 2 tests: ID presence + determinism |

## Test Results

- `signal/` — 15 tests PASS (3 new + 12 existing)
- `signals/app` — 7 tests PASS (2 new + 5 existing)
- `signals/domain` — 4 tests PASS (3 new + 1 existing)
- **0 regressions, 0 wire-breaking changes**

## Boundary Verification

| Boundary | Status |
|----------|--------|
| Evidence → Signal: no signal logic leaked into evidence | ✅ Clean |
| Signal → Strategy: no strategy logic in signal/signals | ✅ Clean |
| Insights orthogonal: no signal↔insights coupling | ✅ Clean |
| No wire-breaking changes | ✅ Verified (omitempty on new fields) |
| No duplicated evidence/insights logic | ✅ Verified |
| Deterministic replay safety preserved | ✅ Verified (ID hashing is deterministic) |

## Architecture Decision: Why Parallel Tiers

The two-tier parallel design was preserved because:

1. **Different detection granularity** — Tier 1 combines multiple evidence events (windowed rules), Tier 2 enriches single events with regime/cross-venue context
2. **Independent evolution** — detection rules and composition rules change at different rates
3. **Bounded blast radius** — a bug in composition doesn't affect detection emissions
4. **Strategy flexibility** — orchestrator can choose to consume Tier 1 only, Tier 2 only, or both
