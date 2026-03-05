---
name: Pareto Analysis
description: 80/20 analysis to identify highest-impact items from a scoped list
phases: [P]
---

# Pareto Analysis (80/20)

## When to Use
- Prioritizing backlog, tech debt, features, or risks.
- Deciding where effort yields disproportionate value.
- Scoping a milestone or PRD to the vital few.

## Instructions

### 1. Define Scope
State the domain being analyzed (e.g., "storage tech debt", "delivery features", "operational gaps").

### 2. Enumerate Items
List all candidate items. Each item needs:
- **ID**: short identifier
- **Description**: one line
- **Impact**: H / M / L (value delivered or risk removed)
- **Effort**: H / M / L (cost to implement)

### 3. Score & Rank
| Impact \ Effort | Low Effort | Medium Effort | High Effort |
|-----------------|-----------|--------------|------------|
| High Impact     | **DO FIRST** | DO NEXT | PLAN |
| Medium Impact   | DO NEXT | EVALUATE | DEFER |
| Low Impact      | MAYBE | DEFER | DROP |

### 4. Cut Line
Select top ~20% items that deliver ~80% of value. Mark as **Pareto Set**.

### 5. Output Format

```markdown
## Pareto Analysis: {scope}
Date: YYYY-MM-DD

### Pareto Set (top 20%)
| ID | Description | Impact | Effort | Action |
|----|-------------|--------|--------|--------|

### Deferred
| ID | Description | Reason |
|----|-------------|--------|

### Decision
{One paragraph: why this cut, what it unlocks.}
```

## Integration
- Output feeds `write-prd` or `milestone-plan` skills.
- Link result in `.context/plans/` or `.context/evidence/`.