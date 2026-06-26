# Sequence Diagram — Live Data Ingestion

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/subsystems.md`, `docs/contracts/event-bus.md`

---

## What this shows

The end-to-end happy path for live market data: from an exchange WebSocket message
arriving at the Consumer, through NATS JetStream, through the Processor's aggregation
pipeline, and finally to both storage and connected client sessions.

---

## Full Pipeline Sequence

```mermaid
sequenceDiagram
    autonumber

    participant Exch as Exchange<br/>(WebSocket Feed)
    participant WsMgr as ws.Manager<br/>(consumer)
    participant MDSub as MarketData Subsystem<br/>(consumer actor)
    participant JS as NATS JetStream<br/>(event bus)
    participant AggSub as Aggregation Subsystem<br/>(processor actor)
    participant InsSub as Insights Subsystem<br/>(processor actor)
    participant DelSub as Delivery Subsystem<br/>(server actor)
    participant StorSub as Storage Subsystem<br/>(store actor)
    participant TsDB as TimescaleDB<br/>(L1 hot)
    participant CH as ClickHouse<br/>(L2 cold)
    participant Client as Client<br/>(Odin cockpit)

    Note over Exch,WsMgr: Exchange pushes raw WebSocket message

    Exch->>WsMgr: raw WS frame (trade / bookdelta / markprice / liquidation)
    WsMgr->>MDSub: parsed raw payload (exchange-specific struct)

    Note over MDSub: Canonicalize → CMM, dedup, monotonic seq
    MDSub->>MDSub: canonicalize(raw) → CMM event
    MDSub->>MDSub: dedup check (idempotency_key)
    MDSub->>MDSub: assign monotonic seq + prev_seq

    alt duplicate detected
        MDSub-->>WsMgr: drop (increment dup_total counter)
    else canonical envelope ready
        MDSub->>JS: publish marketdata.{type}.v1<br/>(Envelope{seq, prev_seq, idempotency_key, payload})
        JS-->>MDSub: PubAck (sequence confirmed)
    end

    Note over JS,AggSub: JetStream delivers to durable consumer group

    JS->>AggSub: marketdata.{type}.v1 envelope
    par Aggregation parallel paths
        AggSub->>AggSub: build candle(s) [9 TF: 1s,5s,15s,1m,5m,15m,1h,4h,1d]
        AggSub->>AggSub: apply bookdelta → orderbook snapshot
        AggSub->>AggSub: update stats (OHLCV, cross-source metrics)
        AggSub->>AggSub: append to tape (trade print)
    end

    AggSub->>JS: publish aggregation.candle.v1
    AggSub->>JS: publish aggregation.snapshot.v1
    AggSub->>JS: publish aggregation.stats.v1
    AggSub->>JS: publish aggregation.tape.v1
    JS-->>AggSub: PubAck × 4

    JS->>InsSub: aggregation.{type}.v1
    par Insights parallel paths
        InsSub->>InsSub: accumulate heatmap bucket
        InsSub->>InsSub: update VPVR slice
    end
    InsSub->>JS: publish insights.heatmap.v1
    InsSub->>JS: publish insights.volume_profile.v1
    JS-->>InsSub: PubAck × 2

    Note over JS,DelSub: Delivery fan-out and storage write happen concurrently

    par Delivery + Storage fan-out
        JS->>DelSub: aggregation.*.v1 + insights.*.v1
        DelSub->>DelSub: route by session subscriptions
        DelSub->>Client: WebSocket push (Terminal_V1 envelope)
        Client-->>DelSub: (implicit — no ACK required for live stream)
    and
        JS->>StorSub: aggregation.*.v1
        StorSub->>StorSub: write L0 in-memory ring
        StorSub->>TsDB: upsert candle / snapshot / stats (async batch)
        TsDB-->>StorSub: OK
        StorSub->>CH: insert heatmap / VPVR (async batch)
        CH-->>StorSub: OK
    end

    AggSub-->>JS: ACK marketdata.{type}.v1
    InsSub-->>JS: ACK aggregation.{type}.v1
    StorSub-->>JS: ACK aggregation.{type}.v1
    DelSub-->>JS: ACK aggregation.{type}.v1 + insights.{type}.v1
```

---

## Key Invariants Illustrated

| # | Invariant | Where enforced |
|---|-----------|----------------|
| 1 | Every envelope carries `seq` + `prev_seq` — receivers detect gaps | `internal/shared/envelope` |
| 2 | `idempotency_key` prevents duplicate processing across restarts | `MDSub` dedup check (step 5) |
| 3 | ACK is sent only after successful processing — NAK triggers redelivery | `internal/adapters/jetstream/ingest_policy.go:59` |
| 4 | Delivery and Storage are independent JetStream consumers — one slow writer cannot block the other | Container isolation in docker-compose |
| 5 | L0 write is synchronous; L1/L2 writes are async batched — latency budget preserved | `internal/adapters/storage/federation/merge.go` |

---

## Backpressure Paths

- **Consumer:** `ws.Manager` drops messages when `MaxEntries=20_000` canonical state cap is reached (`ws_backpressure_drops_total` counter).
- **Delivery:** Per-session bounded queue; slow clients receive NACK and the session triggers resync.
- **Insights (VPVR):** `vpvr_overload_policy.go` sheds load when budget is exceeded without blocking the aggregation path.

---

## Related Diagrams

- [Client Session Protocol](sequence-client-session.md) — how the client receives the events pushed in step 16
- [Storage Federation Write Path](sequence-storage-federation.md) — steps 18–21 in detail
- [Exchange Reconnect & Recovery](sequence-exchange-recovery.md) — what happens when step 1 fails
