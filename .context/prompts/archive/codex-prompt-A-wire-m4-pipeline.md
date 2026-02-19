# Codex Prompt A — Wire M4 Candle+Stats into Processor Pipeline

## Project Identity

Market Raccoon is a high-performance market intelligence platform (NOT a trading platform). Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

**Language:** Go 1.25+ | **Actor Framework:** Hollywood | **Bus:** NATS JetStream

---

## Context

M4 (Candle + Stats aggregation) domain models and use cases are ALREADY IMPLEMENTED and tested:
- `internal/core/aggregation/domain/candle.go` — CandleV1 aggregate (OHLCV, fixed-point arithmetic)
- `internal/core/aggregation/domain/stats.go` — StatsWindowV1 aggregate (liquidation/markprice/funding)
- `internal/core/aggregation/app/build_candle.go` — BuildCandleFromEvents use case
- `internal/core/aggregation/app/build_stats.go` — BuildStatsFromEvents use case
- `internal/core/aggregation/app/service.go` — AggregationService facade with UpdateBook, Candle, Stats
- `internal/core/aggregation/ports/ports.go` — ArtifactPublisher (4 methods), CandleHotReadModelStore, StatsHotReadModelStore

**Problem:** The processor pipeline does NOT wire candle/stats. Specifically:
1. `cmd/processor/bootstrap.go` uses struct literal `&aggapp.AggregationService{UpdateBook: ...}` instead of `NewAggregationService()`, leaving Candle and Stats nil
2. `internal/actors/aggregation/runtime/processor.go` only routes `marketdata.bookdelta` and `marketdata.trade` — no routing for candle build or stats build
3. No noop implementations of `CandleHotReadModelStore` or `StatsHotReadModelStore` exist

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error` in domain/app)

```go
problem.New(problem.ValidationFailed, "venue must not be empty")
problem.Newf(problem.Internal, "failed to marshal: %v", err)
```

### Use case: `Execute(ctx, req) (response, *problem.Problem)`

### Envelope-driven routing:
```go
// Processor routes envelopes by Type field:
switch env.Type {
case typeBookDelta:  // "marketdata.bookdelta" → UpdateOrderBookFromEvents
case typeTrade:      // "marketdata.trade"     → JoinCrossVenueTrades + BuildCandleFromEvents
case typeLiquidation: // "marketdata.liquidation" → BuildStatsFromEvents
case typeMarkPrice:   // "marketdata.markprice"   → BuildStatsFromEvents
}
```

### Import order: stdlib → external → monorepo

### Codec decode pattern (already used for bookdelta/trade):
```go
decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
if prob != nil {
    return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
}
trade, ok := decoded.(mddomain.TradeTickV1)
```

### Testing: fakes + t.Helper() + spy patterns

---

## Task: Wire M4 into Processor Pipeline

### Step 1: Add noop store stubs in `cmd/processor/bootstrap.go`

Add log-based noop stubs (matching existing `logArtifactPublisher` pattern):

```go
type logCandleHotStore struct{ logger *slog.Logger }

func (s *logCandleHotStore) SaveCandle(_ context.Context, evt aggdomain.CandleClosed) *problem.Problem {
    s.logger.Debug("aggregation: candle saved to hot store",
        "venue", evt.Candle.Venue,
        "instrument", evt.Candle.Instrument,
        "timeframe", evt.Candle.Timeframe,
        "trade_count", evt.Candle.TradeCount,
    )
    return nil
}

type logStatsHotStore struct{ logger *slog.Logger }

func (s *logStatsHotStore) SaveStats(_ context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
    s.logger.Debug("aggregation: stats saved to hot store",
        "venue", evt.Stats.Venue,
        "instrument", evt.Stats.Instrument,
        "timeframe", evt.Stats.Timeframe,
        "liq_count", evt.Stats.LiqCount,
    )
    return nil
}
```

### Step 2: Fix AggregationService wiring in `cmd/processor/bootstrap.go`

**Current (BROKEN — lines 148-152):**
```go
aggSvc := &aggapp.AggregationService{
    UpdateBook: aggapp.NewUpdateOrderBookFromEventsWithConfig(artifactPub, hotStore, aggapp.UpdateConfig{
        MaxBooks: cfg.Processor.MaxInstruments,
    }),
}
```

**Replace with:**
```go
aggSvc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
    Update:      aggapp.UpdateConfig{MaxBooks: cfg.Processor.MaxInstruments},
    Candle:      aggapp.BuildCandleConfig{MaxCandles: cfg.Processor.MaxInstruments},
    Stats:       aggapp.BuildStatsConfig{MaxWindows: cfg.Processor.MaxInstruments},
    Publisher:   artifactPub,
    Store:       hotStore,
    CandleStore: &logCandleHotStore{logger: logger},
    StatsStore:  &logStatsHotStore{logger: logger},
})
```

### Step 3: Extend processor routing in `internal/actors/aggregation/runtime/processor.go`

#### 3a. Add event type constants:

```go
const (
    typeBookDelta   = "marketdata.bookdelta"
    typeTrade       = "marketdata.trade"
    typeRaw         = "marketdata.raw"
    typeLiquidation = "marketdata.liquidation"  // NEW
    typeMarkPrice   = "marketdata.markprice"     // NEW
)
```

#### 3b. Extend `handleEnvelope` switch:

The `handleEnvelope` method currently handles `typeBookDelta`, `typeTrade`, `typeRaw`, and logs warn for unknown types. Add two new cases:

```go
case typeTrade:
    if env.Version != 1 {
        return unsupportedVersionProblem(env.Type, env.Version)
    }
    // Route to candle builder FIRST (always, if configured).
    if p.cfg.Service != nil && p.cfg.Service.Candle != nil {
        if prob := p.handleTradeForCandle(env); prob != nil {
            p.logger.Warn("aggruntime: BuildCandle failed",
                "venue", env.Venue,
                "instrument", env.Instrument,
                "seq", env.Seq,
                "code", prob.Code,
            )
            // Continue — candle failure should not block cross-venue join.
        }
    }
    // Route to cross-venue join (existing behavior).
    if p.cfg.JoinTrades == nil {
        return nil // No join configured, candle already processed.
    }
    return p.handleTrade(env)

case typeLiquidation:
    if env.Version != 1 {
        return unsupportedVersionProblem(env.Type, env.Version)
    }
    return p.handleLiquidation(env)

case typeMarkPrice:
    if env.Version != 1 {
        return unsupportedVersionProblem(env.Type, env.Version)
    }
    return p.handleMarkPrice(env)
```

**IMPORTANT:** The current `typeTrade` case returns a validation error if `JoinTrades == nil`. This MUST change — now trade events should ALSO be routed to `BuildCandleFromEvents` independently of whether `JoinTrades` is configured. Remove the early JoinTrades nil check. If `JoinTrades` is nil but candles are configured, trade processing should still succeed.

#### 3c. Add handler methods:

**`handleTradeForCandle`** — decode TradeTickV1, build `BuildCandleRequest`, call `Service.Candle.Execute()`:

```go
func (p *ProcessorSubsystemActor) handleTradeForCandle(env envelope.Envelope) *problem.Problem {
    if p.cfg.Service == nil || p.cfg.Service.Candle == nil {
        return nil
    }
    decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
    if prob != nil {
        return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
    }
    trade, ok := decoded.(mddomain.TradeTickV1)
    if !ok {
        return problem.WithDetail(
            problem.Newf(problem.ValidationFailed, "decoded trade payload type mismatch: got %T", decoded),
            "reason_code", reasonCodeValidationFailed,
        )
    }
    req := aggapp.BuildCandleRequest{
        Venue:      env.Venue,
        Instrument: env.Instrument,
        Price:      trade.Price,
        Quantity:    trade.Size,
        IsBuy:      strings.EqualFold(trade.Side, "buy"),
        Seq:        env.Seq,
        TsIngest:   env.TsIngest,
    }
    resp, prob := p.cfg.Service.Candle.Execute(context.Background(), req)
    if prob != nil {
        return prob
    }
    if len(resp.Closed) > 0 {
        p.logger.Debug("aggruntime: candles closed",
            "venue", env.Venue,
            "instrument", env.Instrument,
            "closed_count", len(resp.Closed),
            "active_candles", resp.ActiveCandles,
        )
    }
    return nil
}
```

**`handleLiquidation`** — decode LiquidationTickV1, build `BuildStatsRequest{Kind: StatsInputLiquidation}`:

```go
func (p *ProcessorSubsystemActor) handleLiquidation(env envelope.Envelope) *problem.Problem {
    if p.cfg.Service == nil || p.cfg.Service.Stats == nil {
        p.logger.Warn("aggruntime: no Stats use case configured — dropping liquidation")
        return nil
    }
    decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
    if prob != nil {
        return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
    }
    liq, ok := decoded.(mddomain.LiquidationTickV1)
    if !ok {
        return problem.WithDetail(
            problem.Newf(problem.ValidationFailed, "decoded liquidation type mismatch: got %T", decoded),
            "reason_code", reasonCodeValidationFailed,
        )
    }
    req := aggapp.BuildStatsRequest{
        Venue:           env.Venue,
        Instrument:      env.Instrument,
        Kind:            aggapp.StatsInputLiquidation,
        Seq:             env.Seq,
        TsIngest:        env.TsIngest,
        LiquidationSide: liq.Side,
        LiquidationQty:  liq.Size,
    }
    resp, prob := p.cfg.Service.Stats.Execute(context.Background(), req)
    if prob != nil {
        return prob
    }
    if len(resp.Closed) > 0 {
        p.logger.Debug("aggruntime: stats windows closed (liquidation)",
            "venue", env.Venue,
            "instrument", env.Instrument,
            "closed_count", len(resp.Closed),
        )
    }
    return nil
}
```

**`handleMarkPrice`** — decode MarkPriceTickV1, build TWO stats requests (markprice + fundingrate if non-zero):

```go
func (p *ProcessorSubsystemActor) handleMarkPrice(env envelope.Envelope) *problem.Problem {
    if p.cfg.Service == nil || p.cfg.Service.Stats == nil {
        p.logger.Warn("aggruntime: no Stats use case configured — dropping markprice")
        return nil
    }
    decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
    if prob != nil {
        return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
    }
    mp, ok := decoded.(mddomain.MarkPriceTickV1)
    if !ok {
        return problem.WithDetail(
            problem.Newf(problem.ValidationFailed, "decoded markprice type mismatch: got %T", decoded),
            "reason_code", reasonCodeValidationFailed,
        )
    }
    // Apply mark price.
    if mp.MarkPrice > 0 {
        req := aggapp.BuildStatsRequest{
            Venue:      env.Venue,
            Instrument: env.Instrument,
            Kind:       aggapp.StatsInputMarkPrice,
            Seq:        env.Seq,
            TsIngest:   env.TsIngest,
            MarkPrice:  mp.MarkPrice,
        }
        if _, prob := p.cfg.Service.Stats.Execute(context.Background(), req); prob != nil {
            return prob
        }
    }
    // Apply funding rate if present (non-zero).
    if mp.FundingRate != 0 {
        req := aggapp.BuildStatsRequest{
            Venue:       env.Venue,
            Instrument:  env.Instrument,
            Kind:        aggapp.StatsInputFundingRate,
            Seq:         env.Seq,
            TsIngest:    env.TsIngest,
            FundingRate: mp.FundingRate,
        }
        if _, prob := p.cfg.Service.Stats.Execute(context.Background(), req); prob != nil {
            return prob
        }
    }
    return nil
}
```

### Step 4: Update processor tests

**File:** `internal/actors/aggregation/runtime/processor_test.go`

The existing `spyArtifactPublisher` already has `PublishCandleClosed` and `PublishStatsClosed` stubs. Add integration tests:

```go
func TestProcessorSubsystem_TradeRoutesToCandle(t *testing.T) {
    // Send trade envelope → verify BuildCandleFromEvents receives it.
    // Verify candle opens on first trade, no closed events.
}

func TestProcessorSubsystem_TradeRoutesToBothCandleAndJoin(t *testing.T) {
    // Send trade envelope with both Candle and JoinTrades configured.
    // Verify BOTH paths are exercised.
}

func TestProcessorSubsystem_LiquidationRoutesToStats(t *testing.T) {
    // Send liquidation envelope → verify BuildStatsFromEvents receives it.
}

func TestProcessorSubsystem_MarkPriceRoutesToStats(t *testing.T) {
    // Send markprice envelope → verify BuildStatsFromEvents receives it.
    // Send markprice with FundingRate > 0 → verify both markprice AND funding inputs applied.
}

func TestProcessorSubsystem_TradeWithNilJoinTrades_StillRoutesToCandle(t *testing.T) {
    // JoinTrades = nil, Candle configured.
    // Send trade → candle should work, no error.
}

func TestProcessorSubsystem_WindowClose_EmitsCandleClosed(t *testing.T) {
    // Send trades in window T, then trade in window T+1.
    // Verify PublishCandleClosed was called on spyArtifactPublisher.
}

func TestProcessorSubsystem_WindowClose_EmitsStatsClosed(t *testing.T) {
    // Send liquidation in window T, then liquidation in window T+1.
    // Verify PublishStatsClosed was called.
}
```

### Step 5: Ensure `spyArtifactPublisher` tracks calls

In the test file, the spy should capture actual calls, not just stub:

```go
type spyArtifactPublisher struct {
    mu                 sync.Mutex
    snapshots          []aggdomain.SnapshotProduced
    inconsistents      []aggdomain.OrderBookInconsistentDetected
    candlesClosed      []aggdomain.CandleClosed       // NEW
    statsClosed        []aggdomain.StatsWindowClosed   // NEW
}

func (s *spyArtifactPublisher) PublishCandleClosed(_ context.Context, evt aggdomain.CandleClosed) *problem.Problem {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.candlesClosed = append(s.candlesClosed, evt)
    return nil
}

func (s *spyArtifactPublisher) PublishStatsClosed(_ context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.statsClosed = append(s.statsClosed, evt)
    return nil
}
```

---

## Available Domain Types for Decode

```go
// Codec registry (internal/shared/contracts/payload_registry.go):
// "marketdata.trade"       v1 → mddomain.TradeTickV1
// "marketdata.bookdelta"   v1 → mddomain.BookDeltaV1
// "marketdata.markprice"   v1 → mddomain.MarkPriceTickV1
// "marketdata.liquidation" v1 → mddomain.LiquidationTickV1

// TradeTickV1 fields: Price, Size, Side, TradeID, Timestamp
// LiquidationTickV1 fields: Side, Price, Size, Timestamp
// MarkPriceTickV1 fields: MarkPrice, IndexPrice, FundingRate, Timestamp
```

---

## Use Case Input Types

```go
// BuildCandleRequest: Venue, Instrument, Price, Quantity, IsBuy, Seq, TsIngest
// BuildStatsRequest: Venue, Instrument, Kind (liquidation|markprice|fundingrate), Seq, TsIngest
//   + LiquidationSide, LiquidationQty (when Kind=liquidation)
//   + MarkPrice (when Kind=markprice)
//   + FundingRate (when Kind=fundingrate)
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/actors/aggregation/runtime/processor.go` | Main file to modify (routing) |
| `internal/actors/aggregation/runtime/processor_test.go` | Tests to extend |
| `cmd/processor/bootstrap.go` | Composition root to fix |
| `internal/core/aggregation/app/service.go` | AggregationService facade |
| `internal/core/aggregation/app/build_candle.go` | Candle use case (reference) |
| `internal/core/aggregation/app/build_stats.go` | Stats use case (reference) |
| `internal/core/aggregation/ports/ports.go` | Port interfaces |
| `internal/core/marketdata/domain/payloads.go` | TradeTickV1, LiquidationTickV1, MarkPriceTickV1 |
| `internal/shared/contracts/payload_registry.go` | Codec registry |
| `internal/core/aggregation/domain/candle.go` | CandleV1 aggregate |
| `internal/core/aggregation/domain/stats.go` | StatsWindowV1 aggregate |

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

### STOP CONDITIONS (halt immediately if any occur):
- Cross-BC domain import (core → actors, core → adapters)
- Layering violation: actors importing interfaces
- Race condition detected under `-race`
- Candle/stats invariant violation (CA-1..CA-7, ST-1..ST-6)

### Commit message format (conventional commits, max 72 char title):
```
feat(m4): wire candle+stats pipeline into processor

- Fix bootstrap to use NewAggregationService() with all 3 use cases
- Add logCandleHotStore and logStatsHotStore noop stubs
- Extend processor routing: trade→candle, liquidation→stats, markprice→stats
- Add integration tests for full pipeline coverage

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **No time.Now() in domain/app** — inject `clock.Clock`
2. **No plain error** — always `*problem.Problem`
3. **No cross-BC imports** — processor (actors) may import core, NOT the reverse
4. **Trade routing change:** trade must route to BOTH candle (always if configured) and JoinTrades (only if configured). Candle failure should NOT block JoinTrades.
5. **MarkPrice dual-routing:** MarkPriceTickV1 contains BOTH MarkPrice and FundingRate — route as two separate stats inputs.
6. **Seq monotonicity:** Stats use case requires seq to be monotonic per window. When routing markprice+funding from same envelope, use the SAME seq for both.
7. **Bounded everything** — BoundedMap is already used in use cases; no unbounded maps in processor.
8. **Deterministic replay** — same input sequence must produce same output.
