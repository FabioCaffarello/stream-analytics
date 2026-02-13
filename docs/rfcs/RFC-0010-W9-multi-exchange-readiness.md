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

## 12. W9-1 Evidence (Implemented 2026-02-12)

### 12.1 Config Examples

Legacy single-exchange (still supported, unchanged):

```jsonc
{
  "consumer": {
    "exchange": "binance",
    "market_type": "SPOT",
    "tickers": ["BTC-USDT", "ETH-USDT"],
    "binance_ws_base_url": "wss://stream.binance.com:9443/stream"
  }
}
```

Multi-exchange config (new):

```jsonc
{
  "consumer": {
    "exchanges": [
      {
        "name": "binance",
        "type": "binance",
        "base_url": "wss://stream.binance.com:9443/stream",
        "tickers": ["BTC-USDT", "ETH-USDT"],
        "market_type": "SPOT"
      },
      {
        "name": "bybit",
        "type": "bybit",
        "base_url": "wss://stream.bybit.com/v5/public/spot",
        "tickers": ["BTC-USDT", "ETH-USDT"],
        "market_type": "SPOT"
      }
    ]
  }
}
```

### 12.2 Commands Executed

```bash
go test ./...                       # cmd/consumer
go test ./...                       # internal/adapters
go test ./...                       # internal/shared
go test ./...                       # internal/core/marketdata
go test ./...                       # internal/actors
make test-workspace GO_TEST_FLAGS='-race'
pre-commit run -a
```

All commands above passed in this implementation run.

### 12.2.1 W9-3 Feature Subject Validation Gate (Implemented 2026-02-12)

#### Rule Added

Fail-fast validation now cross-checks feature flags against required JetStream subject families:

- `processor.insights.enable_crossvenue_join=true`
  - requires `processor.insights.join_trades_subject` to be a valid NATS subject pattern in `marketdata.trade` family
  - requires runtime input filters to cover trade subjects for all configured exchanges (including wildcard coverage)
- `replay.mode=jetstream`
  - requires `replay.jetstream.subject_filter` to be a valid NATS subject pattern
  - requires `replay.jetstream.subject_filter` to include `marketdata.*` family coverage
- `processor.insights.snapshot_subject_prefix` (when set)
  - must be a concrete publish prefix without wildcards and must start with `insights.`

Validation messages are short and actionable, include the failing key, and include an example subject.

#### Example Config Snippet

```jsonc
{
  "bus": {"type": "jetstream"},
  "jetstream": {
    "filter_subjects": ["marketdata.bookdelta.v1.>"]
  },
  "processor": {
    "insights": {
      "enable_crossvenue_join": true,
      "join_trades_subject": "marketdata.trade.v1.>",
      "snapshot_subject_prefix": "insights.crossvenue.trade_snapshot.v1"
    }
  },
  "replay": {
    "mode": "off",
    "jetstream": {
      "subject_filter": "marketdata.>"
    }
  }
}
```

#### Test Evidence

New tests in `internal/shared/config/loader_test.go`:

- `TestJoinEnabled_MissingSubjects_Fails`
- `TestJoinEnabled_SubjectsPresent_Passes`
- `TestReplayJetStream_MissingSubjects_Fails`
- `TestDefaults_NoBehaviorChange`

### 12.3 What Changed

- Added `internal/adapters/exchange/bybit/` with:
  - `endpoint.go` + `endpoint_test.go`
  - `parser.go` + `parser_test.go`
- Extended `consumer` config with `consumer.exchanges[]` including:
  - legacy -> multi synthesis
  - deterministic ordering normalization
  - validation for duplicates, empty tickers, market_type, exchange type
- Updated `cmd/consumer` wiring to build one marketdata subsystem per configured exchange.
- Extended Guardian runtime handling to include dynamic subsystem keys (for `marketdata:<name>`), while preserving legacy behavior for single exchange.
- Extended marketdata stream identity partitioning to include `market_type` (`venue + instrument + market_type`) without changing envelope subject format.
- Added integration-oriented tests in `cmd/consumer/main_test.go` proving Binance + Bybit parse/ingest in the same process path.

### 12.4 What Did NOT Change

- No bus semantic changes.
- No routing protocol or envelope subject format changes.
- No JetStream behavior changes.
- No actor topology redesign beyond enabling N configured marketdata subsystems.
- No dual-write.
- No protobuf imports added into `internal/core/*`, `internal/actors/*`, or `internal/interfaces/*`.
- No full orderbook snapshot pipeline introduced.

### 12.5 Explicit Invariants Checked

- Stream identity partitioning: `venue + instrument + market_type`.
- Subject format unchanged: `{type}.v{version}.{venue_lower}.{instrument}`.
- Canonical instrument normalization remains stable (`naming.CanonicalInstrument`).
- Legacy single-exchange runtime still starts with equivalent behavior.
- Multi-exchange readiness tracks all configured marketdata subsystem keys.

## 13. W9-2 E2E Consumer Gate (Implemented 2026-02-12)

### 13.1 Scope

- Added opt-in process gate for `cmd/consumer` under `E2E_TEST_MODE=1`.
- Default runtime path is unchanged when `E2E_TEST_MODE` is not set.

### 13.2 E2E Probe Endpoints

When enabled:
- `GET /healthz` -> `200`
- `GET /readyz` -> `200` only when Guardian reports all expected subsystems ready
- `GET /metrics` -> Prometheus exposition from shared registry

### 13.3 No External Network Dependency

- In E2E mode, consumer uses deterministic in-process message feed per exchange subsystem.
- Real WS manager/dial path is bypassed only in E2E mode.
- Feed emits valid Binance and Bybit trade/bookdelta payloads so ingest/WS metrics advance.

### 13.4 Integration Test

Added:
- `cmd/consumer/e2e_consumer_integration_test.go` (`//go:build integration`)

Flow:
1. Build real binary `go build ./cmd/consumer`
2. Start process with multi-ex config (`binance` + `bybit`) and `E2E_TEST_MODE=1`
3. Poll `/readyz` until `200`
4. Poll `/metrics` and assert per-exchange label series exist (binance + bybit)
5. Send `SIGTERM`, require graceful exit within 10s
6. Restart and re-validate readiness/metrics

### 13.5 Commands Executed

```bash
go test -tags=integration ./cmd/consumer -run TestE2EConsumerMultiExchange -count=1
make test-workspace GO_TEST_FLAGS='-race'
pre-commit run -a
```

All commands above passed in this implementation run.
