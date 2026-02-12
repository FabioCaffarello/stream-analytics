# ADR-0006 — Storage Split: Hot Path vs Cold Path

**Status:** Accepted  
**Date:** 2026-02-10

## Context

UI and agents require low-latency reads; analytics and history require large-scale storage. One store rarely fits both optimally.

## Decision

We adopt a two-tier storage strategy:

- Hot path: in-memory read models (ring buffers) for real-time delivery.
- Cold path: columnar/time-series DB for persistence and analytics.

Adapters:

- ClickHouse is preferred for large-scale aggregated artifacts (heatmaps, buckets).
- Timescale/Postgres may be used for operational metadata and smaller time-series.
Final selection per workload remains pluggable behind `core/storage/ports`.

## Consequences

- Real-time UX remains fast.
- Historical queries and backfills remain scalable.

## Alternatives

- Single DB for everything (rejected: either slow realtime or expensive analytics).
