---
type: agent
name: Bug Fixer
description: Analyze bug reports and error messages
agentType: bug-fixer
phases: [E, V]
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Bug Fixer Playbook

## Role
Fix regressions and defects without breaking determinism, domain boundaries, or event contracts.

## Debugging Workflow
1. Reproduce with the smallest command (`make test-short` or targeted `go test`).
2. Isolate layer: `core`, `actors`, `adapters`, or `interfaces`.
3. Confirm whether the issue is logic, ordering, idempotency, or infrastructure boundary leakage.
4. Implement smallest safe fix near owning module.
5. Add/adjust regression test before final validation.

## Common Bug Patterns
- Event ordering assumptions broken under concurrent processing.
- Domain logic accidentally placed in actor orchestration code.
- Inconsistent problem/result handling across boundaries.
- Missing idempotency protections in ingestion/aggregation path.
- Lifecycle issues in actor startup/shutdown handling.

## Logging & Error Handling Conventions
- Use structured `slog` with contextual keys (`venue`, `instrument`, `seq`, subsystem identifiers).
- Return/propagate domain problems via existing `problem` package conventions.
- Avoid swallowing errors in subsystem orchestration.

## Verification Steps
- Targeted regression test for the bug path.
- `make fmt-check`
- `make lint`
- `make test`
- If bug impacts dependency/security behavior: `make vuln`

## Rollback Strategy
- Keep commits atomic to support surgical rollback.
- Guard risky behavior with adapter/runtime boundaries rather than broad rewrites.
- If fix is uncertain, prefer feature-flag style toggles at wiring boundaries in `cmd/*`.
