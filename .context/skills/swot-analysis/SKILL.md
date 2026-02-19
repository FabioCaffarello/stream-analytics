---
type: skill
name: SWOT Analysis
description: Structured strengths/weaknesses/opportunities/threats assessment
skillSlug: swot-analysis
phases: [P]
generated: 2026-02-19
status: filled
scaffoldVersion: "2.0.0"
---

# SWOT Analysis

## When to Use
- Evaluating a bounded context, subsystem, or architectural direction.
- Comparing approaches before an ADR.
- Assessing competitive position (e.g., MarketMonkey parity).

## Instructions

### 1. Define Subject
One line: what is being assessed and from whose perspective.

### 2. Fill Quadrants
Each quadrant: 3-5 bullet points max. No prose.

| Internal | |
|----------|---|
| **Strengths** (assets, patterns, capabilities) | **Weaknesses** (gaps, debt, constraints) |

| External | |
|----------|---|
| **Opportunities** (unlocks, market, integrations) | **Threats** (risks, competition, dependencies) |

### 3. Implications Matrix
Cross each S/W with each O/T:

| | Opportunity 1 | Threat 1 |
|---|---|---|
| **Strength 1** | Leverage: {action} | Defend: {action} |
| **Weakness 1** | Invest: {action} | Mitigate: {action} |

### 4. Output Format

```markdown
## SWOT: {subject}
Date: YYYY-MM-DD

### Quadrants
(table above)

### Key Implications
1. {implication → action}
2. {implication → action}
3. {implication → action}

### Recommended Next Step
{One line: what artifact to produce next (ADR, RFC, PRD, plan).}
```

## Integration
- Output feeds `write-adr` (if architectural choice) or `write-prd` (if product direction).
- Link result in `.context/evidence/`.
