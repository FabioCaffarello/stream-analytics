# C4 Level 1 — System Context

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/README.md`, `docs/architecture/diagrams/c4-containers.md`

---

## What this shows

Stream Analytics in its external environment: who interacts with it, what external systems it depends on,
and what it produces. No internal detail — only the system boundary and its relationships.

---

## Diagram

```mermaid
C4Context
    title System Context - Stream Analytics

    Person(operator, "Operator", "Monitors live market data across 6 venues via cockpit")

    System(raccoon, "Stream Analytics", "Real-time multi-exchange crypto market data platform (~161K LOC, Go + Odin)")

    System_Ext(binance_spot, "Binance Spot", "WebSocket feed, SPOT markets")
    System_Ext(binance_futures, "Binance Futures", "WebSocket feed, USD_M_FUTURES")
    System_Ext(bybit, "Bybit", "WebSocket feed, USD_M_FUTURES")
    System_Ext(coinbase, "Coinbase", "WebSocket feed, SPOT markets")
    System_Ext(hyperliquid, "HyperLiquid", "WebSocket feed, USD_M_FUTURES")
    System_Ext(kraken, "Kraken (spot + futures)", "WebSocket feeds, SPOT + USD_M_FUTURES")

    System_Ext(timescale, "TimescaleDB", "Hot storage, 7-day rolling window, PG16")
    System_Ext(clickhouse, "ClickHouse", "Cold storage, analytical archive, long-retention")
    System_Ext(nats, "NATS JetStream", "Durable event bus, at-least-once delivery")
    System_Ext(prometheus, "Prometheus + Grafana", "Metrics: 5 dashboards, 100+ series")

    Rel(operator, raccoon, "Views live market data", "WebSocket / WASM / native app")
    Rel(raccoon, binance_spot, "Subscribes", "WSS")
    Rel(raccoon, binance_futures, "Subscribes", "WSS")
    Rel(raccoon, bybit, "Subscribes", "WSS")
    Rel(raccoon, coinbase, "Subscribes", "WSS")
    Rel(raccoon, hyperliquid, "Subscribes", "WSS")
    Rel(raccoon, kraken, "Subscribes", "WSS")
    Rel(raccoon, nats, "Publishes / Consumes events", "JetStream protocol")
    Rel(raccoon, timescale, "Reads / Writes hot data", "SQL / pgx")
    Rel(raccoon, clickhouse, "Reads / Writes cold data", "ClickHouse HTTP + native")
    Rel(prometheus, raccoon, "Scrapes /metrics", "HTTP pull")
```

---

## Key Decisions Visible at This Level

| Decision | Rationale |
|----------|-----------|
| NATS JetStream as central bus | At-least-once delivery, durable consumers, replay-safe; decouples ingestion from aggregation |
| Dual storage (TimescaleDB + ClickHouse) | TimescaleDB optimizes recent time-series queries; ClickHouse handles analytical/archive reads efficiently |
| Multi-exchange fan-in | 6 venues normalized to a single Canonical Market Model (CMM) — consumers see one event shape |
| Operator-facing only | This is decision infrastructure, not a public API. No external data consumers beyond the cockpit |

---

## What Is NOT Shown Here

- Internal services and actors — see [C4 Container Map](c4-containers.md)
- Data flow sequencing — see [Live Data Ingestion](sequence-live-ingestion.md)
- Actor supervision tree — see [Actor Supervision Tree](actor-supervision-tree.md)
