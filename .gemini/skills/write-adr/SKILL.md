---
name: Write ADR
description: Create an Architecture Decision Record following project convention
phases: [P, R]
---

# Write ADR

## When to Use
- Recording an architectural decision (new or changed).
- Superseding a previous ADR.
- Formalizing a choice surfaced by SWOT or design review.

## Instructions

### Numbering
Next sequential number after highest in `docs/adrs/`. Format: `ADR-XXXX-{slug}.md`.

### Template

```markdown
# ADR-XXXX — {Title}

**Status:** Proposed | Accepted | Superseded by ADR-YYYY
**Date:** YYYY-MM-DD
**Owners:** {roles}

## Context
{Why this decision is needed. Reference PRDs, RFCs, or evidence.}

## Decision
{What we chose. Bullet points for multi-part decisions.}

## Consequences
{What follows. Both positive and negative.}

## Alternatives
{What was considered and why rejected. One line each.}

## Evidence
- Validation gate: {make target or test command}
- Authority path: {file path of this ADR}

## Changelog
- YYYY-MM-DD: initial draft
```

### Rules
- Context must reference concrete evidence (code, metrics, prior ADRs).
- Decision section is prescriptive, not descriptive.
- Alternatives section is mandatory — at minimum "do nothing" must appear.
- Superseded ADRs get a status update, never deleted.

## Integration
- Referenced by PRDs and plans.
- Gate validation tied to `make docs-check`.
- Place in `docs/adrs/`.