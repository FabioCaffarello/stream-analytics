# ADR-0013 — Backpressure & Overload Policies

**Status:** Proposed
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 sections E.2, A.5, RFC-0006 (W5)

---

## Context

The ingest pipeline has multiple bounded stages:

```
WS recv → wsQueue (1024) → ingestWorker → IngestMarketData → EventPublisher → Bus → subscriber channels (1024)
```

Currently:
- `wsQueue` has two drop policies (`drop_oldest`, `drop_depth_keep_trades`) — good.
- `InMemoryBus` drops silently with no counter when a subscriber channel is full — bad.
- There is no priority system between event types beyond the `drop_depth_keep_trades` heuristic.
- There are no metrics on drops at any stage except `backpressureDropsTotal` in telemetry (log-only).

PRD-0001 section A.5 flagged "InMemoryBus drop silencioso" and "Bus drop behavior nao documentado" as risks.

## Decision

### 1. Every Bounded Stage Has Documented Drop Policy

| Stage | Buffer | Capacity | Policy | Metric |
|-------|--------|----------|--------|--------|
| WS → SubsystemActor | `wsQueue` | configurable (default 1024) | `drop_depth_keep_trades` (default) | `backpressure_drops_total{venue,policy}` |
| Bus → Subscriber | `chan envelope.Envelope` | configurable (default 1024) | drop-newest (non-blocking send) | `bus_drops_total{subscriber_id}` |
| JetStream publish (future) | async publish buffer | configurable | block-with-timeout + circuit breaker | `jetstream_publish_errors_total` |

### 2. Every Drop Increments a Counter

No silent drops. Every dropped message MUST increment a Prometheus counter. The counter label includes enough context to diagnose (venue, subscriber_id, policy).

### 3. Event Priority

When the system must shed load, priority determines what survives:

| Priority | Event Type | Rationale |
|----------|-----------|-----------|
| 1 (highest) | `marketdata.trade` | Trades are irreversible market events |
| 2 | `marketdata.bookdelta` | Depth can be reconstructed from snapshot |
| 3 | `marketdata.markprice` | Periodic, not critical |
| 4 (lowest) | `marketdata.liquidation` | Informational |

The `drop_depth_keep_trades` policy in `wsQueue` already implements priority 1 > 2. This ADR formalizes the full priority order for future use.

### 4. Backpressure Signals

When queue depth exceeds 80% capacity:
- Log WARN with queue depth and venue
- Set `backpressure_active{venue}` gauge to 1

When queue depth drops below 50% capacity:
- Set `backpressure_active{venue}` gauge to 0

### 5. InMemoryBus Must Count Drops

`InMemoryBus.Publish()` currently uses `select { case ch <- env: default: }`. The `default` branch MUST increment a counter per subscriber. Implementation:

```go
default:
    atomic.AddUint64(&b.dropCounters[i], 1)
```

Exposed via `bus_drops_total{subscriber_id}` metric.

### 6. No Unbounded Queues

Rule: every channel or queue in the data path MUST have explicit capacity. Unbounded channels (`make(chan T)`) are prohibited in data paths. Actor mailboxes (Hollywood-managed) are the only exception.

## Rationale

Silent data loss is the worst failure mode for a market data platform. Making drops visible via metrics enables:
- Alerting on data quality degradation
- Capacity planning (increase buffer, add partitions, upgrade hardware)
- Root cause analysis (which subscriber is slow? which venue is bursty?)

## Alternatives Considered

1. **Block on full (apply backpressure upstream):** Rejected — blocking the WS read loop would cause the exchange to disconnect us. Non-blocking drops are necessary at the ingest edge.
2. **Unlimited buffers:** Rejected — violates bounded memory invariant (ADR-0012 INV-3).
3. **Dead-letter queue for drops:** Deferred — adds complexity. Counter + alert is sufficient for now. Can add DLQ in future RFC.

## Consequences

### Positive
- Every drop is measurable and alertable
- Operators can tune buffer sizes based on observed drop rates
- Priority system ensures most valuable data survives overload

### Negative
- Per-subscriber drop counters add minor memory overhead (one uint64 per subscriber)
- Priority ordering in wsQueue requires scanning queue on drop (O(n) where n = queue capacity)

### Invariants (testable)
- `BP-1`: `bus_drops_total` increments when subscriber channel is full (integration test)
- `BP-2`: `backpressure_drops_total` increments when wsQueue is full (unit test — already exists)
- `BP-3`: No channel in data path has capacity 0 (code review / grep audit)
- `BP-4`: `backpressure_active` gauge transitions to 1 when depth > 80% (unit test)

## Rollout Plan

1. Add drop counter to `InMemoryBus` (RFC-0005/W4 — observability)
2. Add `backpressure_active` gauge to SubsystemActor (RFC-0005/W4)
3. Formalize priority enum in `internal/actors/marketdata/runtime/` (RFC-0006/W5)
4. Add 80%/50% threshold logging to wsQueue (RFC-0006/W5)
5. Wire all counters to Prometheus registry (RFC-0005/W4)

## Amendment (W11): Current Implicit Coverage

Without changing this ADR status (`Proposed`), W11 established partial implicit coverage that already reduces overload risk:

- Cardinality protection today:
  - `max_instruments` configuration limits active instrument cardinality at runtime.
  - Bounded maps in ingest/aggregation (`BoundedMap` with eviction) cap state growth and prevent unbounded map expansion under symbol churn.
- JetStream operational signals today:
  - `MaxAckPending` is configured and validated, providing a hard cap on outstanding unacked deliveries.
  - Consumer lag is surfaced as an operational signal (`bus_consumer_lag`) and can be used to detect overload before sustained loss.
- Remaining future work (explicit overload policies):
  - Define explicit per-reason overload actions (for example: decode failure, slow consumer, publish timeout, redelivery storm), not only generic drop behavior.
  - Bind each overload reason to deterministic policy (`drop`, `nak`, `term`, `quarantine`, `rate-limit`) with documented precedence.
  - Add dedicated tests/alerts proving policy transitions by overload reason, beyond current implicit boundedness and lag visibility.
