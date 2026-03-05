---
name: Documentation
description: Generate and update technical documentation
phases: [P, C]
---

# Documentation Skill

## Standards For This Repo
- Documentation must match `Makefile`, `go.work`, and CI workflow.
- Prefer operational steps over generic narrative.
- Link architecture sources in `docs/architecture/` and `docs/adrs/` when relevant.

## Required Update Triggers
- New runtime flags in `cmd/*`.
- Changed contracts or event semantics.
- New quality/tooling commands.
- Refactors that alter ownership boundaries.

## Expected Structure
1. Goal and context.
2. Concrete commands/examples.
3. Risk and validation notes.
4. Cross-references to related docs.

## Quality Bar
- No placeholder TODOs.
- No stale references to non-Go toolchains.
- Clear owner-facing next actions.