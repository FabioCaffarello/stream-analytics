# ADR-0005 — Sequencing & Time Normalization

**Status:** Accepted  
**Date:** 2026-02-10

## Context

Exchange timestamps are inconsistent. Without a consistent ordering mechanism, orderbook building and replay correctness degrade.

## Decision

We introduce a Sequencer in `core/marketdata/app`:

- Maintains monotonic `seq` per `(venue, instrument)`.
- Derives `ts_ingest` using a clock abstraction.
- Envelope includes both `ts_exchange` and `ts_ingest`.
- Aggregation logic uses `seq` for ordering; `ts_exchange` is advisory only.

## Consequences

- Deterministic processing and reliable replay.
- Better correctness for derived artifacts (orderbooks/heatmaps/stats).

## Alternatives

- Rely on exchange timestamps (rejected: causes out-of-order issues).
