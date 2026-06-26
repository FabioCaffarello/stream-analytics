**Status:** Active
**Owner:** Platform
**Last updated:** 2026-06-25
**Date:** 2026-01-01
**Author:** Platform Team
**Relates to:** [system-invariants](../architecture/system-invariants.md), [TRUTH-MAP](../architecture/TRUTH-MAP.md)

# RFC — Execution Sequence

## Objective

Define the mandatory execution sequence for CI gates, ensuring that the fastest-failing
quality checks run first and that the full gate chain produces consistent results in both
local and CI environments.

## Acceptance

All gates in the sequence below must pass before any PR can be merged.

## Test Plan

Run the full local gate chain:

```bash
make docs-check
make invariants-check
make test-workspace
make lint
make proto-check
```

## Mandatory Gate Order

1. **Documentation governance** — `make docs-check`
   Validates: doc headers, internal links, TRUTH-MAP consistency, subject registry.
   Anchor: `scripts/ci/docs/check-doc-headers.sh`, `scripts/ci/docs/check-doc-links.sh`, `scripts/ci/docs/check-truth-map.sh`

2. **Domain isolation** — `make invariants-check`
   Validates: layer dependency direction, domain isolation, runtime invariant guards.
   Anchor: `scripts/ci/guards/check-domain-isolation.sh`

3. **Unit + integration tests** — `make test-workspace`
   Validates: all modules pass, no regressions.
   Anchor: `scripts/for-each-module.sh`

4. **Lint** — `make lint`
   Validates: golangci-lint, formatting, vet.

5. **Protocol contracts** — `make proto-check`
   Validates: proto lint, drift detection, breaking change check.

## Risks

- Running gates out of order may produce misleading failures (e.g., test failures caused by broken contracts).
- The docs gate must run first because broken links/stale TRUTH-MAP entries indicate doc drift that can mask test coverage gaps.

## Evidence

- Gate scripts: `scripts/ci/docs/`, `scripts/ci/guards/`
- Makefile: `Makefile`
- CI pipeline: `docs/development-workflow.md`

## Changelog

- 2026-01-01: Accepted — execution sequence formalized
