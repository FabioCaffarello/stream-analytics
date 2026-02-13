---
status: filled
generated: 2026-02-13
agents:
  - type: "documentation-writer"
    role: "Normalize ADR/RFC structure and cross-links"
  - type: "code-reviewer"
    role: "Check contradictions between docs and code/test evidence"
  - type: "test-writer"
    role: "Validate gate commands and map test anchors"
docs:
  - "TRUTH-MAP.md"
  - "DRIFT-REPORT-W11.md"
  - "EXECUTION-SEQUENCE.md"
  - "system-invariants.md"
phases:
  - id: "phase-p"
    name: "Planning & Selection"
    prevc: "P"
  - id: "phase-r"
    name: "Review Truth vs Planned"
    prevc: "R"
  - id: "phase-e"
    name: "Documentation Patches"
    prevc: "E"
  - id: "phase-v"
    name: "Validation Gates"
    prevc: "V"
  - id: "phase-c"
    name: "Confirmation & Handoff"
    prevc: "C"
---

# Doc Governance Normalization (PREVC) Plan

> Objective: normalize ADR/RFC governance format (status, scope, terminology, cross-links), remove structural drift, and keep docs aligned to executable truth without runtime architecture changes.

## Goal and Scope
- Normalize 3 ADRs with immediate drift risk: ADR-0016, ADR-0018, ADR-0017.
- Normalize 2 RFCs with immediate governance drift: EXECUTION-SEQUENCE and RFC-0001 (roadmap authority), plus align references from TRUTH-MAP/DRIFT-REPORT.
- Rebuild `docs/architecture/system-invariants.md` as live invariant index with links to ADRs and test/gate anchors.
- Add mandatory sections for selected documents:
  - ADR: Contexto, Decisão, Consequências, Invariantes, Evidence, Changelog.
  - RFC: Objetivo, Escopo, Não-Escopo, Design, Rollout, Test Plan, Acceptance, Risks.
- Explicitly mark partial docs as `Status: Partially Implemented` and include `Implementation Matrix`.
- Validate with workspace-safe commands only.

Out of scope:
- Runtime code refactor.
- Architectural behavior changes not already implemented/planned in existing ADR/RFC/PRD.

## Selected Documents
- `docs/adrs/ADR-0016-protobuf-contract-layer.md`
- `docs/adrs/ADR-0018-actor-topology-supervision-model.md`
- `docs/adrs/ADR-0017-multi-exchange-normalization.md`
- `docs/rfcs/EXECUTION-SEQUENCE.md`
- `docs/rfcs/RFC-0001-robustness-roadmap.md`
- `docs/architecture/system-invariants.md`

## PREVC Phases

### P — Planning & Selection
1. Use `docs/architecture/TRUTH-MAP.md` and `docs/audits/DRIFT-REPORT-W11.md` to select docs with highest drift.
2. Define compact Doc Contract templates for ADR and RFC.
3. Record chosen docs and patch constraints.

Checkpoint commit:
- `docs(governance): define doc contract templates and patch scope`

### R — Review (True vs Planned)
1. For each selected doc, map:
- True now: code/test anchors in `internal/**`, `cmd/**`, `Makefile`, `scripts/**`.
- Planned: PRD/RFC/ADR forward scope.
2. Identify contradictions and reconciliation approach preserving history.

Checkpoint commit:
- `docs(governance): reconcile truth-vs-plan matrices for selected docs`

### E — Execution (Patches)
1. Patch selected ADRs/RFCs with normalized header:
- `Status`, `Owner`, `Last updated`.
2. Add `Evidence` and `Implementation Matrix` sections.
3. Update `docs/architecture/system-invariants.md` as live index.
4. Update `docs/rfcs/EXECUTION-SEQUENCE.md` for real gates and no ghost checklist.

Checkpoint commits:
- `docs(adr): normalize ADR-0016 ADR-0017 ADR-0018 governance contract`
- `docs(rfc): normalize EXECUTION-SEQUENCE and RFC-0001 governance contract`
- `docs(governance): rebuild system-invariants live index`

### V — Validation
1. Run:
- `make invariants-check`
- `make test-workspace`
- `make test-workspace-race`
2. Re-check patched docs against TRUTH-MAP anchors.

Checkpoint commit:
- `docs(governance): validate governance docs against workspace gates`

### C — Confirmation
1. Provide per-doc diff summary with rationale.
2. Provide open questions (max 10).
3. Provide next docs for normalization wave.

## Success Criteria
- Selected ADRs and RFCs comply with mandated sections.
- Partial implementation status is explicit where applicable.
- `system-invariants.md` becomes the canonical invariants index with doc/test links.
- Validation commands complete successfully.
- No patched statement contradicts TRUTH-MAP.

## Risks and Mitigation
- Risk: Overwriting historical context in older RFC prose.
- Mitigation: Keep historical notes under `Changelog`/`History` sections instead of deleting.

- Risk: Evidence links become stale.
- Mitigation: Prefer stable file:test/file:symbol anchors already used in W11 docs.

- Risk: Status normalization hides uncertainty.
- Mitigation: Use explicit `Partially Implemented` with matrix row-level status.

## Evidence Artifacts
- `docs/architecture/TRUTH-MAP.md`
- `docs/audits/DRIFT-REPORT-W11.md`
- `Makefile`
- `scripts/check-domain-isolation.sh`
- `internal/actors/runtime/guardian_test.go`
- `internal/shared/contracts/authority_test.go`
- `internal/shared/codec/payload_codec_test.go`
- `cmd/consumer/e2e_consumer_integration_test.go`
- `internal/adapters/jetstream/*_integration_test.go`
