# ADR-0000 — Foundation & Decision Records

**Status:** Accepted
**Date:** 2026-02-10
**Owners:** Core maintainers

## Context

We are building a Go platform for market data aggregation and decision-support insights using an actor runtime and clean architecture. Early architectural decisions must be recorded, auditable, and stable.

## Decision

- We will use ADRs as the primary mechanism for recording architecture decisions.
- ADRs are stored under `docs/adrs/` and use sequential numbering `ADR-000X`.
- Every ADR must contain: Context, Decision, Consequences, and Alternatives.
- Changes to an accepted ADR require a new ADR that supersedes it.

## Consequences

- Architectural decisions become traceable.
- Refactors and rewrites can be grounded in explicit intent.

## Alternatives

- RFC-only (rejected: ADRs are simpler and better as a permanent record).
