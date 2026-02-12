---
type: skill
name: Commit Message
description: Generate commit messages following conventional commits with scope detection
skillSlug: commit-message
phases: [E, C]
generated: 2026-02-12
status: unfilled
scaffoldVersion: "2.0.0"
---

# Commit Message Skill

## Convention
Use Conventional Commits with optional scope:
- `feat(scope): ...`
- `fix(scope): ...`
- `refactor(scope): ...`
- `test(scope): ...`
- `docs(scope): ...`
- `chore(scope): ...`

## Scope Guidance
Pick scope from affected bounded context or layer:
- `marketdata`, `aggregation`, `delivery`, `insights`
- `actors`, `adapters`, `interfaces`, `shared`, `cmd`, `docs`, `context`

## Message Quality Rules
- Subject in imperative mood and <= 72 chars when possible.
- Explain behavioral intent, not only file movement.
- If contract/runtime behavior changed, include a body with rationale.

## Good Examples For This Repo
- `feat(marketdata): ingest normalized trade envelopes from ws feed`
- `fix(aggregation): enforce monotonic seq check per venue instrument`
- `refactor(actors): isolate guardian factory wiring from startup path`
- `test(shared): add regression tests for envelope decode edge cases`
- `docs(context): align tooling guide with make ci pipeline`

## SemVer Notes
- `feat` implies minor-level change potential.
- `fix` implies patch-level change potential.
- Breaking changes require `!` or `BREAKING CHANGE:` footer.
