# Horizontal Scaling with Static Shard-Range Consumers

**Status:** Active
**Last updated:** 2026-02-19

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

## Quickstart: N shards local (docker compose)

DEV/local uses `docker compose --scale processor=N` and derives `SHARD_INDEX`
automatically when `SHARD_INDEX` is not set.

```bash
# Start with 3 processor replicas; shard count defaults to replica count
make up PROCESSOR_REPLICAS=3

# Or run the dedicated smoke command
make dev-scale-smoke N=3
```

Expected logs per replica include shard source and assignment, for example:

```
shard resolution applied shard_index_source=hostname shard_index=0 shard_count=3
processor starting shard=0/3 bus_type=jetstream
```

Durable names are shard-aware when count > 1: `processor-v4-s0`, `processor-v4-s1`, `processor-v4-s2`, ...

## DEV vs PROD shard index source

- DEV (`MR_ENV=dev`, default in docker compose): when `SHARD_COUNT>1` and `SHARD_INDEX` is unset, runtime can derive index from hostname/ordinal.
- PROD (`MR_ENV=prod`): `SHARD_INDEX` must be explicit for `SHARD_COUNT>1`; hostname fallback is rejected (fail-fast).
- For single shard (`SHARD_COUNT=1`), index defaults to `0`.

## Optional shard registry lease (feature-flagged)

Processor startup supports an optional JetStream KV lease guard for shard
ownership and topology completeness checks.

- `SHARD_REGISTRY_ENABLED=true` enables lease acquire/heartbeat/release (default: off).
- `SHARD_REGISTRY_STRICT=true` fails startup if topology stays incomplete after grace.
- `SHARD_REGISTRY_GRACE=60s` sets topology wait window (default: `60s`).
- Bucket: `MR_SHARD_REGISTRY`, key format: `shard/{index}`, TTL: `30s`, heartbeat: `10s`.
- On lease loss, processor logs `shard lease lost`, triggers graceful shutdown, and exits non-zero.
