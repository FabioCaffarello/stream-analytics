# Horizontal Scaling with Static Shard-Range Consumers

## Overview

Market-raccoon processors scale horizontally using **static shard-range
consumers** — a coordination-free partitioning strategy where each replica is
responsible for a deterministic subset of the subject space.

No external KV store, etcd, or Zookeeper is required.  Assignment is computed
purely from `FNV-1a(venue + instrument) % groupCount`.

## How it works

Every JetStream subject follows the canonical taxonomy:

```
{event}.v{version}.{venue}.{instrument}
```

The **shard key** is derived from the last two tokens (`venue` + `instrument`),
so all events for the same order book — regardless of event type or version —
always go to the same processor replica.

```
ShardKey("marketdata.bookdelta.v1.binance.BTCUSDT")
  == ShardKey("marketdata.trade.v1.binance.BTCUSDT")
  == ShardKey("aggregation.snapshot.v1.binance.BTCUSDT")
```

Each replica creates its own durable JetStream consumer (`mr-processor-g0`,
`mr-processor-g1`, …).  All replicas subscribe to the same filter subjects
(e.g. `marketdata.bookdelta.>`).  Messages that hash to a different group are
immediately acked and skipped (client-side dispatch).

## Configuration

```jsonc
{
  "bus": { "type": "jetstream" },
  "jetstream": {
    "shard_group_count": 2,   // total number of replicas
    "shard_group_id":    0    // this replica's index, 0-based
  }
}
```

| Field               | Default | Description                                      |
|---------------------|---------|--------------------------------------------------|
| `shard_group_count` | `1`     | Total shard groups (1 = sharding disabled)       |
| `shard_group_id`    | `0`     | Group index for this instance `[0, count)`       |

When `shard_group_count = 1` (the default), the system behaves exactly as
before — no messages are ever skipped, the durable consumer name remains
`processor-v1`, and no shard metrics are emitted.

## Scaling replicas

To scale from 1 to N replicas:

1. **Pick N** (e.g. `N = 2`).  Keep N stable — changing it reassigns subjects.
2. **Deploy N replicas**, each with a unique `shard_group_id` in `[0, N)`:

   ```
   replica-0: shard_group_count=2, shard_group_id=0
   replica-1: shard_group_count=2, shard_group_id=1
   ```

3. **Verify** via Prometheus that each group's lag converges:

   ```promql
   jetstream_shard_consumer_lag{group_id="0"}
   jetstream_shard_consumer_lag{group_id="1"}
   ```

4. To add more replicas you **must redeploy all groups** with the new count,
   because the modulo assignment changes for every existing instrument.

## Important constraints

- **Never change `shard_group_count` without redeploying all groups.**
  Doing so causes subjects to be double-processed or dropped.
- **All replicas must use the same `shard_group_count`.**
- `shard_group_id` must be unique per replica and in `[0, shard_group_count)`.
- Increasing the replica count of one group (K8s HPA) does NOT change shard
  mapping — it just adds redundancy within the same group via JetStream's
  existing pull-consumer round-robin.

## Metrics per shard group

| Metric                              | Type      | Description                        |
|-------------------------------------|-----------|------------------------------------|
| `jetstream_shard_consumer_lag`      | Gauge     | NumPending lag per `{group_id}`    |
| `jetstream_shard_redelivered_total` | Counter   | Redeliveries per `{group_id}`      |
| `jetstream_shard_ack_latency_seconds` | Histogram | Processing latency per `{group_id}`|
| `jetstream_shard_skip_total`        | Counter   | Messages skipped per `{group_id}`  |

All metrics carry the `group_id` label (e.g. `"0"`, `"1"`) so dashboards and
alerts can be scoped per replica.

## Example alert

```yaml
- alert: ShardConsumerHighLag
  expr: jetstream_shard_consumer_lag > 10000
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Shard group {{ $labels.group_id }} lag is high ({{ $value }} messages)"
```
