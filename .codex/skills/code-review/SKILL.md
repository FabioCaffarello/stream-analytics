---
name: Code Review
description: Review code quality, patterns, and best practices
phases: [R, V]
---

# Code Review Skill

## What To Inspect First
- Changed files under `cmd/`, `internal/core/`, `internal/actors/`, `internal/adapters/`, `internal/interfaces/`.
- Cross-boundary imports that may break bounded context isolation.

## Project-Specific Quality Rules
- Keep core behavior deterministic and replay-safe.
- Preserve `problem` and `result` handling consistency from `internal/shared`.
- Prefer explicit error paths over hidden log-only failures.
- Avoid introducing blocking operations in hot actor message loops.

## Security And Performance Checks
- Validate input handling at interface boundaries.
- Check for unbounded memory growth in ingestion/aggregation loops.
- Confirm no accidental secrets or unsafe defaults in `cmd/*`.

## Minimum Review Output
- Findings ordered by severity.
- File references for each finding.
- Residual risks when tests/coverage are limited.
