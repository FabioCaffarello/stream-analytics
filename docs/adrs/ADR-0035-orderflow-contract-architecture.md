# ADR-0035 — Orderflow Contract Architecture

**Status:** Accepted
**Date:** 2026-03-09
**Owners:** Architecture
**Extends:** ADR-0033 (Orderflow Domain Blueprint)

## Context

ADR-0033 established that orderflow is a **cross-cutting concern** spanning existing BCs (marketdata, aggregation, insights, evidence) with a 4-tier data model. S147-S149 delivered the first vertical slices (DOM_Store, Footprint_Store, DOM Ladder renderer).

However, after auditing the codebase post-S154, three structural gaps remain:

1. **Footprint pipeline is disconnected** — `Footprint_Store` exists (200 candles × 50 levels) but `footprint_store_push_trade()` is never called from any reducer. The store is allocated per-stream on `Market_Stream` but permanently empty.

2. **No widget contracts for Footprint or dedicated Trade Counter** — `Widget_Kind` has `DOM` but no `Footprint`. The counter capability is embedded within `Stats` widget rendering, not a first-class widget.

3. **Orderflow data contracts are implicit** — the mapping from WS channels → reducers → stores → widget data contexts is correct but undocumented as a formal contract. Widget authors must trace 4 files to understand data flow.

**Evidence:**
- `market_store_reducers.odin`: `market_store_reduce_trade()` calls `dom_store_push_trade()` but NOT `footprint_store_push_trade()` (S148 wired DOM only)
- `widget_contract.odin:125-138`: no `Footprint` entry in `WIDGET_CONTRACTS`
- `app.odin:25-38`: `Widget_Kind` enum has 12 variants, none for Footprint
- ADR-0033 §5.1: `FootprintCandleV1` defined but not implemented in backend

## Decision

### 1. Orderflow Data Contract Matrix

Formalize the end-to-end data contract for every orderflow capability:

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│  CAPABILITY        │ BACKEND OWNER  │ CHANNEL/API         │ CLIENT STORE       │
├─────────────────────────────────────────────────────────────────────────────────┤
│  Raw Trades        │ marketdata     │ CHANNEL_TRADE       │ Trades_Store       │
│  Trade Tape        │ aggregation    │ CHANNEL_STATS       │ Trades_Store (fb)  │
│  Orderbook L2      │ aggregation    │ CHANNEL_BOOK_SNAP   │ Orderbook_Store    │
│  DOM Fills         │ client-local   │ (from raw trades)   │ DOM_Store          │
│  Footprint Grid    │ client-local   │ (from raw trades)   │ Footprint_Store    │
│  Delta Volume      │ aggregation    │ Analytics stream    │ Analytics_Store    │
│  CVD               │ aggregation    │ Analytics stream    │ Analytics_Store    │
│  Bar Stats         │ aggregation    │ CHANNEL_STATS       │ Analytics_Store    │
│  VPVR              │ insights       │ CHANNEL_VPVR_SNAP   │ VPVR_Store         │
│  Heatmap           │ insights       │ CHANNEL_HEATMAP     │ Heatmap_Store      │
│  Session VP        │ insights       │ HTTP API            │ Session_VPVR_Store │
│  TPO               │ insights       │ HTTP API            │ TPO_Store          │
│  Evidence          │ evidence       │ CHANNEL_EVIDENCE    │ Evidence ring      │
│  Signals           │ evidence       │ CHANNEL_SIGNAL      │ Signal_Store       │
│  FootprintCandle   │ insights [NEW] │ HTTP API [FUTURE]   │ Footprint_Store    │
└─────────────────────────────────────────────────────────────────────────────────┘
```

### 2. Footprint Reducer Wiring (S155 scope)

Wire `footprint_store_push_trade()` from `market_store_reduce_trade()`. The store already exists per-stream on `Market_Stream`. The reducer needs:
- `price`, `qty`, `is_buy` — from `MD_Trade_Event` (already available)
- `trade_ts_ms` — from `evt.unix` (already available)
- `tf_ms` — must be resolved from the stream's active timeframe context

**Design choice:** The footprint store accumulates client-local trade data into candle-aligned bins. This is **independent of backend FootprintCandleV1** — the client accumulates from raw trades for immediate display; the backend will produce authoritative replay-safe footprints in a future stage.

The reducer will receive `tf_ms` from the stream slot's active timeframe (resolved at dispatch time, not stored on the stream). If `tf_ms` is 0 (no TF context), footprint accumulation is skipped.

### 3. Widget Kind Extension

Add `Footprint` to `Widget_Kind`. This enables:
- Compile-time contract registration in `WIDGET_CONTRACTS`
- Widget catalog entry for dashboard layout
- Persistence via `WORKSPACE_SCHEMA`

The footprint renderer is a **future slice** — S155 adds the enum + no-op contract, ensuring the pipeline is wired end-to-end before building UI.

### 4. Orderflow Widget Taxonomy

Classify all orderflow widgets by their data tier and interaction model:

| Widget | Tier | Update Freq | Interaction | Store | Status |
|--------|------|-------------|-------------|-------|--------|
| Trades Tape | T0 | Per-tick | Scroll | Trades_Store | DONE |
| Orderbook | T1 | Per-snap | Scroll | Orderbook_Store | DONE |
| DOM Ladder | T0+T1 | Per-tick | Scroll + grouping | DOM_Store + Orderbook | DONE |
| Trade Counter | T1 | Per-window | Static | Analytics_Store | DONE (via Stats) |
| CVD Subplot | T1 | Per-window | Chart-synced | Analytics_Store | DONE |
| Delta Vol Subplot | T1 | Per-window | Chart-synced | Analytics_Store | DONE |
| OI Subplot | T1 | Per-window | Chart-synced | Analytics_Store | DONE |
| VPVR Overlay | T2 | Per-session | Chart-synced | VPVR_Store | DONE |
| Heatmap | T2 | Per-window | Static | Heatmap_Store | DONE |
| Session VP | T2 | Per-session | Static | Session_VPVR_Store | DONE |
| TPO Profile | T2 | Per-session | Static | TPO_Store | DONE |
| Footprint Chart | T0→local | Per-tick | Chart-synced | Footprint_Store | WIRING |
| Imbalance Overlay | T3 | Per-event | Chart-synced | Evidence ring | DONE |

### 5. Port Contract Alignment

No new ports needed. The existing `MD_Event_Kind.Trade` event already carries all fields required by both `DOM_Store` and `Footprint_Store`. The reducer dispatch in `data_source.odin` routes `Trade` events to `market_store_reduce_trade()`, which is the single entry point for all trade-derived accumulation.

**Invariant:** All trade-derived stores (`Trades_Store`, `DOM_Store`, `Footprint_Store`) are fed from `market_store_reduce_trade()`. No parallel paths.

### 6. Delivery Contract Alignment

Current WS delivery channels are sufficient for all orderflow widgets:
- Raw trades: `CHANNEL_TRADE` (T0)
- Aggregates: `CHANNEL_STATS`, analytics streams (T1)
- Derived: `CHANNEL_VPVR_SNAP`, `CHANNEL_HEATMAP`, HTTP API (T2)
- Evidence: `CHANNEL_EVIDENCE`, `CHANNEL_SIGNAL` (T3)

**Future:** `FootprintCandleV1` from backend can use HTTP API (same pattern as Session VP / TPO — lower frequency, session-scoped). No new WS channel needed unless sub-second footprint updates are required.

### 7. Backend FootprintCandleV1 — Deferred

The backend `FootprintCandleV1` type (ADR-0033 §5.1) remains deferred. Rationale:
- Client-local footprint from raw trades is sufficient for MVP
- Backend construction requires a new builder in `insights/app/` consuming `TapeWindowV1`
- The binning logic mirrors VPVR (existing pattern) — low implementation risk when needed
- Will be triggered when: (a) replay-safe footprints are needed, or (b) multi-client consistency is required

### 8. Price Grouping Contract

Both `DOM_Store` and `Footprint_Store` use price grouping (bucketing) with the same formula:
```
bucket_price = floor(price / group) * group
```

**Contract:** `price_group` flows from the chart component's `dom_group_idx` field → resolved at render time. Stores default to `group=1.0` when no explicit grouping is set. This is consistent with how `Orderbook_Store` bins are already rendered.

## Consequences

### Positive
- **Single source of truth** — all trade-derived stores fed from one reducer
- **Footprint wired end-to-end** — store populated from live trades, renderer can be built independently
- **Widget catalog complete** — `Footprint` kind enables persistence and catalog integration
- **No backend changes** — client-local accumulation avoids backend coupling for MVP

### Negative
- **Client footprint is non-authoritative** — disconnects/reconnects lose accumulated data (acceptable for MVP; backend FootprintCandleV1 solves this later)
- **TF dependency** — footprint accumulation requires knowing the active timeframe at reduce time; if TF changes, old entries become orphaned (mitigated by store reset on TF switch)

## Alternatives

1. **Do nothing** — Rejected: Footprint_Store remains permanently empty, wasting 200×50 entries per stream. Widget_Kind remains incomplete.
2. **Backend-first footprint** — Rejected: premature complexity. Client-local accumulation proves the UX before investing in backend pipeline.
3. **New `core/orderflow` bounded context** — Rejected (reaffirm ADR-0033): cross-cutting concern, not a BC. Creating one would duplicate types owned by aggregation/insights.
4. **Separate reducer for footprint** — Rejected: footprint feeds from the same trade events as DOM. Single reducer entry point is cleaner.

## Evidence
- Validation gate: `make test` (client), `make ci` (full)
- Authority path: `docs/adrs/ADR-0035-orderflow-contract-architecture.md`
- Related: ADR-0033 (domain blueprint), ADR-0027 (widget host contract), ADR-0028 (data context ownership)

## Changelog
- 2026-03-09: Initial acceptance (S155).
