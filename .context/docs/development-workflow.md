---
type: doc
name: development-workflow
description: Day-to-day engineering processes, branching, and contribution guidelines
category: workflow
generated: 2026-02-12
status: filled
docStatus: ACTIVE
last_reviewed: "2026-02-17"
scaffoldVersion: "2.0.0"
---

# Development Workflow

This repository follows a deterministic workflow centered on Go workspaces, Make targets, and CI parity.

## Branching & Releases
- Main integration branch: `main`.
- Feature/fix branches should be short-lived and focused on a single concern.
- Commit messages should follow Conventional Commits (enforced by pre-commit commit-msg hook script).
- CI runs on `push` to `main` and all pull requests (`.github/workflows/ci.yml`).
- Release cadence is currently commit-driven; binaries are built from `cmd/*` through `make build`.

## Local Development
- Install required tools:
```bash
make install-tools
```
- List workspace modules:
```bash
make modules
```
- Keep module dependencies tidy:
```bash
make tidy
make tidy-check-changed
```
- Format and lint:
```bash
make fmt
make fmt-check
make lint-changed
```
- Run tests:
```bash
make test
make test-short-changed
```
- Run legacy guards:
```bash
make legacy-check-staged
make legacy-check
```
- Run vulnerability scan and full CI-equivalent pipeline:
```bash
make vuln
make ci
```
- Build and run binaries:
```bash
make build
make run APP_CMD=./cmd/server
make run APP_CMD=./cmd/consumer
make run APP_CMD=./cmd/processor
```

## Code Review Expectations
Reviewers should block merges that violate system invariants or reduce determinism.

Required review checks:
- Domain logic remains in `internal/core/*`; actors coordinate, they do not own business rules.
- Event contracts remain versioned and backward-safe (`docs/contracts/event-bus.md`).
- Ordering/idempotency semantics remain explicit for `(venue, instrument)` flows.
- Changes include or update tests close to modified behavior.
- `make ci` passes locally or deviations are explained in PR notes.

## Onboarding Tasks
1. Read architecture docs in `docs/architecture/` and ADRs in `docs/adrs/`.
2. Run `make modules` and inspect module boundaries from `go.work`.
3. Run `make tidy-check-changed && make lint-changed && make test-short-changed` first, then `make ci`.
4. Explore binary entrypoints in `cmd/` to understand runtime responsibilities.

## Cross-References
- [Testing Strategy](./testing-strategy.md)
- [Tooling](./tooling.md)
