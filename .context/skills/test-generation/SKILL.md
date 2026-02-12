---
type: skill
name: Test Generation
description: Generate comprehensive test cases for code
skillSlug: test-generation
phases: [E, V]
generated: 2026-02-12
status: unfilled
scaffoldVersion: "2.0.0"
---

# Test Generation Skill

## Framework And Layout
- Use Go native testing (`go test`).
- Place tests as `*_test.go` beside target package.
- Prefer table-driven tests for domain rules.

## Where To Add Tests
- `internal/core/*`: domain and app behavior.
- `internal/actors/*`: lifecycle and orchestration behavior.
- `internal/adapters/*`: adapter contracts.
- `internal/interfaces/http/*`: API/runtime endpoint behavior.
- `internal/shared/*`: value object and utility correctness.

## Mocking Strategy
- Prefer tiny hand-written fakes over heavy mocking frameworks.
- Keep fake sequencers/publishers deterministic.
- Validate behavior through public interfaces where possible.

## Validation Commands
- `make test-short` for fast iteration.
- `make test` for race + coverage mode.
- `make ci VULN_REQUIRED=true` before merge.
