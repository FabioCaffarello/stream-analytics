# C4 Level 2 — Container Map

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/diagrams/c4-context.md`, `docs/architecture/subsystems.md`

---

## What this shows

The 7 service binaries, the shared message bus, and storage tiers — with their primary
communication paths and protocols. Internal module structure is not shown here.

---

## Diagram

```mermaid
C4Container
    title Container Map - Stream Analytics Backend

    Person(operator, "Operator", "Monitors live market data across 6 venues")

    System_Ext(exchanges, "6 Exchanges", "Binance Spot/Futures, Bybit, Coinbase, HyperLiquid, Kraken")

    System_Boundary(raccoon, "Stream Analytics") {

        Container(consumer, "consumer", "Go / Hollywood actors", "Canonicalize, dedup, sequence; publish to JetStream")
        Container(processor, "processor", "Go / Hollywood actors", "Aggregation + Insights + Evidence; publish aggregation events")
        Container(server, "server", "Go / HTTP + WebSocket", "Terminal_V1 gateway; cold reads /api/v1/*; /metrics")
        Container(store, "store", "Go / Storage adapters", "aggregation.* -> TimescaleDB (hot) + ClickHouse (cold)")
        Container(migrate, "migrate", "Go / Goose", "Schema migrations for TimescaleDB and ClickHouse")
        Container(emulator, "emulator", "Go", "Synthetic event injection for integration and soak tests")
        Container(validator, "validator", "Go", "JetStream schema validation; HTTP healthcheck :8089")
        Container(client, "client (cockpit)", "Odin WASM + native", "Live market data: candles, DOM, heatmap, VPVR, orderbook")
    }

    ContainerDb(nats, "NATS JetStream", "NATS 2.10.18", "Event bus; at-least-once delivery; durable consumers")
    ContainerDb(timescale, "TimescaleDB", "PG16 + TS 2.25.1", "Hot storage: 7-day rolling window; candles, OB, stats")
    ContainerDb(clickhouse, "ClickHouse", "24.8.8", "Cold archive: heatmap, VPVR, compressed history")

    Rel(exchanges, consumer, "Raw WebSocket frames", "WSS")
    Rel(consumer, nats, "marketdata.*.v1", "JetStream publish")
    Rel(nats, processor, "marketdata.*.v1", "JetStream durable consumer")
    Rel(processor, nats, "aggregation + insights + evidence", "JetStream publish")
    Rel(nats, store, "aggregation.*.v1", "JetStream durable consumer")
    Rel(store, timescale, "Candles, orderbook, stats, tape", "SQL / pgx")
    Rel(store, clickhouse, "Heatmap, VPVR, archive", "ClickHouse native")
    Rel(nats, server, "aggregation + evidence", "JetStream durable consumer")
    Rel(server, timescale, "Historical range reads", "SQL / pgx")
    Rel(server, clickhouse, "Historical range reads", "ClickHouse HTTP")
    Rel(client, server, "Subscribe + receive events", "WebSocket Terminal_V1")
    Rel(operator, client, "Interacts", "Browser / native app")
```

---

## Subject Taxonomy (JetStream Streams)

| Stream prefix | Producer | Primary consumers |
|---------------|----------|-------------------|
| `marketdata.*.v1` | consumer | processor |
| `aggregation.*.v1` | processor | store, server |
| `insights.*.v1` | processor | server |
| `liquidity.evidence.v1` | processor (Evidence) | server |

Full subject registry: `docs/contracts/event-bus.md`

---

## Runtime Supervision

Each binary runs a **Guardian** actor that manages its subsystem actors with:
- Base backoff: 250ms, max backoff: 5s
- Restart window: 30s, restart limit: 5 per window
- Global circuit breaker: 5 restarts/min

For the actor tree per binary, see Actor Supervision Tree (`actor-supervision-tree.md`).

---

## Storage Tier Summary

| Tier | Technology | Retention | Use case |
|------|------------|-----------|----------|
| L0 | In-memory ring buffer | ~minutes | Ultra-low latency recent reads |
| L1 | TimescaleDB | 7 days rolling | Hot OHLCV, orderbook, stats |
| L2 | ClickHouse | Long-term archive | Heatmap, VPVR, analytical queries |

Federation logic: [`internal/adapters/storage/federation/merge.go`](https://github.com/FabioCaffarello/stream-analytics/blob/main/internal/adapters/storage/federation/merge.go)
