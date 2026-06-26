**Status:** Accepted
**Owner:** Architecture
**Last updated:** 2026-06-25
**Date:** 2026-01-01
**Deciders:** Platform Team
**Relates to:** [system-invariants](../architecture/system-invariants.md), [AUTHORITY-MAP](../architecture/AUTHORITY-MAP.md)

# ADR-0000 — Foundation

## Context

Stream Analytics requires a stable architectural foundation: a dependency direction,
bounded-context ownership model, and time-handling contract that all future decisions can
reference. This ADR establishes those primitives.

## Decision

1. **Layer dependency direction:** `cmd → interfaces → actors → adapters → core → shared`. No upward imports.
2. **Bounded contexts:** 12 contexts own their domain types. Cross-context communication via versioned event contracts only.
3. **Time:** All time-dependent code uses `clock.Clock`. Direct `time.Now()` is banned in `core/` and `actors/`.
4. **Hot-path formatting:** `FieldHasher`, never `fmt.Sprintf`.
5. **Envelope sequencing:** Every NATS envelope carries `seq` + `prev_seq`; receivers detect gaps.

## Consequences

- Layer guard `make invariants-check` enforces the dependency rule automatically.
- Deterministic replay is possible because time is injected, not read from wall clock.
- Event contract versioning is the only approved cross-context coupling mechanism.

## Evidence

- Layer guard: `scripts/ci/guards/check-domain-isolation.sh`
- Clock contract: `internal/shared/clock/`
- Envelope: `internal/shared/envelope/`

## Changelog

- 2026-01-01: Accepted — foundation principles established
