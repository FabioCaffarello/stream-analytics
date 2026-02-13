# ADR-0017 — Multi-Exchange Normalization Invariants

**Status:** Accepted
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 section E.3 (topology), ADR-0011, RFC-0010 (W9)

---

## Context

Currently only Binance is integrated. The canonical instrument format is `BTCUSDT` (no separator, per ADR-0011). Adding a second exchange (Bybit, OKX) requires:
- Consistent instrument normalization across exchanges
- Market type awareness (SPOT vs PERP vs COIN-M)
- Stream ID uniqueness when same pair exists on multiple exchanges/market types
- Adapter isolation (exchange-specific parsing stays in adapters)

PRD-0001 section ADR-0017 (proposed) defines these rules.

## Decision

### 1. Canonical Instrument Format

`BASE-QUOTE` format (e.g., `BTC-USDT`), always uppercase, derived by `naming.CanonicalInstrument()` which strips separators then applies `ParseCanonicalPair()` from `domain/instrument_identity.go`.

**Migration from ADR-0011:** ADR-0011 defined canonical as `BTCUSDT` (no separator). We retain `naming.CanonicalInstrument()` (strips separators) as the internal key for maps and dedup. The `BASE-QUOTE` format (with hyphen) is the *display* canonical used in `InstrumentIdentity.Canonical`. Both are deterministic and derived from the same input.

Map key: `naming.CanonicalInstrument(raw)` → `BTCUSDT` (no separator, for map lookups and dedup).
Display/API: `ParseCanonicalPair(raw)` → `BTC-USDT` (for human-readable contexts).

### 2. Market Type as Metadata

Market type (`SPOT`, `USD_M_FUTURES`, `COIN_M_FUTURES`) is part of stream identity but NOT part of the instrument key.

**Stream ID** = `venue:instrument:market_type`

Example:
```
BINANCE:BTCUSDT:SPOT
BINANCE:BTCUSDT:USD_M_FUTURES
BYBIT:BTCUSDT:SPOT
```

Two streams with same `venue:instrument` but different `market_type` are **different streams** with independent sequence numbers, dedup windows, and order books.

### 3. Instrument Catalog Port

Each exchange adapter provides an `InstrumentCatalog` implementation:

```go
// internal/core/marketdata/ports/ports.go
type InstrumentCatalog interface {
    Resolve(venueSymbol string) (domain.InstrumentMetadata, *problem.Problem)
    ListInstruments(marketType domain.MarketType) ([]domain.InstrumentMetadata, *problem.Problem)
}
```

- `Resolve()` maps exchange-specific symbol (e.g., Bybit `BTCUSDT`) to canonical metadata
- `ListInstruments()` returns all instruments for a market type (used for dynamic discovery)

Phase 1: hardcoded mapping tables per exchange. Phase 2: REST API discovery at startup.

### 4. Cross-Venue Normalization Rule

Two instruments from different exchanges are considered "the same asset pair" if and only if:
- `CanonicalInstrument(venue_symbol_A) == CanonicalInstrument(venue_symbol_B)`
- `market_type_A == market_type_B`

This enables cross-venue arbitrage queries: subscribe to `marketdata.trade.v1.*.BTCUSDT` to get BTC-USDT trades from all venues.

### 5. Exchange Adapter Contract

Every exchange adapter MUST:
1. Implement `ParseMessage(data []byte, recvAt time.Time) (IngestRequest, bool, *problem.Problem)` or `ParseMessageWithMeta` variant
2. Map exchange event types to canonical event types (`marketdata.trade`, `marketdata.bookdelta`)
3. Normalize venue symbol to canonical instrument via `naming.CanonicalInstrument()`
4. Set `Venue` to canonical venue name (`BINANCE`, `BYBIT`, etc.)
5. Derive idempotency key using exchange-specific unique identifiers (trade ID, update sequence ID)
6. Enrich metadata with `InstrumentMetadata` (canonical pair, base, quote, market type)

### 6. Multi-Exchange Guardian Topology

One `MarketDataSubsystem` per exchange:
```
Guardian
├── MarketDataSubsystem["binance"]  → SubsystemKey = "marketdata:binance"
├── MarketDataSubsystem["bybit"]    → SubsystemKey = "marketdata:bybit"
├── AggregationSubsystem
└── DeliverySubsystem
```

`Subsystem` type extends to support exchange-qualified keys. Guardian `Factories` map uses `SubsystemKey` (string) instead of `Subsystem` enum.

## Rationale

- Normalizing at the adapter boundary keeps core domain exchange-agnostic
- Market type in stream ID prevents conflation of spot and futures data
- Consistent canonical format enables cross-venue joins via wildcard subscriptions
- Per-exchange subsystems provide fault isolation (one exchange failing doesn't affect others)

## Alternatives Considered

1. **Single canonical format `BTC-USDT` everywhere (replace BTCUSDT):** Rejected — would require migrating all existing map keys, dedup caches, and test fixtures. Internal key stays separator-less.
2. **Market type as part of instrument key:** Rejected — increases key length and complicates wildcard matching. Market type is metadata, not identity.
3. **Single MarketDataSubsystem with multi-exchange manager:** Rejected — couples exchange lifecycles. One exchange reconnecting shouldn't trigger restart of another.
4. **Venue-specific domain VOs:** Rejected — violates domain purity. Domain sees canonical instruments, never exchange-specific symbols.

## Consequences

### Positive
- Adding a new exchange is an adapter-only change (no core modifications)
- Cross-venue queries work via NATS wildcard subscriptions
- Fault isolation per exchange prevents cascading failures

### Negative
- N MarketDataSubsystems increases actor count (but each is lightweight)
- Guardian needs to support dynamic subsystem keys (minor refactor)
- Hardcoded instrument catalogs need maintenance per exchange

### Invariants (testable)
- `MEX-1`: `naming.CanonicalInstrument("BTCUSDT") == naming.CanonicalInstrument("btcusdt")` (unit test, already passes)
- `MEX-2`: Same venue symbol on Binance and Bybit produces same canonical instrument (cross-adapter unit test)
- `MEX-3`: Two subsystems for different exchanges run independently (integration test: poison one, other continues)
- `MEX-4`: No code in `internal/core/` references exchange-specific types or constants (grep audit)
- `MEX-5`: Adapter `ParseMessage` always returns normalized venue in uppercase (unit test per adapter)

## Rollout Plan

1. Extend Guardian to support string-based SubsystemKey (RFC-0010/W9)
2. Create Bybit adapter stub with at least trade + bookdelta parsing (RFC-0010/W9)
3. Add multi-exchange config to `config.AppConfig` (RFC-0010/W9)
4. Wire two MarketDataSubsystems in `cmd/consumer` (RFC-0010/W9)
5. Validate cross-venue Subject routing in delivery (RFC-0010/W9)
6. Add MEX-4 grep check to CI (RFC-0010/W9)

## Amendment 2026-02-12 (W9-1)

- Implemented second exchange adapter using Bybit (`trade` + `bookdelta`).
- Implemented `consumer.exchanges[]` config model with deterministic normalization and legacy synthesis.
- Stream identity partitioning is now enforced in runtime as `venue:instrument:market_type`.
- `SubjectFromEnvelope` format remains unchanged; market type is identity metadata, not subject dimension.
- Scope note: `InstrumentCatalog` phase remains deferred for follow-up; W9-1 used parser-level normalization with existing canonical naming.

## Amendment 2026-02-13 (Acceptance Changelog)

Acceptance evidence:
- Bybit parser coverage: `internal/adapters/exchange/bybit/parser_test.go` (`file:test TestParseMessage_Trade`, `file:test TestParseMessage_BookDelta`).
- Multi-exchange runtime wiring: `cmd/consumer/e2e_consumer_integration_test.go` (`file:test TestE2EConsumerMultiExchange`).
- Dynamic guardian subsystem keys: `internal/actors/runtime/guardian_test.go` (`file:test TestGuardian_StartOrder_DynamicMarketDataKeys`).
- Cross-exchange canonical normalization invariant: `internal/shared/naming/naming_test.go` (`file:test TestCanonicalInstrument`, `file:test TestCanonicalInstrument_idempotent`).
