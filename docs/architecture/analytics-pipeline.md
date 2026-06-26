# Analytics Pipeline

**Status:** Active
**Last updated:** 2026-06-26
**Relates to:** `deploy/compose/docker-compose.yml`, `flink/sql/`, `sql/timescale/migrations/0009_analytics_metabase_views.sql`, `internal/adapters/kafka/`, `docs/architecture/diagrams/c4-analytics.md`

---

## Purpose

The analytics pipeline is a parallel, best-effort data path that taps canonical trade events
for business intelligence. It does **not** replace the NATS primary path — it augments it
with durable SQL-accessible data for Metabase dashboards and ad-hoc queries.

The analytics layer is deployed as an optional Docker Compose profile (`analytics`). The core
pipeline (NATS JetStream) continues to function whether or not the analytics profile is active.

---

## Architecture Overview

```mermaid
flowchart LR
    EX["Exchange WebSocket\n(6 venues)"]
    CON["Consumer\ncmd/consumer :8081"]
    NATS["NATS JetStream\nPrimary Path"]
    KAFKA["Kafka — Redpanda v24.2.13\nport 9092 / 19092"]
    FLINK["Flink SQL 1.19\nflink-jobmanager :8091\nanalytics profile"]
    TS["TimescaleDB\nanalytics schema\nfact_trades / fact_candles / fact_volume_stats"]
    MB["Metabase v0.52.2\nport 3001\nanalytics profile"]
    PROC["Processor\ncmd/processor :8082"]
    CH["ClickHouse\naggregation_cold tables\n90-day TTL"]

    EX --> CON
    CON -->|"marketdata.*.v1\ncanonical"| NATS
    CON -->|"market.trades JSON\nbest-effort"| KAFKA
    NATS --> PROC
    KAFKA --> FLINK
    FLINK -->|"JDBC INSERT\ntumbling windows"| TS
    TS --> MB
    PROC -->|"cold dual-write"| CH

    style KAFKA fill:#ff9900,color:#000
    style FLINK fill:#e6194b,color:#fff
    style MB fill:#4363d8,color:#fff
    style CH fill:#911eb4,color:#fff
```

**Two independent pipelines:**

| Pipeline | Path | Delivery | Use case |
|----------|------|----------|----------|
| **Operational** | Consumer → NATS → Processor → Server/Store | Strict, at-least-once | Live cockpit, sub-millisecond latency |
| **Analytics** | Consumer → Kafka → Flink → TimescaleDB | At-least-once (Kafka boundary); effectively exactly-once at sink via idempotent upsert — see [ADR-0036](../adrs/ADR-0036-analytics-delivery-semantics.md) | BI dashboards, historical queries |

---

## Consumer → Kafka Bridge

The consumer enables dual-path publishing when `analytics.kafka.enabled: true` in
`deploy/configs/consumer.jsonc`.

Code anchor: `internal/adapters/kafka/composite_publisher.go:29`

**Semantics:** `CompositePublisher` calls the NATS publisher first (strict). If NATS succeeds,
it calls the Kafka `MarketPublisher` as best-effort — errors are logged and swallowed, never
propagated back. A NATS failure does NOT attempt Kafka.

**Routing by `MarketPublisher`** (`internal/adapters/kafka/market_publisher.go`):
- `marketdata.trade*` envelopes → decoded (proto or JSON) + re-serialized as flat JSON → `market.trades` topic
- `marketdata.bookdelta*` envelopes → **skipped** (orderbook deltas can exceed Kafka's default `max.message.bytes`)

### Kafka Wire Schema — `market.trades` topic

| Field | Type | Source |
|-------|------|--------|
| `venue` | string | `envelope.Venue` |
| `symbol` | string | `envelope.Instrument` |
| `trade_id` | string | decoded payload |
| `price` | float64 | decoded payload |
| `quantity` | float64 | decoded payload (`size` field alias) |
| `side` | string | decoded payload (`buy` / `sell`) |
| `ts_exchange_ms` | int64 | `envelope.TsExchange` |
| `ts_ingest_ms` | int64 | `envelope.TsIngest` |

Code anchor: `internal/adapters/kafka/market_publisher.go:15` (tradeKafkaMessage struct)

**This schema is a wire contract.** Changes to the struct require updating the Flink source
table in `flink/sql/00_create_sources.sql` in the same commit.

---

## Kafka (Redpanda v24.2.13)

Infrastructure: `deploy/compose/docker-compose.yml`

**No profile gate** — Kafka starts with every `make up` / `make up-infra` / `make up-core`.

| Parameter | Value |
|-----------|-------|
| Image | `redpandadata/redpanda:v24.2.13` |
| Internal broker | `kafka:9092` |
| Host broker | `localhost:19092` |

**Topics:**

| Topic | Producer | Consumer | Schema |
|-------|----------|---------|--------|
| `market.trades` | Consumer (`MarketPublisher`) | Flink (`flink-market-trades` group) | Flat JSON (see wire schema above) |
| `market.orderbook` | Consumer (`MarketPublisher`) | *(no active consumer)* | Raw bytes (future use) |

---

## Flink SQL Pipeline (Apache Flink 1.19)

Infrastructure: `deploy/compose/docker-compose.yml` — `analytics` profile.

Three compose services:
- `flink-jobmanager` — REST API and scheduler (host port 8091)
- `flink-taskmanager` — task execution worker
- `flink-sql-init` — one-shot job submitter (`restart: "no"`)

`flink-sql-init` concatenates all `flink/sql/0*.sql` files in lexicographic order and submits
them as a single Flink SQL session.

### Source Tables (`flink/sql/00_create_sources.sql`)

| Table | Kafka topic | Group ID | Startup mode | Watermark |
|-------|-------------|---------|--------------|-----------|
| `kafka_trades` | `market.trades` | `flink-market-trades` | `group-offsets` | 5 seconds event-time |

### Sink Tables (`flink/sql/01_create_sinks.sql`)

All sinks write to TimescaleDB `analytics` schema via JDBC connector.

| Flink sink table | TimescaleDB table | Upsert key | Flush |
|-----------------|-------------------|-----------|-------|
| `pg_fact_trades` | `analytics.fact_trades` | `(exchange_name, symbol, trade_id)` | 500 rows / 2s |
| `pg_fact_candles` | `analytics.fact_candles` | `(exchange_name, symbol, timeframe, open_time_ms)` | 200 rows / 5s |
| `pg_fact_volume_stats` | `analytics.fact_volume_stats` | `(exchange_name, symbol, window_secs, window_start_ms)` | 200 rows / 5s |

### SQL Jobs

| File | Description |
|------|-------------|
| `flink/sql/02_ohlcv_job.sql` | 4× `INSERT INTO pg_fact_candles`: tumbling windows 1m, 5m, 15m, 1h. `FIRST_VALUE(price)` = open, `LAST_VALUE(price)` = close — deterministic because the producer keys every message as `venue:instrument`, routing all trades for a symbol to a single Kafka partition and therefore a single Flink sub-task (see [ADR-0036](../adrs/ADR-0036-analytics-delivery-semantics.md)). |
| `flink/sql/03_volume_stats_job.sql` | 5-minute volume stats: total/buy/sell volume, `trade_count`, VWAP per symbol |
| `flink/sql/04_trade_tape_job.sql` | Append-only trade tape: every raw trade copied to `fact_trades` |

**Watermark latency floor:** The 5-second event-time watermark means tumbling window results
are emitted at most 5 seconds after the window boundary.

**Checkpointing:** All three jobs run with `execution.checkpointing.interval = 60s` in
`EXACTLY_ONCE` mode. Checkpoints are stored in the `stream-analytics-flink-checkpoints`
named volume (compose) / `/flink/checkpoints` PVC path (k8s). On restart, Flink restores
from the latest checkpoint; if none exists, it falls back to the committed `group-offsets`
consumer position. Maximum re-processing window: 60 seconds.

---

## TimescaleDB Analytics Schema

Created by migration: `sql/timescale/migrations/0009_analytics_metabase_views.sql`

### Fact Tables (owned by Flink)

| Table | Purpose | Owner |
|-------|---------|-------|
| `analytics.fact_trades` | Raw trade tape (append-only) | Flink (`04_trade_tape_job.sql`) |
| `analytics.fact_candles` | OHLCV by timeframe (1m/5m/15m/1h) | Flink (`02_ohlcv_job.sql`) |
| `analytics.fact_volume_stats` | Volume aggregations (5-min windows) | Flink (`03_volume_stats_job.sql`) |

### Views (queryable by Metabase)

| View | Description |
|------|-------------|
| `v_market_summary_24h` | 24h summary: volume, trade count, buy/sell split, VWAP, high/low per symbol |
| `v_candles` | OHLCV with derived `price_change` and `price_change_pct` |
| `v_volume_stats` | Volume with buy/sell ratio, `delta_volume`, `delta_pct` |
| `v_cvd` | Cumulative Volume Delta via window sum over `fact_volume_stats` |
| `v_ingestion_latency` | Per-trade latency (`ts_ingest_ms − ts_exchange_ms`) for monitoring |
| `v_agg_candles` | Alias of `aggregation_candle` (hot-path OHLCV with buy/sell) |
| `v_agg_stats` | Alias of `aggregation_stats` (liquidations, mark price, funding rate) |
| `v_agg_oi` | Alias of `aggregation_oi` (open interest with delta) |
| `v_agg_cvd` | Alias of `aggregation_cvd` (CVD from hot-path) |
| `v_agg_tape` | Alias of `aggregation_tape` (trade-flow tape with burst flag) |
| `v_agg_delta_volume` | Alias of `aggregation_delta_volume` (per-window delta with flow ratio) |

---

## Metabase (v0.52.2)

Infrastructure: `deploy/compose/docker-compose.yml` — `analytics` profile. Port 3001.

```bash
# Start analytics profile
make up-analytics

# Provision dashboards (run once after Metabase reports healthy)
make metabase-provision

# Smoke check
curl -sf http://127.0.0.1:3001/api/health && echo "metabase: OK"
```

---

## Separation from ClickHouse Cold Path

The Flink analytics pipeline (→ TimescaleDB `analytics` schema) is **entirely separate**
from the ClickHouse cold path managed by `cmd/store`.

| Aspect | Flink analytics | ClickHouse cold |
|--------|----------------|----------------|
| Source | Kafka `market.trades` | NATS JetStream `aggregation.*` |
| Target | TimescaleDB `analytics` schema | ClickHouse `default` database |
| Profile gate | `analytics` | none (always runs via `cmd/store`) |

Flink does NOT write to ClickHouse. ClickHouse is not a Flink sink.

---

## Delivery Semantics

See [ADR-0036 — Analytics Delivery Semantics](../adrs/ADR-0036-analytics-delivery-semantics.md)
for the full rationale. Summary:

| Boundary | Guarantee | Mechanism |
|----------|-----------|-----------|
| Consumer → Kafka | Best-effort | Errors swallowed; never propagates to NATS path |
| Kafka → Flink | At-least-once | Checkpoint-based offset tracking; `group-offsets` fallback |
| Flink → TimescaleDB | Effectively exactly-once | JDBC connector generates `INSERT ... ON CONFLICT DO UPDATE` on the natural key for each fact table |

The combined effect is that any given `(exchange_name, symbol, trade_id)` /
`(exchange_name, symbol, timeframe, open_time_ms)` / `(exchange_name, symbol, window_secs, window_start_ms)`
row converges to the correct final value even under Flink restart, because the upsert
overwrites a partial row with the final aggregated value.

---

## Operational Notes

**Flink job resubmission** — `flink-sql-init` has `restart: "no"` (runs once). To re-submit:
```bash
docker compose -f deploy/compose/docker-compose.yml \
  --env-file deploy/envs/local.env \
  --profile analytics \
  restart flink-sql-init
```

**Kafka lag monitoring:**
```bash
rpk group describe flink-market-trades --brokers=localhost:19092
```

**Flink overview:**
```bash
curl -s http://127.0.0.1:8091/overview | jq .taskmanagers
```

---

## Known Limitations & Roadmap

These are conscious scope decisions, not oversights.

### No `tenant_id` in the analytics path

The analytics schema (`fact_trades`, `fact_candles`, `fact_volume_stats`) has no tenant
dimension. The only segmentation axes are `exchange_name` and `symbol`. `tenant_id` exists
in the WebSocket delivery layer (JWT claim, per-tenant Prometheus metrics, per-tenant rate
limits) but was deliberately excluded from the analytics path because this project targets a
single-operator deployment. Adding tenant support would require:

1. Propagating `tenant_id` from the Consumer into the Kafka message schema.
2. Adding the column to all three Flink sink DDLs and TimescaleDB migrations.
3. Updating Metabase filters to expose per-tenant views.

This makes sense when the platform serves multiple operators or exchange clients from the
same deployment.

### No Schema Registry

Kafka uses plain JSON without a Schema Registry (Confluent or Redpanda SR). The current
wire schema is defined in code (`internal/adapters/kafka/market_publisher.go`) and mirrored
in the Flink DDL (`flink/sql/00_create_sources.sql`). Schema drift between producer and Flink
consumer is caught only at query time (Flink silently drops unknown fields via
`json.ignore-parse-errors: true`).

A Schema Registry would be warranted when:

- The `market.trades` schema needs to evolve with backward/forward compatibility guarantees.
- Multiple teams or services consume the same Kafka topics.
- Compliance or audit requirements demand a versioned schema history.

For a single-team, single-consumer setup the current approach is adequate. The wire schema
is the contract; changes must update both the producer struct and the Flink DDL atomically.
