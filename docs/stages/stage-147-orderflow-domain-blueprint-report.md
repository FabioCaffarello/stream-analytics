# Stage 147 — Orderflow Domain Blueprint

**Date:** 2026-03-09
**Status:** Complete
**ADR:** ADR-0033-orderflow-domain-blueprint.md

## Objective

Model orderflow as a coherent domain capability before building deeper widgets. Produce a blueprint that prevents structural duplication, clarifies data ownership, and enables independent vertical slices.

## Approach

Full audit of existing orderflow-related code across backend (Go) and client (Odin):

- **6 bounded contexts** inspected: marketdata, aggregation, insights, evidence, delivery, client
- **30 proto files** cataloged
- **11 domain types** mapped to tiers
- **12 widget contracts** documented
- **5 client stores** inventoried

## Key Findings

### 1. Orderflow Is Already Well-Structured

The existing four-tier pattern emerged organically and is sound:

| Tier | Owner | Types | Count |
|------|-------|-------|-------|
| T0 Raw | marketdata | TradeTickV1, BookDeltaV1 | 2 |
| T1 Aggregates | aggregation | Tape, Book, DeltaVol, CVD, BarStats, CrossVenueBook | 6 |
| T2 Derived | insights | VPVR, Heatmap, TPO, SessionVP, CrossVenueTrade | 5 |
| T3 Evidence | evidence | LiquidityEvidence (5 rules), RegimeState | 2 |

### 2. No New Bounded Context Needed

Creating `core/orderflow` would duplicate types already owned by `aggregation`. The four-tier model within existing BCs is the correct architecture.

### 3. Client Stores Are Under-Utilized

Two stores are allocated but have no renderer:
- `Footprint_Store` (200 candles × 50 levels) — ready for footprint chart
- `DOM_Store` (512 levels + 128 fills + VWAP/TWAP) — ready for DOM ladder

### 4. One New Domain Type Identified

`FootprintCandleV1` — per-candle bid/ask volume grid by price level. Belongs in `insights/domain/` using existing binning logic. All other orderflow concepts already have domain types.

### 5. Delivery Channels Are Sufficient

All 6 existing delivery channels cover current widgets. One new channel (`CHANNEL_FOOTPRINT`) may be needed when footprint chart ships, or HTTP API can serve it (like TPO/SessionVP).

## Deliverables

| Artifact | Path | Description |
|----------|------|-------------|
| ADR-0033 | `docs/adrs/ADR-0033-orderflow-domain-blueprint.md` | Orderflow domain architecture |
| Stage Report | `docs/stages/stage-147-orderflow-domain-blueprint-report.md` | This document |

## Vertical Slice Roadmap

Six independent slices identified, ordered by priority:

1. **Footprint Chart** — new `FootprintCandleV1` in insights + renderer using existing `Footprint_Store`
2. **DOM Ladder** — client-side composition of existing `Orderbook_Store` + `DOM_Store`
3. **Flow-Weighted VPVR** — enhance existing VP with buy/sell split coloring
4. **Cumulative Delta Profile** — standalone CVD widget (not subplot)
5. **Stacked Imbalance** — consecutive imbalance highlighting on chart
6. **Cross-Venue Spread** — standalone widget from existing `CrossVenueBookV1`

## Metrics

- Files created: 2 (ADR + report)
- Files modified: 0
- Tests added: 0 (design stage)
- Existing tests: unchanged
- Breaking changes: none

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Blueprint claro e documentado | Done (ADR-0033) |
| Bounded context de orderflow definido | Done (not new BC — tiered ownership) |
| Raw/aggregate/derived separation | Done (4-tier model) |
| Contracts para DOM/Orderbook/Trades/Counter/Stats/Burst | Done (widget-to-data table) |
| Ports/stores/delivery alignment | Done (existing ports sufficient) |
| Base pronta para slices verticais | Done (6 slices documented) |
