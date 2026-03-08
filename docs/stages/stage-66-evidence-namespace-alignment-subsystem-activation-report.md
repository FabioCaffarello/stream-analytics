# Stage 66 — Evidence Namespace Alignment + Subsystem Activation

**Date:** 2026-03-07
**Status:** COMPLETE
**Scope:** Backend — evidence domain semantic alignment

## Summary

Aligned the Evidence bounded context to use its own `evidence.*` namespace instead of
borrowing `insights.*`, eliminating semantic ambiguity between the two domains.
Backward compatibility is preserved via legacy aliases at all consumer boundaries.

## Problem

Evidence domain produced three event types, two of which used the wrong namespace:

| Event | Before | After |
|-------|--------|-------|
| Microstructure Evidence | `insights.microstructure_evidence` | `evidence.microstructure_evidence` |
| Regime Evidence | `insights.regime_evidence` | `evidence.regime_evidence` |
| Liquidity Evidence | `liquidity.evidence` | `liquidity.evidence` (unchanged) |

The `ownerBC` in delivery contracts was `"insights"` despite the producer, schema authority,
and all domain logic living in the evidence bounded context.

## Changes

### Domain Constants (source of truth)

| File | Change |
|------|--------|
| `internal/core/evidence/domain/evidence.go:17` | `MicrostructureEvidenceType` → `"evidence.microstructure_evidence"` |
| `internal/core/evidence/domain/regime.go:11` | `RegimeEvidenceType` → `"evidence.regime_evidence"` |

### Delivery Layer (contracts + routing)

| File | Change |
|------|--------|
| `internal/core/delivery/domain/envelope_policy.go` | New canonical `evidence.*` entries with `ownerBC: "evidence"`; legacy `insights.*` entries retained with updated ownerBC |
| `internal/core/delivery/domain/backpressure_policy.go` | Added `evidence.microstructure_evidence` priority entry |
| `internal/actors/delivery/runtime/router.go` | `allowEnvelopeTimeframeOverride` now matches `evidence.*` prefix |
| `internal/actors/delivery/runtime/session_protocol.go` | `channelEnumFromStreamType` accepts both `evidence.*` and legacy `insights.*` |
| `internal/actors/delivery/runtime/session_commands.go` | Added `insights.regime_evidence` to legacy cutover rejection |
| `internal/core/marketmodel/canonicalizer.go` | Reordered canonical entries first, legacy as compat aliases |

### Actor Consumers (backward compat)

| File | Change |
|------|--------|
| `internal/actors/signal/runtime/subsystem.go` | Added `"insights.regime_evidence"` and `"insights.microstructure_evidence"` as legacy compat aliases |
| `internal/actors/signals/runtime/subsystem.go` | Added `"insights.regime_evidence"` and `"insights.microstructure_evidence"` as legacy compat aliases |

### Infrastructure (JetStream + NATS)

| File | Change |
|------|--------|
| `cmd/processor/bootstrap.go` | JetStream filter: `insights.microstructure_evidence.v1.>` → `evidence.microstructure_evidence.v1.>` |
| `cmd/signals/bootstrap.go` | Added `evidence.>` filter subject alongside `insights.>` |

### Documentation

| File | Change |
|------|--------|
| `docs/contracts/subject-registry.yaml` | New `evidence.microstructure_evidence.v1` and `evidence.regime_evidence.v1` entries with `aliases` for legacy |

### Tests

| File | Change |
|------|--------|
| `session_channel_mapping_test.go` | Added canonical evidence channel passthrough test, legacy regime rejection test |
| `session_test.go` | Added legacy regime evidence rejection test case |

## Evidence Domain — Formalized Contracts

### Events Consumed

| Event Type | Source |
|-----------|--------|
| `marketdata.trade` | MarketData subsystem |
| `marketdata.bookdelta` | MarketData subsystem |
| `aggregation.candle` | Aggregation subsystem |
| `aggregation.tape` | Aggregation subsystem (LEL input) |
| `aggregation.snapshot` | Aggregation subsystem (LEL input) |

### Events Produced

| Event Type | Version | Description |
|-----------|---------|-------------|
| `evidence.microstructure_evidence` | v1 | 5 rule types: spread_explosion, liquidity_thinning, persistent_imbalance, absorption, sweep |
| `evidence.regime_evidence` | v1 | 5 regime kinds: trending, ranging, breakout, high_volatility, low_volatility |
| `liquidity.evidence` | v1 | LEL v1: BOOK_IMBALANCE, ABSORPTION, SWEEP, THINNING, SPREAD_REGIME |

### Pipeline Position

```
marketdata → aggregation → evidence → signal
```

Evidence is a **pure observation layer**:
- Deterministic: same inputs → same outputs
- No strategy logic (ADR-0008)
- No composition logic (that's signal's job)
- No buy/sell directives

### Downstream Consumers

| Consumer | Events Consumed | Purpose |
|----------|----------------|---------|
| Signal subsystem | `evidence.microstructure_evidence`, `evidence.regime_evidence` | Compose signals via correlation + regime boosting |
| Delivery router | `evidence.microstructure_evidence`, `evidence.regime_evidence`, `liquidity.evidence` | Route to WS sessions |

## Backward Compatibility

Legacy `insights.*` event types are accepted at all consumer boundaries:
- Signal subsystem switch cases include legacy aliases
- Delivery router validates both namespaces
- Session protocol maps both to `CHANNEL_EVIDENCE`
- JetStream signals bootstrap subscribes to both `insights.>` and `evidence.>`

Legacy `insights.microstructure_evidence` and `insights.regime_evidence` subjects are
**rejected** at the WS subscribe command layer to guide client migration.

## Verification

All affected modules pass:
- `internal/core/evidence` — OK (domain + app)
- `internal/core/delivery` — OK (domain + app)
- `internal/actors/delivery` — OK (router + session + tests)
- `internal/actors/signal` — OK
- `internal/actors/signals` — OK
- `internal/actors/evidence` — OK
- `internal/shared` — OK (contracts + codec)
- `internal/core/marketmodel` — OK

Zero regressions. Zero wire-format changes. Zero behavioral changes.

## Files Modified

| File | Lines |
|------|-------|
| `internal/core/evidence/domain/evidence.go` | 1 |
| `internal/core/evidence/domain/regime.go` | 1 |
| `internal/core/delivery/domain/envelope_policy.go` | +4 |
| `internal/core/delivery/domain/backpressure_policy.go` | +1 |
| `internal/actors/delivery/runtime/router.go` | +1 |
| `internal/actors/delivery/runtime/session_protocol.go` | +2 |
| `internal/actors/delivery/runtime/session_commands.go` | +5 |
| `internal/core/marketmodel/canonicalizer.go` | ~2 |
| `internal/actors/signal/runtime/subsystem.go` | ~2 |
| `internal/actors/signals/runtime/subsystem.go` | ~2 |
| `cmd/processor/bootstrap.go` | ~1 |
| `cmd/signals/bootstrap.go` | +1 |
| `docs/contracts/subject-registry.yaml` | +12 |
| `session_channel_mapping_test.go` | +13 |
| `session_test.go` | +4 |

**15 files modified**, ~+50 lines, zero regressions.
