---
type: doc
name: testing-strategy
description: Testing approach, quality gates, and verification workflow for contributors
category: quality
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Testing Strategy

Testing in this repository protects deterministic event processing, actor supervision behavior, and domain invariants across bounded contexts.

## Testing Layers
- Unit tests for shared primitives and value objects (`internal/shared/*`).
- Domain/application tests for core logic (`internal/core/*`).
- Runtime/actor tests for orchestration and subsystem behavior (`internal/actors/*`).
- Interface tests for HTTP surfaces (`internal/interfaces/http/*`).
- Adapter tests for infrastructure behavior (`internal/adapters/*`).

## Standard Commands
- Fast local cycle:
```bash
make test-short
```
- Full suite with race detector and coverage mode:
```bash
make test
```
- Full CI parity (recommended before PR):
```bash
make ci VULN_REQUIRED=true
```

## Quality Gates
`make ci` composes the required gates:
1. `make tidy-check`
2. `make fmt-check`
3. `make lint`
4. `make test`
5. `make vuln`
6. `make build`

## Test Design Principles
- Keep domain behavior deterministic and assertion-oriented.
- Validate event ordering and idempotency where sequence is relevant.
- Prefer narrow, explicit fixtures over broad integration-style implicit state.
- Ensure actor tests verify startup/shutdown/supervision transitions.
- For concurrency-sensitive code, preserve `-race` compatibility.

## Mocking and Fakes
- Prefer small hand-rolled fakes at boundaries (publisher/store/sequencer style) over complex mocking frameworks.
- Use in-memory adapters in tests when validating app orchestration logic.
- Keep fake implementations deterministic (fixed ordering, no random time unless injected).

## Coverage Expectations
There is no hard numeric threshold in CI, but practical expectations are:
- New domain rules require targeted tests.
- Bug fixes require regression tests.
- Changed public behavior in runtime/interfaces requires tests that fail before the fix.

## CI/CD Integration
GitHub Actions workflow (`.github/workflows/ci.yml`) runs:
- Go setup
- Tool installation (`make install-tools`)
- Full pipeline (`make ci VULN_REQUIRED=true`)

This means local `make ci VULN_REQUIRED=true` is the closest reliable pre-PR signal.

## Common Failure Triage
- `tidy-check` failures: run `make tidy`.
- `fmt-check` failures: run `make fmt`.
- Lint failures: address static analysis warnings; avoid disabling linters unless justified.
- Flaky behavior under race mode: inspect shared state and actor message ordering assumptions.
