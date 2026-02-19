# Codex Prompt C — Tests, Hardening, Delivery Routes

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

---

## Context

After Prompts A and B:
- Processor routes trade→candle, liquidation→stats, markprice→stats
- Candle/Stats registered in codec, delivery contracts, subject registry
- Config schema has candle/stats fields

Remaining gaps:
1. No golden replay tests proving candle/stats determinism
2. No soak tests for cardinality under high load
3. Delivery WS router does not route candle/stats subjects to subscribers
4. No E2E test proving trade→candle→WS delivery pipeline

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error`)
### Testing: fakes + t.Helper() + spy patterns
### Golden replay: deterministic `(ts_ingest, seq)` ordering, byte-identical output
### Soak tests: `testing.Short()` guard, bounded resource usage assertions

---

## Task: Tests + Hardening + Delivery Wiring

### Phase 1: Golden Replay Tests for Candle Determinism

**File:** `internal/core/aggregation/app/build_candle_golden_test.go` (NEW)

```go
func TestBuildCandle_GoldenDeterminism(t *testing.T) {
    // 1. Construct BuildCandleFromEvents with FakeClock.
    // 2. Feed N trades spanning multiple 1m windows (use fixed timestamps).
    // 3. Collect all CandleClosed events in order.
    // 4. Run the exact same sequence a second time on a fresh use case.
    // 5. Assert both runs produce identical CandleClosed sequences (deep equal).
    // 6. Assert candle fields match expected OHLCV values.
}

func TestBuildCandle_GoldenCascade_5mFrom1m(t *testing.T) {
    // 1. Feed trades spanning a full 5m window (5 × 1m closes).
    // 2. Verify exactly 5 × 1m CandleClosed + 1 × 5m CandleClosed emitted.
    // 3. Verify 5m candle OHLCV matches aggregate of 5 × 1m candles.
    // 4. Run same sequence on fresh use case, assert byte-identical output.
}
```

**Key assertions:**
- `candleClosed[i].Candle.Volume == candleClosed[i].Candle.BuyVolume + candleClosed[i].Candle.SellVolume` (CA-6)
- `candleClosed[i].Candle.High >= candleClosed[i].Candle.Low` (CA-5)
- `candleClosed[i].Candle.High >= max(candleClosed[i].Candle.Open, candleClosed[i].Candle.ClosePrice)` (CA-5)
- Two runs produce identical slices (CA-1 determinism, CA-4 replay stable)

### Phase 2: Golden Replay Tests for Stats Determinism

**File:** `internal/core/aggregation/app/build_stats_golden_test.go` (NEW)

```go
func TestBuildStats_GoldenDeterminism_Liquidation(t *testing.T) {
    // Feed N liquidations spanning multiple 1m windows.
    // Verify StatsWindowClosed events are deterministic across two runs.
}

func TestBuildStats_GoldenDeterminism_MixedInputs(t *testing.T) {
    // Feed interleaved liquidation + markprice + funding events.
    // Verify partial windows are tolerated (ST-6).
    // Verify deterministic output.
}

func TestBuildStats_GoldenPartialInputs(t *testing.T) {
    // Window with only markprice input (no liquidation, no funding).
    // Verify LiqCount == 0, LiqVolumes == 0, MarkPrice OHLC populated, FundingRate == 0.
    // This proves ST-6 (partial inputs OK).
}
```

### Phase 3: Soak Tests for Cardinality Under Load

**File:** `internal/core/aggregation/app/build_candle_soak_test.go` (NEW)

```go
func TestBuildCandle_Soak_HighCardinality(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test in short mode")
    }
    // 1. Create use case with MaxCandles=1000.
    // 2. Feed trades for 2000 unique instruments.
    // 3. Verify uc.ActiveCandles() never exceeds MaxCandles.
    // 4. Verify no panic, no memory leak (runtime.MemStats before/after).
}

func TestBuildCandle_Soak_RapidWindowRolls(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test in short mode")
    }
    // 1. Feed 100k trades with rapidly incrementing timestamps.
    // 2. Verify all windows close properly, no orphaned state.
    // 3. Verify ActiveCandles() stays bounded.
}
```

**File:** `internal/core/aggregation/app/build_stats_soak_test.go` (NEW)

```go
func TestBuildStats_Soak_HighCardinality(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test in short mode")
    }
    // Same pattern as candle soak with MaxWindows=1000 and 2000 instruments.
}
```

### Phase 4: Delivery Router Extension

**Context:** The delivery subsystem routes published envelopes to WS subscribers. It needs to know about candle/stats subjects.

**File:** `internal/actors/delivery/runtime/router.go` (or wherever the subject→subscriber routing lives)

Check the current router implementation to understand how it matches subjects to subscriptions. The delivery contracts were already extended in Prompt B, so the router should already accept candle/stats envelopes if it uses the contracts map. Verify this works.

If there's explicit routing logic, extend it:

```go
// These subjects should be routable to subscribers:
// "aggregation.candle.v1.{venue}.{instrument}"
// "aggregation.stats.v1.{venue}.{instrument}"
```

### Phase 5: E2E WS Contract Test

**File:** `internal/interfaces/ws/candle_stats_delivery_contract_test.go` (NEW)

```go
func TestWSDelivery_CandleClosed_RoutedToSubscriber(t *testing.T) {
    // 1. Set up WS server with test client subscribed to "aggregation.candle.v1.BINANCE.BTCPERP".
    // 2. Publish a CandleClosed envelope on the bus.
    // 3. Verify client receives the candle payload.
    // 4. Verify envelope fields (type, version, venue, instrument) match.
}

func TestWSDelivery_StatsClosed_RoutedToSubscriber(t *testing.T) {
    // Same pattern for stats.
}

func TestWSDelivery_CandleClosed_WildcardSubscription(t *testing.T) {
    // Subscribe to "aggregation.candle.v1.>" (all venues/instruments).
    // Publish candle for BINANCE.BTCPERP → verify received.
    // Publish candle for BYBIT.ETHPERP → verify received.
}
```

### Phase 6: Processor E2E Pipeline Test

**File:** `internal/actors/aggregation/runtime/processor_e2e_test.go` (NEW)

```go
func TestProcessorE2E_TradeToCandle_WindowClose(t *testing.T) {
    // 1. Wire full processor with real AggregationService (all 3 use cases).
    // 2. Send trade envelopes spanning two 1m windows via InMemoryBus.
    // 3. Wait for processing.
    // 4. Verify spyArtifactPublisher.candlesClosed has 1 entry.
    // 5. Verify candle fields match expected OHLCV.
}

func TestProcessorE2E_LiquidationToStats_WindowClose(t *testing.T) {
    // Same pattern for liquidation → stats.
}

func TestProcessorE2E_MarkPriceWithFunding_DualRouting(t *testing.T) {
    // Send markprice envelope with FundingRate != 0.
    // Verify stats window has both markprice AND funding data.
}
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/core/aggregation/app/build_candle.go` | Candle use case (reference) |
| `internal/core/aggregation/app/build_candle_test.go` | Existing unit tests (reference) |
| `internal/core/aggregation/app/build_stats.go` | Stats use case (reference) |
| `internal/core/aggregation/app/build_stats_test.go` | Existing unit tests (reference) |
| `internal/actors/aggregation/runtime/processor.go` | Processor (after Prompt A) |
| `internal/actors/aggregation/runtime/processor_test.go` | Processor tests (after Prompt A) |
| `internal/interfaces/ws/orderbook_delivery_contract_test.go` | Pattern: E2E WS test |
| `internal/interfaces/ws/delivery_contract_e2e_test.go` | Pattern: E2E WS test |
| `internal/shared/clock/fake.go` | FakeClock for deterministic tests |
| `internal/shared/ds/bounded_map.go` | BoundedMap (used by candle/stats) |

---

## Execution Rules

### Before EVERY commit:
```bash
make docs-check
make invariants-check
```

### Before commits touching runtime code:
```bash
make test-workspace
make test-workspace-race
```

### Soak tests run with:
```bash
go test -v -count=1 -run Soak ./internal/core/aggregation/app/
```

### STOP CONDITIONS:
- Determinism break (two runs of same input produce different output)
- BoundedMap cardinality exceeds configured cap
- Race condition detected under `-race`
- WS delivery test receives wrong envelope type or payload

### Commit sequence:
```
test(m4): add candle+stats golden determinism tests
test(m4): add candle+stats soak tests for bounded cardinality
feat(m4): extend delivery routing for candle+stats subjects
test(m4): add E2E processor and WS delivery contract tests
```

Each commit passes `make test-workspace` before proceeding.

---

## Important Constraints

1. **Golden tests must be deterministic** — use `clock.FakeClock`, fixed timestamps, sequential seq values
2. **Soak tests behind `testing.Short()`** — must not slow CI in short mode
3. **E2E tests use InMemoryBus** — no real NATS dependency
4. **No floating-point comparison** — use fixed-point integer assertions or tolerance helpers
5. **Bounded everything** — verify `ActiveCandles() <= MaxCandles` and `ActiveWindows() <= MaxWindows`
6. **Replay stability** — same input twice → same output (CA-4, ST-4)
7. **WS tests should match existing patterns** — see `orderbook_delivery_contract_test.go` and `delivery_contract_e2e_test.go` for test setup helpers
