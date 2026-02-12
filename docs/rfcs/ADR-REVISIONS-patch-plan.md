# ADR Revisions — Surgical Patch Plan

**Date:** 2026-02-12
**Context:** PRD-0001 section C.1 identified 6 existing ADRs with gaps.
**Rule:** Existing ADRs are amended in-place (append-only). No section is removed. New content is added under `## Amendment` sections with date tags.

---

## ADR-0002 — Envelope Design & Versioning

### What's Missing

1. No wire format strategy (JSON vs protobuf vs CBOR)
2. No schema registry or consumer schema discovery mechanism
3. No compatibility rules (field reservation, field reuse prohibition)
4. `Meta map[string]string` is ad-hoc — no standardized fields

### Patch (append after Consequences)

```markdown
## Amendment — 2026-02-12

### Wire Format Strategy

Envelope supports multiple wire formats via `ContentType` field (ADR-0016):
- `"application/json"` (default, backward compatible)
- `"application/protobuf"` (opt-in, Phase 2)

Codec auto-detects format based on ContentType.

### Schema Discovery

Schemas are defined in `proto/` directory with Buf toolchain (ADR-0016).
Schema registry lite: `proto/registry.json` maps `(type, version)` → proto file.
No external registry service needed.

### Compatibility Rules

1. Field numbers are never reused (`reserved` directive for removed fields)
2. Field types are never changed
3. New fields are optional only
4. `oneof` groups are never extended
5. `buf breaking` enforces in CI on every PR

### Standardized Meta Fields

| Key | Description | Set By |
|-----|-------------|--------|
| `trace_id` | Distributed trace ID (future) | Ingest adapter |
| `source_stream` | WS stream name that produced this event | MarketDataSubsystem |
| `market_type` | SPOT, USD_M_FUTURES, COIN_M_FUTURES | InstrumentCatalog |
| `content_type` | Wire format of payload | Publisher |

See: ADR-0016, RFC-0007 (W6)
```

---

## ADR-0003 — Actor Runtime (Hollywood)

### What's Missing

1. No lifecycle guarantees documentation (actor.Stopped delivery order)
2. No `engine.Request()` pattern documentation
3. No generation counter documentation for stale retry prevention
4. No max restart limits documentation

### Patch (append after Consequences)

```markdown
## Amendment — 2026-02-12

### Lifecycle Guarantees (confirmed via Hollywood source)

1. `actor.Stopped` is delivered to the actor itself when it or its parent is poisoned
2. Child actors receive `actor.Stopped` BEFORE parent — bottom-up teardown
3. `e.Poison(pid).Done()` returns a channel that closes when the actor AND all children stop
4. New actor instance's `actor.Started` is delivered AFTER old instance's `actor.Stopped`
5. `actor.Stopped` handler MUST release all resources (close connections, cancel contexts, stop timers)

### Request/Reply Pattern

`engine.Request(pid, msg)` sends a message and waits for a response:
- The receiving actor uses `c.Respond(response)` or sends to `c.Sender()`
- For messages with `ReplyTo *actor.PID` field: handler checks `ReplyTo != nil`, falls back to `c.Sender()`
- This enables both `engine.Request()` (where ReplyTo is nil, Sender is the requester) and explicit `ReplyTo` routing

Example: Snapshot handler in Guardian:
```go
func (g *Guardian) handleSnapshot(msg Snapshot, c actor.Context) {
    target := msg.ReplyTo
    if target == nil {
        target = c.Sender()
    }
    c.Engine().Send(target, g.buildSnapshotResponse())
}
```

### Generation Counter for Stale Retry Prevention

Guardian tracks `generation` per subsystem. Incremented on each restart. When a scheduled retry fires, it checks:
```go
if msg.Generation != g.currentGeneration[subsystem] {
    return // stale retry, subsystem already restarted
}
```

This prevents applying a retry decision that was computed for a previous instance.

### SupervisorPolicy Integration

`SupervisorPolicy` defines restart behavior per subsystem:
- `MaxFailures`: failures allowed within `Window` before entering degraded mode
- `Cooldown`: duration to wait in degraded mode before attempting restart
- `Backoff`: exponential backoff with jitter between retry attempts
- Global restart rate limiter (ADR-0018): max N restarts per minute across ALL subsystems

See: ADR-0012 (Lifecycle Invariants), ADR-0018 (Topology)
```

---

## ADR-0004 — NATS JetStream

### What's Missing

1. No concrete subject hierarchy
2. No consumer config (MaxAckPending, DeliverPolicy, AckPolicy)
3. No dedup window configuration
4. No stream retention policy details

### Patch (append after Consequences)

```markdown
## Amendment — 2026-02-12

### Subject Hierarchy (ADR-0014)

```
{context}.{event_type}.v{version}.{venue_lower}.{instrument}

Examples:
  marketdata.trade.v1.binance.BTCUSDT
  marketdata.bookdelta.v1.binance.ETHUSDT
  aggregation.snapshot.v1.binance.BTCUSDT
```

### Stream Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Stream name | `MARKETDATA` | Single stream for all market data events |
| Subjects | `["marketdata.>"]` | Wildcard captures all market data |
| Retention | `LimitsPolicy` | Bounded by time + size |
| MaxAge | `24h` | Rolling window of recent data |
| MaxBytes | `10GB` | Hard disk limit |
| Storage | `FileStorage` | Persistence across restarts |
| Replicas | `1` | Single node (initial deployment) |
| DedupWindow | `5m` | Prevents duplicate publish on subsystem restart |

### Consumer Configuration

| Setting | Value | Rationale |
|---------|-------|-----------|
| Durable name | `raccoon-{subsystem}-v1` | Survives consumer restart |
| DeliverPolicy | `DeliverAll` (first time) / `DeliverLast` (recovery) | Full history or latest |
| AckPolicy | `AckExplicit` | Consumer controls acknowledgment |
| MaxAckPending | `1` (ordered) or `256` (parallel) | Ordering vs throughput |
| AckWait | `30s` | Timeout before redelivery |
| FilterSubject | Per-consumer wildcard | Route events to correct consumer |

### Dedup Strategy

- Publisher sets `Nats-Msg-Id` header = `envelope.IdempotencyKey`
- JetStream dedup window (5 min) rejects duplicate Msg-ID
- Consumer-side idempotency check as defense in depth

See: ADR-0014 (Stream Partitioning), RFC-0008 (W7)
```

---

## ADR-0005 — Sequencing Strategy

### What's Missing

1. No persistent sequencer strategy
2. No interaction with JetStream sequence numbers
3. No replay interaction defined

### Patch (append after Consequences)

```markdown
## Amendment — 2026-02-12

### Sequence Authorities

Two independent sequence domains:

| Sequence | Authority | Scope | Persisted |
|----------|-----------|-------|-----------|
| `envelope.Seq` | Application sequencer | Per (venue, instrument) | In-memory (lost on restart) |
| JetStream seq | NATS server | Per stream | Durable (JetStream storage) |

These are independent. `envelope.Seq` is the domain-level ordering guarantee. JetStream seq is the transport-level ordering.

On subsystem restart, `envelope.Seq` resets to 0. This is acceptable because:
- JetStream dedup prevents duplicate publish (same IdempotencyKey)
- Consumers use `envelope.Seq` for gap detection within a session
- Cross-session continuity is provided by JetStream durable consumer offset

### Replay Interaction (ADR-0015)

In replay mode:
- `ReplaySequencer` returns `envelope.Seq` values from the fixture file
- Domain logic sees the same seq values as during recording
- This ensures deterministic gap detection, dedup, and ordering

In live mode:
- `AtomicSequencer` (current) generates monotonic seq per stream
- On restart, seq resets — consumers must handle seq reset gracefully

### Future: Persistent Sequencer

If seq continuity across restarts becomes required:
- Store `lastSeq` per (venue, instrument) in embedded KV (bbolt) or JetStream KV
- On startup, load last known seq and continue from there
- Trade-off: disk I/O on every publish (mitigated by batching)
- Decision deferred until JetStream KV is available (post-W7)

See: ADR-0015 (Deterministic Replay), RFC-0009 (W8)
```

---

## ADR-0009 — Configuration

### What's Missing

1. No hot-reload strategy
2. No multi-exchange config pattern
3. No secrets management

### Patch (append after Consequences)

```markdown
## Amendment — 2026-02-12

### Hot-Reload Strategy

Current: `POST /runtime/reload` triggers subsystem restart (not hot-reload).

Target: config changes that DON'T require restart:
- Log level changes
- Ticker additions/removals (WS Manager handles subscription changes)
- Backpressure thresholds

Config changes that DO require restart:
- Exchange additions/removals
- WS endpoint changes
- JetStream URL changes
- Port/bind changes

Implementation: Guardian receives `ReloadConfig` message, diff against current, apply non-restart changes immediately, schedule restart for restart-requiring changes.

### Multi-Exchange Config (ADR-0017)

```json
{
  "exchanges": [
    {
      "name": "binance",
      "enabled": true,
      "tickers": ["BTCUSDT", "ETHUSDT"],
      "market_type": "spot",
      "ws": { ... }
    },
    {
      "name": "bybit",
      "enabled": true,
      "tickers": ["BTCUSDT"],
      "market_type": "spot",
      "ws": { ... }
    }
  ]
}
```

Each exchange config spawns one `MarketDataSubsystem` in Guardian with key `"marketdata:{name}"`.

### Secrets Management

Exchange API keys (when needed for authenticated endpoints):
- NOT stored in config.jsonc
- Loaded from environment variables: `MR_{EXCHANGE}_API_KEY`, `MR_{EXCHANGE}_API_SECRET`
- Config references env var names: `"api_key_env": "MR_BINANCE_API_KEY"`
- Validation: if `api_key_env` is set but env var is empty, fail-fast at startup

Decision deferred until an exchange requires authentication for WebSocket streams.

See: ADR-0017 (Multi-Exchange), RFC-0010 (W9)
```

---

## ADR-0011 — Binance Canonical Mapping

### What's Missing

1. No market type normalization
2. No mapping table strategy (hardcoded vs dynamic)
3. No extension pattern for new event types

### Patch (append after Consequences)

```markdown
## Amendment — 2026-02-12

### Market Type Normalization (ADR-0017)

Market type is metadata on `InstrumentMetadata`, not part of the instrument key:

| Binance Endpoint | Market Type | Canonical Instrument |
|-----------------|-------------|---------------------|
| `wss://stream.binance.com` | `SPOT` | `BTCUSDT` |
| `wss://fstream.binance.com` | `USD_M_FUTURES` | `BTCUSDT` |
| `wss://dstream.binance.com` | `COIN_M_FUTURES` | `BTCUSD_PERP` |

Stream ID: `BINANCE:BTCUSDT:SPOT` (unique per venue:instrument:market_type).

### Mapping Strategy

Phase 1 (current): Hardcoded `InstrumentCatalog` with top 50 pairs.
Phase 2 (future): REST API discovery at startup (`GET /api/v3/exchangeInfo`).

The `InstrumentCatalog.Resolve(venueSymbol)` interface supports both strategies. Adapter chooses implementation.

### Adding New Event Types

Parser pattern (`ParseFunc`) maps exchange event types to canonical types:
- Exchange `"aggTrade"` → canonical `"marketdata.trade"`
- Exchange `"depthUpdate"` → canonical `"marketdata.bookdelta"`

To add a new event type:
1. Add payload VO to `internal/core/marketdata/domain/payloads.go`
2. Add canonical type constant
3. Add parser branch in exchange adapter
4. Register decoder in `codec.Registry`

No changes to core ports or use cases needed (IngestMarketData is event-type agnostic).

See: ADR-0017 (Multi-Exchange), RFC-0010 (W9)
```

---

## Summary: Files to Edit

| ADR | File | Section to Append |
|-----|------|-------------------|
| ADR-0002 | `docs/adrs/ADR-0002-envelope-design-versioning.md` | Amendment: Wire Format, Schema Discovery, Compatibility Rules, Meta Fields |
| ADR-0003 | `docs/adrs/ADR-0003-actor-runtime-hollywood.md` | Amendment: Lifecycle, Request/Reply, Generation Counter, SupervisorPolicy |
| ADR-0004 | `docs/adrs/ADR-0004-nats-jetstream.md` | Amendment: Subject Hierarchy, Stream Config, Consumer Config, Dedup |
| ADR-0005 | `docs/adrs/ADR-0005-sequencing-strategy.md` | Amendment: Sequence Authorities, Replay Interaction, Persistent Sequencer |
| ADR-0009 | `docs/adrs/ADR-0009-configuration.md` | Amendment: Hot-Reload, Multi-Exchange Config, Secrets |
| ADR-0011 | `docs/adrs/ADR-0011-binance-canonical-mapping.md` | Amendment: Market Type, Mapping Strategy, New Event Types |

**Execution rule:** Each amendment is appended to the existing ADR file. No existing sections are modified or removed. Each amendment starts with `## Amendment — 2026-02-12` for traceability.
