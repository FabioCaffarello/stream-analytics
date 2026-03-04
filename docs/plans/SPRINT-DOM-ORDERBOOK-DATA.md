# Sprint: DOM + OrderBook Data + Interaction (MMT Parity #1)

**Status:** Completed (2026-03-04)
**Created:** 2026-03-04
**Scope:** Backend data layer, wire contracts, grouping logic, observability. Zero UI changes.
**Constraints:** No soak tests, short gates only, no UI cosmetics, zero new legacy, determinism + boundedness + PROCESSOR_REPLICAS=2.

---

## Current State Summary

| Component | Status | Key File |
|---|---|---|
| OrderBook aggregate (BTree, max 1000/side) | DONE | `internal/core/aggregation/domain/btree_orderbook.go` |
| ApplyDelta/ApplySnapshot + crossed-book detection | DONE | `internal/core/aggregation/domain/btree_orderbook.go` |
| `AggregationSnapshotV1` wire DTO (PascalCase, no json tags) | DONE | `internal/shared/contracts/aggregation_payload_types.go` |
| Processor routing `marketdata.bookdelta` → aggregation | DONE | `internal/actors/aggregation/runtime/processor.go` |
| WS delivery `aggregation.snapshot/<venue>/<symbol>/raw` | DONE | `internal/actors/delivery/runtime/session_protocol.go` |
| Client orderbook store (50 levels/side) | DONE | `client/src/core/services/orderbook_store.odin` |
| Client DOM store (512 fill levels, 128 ring) | DONE | `client/src/core/services/dom_store.odin` |
| Client grouping (auto 10^N, floor-division) | DONE | `client/src/core/app/app_util.odin` |
| 7 Prometheus orderbook metrics | DONE | `internal/shared/metrics/metrics.go:541-589` |
| 17 domain + 10 app + 1 determinism tests | DONE | `internal/core/aggregation/domain/orderbook_test.go` |

### Gaps This Sprint Closes

| Gap | Impact | Fix |
|---|---|---|
| No checksum on snapshots | Cannot verify replay determinism | C1: CRC32C checksum in domain + wire |
| Wire sends up to 1000 levels, client uses 50 | Bandwidth waste ~20x | C1: `DepthCap` on publish path |
| No top-of-book summary in wire | Client re-derives spread/best | C1: `BestBid/Ask/SpreadBPS` in V2 |
| Grouping is client-only, 10^N only | No authority, no tick-size awareness | C2: tick registry + deterministic group fn |
| No bad-level breakdown metric | Cannot diagnose per-venue data quality | C3: `reason`-labeled counter |
| No stale orderbook detection | Silent staleness goes unnoticed | C3: gauge with age since last update |
| No snapshot wire-size tracking | Cannot budget bandwidth | C3: histogram of frame bytes |
| No grouped-book replay proof | Cannot prove REPLICAS=2 equivalence | C4: golden test + property test |

---

## Commit 1: `feat(aggregation): snapshot V2 — checksum, top-of-book, depth cap`

### Objective
Enrich the orderbook snapshot wire DTO with metadata fields needed for deterministic verification, efficient delivery, and DOM data quality. Maintain V1 backward compatibility.

### Types to Create/Modify

**File: `internal/shared/contracts/aggregation_payload_types.go`**
```go
// AggregationSnapshotV2 is the enriched wire DTO for aggregation.snapshot v2.
// V1 clients ignore unknown fields (standard JSON forward-compat).
type AggregationSnapshotV2 struct {
    Venue      string
    Instrument string
    Seq        int64
    Bids       []AggregationOrderBookLevelV1   // reuse V1 level type
    Asks       []AggregationOrderBookLevelV1
    // --- V2 additions ---
    BestBidPrice float64   // top bid price (0 if empty)
    BestAskPrice float64   // top ask price (0 if empty)
    SpreadBPS    float64   // (bestAsk - bestBid) / midPrice * 10000; -1 if empty
    Checksum     uint32    // CRC32C of canonical level representation
    TsIngestMs   int64     // server wall-clock when snapshot was produced
    BidCount     int       // total raw bid levels before depth cap
    AskCount     int       // total raw ask levels before depth cap
    DepthCap     int       // max levels per side in this payload (0 = uncapped)
    Version      int       // always 2
}
```

**File: `internal/core/aggregation/domain/orderbook_checksum.go`** (NEW)
```go
// Checksum computes a deterministic CRC32C of the orderbook state.
// Algorithm:
//   1. Write all bids desc by price: for each level write price(f64 LE) + qty(f64 LE)
//   2. Write separator byte 0xFF
//   3. Write all asks asc by price: same encoding
//   4. Return CRC32C of the buffer
//
// Invariant: same levels in same order → same checksum, always.
// Uses math.Float64bits for bit-exact encoding.
func (b *OrderBook) Checksum() uint32
```

**File: `internal/core/aggregation/domain/orderbook.go`**
```go
// TopN returns at most n levels per side. If n <= 0, returns all.
// Bids: descending by price. Asks: ascending by price.
func (b *OrderBook) TopN(n int) (bids, asks []Level)
```

### Changes to Existing Code

**`internal/core/aggregation/app/update_orderbook.go`**
- `UpdateResponse` gets new fields: `Checksum uint32`, `BestBidPrice float64`, `BestAskPrice float64`, `SpreadBPS float64`
- After `ApplyDelta`/`ApplySnapshot`, compute checksum and top-of-book
- `publishSnapshot` builds V2 DTO with `DepthCap` from config (default 50 for WS publish, 0 for storage save)

**`internal/shared/config/schema.go`**
- Add `WsSnapshotDepthCap int` to `ProcessorRTPublishConfig` (default 50, validated: 10–1000)

**`internal/shared/contracts/codec_registry.go`** (or equivalent)
- Register `aggregation.snapshot` v2 codec

### Proto

**File: `proto/aggregation/v2/snapshot_v2.proto`** (NEW)
```proto
syntax = "proto3";
package aggregation.v2;

message OrderBookLevelV1 {
  double price    = 1;
  double quantity = 2;
}

message OrderBookSnapshotV2 {
  string venue        = 1;
  string instrument   = 2;
  int64  seq          = 3;
  repeated OrderBookLevelV1 bids = 4;
  repeated OrderBookLevelV1 asks = 5;
  double best_bid_price  = 6;
  double best_ask_price  = 7;
  double spread_bps      = 8;
  uint32 checksum        = 9;
  int64  ts_ingest_ms    = 10;
  int32  bid_count       = 11;
  int32  ask_count       = 12;
  int32  depth_cap       = 13;
  int32  version         = 14;
}
```

### Invariants
- `SNAP-V2-1`: `Checksum` MUST be identical for two `OrderBook` instances with identical level sets, regardless of delta application order.
- `SNAP-V2-2`: `DepthCap > 0` ⇒ `len(Bids) <= DepthCap` AND `len(Asks) <= DepthCap`.
- `SNAP-V2-3`: `BestBidPrice` = `Bids[0].Price` when `len(Bids) > 0`, else `0`.
- `SNAP-V2-4`: `SpreadBPS` = `(BestAskPrice - BestBidPrice) / midPrice * 10000` when both sides exist, else `-1`.
- `SNAP-V2-5`: V1 clients that ignore unknown JSON fields MUST NOT break (forward-compat).
- `SNAP-V2-6`: `Version` field MUST be `2`.

### Tests (`-short`)

| Test | File | What |
|---|---|---|
| `TestOrderBook_Checksum_Deterministic` | `domain/orderbook_checksum_test.go` | Same levels → same CRC32C |
| `TestOrderBook_Checksum_DifferentOrder_SameResult` | same | Apply deltas in different order → same final checksum |
| `TestOrderBook_Checksum_EmptyBook` | same | Empty book → fixed sentinel checksum |
| `TestOrderBook_TopN_Bounded` | `domain/orderbook_test.go` | TopN(25) on 1000-level book returns exactly 25/side |
| `TestOrderBook_TopN_LessThanN` | same | TopN(100) on 10-level book returns 10/side |
| `TestSnapshotV2_RoundTrip_JSON` | `contracts/aggregation_payload_test.go` | Marshal/unmarshal V2 preserves all fields |
| `TestSnapshotV2_BackwardCompat_V1Client` | same | V2 JSON can be unmarshaled into V1 struct (unknown fields ignored) |
| `TestUpdateOrderBook_V2_Checksum_Populated` | `app/update_orderbook_test.go` | Execute returns non-zero checksum |
| `TestUpdateOrderBook_V2_DepthCap_Respected` | same | With DepthCap=25, published snapshot has ≤25 levels/side |

---

## Commit 2: `feat(shared): tick-size registry + deterministic grouped-book function`

### Objective
Provide a shared, deterministic tick-size registry and a pure function for grouping orderbook levels into price buckets. This is a DOMAIN function (no I/O, no actors) usable by both backend and client.

### Types to Create

**File: `internal/shared/ticksize/registry.go`** (NEW package)
```go
package ticksize

// TickSpec defines the tick size for a price range.
type TickSpec struct {
    MinPrice float64 // inclusive lower bound
    MaxPrice float64 // exclusive upper bound (0 = infinity)
    TickSize float64 // price group size
}

// Registry holds per-venue tick tables with a global fallback.
type Registry struct {
    venues   map[string][]TickSpec  // venue → sorted specs by MinPrice asc
    fallback func(price float64) float64
}

// NewRegistry creates a registry with the default auto-group fallback.
func NewRegistry() *Registry

// RegisterVenue adds a tick table for a venue. Specs must be sorted by MinPrice asc.
func (r *Registry) RegisterVenue(venue string, specs []TickSpec) *problem.Problem

// GroupSizeForPrice returns the tick-group size for a given venue + price.
// If no venue match, falls back to AutoGroupSize(price).
func (r *Registry) GroupSizeForPrice(venue string, price float64) float64

// AutoGroupSize computes 10^floor(log10(price * 0.0001)).
// Matches client logic in orderbook_auto_price_group.
// BTC@90000 → 10, ETH@3000 → 1, DOGE@0.08 → 0.0001.
func AutoGroupSize(price float64) float64
```

**Default tick tables (hardcoded, sourced from exchange docs):**

| Venue | Price Range | Tick Size |
|---|---|---|
| binance | 0–0.001 | 0.000001 |
| binance | 0.001–0.01 | 0.00001 |
| binance | 0.01–0.1 | 0.0001 |
| binance | 0.1–1 | 0.001 |
| binance | 1–10 | 0.01 |
| binance | 10–100 | 0.1 |
| binance | 100–1000 | 1 |
| binance | 1000–10000 | 10 |
| binance | 10000–100000 | 10 |
| binance | 100000+ | 100 |
| bybit | (same pattern as binance for crypto perpetuals) |
| coinbase | 0–1 | 0.0001 |
| coinbase | 1–100 | 0.01 |
| coinbase | 100–10000 | 0.01 |
| coinbase | 10000+ | 1 |
| *fallback* | any | AutoGroupSize(price) |

> NOTE: These are *default display grouping ticks*, not exchange-native order ticks. They define the minimum useful visual grouping for a DOM ladder. Can be overridden via config.

**File: `internal/core/aggregation/domain/grouped_book.go`** (NEW)
```go
package domain

// GroupedLevel is a price bucket with aggregated quantity.
type GroupedLevel struct {
    Price             Price    // bucket floor price
    TotalQuantity     Quantity // sum of all raw quantities in bucket
    LevelCount        int      // number of raw levels collapsed
    CumulativeQuantity Quantity // running sum (filled by caller after grouping)
}

// GroupLevels groups raw levels into price buckets using floor-division.
// Levels MUST be pre-sorted (bids desc, asks asc).
// Returns at most maxRows grouped levels, closest to best price.
// Algorithm:
//   1. For each raw level: bucket = floor(price / groupSize) * groupSize
//   2. Accumulate into bucket (linear scan, no map — deterministic)
//   3. Truncate to maxRows closest to best (bids: highest prices, asks: lowest prices)
//
// Invariant: same input levels + same groupSize + same maxRows → identical output, always.
func GroupLevels(levels []Level, groupSize float64, maxRows int) []GroupedLevel

// FillCumulative fills CumulativeQuantity as running sum from best to worst.
func FillCumulative(grouped []GroupedLevel)
```

### Invariants
- `GRP-1`: `GroupLevels` is a PURE function — no allocation beyond the output slice, no global state.
- `GRP-2`: Floor-division: `bucket = math.Floor(price / groupSize) * groupSize`. This is deterministic for IEEE-754 f64.
- `GRP-3`: `len(result) <= maxRows`.
- `GRP-4`: `sum(result[i].LevelCount) <= len(input)`.
- `GRP-5`: Tick registry lookup is O(log N) per venue (binary search on sorted specs).
- `GRP-6`: `AutoGroupSize` matches the client's `orderbook_auto_price_group` exactly — same formula, same edge cases.

### Tests (`-short`)

| Test | File | What |
|---|---|---|
| `TestAutoGroupSize_KnownPrices` | `ticksize/registry_test.go` | BTC→10, ETH→1, DOGE→0.0001, SOL→0.01 |
| `TestAutoGroupSize_EdgeCases` | same | price=0→1, price<0→1, price=0.0001→0.00000001 |
| `TestRegistry_VenueLookup` | same | binance BTC@90000→10, coinbase ETH@3000→0.01 |
| `TestRegistry_Fallback` | same | unknown venue → AutoGroupSize |
| `TestGroupLevels_Deterministic` | `domain/grouped_book_test.go` | Shuffle input (within same sort) → same output |
| `TestGroupLevels_FloorDivision` | same | Price 90001 with group 10 → bucket 90000 |
| `TestGroupLevels_MaxRows` | same | 100 groups, maxRows=25 → exactly 25 |
| `TestGroupLevels_EmptyInput` | same | Empty → empty |
| `TestGroupLevels_SingleLevel` | same | One bid → one grouped level |
| `TestGroupLevels_AllSameBucket` | same | 50 levels at same price ±tick → one bucket |
| `TestFillCumulative` | same | Running sum is monotonic |
| `TestGroupLevels_MatchesClient` | same | Compare Go output with expected Odin output for known fixture |

---

## Commit 3: `feat(metrics): per-venue orderbook data quality + stale detection`

### Objective
Add granular data-quality metrics for orderbook health. Enable alerting on bad data, staleness, and wire efficiency.

### New Metrics

**File: `internal/shared/metrics/metrics.go`** — add to existing orderbook section:

| Variable | Metric Name | Type | Labels | Description |
|---|---|---|---|---|
| `MROrderBookBadLevelTotal` | `mr_orderbook_bad_level_total` | CounterVec | `venue`, `instrument_bucket`, `reason` | Rejected levels (reason: `nan`, `inf`, `neg_price`, `neg_qty`, `zero_price`) |
| `MROrderBookStaleDurationSeconds` | `mr_orderbook_stale_duration_seconds` | GaugeVec | `venue`, `instrument_bucket` | Seconds since last successful update (0 = fresh) |
| `MROrderBookPublishDepth` | `mr_orderbook_publish_depth` | HistogramVec | `venue`, `side` | Levels per side in published snapshot (buckets: 5,10,25,50,100,200,500,1000) |
| `MROrderBookWireBytes` | `mr_orderbook_wire_bytes` | HistogramVec | `venue` | Snapshot frame size in bytes (buckets: 256,512,1K,2K,4K,8K,16K,32K) |
| `MROrderBookChecksumMismatchTotal` | `mr_orderbook_checksum_mismatch_total` | CounterVec | `venue`, `instrument_bucket` | Checksum mismatches on consecutive snapshots with same seq (0 expected) |

Helper functions:
```go
func IncMROrderBookBadLevel(venue, instrument, reason string)
func SetMROrderBookStaleDuration(venue, instrument string, seconds float64)
func ObserveMROrderBookPublishDepth(venue, side string, depth int)
func ObserveMROrderBookWireBytes(venue string, bytes int)
func IncMROrderBookChecksumMismatch(venue, instrument string)
```

### Changes to Existing Code

**`internal/core/aggregation/domain/btree_orderbook.go`**
- `validateLevel` currently returns `*problem.Problem` on any invalid level. Enhance to include `reason` in the problem detail:
  ```go
  func validateLevel(l levelNode) *problem.Problem {
      if math.IsNaN(float64(l.Price)) { return problem.New("invalid_level", "nan_price") }
      if math.IsInf(float64(l.Price), 0) { return problem.New("invalid_level", "inf_price") }
      if l.Price <= 0 { return problem.New("invalid_level", "neg_price") }
      // ... same for Quantity
  }
  ```
- Return `ValidationFailure` with `Detail.Reason` field so the app layer can emit the labeled metric.

**`internal/core/aggregation/app/update_orderbook.go`**
- In `Execute`, on `validateLevel` failure → `metrics.IncMROrderBookBadLevel(venue, instrument, reason)`
- After publish → `metrics.ObserveMROrderBookPublishDepth(venue, "bid", len(bids))` + same for asks
- After marshal → `metrics.ObserveMROrderBookWireBytes(venue, len(payload))`

**`internal/actors/aggregation/runtime/processor.go`**
- Add stale detection to the existing `SnapshotTick` handler:
  ```go
  // On every SnapshotTick (every 200ms-1s):
  for bookID, lastUpdateTime := range activeOrderBooks {
      age := time.Since(lastUpdateTime)
      metrics.SetMROrderBookStaleDuration(bookID.Venue, bookID.Instrument, age.Seconds())
  }
  ```
- Track `lastUpdateAt map[BookID]time.Time` — set on every successful `handleBookDelta`.

### Label Cardinality

All `instrument_bucket` labels MUST use the existing `metrics.InstrumentBucket(instrument)` function which maps to a bounded set (e.g., `BTCUSDT`, `ETHUSDT`, `OTHER`). This prevents cardinality explosion.

`reason` label has exactly 5 values: `nan`, `inf`, `neg_price`, `neg_qty`, `zero_price`. Bounded.

### Invariants
- `MET-1`: All new metrics use `instrument_bucket`, never raw instrument. Max cardinality = |venues| × |buckets| × |reasons| ≈ 6 × 10 × 5 = 300.
- `MET-2`: Stale gauge resets to 0 on successful update.
- `MET-3`: `checksum_mismatch_total` should be 0 in production — any non-zero triggers alert.

### Tests (`-short`)

| Test | File | What |
|---|---|---|
| `TestBadLevelMetric_NaN` | `app/update_orderbook_test.go` | NaN price → counter incremented with reason=nan |
| `TestBadLevelMetric_NegativePrice` | same | Negative price → reason=neg_price |
| `TestBadLevelMetric_InfQuantity` | same | +Inf qty → reason=inf |
| `TestStaleDuration_UpdateResetsToZero` | `runtime/processor_stale_test.go` | After successful delta, stale gauge = 0 |
| `TestStaleDuration_IncreasesWithoutUpdate` | same | No deltas for 5s → gauge ≈ 5 |
| `TestPublishDepth_Histogram` | `app/update_orderbook_test.go` | 50-level publish → observed in depth histogram |
| `TestMetricCardinality_Bounded` | `metrics/metrics_test.go` | All new metrics have bounded label sets |

---

## Commit 4: `test+docs: determinism proof under REPLICAS=2, contracts, DoD`

### Objective
Prove the entire pipeline is deterministic under multi-replica conditions. Update architecture docs. Define DoD with metric thresholds.

### Golden Replay Tests

**File: `internal/core/aggregation/domain/orderbook_determinism_test.go`** (NEW)

```go
// TestOrderBook_Determinism_TwoInstances_IdenticalChecksum
// Creates two independent OrderBook instances.
// Applies the same sequence of 1000 deltas to both.
// Asserts: Checksum(), Bids(), Asks() are byte-identical.

// TestOrderBook_Determinism_SnapshotThenDeltas
// Instance A: apply snapshot + 100 deltas.
// Instance B: apply same snapshot + same 100 deltas.
// Assert: identical state.

// TestOrderBook_Determinism_CrossedBookRecovery
// Both instances hit crossed book → clear → resume.
// Assert: identical post-recovery state.

// TestGroupedBook_Determinism_SameInputSameOutput
// Group same 500-level book with groupSize=10, maxRows=25.
// Run 100 times. Assert: all outputs identical.

// TestGroupedBook_Determinism_MatchesClientLogic
// Known fixture: 20 levels, groupSize=10, maxRows=10.
// Assert: Go output matches expected Odin output (hardcoded expected values).
```

**File: `internal/core/aggregation/app/update_orderbook_determinism_test.go`** (NEW)

```go
// TestUpdateOrderBook_Determinism_TwoUseCases_SameChecksum
// Two independent use case instances (different publisher/store mocks).
// Same delta sequence. Assert: published V2 snapshots have identical checksums.

// TestUpdateOrderBook_Determinism_DepthCap_SameTopN
// 1000-level book, DepthCap=50. Assert: same top-50 bids/asks regardless of internal pruning order.
```

**File: `internal/actors/aggregation/runtime/processor_determinism_test.go`** (EXTEND)

```go
// TestProcessor_Determinism_BookDelta_Replay
// Send 500 bookdelta envelopes to processor.
// Capture all published snapshots.
// Restart processor, replay same envelopes.
// Assert: identical snapshot sequence (seq, checksum, levels).
```

### Edge Case Tests

| Test | Scenario |
|---|---|
| Empty book → V2 checksum | Should produce a fixed sentinel (e.g., CRC32C of 0xFF byte) |
| Single level both sides | Checksum, spread, grouping all valid |
| Alternating snapshot/delta | 50 snapshots + 50 deltas interleaved → correct state |
| Rapid crossed→heal (10 cycles) | State converges correctly, events emitted correctly |
| DepthCap=1 | Only best bid/ask in payload |
| GroupSize = 0 | Treated as 1.0 (no grouping), not panic |
| GroupSize = price (extreme) | All levels collapse to one bucket |
| maxRows = 0 | Returns all groups (no limit) |

### Docs

**File: `docs/adrs/ADR-0020-snapshot-v2-grouped-book.md`** (NEW)

```
Title: Enriched Orderbook Snapshot V2 and Deterministic Tick Grouping
Status: Accepted
Context: DOM and OrderBook widgets need richer metadata (checksum, spread, depth cap)
         and consistent tick-based grouping across all consumers.
Decision: 1. Extend AggregationSnapshotV1 → V2 with backward-compat fields.
          2. Add shared tick-size registry in internal/shared/ticksize.
          3. Add pure GroupLevels function in aggregation/domain.
          4. All grouping uses floor-division: bucket = floor(price/group) * group.
Consequences: V1 clients unaffected (unknown JSON fields ignored).
              Checksum enables replay verification without full level comparison.
              Tick registry is static defaults; config override planned for later.
```

**Update: `docs/architecture/orderbook.md`**
- Add V2 contract fields to "Contracts" section
- Add grouped-book to "Outputs" data plane
- Update implementation matrix: checksum=DONE, tick registry=DONE, grouped book=DONE
- Update observability section with new metrics

**Update: `docs/contracts/delivery-ws.md`**
- Add V2 snapshot fields to frame documentation
- Document `depth_cap` query parameter (future)

---

## Definition of Done (DoD)

### Metrics Thresholds (Production)

| Metric | Threshold | Meaning |
|---|---|---|
| `mr_orderbook_checksum_mismatch_total` | **= 0** | Any non-zero → replay integrity broken → P0 alert |
| `mr_orderbook_bad_level_total` | **< 10/min** per venue | Occasional NaN from exchange is tolerable; sustained = bug |
| `mr_orderbook_stale_duration_seconds` | **< 30s** per active book | > 30s → reconnect or feed dead |
| `mr_orderbook_crossed_total` | **< 5/hour** per venue | Occasional from exchange glitches; sustained = parser bug |
| `mr_orderbook_spread_bps` | **> 0** for active books | Negative spread = crossed book |
| `mr_orderbook_publish_depth` p99 | **<= DepthCap** | Wire never exceeds configured cap |
| `mr_orderbook_wire_bytes` p99 | **< 16KB** per snapshot at depth_cap=50 | Budget for WS frame size |
| `mr_orderbook_prune_total` | **informational** | Tracks cap enforcement activity |

### Test Gate (All Must Pass)

```
make test MODULE=./internal/shared -short
make test MODULE=./internal/core/aggregation -short
make test MODULE=./internal/actors -short
```

Specific test functions that MUST exist and pass:

#### Domain
- [ ] `TestOrderBook_Checksum_Deterministic`
- [ ] `TestOrderBook_Checksum_DifferentOrder_SameResult`
- [ ] `TestOrderBook_Checksum_EmptyBook`
- [ ] `TestOrderBook_TopN_Bounded`
- [ ] `TestOrderBook_TopN_LessThanN`
- [ ] `TestOrderBook_Determinism_TwoInstances_IdenticalChecksum`
- [ ] `TestOrderBook_Determinism_SnapshotThenDeltas`
- [ ] `TestOrderBook_Determinism_CrossedBookRecovery`

#### Grouped Book
- [ ] `TestGroupLevels_Deterministic`
- [ ] `TestGroupLevels_FloorDivision`
- [ ] `TestGroupLevels_MaxRows`
- [ ] `TestGroupLevels_EmptyInput`
- [ ] `TestGroupLevels_SingleLevel`
- [ ] `TestGroupLevels_AllSameBucket`
- [ ] `TestFillCumulative`
- [ ] `TestGroupLevels_MatchesClient`
- [ ] `TestGroupedBook_Determinism_SameInputSameOutput`
- [ ] `TestGroupedBook_Determinism_MatchesClientLogic`

#### Tick Registry
- [ ] `TestAutoGroupSize_KnownPrices`
- [ ] `TestAutoGroupSize_EdgeCases`
- [ ] `TestRegistry_VenueLookup`
- [ ] `TestRegistry_Fallback`

#### App / Use Case
- [ ] `TestSnapshotV2_RoundTrip_JSON`
- [ ] `TestSnapshotV2_BackwardCompat_V1Client`
- [ ] `TestUpdateOrderBook_V2_Checksum_Populated`
- [ ] `TestUpdateOrderBook_V2_DepthCap_Respected`
- [ ] `TestUpdateOrderBook_Determinism_TwoUseCases_SameChecksum`
- [ ] `TestUpdateOrderBook_Determinism_DepthCap_SameTopN`

#### Metrics
- [ ] `TestBadLevelMetric_NaN`
- [ ] `TestBadLevelMetric_NegativePrice`
- [ ] `TestBadLevelMetric_InfQuantity`
- [ ] `TestStaleDuration_UpdateResetsToZero`
- [ ] `TestStaleDuration_IncreasesWithoutUpdate`
- [ ] `TestPublishDepth_Histogram`
- [ ] `TestMetricCardinality_Bounded`

#### Actor / Integration
- [ ] `TestProcessor_Determinism_BookDelta_Replay`

### Docs Gate
- [ ] `docs/adrs/ADR-0020-snapshot-v2-grouped-book.md` exists and is non-empty
- [ ] `docs/architecture/orderbook.md` implementation matrix updated
- [ ] `docs/contracts/delivery-ws.md` V2 fields documented
- [ ] All new public functions have godoc comments

### Negative Gate (Things That Must NOT Happen)
- [ ] No new files under `zip/`
- [ ] No changes to client rendering code (widgets, layers)
- [ ] No soak test harnesses
- [ ] No UI cosmetic changes
- [ ] No new external dependencies (only stdlib + existing deps)
- [ ] No `fmt.Sprintf` / `fmt.Errorf` in hot path (aggregation domain/app)
- [ ] No unbounded label cardinality in metrics
- [ ] No map iteration in deterministic paths (use sorted slices or btree iteration)
- [ ] All `replace` directives present in any modified go.mod

---

## Commit Sequence Summary

| # | Scope | Files Created | Files Modified | Tests |
|---|---|---|---|---|
| C1 | Snapshot V2 contract | `domain/orderbook_checksum.go`, `proto/aggregation/v2/snapshot_v2.proto` | `contracts/aggregation_payload_types.go`, `app/update_orderbook.go`, `config/schema.go` | 9 |
| C2 | Tick registry + grouped book | `shared/ticksize/registry.go`, `domain/grouped_book.go` | (none) | 12 |
| C3 | Data quality metrics | `runtime/processor_stale_test.go` | `metrics/metrics.go`, `domain/btree_orderbook.go`, `app/update_orderbook.go`, `runtime/processor.go` | 7 |
| C4 | Determinism proof + docs | `domain/orderbook_determinism_test.go`, `app/update_orderbook_determinism_test.go`, `docs/adrs/ADR-0020-*.md` | `docs/architecture/orderbook.md`, `docs/contracts/delivery-ws.md`, `runtime/processor_determinism_test.go` | 10+ |

**Total: ~38 tests, 4 commits, 0 soak, 0 UI changes.**

---

## Execution Report (Before/After)

### Before → After (Data Contract + Runtime)

| Item | Before | After |
|---|---|---|
| Snapshot contract | `aggregation.snapshot` with base levels only | `aggregation.snapshot` V2 payload shape with `best_bid_price`, `best_ask_price`, `spread_bps`, `checksum`, `ts_ingest_ms`, `bid_count`, `ask_count`, `depth_cap`, `version` |
| Publish boundedness | Snapshot payload could ship full in-memory depth | Publish path capped with `ws_snapshot_depth_cap` (default 50, validated 10..1000) |
| Determinism in grouping labels | Instrument bucket classification used map iteration path | Deterministic ordered rule evaluation (no map iteration in bucket matching path) |
| Replay evidence | No explicit processor replay parity test for orderbook snapshots | `TestProcessor_Determinism_BookDelta_Replay` with fixed ingest clock and stable payload comparison |
| Proto registry | No V2 entry for snapshot | `proto/registry.json` updated with `aggregation.snapshot` version 2 mapping |

### New/Extended Observability (OrderBook Data Quality)

Added and validated:

- `mr_orderbook_bad_level_total{venue,instrument_bucket,reason}`
- `mr_orderbook_gap_total{venue,instrument_bucket}`
- `mr_orderbook_drops_total{venue,instrument_bucket,reason}`
- `mr_orderbook_stale_duration_seconds{venue,instrument_bucket}`
- `mr_orderbook_publish_depth{venue,side}`
- `mr_orderbook_wire_bytes{venue}`
- `mr_orderbook_checksum_mismatch_total{venue,instrument_bucket}`

### Invariants Proven (with tests)

- `SNAP-V2-DET`: Same book state => same checksum (`orderbook_checksum_test.go`)
- `SNAP-V2-BOUND`: publish depth cap respected in emitted snapshots (`update_orderbook_test.go`)
- `GROUP-DET`: grouped-book output deterministic for same sorted input (`grouped_book_test.go`)
- `PROC-REPLAY-DET`: replaying identical bookdelta stream yields identical snapshot payload sequence (`processor_determinism_replay_test.go`)
- `MET-CARD-BOUND`: bad-level metric reason/instrument labels remain bounded (`metrics_test.go`)

### Fallback Removal Criteria

Remove compatibility fallback paths only when all criteria below hold together:

1. `mr_orderbook_checksum_mismatch_total` remains `0` in production for 14 consecutive days.
2. `mr_orderbook_wire_bytes` p99 stays within budget at configured `depth_cap` (<=16KB target for cap=50).
3. No client parse failures for `aggregation.snapshot` V2 fields across native + wasm builds/releases.
4. Contract checks pass with only canonical V2 encode/decode path enabled (no legacy conversion fallback).

## IQ Evidence (2026-03-04)

Run: `artifacts/iq/20260304-171040`

Operational verification executed with:

- `make up PROCESSOR_REPLICAS=2`
- Playwright MCP against `http://localhost:8090/`
- Gates: `make test-short`, `make lint`, `make proto-check`, `make docs-check`, `make -C client doctor`, `make -C client build-native`, `make -C client build-wasm`, `make -C client check-core`

Result summary:

- WS + HELLO/ACK: PASS (`artifacts/iq/20260304-171040/playwright.console.log`)
- Snapshot V2 observed on `aggregation.snapshot/binance/BTCUSDT/raw`: PASS (`artifacts/iq/20260304-171040/playwright.ws-diagnostic.json`)
- DOM/OrderBook activity (trades + orderbook stream events): PASS (`artifacts/iq/20260304-171040/playwright.ws-diagnostic.json`)
- Console errors/warnings: PASS (0/0)
- Retry behavior: PASS (`reconnect_attempt count=1`, no retry storm)
- Parsing/deserialize errors + drops/backpressure: PASS (`artifacts/iq/20260304-171040/metrics-summary.txt`, `artifacts/iq/20260304-171040/consumer.counters.summary.txt`)
- `mr_orderbook_checksum_mismatch_total` and `mr_orderbook_wire_bytes` (p95/p99) on **server** endpoint: NOT EXPOSED (`artifacts/iq/20260304-171040/server.metrics.prom`)

IQ verdict for this run: **FAIL (strict thresholds)** due to missing orderbook observability metrics on server endpoint, despite healthy runtime behavior.

## IQ Evidence Rerun (2026-03-04)

Run: `artifacts/iq/20260304-172513`

Change under validation:

- `fix(shared): expose orderbook v2 metrics on server registry`

Validation summary:

- Gates: PASS (`make test-short`, `make lint`, `make proto-check`, `make docs-check`) — see `artifacts/iq/20260304-172513/gates/summary.txt`
- WS + HELLO/ACK: PASS (`artifacts/iq/20260304-172513/playwright.console.log`)
- Snapshot V2 + streams ativos (orderbook + trades): PASS (`artifacts/iq/20260304-172513/playwright.ws-diagnostic.json`)
- Console errors/warnings: PASS (0/0)
- Parsing/deserialize + drops/backpressure: PASS (`artifacts/iq/20260304-172513/metrics-summary.txt`)
- `mr_orderbook_checksum_mismatch_total` now exposed on server metrics: PASS
- `mr_orderbook_wire_bytes` histogram now exposed on server metrics (`_bucket/_sum/_count`): PASS

Evidence for required metric series:

- `artifacts/iq/20260304-172513/metrics.orderbook-series.txt`
- `artifacts/iq/20260304-172513/server.metrics.prom`

IQ verdict for rerun: **PASS**

---

## CODEX Execution Checklist

```
[ ] C1: Create domain/orderbook_checksum.go with Checksum() method
[ ] C1: Add TopN(n) to OrderBook interface and implementation
[ ] C1: Create AggregationSnapshotV2 in contracts
[ ] C1: Update UpdateOrderBookFromEvents to produce V2
[ ] C1: Add WsSnapshotDepthCap to config schema
[ ] C1: Create proto/aggregation/v2/snapshot_v2.proto
[ ] C1: Write 9 tests, run `make test MODULE=./internal/core/aggregation -short`
[ ] C1: Commit "feat(aggregation): snapshot V2 — checksum, top-of-book, depth cap"

[ ] C2: Create internal/shared/ticksize/registry.go
[ ] C2: Implement AutoGroupSize matching client logic
[ ] C2: Implement default venue tick tables (binance, bybit, coinbase)
[ ] C2: Create domain/grouped_book.go with GroupLevels + FillCumulative
[ ] C2: Write 12 tests, run `make test MODULE=./internal/shared -short && make test MODULE=./internal/core/aggregation -short`
[ ] C2: Commit "feat(shared): tick-size registry + deterministic grouped-book function"

[ ] C3: Add 5 new metrics to shared/metrics/metrics.go
[ ] C3: Enhance validateLevel to return typed reason
[ ] C3: Wire bad-level counter in update_orderbook.go Execute
[ ] C3: Add stale detection in processor.go SnapshotTick handler
[ ] C3: Wire publish-depth and wire-bytes histograms
[ ] C3: Write 7 tests, run `make test MODULE=./internal/shared -short && make test MODULE=./internal/core/aggregation -short`
[ ] C3: Commit "feat(metrics): per-venue orderbook data quality + stale detection"

[ ] C4: Create domain/orderbook_determinism_test.go (5 tests)
[ ] C4: Create app/update_orderbook_determinism_test.go (2 tests)
[ ] C4: Extend runtime/processor_determinism_test.go (1 test)
[ ] C4: Write edge-case tests (8+ scenarios)
[ ] C4: Create docs/adrs/ADR-0020-snapshot-v2-grouped-book.md
[ ] C4: Update docs/architecture/orderbook.md implementation matrix
[ ] C4: Update docs/contracts/delivery-ws.md with V2 fields
[ ] C4: Run full gate: `make test MODULE=./internal/shared -short && make test MODULE=./internal/core/aggregation -short && make test MODULE=./internal/actors -short`
[ ] C4: Commit "test+docs: determinism proof under REPLICAS=2, contracts, DoD"

[ ] FINAL: Verify all DoD metric thresholds documented
[ ] FINAL: Verify negative gate (no zip/, no UI, no soak, no unbounded labels)
[ ] FINAL: `make lint` passes
```
