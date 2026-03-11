# ADR-0033: Orderflow Domain Blueprint

**Status:** Accepted
**Date:** 2026-03-09
**Stage:** S147 — Orderflow Domain Blueprint

## Context

Market Raccoon has accumulated significant orderflow capability across multiple bounded contexts:

- **marketdata**: raw `TradeTickV1`, `BookDeltaV1` ingestion (6 exchanges)
- **aggregation**: `TapeWindowV1` (250ms/1s/5s), `OrderBook` (BTree), `DeltaVolumeWindowV1`, `CVDWindowV1`, `BarStatsWindowV1`, `OrderBookSnapshotV2`, `CrossVenueBookSnapshotV1`
- **insights**: `VolumeProfileSnapshotV1`, `HeatmapCellV1`, `TPOProfileV1`, `SessionVolumeProfileV1`, `CrossVenueTradeSnapshotV1`
- **evidence**: `LiquidityEvidence` (5 detection rules: book imbalance, absorption, sweep, thinning, spread regime)
- **client**: `Trades_Store`, `Orderbook_Store`, `DOM_Store`, `Footprint_Store`, 4 render procs, 3 analytics subplots

This work grew organically across 30+ stages. Before adding deeper orderflow widgets (footprint chart, DOM ladder, flow-weighted VPVR, cumulative delta profiles), we need a coherent domain model that prevents structural duplication and clarifies data ownership.

**Problem:** There is no single bounded context that owns "orderflow" as a first-class concept. Orderflow data is scattered across `aggregation` (tape, delta volume, CVD), `insights` (VPVR, heatmap, TPO), `evidence` (LEL rules), and `marketdata` (raw feeds). Widget authors must navigate 4+ BCs to build an orderflow feature.

## Decision

### 1. Orderflow Is NOT a New Bounded Context

Orderflow is a **cross-cutting concern** that spans existing BCs. Creating a new `core/orderflow` BC would duplicate domain types already owned by `aggregation` and `insights`. Instead, we define clear **data tiers** and **ownership rules** within existing BCs.

### 2. Four-Tier Data Model

```
┌──────────────────────────────────────────────────────────┐
│  TIER 0 — RAW FEEDS              Owner: marketdata       │
│  TradeTickV1, BookDeltaV1                                │
│  ↓ normalized, sequenced, timestamped                    │
├──────────────────────────────────────────────────────────┤
│  TIER 1 — AGGREGATES             Owner: aggregation      │
│  TapeWindowV1          (trade flow per window)           │
│  OrderBookSnapshotV2   (full book state)                 │
│  DeltaVolumeWindowV1   (buy-sell split per window)       │
│  CVDWindowV1           (cumulative delta)                │
│  BarStatsWindowV1      (trade stats per window)          │
│  CrossVenueBookV1      (merged multi-venue book)         │
│  ↓ deterministic, windowed, replay-safe                  │
├──────────────────────────────────────────────────────────┤
│  TIER 2 — DERIVED ARTIFACTS      Owner: insights         │
│  VolumeProfileV1       (price-bucketed volume)           │
│  HeatmapCellV1         (2D price × size footprint)       │
│  TPOProfileV1          (time-price opportunity)          │
│  SessionVolumeProfileV1(session-scoped VP)               │
│  CrossVenueTradeV1     (multi-venue trade join)          │
│  FootprintCandle  [NEW](per-candle bid/ask volume grid)  │
│  ↓ session-scoped, higher latency tolerance              │
├──────────────────────────────────────────────────────────┤
│  TIER 3 — EVIDENCE & SIGNALS     Owner: evidence         │
│  LiquidityEvidence     (imbalance, sweep, absorption)    │
│  RegimeState           (spread regime transitions)       │
│  ↓ stateful detection, confidence-scored                 │
└──────────────────────────────────────────────────────────┘
```

### 3. Ownership Rules

| Concern | Owner BC | Rationale |
|---------|----------|-----------|
| Trade normalization | `marketdata` | Source-of-truth for raw feed identity |
| Trade aggregation (tape, delta, CVD) | `aggregation` | Deterministic windowed computation |
| Order book state machine | `aggregation` | Consistency invariants (no crossed books) |
| Volume profiling (VPVR, heatmap, TPO) | `insights` | Session-scoped derived analytics |
| Footprint candle construction | `insights` | Per-candle derived artifact (new) |
| Microstructure detection | `evidence` | Stateful rule evaluation |
| DOM trade accumulation | `client` | Client-local real-time state |
| Widget rendering | `client` | Presentation layer |

### 4. Widget-to-Data Contracts

Each orderflow widget maps to exactly one primary data tier:

| Widget | Primary Data | Tier | Delivery Channel |
|--------|-------------|------|------------------|
| Trades Tape | `TradeTickV1` | T0 | `CHANNEL_TRADE` |
| Orderbook / DOM | `OrderBookSnapshotV2` | T1 | `CHANNEL_BOOK_SNAPSHOT` |
| Trade Counter | `TapeWindowV1` + `BarStatsWindowV1` | T1 | `CHANNEL_STATS` (bundled) |
| CVD Subplot | `CVDWindowV1` | T1 | Analytics stream |
| Delta Volume Subplot | `DeltaVolumeWindowV1` | T1 | Analytics stream |
| Volume Profile (VPVR) | `VolumeProfileSnapshotV1` | T2 | `CHANNEL_VOLUME_PROFILE_SNAPSHOT` |
| Heatmap | `HeatmapCellV1` | T2 | `CHANNEL_HEATMAP_SNAPSHOT` |
| TPO Profile | `TPOProfileV1` | T2 | HTTP API |
| Session VP | `SessionVolumeProfileV1` | T2 | HTTP API |
| Footprint Chart [NEW] | `FootprintCandle` [NEW] | T2 | New channel or HTTP |
| DOM Ladder [NEW] | `OrderBookSnapshotV2` + `TradeTickV1` | T0+T1 | Composite |
| Imbalance Overlay | `LiquidityEvidence` | T3 | `CHANNEL_EVIDENCE` (existing) |

### 5. New Domain Types Required

#### 5.1 FootprintCandle (insights/domain)

Per-candle volume distribution by price level — the missing link between `TapeWindowV1` and `VolumeProfileSnapshotV1`:

```go
type FootprintCandleV1 struct {
    Venue       string
    Instrument  string
    Timeframe   string
    OpenTs      int64        // candle open timestamp
    CloseTs     int64        // candle close timestamp
    Levels      []FootprintLevelV1
    TotalBuy    float64
    TotalSell   float64
    TradeCount  int64
    POCPrice    float64      // price level with max total volume
    DeltaTotal  float64      // total buy - total sell
    Seq         int64
    TsIngestMs  int64
}

type FootprintLevelV1 struct {
    Price       float64
    BuyVolume   float64
    SellVolume  float64
    Delta       float64      // buy - sell at this level
    TradeCount  int64
}
```

**Ownership:** `insights` (derived from `TapeWindowV1` + price bucketing, same binning logic as VPVR).

#### 5.2 DOM Ladder Enrichment (client-local)

The client `DOM_Store` already tracks trade volume per price level. The DOM Ladder widget composes:
- `OrderBookSnapshotV2` (resting liquidity) — from `aggregation`
- `DOM_Store` trade fills (executed volume) — client-local accumulation
- Visual: price ladder with bid/ask depth + fill volume overlay

No new backend type needed — this is a **client-side composition** of existing T0 + T1 data.

#### 5.3 Imbalance Metrics on Tape (aggregation enhancement)

`TapeWindowV1` already carries `Imbalance` and `IsBurst`. For future stacked imbalance detection, add to `BarStatsWindowV1`:

```go
// Already exists — no change needed:
// Imbalance float64  // (buy-sell)/total in [-1,+1]
// IsBurst  bool

// Future consideration (not in this stage):
// ConsecutiveImbalance int32  // count of same-sign windows
// DominantSide         string // "bid" or "ask"
```

This is deferred — the evidence BC's `PersistentImbalanceRule` already handles consecutive detection.

### 6. Delivery Contracts

#### Existing Channels (no changes needed)

| Channel | Payload | Status |
|---------|---------|--------|
| `CHANNEL_TRADE` | `TradeTickV1` | Active |
| `CHANNEL_BOOK_DELTA` | `BookDeltaV1` | Active |
| `CHANNEL_BOOK_SNAPSHOT` | `OrderBookSnapshotV2` | Active |
| `CHANNEL_STATS` | Bundled (candle + stats + tape) | Active |
| `CHANNEL_HEATMAP_SNAPSHOT` | `HeatmapCellV1` | Active |
| `CHANNEL_VOLUME_PROFILE_SNAPSHOT` | `VolumeProfileSnapshotV1` | Active |

#### New Channel (future stage)

| Channel | Payload | When |
|---------|---------|------|
| `CHANNEL_FOOTPRINT` | `FootprintCandleV1` | When footprint chart widget is built |

Footprint can alternatively be served via HTTP API (like TPO/Session VP) to avoid WS overhead for less-frequently-updated data.

### 7. Client Store Architecture

Current stores are well-structured. No structural changes needed:

| Store | Cap | Purpose | Status |
|-------|-----|---------|--------|
| `Trades_Store` | 256 | Recent trade ticks | Active |
| `Orderbook_Store` | 50/side | L2 book snapshot | Active |
| `DOM_Store` | 512 levels | Trade fill accumulation | Active (underused) |
| `Footprint_Store` | 200 candles | Per-candle volume grid | Active (no renderer) |
| `Analytics_Store` | ring | CVD, DeltaVol, OI, BarStats | Active |

**Key finding:** `Footprint_Store` and `DOM_Store` are already instantiated but have no dedicated renderers. The blueprint for footprint chart and DOM ladder widgets can leverage these existing stores directly.

### 8. Port Architecture

Existing port interfaces are sufficient. The orderflow pipeline follows:

```
Exchange Adapter → marketdata ports (Writer)
    → aggregation ports (Publisher: Tape/Book/DeltaVol/CVD)
        → insights ports (Publisher: VPVR/Heatmap/TPO/Footprint)
        → evidence ports (Publisher: LiquidityEvidence)
            → delivery ports (Session: subscribe/stream to client)
```

No new port interfaces needed for the blueprint. When `FootprintCandleV1` is implemented, it gets a standard `FootprintPublisher` + `FootprintHotReadModelStore` in `insights/ports/`.

## Consequences

### Positive
- **No new BC** — avoids structural refactoring and package proliferation
- **Clear tier ownership** — widget authors know exactly where data lives
- **Incremental delivery** — each widget can be built as a vertical slice without touching other tiers
- **Existing stores** — `Footprint_Store` and `DOM_Store` are already allocated in client

### Negative
- **Cross-BC queries** — some widgets (DOM Ladder) compose data from multiple BCs; this is handled at the client composition layer, not the backend
- **Footprint construction** — requires a new builder in `insights/app/` that consumes `TapeWindowV1` events; this is straightforward given existing VPVR builder patterns

### Risks
- **Footprint at scale** — 50 price levels × 200 candles × N instruments could be memory-intensive on client; mitigate with configurable level cap and LRU eviction (already in `Footprint_Store` design)

## Vertical Slice Roadmap

Each slice is independently deliverable:

| # | Slice | Backend | Client | Priority |
|---|-------|---------|--------|----------|
| 1 | Footprint Chart | `FootprintCandleV1` builder in insights | Renderer using existing `Footprint_Store` | High |
| 2 | DOM Ladder | None (existing data) | Compose `Orderbook_Store` + `DOM_Store` | High |
| 3 | Flow-Weighted VPVR | Enhance `VolumeProfileSnapshotV1` with buy/sell split | Render delta coloring | Medium |
| 4 | Cumulative Delta Profile | Already exists (`CVDWindowV1`) | Standalone widget (not subplot) | Medium |
| 5 | Stacked Imbalance | `BarStatsWindowV1` consecutive tracking | Highlight on chart | Low |
| 6 | Cross-Venue Spread | Already exists (`CrossVenueBookV1`) | Standalone widget | Low |

## Alternatives Considered

1. **New `core/orderflow` BC** — Rejected: would duplicate `TapeWindowV1`, `OrderBook`, `DeltaVolume` types already owned by `aggregation`. Cross-cutting concerns are better modeled as tier contracts than new boundaries.

2. **Flatten all orderflow into `aggregation`** — Rejected: session-scoped artifacts (VPVR, TPO, footprint) have different lifecycle and latency characteristics than windowed aggregates.

3. **Client-only footprint** — Rejected: server-side construction ensures deterministic replay and multi-client consistency.

## Evidence

- Validation gate: `make docs-check-full`
- Authority path: `docs/adrs/ADR-0033-orderflow-domain-blueprint.md`
- Existing ADRs: ADR-0001 (bounded contexts), ADR-0016 (proto contracts)

## Changelog

- 2026-03-09: Initial acceptance (S147).
