# ADR-0012 — Lifecycle Invariants & Leak Prevention

**Status:** Proposed
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 sections E.1, A.3 (R1, R2, R3), RFC-0006 (W5)

---

## Context

The market-raccoon runtime spawns goroutines across multiple layers (WS consumer readLoop/keepalive/heartbeat, subsystem ingest workers, aggregation consume loops, delivery session read loops). Timers are used for Guardian retry scheduling and Manager stream rotation. State maps in `IngestMarketData` and `UpdateOrderBookFromEvents` grow without bound as new instruments appear.

PRD-0001 section A.3 identified three production risks directly tied to lifecycle management:
- **R1:** Goroutine leaks in `ws/consumer.go` partial-setup paths
- **R2:** Timer leaks if `cancelSchedule` is not called on all exit paths
- **R3:** Unbounded state maps in app-layer use cases

Without formal invariants, these risks grow with scale (more instruments, more exchanges, longer uptimes).

## Decision

We adopt the following **lifecycle invariants** as system-wide rules. Violations are treated as bugs.

### INV-1: No Goroutine Without Cancellation Path

Every goroutine spawned outside an actor mailbox MUST select on at least one of:
- `<-ctx.Done()` (context cancellation)
- `<-sentinel` (dedicated `chan struct{}` closed on stop)

The goroutine MUST exit within 5 seconds of the signal being delivered.

**Verification:** `runtime.NumGoroutine()` exposed as Prometheus gauge. Soak test asserts `delta(goroutines, 30min) <= 5`.

### INV-2: No Timer Without Cancel

Every `time.AfterFunc`, `time.NewTimer`, or `time.NewTicker` MUST store its cancel/stop function. The cancel MUST be called in the owning actor's `actor.Stopped` handler or equivalent cleanup path.

**Verification:** After `stopAll()` in Guardian, `len(scheduledRetry) == 0`. After `handleStopped()` in Manager, `len(scheduledPoison) == 0`. Unit tests assert these post-conditions.

### INV-3: Bounded State Maps

All in-memory maps that grow with cardinality (instruments, sessions, streams) MUST have one of:
- **Max-size bound** with LRU eviction
- **TTL eviction** with clock-based expiry
- **Explicit lifecycle** (created on subscribe, removed on unsubscribe)

Concrete bounds:
| Map | Owner | Default Max | Eviction |
|-----|-------|-------------|----------|
| `streams` | `IngestMarketData` | 10,000 | LRU + TTL (1h idle) |
| `books` | `UpdateOrderBookFromEvents` | 10,000 | LRU + TTL (1h idle) |
| `bids/asks` per OrderBook | `OrderBook` | 1,000 levels per side | Trim deepest on insert |
| `sessions` | `RouterActor` | Explicit (register/unregister) | N/A |
| `dedup seen` per stream | `InstrumentStream` | DedupWindow (1024) | FIFO (already bounded) |

**Verification:** `ingest_streams_active` and `aggregation_books_active` Prometheus gauges. Soak test asserts `max(gauge) <= configured_limit`.

### INV-4: Deterministic Shutdown Choreography

Shutdown of any resource-owning component follows this order:
1. Set shutdown flag (prevent new work)
2. Cancel scheduled timers
3. Signal goroutines (close channels / cancel context)
4. Close external connections (with write deadline)
5. Wait for goroutine exit (bounded timeout)

**Verification:** Shutdown test measures time from SIGTERM to process exit. Must be < 10s.

### INV-5: Actor Restart Finalizer

When a subsystem restarts, all resources of the old instance MUST be finalized before the new instance's `actor.Started` is delivered. Hollywood guarantees this via `actor.Stopped` → new spawn ordering. Actors MUST release all resources in `actor.Stopped`.

**Verification:** Restart cycle test: restart subsystem 10 times, assert goroutine count returns to baseline after each cycle.

## Rationale

Leak prevention cannot be ad-hoc. Invariants formalize expectations so that:
- Code review has concrete checklist items
- CI can validate via goroutine/heap metrics
- Soak tests have objective pass/fail criteria

## Alternatives Considered

1. **Runtime leak detector (goleak):** Useful for tests but doesn't prevent production leaks. Adopted as complementary tool, not primary defense.
2. **No explicit bounds (rely on GC):** Rejected — GC cannot reclaim goroutines or maps with live references.
3. **Actor-per-instrument (eliminates maps):** Too many actors for 10k+ instruments; Hollywood mailbox overhead would dominate.

## Consequences

### Positive
- Memory usage becomes predictable and bounded
- Goroutine count stabilizes in steady state
- Shutdown becomes deterministic and fast

### Negative
- LRU eviction adds ~200 LOC (generic `BoundedMap[K,V]`)
- TTL eviction requires clock injection (already available)
- Max-levels trim in OrderBook may lose deep liquidity data (acceptable for current use case)

### Invariants (testable)
- `INV-1`: `delta(runtime.NumGoroutine(), 30min) <= 5` in soak test
- `INV-2`: `len(scheduledRetry) == 0` after Guardian.stopAll()
- `INV-3`: `max(ingest_streams_active) <= MaxStreams` in soak test
- `INV-4`: `time(SIGTERM → exit) < 10s`
- `INV-5`: `goroutines(before_restart) == goroutines(after_restart)` within tolerance of 2

## Rollout Plan

1. Implement `internal/shared/ds/boundedmap.go` with LRU+TTL (RFC-0006/W5)
2. Wire into IngestMarketData and UpdateOrderBookFromEvents
3. Add MaxLevels to OrderBook constructor
4. Audit consumer.go lifecycle paths (close(donech) in all connectOnce exits)
5. Add goroutine metric and soak test validation
6. Add INV-2 post-condition assertions to guardian_test and manager_test
