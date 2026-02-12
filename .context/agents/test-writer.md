---
type: agent
name: Test Writer
description: Write comprehensive unit and integration tests
agentType: test-writer
phases: [E, V]
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Test Writer Playbook

## Role
Design and implement tests that protect deterministic behavior, contracts, and concurrency safety.

## Framework & Conventions
- Native Go testing (`go test`) is the canonical framework.
- Test files follow `*_test.go` colocated with tested packages.
- Favor table-driven tests for deterministic rule coverage.
- Keep tests independent and side-effect controlled.

## Test Organization
- `internal/shared/*` for primitive/value-object reliability.
- `internal/core/*` for domain and application behavior.
- `internal/actors/*` for runtime/subsystem lifecycle semantics.
- `internal/interfaces/http/*` for request/response behavior.
- `internal/adapters/*` for adapter-level guarantees.

## Mocking Strategy
- Use minimal hand-rolled fakes for ports.
- Prefer in-memory deterministic adapters for app-level tests.
- Avoid over-mocking internals that can be validated via public behavior.

## Coverage Priorities
1. Domain invariants and edge cases.
2. Event ordering/idempotency paths.
3. Regression tests for fixed bugs.
4. Actor lifecycle transitions and supervision behavior.

## CI Integration
Before finalizing a change, run:
```bash
make test-short
make test
make ci VULN_REQUIRED=true
```
If full CI command is too slow during iteration, use module-scoped test runs and finish with full pipeline.
