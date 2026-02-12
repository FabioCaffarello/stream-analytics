# ADR-0003 — Actor Runtime as Execution Model

**Status:** Accepted  
**Date:** 2026-02-10

## Context

We need isolation, concurrency, supervision, backpressure, and deterministic operational behavior under high-frequency streams.

## Decision

We adopt an actor model runtime for orchestration:

- One actor per major unit of concurrency:
  - Exchange ingest actors
  - Instrument/aggregation actors
  - Session/subscription actors
  - Insight detector actors
- Actors are supervised and can restart with explicit restart policies.
- Actors communicate via message passing only (no shared mutable state across actors).
- Backpressure strategies are explicit (bounded mailboxes, drop policies, or batching).

Actors are NOT the domain layer:

- Domain decisions live in `core/*`.
- Actors call use cases and publish outputs.

## Consequences

- Fault isolation and predictable concurrency.
- Clear lifecycle and resource management.

## Alternatives

- Goroutine soup with shared locks (rejected: harder to reason and debug).
