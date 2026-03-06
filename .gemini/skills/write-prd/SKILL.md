---
name: Write PRD
description: Create a Product Requirements Document with milestones and success criteria
phases: [P]
---

# Write PRD

## When to Use
- Defining a new capability or major evolution.
- Translating Pareto/SWOT output into actionable scope.
- Establishing acceptance criteria before implementation plans.

## Instructions

Produce a markdown file in `docs/prds/PRD-XXXX-{slug}.md`.

### Template

```markdown
# PRD-XXXX — {Title}

**Status:** Draft | Approved | Superseded
**Date:** YYYY-MM-DD
**Owner:** {role or person}
**Relates to:** {ADRs, RFCs, plans}

## Problem
{2-3 sentences. What pain or gap exists.}

## Goals
1. {Measurable outcome}
2. {Measurable outcome}

## Non-Goals
- {Explicitly excluded scope}

## Requirements

### Functional
| ID | Requirement | Priority | Acceptance Criteria |
|----|-------------|----------|-------------------|

### Non-Functional
| ID | Requirement | Metric |
|----|-------------|--------|

## Milestones
| Milestone | Deliverables | Gate | Target |
|-----------|-------------|------|--------|

## Risks
| Risk | Impact | Mitigation |
|------|--------|-----------|

## Success Metrics
- {Metric}: {target value}

## References
- {links to ADRs, RFCs, evidence}
```

### Rules
- One PRD per capability. No mega-PRDs.
- Every functional requirement has acceptance criteria.
- Milestones must have gates (what must pass before moving on).
- Risks come from SWOT output when available.

## Integration
- PRD milestones feed `milestone-plan` skill.
- Requirements feed `feature-breakdown` skill.
- Place in `docs/prds/` directory.
