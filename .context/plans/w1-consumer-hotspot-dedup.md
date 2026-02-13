---
status: filled
generated: 2026-02-13
owner: release-engineering
---

# W1 Consumer Hotspot Dedup

## Objective
Remove replay-golden overlap from `test-integration` while preserving strict replay coverage in `test-replay-golden`.

## Commit chain
- D1 (single commit): adjust integration test regex to exclude replay-golden selector.

## Files
- `Makefile`

## Gates
- Before: `make test-integration`, `make test-replay-golden`
- After: `make test-integration`, `make test-replay-golden`, `make ci-local`

## Stop conditions
1. `test-integration` misses expected non-replay integration suites.
2. `test-replay-golden` no longer runs replay golden selectors.
3. `ci-local` fails due to missing replay coverage.

## Rollback
- `git revert <hash>` for D1.
