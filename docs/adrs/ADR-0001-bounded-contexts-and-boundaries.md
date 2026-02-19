# ADR-0001 — Bounded Contexts & Hexagonal Boundaries

**Status:** Accepted
**Date:** 2026-02-10

## Context

The reference codebase mixes concerns across actors, transport, storage, and domain rules. We need long-term modularity and clear separation of responsibilities.

## Decision

We adopt a bounded-context model aligned to hexagonal architecture:

- `core/*` contains the product logic:
  - `domain/`: invariants, value objects, aggregates, domain events
  - `app/`: use cases, orchestration of domain rules
  - `ports/`: interfaces for infrastructure (bus, db, feeds, auth, clock)

- `actors/*` are execution coordinators:
  - Actors are thin and call `core/*` use cases via ports.
  - Actors do not own business rules or domain invariants.

- `adapters/*` implement `ports/*`:
  - Exchange connectors, NATS/Kafka, DBs, metrics, auth providers.

- `interfaces/*` are driving adapters:
  - HTTP/WS/MCP boundaries translate external requests into app commands.

Bounded contexts:

- `marketdata`: ingest + normalize + sequencing
- `aggregation`: orderbook/heatmap/cvd/stat builders
- `storage`: persistence + query contracts
- `delivery`: subscriptions/sessions + snapshots
- `insights`: decision-support insights with evidence

## Consequences

- Domain logic remains testable and portable.
- Infrastructure can be swapped without refactoring core.

## Alternatives

- Monolithic package layout (rejected: increases coupling and slows evolution).

## Evidence

- Validation gate: `make docs-check-full`
- Authority path: `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md`

## Changelog

- 2026-02-13: added required `Evidence` and `Changelog` sections for docs header compliance.
