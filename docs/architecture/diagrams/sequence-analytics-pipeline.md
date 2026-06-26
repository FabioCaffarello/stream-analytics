# Sequence Diagram — Analytics Pipeline

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/analytics-pipeline.md`, `docs/architecture/diagrams/c4-analytics.md`
**Code anchors:** `internal/adapters/kafka/composite_publisher.go`, `flink/sql/`, `sql/timescale/migrations/0009_analytics_metabase_views.sql`

---

## What this shows

The end-to-end flow of the analytics pipeline: from the consumer publishing to Kafka
(best-effort alongside the primary NATS path), through Flink SQL tumbling window
aggregations, to TimescaleDB and Metabase.

---

## Analytics Pipeline Sequence

```mermaid
sequenceDiagram
    autonumber

    participant Exch as Exchange<br/>(WebSocket)
    participant Con as Consumer<br/>(CompositePublisher)
    participant NATS as NATS JetStream<br/>(primary path)
    participant Kafka as Kafka<br/>(Redpanda)
    participant Flink as Flink TaskManager<br/>(kafka_trades source)
    participant TS as TimescaleDB<br/>(analytics schema)
    participant MB as Metabase<br/>(query layer)
    participant Op as Analyst / Operator

    Note over Exch,Con: 1. Exchange trade arrives

    Exch->>Con: raw WS trade frame
    Con->>Con: canonicalize → CMM envelope

    Note over Con,NATS: 2. Primary path (strict)

    Con->>NATS: publish marketdata.trade.v1 envelope
    NATS-->>Con: PubAck

    Note over Con,Kafka: 3. Analytics path (best-effort, parallel)

    Con->>Con: CompositePublisher: decode proto/JSON<br/>→ tradeKafkaMessage{venue, symbol,<br/>trade_id, price, quantity, side,<br/>ts_exchange_ms, ts_ingest_ms}
    Con->>Kafka: produce JSON to market.trades topic

    alt Kafka produce fails
        Kafka--xCon: error
        Con->>Con: log error, swallow — NATS already ACKed
        Note over Con: Primary path unaffected
    else Kafka produce succeeds
        Kafka-->>Con: produce ACK (async)
    end

    Note over Kafka,Flink: 4. Flink consumes from Kafka (continuous)

    Kafka->>Flink: poll market.trades<br/>(group: flink-market-trades)
    Flink->>Flink: assign event_time = TO_TIMESTAMP_LTZ(ts_exchange_ms, 3)
    Flink->>Flink: advance watermark (event_time − 5s)

    Note over Flink,TS: 5. Tumbling window jobs (run concurrently)

    par Trade tape job (04_trade_tape_job.sql)
        Flink->>TS: INSERT INTO analytics.fact_trades<br/>(batch: 500 rows / 2s flush)
        TS-->>Flink: OK
    and OHLCV job (02_ohlcv_job.sql)
        Flink->>Flink: TUMBLE(INTERVAL '1' MINUTE)<br/>FIRST_VALUE/LAST_VALUE open/close<br/>MAX/MIN high/low, SUM qty
        Flink->>TS: INSERT INTO analytics.fact_candles<br/>(timeframe=1m/5m/15m/1h, batch: 200 rows / 5s)
        TS-->>Flink: OK
    and Volume stats job (03_volume_stats_job.sql)
        Flink->>Flink: TUMBLE(INTERVAL '5' MINUTE)<br/>buy/sell volume, VWAP, trade_count
        Flink->>TS: INSERT INTO analytics.fact_volume_stats<br/>(window_secs=300, batch: 200 rows / 5s)
        TS-->>Flink: OK
    end

    Note over TS,MB: 6. Metabase queries TimescaleDB views

    Op->>MB: open dashboard
    MB->>TS: SELECT * FROM analytics.v_market_summary_24h
    TS-->>MB: result set
    MB-->>Op: render chart
```

---

## Latency Budget

| Stage | Latency | Notes |
|-------|---------|-------|
| Exchange → Consumer | ~1–10ms | Network + WebSocket parsing |
| Consumer → Kafka | ~1–5ms | Batch timeout 250ms max |
| Kafka → Flink poll | ~100–500ms | Poll interval |
| Flink watermark advance | 5 seconds | Fixed event-time slack |
| Flink window emission | 0–60s after window close | 1m window = up to 65s total |
| JDBC flush to TimescaleDB | 2–5s | 200–500 rows or timeout |
| Metabase query | ~10–500ms | View complexity |
| **Total end-to-end** | **~10–90 seconds** | For 1-minute candle to appear |

This is by design — the analytics path trades latency for queryability.

---

## Key Invariant: Best-Effort Semantics

Step 3 shows that Kafka publish failures are silently absorbed:
- Analytics pipeline can tolerate Kafka downtime without affecting the primary NATS path
- Some trades may be missing from Flink aggregations if Kafka was down during ingestion
- No exactly-once guarantee between Consumer and Kafka (at-most-once in practice)
- Flink uses `scan.startup.mode: latest-offset` — events before Flink connects are not replayed

---

## Related Diagrams

- C4 Analytics (`c4-analytics.md`) — container topology for this pipeline
- Live Data Ingestion (`sequence-live-ingestion.md`) — the primary NATS path (step 2 above)
- Storage Federation (`sequence-storage-federation.md`) — ClickHouse cold path (separate)
