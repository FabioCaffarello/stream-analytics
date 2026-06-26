---
type: doc
status: Draft
last_updated: 2026-06-25
---

# SLO Definitions

**Status:** Draft
**Owner:** Platform
**Last updated:** 2026-06-25

---

## Service Level Objectives

| Service | Indicator | Target | Window |
|---------|-----------|--------|--------|
| Consumer | Message lag (p99) | < 50ms | 30 days |
| Processor | Throughput | ≥ 100K evt/sec | 30 days |
| Delivery | WebSocket latency (p95) | < 25ms | 30 days |
| Store | Write latency (p99) | < 100ms | 30 days |

## PromQL Expressions

```promql
# Consumer lag p99 (placeholder)
histogram_quantile(0.99, sum(rate(mr_consumer_lag_seconds_bucket[5m])) by (le))

# Processor throughput
sum(rate(mr_processor_events_total[1m]))
```

> This document is a stub. Full SLO definitions with burn-rate alerts to be added.
> See [Metrics Catalogue](../architecture/metrics-catalogue.md) for available metrics.
