# Actor Supervision Tree

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/subsystems.md`, `docs/architecture/diagrams/c4-containers.md`
**Code anchor:** `internal/actors/runtime/guardian.go`

---

## What this shows

The Hollywood actor supervision tree for each binary. The Guardian orchestrates subsystem actors
with exponential backoff and circuit-breaking. Actors coordinate; use cases decide; domain enforces.

---

## Supervision Policy (all binaries)

```
BaseBackoff:     250ms
MaxBackoff:      5s
RestartWindow:   30s
RestartLimit:    5 per window
GlobalLimit:     5 restarts/min  (circuit breaker)
```

Code: `internal/actors/runtime/guardian.go:273`

---

## cmd/consumer — MarketData Pipeline

```mermaid
graph TD
    Engine["Engine<br/>(Hollywood runtime)"]
    Guardian["Guardian<br/>actor supervisor"]
    MD["MarketData<br/>SubsystemMarketData<br/>internal/actors/marketdata/runtime"]
    WsPool["ws.Manager<br/>WebSocket pool<br/>per exchange bucket"]
    BinSpot["Binance Spot<br/>adapter"]
    BinFut["Binance Futures<br/>adapter"]
    Bybit["Bybit<br/>adapter"]
    Coinbase["Coinbase<br/>adapter"]
    HL["HyperLiquid<br/>adapter"]
    Kraken["Kraken Spot<br/>adapter"]
    KrakenF["Kraken Futures<br/>adapter"]
    JetPub["JetStream Publisher<br/>marketdata.*.v1"]

    Engine --> Guardian
    Guardian -->|"supervises"| MD
    MD --> WsPool
    WsPool --> BinSpot
    WsPool --> BinFut
    WsPool --> Bybit
    WsPool --> Coinbase
    WsPool --> HL
    WsPool --> Kraken
    WsPool --> KrakenF
    MD --> JetPub

    style Engine fill:#1a1a2e,color:#eee,stroke:#444
    style Guardian fill:#16213e,color:#eee,stroke:#0f3460
    style MD fill:#0f3460,color:#eee,stroke:#e94560
    style WsPool fill:#1a1a2e,color:#ccc,stroke:#444
    style JetPub fill:#1a1a2e,color:#ccc,stroke:#444
```

> **Note:** Dynamic per-exchange actors use key `marketdata:{exchange}` and bypass the static
> `SubsystemMarketData` slot when present (`guardian.go:603-616`).

---

## cmd/processor — Aggregation + Insights + Evidence

```mermaid
graph TD
    Engine["Engine<br/>(Hollywood runtime)"]
    Guardian["Guardian<br/>actor supervisor"]

    Agg["Aggregation<br/>SubsystemAggregation<br/>internal/actors/aggregation/runtime"]
    Ins["Insights<br/>SubsystemInsights<br/>internal/actors/insights/runtime"]
    Ev["Evidence<br/>SubsystemEvidence<br/>internal/actors/evidence/runtime"]

    JetCon["JetStream Consumer<br/>marketdata.*.v1"]

    Candles["Candle Builder<br/>9 timeframes<br/>(1s→1M)"]
    OB["Orderbook Processor<br/>snapshot + delta apply"]
    Stats["Stats Builder<br/>OHLCV + cross-source"]
    Tape["Tape Builder<br/>trade prints"]
    Heatmap["Heatmap Builder<br/>liquidity zones"]
    VPVR["VPVR Builder<br/>volume profile"]

    LEL["LEL Engine<br/>5 stateful rules"]
    ShardReg["ShardRegistry<br/>hash-based replica<br/>ownership"]

    JetPub["JetStream Publisher<br/>aggregation.*.v1<br/>insights.*.v1<br/>liquidity.evidence.v1"]

    Engine --> Guardian
    Guardian -->|"supervises"| Agg
    Guardian -->|"supervises"| Ins
    Guardian -->|"supervises"| Ev

    JetCon --> Agg
    Agg --> Candles
    Agg --> OB
    Agg --> Stats
    Agg --> Tape
    Agg --> JetPub

    JetCon --> Ins
    Ins --> Heatmap
    Ins --> VPVR
    Ins --> JetPub

    JetCon --> Ev
    Ev --> LEL
    Ev --> ShardReg
    Ev --> JetPub

    style Engine fill:#1a1a2e,color:#eee,stroke:#444
    style Guardian fill:#16213e,color:#eee,stroke:#0f3460
    style Agg fill:#0f3460,color:#eee,stroke:#e94560
    style Ins fill:#0f3460,color:#eee,stroke:#e94560
    style Ev fill:#0f3460,color:#eee,stroke:#e94560
```

---

## cmd/server — Delivery Gateway

```mermaid
graph TD
    Engine["Engine<br/>(Hollywood runtime)"]
    Guardian["Guardian<br/>actor supervisor"]

    Del["Delivery<br/>SubsystemDelivery<br/>internal/actors/delivery/runtime"]

    Router["Router<br/>envelope fan-out<br/>per subscription"]
    Sessions["WS Sessions<br/>per-client actors<br/>Terminal_V1 protocol"]
    HelloGate["Hello Gate<br/>capability negotiation<br/>clock skew validation"]
    BP["Backpressure<br/>per-session bounded queue"]
    BF["Backfill<br/>fetch from storage<br/>on subscribe"]

    HTTP["HTTP Handler<br/>/api/v1/* cold reads<br/>/metrics Prometheus"]
    JetCon["JetStream Consumer<br/>aggregation.*.v1<br/>liquidity.evidence.v1"]

    Engine --> Guardian
    Guardian -->|"supervises"| Del

    JetCon --> Del
    Del --> Router
    Router --> Sessions
    Sessions --> HelloGate
    Sessions --> BP
    Sessions --> BF
    Del --> HTTP

    style Engine fill:#1a1a2e,color:#eee,stroke:#444
    style Guardian fill:#16213e,color:#eee,stroke:#0f3460
    style Del fill:#0f3460,color:#eee,stroke:#e94560
    style Sessions fill:#1a1a2e,color:#ccc,stroke:#444
```

---

## cmd/store — Storage Lifecycle

```mermaid
graph TD
    Engine["Engine<br/>(Hollywood runtime)"]
    Guardian["Guardian<br/>actor supervisor"]

    Stor["Storage<br/>SubsystemStorage<br/>internal/adapters/storage"]

    JetCon["JetStream Consumer<br/>aggregation.*.v1"]

    L0["L0 — In-memory ring<br/>ultra-low latency<br/>recent window"]
    L1["L1 — TimescaleDB<br/>hot store 7-day<br/>candles, OB, stats, tape"]
    L2["L2 — ClickHouse<br/>cold archive<br/>heatmap, VPVR, compressed history"]
    Fed["Federation Layer<br/>merge + range query<br/>across tiers"]

    Engine --> Guardian
    Guardian -->|"supervises"| Stor

    JetCon --> Stor
    Stor --> L0
    Stor --> L1
    Stor --> L2
    L0 --> Fed
    L1 --> Fed
    L2 --> Fed

    style Engine fill:#1a1a2e,color:#eee,stroke:#444
    style Guardian fill:#16213e,color:#eee,stroke:#0f3460
    style Stor fill:#0f3460,color:#eee,stroke:#e94560
    style L0 fill:#1a3a1a,color:#ccc,stroke:#4caf50
    style L1 fill:#1a2a3a,color:#ccc,stroke:#2196f3
    style L2 fill:#2a1a3a,color:#ccc,stroke:#9c27b0
    style Fed fill:#3a2a1a,color:#ccc,stroke:#ff9800
```
