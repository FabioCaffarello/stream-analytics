# Authority Map

**Status:** Active | **Date:** 2026-03-10 | **Version:** 4

---

## Document Hierarchy

```
T1 Canonical ──── governs code, architecture, contracts
T2 Operational ── how to run, develop, test
T3 Evolutionary ─ proposals not yet binding
T4 Historical ─── past records, never governs
```

**Precedence:** T1 > T2 > T3 > T4. Within a tier, the more specific document wins.

---

## T1 — Canonical (Source of Truth)

Code must conform to T1 documents. Changes require ADR, PRD update, or contract revision.

### ADRs (36 total) — `docs/adrs/ADR-NNNN-*.md`

Why this approach? What alternatives were rejected?

| Range | Domain |
|-------|--------|
| 0000–0020 | Foundation, bounded contexts, runtime, storage, bus, config, exchanges, lifecycle, backpressure, sequencing, replay, supervision, GitOps |
| 0021–0023 | Signals-strategist cutover, Odin action pipeline, frozen semantic model |
| 0024–0031 | Dashboard & workspace: architecture, split-tree, pane runtime, widget host, data context, migration, operating model |
| 0032–0035 | Stream health & orderflow: reliability model, orderflow blueprint, health recovery, orderflow contracts |

### PRDs (5 total) — `docs/prds/PRD-NNNN-*.md`

What must be true? What are the acceptance criteria?

| PRD | Scope | Status |
|-----|-------|--------|
| 0001 | Program baseline: SLOs, invariants | Implemented |
| 0002 | Backend stable + Odin-ready acceptance | Implemented |
| 0003 | MarketMonkey backend parity (M1–M7) | Validated |
| 0004 | Backend evolution — production hardening | Draft |
| 0006 | Client evolution — MM parity | Partially Implemented |

**PRD vs ADR:** PRDs win on scope ("what"), ADRs win on mechanism ("how").

### Wire Contracts — `docs/contracts/`

Machine-enforceable truth. What goes on the wire?

| Document | Scope |
|----------|-------|
| `event-bus.md` | Envelope schema, subject taxonomy, versioning |
| `subject-registry.yaml` | Subject inventory (CI: `make contract-gates`) |
| `delivery-ws.md` | WS protocol, client commands, backpressure |
| `boundedness-matrix.md` | Resource limits per subsystem |
| `canonical-market-model.md` | CMM field definitions |
| `liquidity-evidence-layer.md` | LEL rule contracts |
| `signal-engine.md` | Signal engine contract |

### Architecture Docs (structural)

How does the system work? These define structure, not wire formats.

| Document | Scope |
|----------|-------|
| `architecture/README.md` | System overview, data flow, bounded contexts |
| `architecture/subsystems.md` | Per-subsystem boundaries, I/O, capabilities |
| `architecture/system-invariants.md` | INV-* rules (CI: `make invariants-check`) |
| `architecture/sequencing-model.md` | Ordering guarantees, DecideMonotonic |
| `architecture/iq-loop-invariants.md` | IQ1–IQ10 execution properties |
| `architecture/decisions.md` | ADR + RFC index |
| `architecture/TRUTH-MAP.md` | Per-theme source of truth with code anchors |
| `architecture/AUTHORITY-MAP.md` | This file |

### Client Architecture

| Document | Scope |
|----------|-------|
| `client/client-architecture.md` | Layer hierarchy (ports→services→layers→app) |
| `client/layer-architecture.md` | Strategy lifecycle, rendering pipeline |
| `client/client-memory-ownership-rules.md` | Odin lifetime & allocation rules |

---

## T2 — Operational

How to run, develop, test. Updated in-place by any contributor. Does not define architecture.

| Category | Documents |
|----------|-----------|
| Dev setup | `local-dev.md`, `tooling.md`, `development-workflow.md`, `CONTRIBUTING.md` |
| Testing | `testing-strategy.md` |
| Product | `product-definition.md` |
| Observability | `observability/slo.md`, `observability/metrics-policy.md`, `observability/runbooks/*.md` |
| Operations | `operations/sharding.md`, `operations/cold-path-runbook.md`, `operations/backup-recovery.md`, `operations/degradation.md`, `operations/subminute-rollout.md` |
| Runbooks | `runbooks/*.md` |
| Alerts | `deploy/observability/prometheus/*.yml` |
| Domain detail | `architecture/{orderbook,candle-aggregation,stats-aggregation,heatmap,volume-profiles,liquidations-markprice,storage,insights}.md` |
| Templates | `doc-contract-template.md` |

Domain detail docs describe implementation that is governed by T1 (subsystems.md + ADRs).

---

## T3 — Evolutionary (Active Proposals)

Not yet binding. Consult for direction, do not treat as canonical.

| Document | Scope | Status |
|----------|-------|--------|
| RFC-0012 | Client multi-exchange evolution | Draft (partially superseded) |
| RFC-0013 | Client hardening blueprint | Draft (partially superseded) |
| RFC-0015 | Backend sub-minute hardening | Draft |
| `prds/moat.md` | Competitive analysis | Reference |

Path: `docs/rfcs/RFC-NNNN-*.md`

**Promotion:** RFC accepted + implemented → capture in ADR (T1) → RFC moves to T4.

---

## T4 — Historical

Past records. **Never consulted for future decisions.** Append-only.

| Category | Location | Count |
|----------|----------|-------|
| Stage reports | `docs/stages/` (S010–S158) | 135 |
| Current audits | `docs/audits/*-2026-03-10.md` | 7 |
| Superseded audits | `AUDIT-PACK-W11`, `DRIFT-REPORT-W11`, etc. | — |
| Retired RFCs | RFC-0001–0011, RFC-0014, `EXECUTION-SEQUENCE.md` | — |
| Retired docs | `client-roadmap-6.8-to-8.0.md`, `ingestion.md`, `insights.md`, `rfcs/archive/*` | — |

Stage reports are delivery receipts. Audits are point-in-time snapshots. Neither governs.

---

## Precedence Rules

1. **T1 always wins.** If a stage report contradicts an ADR, the ADR governs.
2. **More specific wins within same tier.** ADR-0032 beats README.md on stream reliability.
3. **PRD vs ADR:** PRDs define scope ("what"), ADRs define mechanism ("how"). PRD wins on scope, ADR wins on mechanism.
4. **Code vs docs:** If code and T1 doc disagree, the T1 doc is the intended truth — fix the code (or update the doc via proper process).
5. **No document chain citations.** Future work cites T1 as authority. Never cite a stage report or audit as justification for a decision.

---

## Classifying New Documents

When creating a new document, answer one question:

| Question | → Tier |
|----------|--------|
| Does code/architecture **must conform** to it? | **T1** — requires ADR or contract revision |
| Does it describe **how to operate/develop**? | **T2** — update in-place, no ADR |
| Is it a **proposal** for future change? | **T3** — track status, promote via ADR when accepted |
| Is it a **record** of completed work? | **T4** — append-only, never update |

If unsure: default to T4. It's easy to promote; hard to demote.

---

## Lifecycle Transitions

| From → To | Trigger |
|-----------|---------|
| T3 → T1 | RFC accepted + implemented → write ADR |
| T1 → T4 | ADR superseded → mark `Superseded by ADR-NNNN` |
| T2 stays T2 | Operational docs updated in-place |
| Audit refresh | New audit → old audit moves to T4 |

---

## Quick Reference

| I need to know... | Go to |
|-------------------|-------|
| Why a decision was made | ADR (T1) |
| What the product must do | PRD (T1) |
| What goes on the wire | Contract (T1) |
| How the system is structured | Architecture doc (T1) |
| How to set up / operate | Operational doc (T2) |
| What's being proposed | RFC (T3) |
| What happened in stage N | Stage report (T4) |
| Current system health | Latest audit (T4) |
| Who owns a topic | TRUTH-MAP (T1) |
