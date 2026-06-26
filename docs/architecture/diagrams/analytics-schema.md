# TimescaleDB Analytics Schema

**Status:** Active
**Last updated:** 2026-06-26
**Relates to:** `docs/architecture/analytics-pipeline.md`, `sql/timescale/migrations/0009_analytics_metabase_views.sql`
**Code anchors:** `sql/timescale/migrations/0009_analytics_metabase_views.sql`

---

## What this shows

The TimescaleDB `analytics` schema has two source layers: Flink-populated fact tables (DW layer)
and hot-path operational tables (aliased into the analytics schema). Eleven views sit above both
layers and are the direct query targets for Metabase dashboards.

---

## Schema Overview

```mermaid
graph TB
    subgraph Flink["Flink → Fact Tables (DW layer)"]
        FC["fact_candles\nexchange_name, symbol, timeframe\nopen_time_ms, open/high/low/close\nvolume, trade_count"]
        FVS["fact_volume_stats\nexchange_name, symbol\nwindow_start_ms, window_secs\ntotal_volume, buy_volume, sell_volume\ntrade_count, vwap"]
        FT["fact_trades\nexchange_name, symbol, trade_id\nprice, quantity, side\nts_exchange_ms, ts_ingest_ms"]
    end

    subgraph Ops["Operational Tables (hot-path)"]
        AC["aggregation_candle\n(buy_volume, sell_volume included)"]
        AS["agg_stats\n(funding_rate, mark_price, liq_*)"]
    end

    subgraph DW["DW Views (over fact tables)"]
        V1["v_market_summary_24h\n24h rolling summary per venue/symbol\ntrade_count, buy/sell volume\nlow_24h, high_24h, vwap_24h"]
        V2["v_candles\nOHLCV + price_change\nprice_change_pct (derived)"]
        V3["v_volume_stats\nbuy_pct, sell_pct\ndelta_volume, delta_pct (derived)"]
        V4["v_cvd\ncumulative volume delta\nwindow function over window_start_ms"]
        V5["v_ingestion_latency\nts_ingest_ms − ts_exchange_ms\nper-trade latency_ms"]
    end

    subgraph OV["Operational Views (over hot-path tables)"]
        V6["v_agg_candles\nbuy_pct derived\nmatches DW column names"]
        V7["v_agg_stats\nliquidations, mark_price\nfunding_rate"]
        V8["v_agg_oi\nopen interest"]
        V9["v_agg_cvd\nCVD from operational tables"]
        V10["v_agg_tape\ntrade tape from operational tables"]
        V11["v_agg_delta_volume\ndelta volume"]
    end

    MB["Metabase v0.52.2\n(queries both view sets\nfor cross-dataset analysis)"]

    FC --> V1
    FC --> V2
    FVS --> V3
    FVS --> V4
    FT --> V1
    FT --> V5

    AC --> V6
    AC --> V9
    AC --> V11
    AS --> V7
    AS --> V8
    AS --> V10

    V1 --> MB
    V2 --> MB
    V3 --> MB
    V4 --> MB
    V5 --> MB
    V6 --> MB
    V7 --> MB
    V8 --> MB
    V9 --> MB
    V10 --> MB
    V11 --> MB
```

---

## Source Layer Comparison

| Layer | Source tables | Populated by | Latency | Use case |
|-------|--------------|--------------|---------|----------|
| **DW** | `fact_candles`, `fact_volume_stats`, `fact_trades` | Flink SQL (JDBC) | 10–90 s | BI dashboards, historical queries |
| **Operational** | `aggregation_candle`, `agg_stats`, … | Store binary (NATS path) | < 1 s | Cross-dataset joins in Metabase |

Metabase queries both layers via the unified `analytics` schema — the `v_agg_*` views alias
hot-path columns to match DW naming conventions (`venue` → `exchange_name`, `instrument` → `symbol`).

---

## Related Diagrams

- [Flink Jobs Detail](flink-jobs-detail.md) — how fact tables are populated
- [C4 Analytics](c4-analytics.md) — container topology of the full analytics profile
- [Analytics Pipeline Sequence](sequence-analytics-pipeline.md) — end-to-end event flow
