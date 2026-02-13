# DRIFT-REPORT-W11 — Pre-Refactor Documentation Drift

**Status:** Draft
**Date:** 2026-02-13
**Scope:** documentation-only round (PREVC), before runtime refactor.

## Purpose

Document drift between intent (PRD/ADR/RFC/docs) and executable truth (code/tests), then define an incremental patch plan before W12/W13 refactor waves.

## Invariants

- Critical claims are anchored to ADR/RFC/PRD and/or code/test.
- Contradictions left unresolved in this round are explicitly tagged `TODO` or `OPEN QUESTION`.
- No runtime refactor is executed in this round.

## Planning (P)

### P1) Base docs summary

| Doc | Summary | Anchor |
|---|---|---|
| `docs/prd/PRD-0001-extreme-runtime.md` | Contains both long-term goals and stale "missing capability" claims now contradicted by W11 code/tests. | `docs/prd/PRD-0001-extreme-runtime.md:35`, `docs/prd/PRD-0001-extreme-runtime.md:37` |
| `docs/audits/AUDIT-PACK-W11-finalization.md` | Best current evidence graph (doc -> code -> test) for invariants and gates. | `docs/audits/AUDIT-PACK-W11-finalization.md:25` |
| `docs/rfcs/EXECUTION-SEQUENCE.md` | Operational timeline says W4..W10 done, with explicit deferred checkpoints. | `docs/rfcs/EXECUTION-SEQUENCE.md:12`, `docs/rfcs/EXECUTION-SEQUENCE.md:54` |
| `docs/rfcs/ADR-REVISIONS-patch-plan.md` | Historical amendment plan; parts already absorbed in ADR files. | `docs/rfcs/ADR-REVISIONS-patch-plan.md:1` |

### P2) Inventory snapshot

- ADR inventory 0000..0018: see `docs/architecture/TRUTH-MAP.md`.
- RFC inventory 0001..0010: see `docs/architecture/TRUTH-MAP.md`.
- Architecture docs:
  - `docs/architecture/README.md`
  - `docs/architecture/ingestion.md`
  - `docs/architecture/insights.md`
  - `docs/architecture/moat.md`
  - `docs/architecture/system-invariants.md`
- Contracts docs:
  - `docs/contracts/event-bus.md`

### P3) Single source of truth per required topic

Authoritative mapping is consolidated in `docs/architecture/TRUTH-MAP.md`, with doc + code/test anchors for:
- runtime invariants;
- subject taxonomy;
- ACK semantics;
- replay;
- backpressure;
- storage hot/cold;
- contract layer;
- multi-exchange.

### P4) Drift report

#### (a) Contradictions

| ID | Severity | A says | B says | Impact | Tracking |
|---|---|---|---|---|---|
| C-01 | P0 | Subject pattern is `<context>.<event>.<venue>.<instrument>`. (`docs/contracts/event-bus.md:77`) | Taxonomy requires event + version + venue + instrument; validator enforces vN segment. (`docs/adrs/ADR-0014-stream-partitioning-strategy.md:33`, `internal/adapters/jetstream/subject_validation.go:24`) | Wrong producer/consumer subjects, misrouting risk. | `TODO`: patch `docs/contracts/event-bus.md` to ADR-0014 canonical taxonomy. |
| C-02 | P0 | PRD claims "Sem NATS JetStream". (`docs/prd/PRD-0001-extreme-runtime.md:36`) | JetStream adapter + durable integration tests already exist. (`internal/adapters/jetstream/consumer.go:67`, `internal/adapters/jetstream/consumer_integration_test.go:152`) | Strategic planning and go/no-go decisions become wrong. | `TODO`: reconcile PRD snapshot section with W11 evidence. |
| C-03 | P0 | PRD claims "Sem deterministic replay". (`docs/prd/PRD-0001-extreme-runtime.md:37`) | Replay player/sequencer/golden are implemented and tested. (`internal/shared/replay/player.go:45`, `internal/shared/replay/golden_test.go:18`, `cmd/consumer/replay_test.go:63`) | Replay capability underestimated; acceptance criteria drift. | `TODO`: update PRD capability matrix to current state. |
| C-04 | P1 | PRD claims no Prometheus/pprof. (`docs/prd/PRD-0001-extreme-runtime.md:38`, `docs/prd/PRD-0001-extreme-runtime.md:39`) | Metrics + pprof endpoints exist and are tested. (`docs/rfcs/EXECUTION-SEQUENCE.md:50`, `internal/interfaces/http/server_test.go:1`) | Operational readiness appears lower than real state. | `TODO`: update PRD observability snapshot with test anchors. |
| C-05 | P1 | PRD claims "Sem multi-exchange testado". (`docs/prd/PRD-0001-extreme-runtime.md:43`) | Multi-exchange runtime and E2E tests are present. (`cmd/consumer/main.go:455`, `cmd/consumer/e2e_consumer_integration_test.go:24`) | Incorrect scope estimation for W12/W13. | `TODO`: patch PRD multi-exchange status. |
| C-06 | P1 | RFC-0008 and RFC-0010 remain `Proposed`. (`docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md:3`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md:3`) | Execution sequence and tests indicate delivered runtime behavior. (`docs/rfcs/EXECUTION-SEQUENCE.md:15`, `docs/rfcs/EXECUTION-SEQUENCE.md:17`) | Status taxonomy loses trust; difficult release audits. | `OPEN QUESTION`: promote RFCs to Accepted now or keep Draft with explicit "implemented evidence" section. |
| C-07 | P1 | ADR-0017 says `BASE-QUOTE` is derived by `CanonicalInstrument`. (`docs/adrs/ADR-0017-multi-exchange-normalization.md:24`) | `CanonicalInstrument` strips separators and returns `BTCUSDT`; ADR-0011 also states no separator canonical. (`internal/shared/naming/naming.go:16`, `docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md:15`) | Ambiguous canonical identity for keys and docs. | `TODO`: clarify ADR-0017 text separating key canonical vs display canonical. |

#### (b) Lacunas

| ID | Severity | Gap | Evidence | Tracking |
|---|---|---|---|---|
| L-01 | P0 | RFC taxonomy is non-uniform (`Done`, `Implemented`, `Proposed`) vs target (`Draft|Accepted`). | `docs/rfcs/RFC-0005-W4-observability-profiling.md:3`, `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md:3`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md:3` | `TODO`: normalize all RFC status headers with migration note preserving history. |
| L-02 | P1 | PRD taxonomy target says `Active|Deprecated`, but PRD is `Draft`. | `docs/prd/PRD-0001-extreme-runtime.md:3` | `TODO`: classify PRD-0001 as `Active` (or `Deprecated` with replacement PRD). |
| L-03 | P1 | Cold-path in ADR-0006 remains accepted but runtime implementation is deferred. | `docs/adrs/ADR-0006-storage-hot-vs-cold.md:32`, `internal/core/aggregation/ports/ports.go:17` | `OPEN QUESTION`: keep ADR accepted with explicit partial scope, or split into Accepted+Superseded follow-up ADR. |
| L-04 | P1 | MEX-4 CI guard (exchange-specific terms forbidden in core) still deferred. | `docs/rfcs/EXECUTION-SEQUENCE.md:202`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md:244` | `TODO`: add deterministic grep/audit command into `make invariants-check`. |
| L-05 | P2 | ACK/NAK/TERM behavior has strong test evidence but lacks a compact architecture doc as canonical entry point. | `internal/adapters/jetstream/ingest_conformance_test.go:15`, `internal/adapters/jetstream/consumer.go:279` | `TODO`: add short architecture note linking ADR-0004 + ingest conformance matrix. |
| L-06 | P2 | PRD and RFC checkpoints still reference non-canonical or deferred commands/evidence artifacts. | `docs/prd/PRD-0001-extreme-runtime.md:619`, `docs/rfcs/EXECUTION-SEQUENCE.md:54` | `TODO`: align checkpoints to workspace-safe gates and evidence files under `.context/evidence/`. |

#### (c) Obsolete or repetitive docs

| ID | Severity | Candidate | Why obsolete/repetitive | Tracking |
|---|---|---|---|---|
| O-01 | P1 | `docs/architecture/system-invariants.md` | Contains bootstrap-era instructions and marketing text mixed with invariants. | `docs/architecture/system-invariants.md:38`, `docs/architecture/system-invariants.md:82` | `TODO`: split into clean invariants doc + archive legacy bootstrap narrative. |
| O-02 | P2 | `docs/rfcs/ADR-REVISIONS-patch-plan.md` | Patch-plan artifact now duplicates amendments already merged in ADRs. | `docs/rfcs/ADR-REVISIONS-patch-plan.md:1` | `TODO`: mark as historical or deprecate after cross-linking to final ADR files. |
| O-03 | P2 | `docs/rfcs/W4-W5-AUDIT.md` vs `docs/audits/AUDIT-PACK-W11-finalization.md` | Two audit narratives overlap; W11 audit is broader and newer. | `docs/rfcs/W4-W5-AUDIT.md:1`, `docs/audits/AUDIT-PACK-W11-finalization.md:1` | `TODO`: keep W4/W5 as historical appendix and point to W11 as current authority. |
| O-04 | P1 | PRD section A snapshot | Stale capability snapshot conflicts with implemented runtime. | `docs/prd/PRD-0001-extreme-runtime.md:35` | `TODO`: preserve history but add explicit "as-of" section and W11 corrected state table. |

## Review (R)

### R1) Incremental patch plan (8 small commits)

1. `docs(w12): add truth map inventory and authority anchors`
   Files: `docs/architecture/TRUTH-MAP.md`
2. `audit(w11): add drift report with contradictions lacunas and obsolescence`
   Files: `docs/audits/DRIFT-REPORT-W11.md`
3. `chore(docs): link execution sequence to truth map and real validation gates`
   Files: `docs/rfcs/EXECUTION-SEQUENCE.md`
4. `docs(w12): normalize RFC status taxonomy to Draft/Accepted`
   Files: `docs/rfcs/RFC-0001..0010*.md`
5. `docs(w12): normalize PRD status taxonomy and refresh capability snapshot`
   Files: `docs/prd/PRD-0001-extreme-runtime.md`
6. `docs(w12): align contracts event-bus subject taxonomy with ADR-0014`
   Files: `docs/contracts/event-bus.md`
7. `docs(w12): clarify ADR-0017 canonical key vs display canonical`
   Files: `docs/adrs/ADR-0017-multi-exchange-normalization.md`
8. `chore(docs): deprecate/supersede repetitive audit and patch-plan docs`
   Files: `docs/rfcs/W4-W5-AUDIT.md`, `docs/rfcs/ADR-REVISIONS-patch-plan.md`

### R2) Truth anchors to add first

- `docs/architecture/TRUTH-MAP.md` should be linked from:
  - `docs/rfcs/EXECUTION-SEQUENCE.md`
  - `docs/prd/PRD-0001-extreme-runtime.md` (next round)
- Every gate item should point to concrete command anchors:
  - `Makefile:123`, `Makefile:136`, `Makefile:139`, `Makefile:142`.

### R3) Header/tag standardization target

Apply to docs touched in W12/W13:
- `Purpose`
- `Invariants`
- `Evidence`
- `Acceptance`
- `Changelog`

## Risks and Mitigation

| Risk | Severity | Mitigation |
|---|---|---|
| Renaming ubiquitous terms can break historical traceability. | P1 | Keep old term in parentheses for one cycle; add explicit migration note in changelog. |
| Moving docs can break cross-links. | P1 | Use incremental move + link check in validation; keep compatibility stubs where needed. |
| Status normalization may hide historical meaning (`Done`, `Implemented`). | P2 | Preserve raw status in changelog while normalizing canonical `Status`. |
| Large one-shot doc rewrite risks losing context. | P1 | Keep append-only patches with line-anchored evidence deltas. |

## Acceptance

- Contradictions/lacunas/obsolete docs are enumerated with severity and anchors.
- Every unresolved contradiction is marked `TODO` or `OPEN QUESTION`.
- Patch plan is incremental (5-10 commits) and doc-first.

## Changelog

- 2026-02-13:
  - created W11 drift report with three required sections;
  - added severity-ranked contradictions, gaps, and obsolescence candidates;
  - defined incremental patch sequence and risk mitigation plan for W12/W13 docs.
