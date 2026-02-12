# RFC-0010 — W9: Multi-Exchange Readiness

**Status:** Proposed
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W9 of PRD-0001
**Relates to:** ADR-0017 (Multi-Exchange Normalization), ADR-0018 (Topology), ADR-0011 (Binance Mapping)

---

## 1. Goal

Validate that the architecture supports multiple exchanges in a single process without changes to core domain logic. After W9:
- A second exchange adapter (Bybit) exists with trade + bookdelta parsing
- Two `MarketDataSubsystem` instances run independently in a single Guardian
- Guardian supports string-based `SubsystemKey` for dynamic subsystem registration
- Cross-venue canonical normalization is validated (`BTCUSDT` on Binance == `BTCUSDT` on Bybit)
- No code in `internal/core/` references exchange-specific types or constants

## 2. Scope

- Create Bybit exchange adapter: WS endpoint builder, trade + bookdelta parser
- Extend Guardian to support `SubsystemKey` (string) instead of `Subsystem` (enum)
- Add multi-exchange config to `config.AppConfig`
- Wire two MarketDataSubsystems in `cmd/consumer`
- Validate cross-venue Subject routing in delivery
- Add MEX-4 grep audit to CI

## 3. Non-Goals

- Full Bybit API coverage (only trade + bookdelta for validation)
- Production-ready Bybit deployment (this is architectural validation)
- Cross-venue arbitrage logic (delivery routing only)
- REST API for instrument discovery (hardcoded catalog for now)
- Bybit futures/perp support (SPOT only for validation)

## 4. Affected Modules

| File | Action | Change |
|------|--------|--------|
| `internal/adapters/exchange/bybit/endpoint.go` | CREATE | WS endpoint builder for Bybit |
| `internal/adapters/exchange/bybit/parser.go` | CREATE | ParseMessage for trade + bookdelta |
| `internal/adapters/exchange/bybit/parser_test.go` | CREATE | Unit tests with Bybit JSON samples |
| `internal/adapters/exchange/bybit/catalog.go` | CREATE | Hardcoded InstrumentCatalog for Bybit |
| `internal/adapters/exchange/bybit/catalog_test.go` | CREATE | Catalog resolution tests |
| `internal/actors/runtime/guardian.go` | ALTER | SubsystemKey (string) replaces Subsystem (enum) |
| `internal/actors/runtime/protocol.go` | ALTER | Messages use SubsystemKey |
| `internal/actors/runtime/guardian_test.go` | ALTER | Tests use SubsystemKey |
| `internal/shared/config/schema.go` | ALTER | Multi-exchange config: []ExchangeConfig |
| `cmd/consumer/main.go` | ALTER | Spawn N MarketDataSubsystems from config |
| `cmd/consumer/config.jsonc` | ALTER | Multi-exchange config example |
| `Makefile` | ALTER | Add `audit-core-purity` target |

## 5. Bybit Adapter Design

### WS Endpoint

```go
package bybit

// Bybit WebSocket V5 API
// Spot stream: wss://stream.bybit.com/v5/public/spot
// Topics: "publicTrade.BTCUSDT", "orderbook.50.BTCUSDT"

type EndpointBuilder struct {
    baseURL string
}

func NewEndpointBuilder() *EndpointBuilder

// BuildURL returns the WebSocket URL for a set of instruments.
// Bybit requires subscribing via JSON message after connect (not URL path).
func (b *EndpointBuilder) BuildURL(marketType domain.MarketType) string

// BuildSubscribeMessage returns the subscription JSON for given instruments.
func (b *EndpointBuilder) BuildSubscribeMessage(instruments []string) []byte
```

### Parser

```go
// ParseMessage handles Bybit V5 WebSocket messages.
// Trade format:
// {"topic":"publicTrade.BTCUSDT","ts":1710000001000,"type":"snapshot",
//  "data":[{"T":1710000001000,"s":"BTCUSDT","S":"Buy","v":"0.01","p":"65000.50","i":"123456"}]}
//
// Orderbook format:
// {"topic":"orderbook.50.BTCUSDT","ts":1710000001000,"type":"delta",
//  "data":{"s":"BTCUSDT","b":[["65000.00","0.5"]],"a":[["65001.00","0.3"]],"u":12345,"seq":67890}}

type ParseFunc func(data []byte, recvAt time.Time) (app.IngestRequest, bool, *problem.Problem)

func MakeParseFunc(catalog InstrumentCatalog, venue string) ParseFunc
```

### Instrument Catalog (Hardcoded Phase 1)

```go
type HardcodedCatalog struct {
    instruments map[string]domain.InstrumentMetadata
}

func NewHardcodedCatalog() *HardcodedCatalog

// Pre-populated with top pairs:
// BTCUSDT → {Canonical: "BTCUSDT", Base: "BTC", Quote: "USDT", MarketType: SPOT}
// ETHUSDT → {Canonical: "ETHUSDT", Base: "ETH", Quote: "USDT", MarketType: SPOT}

func (c *HardcodedCatalog) Resolve(venueSymbol string) (domain.InstrumentMetadata, *problem.Problem)
func (c *HardcodedCatalog) ListInstruments(marketType domain.MarketType) ([]domain.InstrumentMetadata, *problem.Problem)
```

## 6. Guardian SubsystemKey Extension

### Current

```go
type Subsystem int
const (
    SubsystemMarketData Subsystem = iota
    SubsystemAggregation
    SubsystemDelivery
)

type GuardianConfig struct {
    Factories map[Subsystem]actor.Producer
}
```

### Proposed

```go
type SubsystemKey string

const (
    SubsystemAggregation SubsystemKey = "aggregation"
    SubsystemDelivery    SubsystemKey = "delivery"
    // MarketData subsystems are dynamic: "marketdata:binance", "marketdata:bybit"
)

func MarketDataKey(exchange string) SubsystemKey {
    return SubsystemKey("marketdata:" + strings.ToLower(exchange))
}

type GuardianConfig struct {
    Factories          map[SubsystemKey]actor.Producer
    ExpectedSubsystems []SubsystemKey
    // ...
}
```

### Migration

- `Subsystem` type changes from `int` to `string` (SubsystemKey)
- All references in Guardian, protocol messages, tests update accordingly
- Snapshot, ChildFailed, ReadyNotify messages use `SubsystemKey`
- SupervisorPolicy keyed by `SubsystemKey`

## 7. Multi-Exchange Config

### Config Schema

```go
type ExchangeConfig struct {
    Name       string   `json:"name"`        // "binance", "bybit"
    Enabled    bool     `json:"enabled"`     // true
    Tickers    []string `json:"tickers"`     // ["BTCUSDT", "ETHUSDT"]
    MarketType string   `json:"market_type"` // "spot"
    WS         WSConfig `json:"ws"`
}

type AppConfig struct {
    // ...existing...
    Exchanges []ExchangeConfig `json:"exchanges"`
}
```

### Config JSONC Example

```jsonc
{
  "exchanges": [
    {
      "name": "binance",
      "enabled": true,
      "tickers": ["BTCUSDT", "ETHUSDT"],
      "market_type": "spot",
      "ws": {
        "max_websocket_lifetime": "23h",
        "reconnect_backoff_max": "30s"
      }
    },
    {
      "name": "bybit",
      "enabled": true,
      "tickers": ["BTCUSDT", "ETHUSDT"],
      "market_type": "spot",
      "ws": {
        "max_websocket_lifetime": "23h",
        "reconnect_backoff_max": "30s"
      }
    }
  ]
}
```

### Wiring in cmd/consumer

```go
for _, exCfg := range cfg.Exchanges {
    if !exCfg.Enabled {
        continue
    }
    key := runtime.MarketDataKey(exCfg.Name)
    factories[key] = makeSubsystemProducer(exCfg)
    expectedSubs = append(expectedSubs, key)
}
```

## 8. Cross-Venue Normalization Validation

### Test: Same canonical instrument across exchanges

```go
func TestCrossVenueNormalization(t *testing.T) {
    binance := naming.CanonicalInstrument("BTCUSDT")  // from Binance
    bybit := naming.CanonicalInstrument("BTCUSDT")    // from Bybit

    assert.Equal(t, binance, bybit) // same canonical
}
```

### Test: Independent subsystems

```go
func TestMultiExchangeIndependence(t *testing.T) {
    // Spawn Guardian with two MarketDataSubsystems
    // Poison "marketdata:binance"
    // Assert "marketdata:bybit" still running
    // Assert Guardian snapshot shows one degraded, one running
}
```

### CI Grep Audit (MEX-4)

```bash
# internal/core/ must not reference exchange-specific types
if grep -rn '"binance"\|"bybit"\|"okx"\|exchange\.Binance\|exchange\.Bybit' internal/core/; then
    echo "FAIL: exchange-specific references found in internal/core/"
    exit 1
fi
```

## 9. Subject Routing for Delivery

With multiple exchanges publishing to the same bus, delivery clients can subscribe with wildcards:

```
# Bybit trades for BTCUSDT:
marketdata.trade.v1.bybit.BTCUSDT

# All venues, BTCUSDT trades:
marketdata.trade.v1.*.BTCUSDT

# All Binance events:
marketdata.*.v1.binance.*
```

The `SubjectFromEnvelope()` function (RFC-0005/W4) already produces venue-qualified subjects. Delivery routing works unchanged — venue is part of the subject, not a separate routing dimension.

## 10. Test Plan

| Type | Test | Pass Criteria |
|------|------|---------------|
| Unit | Bybit parser: trade JSON → IngestRequest | Fields correctly mapped |
| Unit | Bybit parser: bookdelta JSON → IngestRequest | Bids/asks correctly parsed |
| Unit | Bybit catalog: Resolve("BTCUSDT") | Returns correct InstrumentMetadata |
| Unit | `naming.CanonicalInstrument` same result for Binance/Bybit symbols | Equality |
| Unit | `MarketDataKey("binance")` == `"marketdata:binance"` | String match |
| Integration | Two subsystems in Guardian: both start and report Ready | ReadyResponse.Ready == true |
| Integration | Poison one subsystem: other continues running | Snapshot confirms |
| Integration | Two subsystems publish to same bus: consumer receives from both | Venue field distinguishes |
| Regression | Single-exchange config still works | Existing tests pass |
| Audit | `grep` for exchange names in `internal/core/` | 0 matches |

## 11. Acceptance Criteria

- [ ] Bybit adapter parses at least trade + bookdelta events correctly
- [ ] Bybit adapter normalizes venue to `"BYBIT"` (uppercase)
- [ ] Bybit adapter uses `naming.CanonicalInstrument` for instrument normalization
- [ ] Guardian supports `SubsystemKey` (string) for dynamic subsystem registration
- [ ] Two MarketDataSubsystems run in same process without interference
- [ ] Poison one subsystem: other continues running (integration test)
- [ ] Multi-exchange config in `config.jsonc` documented with examples
- [ ] `naming.CanonicalInstrument("BTCUSDT")` same result regardless of source exchange
- [ ] No code in `internal/core/` references `"binance"`, `"bybit"`, or exchange-specific constants
- [ ] `SubjectFromEnvelope` includes venue in subject (cross-venue routing works)
- [ ] Single-exchange mode still works (regression: existing tests pass)
- [ ] `go test -race ./...` green across all modules
