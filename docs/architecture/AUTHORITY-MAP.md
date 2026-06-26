# Authority Map

**Status:** Active | **Date:** 2026-06-25 | **Version:** 5

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

Code must conform to T1 documents. Changes require a contract revision or explicit architecture update.

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
| `signal-engine.md` | Signal engine contract (**Retired S9** — see T4) |
| `analytics-pipeline.md` | Kafka→Flink→TimescaleDB wire contract (Kafka JSON schema + Flink sink schema) |
| `workspace-schema.md` | Workspace persistence schema (MaxSchemaVersion, fingerprint algorithm) |

### Architecture Docs (structural)

How does the system work? These define structure, not wire formats.

| Document | Scope |
|----------|-------|
| `architecture/README.md` | System overview, data flow, bounded contexts |
| `architecture/subsystems.md` | Per-subsystem boundaries, I/O, capabilities |
| `architecture/system-invariants.md` | INV-* rules (CI: `make invariants-check`) |
| `architecture/sequencing-model.md` | Ordering guarantees, DecideMonotonic |
| `architecture/iq-loop-invariants.md` | IQ1–IQ10 execution properties |
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
| Operations | `operations/sharding.md`, `operations/cold-path-runbook.md`, `operations/backup-recovery.md`, `operations/degradation.md`, `operations/subminute-rollout.md`, `operations/shard-incidents.md`, `operations/emulator.md`, `operations/validator.md` |
| Alerts | `deploy/observability/prometheus/*.yml` |
| Domain detail | `architecture/{orderbook,candle-aggregation,stats-aggregation,heatmap,volume-profiles,liquidations-markprice,storage,metrics-catalogue}.md` |
| Templates | `doc-contract-template.md` |

Domain detail docs describe implementation that is governed by T1 (subsystems.md + ADRs).

---

## T3 — Evolutionary (Active Proposals)

Not yet binding. Consult for direction, do not treat as canonical.

| Document | Scope | Status |
|----------|-------|--------|
| `prds/moat.md` | Competitive analysis | Reference |

**Promotion:** Proposal accepted + implemented → capture in architecture doc (T1) → moves to T4.

---

## T4 — Historical

Past records. **Never consulted for future decisions.**

| Category | Notes |
|----------|-------|
| Stage planning docs (stage1–stage9b) | Removed from repo; described completed work phases |
| Retired docs | `client-roadmap-6.8-to-8.0.md`, `ingestion.md`, `decisions.md`, `insights.md`, `signal-engine.md` (cmd/signals removed in S9) |
| Retired operations | `signals-strategist-cutover.md` (topology no longer exists) |

Stage planning docs and runbooks for retired subsystems are not in the repo. They described work that is now part of the live codebase.

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
| What goes on the wire | Contract (T1) |
| How the system is structured | Architecture doc (T1) |
| Which doc owns a theme | TRUTH-MAP (T1) |
| How to set up / operate | Operational doc (T2) |
| What's being proposed | T3 proposal |
