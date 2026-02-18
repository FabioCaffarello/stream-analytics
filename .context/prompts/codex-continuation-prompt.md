# Codex Continuation Prompt — Market Raccoon Backend Evolution

## Project Identity

Market Raccoon is a high-performance market intelligence platform (NOT a trading platform). It aggregates multi-venue crypto market data, builds real-time read models, and delivers low-latency streams to clients. The architecture is inspired by marketmonkey but redesigned with DDD, hexagonal architecture, and Hollywood actor model.

**Language:** Go 1.25+ | **Actor Framework:** Hollywood | **Bus:** NATS JetStream | **Storage:** TimescaleDB (hot) + ClickHouse (cold)

---

## Workspace Structure

```
go.work modules:
  ./cmd/consumer          — market data ingestion binary
  ./cmd/processor         — aggregation/insights binary
  ./cmd/server            — HTTP + WS delivery binary
  ./cmd/store             — storage writer binary
  ./internal/shared       — foundation primitives (problem, result, validation, envelope, codec, hash, naming, clock, config, metrics, ds)
  ./internal/core/marketdata    — MarketData bounded context (ingest, normalize, dedup)
  ./internal/core/aggregation   — Aggregation bounded context (orderbook, candles, stats)
  ./internal/core/insights      — Insights bounded context (heatmap, volume profile, cross-venue joins)
  ./internal/core/delivery      — Delivery bounded context (sessions, subscriptions, backpressure)
  ./internal/actors             — Actor runtime (guardian, subsystem actors, ws manager/consumer)
  ./internal/adapters           — Adapters (bus, jetstream, exchange parsers, storage stubs)
  ./internal/interfaces         — HTTP server, WS server
```

**Module dependency rule:** Every `internal/core/*` module has its own `go.mod` with `require github.com/market-raccoon/internal/shared v0.0.0` and a `replace` directive pointing to the local path. Replace directives ARE required even in workspace.

---

## Mandatory Patterns (MUST follow exactly)

### Errors: `*problem.Problem` (never plain `error` in domain/app)

```go
// Construction
problem.New(problem.ValidationFailed, "venue must not be empty")
problem.Newf(problem.Internal, "failed to marshal: %v", err)
problem.Wrap(err, problem.Internal, "context")

// Problem codes: VAL_*, SYS_*, MD_*, AGG_*, DEL_*
```

### Validation: `validation.*` returns `*problem.Problem`

```go
if p := validation.Collect(
    validation.NonEmptyString("venue", venue),
    validation.NonEmptyString("instrument", instrument),
    validation.Positive("price", price),
); p != nil {
    return nil, p
}
```

### Domain constructors: always validate

```go
func NewCandle(venue, instrument, timeframe string) (*Candle, *problem.Problem) {
    if p := validation.Collect(
        validation.NonEmptyString("venue", venue),
        validation.NonEmptyString("instrument", instrument),
        validation.OneOf("timeframe", timeframe, allowedTimeframes),
    ); p != nil {
        return nil, p
    }
    return &Candle{venue: venue, instrument: instrument, timeframe: timeframe}, nil
}
```

### Use case: `Execute(ctx, req) (response, *problem.Problem)`

```go
type BuildCandleFromEvents struct {
    publisher ports.ArtifactPublisher
    store     ports.HotReadModelStore
    candles   *ds.BoundedMap[CandleKey, *domain.Candle]
    cfg       BuildCandleConfig
}

func NewBuildCandleFromEvents(pub ports.ArtifactPublisher, store ports.HotReadModelStore, cfg BuildCandleConfig) *BuildCandleFromEvents {
    if cfg.MaxCandles <= 0 { cfg.MaxCandles = 50_000 }
    // ...
    return &BuildCandleFromEvents{publisher: pub, store: store, cfg: cfg}
}

func (uc *BuildCandleFromEvents) Execute(ctx context.Context, req BuildCandleRequest) (BuildCandleResponse, *problem.Problem) {
    // 1. validate
    // 2. get or create aggregate
    // 3. apply business logic
    // 4. persist + publish side effects
    return BuildCandleResponse{...}, nil
}
```

### Envelope (event contract — ADR-0002)

```go
envelope.Envelope{
    Type:           "aggregation.candle",
    Version:        1,
    Venue:          "binance",
    Instrument:     "btc-perp",
    TsExchange:     tsExchange,
    TsIngest:       tsIngest,
    Seq:            seq,
    IdempotencyKey: hash.HashFields(venue, instrument, timeframe, windowStart),
    ContentType:    "application/json",
    Payload:        payloadBytes,
}
```

### Naming normalization (at domain boundaries)

```go
venue := naming.CanonicalVenue(rawVenue)           // "binance" → "BINANCE"
instrument := naming.CanonicalInstrument(rawInstr)  // "BTC-PERP" → "BTCPERP"
```

### Hashing (deterministic idempotency)

```go
key := hash.HashFields(venue, instrument, timeframe, fmt.Sprintf("%d", windowStartTs))
```

### Testing: fakes + helpers + t.Helper()

```go
type fakePublisher struct {
    artifacts []domain.CandleArtifact
    err       *problem.Problem
}
func (f *fakePublisher) PublishCandle(_ context.Context, a domain.CandleArtifact) *problem.Problem {
    if f.err != nil { return f.err }
    f.artifacts = append(f.artifacts, a)
    return nil
}

func newUC(t *testing.T) (*app.BuildCandleFromEvents, *fakePublisher, *fakeStore) {
    t.Helper()
    pub := &fakePublisher{}
    store := &fakeStore{}
    uc := app.NewBuildCandleFromEvents(pub, store, app.BuildCandleConfig{})
    return uc, pub, store
}
```

### Import order

```go
import (
    "context"          // 1. stdlib
    "time"

    "github.com/anthdm/hollywood/actor"  // 2. external

    "github.com/market-raccoon/internal/core/aggregation/domain"  // 3. monorepo
    "github.com/market-raccoon/internal/shared/problem"
    "github.com/market-raccoon/internal/shared/validation"
)
```

### Layering invariants (enforced by `make invariants-check`)

- `core/` CANNOT import `actors/`, `adapters/`, `interfaces/`, policykit
- `core/` CANNOT call `time.Now()` — inject `clock.Clock`
- `actors/` CANNOT import `interfaces/`
- Domain packages import only `shared/problem`, `shared/validation`, `shared/naming`
- No cross-BC domain imports (only through ports)

---

## Current State (as-of 2026-02-18)

### EXISTING and WORKING:
- **MarketData ingest:** Binance + Bybit parsers, IngestMarketData use case, markprice/liquidation normalization + dedup
- **Aggregation:** OrderBook aggregate (BTree, crossed-book detection, bounded levels), UpdateOrderBookFromEvents use case
- **Insights:** BuildHeatmap (517 LOC), BuildVolumeProfile (433 LOC), JoinCrossVenueTrades, InsightsService facade, VPVREmitPolicy (overload L0-L3)
- **Delivery:** Session actor, Router actor, Subject/Topic/Filter domain, BackpressurePolicy skeleton (`drop_newest`)
- **Actors runtime:** Guardian with supervision, readiness, restart limiter; MarketDataSubsystem, AggregationSubsystem actors
- **Infrastructure:** Config JSONC loader, bootstrap composition root, HTTP server (/healthz, /readyz, /runtime/snapshot), WS server with format negotiation
- **Foundation:** problem, result, validation, envelope, codec, hash, naming, clock, metrics (backpressure_drops_total, bus_dropped_total), BoundedMap, replay player/sequencer
- **JetStream:** Publisher + Consumer with durable restart, Msg-ID dedup, ACK/NAK/TERM conformance, subject validation
- **Protobuf:** Schemas v1, buf toolchain, payload registry, semantic equivalence tests
- **Tests:** Golden replay, race-safe, soak (VPVR overload, consumer, WS, boundedmap)

### TODO (prioritized):

**M1 — Storage Writers + Idempotency + ACK-on-Commit** (5 commits)
1. `docs(storage)`: freeze storage boundary contract
2. `feat(m1)`: consolidate envelope for strong idempotency
3. `feat(m1)`: add writer hot/cold ports in aggregation core
4. `fix(runtime)`: ensure ACK only after writer commit
5. `test(replay)`: validate idempotency and ACK boundary with golden

**M2 — Delivery WS Snapshots + Backpressure** (4 commits)
1. `docs(delivery)`: fix WS snapshot contract and backpressure policy
2. `docs(delivery)`: sync feature-pack
3. `feat(m2)`: bounded queue per session + keep-latest for non-critical streams
4. `test(replay)`: deterministic getrange + slow-client coverage

**M3 — Heatmap + Volume Profiles Completion** (remaining: writers + replay tests)
- M3.C2 (builders) is ALREADY DONE
- Remaining: M3.C1 (docs contracts), M3.C3 (storage writers), M3.C4 (replay rebuild tests)

**M4 — Candle + Stats Aggregation (NEW)** — biggest product parity gap
- **Candle aggregation (OHLCV):** Multi-timeframe (1m, 5m, 15m, 30m, 1h) from trade events
  - Architecture doc: `docs/architecture/candle-aggregation.md`
  - Feature pack: `.context/docs/feature-packs/candle-aggregation.md`
  - Invariants: CA-1 (deterministic), CA-2 (closed immutable), CA-3 (fixed timeframes), CA-4 (replay stable), CA-5 (high>=low), CA-6 (volume=buy+sell), CA-7 (bounded open candles)
  - Domain model: `internal/core/aggregation/domain/candle.go` (TODO)
  - Use case: `internal/core/aggregation/app/build_candle.go` (TODO)
  - Subject: `aggregation.candle.v1.{venue}.{instrument}`
- **Stats aggregation (liq/funding/markprice per TF):** Windowed stats from multiple input types
  - Architecture doc: `docs/architecture/stats-aggregation.md`
  - Feature pack: `.context/docs/feature-packs/stats-aggregation.md`
  - Invariants: ST-1 (non-negative), ST-2 (closed immutable), ST-3 (deterministic), ST-4 (replay stable), ST-5 (bounded), ST-6 (partial inputs ok)
  - Domain model: `internal/core/aggregation/domain/stats.go` (TODO)
  - Use case: `internal/core/aggregation/app/build_stats.go` (TODO)
  - Subject: `aggregation.stats.v1.{venue}.{instrument}`

---

## Execution Rules

### Before EVERY commit:
```bash
make docs-check           # documentation governance
make invariants-check     # domain isolation + layering + determinism
```

### Before commits touching runtime code:
```bash
make test-workspace       # all modules
make test-workspace-race  # with -race flag
```

### Before commits touching replay/golden:
```bash
go test ./internal/shared/replay -run TestGoldenReplay
go test ./cmd/consumer -run TestReplayIngestGolden1000
```

### STOP CONDITIONS (halt immediately if any occur):
- Determinism break in golden replay (byte/hash mismatch)
- Subject taxonomy drift from `docs/contracts/event-bus.md`
- ACK emitted before durable commit (ack-on-enqueue)
- Idempotency key collision/duplication
- Replay rebuild inconsistency between hot/cold paths
- Cross-BC domain import (core → actors, core → adapters)

### Commit message format (conventional commits, max 72 char title):
```
feat(m4): add candle OHLCV domain aggregate with invariants

- CandleV1 aggregate with Open/High/Low/Close/Volume
- Validation: CA-1..CA-7 invariants enforced in constructor
- BoundedMap keyed by (venue, instrument, timeframe)
```

---

## Reference Files (read these for context)

| File | Purpose |
|---|---|
| `docs/architecture/TRUTH-MAP.md` | Single source of truth map |
| `docs/rfcs/RFC-0011-product-parity-marketmonkey.md` | Product parity gaps and matrix |
| `.context/plans/retomada-market-parity-commit-chain.md` | M1-M4 commit chains with gates |
| `docs/architecture/candle-aggregation.md` | Candle aggregation architecture |
| `docs/architecture/stats-aggregation.md` | Stats aggregation architecture |
| `internal/core/aggregation/domain/orderbook.go` | Pattern: domain aggregate |
| `internal/core/aggregation/app/update_orderbook.go` | Pattern: use case |
| `internal/core/insights/domain/heatmap_bucket.go` | Pattern: insights domain model |
| `internal/core/insights/app/build_heatmap.go` | Pattern: insights builder use case |
| `internal/core/insights/app/service.go` | Pattern: BC service facade |
| `internal/shared/problem/problem.go` | Error model |
| `internal/shared/validation/validation.go` | Validation primitives |
| `internal/shared/envelope/envelope.go` | Event envelope contract |

---

## Task: Execute M4 — Candle + Stats Aggregation

### Phase 1: Candle Domain Model

Create `internal/core/aggregation/domain/candle.go`:
- `CandleV1` struct: venue, instrument, timeframe, windowStartTs, windowEndTs, open, high, low, close, volume, buyVolume, sellVolume, tradeCount, seqFirst, seqLast, isClosed
- `CandleKey` value object: venue + instrument + timeframe
- Constructor `NewCandleV1(venue, instrument, timeframe, windowStartTs)` returning `(*CandleV1, *problem.Problem)`
- `ApplyTrade(price, qty float64, isBuy bool, seq int64)` mutation method
- `Close(windowEndTs int64)` to finalize
- `Validate() *problem.Problem` enforcing CA-1 through CA-7
- Allowed timeframes constant: `[]string{"1m", "5m", "15m", "30m", "1h"}`
- Domain event: `CandleClosed` with EventName() string

Create `internal/core/aggregation/domain/candle_test.go`:
- TestCandleV1_NewValidation (empty venue, invalid timeframe)
- TestCandleV1_ApplyTrade_OHLCV (open set on first trade, high/low updated correctly)
- TestCandleV1_Close_Immutability (cannot apply trade after close)
- TestCandleV1_VolumeInvariant (volume = buy + sell always)
- TestCandleV1_HighLowInvariant (high >= max(open,close), low <= min(open,close))
- TestCandleV1_Deterministic (same trades in same order = same candle)

### Phase 2: Candle Use Case

Create `internal/core/aggregation/app/build_candle.go`:
- `BuildCandleFromEvents` use case struct
- Config: MaxCandles, WindowDuration per timeframe, Clock
- `Execute(ctx, req)` processing trade events into candle updates
- BoundedMap[CandleKey, *CandleV1] for open candles
- Window close logic based on ts_ingest
- Publish `CandleClosed` event via ArtifactPublisher port

Create `internal/core/aggregation/app/build_candle_test.go`:
- TestBuildCandle_SingleTrade_CreatesOpenCandle
- TestBuildCandle_WindowClose_EmitsCandleClosed
- TestBuildCandle_MultiTimeframe_1mCascades (5m built from 1m closes)
- TestBuildCandle_Deterministic_SameInputSameOutput
- TestBuildCandle_BoundedMap_EvictsOldest

### Phase 3: Stats Domain Model

Create `internal/core/aggregation/domain/stats.go`:
- `StatsWindowV1` struct: venue, instrument, timeframe, windowStartTs, windowEndTs, liqBuyVolume, liqSellVolume, liqTotalVolume, liqCount, markpriceOpen, markpriceHigh, markpriceLow, markpriceClose, fundingRateAvg, fundingRateLast, seqFirst, seqLast, isClosed
- Constructor and validation (ST-1 through ST-6)
- `ApplyLiquidation(...)`, `ApplyMarkPrice(...)`, `ApplyFundingRate(...)` mutation methods
- `Close()` finalization

Create `internal/core/aggregation/domain/stats_test.go`

### Phase 4: Stats Use Case

Create `internal/core/aggregation/app/build_stats.go`:
- Similar pattern to BuildCandleFromEvents but consuming multiple input types
- Partial stats tolerance (ST-6): missing funding rate produces partial window, not error

Create `internal/core/aggregation/app/build_stats_test.go`

### Phase 5: AggregationService Facade Update

Update `internal/core/aggregation/app/service.go`:
- Add `Candle *BuildCandleFromEvents` and `Stats *BuildStatsFromEvents` fields
- Wire in constructor

### Phase 6: Update go.mod if needed

Ensure `internal/core/aggregation/go.mod` has all required dependencies and replace directives.

### Verification:
```bash
make docs-check && make invariants-check && make test-workspace && make test-workspace-race
```

---

## Important Constraints

1. **No time.Now() in domain/app** — inject `clock.Clock` (use `clock.FakeClock` in tests)
2. **No plain error** — always `*problem.Problem`
3. **No cross-BC imports** — aggregation domain CANNOT import insights domain
4. **No floating-point comparison** — use deterministic integer math or fixed-precision for prices
5. **Bounded everything** — every map, queue, buffer must have a hard cap
6. **Deterministic replay** — given same input sequence, output must be byte-identical
7. **Timescale storage is OUT OF SCOPE** — only define ports/interfaces, don't implement writers
8. **Subject root `aggregation` is accepted** — runtime validator already supports it
