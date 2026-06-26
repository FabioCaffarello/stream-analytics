# Sequence Diagram — Evidence Detection (Liquidity Evidence Layer)

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/contracts/liquidity-evidence-layer.md`, `docs/architecture/insights.md`
**Code anchor:** `internal/core/evidence/`, `internal/actors/evidence/runtime/`

---

## What this shows

How the Liquidity Evidence Layer (LEL) detects stateful liquidity signals from aggregated
market data. Includes multi-replica ownership via hash-based shard assignment to prevent
duplicate evidence publishing across processor instances.

---

## Evidence Detection Sequence

```mermaid
sequenceDiagram
    autonumber

    participant JS as NATS JetStream
    participant EvAct as Evidence Subsystem<br/>(processor actor)
    participant ShardReg as ShardRegistry<br/>(hash-based ownership)
    participant LEL as LEL Engine<br/>(5 stateful rules)
    participant State as Evidence State<br/>(per-stream, in-memory)
    participant JS2 as NATS JetStream<br/>(publish side)
    participant DelSub as Delivery Subsystem<br/>(server)
    participant Client as Client

    Note over JS,EvAct: Evidence actor subscribes to aggregated data streams

    JS->>EvAct: aggregation.snapshot.v1<br/>(orderbook snapshot for stream S)
    EvAct->>ShardReg: isOwner(stream=S, replicaID=self)

    alt not owner of this stream shard
        ShardReg-->>EvAct: false
        EvAct-->>JS: ACK (skip, another replica owns this shard)
    else this replica owns stream S
        ShardReg-->>EvAct: true

        EvAct->>State: load evidence state for stream S
        State-->>EvAct: EvidenceState{rules: [R1..R5], last_seq}

        Note over EvAct,LEL: Run all 5 LEL rules against current snapshot

        EvAct->>LEL: evaluate(snapshot, state)

        par Rule evaluation (all 5 concurrent)
            LEL->>LEL: R1 — Iceberg detection<br/>(large hidden orders behind small quotes)
            LEL->>LEL: R2 — Stack absorption<br/>(repeated large fills at same level)
            LEL->>LEL: R3 — Bid/Ask sweep<br/>(aggressive sweep through multiple levels)
            LEL->>LEL: R4 — Spoofing signal<br/>(large order placed + cancelled < threshold_ms)
            LEL->>LEL: R5 — Price magnet<br/>(price gravitating toward large passive cluster)
        end

        LEL-->>EvAct: RuleResults{triggered: [R1, R3], evidence_score: 0.82}

        alt no rules triggered
            EvAct->>State: update(last_seq=snapshot.seq, no_evidence=true)
            EvAct-->>JS: ACK
        else evidence triggered
            EvAct->>State: update(last_seq=snapshot.seq, evidence=RuleResults)
            EvAct->>JS2: publish liquidity.evidence.v1<br/>Envelope{stream=S, triggered_rules=[R1,R3],<br/>evidence_score=0.82, seq, prev_seq}
            JS2-->>EvAct: PubAck

            EvAct-->>JS: ACK

            Note over JS2,Client: Evidence delivered to cockpit via Delivery

            JS2->>DelSub: liquidity.evidence.v1
            DelSub->>DelSub: route to sessions subscribed to stream S
            DelSub->>Client: WebSocket push evidence envelope
        end
    end
```

---

## Multi-Replica Ownership (ShardRegistry)

The Evidence subsystem can run across multiple `cmd/processor` replicas. To prevent
duplicate evidence publishing for the same stream, each replica consults the `ShardRegistry`:

```mermaid
flowchart LR
    Stream["stream key<br/>e.g. BINANCE.BTCUSDT.USD_M"]
    Hash["FNV-1a hash<br/>(zero-alloc)"]
    Mod["hash % num_replicas"]
    Owner["ownerID"]
    Self["self.replicaID"]
    Decision{{"equal?"}}
    Process["process + publish"]
    Skip["skip (ACK only)"]

    Stream --> Hash --> Mod --> Owner --> Decision
    Self --> Decision
    Decision -->|yes| Process
    Decision -->|no| Skip
```

Code: `internal/shared/hash/` (FNV-1a), `internal/shared/shardregistry/`

---

## LEL Rules Reference

| Rule | Signal | State required |
|------|--------|----------------|
| R1 — Iceberg | Large hidden order behind thin quote | Last N snapshots at level |
| R2 — Stack absorption | Repeated large aggressor fills at same price | Cumulative fill tracker |
| R3 — Bid/Ask sweep | Aggressive sweep through ≥3 consecutive levels | Level-by-level fill history |
| R4 — Spoofing | Order placed and cancelled within threshold_ms | Pending order timestamps |
| R5 — Price magnet | Price movement toward a dominant passive cluster | Rolling price + OB cluster |

Authoritative contract: [`docs/contracts/liquidity-evidence-layer.md`](../../contracts/liquidity-evidence-layer.md)

---

## Evidence Score

`evidence_score` is a composite `[0.0, 1.0]` value derived from:
- Number of rules triggered
- Confidence weight per rule
- Recency of corroborating signals

The cockpit displays evidence overlays when `evidence_score > threshold` (configurable per operator workspace).

---

## Related Diagrams

- [Live Data Ingestion](sequence-live-ingestion.md) — how aggregation.snapshot.v1 reaches the Evidence actor
- [Client Session Protocol](sequence-client-session.md) — how the client receives liquidity.evidence.v1 envelopes
