# ADR-0004 — Message Bus Strategy (NATS JetStream First)

**Status:** Accepted
**Date:** 2026-02-10

## Context

We need durable streaming, idempotent publishing, and consumer groups for ingestion and processing. We also want simple local dev.

## Decision

We adopt NATS JetStream as the initial event bus adapter:

- Producers publish versioned envelopes with `Msg-ID` mapped from `idempotency_key`.
- Consumers are durable; each processing stage has a stable durable name.
- Subject naming includes bounded context + venue + instrument partitions (exact schema in an RFC/contract doc).

The bus is accessed ONLY via `core/*/ports` interfaces.
We keep Kafka/Redpanda as a future adapter; no Kafka-specific assumptions in core.

## Consequences

- Fast iteration locally and in production.
- Durable streams with dedup support.

## Alternatives

- Kafka first (rejected initially: higher ops cost and complexity for MVP).
- In-memory pubsub only (rejected: no durability/replay).

## Evidence

- Validation gate: `make docs-check-full`
- Authority path: file-local ADR source.

## Changelog

- 2026-02-13: added required header sections for docs compliance.
