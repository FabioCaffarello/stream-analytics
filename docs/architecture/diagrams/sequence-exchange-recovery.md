# Sequence Diagram — Exchange Reconnect & Recovery

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/subsystems.md`, `docs/architecture/sequencing-model.md`
**Code anchor:** `internal/actors/marketdata/ws/manager.go`, `internal/actors/runtime/guardian.go`

---

## What this shows

What happens when an exchange WebSocket connection drops: the reconnect cycle with
exponential backoff, the gap detection on resume, and how the sequencing model
ensures downstream consistency despite the interruption.

---

## Disconnect, Reconnect & Gap Recovery

```mermaid
sequenceDiagram
    autonumber

    participant Exch as Exchange<br/>(WebSocket Feed)
    participant WsMgr as ws.Manager<br/>(consumer)
    participant MDSub as MarketData Subsystem<br/>(consumer actor)
    participant Guardian as Guardian<br/>(supervision)
    participant JS as NATS JetStream
    participant AggSub as Aggregation Subsystem<br/>(processor)
    participant BF as Exchange Backfill<br/>(REST API)

    Note over Exch,WsMgr: Normal operation — frames arriving

    Exch->>WsMgr: trade frame (seq_remote=N)
    WsMgr->>MDSub: parsed payload
    MDSub->>JS: publish marketdata.trade.v1 {seq=K}

    Note over Exch,WsMgr: Connection drop (network error / exchange maintenance)

    Exch-xWsMgr: WebSocket error / EOF
    WsMgr->>WsMgr: connection_error_total++
    WsMgr->>MDSub: ConnectionDown(exchange, reason)
    MDSub->>MDSub: mark stream state = Disconnected

    Note over MDSub,Guardian: Guardian supervision: actor does NOT crash on disconnect

    MDSub->>Guardian: notify: stream_down(exchange)
    Guardian->>Guardian: record event (does not restart unless actor panics)

    Note over WsMgr: Exponential backoff reconnect loop

    loop until reconnected
        WsMgr->>WsMgr: sleep(backoff) — BaseBackoff=250ms, MaxBackoff=5s
        WsMgr->>Exch: WebSocket dial attempt
        alt dial failed
            Exch--xWsMgr: connection refused / timeout
            WsMgr->>WsMgr: backoff * 2 (capped at MaxBackoff)
        else connected
            Exch-->>WsMgr: 101 Switching Protocols
        end
    end

    WsMgr->>Exch: subscribe(channels: [trades, bookdelta, markprice, ...])
    Exch-->>WsMgr: subscribed ACK

    MDSub->>MDSub: mark stream state = Reconnecting

    Note over MDSub,BF: Gap recovery via exchange REST backfill API

    MDSub->>BF: backfill(exchange, from=last_received_remote_seq, to=now)
    BF->>Exch: REST GET /api/v3/historicalTrades?from=N&limit=1000
    Exch-->>BF: historical trade records [N+1 .. M]
    BF-->>MDSub: backfill events [N+1 .. M]

    loop for each backfill event
        MDSub->>MDSub: canonicalize(event) → CMM
        MDSub->>MDSub: dedup check (idempotency_key)
        MDSub->>JS: publish marketdata.trade.v1 {seq=K+i, prev_seq=K+i-1}
    end

    MDSub->>MDSub: mark stream state = Live (gap closed)
    MDSub->>Guardian: notify: stream_up(exchange)

    Note over WsMgr,Exch: Resume live WebSocket frames

    Exch->>WsMgr: trade frame (seq_remote=M+1)
    WsMgr->>MDSub: parsed payload
    MDSub->>JS: publish marketdata.trade.v1 {seq=K+gap_size+1}

    Note over AggSub: Aggregation sees continuous seq chain — no gap visible

    JS->>AggSub: marketdata.trade.v1 {seq=K+1}
    JS->>AggSub: marketdata.trade.v1 {seq=K+2}
    Note over AggSub: prev_seq chain is intact — Aggregation processes normally
```

---

## Stream State Machine (MarketData)

```mermaid
stateDiagram-v2
    [*] --> Connecting : startup

    Connecting --> Live : connected + subscription ACK
    Live --> Disconnected : connection error / EOF
    Disconnected --> Reconnecting : backoff elapsed, dial succeeded
    Reconnecting --> GapFilling : subscribed + backfill requested
    GapFilling --> Live : backfill complete, seq chain intact
    Reconnecting --> Disconnected : dial failed (retry loop)

    Live --> [*] : process shutdown
```

---

## Client-Side Impact

From the client cockpit perspective, a brief exchange disconnect is transparent if:
- Backfill covers the gap completely
- The `prev_seq` chain remains unbroken in the envelopes the client receives

If the gap is larger than the backfill window (e.g., exchange was down for hours),
the Aggregation subsystem publishes a `stream_gap` signal and the Delivery subsystem
triggers a Resync for affected client sessions.

---

## Key Counters (Prometheus)

| Metric | What it tracks |
|--------|----------------|
| `ws_connection_errors_total{exchange}` | Total WebSocket errors per exchange |
| `ws_reconnect_attempts_total{exchange}` | Reconnect attempts (backoff cycles) |
| `ws_backfill_events_total{exchange}` | Events recovered via REST backfill |
| `ws_gap_total{exchange}` | Unrecoverable gaps (backfill window exhausted) |

---

## Related Diagrams

- [Live Data Ingestion](sequence-live-ingestion.md) — the normal path this sequence restores
- [Actor Supervision Tree](actor-supervision-tree.md) — how Guardian fits into the recovery model
