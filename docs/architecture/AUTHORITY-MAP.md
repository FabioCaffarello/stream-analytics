# Authority Map — Document Governance Domains

**Status:** Active
**Date:** 2026-02-19
**Last reviewed:** 2026-02-19
**Relates to:** `docs/architecture/TRUTH-MAP.md`, `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`

---

## Purpose

Define which documents are authoritative per governance domain.
Every claim in the codebase must anchor to exactly one authority per domain.

## Authority Domains

### (a) Acceptance — Product Requirements

| Authority | Scope | Path |
|---|---|---|
| **PRD-0001** | Program baseline: SLOs, invariants, execution timeline (W1-W14) | `docs/prd/PRD-0001-extreme-runtime.md` |
| **PRD-0002** | Backend stable + Odin-ready acceptance criteria (Gates, Release Checklist) | `docs/prd/PRD-0002-backend-stable-and-odin-ready.md` |

Rule: PRDs define **what** must be true. RFCs/ADRs define **how**. When in conflict, PRDs win.

### (b) Governance — Truth Map & Execution Sequence

| Authority | Scope | Path |
|---|---|---|
| **TRUTH-MAP** | Single source of truth per critical theme; doc inventory; code/test anchors | `docs/architecture/TRUTH-MAP.md` |
| **EXECUTION-SEQUENCE** | W4-W14 delivery timeline, gate authority, implementation matrix | `docs/rfcs/EXECUTION-SEQUENCE.md` |
| **System Invariants** | Living index of runtime invariants + validation gates | `docs/architecture/system-invariants.md` |

Rule: TRUTH-MAP owns the "which doc is authoritative" question. EXECUTION-SEQUENCE owns "what is delivered and what is planned".

### (c) Parity — MarketMonkey Feature Alignment

| Authority | Scope | Path |
|---|---|---|
| **RFC-0011** | Product parity v1 gap/drift checklist, feature set, implementation matrix | `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` |
| **Feature Packs** | Per-feature status, invariants, acceptance tests | `.context/docs/feature-packs/*.md` |

Rule: RFC-0011 defines the parity roadmap. Feature packs track per-feature progress. PRD-0002 overrides scope (Non-Goals take precedence over RFC-0011 TODO items).

### (d) Wire Contracts — Event Bus & Delivery

| Authority | Scope | Path |
|---|---|---|
| **Event Bus Contract** | Envelope schema, subject taxonomy, versioning rules | `docs/contracts/event-bus.md` |
| **Subject Registry** | Subject inventory, status (stable/draft/planned), schema authority | `docs/contracts/subject-registry.yaml` |
| **Delivery WS Contract** | WS wire format, client commands, backpressure, proto opt-in | `docs/contracts/delivery-ws.md` |
| **ADR-0002** | Envelope versioning decision | `docs/adrs/ADR-0002-event-envelope-and-versioning.md` |
| **ADR-0016** | Protobuf contract layer decision | `docs/adrs/ADR-0016-protobuf-contract-layer.md` |

Rule: `event-bus.md` + `subject-registry.yaml` are the wire truth. ADRs record decisions. When runtime diverges from docs, docs must be patched (not the reverse).

### (e) Operations & Observability

| Authority | Scope | Path |
|---|---|---|
| **SLO Definitions** | SLO/SLI targets, PromQL burn-rate expressions | `docs/observability/slo.md` |
| **Metrics Policy** | Label budget, cardinality caps | `docs/observability/metrics-policy.md` |
| **Runbooks** | Per-alert diagnosis and mitigation | `docs/observability/runbooks/*.md` |
| **Degradation Contract** | Storage-induced backpressure scenarios | `docs/operations/degradation.md` |
| **Sharding Guide** | Static shard-range consumers, rebalance procedure | `docs/operations/sharding.md` |
| **Cold-Path Runbook** | Store binary pipeline, batch settings, troubleshooting | `docs/operations/cold-path-runbook.md` |
| **Performance Budgets** | Latency/memory/goroutine budgets + soak evidence | `docs/perf/performance-budgets.md` |
| **Alert Rules** | Active Prometheus alert definitions | `deploy/observability/prometheus/alerts.rules.yml`, `shard-alerts.rules.yml` |

Rule: `slo.md` owns targets. `performance-budgets.md` owns measurement methodology. PRD-0001 is final arbiter when SLO targets conflict.

## Validation Gate

This map is validated by `make docs-check` which runs:
- `scripts/ci/check-doc-headers.sh`
- `scripts/ci/check-doc-links.sh`
- `scripts/ci/check-truth-map.sh`
- `scripts/ci/check-feature-pack-links.sh`
- `scripts/ci/check-pack-subjects-vs-event-bus.sh`
- `scripts/ci/check-registry.sh`

## Changelog

- 2026-02-19: Initial creation as part of context-calibration audit (PRD-0002 alignment).
