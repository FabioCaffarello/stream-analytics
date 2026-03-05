---
name: Feature Breakdown
description: Break down features into implementable tasks
phases: [P]
---

# Feature Breakdown Skill

## Decomposition Model
Break every feature into five slices:
1. Domain behavior (`internal/core/*/domain`).
2. Use-case orchestration (`internal/core/*/app`).
3. Infrastructure adapters (`internal/adapters/*`).
4. Runtime wiring (`internal/actors/*` and `cmd/*`).
5. Tests and documentation updates.

## Estimation Guidance
- Small: one bounded context, no contract change.
- Medium: multiple packages plus new tests/docs.
- Large: contract evolution, orchestration updates, rollout/risk plan.

## Dependency Checklist
- Event envelope/version impacts.
- Actor guardian factory registration.
- Interface/API behavior changes.
- CI and tooling requirements.

## Output Format
For each task provide:
- owner role
- target files/packages
- acceptance criteria
- verification commands