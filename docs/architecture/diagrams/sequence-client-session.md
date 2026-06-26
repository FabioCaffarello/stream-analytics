# Sequence Diagram — Client Session Protocol (Terminal_V1)

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/contracts/delivery-ws.md`, `docs/architecture/diagrams/sequence-live-ingestion.md`
**Code anchor:** `internal/actors/delivery/runtime/session_protocol.go`, `internal/actors/delivery/runtime/session_commands.go`

---

## What this shows

The full lifecycle of a client WebSocket session using the Terminal_V1 protocol:
connection, Hello handshake, subscription, backfill from storage, live streaming,
and the Resync flow that restores coherence after a gap.

---

## Session Lifecycle

```mermaid
sequenceDiagram
    autonumber

    participant Client as Client<br/>(Odin cockpit)
    participant WsH as WebSocket Handler<br/>(server /ws endpoint)
    participant DelSub as Delivery Subsystem<br/>(server actor)
    participant Router as Router<br/>(envelope fan-out)
    participant BF as Backfill Service<br/>(cold + hot reads)
    participant StorFed as Storage Federation<br/>(L0/L1/L2)
    participant JS as NATS JetStream

    Note over Client,WsH: 1. Connection establishment

    Client->>WsH: HTTP GET /ws (Upgrade: websocket)
    WsH-->>Client: 101 Switching Protocols
    WsH->>DelSub: SessionOpen(sessionID)
    DelSub->>DelSub: allocate session state<br/>start Hello gate timer

    Note over Client,DelSub: 2. Hello handshake — must complete before any subscribe

    Client->>WsH: Hello{version: "Terminal_V1",<br/>capabilities: [...],<br/>client_clock_ms: T_client}
    WsH->>DelSub: Hello message

    DelSub->>DelSub: validate version + capabilities
    DelSub->>DelSub: compute clock_skew = T_server - T_client
    alt clock_skew > threshold
        DelSub-->>Client: Error{code: CLOCK_SKEW, detail: "..."}
        DelSub->>DelSub: close session
    else handshake OK
        DelSub-->>Client: HelloAck{session_id, server_clock_ms,<br/>clock_skew_ms, capabilities_granted}
        DelSub->>DelSub: open Hello gate — subscriptions now allowed
    end

    Note over Client,Router: 3. Stream subscription + backfill

    Client->>WsH: Subscribe{streams: ["candle.BINANCE.BTCUSDT.1m",<br/>"orderbook.BINANCE.BTCUSDT",<br/>"heatmap.BINANCE.BTCUSDT"]}
    WsH->>DelSub: Subscribe message

    DelSub->>Router: register(sessionID, streams)

    Note over BF,StorFed: Backfill requested for each subscribed stream

    par Backfill per stream (concurrent)
        DelSub->>BF: backfill(stream="candle.BINANCE.BTCUSDT.1m", from=T-limit)
        BF->>StorFed: rangeQuery(stream, from, to)
        StorFed->>StorFed: merge L0 + L1 + L2 results
        StorFed-->>BF: historical envelopes[]
        BF-->>Client: WebSocket push BackfillBatch{stream, envelopes[]}
    and
        DelSub->>BF: backfill(stream="orderbook.BINANCE.BTCUSDT", ...)
        BF->>StorFed: lastSnapshot(stream)
        StorFed-->>BF: snapshot envelope
        BF-->>Client: WebSocket push BackfillBatch{stream, snapshot}
    end

    DelSub-->>Client: SubscribeAck{streams: [...], watermarks: {stream → last_seq}}

    Note over Router,Client: 4. Live streaming begins (after backfill)

    JS->>Router: aggregation.candle.v1 (new candle event)
    Router->>Router: filter: which sessions subscribed to this stream?
    Router->>Client: WebSocket push Envelope{seq, prev_seq, payload}

    JS->>Router: aggregation.snapshot.v1
    Router->>Client: WebSocket push Envelope{seq, prev_seq, payload}

    Note over Client,Router: 5. Client detects gap (seq != prev_seq + 1)

    Client->>Client: gap detected: expected seq=105, received seq=108
    Client->>WsH: Resync{stream: "candle.BINANCE.BTCUSDT.1m",<br/>from_seq: 105}
    WsH->>DelSub: Resync message

    DelSub->>BF: backfill(stream, from_seq=105, to_seq=107)
    BF->>StorFed: rangeQuery(stream, seq_range=[105,107])
    StorFed-->>BF: gap envelopes[105, 106, 107]
    BF-->>Client: WebSocket push ResyncBatch{stream, envelopes[105..107], watermark=107}
    DelSub-->>Client: ResyncAck{stream, watermark: last_seq=107}

    Note over Client: Client merges gap and resumes from seq=108

    Note over Client,WsH: 6. Session teardown

    Client->>WsH: Close (WebSocket close frame)
    WsH->>DelSub: SessionClose(sessionID)
    DelSub->>Router: deregister(sessionID)
    DelSub->>DelSub: release session state
```

---

## Hello Gate Invariant

The Hello gate is a mandatory pre-condition: **no Subscribe or data message is processed until
HelloAck is sent**. Any message arriving before Hello completes is rejected with `PROTOCOL_VIOLATION`.

Code: `internal/actors/delivery/runtime/session_protocol.go:requireHelloGate`
Test: `internal/actors/delivery/runtime/session_protocol_contract_test.go`

---

## Prev_Seq Chain Invariant

Every envelope delivered over WebSocket carries:
- `seq` — monotonically increasing per stream
- `prev_seq` — the seq of the immediately preceding envelope

The client uses `prev_seq` to detect gaps without needing a heartbeat. A gap triggers Resync.

Test: `TestProtocol_PrevSeqChain_MonotonicAcrossEvents`

---

## Backpressure on Slow Clients

If the session's per-client bounded queue fills (slow network / frozen client):
1. Router drops the envelope and increments `session_drop_total`.
2. After N consecutive drops, Delivery marks the session as `lagging`.
3. A lagging session receives a `Resync` signal automatically on reconnect.

See [`docs/architecture/subsystems.md`](../subsystems.md) — Delivery section for bounds.

---

## Related Diagrams

- [Live Data Ingestion](sequence-live-ingestion.md) — how events arrive at Router (step 12 above)
- [Storage Federation Write Path](sequence-storage-federation.md) — how StorFed serves backfill queries
