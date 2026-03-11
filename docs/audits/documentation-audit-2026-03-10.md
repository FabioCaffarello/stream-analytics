# Documentation Audit — Market Raccoon

**Status:** Active
**Date:** 2026-03-10
**Scope:** 271 markdown files across `docs/`, project root, `.claude/`, `client/`
**Method:** Full inventory, cross-reference against codebase (S158 baseline), conflict analysis

---

## Purpose

Classify every documentation artifact by canonical status, identify conflicts between governance docs and actual project state, and recommend curation actions for the next 11 months.

## Classification Legend

| Category | Code | Meaning |
|----------|------|---------|
| Canonical & Valid | **C1** | Authoritative, code-anchored, current |
| Valid, Needs Update | **C2** | Correct in substance but stale in detail |
| Historical | **C3** | Valuable record, does not govern future decisions |
| Obsolete | **C4** | Generates confusion, should be archived or deleted |

---

## 1. Inventory

### 1.1 — C1: Canonical & Valid (~45 docs)

#### ADRs (Foundational)

| Doc | Status |
|-----|--------|
| `docs/adrs/ADR-0000-foundation.md` | Accepted |
| `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md` | Accepted |
| `docs/adrs/ADR-0002-event-envelope-and-versioning.md` | Accepted |
| `docs/adrs/ADR-0003-actor-runtime.md` | Accepted |
| `docs/adrs/ADR-0004-bus-nats-jetstream.md` | Accepted |
| `docs/adrs/ADR-0005-sequencing-and-time-normalization.md` | Accepted |
| `docs/adrs/ADR-0006-storage-hot-vs-cold.md` | Accepted |
| `docs/adrs/ADR-0007-delivery-ws-sessions.md` | Accepted |
| `docs/adrs/ADR-0008-insights-decision-support.md` | Accepted |
| `docs/adrs/ADR-0009-config-jsonc-determinism.md` | Accepted |
| `docs/adrs/ADR-0010-config-loading-startup-validation.md` | Accepted |
| `docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md` | Accepted |
| `docs/adrs/ADR-0012-lifecycle-invariants-leak-prevention.md` | Accepted |
| `docs/adrs/ADR-0013-backpressure-overload-policies.md` | Accepted |
| `docs/adrs/ADR-0014-stream-partitioning-strategy.md` | Accepted |
| `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` | Accepted |
| `docs/adrs/ADR-0016-protobuf-contract-layer.md` | Accepted (partial impl) |
| `docs/adrs/ADR-0017-multi-exchange-normalization.md` | Accepted |
| `docs/adrs/ADR-0018-actor-topology-supervision-model.md` | Accepted (partial impl) |
| `docs/adrs/ADR-0019-dual-database-operational-strategy.md` | Accepted |
| `docs/adrs/ADR-0020-gitops-secrets-management.md` | Accepted (in progress) |

#### ADRs (Dashboard & Workspace — S122–S142)

| Doc | Status |
|-----|--------|
| `docs/adrs/ADR-0024-dashboard-workspace-architecture.md` | Accepted |
| `docs/adrs/ADR-0025-split-tree-layout-model.md` | Accepted |
| `docs/adrs/ADR-0026-pane-runtime-model.md` | Accepted |
| `docs/adrs/ADR-0027-widget-host-contract.md` | Accepted |
| `docs/adrs/ADR-0028-data-context-ownership.md` | Accepted |
| `docs/adrs/ADR-0029-migration-plan-grid-to-workspace-tree.md` | Accepted |
| `docs/adrs/ADR-0030-pane-data-context-ownership.md` | Accepted |
| `docs/adrs/ADR-0031-dashboard-operating-model.md` | Accepted |

#### ADRs (Stream Health & Orderflow — S143–S157)

| Doc | Status |
|-----|--------|
| `docs/adrs/ADR-0032-stream-reliability-model.md` | Accepted |
| `docs/adrs/ADR-0033-orderflow-domain-blueprint.md` | Accepted |
| `docs/adrs/ADR-0034-stream-health-recovery-completion.md` | Accepted |
| `docs/adrs/ADR-0035-orderflow-contract-architecture.md` | Accepted |

#### Contracts

| Doc | Justification |
|-----|--------------|
| `docs/contracts/event-bus.md` | Wire truth, validated by `make contract-gates` |
| `docs/contracts/delivery-ws.md` | WS protocol, 637 LOC, runtime-adherent |
| `docs/contracts/subject-registry.yaml` | Machine-readable, CI-validated |
| `docs/contracts/boundedness-matrix.md` | Resource limits, runtime-adherent |

#### Architecture

| Doc | Justification |
|-----|--------------|
| `docs/architecture/README.md` | System overview, updated 2026-03-06 |
| `docs/architecture/subsystems.md` | 850+ LOC, boundary definitions |
| `docs/architecture/system-invariants.md` | Validated by `make invariants-check` |
| `docs/architecture/sequencing-model.md` | Ordering model, code-anchored |
| `docs/architecture/iq-loop-invariants.md` | IQ1–IQ10 execution properties |

#### PRDs

| Doc | Justification |
|-----|--------------|
| `docs/prds/PRD-0003-mm-backend-parity.md` | Validated 2026-02-20, M1–M7 done |

#### Operations & Observability

| Doc | Justification |
|-----|--------------|
| `docs/local-dev.md` | 389 LOC, updated 2026-03-06 |
| `docs/tooling.md` | Toolchain guide, Makefile-adherent |
| `docs/testing-strategy.md` | Testing layers, CI-adherent |
| `docs/development-workflow.md` | Workflow, adherent |
| `docs/doc-contract-template.md` | Template, canonical |
| `docs/observability/slo.md` | SLO definitions |
| `docs/observability/metrics-policy.md` | Label budget |
| `docs/operations/sharding.md` | Operational, adherent |
| `docs/operations/cold-path-runbook.md` | Active runbook |
| `docs/operations/backup-recovery.md` | DR procedures |

#### Client

| Doc | Justification |
|-----|--------------|
| `docs/client/client-architecture.md` | Layer hierarchy, post-S158 adherent |
| `docs/client/layer-architecture.md` | ports→services→layers→app, validated |
| `docs/client/client-memory-ownership-rules.md` | Odin lifetime rules |

#### Audits (Current)

| Doc | Justification |
|-----|--------------|
| `docs/audits/client-architecture-audit-2026-03-10.md` | Fresh, zero drift |
| `docs/audits/architectural-audit-2026-03-10.md` | Fresh, zero drift |
| `docs/audits/end-to-end-critical-flows-audit-2026-03-10.md` | Fresh, zero drift |
| `docs/stages/stage-158-consolidation-product-guard-rails-report.md` | Consolidation snapshot |

---

### 1.2 — C2: Valid, Needs Update (~18 docs)

| Doc | Problem | Action |
|-----|---------|--------|
| `docs/architecture/TRUTH-MAP.md` | Covers ADRs 0000–0023 only; **missing ADRs 0024–0035** (12 ADRs for workspace, health, orderflow) | Expand inventory |
| `docs/architecture/AUTHORITY-MAP.md` | References `docs/prd/` (singular) but real path is `docs/prds/` (plural); lists only PRD-0001/0002, **missing PRD-0003, 0004, 0006**; references `.context/docs/feature-packs/` which is being deleted | Fix paths, add PRDs, remove `.context/` ref |
| `docs/prds/PRD-0006-client-evolution-mm-parity.md` | Status "Draft" but S147–S157 already implemented orderflow (G1 partial); LOC count "15,947" outdated (now ~30K+) | Update to Partially Implemented, fix metrics |
| `docs/prds/PRD-0004-backend-evolution-production-hardening.md` | Status "Draft", no milestones started; claims "2/5 security" — needs priority re-evaluation | Priority review |
| `docs/prds/PRD-0001-extreme-runtime.md` | Functional baseline, but W-series finished; implementation matrix outdated | Mark W* milestones Done |
| `docs/prds/PRD-0002-backend-stable-and-odin-ready.md` | Backend stable achieved, Odin achieved (S158) — status must reflect | Update to Implemented |
| `docs/rfcs/EXECUTION-SEQUENCE.md` | Stops at W14; project has evolved 100+ stages beyond | Mark Historical/Complete |
| `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` | Gap checklist GD-01 to GD-13 all "RESOLVED"; served its purpose, GD-11/12 evolved via PRD-0003 | Mark Accepted, cross-ref PRD-0003 |
| `docs/rfcs/RFC-0012-client-multi-exchange-evolution.md` | Client already supports 6 exchanges; RFC may be partially superseded | Verify and update status |
| `docs/rfcs/RFC-0013-client-hardening-blueprint-marketmonkey-parity.md` | Client evolved significantly since writing | Verify status |
| `docs/rfcs/RFC-0014-client-ui-interaction-architecture-marketmonkey-reference.md` | Interaction architecture implemented (S139–S142) | Mark Implemented |
| `docs/rfcs/RFC-0015-backend-subminute-hardening-rollout.md` | Verify if rollout occurred | Status audit |
| `docs/architecture/decisions.md` | Index of 11 decisions; may be missing ADR-0024+ entries | Expand |
| `docs/contracts/canonical-market-model.md` | 70 LOC only, may need enrichment post-orderflow | Verify completeness |
| `docs/contracts/signal-engine.md` | 150 LOC; ADR-0023 (frozen semantic model) may have altered contracts | Verify adherence |
| `docs/CONTRIBUTING.md` | Status "Living draft", 38 LOC, "next steps" may already be done | Update |
| `docs/README.md` | General index — verify all subdirs are covered | Verify completeness |
| `docs/adrs/ADR-0023-frozen-semantic-model-*.md` | Status "Proposed", date 2026-03-11 (future) — may not reflect real state | Verify and normalize |

---

### 1.3 — C3: Historical, Non-Canonical (~145 docs)

#### Stage Reports (134 files)

All files in `docs/stages/stage-010-*` through `docs/stages/stage-157-*` are valuable development records but do not govern future decisions. Only `stage-158` is canonical as the current consolidation snapshot.

#### Superseded Audits

| Doc | Superseded By |
|-----|--------------|
| `docs/audits/AUDIT-PACK-W11-finalization.md` | 2026-03-10 audits |
| `docs/audits/DRIFT-REPORT-W11.md` | 2026-03-10 audits |
| `docs/audits/W4-W5-AUDIT.md` | Explicitly archived |
| `docs/audits/PRE-STAGE9-ARCHITECTURAL-AUDIT-2026-03-06.md` | 2026-03-10 audits |
| `docs/audits/PRE-STAGE9-DRIFT-MATRIX-2026-03-06.md` | 2026-03-10 audits |

#### Other Historical

| Doc | Justification |
|-----|--------------|
| `docs/rfcs/archive/*` (5 files) | Explicitly archived |
| `docs/prds/moat.md` | Competitive analysis from 2026-02-15; reference only |
| `docs/adrs/ADR-0021-signals-strategist-*` | Implemented, cutover done |
| `docs/adrs/ADR-0022-odin-client-action-pipeline-*` | Implemented |

---

### 1.4 — C4: Obsolete / To Retire (~14 docs + 26 .context files)

| Doc | Justification |
|-----|--------------|
| `docs/client/client-roadmap-6.8-to-8.0.md` | **Most severe.** Describes world that no longer exists (RCL, phases 6.8–8.0, "55% parity"). Client is at S158 with 1,317 tests, 13 widgets, workspace tree. Generates active confusion. |
| `docs/rfcs/RFC-0001-robustness-roadmap.md` | W1–W13 roadmap 100% delivered; superseded by stage system |
| `docs/rfcs/RFC-0002-w1-config-shutdown-hardening.md` | W1 complete, code implemented, tested |
| `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md` | W2 complete |
| `docs/rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md` | W3 complete |
| `docs/rfcs/RFC-0005-W4-observability-profiling.md` | W4 complete |
| `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md` | W5 complete |
| `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md` | W6 partial, superseded by stage system |
| `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md` | W7 complete |
| `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md` | W8 complete |
| `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md` | W9 complete |
| `docs/architecture/ingestion.md` | 30 LOC, superficial diagram, replaced by `subsystems.md` |
| `docs/architecture/insights.md` | 25 LOC, shallow overview, replaced by `subsystems.md` |
| `.context/` (all 26 files staged for deletion) | Entire directory obsolete |

---

## 2. Document Conflicts

### Conflict 1 — AUTHORITY-MAP blind to 60% of PRDs (CRITICAL)

- **Location:** `docs/architecture/AUTHORITY-MAP.md`
- **Problem:** Lists only PRD-0001/0002 as product authorities. PRD-0003 (backend parity, validated), PRD-0004 (production hardening), PRD-0006 (client evolution) are unregistered.
- **Secondary:** Uses wrong path `docs/prd/` instead of `docs/prds/`. References `.context/docs/feature-packs/` which is being deleted.
- **Impact:** Any decision consulting the authority map ignores 3 active PRDs.

### Conflict 2 — TRUTH-MAP covers half the architecture (CRITICAL)

- **Location:** `docs/architecture/TRUTH-MAP.md`
- **Problem:** Inventories ADRs 0000–0023 only. ADRs 0024–0035 (12 ADRs covering workspace, dashboard, stream health, orderflow) have no truth anchors.
- **Impact:** Half of client architecture has no truth chain.

### Conflict 3 — Client roadmap vs reality (SEVERE)

- **Location:** `docs/client/client-roadmap-6.8-to-8.0.md`
- **Problem:** Describes "55% feature parity", phases 6.8–8.0, RCL Golden Render — all superseded. Client is at S158 with 1,317 tests, 13 widgets, workspace tree model, orderflow domain.
- **Impact:** Any new reader gets completely wrong picture of client state.

### Conflict 4 — PRD-0006 status vs code (MODERATE)

- **Location:** `docs/prds/PRD-0006-client-evolution-mm-parity.md`
- **Problem:** Status "Draft", lists 6 gaps as unresolved. S147–S157 already implemented DOM stores, footprint rendering, trade stores. G1 (orderflow) is partially delivered.
- **Impact:** Document understates actual progress.

### Conflict 5 — RFC W-series as active authority (MODERATE)

- **Location:** `docs/rfcs/RFC-0001` through `RFC-0010`
- **Problem:** 10 RFCs covering W1–W9 exist as non-archived documents. All content has been implemented and superseded by the stage system. EXECUTION-SEQUENCE stops at W14.
- **Impact:** Clutters active document space; risk of consulting outdated plans.

### Conflict 6 — ADR-0023 future date (MINOR)

- **Location:** `docs/adrs/ADR-0023-frozen-semantic-model-*.md`
- **Problem:** Date 2026-03-11 (tomorrow). Status "Proposed". May not reflect actual state.
- **Impact:** Low — but signals governance gap.

---

## 3. Top 15 Documents Requiring Immediate Action

| # | Doc | Action | Severity |
|---|-----|--------|----------|
| 1 | `docs/architecture/AUTHORITY-MAP.md` | Fix paths (`prd/`→`prds/`), add PRD-0003/0004/0006, remove `.context/` ref, add client governance domains | CRITICAL |
| 2 | `docs/architecture/TRUTH-MAP.md` | Expand to cover ADRs 0024–0035, RFCs 0012–0015 | CRITICAL |
| 3 | `docs/client/client-roadmap-6.8-to-8.0.md` | Retire or rewrite — generates active confusion | CRITICAL |
| 4 | `docs/prds/PRD-0006-client-evolution-mm-parity.md` | Update status Draft→Partially Implemented, mark G1 partial | HIGH |
| 5 | `docs/prds/PRD-0002-backend-stable-and-odin-ready.md` | Update status to Implemented | HIGH |
| 6 | `docs/rfcs/EXECUTION-SEQUENCE.md` | Mark as Historical/Complete | HIGH |
| 7 | `docs/prds/PRD-0001-extreme-runtime.md` | Update implementation matrix (W*→Done) | HIGH |
| 8 | `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` | Mark Accepted, cross-ref PRD-0003 as successor | MEDIUM |
| 9 | `docs/rfcs/RFC-0014-client-ui-interaction-architecture-*.md` | Mark Implemented (S139–S142) | MEDIUM |
| 10 | `docs/architecture/decisions.md` | Expand index with ADRs 0024–0035 | MEDIUM |
| 11 | `docs/CONTRIBUTING.md` | Update "next steps", remove completed items | MEDIUM |
| 12 | `docs/README.md` | Verify index covers audits, stages, client | MEDIUM |
| 13 | `docs/contracts/signal-engine.md` | Verify adherence vs ADR-0023 | MEDIUM |
| 14 | `docs/prds/PRD-0004-backend-evolution-production-hardening.md` | Re-prioritize: are security/TLS/JWT still real gaps? | LOW |
| 15 | `docs/rfcs/RFC-0012` + `RFC-0013` | Verify status vs current client | LOW |

---

## 4. Curation Recommendations

### A. Structural Actions (do now)

1. **Retire `client-roadmap-6.8-to-8.0.md`** — move to `docs/archive/` with supersession note.
2. **Move RFC W-series (0001–0010) to `docs/rfcs/archive/`** — all fulfilled, polluting active space.
3. **Update AUTHORITY-MAP** — governance document #1 is blind to 60% of PRDs.
4. **Expand TRUTH-MAP** — without ADRs 0024–0035 coverage, the truth chain is broken.

### B. Stage Reports Policy

The 134 stage reports are valuable historical records but should not be consulted for future decisions. Recommendations:
- Keep in `docs/stages/` as-is.
- Do not index in TRUTH-MAP (only stage-158 as consolidation snapshot).
- Consider a `docs/stages/INDEX.md` with 1-line summary per stage.

### C. Maintenance Cycle (11 months remaining)

| Frequency | Action |
|-----------|--------|
| Per stage | Update PRD status + TRUTH-MAP if new ADR created |
| Monthly | Verify AUTHORITY-MAP vs active PRDs |
| Quarterly | Full audit like this one; move obsolete docs to archive |

### D. Inventory Metrics

| Metric | Value | Percentage |
|--------|-------|-----------|
| Total docs | 271 | 100% |
| C1 — Canonical & Valid | ~45 | 17% |
| C2 — Valid, Needs Update | ~18 | 7% |
| C3 — Historical | ~145 | 53% |
| C4 — Obsolete | ~40 | 15% |
| Not audited (observability runbooks, ops detail) | ~23 | 8% |

### E. Verdict

The documentation base is **mature and well-structured** but suffers from **historical accumulation without curation** and **governance documents (AUTHORITY-MAP, TRUTH-MAP) lagging behind actual project evolution**. The 3 critical conflicts (#1, #2, #3) must be resolved before any new stage begins.

---

## Changelog

- 2026-03-10: Initial documentation audit. 271 files classified. 6 conflicts identified. 15 priority actions listed.
