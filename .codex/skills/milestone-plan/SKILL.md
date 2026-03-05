---
name: Milestone Plan
description: Break a PRD or RFC into gated milestones with deliverables and dependencies
phases: [P]
---

# Milestone Plan

## When to Use
- Converting PRD milestones or RFC waves into executable plans.
- Defining a `.context/plans/` entry linked to PREVC workflow.
- Establishing dependency chains between work items.

## Instructions

### Input
One of:
- PRD milestone table
- RFC rollout table
- Pareto set output

### Template

```markdown
---
status: pending
progress: 0
generated: YYYY-MM-DD
title: {Plan Title}
owner: {role}
workflow: PREVC
phase: P
---

# {Plan Title}

> {One line: goal of this milestone.}

## Scope
- {Deliverable 1}
- {Deliverable 2}

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| {ADR/RFC/plan} | blocks / informs | {done/pending} |

## Phases

### P — Plan
- [ ] Scope confirmed
- [ ] ADRs/RFCs referenced
- [ ] Pareto analysis linked (if applicable)

### R — Review
- [ ] Design reviewed
- [ ] Contracts validated

### E — Execute
- [ ] {Deliverable 1 implementation}
- [ ] {Deliverable 2 implementation}
- [ ] Tests pass: `{make target}`

### V — Validate
- [ ] Gate: `{make target or CI check}`
- [ ] Evidence captured in `.context/evidence/`

### C — Complete
- [ ] Docs updated
- [ ] Plan status → completed

## Acceptance Criteria
1. {Criterion with verification command}
2. {Criterion with verification command}

## Risks
| Risk | Mitigation |
|------|-----------|
```

### Rules
- One plan per milestone. Chain plans via Dependencies table.
- Every E phase item maps to a bounded context or module.
- Gate commands must be runnable (`make test`, `make lint`, etc).
- Link plan to workflow: `plan({ action: "link", planSlug: "..." })`.

## Integration
- Plans are created in `.context/plans/`.
- Linked to PREVC via `workflow-init` + `plan link`.
- E phase items feed `feature-breakdown` skill for task decomposition.