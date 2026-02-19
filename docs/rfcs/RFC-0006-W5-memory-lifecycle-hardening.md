# RFC-0006 — W5: Memory Leak Mitigation & Lifecycle Hardening

**Status:** Accepted
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W5 of PRD-0001
**Relates to:** ADR-0012 (Lifecycle Invariants), ADR-0013 (Backpressure), ADR-0018 (Topology)

---

## 1. Goal

Eliminate all known memory and goroutine leak vectors, bound every state map, and formalize shutdown choreography. After W5:
- `IngestMarketData.streams` and `UpdateOrderBookFromEvents.books` are LRU-bounded
- `OrderBook.bids/asks` are trimmed to configurable max levels
- `ws/consumer.go` lifecycle paths are audited and all goroutines cancel deterministically
- Guardian has a global restart rate limiter
- Soak test validates goroutine and heap stability over 30 minutes

Implementation note (W11 partial follow-up):
- Runtime sizing is now externally bounded by config: `marketdata.max_instruments` and `processor.max_instruments` (default `2048` each), with deterministic LRU eviction.

## 2. Scope

- Create `internal/shared/ds/` package with generic `BoundedMap[K,V]` (LRU + TTL)
- Wire `BoundedMap` into `IngestMarketData` and `UpdateOrderBookFromEvents`
- Add `MaxLevels` to `OrderBook` constructor — trim deepest levels on insert
- Audit all goroutine lifecycle paths in `ws/consumer.go`
- Add global restart rate limiter to Guardian
- Add goroutine leak detector helper for tests (`leaktest` pattern)
- Add soak test script with pass/fail criteria

## 3. Non-Goals

- Performance optimization (profiling-driven, W4 first) — only bounding, not optimizing
- JetStream integration — separate RFC (W7)
- Protobuf schemas — separate RFC (W6)

## 4. Affected Modules

| File | Action | Change |
|------|--------|--------|
| `internal/shared/ds/boundedmap.go` | CREATE | Generic LRU+TTL map |
| `internal/shared/ds/boundedmap_test.go` | CREATE | Insert, access, evict, TTL tests |
| `internal/core/marketdata/app/ingest.go` | ALTER | Replace `map[string]*InstrumentStream` with `BoundedMap` |
| `internal/core/marketdata/app/ingest_test.go` | ALTER | Test LRU eviction at MaxStreams |
| `internal/core/aggregation/app/update_orderbook.go` | ALTER | Replace `map[string]*OrderBook` with `BoundedMap` |
| `internal/core/aggregation/app/update_orderbook_test.go` | ALTER | Test LRU eviction at MaxBooks |
| `internal/core/aggregation/domain/orderbook.go` | ALTER | Add `MaxLevels int` to constructor, trim on apply |
| `internal/core/aggregation/domain/orderbook_test.go` | ALTER | Test max levels enforcement |
| `internal/actors/marketdata/ws/consumer.go` | ALTER | Audit connectOnce paths, defer close(donech) |
| `internal/actors/marketdata/ws/consumer_test.go` | ALTER | Add goroutine leak test for connect/disconnect cycles |
| `internal/actors/runtime/guardian.go` | ALTER | Add `restartRateLimiter` |
| `internal/actors/runtime/guardian_test.go` | ALTER | Test rate limiter: 6th restart denied in window |
| `internal/shared/ds/leaktest.go` | CREATE | `AssertNoGoroutineLeak(t, fn)` helper |
| `scripts/soak-test.sh` | CREATE | Soak test runner with goroutine/heap assertions |

## 5. API Changes

### New package: `internal/shared/ds`

```go
package ds

import (
    "github.com/market-raccoon/internal/shared/clock"
)

// BoundedMap is a generic LRU+TTL bounded map.
// When maxSize is reached, the least-recently-used entry is evicted.
// Entries idle longer than TTL are evicted on access or sweep.
type BoundedMap[K comparable, V any] struct {
    maxSize int
    ttl     time.Duration
    clock   clock.Clock
    // internal: map + doubly-linked list for LRU ordering
}

func NewBoundedMap[K comparable, V any](maxSize int, ttl time.Duration, clk clock.Clock) *BoundedMap[K, V]

func (m *BoundedMap[K, V]) Get(key K) (V, bool)
func (m *BoundedMap[K, V]) Put(key K, value V)
func (m *BoundedMap[K, V]) Delete(key K)
func (m *BoundedMap[K, V]) Len() int
func (m *BoundedMap[K, V]) Sweep() int  // evict expired entries, return count evicted

// OnEvict is called when an entry is evicted (LRU or TTL).
// Use for cleanup (e.g., closing resources, logging).
func (m *BoundedMap[K, V]) SetOnEvict(fn func(key K, value V))
```

### IngestMarketData changes

```go
type IngestConfig struct {
    MaxStreams int           // default 10000
    StreamTTL time.Duration // default 1h
}

type IngestMarketData struct {
    streams *ds.BoundedMap[string, *domain.InstrumentStream]
    // ...
}
```

### OrderBook changes

```go
// OrderBook constructor gains MaxLevels
func NewOrderBook(instrument string, maxLevels int) *OrderBook

// Apply trims bids/asks to MaxLevels after delta application
func (ob *OrderBook) Apply(delta BookDeltaV1) []Event
```

### Guardian rate limiter

```go
type restartRateLimiter struct {
    window    time.Duration  // default 1 minute
    maxPerWin int            // default 5
    history   []time.Time
    clock     clock.Clock
}

func (r *restartRateLimiter) Allow() bool
func (r *restartRateLimiter) Reset()
```

### Goroutine leak detector

```go
package ds

// AssertNoGoroutineLeak runs fn and asserts goroutine count
// returns to baseline (±tolerance) within timeout.
func AssertNoGoroutineLeak(t *testing.T, fn func(), tolerance int, timeout time.Duration)
```

## 6. Implementation Details

### 6.1 BoundedMap Internals

```
map[K]*entry[V]  ←  O(1) lookup
doubly-linked list  ←  O(1) LRU reorder on access
                    ←  O(1) eviction of tail
entry {
    key       K
    value     V
    expiresAt time.Time
    element   *list.Element
}
```

- `Get()`: if found and not expired, move to head; if expired, evict and return miss.
- `Put()`: if at capacity, evict tail; insert at head.
- `Sweep()`: walk from tail, evict expired entries. Called periodically or on threshold.
- Thread safety: `sync.RWMutex` (read lock for Get, write lock for Put/Delete/Sweep).

### 6.2 Consumer Lifecycle Audit

Current risk in `connectOnce()`:
```
1. Dial WS connection
2. Subscribe to streams     ← can fail after step 1
3. Spawn readLoop goroutine
4. Spawn keepalive goroutine ← can fail to spawn if step 3 panics
5. Spawn heartbeat goroutine
```

Fix: `defer close(donech)` at top of `connectOnce()` ensures all goroutines see the signal regardless of where the function exits. This is a one-line change but critical for correctness.

Additional: add explicit `closeResources()` method called from all exit paths (error returns, context cancellation, shutdown).

### 6.3 Guardian Rate Limiter

```go
func (r *restartRateLimiter) Allow() bool {
    now := r.clock.Now()
    // Prune entries outside window
    cutoff := now.Add(-r.window)
    valid := r.history[:0]
    for _, t := range r.history {
        if t.After(cutoff) {
            valid = append(valid, t)
        }
    }
    r.history = valid
    if len(r.history) >= r.maxPerWin {
        return false
    }
    r.history = append(r.history, now)
    return true
}
```

When `Allow()` returns false:
- Guardian logs ERROR: "restart rate limit exceeded for subsystem X"
- Subsystem stays in degraded state
- Metric: `guardian_rate_limited_total` incremented
- Retry deferred until window expires

### 6.4 OrderBook MaxLevels

After applying a delta:
```go
func (ob *OrderBook) trimLevels() {
    if ob.maxLevels <= 0 {
        return // no limit
    }
    if len(ob.bids) > ob.maxLevels {
        ob.bids = ob.bids[:ob.maxLevels] // bids sorted desc by price; trim deepest
    }
    if len(ob.asks) > ob.maxLevels {
        ob.asks = ob.asks[:ob.maxLevels] // asks sorted asc by price; trim deepest
    }
}
```

## 7. Migration Strategy

No migration needed. All changes are backward compatible:
- `BoundedMap` defaults can be set to large values (effectively unbounded) during transition
- `MaxLevels=0` means no limit (current behavior)
- Rate limiter is off by default if `maxPerWin=0`

## 8. Test Plan

| Type | Test | Pass Criteria |
|------|------|---------------|
| Unit | `BoundedMap` insert N+1 items with maxSize=N | Oldest evicted, Len()==N |
| Unit | `BoundedMap` TTL expiry | Expired entry returns miss |
| Unit | `BoundedMap` LRU access pattern | Recently accessed entry not evicted |
| Unit | `BoundedMap` OnEvict callback | Callback invoked with correct key/value |
| Unit | `BoundedMap` concurrent access | `-race` clean with 10 goroutines |
| Unit | `OrderBook` with MaxLevels=100 | Never exceeds 100 per side after Apply |
| Unit | Rate limiter allows N, denies N+1 within window | Correct allow/deny |
| Unit | Rate limiter resets after window expires | Allows again |
| Integration | Consumer connect/disconnect 100 cycles | Goroutine delta == 0 |
| Integration | IngestMarketData with MaxStreams=10, ingest 20 instruments | Len(streams)==10, oldest evicted |
| Soak | 30min with 200 tickers | Goroutine delta <= 5, heap growth < 10% |

## 9. Acceptance Criteria

- [x] `BoundedMap` with LRU+TTL passes all unit tests including `-race`
- [x] `IngestMarketData` with `MaxStreams=10` evicts oldest stream when 11th arrives
- [x] `UpdateOrderBookFromEvents` with `MaxBooks=10` evicts oldest book when 11th arrives
- [x] `OrderBook` with `MaxLevels=100` never has > 100 levels on either side
- [x] `ws/consumer.go` — `close(donech)` called on ALL exit paths of `connectOnce()`
- [x] Consumer stop/reconnect cycle (100 iterations): 0 goroutine leaks
- [x] Guardian rate limiter: 6 rapid `ChildFailed` → only 5 restarts, 6th deferred
- [x] `go test -race ./...` green across all modules
- [x] pprof heap profile shows stable allocations in 30min soak
- [x] `guardian_rate_limited_total` metric emits when rate limit is hit

## 10. Execution Evidence (2026-02-12)

- Soak harness executed (`scripts/soak-test.sh`) with goroutine/heap leak checks and bounded-map stress pass (`.context/evidence/w5-soak.txt`).
- Full module matrix green (`go test` and `go test -race`) except `cmd/store` expected `no packages to test` (recorded in `.context/evidence/w5-full-test.txt` and `.context/evidence/w5-full-race.txt`).
- Runtime hardening delivered in `internal/actors/marketdata/ws/consumer.go`, bounded state maps in core use-cases, and bounded order book depth in domain aggregate.

## Changelog

- 2026-02-13:
  - normalizado status para taxonomia RFC (`Draft|Accepted`);
  - mantidas evidências de hardening da rodada W5.

## Test Plan

```bash
make docs-check-full
```

## Acceptance

- Required RFC sections are present and validated by `make docs-check-full`.
