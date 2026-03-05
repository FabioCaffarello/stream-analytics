---
type: skill
name: Write ADR
description: Create an Architecture Decision Record following project convention
skillSlug: write-adr
phases: [P, R]
generated: 2026-03-05
status: filled
scaffoldVersion: "2.0.0"
---

# Write ADR for Market Raccoon

## When to Use
- Recording an architectural decision (new or changed).
- Formalizing a pivot away from an existing Invariant or Subsystem rule.

## Strict Instructions

### 1. File Placement & Naming
Check `docs/adrs/` for the highest existing `ADR-00XX` number. Create your file as `docs/adrs/ADR-00XY-{slug}.md`.

### 2. Mandatory Template
```markdown
# ADR-00XY — {Title}

**Status:** Proposed | Accepted | Superseded by ADR-00ZZ
**Date:** YYYY-MM-DD
**Owners:** Core Engineering
**Relates to:** {List of affected docs like `docs/architecture/subsystems.md`, `ADR-0005`, etc}

## Context
{Why is this decision needed? Reference metrics, domain invariants like INV-DOM or IQ Loop validation.}

## Decision
{What was decided. Must be extremely prescriptive. Explain the mechanism, the subsystem, and the bounds.}

## Consequences
- **Positive:** {Benefits}
- **Negative:** {Trade-offs, performance impacts, backpressure limits}

## Alternatives
{What else was considered and why it was rejected.}
```

### 3. The Enabler Step (CRITICAL)
Whenever you create or modify an ADR, you MUST also execute these two actions to prevent context drift:
1. Append a summary of this ADR into `docs/architecture/decisions.md` under its relevant domain table.
2. Update the system registry at `docs/architecture/TRUTH-MAP.md` if this ADR shifts an authority boundary.

### Rules
- Never delete superseded ADRs. Mark them as `Superseded by ADR-XYZ`.
- Ensure changes pass `make docs-check`.
