**Status:** Accepted
**Owner:** Architecture
**Last updated:** 2026-06-26
**Date:** 2026-06-26
**Deciders:** Platform Team
**Relates to:** [ADR-0000](ADR-0000-foundation.md), [analytics-pipeline](../architecture/analytics-pipeline.md)

# ADR-0036 — Analytics Delivery Semantics

## Context

The analytics path (Consumer → Kafka → Flink → TimescaleDB) has different delivery
requirements than the primary NATS path. Three concrete gaps drove this ADR:

1. The delivery semantics of the analytics pipeline were never formally declared.
2. The Flink JDBC sink used `PRIMARY KEY NOT ENFORCED` but no document explained
   what guarantees this provided (or didn't) at the TimescaleDB level.
3. Flink was running without `execution.checkpointing.interval`, meaning the
   restart strategy was configured but no checkpoints were actually taken — a
   restart would lose all in-flight data back to `latest-offset`.

## Decision

### 1. Declared delivery semantics

| Boundary | Guarantee |
|----------|-----------|
| Consumer → Kafka | Best-effort. Kafka errors are swallowed; a Kafka outage never blocks the primary NATS path. |
| Kafka → Flink | At-least-once. Flink re-reads from the last checkpointed offset on restart; `scan.startup.mode = 'group-offsets'` provides a fallback for cold starts without a checkpoint. |
| Flink → TimescaleDB | Effectively exactly-once via idempotent upsert. The Flink JDBC connector generates `INSERT ... ON CONFLICT (pk) DO UPDATE SET ...` for PostgreSQL when the sink table declares `PRIMARY KEY NOT ENFORCED`. The natural keys are: |

| Fact table | Natural key (upsert key) |
|------------|--------------------------|
| `fact_trades` | `(exchange_name, symbol, trade_id)` |
| `fact_candles` | `(exchange_name, symbol, timeframe, open_time_ms)` |
| `fact_volume_stats` | `(exchange_name, symbol, window_secs, window_start_ms)` |

Combined effect: at-least-once delivery at the Kafka boundary with idempotent
sink means the end-to-end observable result is exactly-once for any given natural
key. Duplicate delivery (e.g., on Flink restart within the checkpoint interval)
overwrites rows with identical values — no phantom rows, no counters inflated.

### 2. Checkpointing

`execution.checkpointing.interval = 60s` with `EXACTLY_ONCE` mode is set in
`flink/sql/init.sh`. This means:

- Maximum re-processing on restart: up to 60 seconds of trades from Kafka.
- Checkpoint storage: named Docker volume `stream-analytics-flink-checkpoints`
  (compose) / `/flink/checkpoints` PVC path (k8s — requires a PersistentVolumeClaim
  in the cluster overlay; the ConfigMap path is set but the PVC binding is left to
  the cluster operator).
- Recovery gap: events produced during Flink downtime that remain within Kafka's
  retention window (default 7 days for `market.trades`) are replayed automatically
  on restart.

### 3. Open/close determinism in OHLCV candles

`FIRST_VALUE(price)` and `LAST_VALUE(price)` in `flink/sql/02_ohlcv_job.sql` are
deterministic for this pipeline because:

- The Kafka producer keys every message as `venue:instrument`
  (`internal/adapters/kafka/market_publisher.go:126`).
- Kafka routes all messages with the same key to a single partition; Flink's keyed
  state routes all records for a given `(venue, symbol)` to a single sub-task.
- Records are therefore processed in Kafka-offset order within each symbol, so
  `FIRST_VALUE` = earliest-sent trade (open) and `LAST_VALUE` = latest-sent trade
  (close) for each tumbling window.

**Invariant to preserve:** if the producer key scheme or topic partition count
changes, a consumer-group offset reset is required before restarting Flink jobs,
otherwise open/close values may be incorrect for the transition window.

## Consequences

- The analytics path tolerates data loss only at the Consumer → Kafka boundary
  (fire-and-forget). All other stages are loss-free within Kafka's retention window.
- End-to-end latency budget is unaffected: the 60s checkpoint interval adds no
  observable latency to steady-state operation.
- The k8s Flink overlay requires a `PersistentVolumeClaim` named to match the
  `state.checkpoints.dir` path. This is left to the cluster operator and is
  explicitly out of scope for the local-dev compose setup.

## Evidence

- Checkpointing config: `flink/sql/init.sh`
- Startup mode: `flink/sql/00_create_sources.sql`
- Upsert sink DDL: `flink/sql/01_create_sinks.sql`
- Open/close determinism comment: `flink/sql/02_ohlcv_job.sql`
- Checkpoint volume (compose): `deploy/compose/docker-compose.yml`
- Checkpoint path (k8s): `deploy/gitops/apps/analytics/flink/base/flink-config-configmap.yaml`
- Producer key: `internal/adapters/kafka/market_publisher.go:126`

## Changelog

- 2026-06-26: Accepted — analytics delivery semantics formalised; checkpointing enabled;
  open/close determinism documented
