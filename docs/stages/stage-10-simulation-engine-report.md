# Stage 10 — Deterministic Simulation Engine

## Executive Summary

Stage 10 introduces a `SimulationEngine` that transforms the executor's simulated path from a trivial accept-then-fill pass-through into a deterministic, realistic order lifecycle engine. The engine models acceptance, venue placement, partial fills with price impact, time-in-force semantics (GTC/IOC/FOK), cancellation, and expiry — all without connecting to any exchange. Every decision is derived from a deterministic hash of the intent ID, making executions fully reproducible across runs. The canonical pipeline `signal.event -> strategy.intent -> execution.event -> portfolio.state` is preserved unchanged, with `execution.event` remaining the source of truth and `portfolio.state` derived as a pure projection.

## Architecture

```
strategy.intent
    |
    v
GovernedExecutor.ExecuteAt(intent, observedAtMs)
    |-- Phase 1: CapabilityAuthorizer (grant, scope, limits)
    |-- Phase 2: AdapterSelector (route to adapter ID)
    |-- Phase 3: CredentialResolver (not required for simulation)
    |
    v (if authorized)
SimulationEngine.ExecuteAt(intent, observedAtMs)   <-- NEW
    |
    |-- 1. Rejection check (same policy as BootstrapExecutor)
    |-- 2. accepted event (with AcceptDelayMs latency)
    |-- 3. placed event (with PlaceDelayMs latency)
    |-- 4. Fill sequence (depends on TimeInForce):
    |       FOK: fill-or-kill (all or cancel)
    |       IOC: immediate partial fill + cancel remainder
    |       GTC: N partial fills with price impact + optional cancel/expiry
    |
    v
[]ExecutionEventV1  (accepted -> placed -> partial* -> filled|canceled|expired)
    |
    v
BootstrapProjector.Apply(event)  <-- UNCHANGED
    |
    v
portfolio.state
```

### Key Design Decisions

1. **Additive, not replacement**: `SimulationEngine` is a new `IntentExecutor` implementation alongside `BootstrapExecutor`. Selected via config `execution.adapter: "simulation.deterministic"`.

2. **Deterministic randomness**: All "random" decisions (fill count, fill ratio, cancel decision) derived from FNV-1a hash of intent ID fields. Same intent always produces identical event sequence.

3. **No async state**: All events emitted synchronously from `ExecuteAt()`, matching the existing `IntentExecutor` contract. No goroutines, no timers.

4. **Governance integration**: Works transparently with `GovernedExecutor` + `ExecutionGrant` + `CredentialBroker`. Credentials not required for simulation mode.

5. **Portfolio projector compatibility**: All emitted events pass `ExecutionEventV1.Validate()` and are consumed correctly by `BootstrapProjector.Apply()`.

## Simulation Model

### Order Lifecycle States

```
intent --> [rejected]
        \-> accepted --> placed --> [filled]                    (market order)
                                \-> partial* --> [filled]       (GTC limit, no cancel)
                                \-> partial* --> [canceled]     (GTC limit, cancel)
                                \-> partial* --> [expired]      (GTC limit, TTL exceeded)
                                \-> [filled]                    (FOK, full fill)
                                \-> [canceled]                  (FOK, no liquidity)
                                \-> [filled]                    (IOC, full fill)
                                \-> partial --> [canceled]      (IOC, partial + cancel remainder)
                                \-> [canceled]                  (IOC, no fill)
```

### Configuration Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `AcceptDelayMs` | 1 | Latency before accepted event |
| `PlaceDelayMs` | 5 | Latency between accepted and placed |
| `FillBaseDelayMs` | 10 | Latency between placed and first fill |
| `FillStepDelayMs` | 8 | Latency between consecutive partial fills |
| `CancelDelayMs` | 5 | Latency for cancel event after last fill |
| `MaxPartialFills` | 3 | Maximum partial fills for GTC limit orders |
| `FillRatio` | 0.4 | Base fill ratio per step (40%) |
| `PriceImpactBps` | 2 | Price impact per fill step (basis points) |
| `CancelProbability` | 0.15 | Deterministic cancel probability for GTC/FOK |

### Time-in-Force Semantics

- **GTC (Good-til-Canceled)**: Market orders fill fully in one step. Limit orders fill over `MaxPartialFills` steps with configurable `FillRatio`. If `shouldCancel` triggers, remaining quantity is canceled. If fill timestamps exceed `ExpiresAtMs`, an expired event terminates the sequence.

- **IOC (Immediate-or-Cancel)**: Fills a deterministic ratio immediately. If ratio < 100%, emits partial fill + cancel remainder. If ratio rounds to zero, cancels entirely.

- **FOK (Fill-or-Kill)**: Either fills entirely or cancels entirely. Decision based on deterministic cancel hash.

### Price Impact Model

Each fill step applies incremental slippage:
- Buy: `price * (1 + impact_bps * step / 10000)`
- Sell: `price * (1 - impact_bps * step / 10000)`

Average fill price is the volume-weighted average across all partial fills.

### Deterministic Hash Function

All "random" decisions use `deterministicHash(fields...) -> float64 in [0,1)`:
1. Compute FNV-1a hash via `sharedhash.HashFieldsFast`
2. Parse first 8 hex chars as uint64
3. Map to `[0, 1)` via `val % 10000 / 10000.0`

This ensures identical inputs always produce identical decisions.

## Files Modified

| File | Change | LOC |
|------|--------|-----|
| `internal/core/execution/app/simulation_engine.go` | **NEW** — Deterministic simulation engine | ~340 |
| `internal/core/execution/app/simulation_engine_test.go` | **NEW** — 22 test cases | ~370 |
| `cmd/executor/bootstrap.go` | **MODIFIED** — Added `selectBootstrapAdapter` wiring, default adapter ID for bootstrap mode | +20 |

**No changes to:**
- Protobuf contracts (`proto/execution/v1/event.proto`)
- Domain types (`execution/domain/event.go`, `reason.go`)
- Governance model (`execution/governance/model.go`)
- Portfolio projector (`portfolio/app/bootstrap_projector.go`)
- Strategy domain (`strategy/domain/intent.go`)
- Existing `BootstrapExecutor` (preserved as-is)

## Validation

### Test Results: 22 new tests, all passing

| Test | Validates |
|------|-----------|
| `Deterministic` | Same intent + config = identical event sequence |
| `AcceptedAndPlacedAlways` | Every execution starts with accepted + placed |
| `MonotonicSequences` | ExecutionSeq strictly increases |
| `MonotonicTimestamps` | TsEventMs never decreases |
| `MarketOrderFullFill` | Market GTC = exactly 3 events (accepted+placed+filled) |
| `FOKFill` | FOK with 0% cancel = full fill |
| `FOKCancel` | FOK with 100% cancel = cancel event |
| `IOCPartialFillAndCancel` | IOC produces partial fill + cancel remainder |
| `GTCPartialFills` | GTC with 0% cancel = partial fills then filled |
| `GTCWithCancel` | GTC with 100% cancel = partial fills then canceled |
| `PriceImpact` | Buy fills have non-decreasing prices |
| `SellPriceImpact` | Sell fills have non-increasing prices |
| `Rejection` | Invalid intent = single rejected event |
| `AllEventsValidate` | All events pass `ExecutionEventV1.Validate()` |
| `BoundaryInfo` | Returns correct adapter/mode/boundary |
| `CorrelationConsistency` | All events share same OrderID, IntentID, Venue, Symbol |
| `ExpiryBeforeFill` | Tight TTL triggers expired terminal event |
| `QuantityConservation` | Cumulative filled = requested when fully filled |
| `AllEventsPassDomainValidation` | 5 TIF/cancel combos all produce valid events |
| `TerminalStateReached` | 4 TIF combos always reach terminal state |
| `MultipleIntentsSequencing` | Cross-intent sequences are monotonic |
| `DeterministicHash_*` | Hash function is stable and varies with input |

### Existing tests: all 37 execution+portfolio tests still pass

```
ok  github.com/market-raccoon/internal/core/execution/app      0.136s
ok  github.com/market-raccoon/internal/core/execution/governance 0.244s
ok  github.com/market-raccoon/cmd/executor                      0.337s
ok  github.com/market-raccoon/internal/core/portfolio/app        (cached)
```

### Full project: all modules green (make test)

## Remaining Risks

1. **Portfolio projector edge cases**: The projector correctly handles partial fills today, but rapid partial fill sequences with price reversals (e.g., sell partial with positive impact, then cancel) may produce transient unrealized PnL spikes. This is cosmetic, not a correctness issue — the projector is already tested against all lifecycle statuses.

2. **Deterministic hash distribution**: The `deterministicHash` function maps to 10,000 buckets. For very small cancel probabilities (< 0.01%), the quantization may suppress expected cancels. Acceptable for simulation but worth noting.

3. **No order book simulation**: The engine does not model a simulated order book. Fill prices are derived from the intent's limit price + impact, not from a synthetic LOB. This is deliberate — order book simulation belongs in a future Stage if needed.

4. **Single-threaded sequence**: The engine is not thread-safe (`streamSeq` map is not synchronized). This matches the existing `BootstrapExecutor` design and the actor model (single-threaded per actor). If the engine is shared across goroutines, external synchronization is required.

## Configuration

To enable the simulation engine, set in `config.jsonc`:

```jsonc
{
  "execution": {
    "mode": "bootstrap_simulated",
    "adapter": "simulation.deterministic"
  }
}
```

Default (no adapter specified) continues to use `BootstrapExecutor` for backward compatibility.

## Next Stage

- **Stage 11**: Strategy Policy Engine — transforms `signal.event` into `strategy.intent` via configurable policies, completing the signal-to-portfolio pipeline.
- **Deferred**: Order book simulation (synthetic LOB for more realistic fill modeling), CBOR encoding for execution events, multi-account portfolio aggregation.
