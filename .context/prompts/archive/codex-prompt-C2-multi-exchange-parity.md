# Codex Prompt C2 — Multi-Exchange Parity (Coinbase + HyperLiquid + Spot/Futures Split)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture. Dual storage (TimescaleDB + ClickHouse). 5 bounded contexts: MarketData, Aggregation, Delivery, Insights, Storage.

---

## Context

After C1, the pipeline has proto wire format activation, shard-aware topology, and E2E benchmarks. Currently the consumer supports **2 exchange types**: Binance and Bybit. MarketMonkey supports **5 active exchanges**: Binance spot, Binance futures, Bybit, Coinbase, and HyperLiquid.

**Four gaps remain for multi-exchange parity:**

1. **No Coinbase adapter** — MarketMonkey uses `wss://ws-feed.exchange.coinbase.com` with message-based subscriptions for channels `matches` (trades), `level2_batch` (orderbook), and `ticker` (price). Coinbase is spot-only (no funding rate, no liquidations).

2. **No HyperLiquid adapter** — MarketMonkey uses `wss://api.hyperliquid.xyz/ws` with per-ticker per-channel message subscriptions for `trades` and `l2Book`. HyperLiquid sends **full orderbook snapshots** (not deltas) on every l2Book message. Trade IDs are blockchain tx hashes, not integers.

3. **No Binance spot/futures split** — The current Binance adapter uses a single `VenueBinance = "BINANCE"` constant. MarketMonkey differentiates: `binance` (spot) connects to `wss://stream.binance.com`, subscribes to 2 streams (aggTrade + depth); `binancef` (futures) connects to `wss://fstream.binance.com`, subscribes to 4 streams (aggTrade + depth + markPrice + forceOrder). The adapter **already** parses all 4 event types, but the endpoint builder doesn't differentiate base URLs, and `StreamsPerTicker` is not adjusted per market type.

4. **Funding rate pipeline not exercised end-to-end** — `MarkPriceTickV1.FundingRate` field **already exists** and both Binance and Bybit parsers **already populate it**. The IngestMarketData usecase already publishes markprice envelopes. However, there are **no tests** that verify funding rate flows from parser → ingest → bus → aggregation, and the processor doesn't route markprice events to a dedicated funding rate handler.

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error` in domain/app)
### Results: `result.Result[T]` for usecase returns
### Import order: stdlib → external → monorepo
### Tests: table-driven, deterministic (use `clock.FakeClock`)
### Normalization: `naming.CanonicalVenue/CanonicalInstrument` at domain boundary
### Parser contract: every exchange parser must implement the same 4-function API as Binance/Bybit

```go
// Required public API for every exchange parser package:
func ParseMessage(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem)
func ParseMessageWithMeta(data []byte, recvAt time.Time) (app.IngestRequest, bool, ParseMeta)
func ParseMessageForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, *problem.Problem)
func ParseMessageWithMetaForMarketType(data []byte, recvAt time.Time, marketType string) (app.IngestRequest, bool, ParseMeta)
```

### ParseMeta contract: every exchange ParseMeta must have identical fields

```go
type ParseMeta struct {
    EventType  string
    SkipReason string
    Problem    *problem.Problem
    WSStream   string
    Ticker     string
}
```

---

## Task: Four-Part Multi-Exchange Expansion

### PART 1: Coinbase Exchange Adapter

#### Step 1.1: Coinbase endpoint builder

**File:** `internal/adapters/exchange/coinbase/endpoint.go` (NEW)

```go
package coinbase

const DefaultWSBaseURL = "wss://ws-feed.exchange.coinbase.com"

// BuildSubscriptions returns the subscribe message for Coinbase WS.
// Coinbase uses message-based subscriptions (sent after connect).
// No streams are encoded in the URL.
func BuildSubscriptions(tickers []string) ([][]byte, *problem.Problem)
```

Coinbase subscription format (single JSON message):
```json
{
    "type": "subscribe",
    "product_ids": ["BTC-USD", "ETH-USD"],
    "channels": ["matches", "level2_batch", "ticker"]
}
```

**Symbol format:** Coinbase expects hyphenated pairs: `BTC-USD`, `ETH-USD`, `ETH-BTC`. Tickers from config arrive as `BTC-USD` or `BTCUSD` — normalize to Coinbase format:
- If ticker already contains `-`, use as-is
- Otherwise, apply `canonicalPairFromSymbol()` to split base/quote, then format `BASE-QUOTE`

**Endpoint builder:** Always returns `DefaultWSBaseURL` (or config override) — Coinbase has a single WS endpoint.

```go
func BuildEndpoint(baseURL string) string {
    if baseURL != "" {
        return baseURL
    }
    return DefaultWSBaseURL
}
```

**Tests:** `internal/adapters/exchange/coinbase/endpoint_test.go` (NEW)
- TestBuildSubscriptions returns valid JSON with all 3 channels
- TestBuildSubscriptions with empty tickers returns `*problem.Problem`
- TestBuildEndpoint default and override

#### Step 1.2: Coinbase parser

**File:** `internal/adapters/exchange/coinbase/parser.go` (NEW)

```go
package coinbase

const VenueCoinbase = "COINBASE"
```

Wire types to decode (based on Coinbase WS API):

```go
// Top-level message — all Coinbase messages have a "type" field
type wsMessage struct {
    Type string `json:"type"`
}

// Trade ("match" message)
type matchMessage struct {
    Type      string `json:"type"`      // "match" or "last_match"
    TradeID   int64  `json:"trade_id"`
    ProductID string `json:"product_id"` // "BTC-USD"
    Price     string `json:"price"`
    Size      string `json:"size"`
    Side      string `json:"side"`       // "buy" or "sell"
    Time      string `json:"time"`       // RFC3339 "2024-01-01T00:00:00.000000Z"
}

// Orderbook snapshot ("snapshot" message)
type snapshotMessage struct {
    Type      string     `json:"type"`       // "snapshot"
    ProductID string     `json:"product_id"`
    Bids      [][]string `json:"bids"`       // [["price","size"], ...]
    Asks      [][]string `json:"asks"`
}

// Orderbook delta ("l2update" message)
type l2UpdateMessage struct {
    Type      string     `json:"type"`       // "l2update"
    ProductID string     `json:"product_id"`
    Time      string     `json:"time"`       // RFC3339
    Changes   [][]string `json:"changes"`    // [["side","price","size"], ...]
}

// Ticker message
type tickerMessage struct {
    Type      string `json:"type"`       // "ticker"
    ProductID string `json:"product_id"`
    Price     string `json:"price"`
    Time      string `json:"time"`       // RFC3339
}
```

**Dispatch logic:** Read `type` field, route to handler:
- `"match"` or `"last_match"` → `parseTrade` → `domain.TradeTickV1`
- `"snapshot"` → `parseSnapshot` → `domain.BookDeltaV1` with **full book as delta** (set `FirstID=0`, `FinalID=1`, clear previous book)
- `"l2update"` → `parseL2Update` → `domain.BookDeltaV1` (changes split into bids/asks by side field)
- `"ticker"` → `parseTicker` → `domain.MarkPriceTickV1` (price only, no funding rate — Coinbase is spot)
- `"error"`, `"subscriptions"`, `"heartbeat"` → skip (control messages)
- unknown → skip

**Symbol normalization:** Coinbase uses `BTC-USD` format. Strip hyphen via `naming.CanonicalInstrument("BTC-USD")` → `"BTCUSD"`.

**Side normalization:** Coinbase sends `"buy"` or `"sell"` — already normalized, pass through.

**Timestamp handling:** Coinbase sends RFC3339 timestamps (`time.Parse(time.RFC3339Nano, raw)`). Convert to Unix ms for `TsExchange`.

**Idempotency keys:**
- Trades: `venue=COINBASE|instrument=BTCUSD|trade_id=12345` (trade_id from `trade_id` field)
- Orderbook: No sequence IDs from Coinbase — use envelope timestamp as fallback. Set `FirstID=0`, `FinalID` = incrementing per product (use `time.Now().UnixNano()` as placeholder — the ingest usecase adds its own sequence).

**IMPORTANT:** The l2update `changes` array has 3-element entries: `["side", "price", "size"]`. Split by side into bids (side="buy") and asks (side="sell"):
```go
for _, change := range msg.Changes {
    side, price, size := change[0], change[1], change[2]
    level := domain.PriceLevel{Price: parseFloat(price), Size: parseFloat(size)}
    if side == "buy" {
        bids = append(bids, level)
    } else {
        asks = append(asks, level)
    }
}
```

**Tests:** `internal/adapters/exchange/coinbase/parser_test.go` (NEW)
- Table-driven tests with real Coinbase JSON fixtures:
  - Match message → TradeTickV1 with correct price/size/side/timestamp
  - Snapshot message → BookDeltaV1 with full bids/asks
  - L2Update message → BookDeltaV1 with changes split by side
  - Ticker message → MarkPriceTickV1 (price only, funding=0)
  - Error/heartbeat/subscriptions → skip=true
  - Invalid JSON → skip=true with Problem
  - Empty product_id → skip=true with Problem
  - ParseMeta populated correctly for all cases

#### Step 1.3: Coinbase exchange runtime builder

**File:** `cmd/consumer/exchanges.go` (EXTEND)

Add `buildCoinbaseRuntime` function following the exact pattern of `buildBinanceRuntime` and `buildBybitRuntime`:

```go
import "github.com/market-raccoon/internal/adapters/exchange/coinbase"

func buildCoinbaseRuntime(
    cfg config.AppConfig,
    logger *slog.Logger,
    ex config.ConsumerExchangeConfig,
    subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
    managerCfg := baseManagerConfig(cfg, ex)
    managerCfg.EndpointBuilder = func(bucket []string) string {
        endpoint := coinbase.BuildEndpoint(ex.BaseURL)
        logger.Info("consumer: ws endpoint planned", "exchange", ex.Name, "endpoint", endpoint, "bucket", bucket)
        return endpoint
    }
    managerCfg.SubscriptionBuilder = func(bucket []string) [][]byte {
        msgs, p := coinbase.BuildSubscriptions(bucket)
        if p != nil {
            logger.Error("consumer: coinbase subscription build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
            return nil
        }
        return msgs
    }
    // ... same parseV1/parseV2 pattern as Bybit, using coinbase.ParseMessageWithMetaForMarketType
}
```

Add `"coinbase"` case in `buildExchangeRuntime` switch:
```go
case "coinbase":
    return buildCoinbaseRuntime(cfg, logger, ex, subsystem), nil
```

**StreamsPerTicker for Coinbase:** 3 (matches + level2_batch + ticker).

---

### PART 2: HyperLiquid Exchange Adapter

#### Step 2.1: HyperLiquid endpoint builder

**File:** `internal/adapters/exchange/hyperliquid/endpoint.go` (NEW)

```go
package hyperliquid

const DefaultWSBaseURL = "wss://api.hyperliquid.xyz/ws"

// BuildEndpoint returns the WS endpoint. HyperLiquid uses a single fixed endpoint.
func BuildEndpoint(baseURL string) string

// BuildSubscriptions returns per-ticker per-channel subscribe messages.
// HyperLiquid requires one message per ticker per channel type.
// For N tickers, returns 2*N messages (trades + l2Book per ticker).
func BuildSubscriptions(tickers []string) ([][]byte, *problem.Problem)
```

HyperLiquid subscription format (one message per ticker per channel):
```json
{"method":"subscribe","subscription":{"type":"trades","coin":"BTC"}}
{"method":"subscribe","subscription":{"type":"l2Book","coin":"BTC"}}
```

**Symbol format:** HyperLiquid expects bare coin names: `BTC`, `ETH` (not pairs). If ticker contains a quote currency suffix (USDT, USD, PERP), strip it:
- `BTCUSDT` → `BTC`
- `ETHPERP` → `ETH`
- `BTC` → `BTC` (already bare)

Implement `toCoinName(ticker string) string` helper for this normalization.

**Tests:** `internal/adapters/exchange/hyperliquid/endpoint_test.go` (NEW)
- TestBuildSubscriptions with 2 tickers → 4 messages
- TestBuildSubscriptions empty tickers → Problem
- TestToCoinName various formats

#### Step 2.2: HyperLiquid parser

**File:** `internal/adapters/exchange/hyperliquid/parser.go` (NEW)

```go
package hyperliquid

const VenueHyperLiquid = "HYPERLIQUID"
```

Wire types to decode (based on HyperLiquid WS API):

```go
// Top-level message — HyperLiquid messages have a "channel" field
type wsResponse struct {
    Channel string          `json:"channel"`
    Data    json.RawMessage `json:"data"`
}

// Trade data
type tradeEntry struct {
    Coin string `json:"coin"` // "BTC"
    Side string `json:"side"` // "B" (buy) or "A" (ask/sell)
    Px   string `json:"px"`   // price
    Sz   string `json:"sz"`   // size
    Time int64  `json:"time"` // unix ms
    Hash string `json:"hash"` // blockchain tx hash (trade ID)
    Tid  int64  `json:"tid"`  // numeric trade ID
}

// Orderbook data
type l2BookData struct {
    Coin   string          `json:"coin"`
    Time   int64           `json:"time"` // unix ms
    Levels [2][]bookLevel  `json:"levels"` // [0]=bids, [1]=asks
}

type bookLevel struct {
    Px string `json:"px"` // price
    Sz string `json:"sz"` // size
    N  int    `json:"n"`  // number of orders
}
```

**Dispatch logic:** Read `channel` field:
- `"trades"` → data is `[]tradeEntry` → parse first entry → `domain.TradeTickV1`
- `"l2Book"` → data is `l2BookData` → `domain.BookDeltaV1` as **full snapshot**
- Empty channel with `"method"` or `"channel"` absent → control message, skip
- unknown → skip

**CRITICAL: HyperLiquid book is always a full snapshot**, not incremental deltas. Set `BookDeltaV1.FirstID = data.Time` and `BookDeltaV1.FinalID = data.Time` (using the server timestamp as a monotonic sequence proxy). The aggregation layer's `UpdateOrderBookFromEvents` must be prepared to receive full snapshots (complete bid/ask replacement). This is already supported because BookDeltaV1 represents a set of levels — if the aggregation replaces the entire book when receiving a "snapshot-style" delta (all levels present, no incremental sequence), it works correctly.

**Side normalization:** HyperLiquid sends `"B"` (buy) or `"A"` (ask/sell):
```go
func normalizeSide(side string) (string, *problem.Problem) {
    switch strings.ToUpper(strings.TrimSpace(side)) {
    case "B", "BUY":
        return "buy", nil
    case "A", "S", "SELL", "ASK":
        return "sell", nil
    default:
        return "", problem.Newf(problem.ValidationFailed, "hyperliquid: unsupported side %q", side)
    }
}
```

**Symbol normalization:** HyperLiquid sends bare coin names (`BTC`, `ETH`). The canonical instrument should include the implicit quote:
```go
instrument := naming.CanonicalInstrument(entry.Coin + "USD")
```
This produces `BTCUSD` which is consistent with the canonical format. This is a design decision — HyperLiquid perps are USD-denominated.

**Trade ID:** Use the `hash` field (blockchain tx hash) as the trade ID. If empty, fall back to `tid` (numeric).

**Idempotency keys:**
- Trades: `venue=HYPERLIQUID|instrument=BTCUSD|trade_id=0x3fff...`

**Tests:** `internal/adapters/exchange/hyperliquid/parser_test.go` (NEW)
- Table-driven with real HyperLiquid JSON fixtures:
  - Trades with side "B" and "A" → correct buy/sell normalization
  - L2Book → full snapshot BookDeltaV1 with all bids and asks
  - Hash-based trade ID
  - Control messages → skip
  - ParseMeta populated correctly

#### Step 2.3: HyperLiquid exchange runtime builder

**File:** `cmd/consumer/exchanges.go` (EXTEND)

Add `buildHyperLiquidRuntime` following the same pattern. Add `"hyperliquid"` case in switch.

**StreamsPerTicker for HyperLiquid:** 2 (trades + l2Book).

**Market type:** HyperLiquid is perpetual futures — default market type is `USD_M_FUTURES`.

---

### PART 3: Binance Spot/Futures Split

#### Step 3.1: Separate Binance endpoint URLs by market type

**File:** `internal/adapters/exchange/binance/endpoint.go` (EXTEND)

Add futures URL constant:
```go
const DefaultFuturesWSBaseURL = "wss://fstream.binance.com/stream"
```

Update `BuildEndpoint` to accept market type parameter:
```go
func BuildEndpoint(baseURL string, tickers []string, includeMarkPriceLiquidation bool) (string, *problem.Problem)
```

The `baseURL` already controls spot vs futures. The caller (exchanges.go) should pass the correct base URL based on market type:
- `SPOT` → `DefaultWSBaseURL` (`wss://stream.binance.com:9443/stream`)
- `USD_M_FUTURES` → `DefaultFuturesWSBaseURL` (`wss://fstream.binance.com/stream`)

**No changes needed to the endpoint builder function itself** — just add the constant and update the exchange runtime builder to select the correct URL.

#### Step 3.2: Adjust StreamsPerTicker by market type

**File:** `cmd/consumer/exchanges.go` (EXTEND `buildBinanceRuntime`)

Spot subscribes to 2 streams per ticker (aggTrade + depth).
Futures subscribes to 4 streams per ticker (aggTrade + depth + markPrice + forceOrder).

```go
func buildBinanceRuntime(...) consumerExchangeRuntime {
    managerCfg := baseManagerConfig(cfg, ex)

    // Adjust StreamsPerTicker based on market type
    isFutures := strings.Contains(strings.ToUpper(ex.MarketType), "FUTURES")
    if isFutures {
        managerCfg.StreamsPerTicker = 4
        if strings.TrimSpace(ex.BaseURL) == "" {
            ex.BaseURL = binance.DefaultFuturesWSBaseURL
        }
    } else {
        managerCfg.StreamsPerTicker = 2
    }

    // Enable markprice+liquidation for futures by default
    enableExtras := cfg.Consumer.EnableMarkPriceLiquidation || isFutures

    managerCfg.EndpointBuilder = func(bucket []string) string {
        endpoint, p := binance.BuildEndpoint(ex.BaseURL, bucket, enableExtras)
        // ...
    }
    // ... rest unchanged
}
```

#### Step 3.3: Config for multi-exchange with Binance split

**File:** `cmd/consumer/config.jsonc` (EXTEND)

Add example multi-exchange config:
```jsonc
{
    "consumer": {
        // Multi-exchange config — overrides legacy single-exchange fields
        "exchanges": [
            {
                "name": "binance-spot",
                "type": "binance",
                "tickers": ["BTCUSDT", "ETHUSDT"],
                "market_type": "SPOT"
                // base_url defaults to wss://stream.binance.com:9443/stream
            },
            {
                "name": "binance-futures",
                "type": "binance",
                "tickers": ["BTCUSDT", "ETHUSDT"],
                "market_type": "USD_M_FUTURES"
                // base_url defaults to wss://fstream.binance.com/stream
            },
            {
                "name": "bybit",
                "type": "bybit",
                "tickers": ["BTCUSDT", "ETHUSDT"],
                "market_type": "USD_M_FUTURES"
            }
        ]
    }
}
```

#### Step 3.4: Tests for spot/futures differentiation

**File:** `internal/adapters/exchange/binance/endpoint_test.go` (EXTEND)

Add tests:
- TestBuildEndpoint_SpotURL builds URL with `stream.binance.com` base
- TestBuildEndpoint_FuturesURL builds URL with `fstream.binance.com` base
- TestBuildEndpoint_Spot_TwoStreamsPerTicker (aggTrade + depth only)
- TestBuildEndpoint_Futures_FourStreamsPerTicker (aggTrade + depth + markPrice + forceOrder)

**File:** `cmd/consumer/exchanges_test.go` (NEW or EXTEND)

Add integration-style test:
- TestBuildExchangeRuntimes_BinanceSpotFuturesSplit verifies:
  - Spot runtime has StreamsPerTicker=2, correct base URL
  - Futures runtime has StreamsPerTicker=4, correct base URL
  - Both share the same parser (binance.ParseMessageWithMetaForMarketType)
  - Different subsystem keys (marketdata:binance-spot vs marketdata:binance-futures)

---

### PART 4: Funding Rate End-to-End Verification

#### Step 4.1: Verify markprice→funding flow exists

The funding rate pipeline is **already wired**:
- Binance parser: `parseMarkPriceUpdate` populates `domain.MarkPriceTickV1.FundingRate` from `r` field
- Bybit parser: `parseMarkPrice` populates `domain.MarkPriceTickV1.FundingRate` from `fundingRate` field
- IngestMarketData: publishes `marketdata.markprice` envelopes with full payload
- Processor: routes markprice events to normalization (deduplicate)

**This step is verification only.** Read the existing code and confirm the flow. If anything is missing, add it.

#### Step 4.2: Add funding rate parser tests

**File:** `internal/adapters/exchange/binance/parser_test.go` (EXTEND)

Add test case: `TestParseMarkPriceUpdate_WithFundingRate`
```go
{
    name: "markprice_with_funding_rate",
    input: `{"stream":"btcusdt@markPrice","data":{"e":"markPriceUpdate","E":1700000000000,"s":"BTCUSDT","p":"42000.50","i":"42001.00","r":"0.00010000"}}`,
    wantSkip: false,
    wantReq: app.IngestRequest{
        Venue: "BINANCE", Instrument: "BTCUSDT", EventType: "marketdata.markprice",
        Payload: domain.MarkPriceTickV1{
            MarkPrice: 42000.50, IndexPrice: 42001.0, FundingRate: 0.0001,
        },
    },
},
```

**File:** `internal/adapters/exchange/bybit/parser_test.go` (EXTEND)

Add test case: `TestParseMarkPrice_WithFundingRate`
```go
{
    name: "ticker_with_funding_rate",
    input: `{"topic":"tickers.BTCUSDT","type":"snapshot","ts":1700000000000,"data":{"symbol":"BTCUSDT","markPrice":"42000.50","indexPrice":"42001.00","fundingRate":"0.00010000"}}`,
    wantSkip: false,
    wantReq: app.IngestRequest{
        Venue: "BYBIT", Instrument: "BTCUSDT", EventType: "marketdata.markprice",
        Payload: domain.MarkPriceTickV1{
            MarkPrice: 42000.50, IndexPrice: 42001.0, FundingRate: 0.0001,
        },
    },
},
```

#### Step 4.3: E2E funding rate flow test

**File:** `internal/actors/aggregation/runtime/funding_rate_e2e_test.go` (NEW)

```go
func TestFundingRate_EndToEnd_BinanceMarkPrice(t *testing.T) {
    // 1. Create InMemoryBus + IngestMarketData service
    // 2. Create raw Binance markprice payload with funding rate
    // 3. Parse with binance.ParseMessage → IngestRequest
    // 4. Execute IngestMarketData → envelope published to bus
    // 5. Subscribe to bus, receive envelope
    // 6. Decode envelope payload → verify MarkPriceTickV1.FundingRate preserved
    // 7. Assert: funding rate value survives full parse→ingest→bus→decode roundtrip
}

func TestFundingRate_EndToEnd_BybitTicker(t *testing.T) {
    // Same pattern for Bybit
}
```

This proves the funding rate value is preserved across the entire pipeline boundary, not just in isolation.

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/adapters/exchange/binance/parser.go` | Binance parser — reference pattern for new parsers |
| `internal/adapters/exchange/binance/parser_test.go` | Binance parser tests — reference test pattern |
| `internal/adapters/exchange/binance/endpoint.go` | Binance endpoint builder — reference pattern |
| `internal/adapters/exchange/binance/endpoint_test.go` | Binance endpoint tests |
| `internal/adapters/exchange/bybit/parser.go` | Bybit parser — reference pattern (message-based subscriptions) |
| `internal/adapters/exchange/bybit/parser_test.go` | Bybit parser tests |
| `internal/adapters/exchange/bybit/endpoint.go` | Bybit endpoint builder — reference (subscriptions vs URL) |
| `internal/adapters/exchange/bybit/endpoint_test.go` | Bybit endpoint tests |
| `cmd/consumer/exchanges.go` | Exchange runtime builder — extend with Coinbase + HyperLiquid |
| `cmd/consumer/bootstrap.go` | Consumer composition root |
| `cmd/consumer/main.go` | Consumer CLI flags |
| `cmd/consumer/config.jsonc` | Consumer deploy config |
| `internal/actors/marketdata/runtime/parse.go` | ParseFunc/ParseFuncV2/ParseMeta contracts |
| `internal/actors/marketdata/runtime/subsystem.go` | SubsystemConfig + message dispatch |
| `internal/actors/marketdata/ws/manager.go` | ManagerConfig + bucket planning |
| `internal/actors/marketdata/ws/consumer.go` | WS consumer + reconnect |
| `internal/core/marketdata/domain/payloads.go` | TradeTickV1, BookDeltaV1, MarkPriceTickV1, LiquidationTickV1 |
| `internal/core/marketdata/domain/market_type.go` | MarketType enum (SPOT, USD_M_FUTURES, COIN_M_FUTURES) |
| `internal/core/marketdata/app/ingest.go` | IngestMarketData usecase + IngestRequest |
| `internal/shared/naming/naming.go` | CanonicalVenue, CanonicalInstrument |
| `internal/shared/config/schema.go` | ConsumerConfig + ConsumerExchangeConfig |
| `internal/shared/problem/problem.go` | Problem type |
| `internal/adapters/bus/inmemory_bus.go` | InMemoryBus for tests |

---

## Execution Rules

```bash
# All gates must pass after each part:
make test-workspace
make test-workspace-race
make docs-check
make invariants-check

# After all 4 parts:
make tidy
make test-workspace-race
```

### STOP CONDITIONS:
- New exchange parser returning different output types than existing parsers (must return `app.IngestRequest` + domain payload types)
- Cross-module import from `internal/adapters/exchange/coinbase` into `internal/core/` (layering violation)
- HyperLiquid book snapshots causing orderbook corruption in aggregation (test for this)
- Funding rate value mutation during parse → ingest → bus → decode roundtrip
- `naming.CanonicalInstrument` returning different values for same logical instrument across exchanges
- Binance spot accidentally subscribing to markPrice/forceOrder streams
- Binance futures missing markPrice/forceOrder streams
- Any `go.mod` missing required `replace` directive after new packages added

### Commit sequence:
```
feat(c2): add Coinbase exchange adapter (spot — trades, orderbook, ticker)

- Package: internal/adapters/exchange/coinbase/
- WS endpoint: wss://ws-feed.exchange.coinbase.com
- Message-based subscriptions: matches, level2_batch, ticker channels
- Handles: match → TradeTickV1, snapshot → BookDeltaV1, l2update → BookDeltaV1, ticker → MarkPriceTickV1
- Symbol normalization: BTC-USD → BTCUSD via naming.CanonicalInstrument
- Registered in exchanges.go as type "coinbase"
- Full table-driven parser tests with real Coinbase JSON fixtures

feat(c2): add HyperLiquid exchange adapter (perps — trades, l2Book)

- Package: internal/adapters/exchange/hyperliquid/
- WS endpoint: wss://api.hyperliquid.xyz/ws
- Per-ticker per-channel subscribe messages (2N messages for N tickers)
- Handles: trades → TradeTickV1, l2Book → BookDeltaV1 (full snapshot)
- Side normalization: "B"→"buy", "A"→"sell"
- Symbol normalization: bare coin "BTC" → "BTCUSD" (perps are USD-denominated)
- Trade IDs from blockchain tx hashes
- Registered in exchanges.go as type "hyperliquid"
- Full table-driven parser tests with real HyperLiquid JSON fixtures

feat(c2): split Binance spot/futures with correct URLs and stream counts

- Spot: wss://stream.binance.com:9443/stream, 2 streams/ticker (aggTrade + depth)
- Futures: wss://fstream.binance.com/stream, 4 streams/ticker (+ markPrice + forceOrder)
- EnableMarkPriceLiquidation auto-enabled for futures market types
- Multi-exchange config example with binance-spot + binance-futures
- Tests verify URL selection, stream count, and subsystem key differentiation

test(c2): verify funding rate E2E flow from parser through bus

- Binance markprice with funding rate → full roundtrip preserved
- Bybit ticker with funding rate → full roundtrip preserved
- Parser-level tests for funding rate extraction

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **No new go.mod files** — Coinbase and HyperLiquid packages go under `internal/adapters/exchange/` which already has its own go.mod. No new modules.
2. **No new domain types** — All exchanges must map to existing domain payloads (TradeTickV1, BookDeltaV1, MarkPriceTickV1, LiquidationTickV1). If an exchange doesn't support a payload type (e.g., Coinbase has no liquidations), simply don't emit it.
3. **Parser purity** — Parsers must be pure functions (no side effects, no network calls, no state). They receive `[]byte` and return `IngestRequest`. All state management is in the actor layer.
4. **Backward compatibility** — Existing Binance and Bybit adapters must continue working unchanged. The spot/futures split is additive (new config options, default behavior preserved).
5. **HyperLiquid book is always a full snapshot** — Do NOT attempt incremental delta tracking. Every l2Book message replaces the full book. The aggregation layer handles this correctly because BookDeltaV1 represents a set of levels.
6. **Coinbase has no sequence IDs for orderbook** — Unlike Binance (U/u/pu) and Bybit (seq/u/pu), Coinbase doesn't provide monotonic sequence IDs. Use timestamp-based fallback for FinalID.
7. **Symbol normalization must be exchange-specific** — Each exchange has its own symbol format. The parser normalizes to canonical format before building IngestRequest. Do not share normalization code across exchanges.
8. **go.mod hygiene** — Run `make tidy` after adding new packages. Verify no missing `replace` directives.
9. **`*problem.Problem` at boundaries** — All parser error returns use `*problem.Problem`, not plain `error`.
10. **Metrics cardinality** — New exchanges add venue labels. Follow `docs/observability/metrics-policy.md`.
