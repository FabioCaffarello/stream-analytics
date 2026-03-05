---
type: doc
name: development-workflow
description: Standard development practices, branching, and commit processes
category: workflow
generated: 2026-03-05
status: filled
scaffoldVersion: "2.0.0"
---

# Development Workflow

Market Raccoon leverages an industrialized Makefile-driven pipeline and conventional commits. All development must be validated against `IQ Loop` and `invariants-check` before merging.

## Daily Loop (Local Check)

```bash
make quick              # Fast local loop (fmt-check + vet + invariants-check + short tests)
make docs-check-fast    # Lightweight docs guardrails for local loop
make test-unit          # Runs fast short/unit-oriented workspace tests
```

## Pull Request Pipeline

The pipeline is strictly gated. The CI pipeline will automatically run:
```bash
make ci                 # tidy-check + fmt-check + lint + test + vuln + build
```

**Documentation Validation:**
- We follow a highly structural **Doc-First Strategy**. If you alter architecture boundaries, you must amend `docs/architecture/*` or create a new ADR.
- `make docs-check` validates all PRs against internal markdown links and header structure constraints.

## Commit Standards

- Commits must follow **Conventional Commits** (e.g. `feat(core): ...`, `fix(delivery): ...`, `docs(adrs): ...`)
- The `make commit-msg-self-check` runs natively to ensure strings don't contain forbidden legacy data (`make legacy-check-staged`).

## Adding New Features / Run-time

- Always follow the PREVC plan strategy.
- Changes must pass the sub-minute and cold-path runbooks: `make soak-check`, `make subminute-rollout-gate`.
