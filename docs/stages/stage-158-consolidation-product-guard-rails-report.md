# Stage 158 — Consolidation & Product Guard Rails

**Date:** 2026-03-10
**Type:** Consolidation / Audit
**Scope:** S151–S157 full-stack review
**Baseline:** 1,317 tests (512 md_common + 472 app + 246 services + 57 layers + 16 streams + 14 util), all green

---

## Objective

Consolidate everything that changed between S151–S157 before opening new product surfaces. Verify architecture integrity, remove residual legacy, document guard rails.

---

## Audit Summary

| Domain | Files Surveyed | Status | Finding |
|--------|---------------|--------|---------|
| Stream Health Model | 7 files | **Clean** | 5-layer pipeline (transport→delivery→snapshot→health→reliability), pure functions, no redundancy |
| Snapshot/Backfill Semantics | 6 files | **Clean** | 3 orthogonal concerns (backfill, composition, snapshot), single source of truth in Stream_Apply_State |
| State Model | 8 files | **Clean** | S154 removals verified — 6 dead fields gone, Cell_Surface_View at 10 fields |
| Workspace Persistence | 5 files | **Clean** | WORKSPACE_SCHEMA_VERSION=12, RUNTIME_SNAPSHOT_VERSION=3, CRC integrity |
| Orderflow Domain | 12 files | **Clean** | Full vertical slice wired end-to-end, 69 dedicated tests, zero dead code |
| Layer Boundaries | 11 files | **Clean** | ports→services→layers→app, zero cyclic dependencies, zero cross-boundary violations |
| ADRs 0032–0035 | 4 ADRs | **Consistent** | No contradictions between ADRs or between ADR and implementation |
| Stage Reports S151–S157 | 6 reports | **Aligned** | All deferred items documented with priority and trigger conditions |

---

## Architecture State

### Health Pipeline (ADR-0032 + ADR-0034)

```
Transport (Stream_State)
  → Delivery (Composition_Stage: 5 states)
    → Snapshot (Snapshot_Lifecycle: 5 states, diagnostics-only)
      → Health (System_Health_Level: 4 states)
        → Reliability (Stream_Reliability: 7 states — canonical trust gate)
          → Readiness (Data_Readiness: 6 states — data availability only)
            → Visual (Pane_Visual_State: 8 states — display decision)
```

All derivations are **pure functions**. No cached health state. Side effects isolated to `health.odin` and `store_adapters.odin`.

### Cell_Surface_View (10 fields — canonical per-cell read model)

| Field | Type | Source |
|-------|------|--------|
| composition | Composition_Stage | apply_state_composition_stage() |
| has_live_data | bool | any artifact live |
| artifact_has_live | [Artifact_Kind]bool | per-artifact flags |
| health_level | System_Health_Level | stream_health_level() |
| recovery_attempts | u8 | apply_state.recovery_attempts |
| reliability | Stream_Reliability | stream_reliability() |
| backfill_expectation | Backfill_Expectation | derive_backfill_expectation() |
| venue | string | resolved label |
| symbol | string | resolved label |
| stream_bound | bool | explicit binding |

S154 removed: candle_health, snapshot_lifecycle, is_transport_lagging, recovery_status, stale_count, aging_count. All removals verified clean — zero orphan references.

### Orderflow Vertical Slice (ADR-0033 + ADR-0035)

```
Trade events → market_store_reduce_trade()
  ├→ Trades_Store (always)
  ├→ DOM_Store (per-stream, TF-independent)
  └→ Footprint_Store (per-stream, TF-aware)

Widget_Kind.Footprint → render_footprint_contract → render_footprint_widget
Widget_Kind.DOM → orderbook_dom_render (via layer strategy)
```

- 4 stores: DOM (18 tests), Footprint (15 tests), Orderbook (11 tests), Trades (8 tests)
- 17 integration tests (s157_orderflow_slice_test.odin)
- TF-change clearing in 3 code paths
- Store resolution for both follow-active and per-pane paths

### Layer Dependency Graph

```
ports  →  services  →  layers  →  app
  ↑          ↑           ↑
  └──────────┴───────────┘
       (no reverse imports)
```

Zero cyclic dependencies. Zero cross-boundary violations confirmed.

---

## Deferred Items Registry

### P2 — Before Shipping Multi-Instrument

| Item | Origin | Trigger |
|------|--------|---------|
| Footprint memory soak test (10+ instruments × 200 candles) | S155 | Before production multi-instrument |

### P3 — UX Refinement

| Item | Origin | Trigger |
|------|--------|---------|
| DOM scroll/zoom interaction | S157 | Post-MVP UX pass |
| DOM price grouping UI (wire dom_group_idx) | S155 | Footprint price grouping feature |
| Footprint candle-viewport alignment | S157 | After chart scroll/zoom refactor |

### P4 — Low Priority / Future Architecture

| Item | Origin | Trigger |
|------|--------|---------|
| Transport lag badge in cell headers | S151 | UI polish pass |
| Adaptive backfill timeout from server latency | S152 | Cold reader latency variance observed |
| Cross-venue orderflow composition | S156 | Multi-connection architecture |
| Fill age decay animation | S156 | UX feedback |
| Backend FootprintCandleV1 | ADR-0035 | Multi-client consistency or replay-safe required |
| health_level→reliability color mapping in draw_health_dot | S154 | UI polish pass |
| Per-venue backfill expectations | S152 | Exchange-specific history depth differentiation |
| Backfill progress indicator (completion %) | S152 | Long GetRange UX improvement |

---

## Guard Rails for Next Stages

### Structural Constraints

1. **Cell_Surface_View ceiling: 10 fields** — Any new per-cell state must replace an existing field or prove it cannot be derived on demand
2. **Data_Readiness: 6 variants** — Reliability is checked separately in resolve_pane_visual_state, never mixed into readiness
3. **Pure derivation only** — Health, reliability, readiness, backfill expectation are always computed from Stream_Apply_State, never cached
4. **Per-stream store isolation** — DOM, Footprint, and all future orderflow stores live on Market_Stream, not global state

### Boundary Rules

5. **No new bounded contexts** — Orderflow data plane lives inside existing layers/services hierarchy
6. **Layer contract immutability** — Layer_Context is read-only; strategies are stateless functions
7. **services/ never imports layers/ or app/** — Store types are leaf nodes in dependency graph
8. **layers/ never imports app/** — Market_Store is orchestrated by app, not the reverse

### Versioning

9. **Workspace schema bump only on persistence format change** — Not for every feature addition
10. **Runtime snapshot bump only when indicator_flags or chart_display bits change**

### Quality

11. **1,317 test baseline** — No stage ships with fewer passing tests
12. **All health/state types must have test coverage for boundary transitions**
13. **No fmt.Sprintf/tprintf on hot path** — Use buffer concat or strings.builder

---

## Risks

| Risk | Severity | Mitigation | Status |
|------|----------|------------|--------|
| Footprint memory at scale (N instruments × 200 candles × 50 levels) | Medium | Configurable level cap, LRU eviction in Footprint_Store design | Designed, not soak-tested |
| Client-local footprint non-authoritative (disconnect loses data) | Low | Backend FootprintCandleV1 deferred; acceptable for MVP | Accepted |
| Per-slot transport state (all cells share global) | Low | Multi-connection architecture required for fix | Future |
| Memory leak warnings in test suite (unmarshal_string_token, push_text) | Low | Does not affect test results; Odin allocator tracking noise | Monitor |

---

## Outcome

**Zero code changes required.** The S151–S157 block left the codebase consolidated:

- No residual legacy
- No duplication across domains
- No boundary violations
- No contradictions between ADRs and implementation
- All deferred items documented with priority and trigger
- 1,317 tests passing as baseline

The architecture is **simpler and more solid** than before S151. Ready for the next product block.
