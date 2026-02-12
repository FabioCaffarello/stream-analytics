# Insights Philosophy

## Purpose

Convert market structure into actionable clarity without providing execution directives.

---

## Non-Negotiable Rules

The system NEVER:

- issues buy/sell commands
- recommends entries
- guarantees outcomes

---

## Insight Structure

```text
Insight {
    type
    confidence
    evidence[]
    window
    venues[]
    invalidation_conditions[]
}

```

---

## Evidence Requirement

Every insight must explain:

- what was detected
- why it matters
- what could invalidate it

No black boxes.

---

## Audit Trail

All generated insights must be reproducible from the event stream.

If we cannot replay it,
we cannot trust it.

---
