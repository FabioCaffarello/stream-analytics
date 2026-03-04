# Sprint: Trades + Tape Data & Interaction (MMT Parity #2)

**Status:** Planned
**Created:** 2026-03-04
**Scope:** Trade data-quality observability, tape aggregation domain, wire contracts, client wiring. Zero UI redesign.
**Constraints:** No soak tests, short gates only, no UI cosmetics, zero new legacy, determinism + boundedness + PROCESSOR_REPLICAS=2, max 5 commits.

---

## Current State Summary

| Component | Status | Key File |
|---|---|---|
| TradeTickV1 proto + domain model | DONE | `proto/marketdata/v1/trade.proto`, `internal/core/marketmodel/events.go` |
| 6 exchange parsers (trade path) | DONE | `internal/adapters/exchange/*/parser.go` |
| IngestMarketData → JetStream publish | DONE | `internal/core/marketdata/app/ingest.go` |
| Processor routing: trade → BuildCandle | DONE | `internal/actors/aggregation/runtime/processor.go:476` |
| BuildCandleFromEvents (9 TFs, multi-TF rollup) | DONE | `internal/core/aggregation/app/build_candle.go` |
| WS delivery: `marketdata.trade/<venue>/<symbol>/raw` | DONE | `internal/actors/delivery/runtime/session_delivery.go` |
| Subject registry entry: `marketdata.trade.v1` | DONE | `docs/contracts/subject-registry.yaml` |
| Payload codec: JSON + Proto round-trip | DONE | `internal/shared/contracts/payload_registry.go` |
| Client: `Trades_Store` (ring buf 256) | DONE | `client/src/core/services/trades_store.odin` |
| Client: `trades_widget` (tape table + whale detect) | DONE | `client/src/core/widgets/trades_widget.odin` |
| Client: `trade_counter` (buy/sell bar chart) | DONE | `client/src/core/widgets/trade_counter.odin` |
| Client: `parse_trade` + `handle_trade_event` | DONE | `client/src/core/services/message_parser.odin`, `client/src/core/app/marketdata.odin` |
| **Trade data-quality metrics (bad values, OOO, drops)** | **GAP** | NO equivalent of `mr_orderbook_bad_level_total` for trades |
| **Tape aggregation (rate, imbalance, burst detect)** | **GAP** | Trades only feed candle — no standalone tape aggregation |
| **Trade wire-size tracking** | **GAP** | `ws_bytes_out_total{channel}` exists but no trade-specific histogram |
| **Trade parser validation (price>0, size>0)** | **WEAK** | `marketmodel.Trade.Validate()` exists but parsers do NOT call it pre-publish |

### Gaps This Sprint Closes

| Gap | Impact | Fix |
|---|---|---|
| No trade-specific data-quality metrics | Cannot diagnose per-venue bad trades, drops, OOO | C1: 5 Prometheus counters + integration in ingest path |
| No parser-level validation guard | Bad trades (price=0, size=NaN) enter JetStream | C1: Call `Trade.Validate()` in all 6 parsers |
| No tape aggregation (rate/imbalance/burst) | Client has no server-side trade flow metrics | C2: `TapeWindowV1` domain + `BuildTapeFromTrades` use case |
| No tape wire contract | Cannot deliver tape aggregation to client | C3: Proto + registry + codec + processor routing |
| No trade wire-size tracking | Cannot budget trade bandwidth per venue | C3: `mr_trade_wire_bytes` histogram |
| Client tape widget shows raw trades only | No rate/imbalance/burst indicator | C4: Client wiring for `aggregation.tape` channel |
| No IQ tests for trade quality | No machine-checked DoD | C5: Metrics exposure test + determinism tests |

---

## Commit 1: `feat(shared): trade data-quality metrics + parser validation hardening`

### Objective
Close the trade data-quality observability gap. Add trade-specific Prometheus counters mirroring the orderbook pattern (`mr_orderbook_bad_level_total` → `mr_trade_bad_value_total`). Harden all 6 exchange parsers to reject bad trades before they reach JetStream.

### Metrics to Add

**File: `internal/shared/metrics/metrics.go`**
```go
// Trade data-quality counters — mirrors orderbook quality pattern.

// mr_trade_bad_value_total counts rejected trades by reason.
// Reasons: "nan_price", "nan_size", "zero_price", "zero_size", "neg_price",
//          "neg_size", "empty_side", "empty_trade_id", "bad_timestamp"
MRTradeBadValueTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "mr_trade_bad_value_total",
        Help: "Total trades rejected by data-quality validation, by venue and reason.",
    },
    []string{"venue", "reason"},
)

// mr_trade_out_of_order_total counts trades arriving out of timestamp order.
MRTradeOutOfOrderTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "mr_trade_out_of_order_total",
        Help: "Total out-of-order trades detected per venue/instrument.",
    },
    []string{"venue", "instrument_bucket"},
)

// mr_trade_duplicate_total counts trades dropped as duplicates by idempotency key.
MRTradeDuplicateTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "mr_trade_duplicate_total",
        Help: "Total duplicate trades dropped per venue.",
    },
    []string{"venue"},
)

// mr_trade_ingest_total counts successfully ingested trades per venue.
MRTradeIngestTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "mr_trade_ingest_total",
        Help: "Total trades successfully ingested per venue.",
    },
    []string{"venue"},
)

// mr_trade_latency_seconds measures exchange-timestamp → ingest-timestamp lag.
MRTradeLatencySeconds = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "mr_trade_latency_seconds",
        Help:    "Latency from exchange timestamp to MR ingest, per venue.",
        Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
    },
    []string{"venue"},
)
```

**Helpers:**
```go
func IncMRTradeBadValue(venue, reason string)
func IncMRTradeOutOfOrder(venue, instrumentBucket string)
func IncMRTradeDuplicate(venue string)
func IncMRTradeIngest(venue string)
func ObserveMRTradeLatency(venue string, seconds float64)
```

### Parser Validation Hardening

For each of the 6 exchange parsers (`binance`, `bybit`, `coinbase`, `hyperliquid`, `kraken`, `krakenf`):

**Before returning `IngestRequest` with `EventType: "marketdata.trade"`:**
```go
trade := domain.TradeTickV1{Price: price, Size: size, Side: side, TradeID: tradeID, Timestamp: tsMs}
if p := trade.Validate(); p != nil {
    metrics.IncMRTradeBadValue(venueName, classifyTradeValidationReason(trade))
    return IngestRequest{}, true, nil // skip=true, no error (metric counted)
}
metrics.IncMRTradeIngest(venueName)
if tsMs > 0 && ingestTime.UnixMilli() > tsMs {
    metrics.ObserveMRTradeLatency(venueName, float64(ingestTime.UnixMilli()-tsMs)/1000.0)
}
```

**Add helper: `internal/adapters/exchange/common/trade_quality.go`** (NEW)
```go
package common

// ClassifyTradeValidationReason returns a Prometheus-safe reason string.
func ClassifyTradeValidationReason(price, size float64, side, tradeID string, tsMs int64) string {
    switch {
    case math.IsNaN(price): return "nan_price"
    case math.IsNaN(size):  return "nan_size"
    case price <= 0:        return "zero_price"  // includes negative
    case size <= 0:         return "zero_size"
    case side == "":        return "empty_side"
    case tradeID == "":     return "empty_trade_id"
    case tsMs <= 0:         return "bad_timestamp"
    default:                return "unknown"
    }
}
```

### Registry

All 5 metrics registered in `registerAll()` and exposed on `/metrics`.

### Invariants
- `TRADE-Q-1`: Every trade published to JetStream MUST have passed `Trade.Validate()`.
- `TRADE-Q-2`: `mr_trade_bad_value_total` increments BEFORE the trade is dropped (never published).
- `TRADE-Q-3`: `mr_trade_latency_seconds` only observed when both exchange-ts and ingest-ts are positive.
- `TRADE-Q-4`: All 5 metrics are present in `/metrics` scrape output (even if zero-valued via `InitLabelValues`).

### Tests (`-short`)

| Test | File | What |
|---|---|---|
| `TestTradeQualityMetrics_ExposedOnRegistry` | `metrics/metrics_test.go` | All 5 trade metrics present on `/metrics` |
| `TestClassifyTradeValidationReason` | `common/trade_quality_test.go` | All 9 reason strings |
| `TestBinanceParser_RejectsBadTrade` | `binance/parser_test.go` | price=0 → skip + metric |
| `TestBybitParser_RejectsBadTrade` | `bybit/parser_test.go` | size=NaN → skip + metric |
| `TestCoinbaseParser_RejectsBadTrade` | `coinbase/parser_test.go` | empty trade_id → skip + metric |

---

## Commit 2: `feat(aggregation): TapeWindowV1 domain — rate, volume, imbalance, burst detection`

### Objective
Add a pure, deterministic domain aggregate for tape metrics in fixed-size windows. This is the **data backbone** — no I/O, no actors. Operates on the same `bucketWindowStart(ts, windowMs)` logic as candles/stats.

### Domain Type

**File: `internal/core/aggregation/domain/tape.go`** (NEW)
```go
package domain

// AllowedTapeTimeframes defines the tape window set.
// Sub-second (250ms) for burst detection; 1s for summary; 5s for smoothed rate.
var AllowedTapeTimeframes = []string{"250ms", "1s", "5s"}

// TapeKey identifies one open tape window state.
type TapeKey struct {
    Venue      string
    Instrument string
    Timeframe  string
}

// TapeWindowV1 accumulates trade metrics within a fixed time window.
// All fields are deterministic (event-time bucketed, no wall-clock).
type TapeWindowV1 struct {
    Venue         string  // canonical venue
    Instrument    string  // canonical instrument
    Timeframe     string  // "250ms", "1s", "5s"
    WindowStartTs int64   // bucket start (ms)

    TradeCount    int64   // total trades in window
    BuyCount      int64   // buy-side trades
    SellCount     int64   // sell-side trades

    BuyVolume     float64 // sum(size) for buys
    SellVolume    float64 // sum(size) for sells
    TotalVolume   float64 // BuyVolume + SellVolume

    BuyNotional   float64 // sum(price*size) for buys
    SellNotional  float64 // sum(price*size) for sells

    VwapPrice     float64 // (BuyNotional + SellNotional) / TotalVolume

    MaxPrice      float64 // highest trade price in window
    MinPrice      float64 // lowest trade price in window
    LastPrice     float64 // most recent trade price

    MaxTradeSize  float64 // largest single trade size (whale proxy)

    LastSeq       int64   // highest seq seen (for dedup/ordering)
}

// NewTapeWindowV1 creates one open tape window.
func NewTapeWindowV1(venue, instrument, timeframe string, windowStartTs int64) (*TapeWindowV1, *problem.Problem)

// ApplyTrade applies a single trade to this window. Pure, deterministic.
// Invariant: price > 0, size > 0, isBuy is canonical.
func (w *TapeWindowV1) ApplyTrade(price, size float64, isBuy bool, seq int64) *problem.Problem

// Close finalizes the window. After Close, no more ApplyTrade calls.
// Computes derived metrics: VwapPrice, Rate, Imbalance.
func (w *TapeWindowV1) Close(windowEndTs int64) *problem.Problem

// --- Derived (computed on Close) ---

// Rate returns trades-per-second for this window.
func (w *TapeWindowV1) Rate() float64

// Imbalance returns (BuyVolume - SellVolume) / TotalVolume.
// Range [-1, +1]. 0 = balanced. +1 = all buys. -1 = all sells.
func (w *TapeWindowV1) Imbalance() float64

// IsBurst returns true if TradeCount exceeds the given threshold.
// Threshold is per-window (e.g., 250ms window with threshold=50 means 200 trades/sec rate).
func (w *TapeWindowV1) IsBurst(threshold int64) bool

// Validate checks all invariants.
func (w *TapeWindowV1) Validate() *problem.Problem
```

### Window Duration Map

```go
var TapeWindowDurations = map[string]int64{
    "250ms": 250,
    "1s":    1_000,
    "5s":    5_000,
}
```

### Burst Thresholds (defaults, configurable)

| Window | Threshold | Effective Rate |
|---|---|---|
| 250ms | 25 trades | 100 trades/sec |
| 1s | 80 trades | 80 trades/sec |
| 5s | 300 trades | 60 trades/sec |

Burst detection is deterministic: `TradeCount > Threshold` at window close.

### Invariants
- `TAPE-1`: `ApplyTrade` MUST NOT accept `price <= 0` or `size <= 0` (returns `*problem.Problem`).
- `TAPE-2`: `VwapPrice = (BuyNotional + SellNotional) / TotalVolume` (NaN-safe: 0 if TotalVolume == 0).
- `TAPE-3`: `Imbalance ∈ [-1, +1]`, 0 if TotalVolume == 0.
- `TAPE-4`: `Rate = TradeCount / (windowDurationMs / 1000.0)`.
- `TAPE-5`: `IsBurst(T)` is monotonic: if true at `n` trades, true for all `m > n`.
- `TAPE-6`: Two replicas processing the same trades in different order produce identical `Close()` output (commutativity for SUM aggregates).
- `TAPE-7`: `MaxTradeSize` tracks the single largest trade, not cumulative.

### Tests (`-short`)

| Test | File | What |
|---|---|---|
| `TestTapeWindowV1_ApplyTrade_Accumulates` | `domain/tape_test.go` | Volume, count, notional correctness |
| `TestTapeWindowV1_Close_DerivedMetrics` | same | Rate, Imbalance, VwapPrice after Close |
| `TestTapeWindowV1_BurstDetection` | same | IsBurst at boundary + above + below |
| `TestTapeWindowV1_Imbalance_AllBuys` | same | Imbalance = +1.0 for pure buy flow |
| `TestTapeWindowV1_Imbalance_AllSells` | same | Imbalance = -1.0 for pure sell flow |
| `TestTapeWindowV1_Imbalance_Empty` | same | Imbalance = 0.0 for no trades |
| `TestTapeWindowV1_VwapPrice_Empty` | same | VwapPrice = 0 when no trades |
| `TestTapeWindowV1_Validate_RejectsBadInputs` | same | price=0, size=NaN, empty venue |
| `TestTapeWindowV1_CommutativeProperty` | same | Same trades in 2 orders → identical Close |
| `TestTapeWindowV1_MaxTradeSize` | same | Tracks single largest, not cumulative |
| `TestNewTapeWindowV1_InvalidTimeframe` | same | Rejects "2m" |

---

## Commit 3: `feat(aggregation): BuildTapeFromTrades use case + wire contract + processor routing`

### Objective
Wire the `TapeWindowV1` domain into the aggregation pipeline. Add the wire DTO, proto, codec, subject registry, and processor routing so tape aggregations are published to JetStream and delivered to WS clients.

### Use Case

**File: `internal/core/aggregation/app/build_tape.go`** (NEW)
```go
package app

// BuildTapeFromTrades accumulates trades into sub-second and multi-second tape windows.
// Follows identical lifecycle to BuildCandleFromEvents:
//   1. Observe window lifecycle (watermark manager)
//   2. ApplyTrade to open window
//   3. On window close → publish TapeSnapshot to bus
type BuildTapeFromTrades struct {
    windows   WindowLifecycleManager  // reuse existing interface
    windowMs  map[string]int64        // "250ms"→250, "1s"→1000, "5s"→5000
    state     map[TapeKey]*domain.TapeWindowV1
    publisher SnapshotPublisher
    burst     map[string]int64        // per-timeframe burst thresholds
    logger    *slog.Logger
}

type BuildTapeRequest struct {
    Venue      string
    Instrument string
    Price      float64
    Quantity   float64
    IsBuy      bool
    Seq        int64
    TsIngest   int64  // event-time ms (NOT wall-clock)
}

type BuildTapeResponse struct {
    ClosedWindows []TapeCloseEvent
}

type TapeCloseEvent struct {
    Window  *domain.TapeWindowV1
    IsBurst bool
}

func (uc *BuildTapeFromTrades) Execute(ctx context.Context, req BuildTapeRequest) (BuildTapeResponse, *problem.Problem)
```

**State boundedness:** `maxOpenWindows = len(AllowedTapeTimeframes) * maxInstruments` (same pattern as candle/stats).

### Wire DTO

**File: `internal/shared/contracts/aggregation_tape_types.go`** (NEW)
```go
// AggregationTapeV1 is the wire DTO for aggregation.tape v1.
// Uses PascalCase (no json tags) to match existing MR convention for aggregation types.
type AggregationTapeV1 struct {
    Venue         string
    Instrument    string
    Timeframe     string  // "250ms", "1s", "5s"
    WindowStartTs int64
    TradeCount    int64
    BuyCount      int64
    SellCount     int64
    BuyVolume     float64
    SellVolume    float64
    TotalVolume   float64
    BuyNotional   float64
    SellNotional  float64
    VwapPrice     float64
    MaxPrice      float64
    MinPrice      float64
    LastPrice     float64
    MaxTradeSize  float64
    Rate          float64  // trades/sec
    Imbalance     float64  // [-1, +1]
    IsBurst       bool
    Seq           int64
    TsIngestMs    int64
}
```

### Proto

**File: `proto/aggregation/v1/tape.proto`** (NEW)
```proto
syntax = "proto3";
package aggregation.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/aggregation/v1;aggregationv1";

message TapeWindowV1 {
  string venue          = 1;
  string instrument     = 2;
  string timeframe      = 3;
  int64  window_start_ts = 4;
  int64  trade_count    = 5;
  int64  buy_count      = 6;
  int64  sell_count     = 7;
  double buy_volume     = 8;
  double sell_volume    = 9;
  double total_volume   = 10;
  double buy_notional   = 11;
  double sell_notional  = 12;
  double vwap_price     = 13;
  double max_price      = 14;
  double min_price      = 15;
  double last_price     = 16;
  double max_trade_size = 17;
  double rate           = 18;
  double imbalance      = 19;
  bool   is_burst       = 20;
  int64  seq            = 21;
  int64  ts_ingest_ms   = 22;
}
```

### Subject Registry

```yaml
# docs/contracts/subject-registry.yaml
- id: aggregation.tape.v1
  pattern: aggregation.tape.v1.{venue}.{instrument}
  root: signal  # same root as aggregation.candle/stats
  owner_bc: aggregation
  producer_bc: aggregation
  schema_authority_bc: aggregation
  consumer_bcs: [delivery, storage]
  status: stable
```

### Codec Registration

**File: `internal/shared/contracts/payload_registry.go`** — add in `BootstrapPayloadCodecRegistry()`:
```go
registerPayloadDual(reg, "aggregation.tape",
    codec.JSONCodec[AggregationTapeV1]{},
    domainProtoPayloadCodec[AggregationTapeV1, *aggregationv1.TapeWindowV1]{...},
)
```

### Processor Routing

**File: `internal/actors/aggregation/runtime/processor.go`**

In the `typeTrade` case, AFTER the existing candle + insights handling:
```go
if p.tapeEnabled() && p.cfg.Service != nil && p.cfg.Service.Tape != nil {
    if prob := p.handleTradeForTape(env); prob != nil {
        p.logger.Warn("aggruntime: BuildTape failed", ...)
    }
}
```

`handleTradeForTape` decodes the trade, builds `BuildTapeRequest`, calls `Execute`, and publishes closed windows.

### Wire Budget

**Add metric:**
```go
MRTradeWireBytes = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "mr_trade_wire_bytes",
        Help:    "Encoded trade event frame size in bytes.",
        Buckets: []float64{64, 128, 256, 512, 1024},
    },
    []string{"venue", "channel"},  // channel: "trade" or "tape"
)
```

Observed in delivery `writeDeliveryEvent` when channel is `trade` or `tape`.

### Invariants
- `TAPE-W-1`: `aggregation.tape` subject uses same JetStream root (`signal`) as candle/stats.
- `TAPE-W-2`: Tape windows are published ONLY on close (not on every trade).
- `TAPE-W-3`: Burst flag is computed at close-time, not re-checked.
- `TAPE-W-4`: Under `PROCESSOR_REPLICAS=2`, both replicas produce identical tape windows for identical trade sets (deterministic).
- `TAPE-W-5`: `mr_trade_wire_bytes` p99 < 1KB per tape frame (tape is compact).

### Tests (`-short`)

| Test | File | What |
|---|---|---|
| `TestBuildTapeFromTrades_Execute_BasicFlow` | `app/build_tape_test.go` | 10 trades → 3 windows close with correct metrics |
| `TestBuildTapeFromTrades_WindowBoundary` | same | Trade at boundary starts new window |
| `TestBuildTapeFromTrades_BurstFlag` | same | High-rate trades trigger burst in 250ms window |
| `TestBuildTapeFromTrades_DeterministicReplay` | same | Same trades, 2 orderings → identical output |
| `TestAggregationTapeV1_RoundTrip_JSON` | `contracts/aggregation_tape_test.go` | Marshal/unmarshal preserves all fields |
| `TestAggregationTapeV1_Proto_RoundTrip` | same | Proto ↔ domain conversion lossless |
| `TestProcessorRouting_TradeToTape` | `runtime/processor_test.go` | Trade envelope routes to tape use case |

---

## Commit 4: `feat(client): wire tape aggregation channel + enrich trades widget`

### Objective
Minimal client wiring to subscribe to `aggregation.tape`, parse the payload, and surface rate/imbalance/burst in the existing trades widget. No redesign — extend what exists.

### New Channel

**File: `client/src/core/ports/marketdata.odin`**
```odin
// Add to MD_Channel enum:
Tape :: 7,  // (or next available value)
```

**File: `client/src/core/util/subject.odin`**
```odin
// Add mapping:
case .Tape: return "aggregation.tape"

// Tape uses "1s" timeframe by default (configurable per-cell later):
case .Tape: return "1s"
```

### Wire Protocol Struct

**File: `client/src/core/util/mr_protocol.odin`**
```odin
MR_Tape :: struct {
    venue:          string `json:"Venue"`,
    instrument:     string `json:"Instrument"`,
    timeframe:      string `json:"Timeframe"`,
    window_start:   i64    `json:"WindowStartTs"`,
    trade_count:    i64    `json:"TradeCount"`,
    buy_count:      i64    `json:"BuyCount"`,
    sell_count:     i64    `json:"SellCount"`,
    buy_volume:     f64    `json:"BuyVolume"`,
    sell_volume:    f64    `json:"SellVolume"`,
    total_volume:   f64    `json:"TotalVolume"`,
    vwap_price:     f64    `json:"VwapPrice"`,
    max_price:      f64    `json:"MaxPrice"`,
    min_price:      f64    `json:"MinPrice"`,
    last_price:     f64    `json:"LastPrice"`,
    max_trade_size: f64    `json:"MaxTradeSize"`,
    rate:           f64    `json:"Rate"`,
    imbalance:      f64    `json:"Imbalance"`,
    is_burst:       bool   `json:"IsBurst"`,
}

MR_Tape_Frame :: struct {
    payload: MR_Tape `json:"payload"`,
}
```

### Tape Store

**File: `client/src/core/services/tape_store.odin`** (NEW)
```odin
TAPE_CAP :: 64  // 64 windows = ~64s at 1s resolution

Tape_Entry :: struct {
    window_start: i64,
    trade_count:  i64,
    buy_volume:   f64,
    sell_volume:  f64,
    total_volume: f64,
    vwap_price:   f64,
    rate:         f64,       // trades/sec
    imbalance:    f64,       // [-1, +1]
    is_burst:     bool,
    max_trade_size: f64,
}

Tape_Store :: struct {
    entries: [TAPE_CAP]Tape_Entry,
    head:    int,
    count:   int,
}

push_tape :: proc(store: ^Tape_Store, entry: Tape_Entry)
get_tape  :: proc(store: ^Tape_Store, i: int) -> Tape_Entry  // 0=most recent
```

### Message Parser

**File: `client/src/core/services/message_parser.odin`**

Add case in dispatch:
```odin
case "aggregation.tape":
    if r, ok := parse_tape(raw, env.ts_ingest, subject_id); ok {
        result.kind = .Tape
        result.data.tape = r
    }
```

### Event Handler

**File: `client/src/core/app/marketdata.odin`**

Add `handle_tape_event`:
```odin
handle_tape_event :: proc(state: ^App_State, slot: ^Market_Stream, t: ports.MD_Tape_Event, ...) {
    push_tape(&slot.tape_store, Tape_Entry{...})
    if is_active_stream {
        push_tape(&state.stores.tape, Tape_Entry{...})
    }
}
```

### Widget Enrichment

**File: `client/src/core/widgets/trades_widget.odin`**

Add to the existing widget header area (above the trade table):
```
┌─────────────────────────────────┐
│ TAPE  rate: 47/s  imb: +0.23   │  ← NEW: single line from tape store
│ [BURST]                         │  ← NEW: only when is_burst=true (yellow flash)
├─────────────────────────────────┤
│ Time    Side  Price    Qty      │  ← EXISTING: unchanged
│ 12:34   BUY   42100   0.50     │
│ ...                             │
└─────────────────────────────────┘
```

- Rate: `fmt.tprintf` is BANNED — use buffer concat (see Odin gotchas).
- Imbalance: colored green (+buy), red (-sell), white (neutral <0.05 abs).
- Burst: yellow background flash for 500ms when `is_burst` transitions true.

### Subscription Reconcile

**File: `client/src/core/app/reconcile.odin`**

Add `Tape` to `channels_for_widget` for the trades widget:
```odin
case .Trades: return {.Trades, .Tape}  // subscribe to both raw trades AND tape aggregation
```

### Invariants
- `CLIENT-TAPE-1`: `MR_Tape` json tags match PascalCase Go wire format exactly.
- `CLIENT-TAPE-2`: Tape store is independent per `Market_Stream` (no cross-stream state).
- `CLIENT-TAPE-3`: Zero `fmt.tprintf` in rendering path — all string formatting via buffer concat.
- `CLIENT-TAPE-4`: Burst flash uses frame counter (not wall-clock) for deterministic rendering.

### Tests
No automated Odin tests in CI (existing pattern). Validated via IQ run in C5.

---

## Commit 5: `test(aggregation): deterministic IQ tests + metrics exposure gate`

### Objective
Machine-checkable DoD. Deterministic tests proving PROCESSOR_REPLICAS=2 equivalence, metric exposure assertions, and trade-quality threshold contracts.

### Metrics Exposure Tests

**File: `internal/shared/metrics/metrics_test.go`** — extend existing:
```go
func TestTradeQualityMetricsExposedWithoutRuntimeEvents(t *testing.T) {
    mfs, err := Registry().Gather()
    require.NoError(t, err)
    for _, name := range []string{
        "mr_trade_bad_value_total",
        "mr_trade_out_of_order_total",
        "mr_trade_duplicate_total",
        "mr_trade_ingest_total",
        "mr_trade_latency_seconds",
        "mr_trade_wire_bytes",
    } {
        found := false
        for _, mf := range mfs {
            if mf.GetName() == name { found = true; break }
        }
        assert.True(t, found, "metric %s not in /metrics", name)
    }
}
```

### Determinism Tests

**File: `internal/core/aggregation/domain/tape_determinism_test.go`** (NEW)
```go
// TestTapeWindowV1_DeterministicReplay_PropertyBased uses rapid for property testing.
// Generates random trade sequences, applies in 2 orderings, asserts identical Close output.
func TestTapeWindowV1_DeterministicReplay_PropertyBased(t *testing.T)

// TestTapeWindowV1_ReplicaEquivalence simulates PROCESSOR_REPLICAS=2.
// Both replicas receive identical events (possibly interleaved differently).
// Asserts: Rate, Imbalance, VwapPrice, TradeCount, BurstFlag are identical.
func TestTapeWindowV1_ReplicaEquivalence(t *testing.T)
```

**File: `internal/core/aggregation/app/build_tape_determinism_test.go`** (NEW)
```go
// TestBuildTapeFromTrades_MultiReplica sends same trades to 2 independent use case instances.
// Asserts all closed windows are identical.
func TestBuildTapeFromTrades_MultiReplica(t *testing.T)
```

### IQ Thresholds (DoD Gate)

These thresholds are checked via `/metrics` scrape after a 60s live run with `PROCESSOR_REPLICAS=2`:

| Metric | Condition | Threshold |
|---|---|---|
| `mr_trade_bad_value_total` | SUM | 0 (no bad trades in clean venues) |
| `mr_trade_out_of_order_total` | SUM | < 100 (tolerance for REPLICAS=2 interleaving) |
| `mr_trade_ingest_total` | SUM | > 0 (at least 1 trade ingested per venue) |
| `mr_trade_latency_seconds` | p99 | < 2s (exchange → ingest lag) |
| `mr_trade_wire_bytes{channel="tape"}` | p99 | < 1KB |
| `mr_trade_wire_bytes{channel="trade"}` | p99 | < 512B |

### Evidence Artifacts

Run produces `artifacts/iq/<timestamp>/`:
- `server.metrics.prom` — full Prometheus scrape
- `metrics-summary.txt` — extracted trade thresholds
- `playwright.ws-diagnostic.json` — client subscription proof (includes `aggregation.tape` channel)

### Invariants
- `IQ-1`: All `TestTape*` tests pass under `go test -short -count=1 -race`.
- `IQ-2`: Metric exposure test catches any missing registration (regression guard).
- `IQ-3`: Property-based determinism test uses at least 1000 random trade sequences.

---

## Execution Checklist (for CODEX)

```
□ C1: feat(shared): trade data-quality metrics + parser validation hardening
  □ Add 5 Prometheus counters to metrics/metrics.go
  □ Add helpers (Inc/Observe) to metrics/metrics.go
  □ Register all 5 in registerAll()
  □ Add common/trade_quality.go (ClassifyTradeValidationReason)
  □ Harden binance/parser.go — call Validate(), emit metric, skip on fail
  □ Harden bybit/parser.go — same
  □ Harden coinbase/parser.go — same
  □ Harden hyperliquid/parser.go — same
  □ Harden kraken/parser.go — same
  □ Harden krakenf/parser.go — same
  □ Add latency observation in each parser (exchange_ts → ingest_ts)
  □ Test: metrics_test.go — 5 metrics exposed
  □ Test: trade_quality_test.go — 9 reason strings
  □ Test: 3 parser rejection tests (binance, bybit, coinbase)
  □ Gate: make test MODULE=./internal/shared && make test MODULE=./internal/adapters
  □ Gate: make lint

□ C2: feat(aggregation): TapeWindowV1 domain — rate, volume, imbalance, burst
  □ Create domain/tape.go (TapeWindowV1, TapeKey, AllowedTapeTimeframes)
  □ Implement NewTapeWindowV1 with validation
  □ Implement ApplyTrade (accumulate counters + extremes)
  □ Implement Close (derive Rate, Imbalance, VwapPrice)
  □ Implement IsBurst, Rate, Imbalance accessors
  □ Implement Validate
  □ Test: 11 tests in domain/tape_test.go
  □ Gate: make test MODULE=./internal/core/aggregation

□ C3: feat(aggregation): BuildTapeFromTrades + wire contract + processor routing
  □ Create proto/aggregation/v1/tape.proto
  □ Run protoc (make proto)
  □ Create contracts/aggregation_tape_types.go (AggregationTapeV1 DTO)
  □ Create contracts/converters_tape.go (domain ↔ proto ↔ DTO)
  □ Register codec in payload_registry.go
  □ Add subject to subject-registry.yaml
  □ Create app/build_tape.go (use case)
  □ Wire processor routing in processor.go (typeTrade → handleTradeForTape)
  □ Add mr_trade_wire_bytes histogram to metrics.go
  □ Add wire-bytes observation in delivery path
  □ Test: 7 tests (use case + codec + processor routing)
  □ Gate: make test MODULE=./internal/core/aggregation && make test MODULE=./internal/shared
  □ Gate: make proto-check && make lint

□ C4: feat(client): wire tape aggregation channel + enrich trades widget
  □ Add Tape to MD_Channel enum in ports
  □ Add "aggregation.tape" mapping in subject.odin
  □ Add MR_Tape + MR_Tape_Frame to mr_protocol.odin
  □ Create tape_store.odin (ring buffer, cap=64)
  □ Add parse_tape to message_parser.odin
  □ Add Tape_Store to Market_Stream struct
  □ Add handle_tape_event to marketdata.odin
  □ Add Tape to channels_for_widget(.Trades)
  □ Enrich trades_widget header (rate, imbalance, burst flash)
  □ VERIFY: zero fmt.tprintf in new code
  □ VERIFY: json tags match PascalCase

□ C5: test(aggregation): deterministic IQ tests + metrics exposure gate
  □ Extend metrics_test.go — 6 trade metrics exposed
  □ Create domain/tape_determinism_test.go (property-based + commutativity)
  □ Create app/build_tape_determinism_test.go (multi-replica)
  □ Gate: make test -race
  □ Gate: make lint
  □ IQ run: docker deploy + 60s live + /metrics scrape + threshold check
  □ Record evidence in artifacts/iq/<timestamp>/
```

---

## Dependency Graph

```
C1 (metrics + parser hardening)
 │
 ├── C2 (TapeWindowV1 domain) ──── standalone, no dep on C1
 │     │
 │     └── C3 (use case + wire + routing) ── depends on C2
 │           │
 │           └── C4 (client wiring) ── depends on C3
 │
 └── C5 (IQ tests) ── depends on C1 + C2 + C3
```

**Parallelizable:** C1 and C2 can be done in parallel (no file overlap).
**Sequential:** C3 requires C2. C4 requires C3. C5 requires C1+C2+C3.

---

## Risk Register

| Risk | Mitigation |
|---|---|
| 250ms windows under REPLICAS=2 produce inconsistent close times | Watermark manager + event-time bucketing (deterministic). Same pattern proven for 1s candles. |
| Tape throughput overhead on processor hot path | TapeWindowV1 is SUM-only (no sort, no BTree). ApplyTrade is O(1). |
| Client tape subscription doubles WS events per market | Tape publishes only on window close (~1-4/sec), not per trade. Raw trades already flow separately. |
| Proto code generation breaks CI | `make proto-check` gate before merge. |
| Parser hardening rejects legitimate edge-case trades | Validation matches existing `Trade.Validate()` which is already the domain contract. Conservative reasons (only reject NaN/zero/negative). |
