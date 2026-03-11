# Stage 148 — Orderflow Data Plane Foundations

**Date:** 2026-03-09
**Status:** Complete
**Depends on:** ADR-0033 (S147 — Orderflow Domain Blueprint)

## Objective

Document and prepare the orderflow data plane so vertical slices (DOM, Trades, Counter, Stats, Footprint) can be built without reopening architecture. Define explicit budgets, retention, cadence, and clipping policies.

## 1. Subject/Stream Inventory

All orderflow subjects are already registered in the delivery router. No new subjects needed.

### Orderflow Subjects (Backend → Client)

| Subject | Channel | Cadence | Payload | Priority |
|---------|---------|---------|---------|----------|
| `marketdata.trade` | `CHANNEL_TRADE` | Per-tick | `TradeTickV1` | 100 (highest) |
| `marketdata.bookdelta` | `CHANNEL_BOOK_DELTA` | Per-tick | `BookDeltaV1` | 20 |
| `aggregation.snapshot` | `CHANNEL_BOOK_SNAPSHOT` | Rate-limited | `OrderBookSnapshotV2` | — |
| `aggregation.tape` | (unassigned) | 250ms/1s/5s | `TapeWindowV1` | — |
| `aggregation.stats` | `CHANNEL_STATS` | 1s–1d windows | `StatsWindowClosedV1` | 80 |
| `aggregation.bar_stats` | `CHANNEL_STATS` | 250ms/1s/5s | `BarStatsWindowV1` | 78 |
| `aggregation.delta_volume` | (unassigned) | 250ms/1s/5s | `DeltaVolumeWindowV1` | 76 |
| `aggregation.cvd` | (unassigned) | 250ms/1s/5s | `CVDWindowV1` | 76 |
| `aggregation.oi` | (unassigned) | Per-tick | `OpenInterestWindowV1` | 72 |
| `aggregation.candle` | `CHANNEL_CANDLE` | TF-aligned | `CandleClosedV1` | 90 |

### Client Channel Aliases

```
trade          → marketdata.trade
book_delta     → marketdata.bookdelta
book_snapshot  → aggregation.snapshot
stats          → aggregation.stats
bar_stats      → aggregation.bar_stats
delta_volume   → aggregation.delta_volume
cvd            → aggregation.cvd
tape           → aggregation.tape
candle         → aggregation.candle
```

### Observation: Unassigned Channels

`tape`, `delta_volume`, `cvd` have no `CHANNEL_*` enum in `terminal.proto`. They are routed via subject string matching, not proto enum. This is fine for streaming but means no `ClientSubscribe.channel` shorthand exists. Widgets consuming these use analytics reducers, not direct channel subscription.

---

## 2. Snapshot vs Delta Semantics

### Order Book

| Aspect | Backend | Client |
|--------|---------|--------|
| **Ingestion** | Incremental `BookDeltaV1` (is_snapshot=false) or full snapshot (is_snapshot=true) | Always full snapshot replacement |
| **State machine** | BTree with `ApplyDelta()` / `ApplySnapshot()` + crossed-book detection | `update_orderbook()` — flat array copy |
| **Publication** | Capped to `PublishDepthCap` (50 levels) per side | Receives capped snapshot |
| **Integrity** | CRC32C checksum, monotonic seq | seq tracking via `apply_state` |

**No delta delivery to client.** The backend always publishes complete snapshots (capped). Client never applies incremental deltas to its `Orderbook_Store`. This is correct — the client is a thin render layer, not a book state machine.

### Tape / Analytics

Tape windows are **closed aggregates**, not deltas. Each `TapeWindowV1` is a self-contained observation for a time window. DeltaVolume, CVD, and BarStats are projections from tape — also self-contained per window.

**CVD is cumulative** — each `CVDWindowV1` carries both `delta_volume` (current window) and `cvd` (accumulated total). Client uses the `cvd` field directly, no local accumulation needed.

### Trade Ticks

Raw `TradeTickV1` events are individual, not windowed. Each tick is independent. Client stores in ring buffer (newest-first). No snapshot/delta semantics — pure append.

---

## 3. Tape / Trade Aggregation Review

### Backend Pipeline

```
TradeTickV1 (raw, per-tick)
    ↓ BuildTapeFromTrades
TapeWindowV1 (250ms / 1s / 5s windows)
    ↓ persistClosedWindow → publishDerivedAnalytics
    ├── DeltaVolumeWindowV1 (buy - sell per window)
    ├── CVDWindowV1 (cumulative delta, running sum)
    └── BarStatsWindowV1 (trade count, volume, VWAP, imbalance, burst)
```

### Window Configuration

| Timeframe | Duration | Burst Threshold | Max Open Windows | TTL |
|-----------|----------|-----------------|------------------|-----|
| 250ms | 250ms | 25 trades | 96 | 1h |
| 1s | 1000ms | 80 trades | 96 | 1h |
| 5s | 5000ms | 300 trades | 96 | 1h |

### Client Consumption

Tape events arrive as `MD_Event` and are reduced to:
- **Trade fallback**: If no raw trades available, tape synthesizes a single `Trade_Entry` (price=last_price, qty=total_volume, side=dominant)
- **Analytics**: BarStats/DeltaVol/CVD populate `Analytics_Store` (64-entry ring)

**Assessment:** Pipeline is well-structured. No changes needed.

---

## 4. Retention, Clipping, Cadence & Budgets

### Backend Retention

| Component | Bounded Map Cap | TTL | Sweep Interval |
|-----------|----------------|-----|----------------|
| Order Books | 10,000 | 1h | 1024 ops / 1s |
| Tape Windows | 50,000 | 1h | 1024 ops / 1s |
| CVD State | 50,000 | 1h | 1024 ops / 1s |
| Candle Windows | 50,000 | 1h | 1024 ops / 1s |
| Stats Windows | 50,000 | 1h | 1024 ops / 1s |
| OI State | 50,000 | 1h | 1024 ops / 1s |

### Backend Clipping

| Artifact | Clip Policy | Value |
|----------|-------------|-------|
| Book Levels (in-memory) | Max per side | 1,000 |
| Book Levels (published) | Depth cap | 50 |
| Book Prune Strategy | Furthest-from-mid | Auto |
| Tape Late Tolerance | Reject if too old | 30s |
| Window Cap (concurrent) | Per key namespace | 96 |

### Client Store Budgets

| Store | Location | Cap | Entry Size (est.) | Total Budget |
|-------|----------|-----|-------------------|-------------|
| `Trades_Store` | Per-stream | 256 | 32B | 8 KB |
| `Orderbook_Store` | Per-stream | 50/side | 16B × 100 | 1.6 KB |
| `Stats_Store` | Per-stream | 64 | 64B | 4 KB |
| `Analytics_Store` | Per-stream | 64 | 96B | 6 KB |
| `Candle_Store` | Per-stream | 750 | 88B | 66 KB |
| `Signal_Store` | Per-stream | 400 (8×50) | 176B | 70 KB |
| `Heatmap_Store` | Per-stream | 128×256 | 16B | 512 KB |
| `VPVR_Store` | Per-stream | 200 | 24B | 4.8 KB |
| `DOM_Store` | **Global** | 512+128 | 24B+32B | 16 KB |
| `Footprint_Store` | **Global** | 200×50 | 24B | 240 KB |
| **Per-stream subtotal** | — | — | — | ~673 KB |
| **× 16 streams** | — | — | — | ~10.5 MB |
| **Global stores** | — | — | — | ~256 KB |
| **Grand total** | — | — | — | **~10.8 MB** |

### Cadence Policies

| Data Type | Expected Cadence | Client Action on Silence |
|-----------|-----------------|------------------------|
| Trade ticks | 10–500ms (varies by venue) | Stale after 30s |
| Book snapshots | Rate-limited by backend | Stale after 30s |
| Tape (250ms) | 250ms | Stale after 2s |
| Tape (1s) | 1s | Stale after 5s |
| Tape (5s) | 5s | Stale after 15s |
| Stats | 1s–1d | TF-adaptive staleness |
| OI | Venue-dependent | Adaptive (cadence_hint_ms × 3) |
| Candles | TF-aligned | TF-adaptive staleness |

---

## 5. Client Store Preparation — Issues Found

### Issue 1: DOM_Store and Footprint_Store Are Global Singletons

**Current state:** `Global_Stores` in `components.odin` holds a single `DOM_Store` and `Footprint_Store`. They are reset on stream switch (`stream_views.odin:125-126`, `actions_cell_mutations.odin:59-60`).

**Problem:** In multi-market scenarios (compare mode, multiple panes), only one market's DOM/footprint data can exist at a time. Switching streams destroys accumulated data.

**Resolution for S148:** Move DOM and Footprint stores to per-stream storage (`Market_Stream`). Add `dom` and `footprint` pointers to `Cell_Stores` so widgets can access them through `Widget_Data_Context`.

### Issue 2: DOM_Store and Footprint_Store Are Never Populated

**Current state:** `dom_store_push_trade()` and `footprint_store_push_trade()` are defined but never called from any reducer or event handler.

**Resolution for S148:** Wire `dom_store_push_trade` into the trade reducer (`market_store_reduce_trade`). DOM is TF-independent (accumulates all fills regardless of timeframe). Footprint is NOT wired in the trade reducer because it requires TF context — footprint population will be wired when the footprint widget is built (the widget knows its active TF).

### Issue 3: Cell_Stores Missing DOM and Footprint

**Current state:** `Cell_Stores` has pointers for candle, heatmap, vpvr, trades, orderbook, stats, analytics, session_vpvr, tpo — but no `dom` or `footprint`.

**Resolution for S148:** Add `dom: ^services.DOM_Store` and `footprint: ^services.Footprint_Store` to `Cell_Stores`. Wire resolution in `resolve_stores_for_pane`.

---

## 6. Changes Made

### 6.1 Move DOM_Store and Footprint_Store to Per-Stream

**File:** `client/src/core/layers/market_store.odin`
- Add `dom: services.DOM_Store` and `footprint: services.Footprint_Store` to `Market_Stream` struct

**File:** `client/src/core/app/components.odin`
- Remove `dom` and `footprint` from `Global_Stores`

**File:** `client/src/core/app/stream_views.odin` / `actions_cell_mutations.odin`
- Remove global `dom_store_reset` / `footprint_store_reset` calls (no longer global)

### 6.2 Wire DOM to Trade Reducer

**File:** `client/src/core/layers/market_store_reducers.odin`
- In `market_store_reduce_trade`: call `dom_store_push_trade` with `price_group=0` (uses DOM's internal default of 1.0)
- Footprint NOT wired here — it requires TF context which the reducer doesn't have; will be wired by the footprint widget slice

### 6.3 Add DOM/Footprint to Cell_Stores

**File:** `client/src/core/app/stream_slots.odin`
- Add `dom: ^services.DOM_Store` and `footprint: ^services.Footprint_Store` to `Cell_Stores`
- Wire in `resolve_stores_for_pane`

---

## 7. Widget → Data Plane Mapping (Ready for Vertical Slices)

| Widget | Store(s) | Subject(s) | Reducer | Ready? |
|--------|----------|-----------|---------|--------|
| **Trades Tape** | `Trades_Store` (per-stream) | `marketdata.trade` | `market_store_reduce_trade` | YES |
| **Orderbook** | `Orderbook_Store` (per-stream) | `aggregation.snapshot` | `market_store_reduce_orderbook` | YES |
| **Trade Counter** | `Analytics_Store` (per-stream) | `aggregation.bar_stats` | `market_store_reduce_analytics` | YES |
| **Stats Panel** | `Stats_Store` (per-stream) | `aggregation.stats` | `market_store_reduce_stats` | YES |
| **DOM Ladder** | `DOM_Store` (per-stream) + `Orderbook_Store` | `marketdata.trade` + `aggregation.snapshot` | `market_store_reduce_trade` | YES |
| **Footprint Chart** | `Footprint_Store` (per-stream) | `marketdata.trade` | Widget-level wiring needed | Store ready, reducer TBD |
| **CVD Subplot** | `Analytics_Store` (per-stream) | `aggregation.cvd` | `market_store_reduce_analytics` | YES |
| **Delta Vol Subplot** | `Analytics_Store` (per-stream) | `aggregation.delta_volume` | `market_store_reduce_analytics` | YES |
| **OI Subplot** | `Analytics_Store` (per-stream) | `aggregation.oi` | `market_store_reduce_analytics` | YES |

---

## 8. Backpressure Summary

Delivery backpressure priorities for orderflow subjects:

```
100  marketdata.trade         ← highest (never drop trades)
 90  aggregation.candle
 80  aggregation.stats
 78  aggregation.bar_stats
 76  aggregation.delta_volume
 76  aggregation.cvd
 72  aggregation.oi
 20  marketdata.bookdelta     ← lowest orderflow priority (rebuilt from snapshots)
```

**Policy:** `drop_oldest` under backpressure. Trade ticks have highest priority — UI operator trust depends on trade tape fidelity.

---

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Subjects/streams reviewed | Done (§1) |
| Snapshots/deltas reviewed | Done (§2) |
| Tape/trade aggregation reviewed | Done (§3) |
| Retention, clipping, cadence, budgets defined | Done (§4) |
| Backend prepared (no changes needed) | Done |
| Client stores prepared for DOM | Done (§6) |
| Client stores prepared for Trades | Already ready |
| Client stores prepared for Counter | Already ready |
| Client stores prepared for Stats | Already ready |
| Data plane documented | Done (this report) |
| No undue duplication | Verified |

## Metrics

- Files created: 1 (stage report)
- Files modified: 5 (market_store.odin, market_store_reducers.odin, components.odin, stream_slots.odin, stream_views.odin, actions_cell_mutations.odin)
- Tests added: 3 (trade_reducer_populates_dom_store, dom_store_per_stream_isolation, footprint_store_in_market_stream)
- Breaking changes: none (Global_Stores field removal is internal, no external API)
- Memory impact: DOM_Store (~16KB) + Footprint_Store (~240KB) × 16 streams = ~4.1 MB additional per-stream budget (was ~256KB global)

## Design Decisions

1. **DOM wired in trade reducer, Footprint not** — DOM is TF-independent (accumulates all fills). Footprint needs TF context (`tf_ms` for candle bucketing) which the trade reducer doesn't have. Footprint wiring belongs in the widget slice where TF is resolved.

2. **price_group=0 for DOM** — Lets `DOM_Store` use its internal default (1.0). The actual price_group should be derived from instrument tick size, which will be set when the DOM widget configures its grouping.

3. **Per-stream instead of per-slot** — DOM and Footprint are in `Market_Stream` (not `Stream_View_Slot`) because they accumulate from trade events which flow through the market store, not the stream view layer.
