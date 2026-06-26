# C4 Level 2 — Analytics Profile Container Map

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/analytics-pipeline.md`, `docs/architecture/diagrams/c4-containers.md`

---

## What this shows

The analytics Docker Compose profile containers and their relationships.
Complements the main C4 Container Map by focusing on the
best-effort analytics branch (Kafka → Flink → TimescaleDB analytics → Metabase).

---

## Diagram

```mermaid
C4Container
    title Analytics Profile - Container Map

    Person(operator, "Analyst / Operator", "Views BI dashboards and runs historical queries")

    System_Boundary(core, "Stream Analytics Core (always on)") {
        Container(consumer, "consumer", "Go binary", "Dual-publishes to NATS (primary) and Kafka (analytics)")
        Container(processor, "processor", "Go binary", "Aggregates market data; cold-writes to ClickHouse")
    }

    System_Boundary(analytics, "Analytics Profile (opt-in)") {
        Container(kafka, "Kafka / Redpanda", "v24.2.13, port 9092", "Distributes market.trades and market.orderbook topics")
        Container(flink_jm, "Flink JobManager", "Flink 1.19, port 8091", "Orchestrates 3 SQL jobs: ohlcv, volume_stats, trade_tape")
        Container(flink_tm, "Flink TaskManager", "Flink 1.19", "Executes Flink tasks; reads Kafka, writes TimescaleDB via JDBC")
        Container(metabase, "Metabase", "v0.52.2, port 3001", "BI dashboards; 11 views over TimescaleDB analytics schema")
    }

    System_Boundary(storage, "Storage (always on)") {
        ContainerDb(ts_analytics, "TimescaleDB analytics", "PostgreSQL 16", "3 fact tables; 11 Metabase views; owned by Flink")
        ContainerDb(ts_hot, "TimescaleDB public", "PostgreSQL 16", "Hot-path aggregations; aliased by v_agg_* views")
        ContainerDb(clickhouse, "ClickHouse", "24.8.8", "Cold archive: aggregation_*_cold; 90-day TTL; not a Flink target")
    }

    Rel(consumer, kafka, "Publishes trades", "Kafka protocol")
    Rel(kafka, flink_jm, "market.trades topic", "Kafka consumer group")
    Rel(flink_jm, flink_tm, "Task distribution", "Flink internal")
    Rel(flink_tm, ts_analytics, "INSERT fact rows", "JDBC batch flush")
    Rel(ts_analytics, metabase, "SQL queries", "PostgreSQL wire")
    Rel(ts_hot, metabase, "v_agg_* views", "PostgreSQL wire")
    Rel(processor, clickhouse, "Cold writes", "ClickHouse native")
    Rel(operator, metabase, "Views dashboards", "HTTP browser")
```

---

## Key Architectural Decisions

| Decision | Rationale |
|----------|-----------|
| Kafka has no profile gate | Starts with core stack — costs nothing idle; avoids dependency gap when analytics is enabled |
| Flink writes only to TimescaleDB `analytics` schema | Keeps analytics and operational schemas isolated; ClickHouse is not a Flink target |
| Metabase reads both schemas | The 11 views include `v_agg_*` aliases of hot-path tables for cross-dataset analysis |
| `flink-sql-init` is one-shot | Jobs submitted once; Flink manages them. Re-submit manually after schema changes |
