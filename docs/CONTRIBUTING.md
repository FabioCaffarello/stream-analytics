## Contributing — Tests, Hooks and CI

This repository follows a few simple guidelines for tests and hooks. This document is a living draft — we'll expand as we standardize tags and test tree structure.

Testing categories
- Unit: fast tests that should run with `go test -short`. These run in pre-commit / local quick loops.
- Integration: slower tests that exercise infra or multiple components. Run via `make test-integration` or `make test-integration-changed`.
- E2E / Conformance: full end-to-end scenarios. Run in CI or dedicated environments.
- Soak / Stress: long-running soak tests (naming convention: `soak`, `vpvr`, `store-soak`, `TestStoreSoak_`, etc.). Run via `make soak-check` and related targets.

Test tagging strategy (proposal)
- Prefer test-name prefixes (`TestStoreSoak_`, `TestFooIntegration`) for quick adoption.
- Long-term: add Go build tags for integration/e2e (e.g. `//go:build integration`) and update CI Makefile targets to use `-tags=integration`.
- Use `-short` in unit tests to allow fast local loops.

Pre-commit hooks
- We require a set of local hooks (see `.pre-commit-config.yaml`). Install locally with:

```bash
make pre-commit-install-all
```

- Heavy checks (vuln scans) run at pre-push and can be gated via `VULN_REQUIRED=true`.

Scripts
- **Organization**
- Scripts are organized under `scripts/` with categorized wrappers:
  - `scripts/ci/` — lightweight CI/guard wrappers (called from `Makefile`).
  - `scripts/test/` — test harness wrappers (e.g. `scripts/test/soak/*`).
  - `scripts/util/` — utility helpers (e.g. `list-tests-by-category.sh` wrapper).

- Long-running soaks are invoked via `make soak-*` targets which call the `scripts/test/soak/*` wrappers.
- Use `make list-tests` (calls `scripts/util/list-tests-by-category.sh`) to get a quick inventory of tests by category.

Next steps
- Finalize naming conventions and decide whether to adopt build tags for integration/e2e.
- Sweep test files and add build tags where appropriate.
- Keep the `scripts/legacy/` folder for a short freeze period while teams migrate to the wrapper locations; after validation we can delete or archive it permanently.
