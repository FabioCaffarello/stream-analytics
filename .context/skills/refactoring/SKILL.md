---
type: skill
name: Refactoring
description: Safe code refactoring with step-by-step approach
skillSlug: refactoring
phases: [E]
generated: 2026-02-12
status: unfilled
scaffoldVersion: "2.0.0"
---

# Refactoring Skill

## Safe Refactor Procedure
1. Establish baseline with existing tests.
2. Apply one structural concern per change.
3. Keep public/inter-module contracts stable unless migration is intentional.
4. Run validation after each step.

## Common Refactor Targets
- Reduce duplication across bounded contexts.
- Improve package boundaries and naming clarity.
- Move misplaced logic from actors/adapters into `internal/core/*`.

## Guardrails
- Do not combine refactor and feature behavior changes without clear separation.
- Preserve determinism in event processing paths.
- Keep commit history reviewable and reversible.

## Post-Refactor Validation
- `make fmt-check`
- `make lint`
- `make test`
