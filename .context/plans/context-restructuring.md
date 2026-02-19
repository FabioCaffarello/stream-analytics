---
status: filled
generated: 2026-02-17
agents:
  - type: "documentation-writer"
    role: "Execute context restructuring: delete garbage, archive obsoletes, normalize docs"
  - type: "code-reviewer"
    role: "Validate make docs-check passes after each commit"
docs:
  - "project-overview.md"
  - "truth-pack.md"
  - "development-workflow.md"
  - "testing-strategy.md"
  - "tooling.md"
phases:
  - id: "phase-1"
    name: "Cleanup — Delete garbage, archive obsoletes"
    prevc: "P"
    agent: "documentation-writer"
  - id: "phase-2"
    name: "Normalize — AGENTS.md, STATUS headers, 00-START-HERE"
    prevc: "E"
    agent: "documentation-writer"
  - id: "phase-3"
    name: "Sync — Claude + Codex export, workflow validation"
    prevc: "V"
    agent: "code-reviewer"
---

# Context Engineering Restructuring Plan

> Single source of truth per tema, remover scaffolding garbage, adicionar STATUS headers, criar 00-START-HERE, sync Claude+Codex

## Task Snapshot
- **Primary goal:** Consolidar .context/ como fonte primária para IA com zero duplicação e navegação < 2 min
- **Success signal:** `make docs-check` green + 00-START-HERE.md funcional + agents synced para Claude/Codex
- **Key references:**
  - [Truth Pack](../docs/truth-pack.md)
  - [TRUTH-MAP](../../docs/architecture/TRUTH-MAP.md)
  - [Agent Handbook](../agents/README.md)

## Agent Lineup
| Agent | Role | Playbook |
| --- | --- | --- |
| Documentation Writer | Execute all doc changes | [Playbook](../agents/documentation-writer.md) |
| Code Reviewer | Validate gates pass | [Playbook](../agents/code-reviewer.md) |

## Working Phases

### Phase 1 — Cleanup (Commits 1-3)

**Objective:** Remove template garbage, duplicates, archive obsoletes

| # | Task | Status | Deliverable |
|---|------|--------|-------------|
| 1.1 | Delete .context/docs/qa/ (6 files) + codebase-map.json | pending | 7 files removed |
| 1.2 | Fix refs in README.md, project-overview.md, truth-pack.md | pending | No broken links |
| 1.3 | Delete docs/architecture/vpvr-overload-runbook.md (duplicate) | pending | SoT at observability/runbooks/ |
| 1.4 | Archive 3 historical RFCs with supersession notes | pending | W4-W5-AUDIT, W5.1-SWEEP, ADR-REVISIONS |
| 1.5 | Update TRUTH-MAP inventory with ARCHIVED markers | pending | Consistent inventory |

**Gate:** `make docs-check` after each commit

---

### Phase 2 — Normalize (Commits 4-6)

**Objective:** Fix AGENTS.md, add STATUS headers, create 00-START-HERE

| # | Task | Status | Deliverable |
|---|------|--------|-------------|
| 2.1 | Rewrite AGENTS.md (npm→Go) | pending | Correct Go/Make content |
| 2.2 | Add STATUS: ACTIVE to 14 .context/ docs + TRUTH-MAP | pending | All docs have status |
| 2.3 | Create 00-START-HERE.md | pending | Navigation < 2 min |
| 2.4 | Create workflow/archive/README.md | pending | Agent guard |

**Gate:** `make docs-check` after each commit

---

### Phase 3 — Sync Claude + Codex

**Objective:** Export rules and agents for multi-tool AI support

| # | Task | Status | Deliverable |
|---|------|--------|-------------|
| 3.1 | sync-agents --preset claude | pending | .claude/agents/ populated |
| 3.2 | export-rules --preset claude | pending | Claude rules exported |
| 3.3 | export-rules --preset codex | pending | Codex rules exported |
| 3.4 | Verify with workflow advance | pending | Workflow completed |

## Summary

| Action | Count | Files |
|--------|-------|-------|
| DELETE | 8 | qa/* (6), codebase-map.json (1), vpvr-overload-runbook.md (1) |
| ARCHIVE | 3 | W4-W5-AUDIT, W5.1-SWEEP, ADR-REVISIONS-patch-plan |
| CREATE | 2 | 00-START-HERE.md, workflow/archive/README.md |
| FIX/UPDATE | 17 | AGENTS.md, truth-pack, project-overview, README, TRUTH-MAP, 13 STATUS headers |
| SYNC | 3 | agents (Claude), rules (Claude), rules (Codex) |
