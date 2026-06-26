---
type: doc
status: Active
last_updated: 2026-06-25
---

# Ingest Pipeline Runbook

**Scope:** Consumer → NATS JetStream → Processor throughput and lag alerts.

---

## Alert: `IngestLagHigh`

**Meaning:** Consumer-to-processor message lag exceeds 50 ms p99.

**Immediate check:**
```bash
make logs service=consumer | grep "lag\|dropped\|backpressure"
make logs service=processor | grep "lag\|slow\|overflow"
make ps
```

**Common causes and actions:**

| Cause | Signal | Action |
|-------|--------|--------|
| Exchange burst | Consumer CPU spike | Verify adapter back-pressure; scale consumer replicas |
| Processor overload | Processor queue depth rising | Check `processor_queue_depth` metric; reduce enabled pairs |
| NATS JetStream pressure | `nats_js_pending_msgs` climbing | Increase consumer replicas or reduce subject fanout |
| Network partition | Connection resets in consumer log | Allow reconnect; monitor gap-fill completion |

**Escalation:** If lag stays > 200 ms for > 5 min, page on-call.

---

## Alert: `ConsumerDropRate`

**Meaning:** Consumer is dropping messages due to queue overflow.

```bash
make logs service=consumer | grep "drop\|overflow"
```

Increase `CONSUMER_QUEUE_DEPTH` env var or scale horizontally.

---

## Useful Grafana panels

- `Ingest` dashboard → `evt/sec`, `lag p99`, `drop rate`
- `Overview` dashboard → NATS publish rate

---

## See also

- [SLO Definitions](../slo.md)
- [Consumer Stall Runbook](consumer-stall.md)
- [Bus Runbook](bus.md)
