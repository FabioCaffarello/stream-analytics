---
name: Bug Investigation
description: Systematic bug investigation and root cause analysis
phases: [E, V]
---

# Bug Investigation Skill

## Investigation Flow
1. Reproduce with smallest failing command/test.
2. Identify owning layer: core, actors, adapters, interfaces.
3. Check invariants: ordering, idempotency, replayability.
4. Isolate root cause and write regression test.
5. Implement smallest safe fix.

## Frequent Patterns In This Repo
- Sequence/order handling issues per `(venue, instrument)`.
- Orchestration code carrying business rules.
- Incomplete propagation of `problem` errors.
- Race conditions in concurrent actor paths.

## Diagnostics Guidance
- Use structured logs with keys like `venue`, `instrument`, `seq`, `subsystem`.
- Trace input envelope shape against `docs/contracts/event-bus.md`.

## Verification
- Targeted test for bug path.
- `make test`
- `make lint`