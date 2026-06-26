**Status:** Accepted
**Owner:** Architecture
**Last updated:** 2026-06-25
**Date:** 2026-05-01
**Deciders:** Platform Team
**Relates to:** [system-invariants](../architecture/system-invariants.md)

# ADR-0032 — Stream Reliability Model

## Context

The delivery subsystem must expose stream health to the operator cockpit in a way that
distinguishes between exchange-level failures, pipeline gaps, and slow-client backpressure.
A 5-layer model was proposed to cover detection, classification, signaling, recovery, and
operator visibility.

## Decision

Adopt a 5-layer stream health pipeline:
1. **Detect** — gap detection via `prev_seq` mismatch
2. **Classify** — distinguish exchange drop, pipeline backlog, client backpressure
3. **Signal** — health envelope published to `delivery.health.v1.*`
4. **Recover** — backfill on reconnect; Guardian restart on hard failure
5. **Surface** — cockpit health widget with per-stream reliability indicators

## Consequences

- All exchange adapters must maintain their own `seq` chain.
- Delivery actors publish health events that the Odin client consumes.
- Recovery path requires gap-fill protocol in `sequence-exchange-recovery.md`.

## Evidence

- Health events: `docs/contracts/event-bus.md`
- Delivery sequence: `docs/architecture/diagrams/sequence-live-ingestion.md`
- Exchange recovery: `docs/architecture/diagrams/sequence-exchange-recovery.md`

## Changelog

- 2026-05-01: Accepted — 5-layer model adopted
