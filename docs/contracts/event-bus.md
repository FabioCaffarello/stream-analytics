# Event Bus Contract

## Goal

Define a canonical structure for all messages flowing through the system.

This ensures:

- replayability
- compatibility
- deterministic processing
- auditability

---

## Envelope

All messages MUST follow:

```json
{
  "type": "marketdata.trade",
  "version": 1,
  "venue": "binance",
  "instrument": "BTC-PERP",
  "ts_exchange": 1710000000,
  "ts_ingest": 1710000005,
  "seq": 9283749823,
  "idempotency_key": "binance-BTC-123456",
  "payload": {}
}
```

---

## Field Definitions

### type

Stable event identifier.

Never rename.

---

### version

Increment when payload changes break decoding.

Consumers must support N-1 versions during migration.

---

### seq

Monotonic per `(venue, instrument)`.

Used for ordering.

Never trust exchange timestamps alone.

---

### idempotency_key

Guarantees deduplication.

Must be deterministic.

---

## Subject Naming Strategy

Pattern:

```text
<context>.<event>.<venue>.<instrument>
```

Example:

```text
marketdata.trade.binance.btc-perp
aggregation.orderbook.binance.btc-perp
insights.liquidity_shift.global.btc
```

Avoid wildcard-heavy patterns that destroy partitioning.

---

## Versioning Rules

Allowed:

- add optional fields
- expand payload

Forbidden:

- rename fields
- change semantics silently

---
