# Shard Incident Runbook

Operational runbook for diagnosing and resolving shard-related incidents in
Market Raccoon processor deployments.

> **Prerequisites:** Familiarity with [sharding overview](sharding.md) and
> access to Prometheus/Grafana dashboards.

---

## Alerts Reference

| Alert                       | Severity | Condition                                      | SLO / Window |
|-----------------------------|----------|-------------------------------------------------|-------------|
| `ShardHotSkew`              | warning  | One shard has 3x median throughput              | 5 min       |
| `ShardLagBudgetExceeded`    | critical | Shard lag exceeds configured `max_lag` budget   | 3 min       |
| `ShardConsumerHighLag`      | warning  | Shard consumer lag > 10 000 messages            | 5 min       |

Alert rules are defined in [`deploy/alerts/shard-alerts.yaml`](../../deploy/alerts/shard-alerts.yaml).

---

## Scenario: Hot Shard (Skew)

**Alert:** `ShardHotSkew`

### Symptoms

- One shard group processes significantly more events/sec than others.
- `rate(jetstream_shard_events_total[5m])` for one `group_id` is 3x+ the
  median across all groups.
- Lag may be climbing on the hot shard while others are idle.

### Root Causes

1. **Uneven key distribution** — a small number of high-volume instruments
   (e.g. BTCUSDT on multiple venues) hash to the same shard.
2. **Burst on specific instruments** — a market event causes a temporary spike
   on instruments that happen to share a shard.
3. **Wrong shard count** — too few shards for the instrument set, increasing
   collision probability.

### Resolution

1. **Confirm the skew** — query per-shard throughput:
   ```promql
   sort_desc(rate(jetstream_shard_events_total[5m]))
   ```
2. **Identify hot instruments** — check application logs for the hot shard's
   `group_id` to see which `venue.instrument` pairs dominate.
3. **Short-term:** If the hot shard is keeping up (lag stable), monitor and
   wait — the skew may be transient.
4. **Medium-term:** Increase `shard.count` to spread load across more groups.
   This requires redeploying **all** shard replicas simultaneously (see
   [Manual Rebalance](#scenario-manual-rebalance)).
5. **Verify resolution:**
   ```promql
   (
     rate(jetstream_shard_events_total[5m])
     / ignoring(group_id) group_left()
     quantile without(group_id) (0.5, rate(jetstream_shard_events_total[5m]))
   ) < 3
   ```

---

## Scenario: Lag Budget Exceeded

**Alert:** `ShardLagBudgetExceeded`

### Symptoms

- `jetstream_shard_consumer_lag{group_id="X"}` exceeds the configured
  `jetstream_shard_lag_budget{group_id="X"}`.
- Application logs show `"shard lag budget exceeded"` warnings with the
  affected group ID, current lag, and budget.

### Root Causes

1. **Consumer too slow** — processing latency is higher than message arrival
   rate for this shard.
2. **Downstream bottleneck** — the aggregation or storage layer is
   backpressuring the consumer.
3. **Resource starvation** — CPU/memory limits on the pod are constraining
   throughput.

### Resolution

1. **Check processing latency:**
   ```promql
   histogram_quantile(0.99, rate(jetstream_shard_ack_latency_seconds_bucket[5m]))
   ```
2. **Check for redeliveries** (symptom of slow processing or crashes):
   ```promql
   rate(jetstream_shard_redelivered_total[5m])
   ```
3. **Short-term:** If the lag is climbing slowly, it may self-recover after a
   burst. Monitor for 10-15 min.
4. **Medium-term:**
   - Scale vertically (more CPU/memory for the affected replica).
   - Increase `shard.count` to reduce per-shard load (requires full rebalance).
   - Increase `shard.max_lag` if the budget was set too aggressively.
5. **Verify resolution:** lag should drop below budget:
   ```promql
   jetstream_shard_consumer_lag < jetstream_shard_lag_budget
   ```

---

## Scenario: High Lag (General)

**Alert:** `ShardConsumerHighLag`

### Symptoms

- `jetstream_shard_consumer_lag > 10000` for a shard group sustained over
  5 minutes.

### Resolution

Follow the same diagnostic steps as [Lag Budget Exceeded](#scenario-lag-budget-exceeded).
If the lag is below the configured budget, this alert is informational — the
system is behind but within acceptable limits.

---

## Scenario: Manual Rebalance

Changing `shard.count` reassigns **every** instrument to a potentially
different shard. This is a breaking change that requires coordinated
redeployment.

### When to Rebalance

- Persistent hot-shard skew that cannot be resolved by scaling vertically.
- Adding capacity to handle growth in instrument count.

### Procedure

1. **Choose new shard count** — prefer powers of 2 (2, 4, 8) for even
   distribution.
2. **Drain existing consumers** — stop all processor replicas. JetStream
   retains messages based on retention policy.
3. **Update configuration** — set `shard.count` to the new value in all
   deployment configs (`deploy/configs/processor.jsonc` or env vars).
4. **Deploy all replicas simultaneously** — each with a unique
   `shard.index` in `[0, new_count)`.
5. **Verify:**
   ```promql
   jetstream_shard_info{shard_count="<new>"}
   ```
   Confirm all expected `shard_index` values appear.
6. **Monitor lag convergence** — all shards should converge from the
   accumulated backlog within minutes.

### Never Do

- **Never change `shard.count` on a subset of replicas.** All replicas must
  agree on the same count or messages will be dropped/duplicated.
- **Never reuse old durable consumer names** with a new shard count. The
  auto-generated names (`mr-processor-g0`, etc.) change with the count.
- **Never skip the drain step** if you need exactly-once semantics during
  the transition.

---

## Scenario: Shard Consumer Stuck

### Symptoms

- One shard's `jetstream_shard_events_total` rate drops to zero.
- `jetstream_shard_redelivered_total` rate spikes (messages keep being
  redelivered because the consumer never acks).
- `jetstream_shard_consumer_lag` climbs monotonically.

### Root Causes

1. **Consumer crash loop** — the processor pod is restarting repeatedly.
2. **Deadlock or goroutine leak** — the consumer goroutine is blocked.
3. **Poison message** — a single malformed message causes repeated processing
   failures and NAK/redelivery cycles.

### Resolution

1. **Check pod status:**
   ```bash
   kubectl get pods -l app=processor,shard-index=<X>
   kubectl logs -l app=processor,shard-index=<X> --tail=100
   ```
2. **Check for poison messages** — look for `ingest_quarantine_total` or
   `ingest_term_total` spikes:
   ```promql
   rate(ingest_quarantine_total[5m])
   rate(ingest_term_total[5m])
   ```
3. **Check goroutine count:**
   ```promql
   process_goroutines{app="processor", shard_index="<X>"}
   ```
4. **Short-term:** Restart the affected pod.
5. **Medium-term:** If caused by a poison message, the consumer's quarantine
   logic should eventually TERM the message. If it doesn't, investigate the
   `max_deliver` JetStream consumer config.

---

## Dashboards

Recommended Grafana panels for shard observability:

### Throughput per Shard

```promql
rate(jetstream_shard_events_total[5m])
```

Legend: `{{ group_id }}`

### Lag per Shard

```promql
jetstream_shard_consumer_lag
```

Legend: `{{ group_id }}` — add a threshold line at the configured `max_lag`.

### Lag Budget Headroom

```promql
1 - (jetstream_shard_consumer_lag / jetstream_shard_lag_budget > 0)
```

Values < 0 mean budget exceeded.

### Skew Ratio

```promql
rate(jetstream_shard_events_total[5m])
/ ignoring(group_id) group_left()
quantile without(group_id) (0.5, rate(jetstream_shard_events_total[5m]))
```

Legend: `{{ group_id }}` — add a threshold line at 3.

### Skip Rate

```promql
rate(jetstream_shard_skip_total[5m])
```

Legend: `{{ group_id }}` — high skip rates are normal; they indicate messages
being routed to other shards.

### Ack Latency p99

```promql
histogram_quantile(0.99, rate(jetstream_shard_ack_latency_seconds_bucket[5m]))
```

Legend: `{{ group_id }}`

### Redelivery Rate

```promql
rate(jetstream_shard_redelivered_total[5m])
```

Legend: `{{ group_id }}` — any sustained redeliveries indicate processing
failures.

### Topology Info

```promql
jetstream_shard_info
```

Verify all expected `shard_index` / `shard_count` combinations are present.
