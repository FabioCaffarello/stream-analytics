# Sequence Diagram — Storage Federation Write & Read Path

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `docs/architecture/storage.md`, `docs/architecture/diagrams/c4-containers.md`
**Code anchor:** `internal/adapters/storage/federation/merge.go`, `internal/adapters/storage/federation/candle_reader.go`

---

## What this shows

How the Storage subsystem receives aggregated envelopes from JetStream and fans them out
across three tiers (L0 → L1 → L2), and how federated reads merge results across tiers
when the server serves a historical range query or backfill.

---

## Write Path — Tier Fan-out

```mermaid
sequenceDiagram
    autonumber

    participant JS as NATS JetStream
    participant StorAct as Storage Actor<br/>(cmd/store)
    participant L0 as L0 — In-Memory Ring<br/>(bounded, recent window)
    participant L1 as L1 — TimescaleDB<br/>(hot, 7-day rolling)
    participant L2 as L2 — ClickHouse<br/>(cold, long-term archive)

    JS->>StorAct: aggregation.candle.v1 envelope
    StorAct->>StorAct: parse + validate envelope

    Note over StorAct,L0: L0 write is synchronous — always first

    StorAct->>L0: write(stream, envelope)
    L0-->>StorAct: OK (ring-buffer slot allocated)

    Note over StorAct,L1: L1 and L2 writes are async batched — do not block

    par Async writes (fire-and-flush cycle)
        StorAct->>L1: batch.append(candle row)
        Note over L1: Flush when batch_size=1000<br/>or flush_interval=500ms
        L1->>L1: upsert INTO candles ON CONFLICT DO UPDATE
        L1-->>StorAct: flush OK
    and
        StorAct->>L2: batch.append(candle row)
        Note over L2: Flush when batch_size=5000<br/>or flush_interval=2s
        L2->>L2: INSERT INTO candles (append-only, no conflict)
        L2-->>StorAct: flush OK
    end

    StorAct-->>JS: ACK envelope

    Note over StorAct,L2: Heatmap and VPVR go only to L2 (analytical, no hot path needed)

    JS->>StorAct: insights.heatmap.v1 envelope
    StorAct->>L2: batch.append(heatmap_bucket row)
    L2-->>StorAct: (async, batched flush)
    StorAct-->>JS: ACK envelope
```

---

## Read Path — Federated Range Query

```mermaid
sequenceDiagram
    autonumber

    participant Caller as Caller<br/>(Backfill Service or HTTP handler)
    participant Fed as Federation Layer<br/>(internal/adapters/storage/federation)
    participant L0 as L0 — In-Memory Ring
    participant L1 as L1 — TimescaleDB
    participant L2 as L2 — ClickHouse

    Caller->>Fed: rangeQuery(stream, from=T-24h, to=T)

    Note over Fed: Federation determines tier coverage by time range

    Fed->>Fed: classify range:
    Note over Fed: T - retention_L0 → now  = L0 coverage<br/>T - 7d → T - retention_L0  = L1 coverage<br/>T_archive → T - 7d          = L2 coverage

    par Parallel tier reads
        Fed->>L0: read(stream, max(from, T-retention_L0), to)
        L0-->>Fed: envelopes[] (sorted by seq, may be empty for old ranges)
    and
        Fed->>L1: SELECT candles WHERE time BETWEEN ... ORDER BY time
        L1-->>Fed: rows[]
    and
        Fed->>L2: SELECT candles WHERE time BETWEEN ... ORDER BY time
        L2-->>Fed: rows[]
    end

    Note over Fed: Merge: deduplicate by (stream, seq), sort by time, fill gaps

    Fed->>Fed: merge(L0_results, L1_results, L2_results)
    Fed->>Fed: dedup by idempotency_key
    Fed->>Fed: sort ascending by open_time

    Fed-->>Caller: merged envelopes[] (gapless, deduplicated)
```

---

## Retention & Coverage Rules

| Tier | Technology | Typical retention | Written by | Read by |
|------|------------|-------------------|------------|---------|
| L0 | In-memory ring buffer | ~5 min (bounded slots) | Storage actor (sync) | Federation, Backfill |
| L1 | TimescaleDB | 7 days rolling | Storage actor (async batch) | Federation, HTTP /api/v1/* |
| L2 | ClickHouse | Indefinite (archive) | Storage actor (async batch) | Federation, HTTP /api/v1/* |

Heatmap and VPVR data skip L0 and L1 — they land only in L2 due to their analytical nature and large row size.

---

## Idempotency on Write

TimescaleDB upserts use `ON CONFLICT (stream_id, open_time, timeframe) DO UPDATE` to
handle redelivered envelopes after a consumer restart. ClickHouse uses append-only inserts;
deduplication on read is handled by the federation merge layer.

Code: `internal/adapters/storage/timescale/candle_reader.go:1`
Test: `internal/adapters/storage/federation/consistency_test.go:1`

---

## Related Diagrams

- Live Data Ingestion (`sequence-live-ingestion.md`) — the producer side that feeds StorAct (steps 18–21)
- Client Session Protocol (`sequence-client-session.md`) — how the Backfill Service uses rangeQuery (step 11)
