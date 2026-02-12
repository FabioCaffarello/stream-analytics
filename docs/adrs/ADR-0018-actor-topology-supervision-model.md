# ADR-0018 — Actor Topology & Supervision Model

**Status:** Proposed
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 section E.3, ADR-0003, ADR-0012, RFC-0006 (W5)

---

## Context

The current actor topology works but is informally documented. As we add multi-exchange support and increase instrument count, we need explicit rules for:
- Which actors exist and their parent-child relationships
- Restart policies per level (transient vs fatal errors)
- How to prevent restart storms
- How to guarantee no double-publish after restart

PRD-0001 section E.3 designed the target topology. This ADR formalizes it.

## Decision

### 1. Target Topology

```
Guardian (root supervisor)
│
├── MarketDataSubsystem["binance"]
│   ├── WS Manager
│   │   ├── Consumer[bucket-0]  (goroutines: readLoop, keepalive, heartbeat)
│   │   ├── Consumer[bucket-1]
│   │   └── Consumer[bucket-N]
│   └── IngestWorker (goroutine: wsQueue → IngestMarketData)
│
├── MarketDataSubsystem["bybit"]  (future: one per exchange)
│   └── (same structure)
│
├── AggregationSubsystem
│   └── consumeLoop (goroutine: bus channel → UpdateOrderBookFromEvents)
│
├── DeliverySubsystem
│   └── RouterActor
│       ├── SessionActor[session-1]  (per WS client)
│       └── SessionActor[session-N]
│
└── InsightsSubsystem (future)
```

### 2. Supervision Strategy Per Level

| Level | Supervisor | Restart Policy | Escalation |
|-------|-----------|----------------|------------|
| **Consumer** | WS Manager | Manager rotates/respawns on MaxWebsocketLifetime. Consumer retries internally with backoff on transient errors. | Non-transient WS errors → `ChildFailed` to SubsystemActor |
| **WS Manager** | SubsystemActor | Part of subsystem lifecycle. Manager crash = subsystem crash. | Escalates to Guardian |
| **SubsystemActor** | Guardian | `SupervisorPolicy`: 5 failures in 30s window → degraded 30s cooldown. Exponential backoff with jitter. | Guardian decides restart vs degrade |
| **RouterActor** | DeliverySubsystem | Part of subsystem lifecycle. | Escalates to Guardian |
| **SessionActor** | RouterActor | Self-poisons on WS disconnect. No restart — client reconnects. | Unregister from router on stop |
| **Guardian** | None (root) | Never restarts. Process exit on unrecoverable failure. | N/A |

### 3. Error Classification

```
TRANSIENT (local retry, no escalation):
  WS errors: dial, read, subscribe, pingpong, heartbeat
  → Consumer retries internally via reconnect backoff
  → Metric: ws_reconnects_total

ESCALATABLE (subsystem restart via Guardian):
  Unknown WS errors (not in transient list)
  Bus closure (channel closed)
  Config errors at runtime
  → ChildFailed message to Guardian
  → Guardian applies SupervisorPolicy
  → Metric: guardian_restarts_total{subsystem}

FATAL (process exit):
  Config validation failure at startup
  → os.Exit(1) before any actor spawn

DOMAIN (log + skip, no restart):
  Out-of-order sequence (MD_OUT_OF_ORDER)
  Duplicate (MD_DUPLICATE)
  Parse error (VALIDATION_FAILED)
  → Metric + sampled log, processing continues
```

### 4. Restart Storm Prevention

**Global restart rate limiter** in Guardian:

```go
type restartRateLimiter struct {
    window    time.Duration  // 1 minute
    maxPerWin int            // 5 total subsystem restarts
    history   []time.Time
}
```

If rate limiter denies a restart:
- Subsystem enters degraded mode
- Cooldown = `max(policy.Cooldown, remaining_window_time)`
- Log ERROR: "restart rate limit exceeded, deferring restart"
- Metric: `guardian_rate_limited_total`

This prevents cascading restart storms when multiple subsystems fail simultaneously (e.g., network outage affecting all exchanges).

### 5. No Double-Publish Guarantee

**Problem:** When a subsystem restarts, `IngestMarketData` loses its in-memory `streams` map. Without JetStream dedup, the same event could be published twice.

**Solution by bus type:**
- **InMemoryBus:** No guarantee possible (bus is ephemeral). Acceptable because InMemoryBus consumers are also ephemeral.
- **JetStream:** NATS dedup window (5 minutes) prevents double-publish. `IdempotencyKey` maps to NATS `Msg-ID` header. If same `Msg-ID` is published within dedup window, NATS rejects it silently.

**Consumer-side defense:** All consumers MUST be idempotent. `IdempotencyKey` enables downstream dedup regardless of bus guarantees.

### 6. Actor Lifecycle Rules

1. **Actors MUST NOT call `os.Exit()`** — only `cmd/*/main.go` may exit the process.
2. **Actors MUST release all resources in `actor.Stopped`** — close connections, cancel contexts, stop timers.
3. **Actors MUST NOT spawn goroutines without cancellation** — per ADR-0012 INV-1.
4. **Child actors MUST NOT outlive their parent** — Hollywood guarantees this via poison propagation.
5. **Actors MUST NOT hold references to other actors' state** — communicate only via messages.

## Rationale

Explicit topology and restart policies:
- Enable operators to understand failure domains
- Prevent restart storms from cascading across subsystems
- Ensure predictable recovery times (SLO: < 5s subsystem, < 30s full)
- Make double-publish risk explicit and mitigated

## Alternatives Considered

1. **Flat actor topology (all actors direct children of Guardian):** Rejected — no intermediate supervision. One WS consumer failure would restart the entire marketdata subsystem.
2. **Actor-per-instrument:** Rejected — 10k+ actors for large instrument sets. Hollywood mailbox overhead would dominate. Goroutine-based workers within subsystem are more efficient.
3. **No restart rate limiter:** Rejected — thundering herd of restarts after network blip could overwhelm system.
4. **Persistent lastPublishedSeq for dedup:** Rejected — adds storage dependency to hot path. NATS dedup is simpler and sufficient.

## Consequences

### Positive
- Clear failure domains: WS failure ≠ aggregation failure
- Restart storms prevented by rate limiter
- Double-publish prevented by NATS dedup (production) or accepted (dev/InMemoryBus)
- Operators can reason about recovery time from topology diagram

### Negative
- Per-exchange subsystems increase actor count (minor overhead)
- Rate limiter may delay legitimate restarts if threshold is too aggressive
- NATS dedup window (5min) means restarts within 5min of last publish are safe; longer gaps may allow duplicates (edge case)

### Invariants (testable)
- `TOP-1`: Guardian with 3 subsystems — poison one — other two continue running (integration test)
- `TOP-2`: Rate limiter: 6 rapid ChildFailed events → only 5 restarts, 6th deferred (unit test)
- `TOP-3`: SessionActor disconnect → unregister from router → actor stopped (integration test)
- `TOP-4`: Subsystem restart cycle (10x): goroutine count returns to baseline (soak test)
- `TOP-5`: JetStream publish with duplicate Msg-ID within 5min → no duplicate delivery (integration test with testcontainers)

## Rollout Plan

1. Add global restart rate limiter to Guardian (RFC-0006/W5)
2. Add `guardian_rate_limited_total` metric (RFC-0005/W4)
3. Extend Guardian to support string-based SubsystemKey for multi-exchange (RFC-0010/W9)
4. Document topology diagram in `docs/architecture/topology.md` (RFC-0006/W5)
5. Validate TOP-1 through TOP-4 in integration tests (RFC-0006/W5)
6. Validate TOP-5 with testcontainers NATS (RFC-0008/W7)

## Amendment 2026-02-12 (W9-1)

- Guardian now manages dynamic subsystem keys for configured marketdata instances (for example `marketdata:binance`, `marketdata:bybit`) while preserving legacy single-key behavior.
- Readiness expectations are explicitly provided from consumer wiring for all configured marketdata subsystem keys.
- No change to supervision policy semantics (restart, cooldown, degraded mode), bus behavior, or delivery routing.
