# ADR-0002 — Canonical Event Envelope & Versioning

**Status:** Accepted  
**Date:** 2026-02-10

## Context

Multi-venue market data is noisy, heterogeneous, and time-inconsistent. We need reproducible pipelines (replay), idempotency, and backward compatibility.

## Decision

All messages exchanged between components (bus, internal pubsub, persisted streams) use a canonical envelope:

Envelope MUST include:

- `type`: stable event type name (e.g., `marketdata.trade`)
- `version`: integer schema version
- `venue`: exchange identifier (e.g., `binance`, `bybit`)
- `instrument`: canonical instrument (symbol + contract type)
- `ts_exchange`: exchange-provided timestamp (if present)
- `ts_ingest`: local ingest timestamp (monotonic wall clock)
- `seq`: monotonic sequence per `(venue, instrument)` produced by a Sequencer
- `idempotency_key`: stable key used for deduplication
- `payload`: versioned payload

Versioning rules:

- Payload changes that break decoding increment `version`.
- Consumers MUST support at least N-1 versions during migration windows.
- Event types MUST be stable; avoid renaming.

## Consequences

- Replay, audit, and debugging become reliable.
- Consumers can be upgraded independently.

## Alternatives

- “Raw structs per adapter” (rejected: breaks replay and cross-venue invariants).

## Amendment — 2026-02-12

### Wire Format Strategy

Envelope suporta multiplos formatos por `content_type`:
- `application/json` (default atual)
- `application/protobuf` (placeholder para W6)

A deteccao de codec deve respeitar `content_type` sem quebrar retrocompatibilidade.

### Schema Discovery

Discovery de schema fica em manifest local (`proto/registry.json`) mapeando `(type, version)` para definicao. Sem servico externo nesta fase.

### Compatibility Rules

- nunca reutilizar numero de campo removido
- nunca trocar tipo de campo existente
- novos campos apenas opcionais
- verificacao automatizada via `buf breaking` entra no W6

### Meta Padronizada

Campos reservados em `meta` para interoperabilidade futura:
- `trace_id`
- `source_stream`
- `market_type`
- `content_type`
