# ADR-0009 — Config & Operations: JSONC Settings + Deterministic Pipelines

**Status:** Accepted  
**Date:** 2026-02-10

## Context

We need human-editable configuration, versioning, and deterministic operational behavior for reproducibility.

## Decision

- Configuration is stored as JSONC (`config.jsonc`) with schema versioning.
- Defaults and validations live in `core/*/domain` and `core/*/app`.
- Runtime uses deterministic pipelines where possible; non-determinism must be explicit (time, randomness, IO).
- All operational stages can emit artifacts/logs suitable for debugging and replay.

## Consequences

- Easier config management and migrations.
- Improved supportability and debugging.

## Alternatives

- YAML only (rejected: JSONC + schema validation is more robust for agents).
