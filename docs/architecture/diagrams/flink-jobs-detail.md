# Flink SQL Jobs — Detail

**Status:** Active
**Last updated:** 2026-06-26
**Relates to:** `docs/architecture/analytics-pipeline.md`, `flink/sql/`, `docs/architecture/diagrams/c4-analytics.md`
**Code anchors:** `flink/sql/02_ohlcv_job.sql`, `flink/sql/03_volume_stats_job.sql`, `flink/sql/04_trade_tape_job.sql`

---

## What this shows

The three Flink SQL jobs that run concurrently inside the Flink cluster, their Kafka source,
window types, key aggregation logic, and TimescaleDB sinks. All three jobs share the same
`kafka_trades` source connector but produce independent output tables.

---

## Diagram

```mermaid
graph LR
    subgraph Kafka["Kafka / Redpanda — market.trades topic"]
        KT["kafka_trades\nJSON: venue, symbol, trade_id\nprice, quantity, side\nts_exchange_ms, ts_ingest_ms\nevent_time watermark − 5s"]
    end

    subgraph Flink["Flink Cluster (JobManager :8091 + TaskManagers)"]
        J2["02_ohlcv_job.sql\n─────────────────\nWindow: TUMBLE on event_time\n  1m / 5m / 15m / 1h\nAgg: FIRST_VALUE(price) → open\n     LAST_VALUE(price) → close\n     MAX(price) → high\n     MIN(price) → low\n     SUM(quantity) → volume\nBatch: 200 rows / 5s flush"]

        J3["03_volume_stats_job.sql\n─────────────────\nWindow: TUMBLE INTERVAL '5' MINUTE\nAgg: SUM qty WHERE side='buy' → buy_volume\n     SUM qty WHERE side='sell' → sell_volume\n     SUM(price×qty)/SUM(qty) → vwap\n     COUNT(*) → trade_count\nBatch: 200 rows / 5s flush"]

        J4["04_trade_tape_job.sql\n─────────────────\nNo windowing — row-by-row\nPassthrough: all fields verbatim\nBatch: 500 rows / 2s flush\nAppend-only audit trail"]
    end

    subgraph TS["TimescaleDB — analytics schema"]
        FC["fact_candles\n(exchange_name, symbol, timeframe\nopen_time_ms, open, high, low,\nclose, volume, trade_count)"]
        FVS["fact_volume_stats\n(exchange_name, symbol\nwindow_start_ms, window_secs\ntotal_volume, buy_volume, sell_volume\ntrade_count, vwap)"]
        FT["fact_trades\n(exchange_name, symbol, trade_id\nprice, quantity, side\nts_exchange_ms, ts_ingest_ms)"]
    end

    KT --> J2
    KT --> J3
    KT --> J4
    J2 -->|"JDBC INSERT\n4 timeframes"| FC
    J3 -->|"JDBC INSERT\n5-min windows"| FVS
    J4 -->|"JDBC INSERT\nappend-only"| FT
```

---

## Key Architectural Notes

| Property | Value |
|----------|-------|
| Flink version | Apache Flink SQL 1.19 |
| Source connector | Kafka (Redpanda) — `market.trades` topic |
| Sink connector | JDBC → TimescaleDB `analytics` schema |
| Watermark slack | 5 seconds (event-time lag tolerance) |
| Job submission | One-shot via `flink-sql-init` container at startup |
| Recovery | Flink manages job restart; re-submit manually after schema changes |
| Startup mode | `scan.startup.mode: latest-offset` — events before Flink connects are not replayed |

The three jobs run **concurrently** inside the Flink TaskManagers. A failure in one job does not
affect the others. All three read from the same Kafka topic via independent consumer groups.
