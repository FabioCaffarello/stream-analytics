**Status:** Accepted
**Owner:** Architecture
**Last updated:** 2026-06-25
**Date:** 2026-05-20
**Deciders:** Platform Team
**Relates to:** [ADR-0032](ADR-0032-stream-reliability-model.md)

# ADR-0034 — Stream Health Recovery Completion

## Context

ADR-0032 defined the 5-layer stream health model. This ADR completes the recovery layer:
specifically, how the system transitions from a degraded health state back to a healthy state
after gap detection, exchange reconnect, or Guardian restart.

## Decision

1. On gap detection (`prev_seq` mismatch): trigger backfill from the last known good sequence.
2. On exchange disconnect: exponential backoff reconnect with jitter; gap-fill on reconnect.
3. On Guardian restart: re-subscribe with the latest available sequence; no history replay.
4. Completion signal: `delivery.health.v1.*` transitions to `healthy` once the first post-recovery envelope is ACKed.

## Consequences

- The gap-fill protocol requires the Store to serve range queries by `(venue, instrument, seq_from)`.
- Guardian restart recovery does NOT guarantee delivery of messages sent during downtime (best-effort).

## Evidence

- Exchange recovery: `docs/architecture/diagrams/sequence-exchange-recovery.md`
- Storage range read: `docs/architecture/diagrams/sequence-storage-federation.md`
- Guardian: `internal/actors/runtime/guardian.go`

## Changelog

- 2026-05-20: Accepted — recovery completion protocol defined
