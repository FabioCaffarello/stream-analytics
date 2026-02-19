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

The top-level `shard` config is the single source of truth.  It propagates
automatically to the JetStream consumer:

```jsonc
{
  "shard": {
    "index": 0,     // 0-based shard index for this instance
    "count": 1,     // total number of shards (1 = disabled)
    "max_lag": 0    // lag budget per shard (0 = no enforcement)
  }
}
```

| Field           | Default | Description                                       |
|-----------------|---------|---------------------------------------------------|
| `shard.index`   | `0`     | Shard index for this instance `[0, count)`        |
| `shard.count`   | `1`     | Total shard count (1 = sharding disabled)         |
| `shard.max_lag` | `0`     | Lag budget per shard (0 = no budget enforcement)  |

CLI flags and environment variables override JSONC values:

| Source          | Flag             | Env var        |
|-----------------|------------------|----------------|
| Shard index     | `-shard-index`   | `SHARD_INDEX`  |
| Shard count     | `-shard-count`   | `SHARD_COUNT`  |

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

| Metric                                | Type      | Description                                    |
|---------------------------------------|-----------|------------------------------------------------|
| `jetstream_shard_consumer_lag`        | Gauge     | NumPending lag per `{group_id}`                |
| `jetstream_shard_redelivered_total`   | Counter   | Redeliveries per `{group_id}`                  |
| `jetstream_shard_ack_latency_seconds` | Histogram | Processing latency per `{group_id}`            |
| `jetstream_shard_skip_total`          | Counter   | Messages skipped per `{group_id}`              |
| `jetstream_shard_events_total`        | Counter   | Events successfully processed per `{group_id}` |
| `jetstream_shard_info`                | Gauge     | Static topology info (`{shard_index, shard_count}`, always 1) |
| `jetstream_shard_lag_budget`          | Gauge     | Configured max-lag budget per `{group_id}` (0 = no budget) |

All metrics carry the `group_id` label (e.g. `"0"`, `"1"`) so dashboards and
alerts can be scoped per replica.

## Alerts

Three production alert rules are defined in
[`deploy/observability/prometheus/shard-alerts.rules.yml`](../../deploy/observability/prometheus/shard-alerts.rules.yml):

| Alert                       | Severity | Condition                                      | Window |
|-----------------------------|----------|-------------------------------------------------|--------|
| `ShardHotSkew`              | warning  | One shard has 3x median throughput              | 5 min  |
| `ShardLagBudgetExceeded`    | critical | Shard lag exceeds configured `max_lag` budget   | 3 min  |
| `ShardConsumerHighLag`      | warning  | Shard consumer lag > 10 000 messages            | 5 min  |

For incident response procedures, see the
**[Shard Incident Runbook](shard-incidents.md)**.

## Quickstart: 2 shards local (docker compose)

The consumer and processor services support `--scale` via `SHARD_INDEX`
and `SHARD_COUNT` environment variables.

```bash
# Start infrastructure
cd deploy/compose
docker compose --profile core up -d nats clickhouse server

# Start 2 processor shards (manual index assignment)
SHARD_COUNT=2 SHARD_INDEX=0 docker compose --profile core up -d processor
SHARD_COUNT=2 SHARD_INDEX=1 docker compose --profile core run -d processor

# Verify both shards are running
docker compose ps processor
docker compose logs processor | grep 'shard='
```

Each processor instance logs its shard assignment at startup:

```
processor starting shard=0/2 bus_type=jetstream
processor starting shard=1/2 bus_type=jetstream
```

The durable consumer name is auto-suffixed: `processor-v1-s0`, `processor-v1-s1`.

## Optional shard registry lease (feature-flagged)

Processor startup supports an optional JetStream KV lease guard for shard
ownership and topology completeness checks.

- `SHARD_REGISTRY_ENABLED=true` enables lease acquire/heartbeat/release (default: off).
- `SHARD_REGISTRY_STRICT=true` fails startup if topology stays incomplete after grace.
- `SHARD_REGISTRY_GRACE=60s` sets topology wait window (default: `60s`).
- Bucket: `MR_SHARD_REGISTRY`, key format: `shard/{index}`, TTL: `30s`, heartbeat: `10s`.
- On lease loss, processor logs `shard lease lost`, triggers graceful shutdown, and exits non-zero.
