# Canonical Market Model (CMM)

**Status:** Active
**Owner:** Market Data + Delivery
**Last updated:** 2026-03-04

## Decision

CMM is the only model.

There is no legacy fallback in the hot path. Ingest, processor, and WS Terminal V1 operate on canonical CMM payloads and canonicalized envelopes only.

## Scope

- Canonical scalar/value types: `Venue`, `Symbol`, `Channel`, `StreamKey`, `Seq`, `ServerTS`, `Price`, `Size`, `Side`.
- Canonical event payloads: `Trade`, `BookSnapshot`, `BookDelta`, `Candle`, `Stats`, `Evidence`.
- Canonicalizer boundary: `NormalizeTrade`, `NormalizeBookDelta`, `NormalizeSnapshot` behind `ExchangeAdapter`.

## Runtime Rules

- Deterministic ordering only: stable sort, no map-iteration output, deterministic level collapse by price.
- Timestamps normalized to millisecond Unix time.
- Per-stream monotonic sequencing enforced on canonical stream state.
- Precision/rounding is explicit per venue+symbol via adapter precision rules.
- Stateful canonical book tracking is bounded by TTL and max entries with eviction metrics.

## Metrics

- `canonicalization_errors_total{venue,reason}`
- `canonical_events_total{channel,venue}`
- `canonical_state_entries`
- `canonical_state_evicted_total{reason}`

## Contracts

- Canonical typed envelope: `proto/marketmodel/v1/market_event.proto` (`marketmodel.v1.MarketEvent`).
- Registry: `proto/registry.json` entry `marketmodel.event` v1.

## Cutover

- `/ws/marketdata` legacy route is disabled by default and removed from server route wiring.
- Hot-path market payload aliases in `internal/core/marketdata/domain` point to CMM types; no parallel legacy payload structs are maintained.
