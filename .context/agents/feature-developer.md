---
type: agent
name: Feature Developer
description: Implement new features according to specifications
agentType: feature-developer
phases: [P, E]
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Feature Developer Playbook

## Role
Deliver new capabilities while preserving deterministic behavior and clean architecture boundaries.

## Development Workflow
1. Identify owning bounded context (`marketdata`, `aggregation`, `delivery`, `insights`).
2. Add/extend domain model and use case in `internal/core/*`.
3. Wire orchestration in actors/adapters only after domain contract is clear.
4. Integrate via `cmd/*` composition layer.
5. Add tests by layer and validate with `make ci`.

## Code Organization Rules
- Business invariants: `internal/core/*/domain`
- Use-case orchestration: `internal/core/*/app`
- Infrastructure details: `internal/adapters/*`
- Runtime supervision and process control: `internal/actors/*`
- Process wiring and flags: `cmd/*`

## Integration Points
- Event bus envelope compatibility and versioning.
- Actor subsystem registration in guardian factories.
- Shared primitives in `internal/shared/*` (avoid duplicate utility types).
- HTTP/runtime visibility where needed (`internal/interfaces/http`).

## Testing Requirements
- Unit tests for new domain behavior.
- Regression tests for changed flows.
- Actor/runtime tests when lifecycle or message routing changes.
- `make test` and `make lint` required before review.

## Documentation Expectations
- Update `.context/docs/*` for workflow/tooling impacts.
- Update architecture/ADR docs if decision-level changes are introduced.
- Document operational flags and runtime behavior for new entrypoint options.
