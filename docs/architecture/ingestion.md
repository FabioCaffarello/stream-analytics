# Ingestion Architecture

## Objective

Transform heterogeneous exchange streams into deterministic domain events.

---

## Pipeline

```text
Websocket → Parser → Normalizer → Sequencer → Envelope → Bus
```

---

## Parser

Exchange-specific.

Lives in adapters.

Never leaks into domain.

---

## Normalizer

Maps raw payloads into canonical domain types.

Example:

TradeTick
BookDelta

---

## Sequencer

Assigns monotonic sequence numbers.

Guarantees ordering even when exchanges deliver out-of-order messages.

Critical for:

- orderbook correctness
- replay
- agent accuracy

---

## Backpressure Strategy

When overwhelmed:

Priority order:

1. preserve ordering
2. avoid memory explosion
3. degrade gracefully

Allowed strategies:

- bounded mailboxes
- batching
- snapshot fallback

---
