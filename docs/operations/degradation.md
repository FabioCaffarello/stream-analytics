# Degradation Contract — Storage-Induced Backpressure

**Status:** Active
**Last updated:** 2026-02-19

This document describes the expected system behavior when the cold-path
storage layer (ClickHouse) degrades or becomes unavailable, how that
degradation propagates through the pipeline, and how to diagnose and
mitigate it.

## Pipeline Overview

```
JetStream (NATS)
  └─ consumer (cmd/consumer)   ← ingest from exchanges
       └─ processor (cmd/processor)  ← aggregation, order book
            ├─ hot-path (TimescaleDB)   ← latest snapshot
            └─ publishes aggregation.snapshot.v1 → JetStream
                 └─ store (cmd/store)   ← cold-path ClickHouse writer
                      └─ ACK only after commit
```

The critical invariant is **ack-on-commit**: the store binary ACKs a
JetStream message only after the ClickHouse write (or in-memory mock)
returns success.  A slow or failing storage layer therefore stalls ACKs,
which propagates back through JetStream as consumer lag and redelivery.

---

## Scenario 1 — ClickHouse Slow (Elevated Latency)

**Symptom:** `store_commit_latency_seconds` p95 rises above baseline
(typical baseline < 10 ms for in-memory mock; real ClickHouse < 50 ms).

**Propagation chain:**

1. `store_commit_latency_seconds` increases.
2. Each message holds its JetStream ACK longer → `bus_ack_latency_seconds`
   for the store consumer rises.
3. JetStream `MaxAckPending` (default 1024) is eventually reached.
4. Store consumer `Fetch()` blocks waiting for ack slots.
5. `bus_consumer_lag{bus_type="jetstream"}` on the store consumer grows.
6. If a message exceeds `AckWait` (default 30 s) without ACK, JetStream
   redelivers it → `bus_redelivered_total` increments.
7. Redelivered messages hit the idempotency guard (`SaveIdempotent`) →
   no duplicate writes, but wasted work.

**Impact:**
- Data pipeline delay increases proportionally to commit latency.
- No data loss as long as JetStream retention holds messages.
- Redelivery wastes resources but is safe (idempotent writes).

---

## Scenario 2 — ClickHouse Down (Connection Refused / Timeout)

**Symptom:** `store_commit_total{status="failed"}` starts increasing.

**Propagation chain:**

1. `store_commit_total{status="failed"}` increments on every write
   attempt.
2. The store handler returns a `*problem.Problem` with retryable
   classification → JetStream consumer NAKs the message.
3. JetStream redelivers the message after `AckWait`.
4. After `MaxDeliver` attempts (default 10), the message is TERM'd →
   `ingest_term_total` increments.
5. TERM'd messages are lost from the consumer's perspective (they remain
   in the stream but won't be redelivered to this consumer).
6. `bus_consumer_lag` rises rapidly as no messages are being ACK'd.

**Impact:**
- Data loss risk after `MaxDeliver` exhaustion (messages TERM'd).
- `slo:data_loss:cold_commit_errors_rate_5m` fires the
  `ColdPathCommitErrorsNonZero` alert.
- Recovery requires ClickHouse to come back online; TERM'd messages need
  manual replay from JetStream stream.

---

## Scenario 3 — ClickHouse Recovering (Intermittent Failures)

**Symptom:** Mix of `store_commit_total{status="ok"}` and
`{status="failed"}` with elevated `store_commit_latency_seconds`.

**Propagation chain:**

1. Successful commits ACK normally; failed commits NAK.
2. Redelivered messages may succeed on retry if ClickHouse stabilizes.
3. Idempotency guards prevent duplicates from successful retries.
4. Consumer lag oscillates — decreases during good periods, increases
   during bad ones.

**Impact:**
- Eventual consistency maintained if ClickHouse recovers before
  `MaxDeliver` exhaustion.
- Latency p95/p99 spikes but median may stay acceptable.

---

## Diagnosis via Prometheus / Grafana

### Query 1 — Storage Commit Latency (Detect Slow Storage)

```promql
histogram_quantile(0.95,
  sum by (le) (rate(store_commit_latency_seconds_bucket[5m]))
)
```

**Interpretation:** When p95 exceeds 100 ms (real ClickHouse) or 1 ms
(in-memory mock), storage is under stress.  Sustained p95 > 1 s triggers
the `StoreCommitLatencyHigh` alert.

### Query 2 — Storage Failures vs Consumer Lag (Prove Storage Is Bottleneck)

```promql
# Panel A: commit failure rate
sum(rate(store_commit_total{status="failed"}[5m]))

# Panel B: consumer lag (same time range)
bus_consumer_lag{bus_type="jetstream"}
```

**Interpretation:** If commit failures rise AND consumer lag rises in
lockstep, storage is the root cause.  If lag rises without commit
failures, the bottleneck is elsewhere (slow handler, network, etc.).

### Query 3 — Redelivery Ratio (Quantify Wasted Work)

```promql
sum(rate(bus_redelivered_total{bus_type="jetstream"}[5m]))
/
clamp_min(sum(rate(bus_consumed_total{bus_type="jetstream"}[5m])), 1e-9)
```

**Interpretation:** A ratio > 0.1 (10%) indicates significant
redelivery pressure.  Under normal operation this should be ~0.  High
redelivery + stable commit count = idempotency working correctly (no
duplicates, but wasted resources).

### Query 4 — End-to-End Pipeline Health (Composite View)

```promql
# Commit success rate
sum(rate(store_commit_total{status="ok"}[5m]))
/
clamp_min(sum(rate(store_commit_total[5m])), 1e-9)
```

**Interpretation:** Should be 1.0 under normal operation.  Values < 0.99
warrant investigation.  Values < 0.9 are incidents.

---

## Mitigation Actions

| Scenario | Action | Effect |
|----------|--------|--------|
| Slow storage | Increase `store.batch.max_rows` (e.g., 10→50) | Amortizes write overhead, reduces per-message latency |
| Slow storage | Increase `jetstream.ack_wait` (e.g., 30s→60s) | Gives more time before redelivery triggers |
| Storage down | Check ClickHouse health (`SELECT 1`) | Confirm root cause |
| Storage down | Restart ClickHouse / fix disk/network | Restore service |
| Storage down | Increase `jetstream.max_deliver` temporarily | Buys time before TERM |
| Recovering | Monitor `store_commit_total{status="ok"}` trend | Confirm recovery |
| Recovering | Check for TERM'd messages in JetStream stream | Plan replay if needed |
| Any | Check `store_quarantine_total` | Identify decode failures (separate from storage) |

## Alert Reference

| Alert | Condition | Severity | Meaning |
|-------|-----------|----------|---------|
| `StoreCommitLatencyHigh` | p95 commit latency > 1 s for 5 min | ticket | Storage is slow |
| `ColdPathCommitErrorsNonZero` | Any cold-path commit errors in 5 min window sustained 15 min | ticket | Storage writes failing |
| `DataLossDropRateNonZero` | Any drops/naks/terms in 5 min window sustained 15 min | ticket | Pipeline losing data |
| `SLODataLossBurnRateFast` | Burn rate > 14.4 on 5m+1h | page | Rapid data loss |

## Related Documentation

- [Local Development Guide](../../.context/docs/local-dev.md) — service URLs, health checks, debug checklist
- [Sharding Guide](sharding.md) — horizontal scaling of consumers
- [Store Dashboard](../../deploy/observability/grafana/dashboards/store.json) — Grafana cold-path panels
