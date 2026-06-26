**Status:** Accepted
**Owner:** Architecture
**Last updated:** 2026-06-25
**Date:** 2026-06-01
**Deciders:** Platform Team
**Relates to:** [ADR-0033](ADR-0033-orderflow-domain-blueprint.md), [event-bus](../contracts/event-bus.md)

# ADR-0035 — Orderflow Contract Architecture

## Context

The orderflow domain (trades, DOM, footprint, liquidations, mark price) spans multiple bounded
contexts. A clear contract architecture is needed to specify which events cross context boundaries,
who produces them, and who owns the schema.

## Decision

All orderflow events use the standard NATS subject format:
`{event}.v{version}.{venue}.{instrument}`

Ownership rules:
- `marketdata.*` subjects: produced by `marketdata` BC, schema owned by `marketdata` BC
- `aggregation.*` subjects: produced by `aggregation` BC after processing CMM events
- Schema evolution requires approval from `schema_authority_bc` per subject registry

Cross-context access is read-only via NATS consumers; no direct type sharing.

## Consequences

- All new orderflow event types must be registered in `docs/contracts/subject-registry.yaml`.
- Protobuf schema changes require `make proto-check` + `make proto-breaking` to pass.
- Consumer BCs must never depend on the internal types of producer BCs.

## Evidence

- Subject registry: `docs/contracts/subject-registry.yaml`
- Proto lint: `make proto-lint`, `make proto-breaking`
- Event bus contract: `docs/contracts/event-bus.md`

## Changelog

- 2026-06-01: Accepted — orderflow contract architecture formalized
