---
name: Pr Review
description: Review pull requests against team standards and best practices
phases: [R, V]
---

# PR Review Skill

## Review Checklist
1. Verify architecture boundaries:
- Domain rules stay in `internal/core/*`.
- Actors remain orchestration-focused in `internal/actors/*`.
2. Validate deterministic event behavior:
- Ordering/idempotency assumptions are explicit.
- Envelope contract compatibility remains safe.
3. Validate quality gates:
- `make fmt-check`
- `make lint`
- `make test`
- `make ci VULN_REQUIRED=true` when applicable
4. Confirm docs were updated for workflow, runtime flags, or contracts.

## Required Evidence In PR
- Test output summary or CI link.
- Short risk note for event-flow changes.
- Mention of updated docs/ADRs when design decisions changed.

## Blocker Examples
- Business logic in actors/adapters bypassing domain layer.
- Missing regression tests for bug fixes.
- Silent contract changes in event payload semantics.