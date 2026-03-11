# Stage 150 — Product Architecture Consolidation Pass II

**Date:** 2026-03-09
**Scope:** Audit and consolidate S143–S149 before opening new surfaces
**Status:** COMPLETE

---

## 1. Audit Summary

### Stages Reviewed

| Stage | Title | Files Modified | New Tests |
|-------|-------|----------------|-----------|
| S143 | Stream Health & Desync Model Hardening | 5 | 22 |
| S144 | Snapshot Lifecycle & Recovery Policy | 3 | 16 |
| S145 | Dashboard Recovery UX & Operator Trust | 5 | 8 |
| S146 | Timeframe/Data Availability Semantics | 4 (+2 new) | 53 |
| S147 | Orderflow Domain Blueprint | 0 (+2 docs) | 0 |
| S148 | Orderflow Data Plane Foundations | 6 | 3 |
| S149 | Orderflow Vertical Slice I: DOM Ladder | 4 (+2 new) | 23 |

**Total:** 125 new tests across 7 stages. **1,180 tests pass** (485 md_common + 437 app + 204 services + 54 layers).

### Compilation Gates

- `check-core`: 10/10 packages OK
- `check-wasm-compile`: OK
- All 4 test suites: **green, zero failures**

---

## 2. Architecture Assessment

### 2.1 Stream Health Model (S143–S145)

**Verdict: CLEAN — no consolidation needed.**

Three orthogonal concerns, correctly layered:

| Concern | Enum | Source | Owner |
|---------|------|--------|-------|
| Transport reliability | `Stream_Reliability` (7 states) | `stream_apply_state.odin` | md_common |
| Data validity | `Snapshot_Lifecycle` (5 states) | `stream_apply_state.odin` | md_common |
| Recovery progress | `Recovery_Status` + `Remediation_Decision` | `stream_apply_state.odin` | md_common |

All derivation is pure-functional. `Cell_Surface_View` exposes `.reliability`, `.snapshot_lifecycle`, `.recovery_attempts` — each orthogonal, no redundancy.

UI layer (S145) consumes all three to produce overlays and badges. No logic duplication between overlay rendering and health derivation.

### 2.2 Timeframe Data Contract (S146)

**Verdict: CLEAN — well-isolated.**

`tf_data_contract.odin` (250 LOC) is a pure value-table module:
- `TF_Class` taxonomy (5 classes)
- Per-class expectation matrices (backfill criticality, live-only utility, overlay patience)
- Query API used by `widget_readiness.odin` and `shell_common.odin`

No coupling to orderflow or health models. No duplication with existing TF handling.

### 2.3 Orderflow Domain (S147–S149)

**Verdict: CLEAN — correct scoping.**

- S147 (blueprint): Design-only, produced ADR-0033. No code changes.
- S148 (data plane): Moved DOM/Footprint stores from global singletons to per-stream. Trade reducer wires DOM_Store.
- S149 (DOM slice): First vertical slice using S148 foundations. Rendering composites orderbook + DOM fills.

**Deferred items (by design, not residual):**
- Footprint_Store wiring (needs TF context from candle reducer)
- DOM scroll/zoom, price grouping UI
- Cross-venue orderflow
- Fill age decay animation

### 2.4 ADR Alignment

34 ADRs total. ADR-0032 (Stream Reliability) and ADR-0033 (Orderflow Blueprint) are:
- Orthogonal to each other
- Aligned with ADR-0001 (Bounded Contexts)
- Compatible with ADR-0026 (Pane Runtime), ADR-0031 (Dashboard Operating Model)

**No conflicts or superseded ADRs detected.**

---

## 3. Residual Legacy Scan

### Intentional Legacy (retained by design)

| Pattern | Location | Reason |
|---------|----------|--------|
| `Legacy_JSON` transport mode | `mr_protocol.odin` | Server protocol downgrade path (production feature) |
| Legacy sidebar | `sidebar.odin` | Used inside dashboard detail panel |
| V6 legacy tolerance | `workspace_artifact.odin` | Backward compat for older workspace files |
| Entity_World sync | `build_cell.odin`, `chart_interaction.odin` | Compare/focus mode still uses Entity_World |
| S97 migration comments | `layer_marketdata.odin` | Informational (documenting code origin) |

### Actionable Legacy from S143–S149

**None found.** All changes are canonical with no superseded patterns.

### Pre-existing TODO

One unrelated TODO: `build_workspace.odin:167` — "persist tree ratios to settings" (pre-dates S143).

---

## 4. Boundary Health

### Layer Boundaries

| Boundary | Direction | Status |
|----------|-----------|--------|
| md_common → app | Pure functions consumed by app | CLEAN |
| services → app | Stores consumed read-only | CLEAN |
| app → layers | Layer_Context read-only bridge | CLEAN |
| layers → services | Data_Source reads from stores | CLEAN |

**No boundary violations detected.** No UI logic in data layers. No store mutation from rendering.

### Hotspot Files (touched by 3+ stages)

| File | Stages | LOC | Risk |
|------|--------|-----|------|
| `stream_slots.odin` | S143, S144, S145 | 1,164 | LOW — integration point by design |
| `widget_readiness.odin` | S143, S146, S149 | ~400 | LOW — query API accumulation is natural |
| `shell_common.odin` | S143, S145, S146 | 716 | LOW — overlay rendering consolidation point |

All hotspots are integration surfaces, not signs of coupling. No file exceeds its natural responsibility.

---

## 5. Duplication Analysis

| Candidate | Assessment |
|-----------|-----------|
| `stream_reliability_blocks_render()` vs `snapshot_lifecycle_blocks_render()` | **NOT duplicate** — orthogonal gates (transport vs data) |
| Overlay messaging (S143→S145→S146) | **Layered extension** — each stage adds, none duplicates |
| `Data_Readiness` variants (S143) vs TF queries (S146) | **Complementary** — S143 adds enum states, S146 adds query API |
| Cell_Surface_View field accumulation | **Orthogonal fields** — reliability, snapshot_lifecycle, recovery_attempts serve different consumers |

**Zero duplication found.**

---

## 6. Test Coverage Quality

| Module | Tests | Coverage Focus |
|--------|-------|----------------|
| md_common | 485 | Health derivation, reliability, TF contract, apply_state boundaries |
| app | 437 | Widget readiness, overlay rendering, interaction, workspace |
| services | 204 | Message parsing, DOM store, analytics range |
| layers | 54 | Rendering strategies, DOM ladder, time axis |

**Gaps:** None critical. S147 (design stage) has 0 tests — correct, no code changes. S148 has 3 tests — minimal but stores are tested transitively via S149 (23 tests).

---

## 7. Memory & Performance

- Per-stream budget post-S148: +4.1 MB across 16 streams (DOM + Footprint per-stream)
- DOM_Store: 512 levels, 128 recent fills — fixed capacity, zero allocation after init
- All health derivation functions: pure, zero-alloc, deterministic

**No performance regressions introduced.**

---

## 8. Risk Register

### Current Risks

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Cell_Surface_View field growth | LOW | Monitor — currently 3 new fields, all orthogonal |
| Footprint_Store unwired | LOW | Deferred by design; wiring location documented (S148) |
| Per-stream memory at scale (32+ streams) | LOW | Budget is ~8.2 MB at 32 streams; within browser limits |

### Retired Risks (resolved by S143–S149)

- Global singleton DOM/Footprint (fixed in S148)
- Offline/Desync blank overlay (fixed in S143)
- Stale snapshot after recovery (fixed in S144)
- TF-unaware overlay messaging (fixed in S146)

---

## 9. Guard Rails for Next Block

1. **Footprint wiring** must go through candle reducer (not trade reducer) — requires TF context
2. **DOM scroll/zoom** should reuse `Chart_Viewport` from S139, not create parallel viewport
3. **Cross-venue orderflow** requires new composition model — deserves its own ADR before implementation
4. **Fill decay** animation should use frame-delta timing from `app.odin` main loop, not wall-clock
5. **New Cell_Surface_View fields** should be justified against existing orthogonal fields — avoid field bloat

---

## 10. Conclusion

**Architecture post-S143–S149 is solid.** No consolidation actions needed — the seven stages were well-scoped and cleanly layered:

- Health/reliability model (S143–S145): three orthogonal concerns converging at UI
- TF contract (S146): isolated pure-value module
- Orderflow (S147–S149): blueprint → foundations → slice progression

**1,180 tests pass. Zero regressions. Zero residual legacy. Zero duplication.**

Base is ready for the next product block.
