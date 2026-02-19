---
name: Write RFC
description: Create a Request for Comments for cross-cutting design proposals
phases: [P]
---

# Write RFC

## When to Use
- Proposing a design that spans multiple bounded contexts.
- Defining a phased rollout (waves) for a capability.
- When an ADR alone is insufficient to capture design + execution plan.

## Instructions

### Numbering
Next sequential after highest in `docs/rfcs/`. Format: `RFC-XXXX-{slug}.md`.

### Template

```markdown
# RFC-XXXX — {Title}

**Status:** Draft | Accepted | Implemented | Withdrawn
**Owner:** {role}
**Date:** YYYY-MM-DD
**Author:** {name/role}
**Relates to:** {ADRs, PRDs, other RFCs}

---

## Objetivo
{One paragraph: what this RFC achieves.}

## Escopo
- {In-scope items as bullets}

## Nao-Escopo
- {Explicitly excluded}

## Design
{Technical design. Diagrams if needed. Reference existing contracts.}

## Rollout
| Wave | Scope | Output | Gate |
|------|-------|--------|------|

## Riscos
| Risk | Mitigation |
|------|-----------|

## Decisoes Pendentes
- {Open question → resolution path}

## Referencias
- {ADRs, PRDs, code paths}
```

### Rules
- RFCs are proposals; they become Accepted after review.
- Every wave must have a gate (CI target, test, or evidence).
- Keep Design section focused — implementation details go to code.
- Cross-reference ADRs for atomic decisions within the RFC scope.

## Integration
- RFC waves feed `milestone-plan` skill.
- Decisions within RFC may spawn ADRs via `write-adr`.
- Place in `docs/rfcs/`.
