# ADR-0014 — Stream Partitioning Strategy

**Status:** Proposed
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 section E.4, ADR-0004, RFC-0008 (W7)

---

## Context

The system needs a deterministic partitioning scheme for:
- NATS JetStream subjects (durable streams)
- Consumer parallelism (processing workers)
- Cross-venue queries (arbitrage consumers subscribing to multiple venues for one instrument)

The current InMemoryBus has no partitioning — all subscribers receive all messages. Moving to JetStream requires explicit subject hierarchy and consumer group design.

## Decision

### 1. NATS Subject Hierarchy

```
{context}.{event_type}.v{version}.{venue}.{instrument}
```

Examples:
```
marketdata.trade.v1.binance.BTCUSDT
marketdata.bookdelta.v1.binance.ETHUSDT
marketdata.markprice.v1.binance.BTCUSDT
aggregation.snapshot.v1.binance.BTCUSDT
insights.divergence.v1.global.BTCUSDT
```

Rules:
- All segments lowercase except `instrument` (uppercase canonical per `naming.CanonicalInstrument`)
- Version segment uses `v` prefix + integer
- `venue` = canonical venue name (lowercase in subjects for NATS convention)
- `instrument` = canonical instrument (uppercase to match domain convention)

### 2. Partition Key

Primary partition key: `(venue, instrument)`.

All events for one `(venue, instrument)` pair route to the same NATS subject. This guarantees **ordering within a partition** — critical for order book correctness and replay.

### 3. JetStream Stream Configuration

```
Stream: MARKETDATA
  Subjects: marketdata.>
  Retention: Limits
  MaxAge: 24h
  MaxBytes: 10GB
  Storage: File
  Replicas: 1 (single-node MVP)
  DedupWindow: 5m
  MaxMsgSize: 1MB
```

### 4. Consumer Groups

**Phase 1 (W7):** Single durable consumer per processing stage.
- Consumer name: `processor-aggregation`
- DeliverPolicy: DeliverAll (for replay) or DeliverNew (for live)
- AckPolicy: Explicit
- MaxAckPending: 1024
- FilterSubject: `marketdata.>` (receives all marketdata events)

**Phase 2 (future):** Per-instrument consumers for parallelism.
- Consumer per `(venue, instrument)` pair
- FilterSubject: `marketdata.*.v*.{venue}.{instrument}`
- Consistent hash of `(venue, instrument)` for worker assignment

### 5. Wildcard Subscriptions for Cross-Venue

Arbitrage and cross-venue consumers use NATS wildcards:
```
marketdata.trade.v1.*.BTCUSDT     # All venues, one instrument, trades only
marketdata.*.v1.*.BTCUSDT         # All event types, all venues, one instrument
marketdata.trade.v1.binance.*     # One venue, all instruments, trades only
```

### 6. Subject Builder Port

Subject construction lives in a pure function, not in adapters:

```go
// internal/shared/envelope/subject.go
func SubjectFromEnvelope(env Envelope) string {
    return fmt.Sprintf("%s.v%d.%s.%s",
        env.Type,
        env.Version,
        strings.ToLower(env.Venue),
        env.Instrument,
    )
}
```

This ensures subject derivation is deterministic and testable without NATS dependency.

## Rationale

- Per-instrument partitioning guarantees ordering without global coordination
- NATS wildcard subscriptions enable cross-venue queries without custom fan-out code
- Version in subject allows consumers to filter by schema version during migrations
- Single stream with subject filtering (vs. multiple streams) simplifies operations

## Alternatives Considered

1. **Per-venue streams:** `MARKETDATA_BINANCE`, `MARKETDATA_BYBIT`, etc. Rejected — complicates cross-venue queries and requires managing N streams.
2. **Hash-based partitioning to N partitions:** Rejected for Phase 1 — NATS subject-based filtering is simpler and sufficient.
3. **Event type as stream:** `TRADES`, `DEPTH`, etc. Rejected — splits instrument events across streams, breaking ordering for aggregation.

## Consequences

### Positive
- Ordering guaranteed per (venue, instrument) — correct for order book building
- Cross-venue subscription is one wildcard away
- Migration-friendly: version in subject enables gradual consumer upgrades

### Negative
- Subject cardinality = `event_types * versions * venues * instruments` — potentially high but manageable with NATS
- Single stream may become bottleneck at extreme scale (>100k messages/sec). Mitigated by NATS horizontal scaling.

### Invariants (testable)
- `PART-1`: `SubjectFromEnvelope(env)` produces identical output for same envelope (deterministic, unit test)
- `PART-2`: All published subjects match pattern `{context}.{event}.v{N}.{venue}.{instrument}` (integration test)
- `PART-3`: Consumer receives events in seq order per (venue, instrument) (integration test with fixture replay)

## Rollout Plan

1. Add `SubjectFromEnvelope()` to `internal/shared/envelope/` (RFC-0005/W4 — prep)
2. Configure JetStream stream in `internal/adapters/jetstream/` (RFC-0008/W7)
3. Publisher uses `SubjectFromEnvelope()` for NATS publish subject (RFC-0008/W7)
4. Consumer uses FilterSubject for subscription (RFC-0008/W7)
5. Cross-venue wildcard tested in RFC-0010/W9 (multi-exchange)
