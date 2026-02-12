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

## Amendment — 2026-02-12

### Lifecycle Guarantees

- `actor.Stopped` deve liberar todos os recursos (conn/timer/context)
- teardown deve ser deterministico e idempotente
- retries agendados devem validar geracao para descartar evento obsoleto

### Request/Reply Pattern

Handlers devem preferir `ReplyTo` quando presente e cair para `c.Sender()` quando nil para compatibilidade com `engine.Request()`.

### Runtime Restart Guardrails

Guardian usa:
- policy por subsystem (backoff/cooldown)
- limiter global de restart por janela para evitar storm/thundering herd

Quando rate-limit global dispara, restart e adiado e subsystem segue degradado ate liberar janela.
