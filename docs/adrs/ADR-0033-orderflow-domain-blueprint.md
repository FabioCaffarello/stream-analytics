**Status:** Accepted
**Owner:** Architecture
**Last updated:** 2026-06-25
**Date:** 2026-05-15
**Deciders:** Platform Team
**Relates to:** [canonical-market-model](../contracts/canonical-market-model.md)

# ADR-0033 — Orderflow Domain Blueprint

## Context

Orderflow data (trades, DOM, orderbook snapshots, footprint) must be modeled consistently
across the Consumer, Aggregation, and Delivery bounded contexts without creating cross-context
type coupling. A canonical blueprint is required.

## Decision

1. Orderflow data flows through the CMM (`internal/core/marketmodel/`) as the single canonical representation.
2. Raw exchange messages are normalized in `adapters/exchange/` before entering the pipeline.
3. Aggregated orderflow (DOM, footprint) is owned by the Aggregation BC.
4. Delivery exposes orderflow via WebSocket using the Terminal_V1 protocol.

## Consequences

- Exchange adapters must not leak raw exchange types past the adapter boundary.
- The CMM `MarketEvent` proto is the authoritative orderflow contract.
- DOM and footprint are derived, not stored as raw data.

## Evidence

- CMM proto: `proto/marketmodel/v1/market_event.proto`
- Adapter boundary: `internal/adapters/exchange/`
- Delivery protocol: `docs/contracts/delivery-ws.md`

## Changelog

- 2026-05-15: Accepted — orderflow blueprint established
