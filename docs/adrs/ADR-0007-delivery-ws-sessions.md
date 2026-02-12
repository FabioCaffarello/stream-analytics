# ADR-0007 — Delivery Boundary: WebSocket Sessions as Actors

**Status:** Accepted  
**Date:** 2026-02-10

## Context

Real-time clients subscribe/unsubscribe frequently. We need per-connection isolation and robust cleanup.

## Decision

- Each WS connection is represented by a Session Actor.
- Subscriptions are modeled in `core/delivery/domain` (topics, filters).
- The WS server is a thin adapter that upgrades connections and delegates to Session Actors.
- Delivery publishes snapshots/events from hot read models; it does not compute aggregates.

## Consequences

- Fault isolation per client.
- Clear lifecycle and manageable backpressure per session.

## Alternatives

- Shared global WS hub (rejected: harder backpressure and isolation).
