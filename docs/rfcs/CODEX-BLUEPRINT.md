# Codex Blueprint — Implementation Prompts

**Date:** 2026-02-12
**Purpose:** Copy-paste-ready prompts for implementing W4 and W5. Each prompt is self-contained with context, constraints, and acceptance criteria.

---

## W4 — Observability & Profiling

### W4-P1: Create metrics package

```
CONTEXT:
- Go workspace at /Volumes/OWC Express 1M2/Develop/market-raccoon
- Module: internal/shared (go.mod: github.com/market-raccoon/internal/shared)
- Pattern: all shared code lives under internal/shared/
- No external metrics exist yet. Logging is via slog.

TASK:
Create internal/shared/metrics/ package with two files:

1. registry.go:
   - Package-level *prometheus.Registry (not the global default — we use a custom registry)
   - func Registry() *prometheus.Registry — returns the shared registry
   - func Handler() http.Handler — returns promhttp.HandlerFor(Registry())

2. metrics.go:
   - Define all metric variables as package-level vars, registered lazily via init()
   - Metrics to define (exact names and labels):

   IngestTotal        *prometheus.CounterVec   labels: venue, instrument, event_type, status
   IngestLatency      *prometheus.HistogramVec labels: venue, instrument, event_type
                      buckets: 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05 (100us to 50ms)
   IngestStreamsActive prometheus.Gauge

   BackpressureQueueDepth  *prometheus.GaugeVec   labels: venue
   BackpressureDropsTotal  *prometheus.CounterVec  labels: venue, policy
   BackpressureActive      *prometheus.GaugeVec    labels: venue

   BusPublishedTotal  *prometheus.CounterVec  labels: type, venue
   BusDropsTotal      *prometheus.CounterVec  labels: subscriber_id

   WSConnectionsActive *prometheus.GaugeVec   labels: exchange
   WSReconnectsTotal   *prometheus.CounterVec labels: exchange, reason
   WSMessagesTotal     *prometheus.CounterVec labels: exchange, stream
   WSErrorsTotal       *prometheus.CounterVec labels: exchange, kind

   GuardianRestartsTotal    *prometheus.CounterVec labels: subsystem, kind
   GuardianDegradedTotal    *prometheus.CounterVec labels: subsystem
   GuardianRateLimitedTotal prometheus.Counter
   GuardianSubsystemState   *prometheus.GaugeVec  labels: subsystem

   ProcessGoroutines   prometheus.Gauge
   ProcessHeapAlloc    prometheus.Gauge

CONSTRAINTS:
- Use prometheus.NewRegistry() (custom, not prometheus.DefaultRegisterer)
- Register metrics with MustRegister on the custom registry
- Add github.com/prometheus/client_golang to internal/shared/go.mod
- Run go mod tidy after adding dependency
- Package name: metrics
- No init() side effects beyond registration

ACCEPTANCE:
- go build ./internal/shared/metrics/ compiles
- go test -race ./internal/shared/metrics/ passes (write a basic test that all metrics are non-nil)
- go vet ./internal/shared/metrics/ clean
```

### W4-P2: Create SubjectFromEnvelope

```
CONTEXT:
- Module: internal/shared
- Package: internal/shared/envelope/
- Existing: envelope.go defines Envelope struct with Type, Version, Venue, Instrument fields
- Needed for NATS subject mapping and delivery routing

TASK:
Create internal/shared/envelope/subject.go:

func SubjectFromEnvelope(env Envelope) string {
    return fmt.Sprintf("%s.v%d.%s.%s",
        env.Type,
        env.Version,
        strings.ToLower(env.Venue),
        env.Instrument,
    )
}

Create internal/shared/envelope/subject_test.go:
- Test: trade envelope → "marketdata.trade.v1.binance.BTCUSDT"
- Test: bookdelta envelope → "marketdata.bookdelta.v1.binance.ETHUSDT"
- Test: uppercase venue normalized to lowercase in subject
- Test: deterministic (same envelope → same subject every time)

CONSTRAINTS:
- strings.ToLower for venue (canonical subjects use lowercase venue)
- Instrument stays as-is (uppercase BTCUSDT)
- No external dependencies needed

ACCEPTANCE:
- go test -race ./internal/shared/envelope/ passes
- SubjectFromEnvelope is deterministic (10 calls, same result)
```

### W4-P3: Add drop counter to InMemoryBus

```
CONTEXT:
- Module: internal/adapters
- File: internal/adapters/bus/inmemory.go
- Current: Publish() uses select { case ch <- env: default: } — silent drop on full channel
- Need: count drops per subscriber

TASK:
1. Read internal/adapters/bus/inmemory.go to understand current structure
2. Add per-subscriber drop counting:
   - Add dropCounts []atomic.Uint64 field to InMemoryBus
   - Initialize in Subscribe() (grow slice for each new subscriber)
   - In Publish() default branch: atomic.AddUint64 to increment drop count
   - Add func (b *InMemoryBus) DroppedCount(subscriberIndex int) uint64 method

3. Update tests in inmemory_test.go:
   - Test: publish to full subscriber channel → DroppedCount > 0
   - Test: publish to non-full channel → DroppedCount == 0

CONSTRAINTS:
- Thread-safe (atomic operations, no new mutexes in hot path)
- Do NOT import prometheus here (adapters don't depend on metrics directly)
- The metrics bridge (InMemoryBus drop count → Prometheus counter) happens in cmd/ wiring
- Preserve existing behavior: non-blocking send, no panic on full

ACCEPTANCE:
- go test -race ./internal/adapters/bus/ passes
- DroppedCount increments when subscriber channel is full
- No new mutex contention in Publish hot path
```

### W4-P4: Add /metrics and /debug/pprof/ to HTTP server

```
CONTEXT:
- Module: internal/interfaces
- File: internal/interfaces/http/server.go
- Existing endpoints: /healthz, /readyz, /runtime/snapshot, /runtime/reload
- Package: httpserver

TASK:
1. Read internal/interfaces/http/server.go
2. Add /metrics endpoint:
   - Import github.com/market-raccoon/internal/shared/metrics
   - Route: GET /metrics → metrics.Handler()
3. Add /debug/pprof/ endpoints:
   - Import net/http/pprof
   - Routes: /debug/pprof/ (index), /debug/pprof/cmdline, /debug/pprof/profile,
     /debug/pprof/symbol, /debug/pprof/trace,
     /debug/pprof/goroutine, /debug/pprof/heap, /debug/pprof/allocs,
     /debug/pprof/block, /debug/pprof/mutex, /debug/pprof/threadcreate
4. pprof localhost restriction:
   - If server binds to 0.0.0.0, pprof handlers check request source IP
   - If not localhost (127.0.0.1, ::1): return 403 Forbidden
   - OR: accept all if config.HTTP.PprofEnabled == true (default false)

CONSTRAINTS:
- Add prometheus/client_golang to internal/interfaces/go.mod (and replace for shared)
- pprof is security-sensitive: default to localhost-only
- Do NOT add middleware or auth — just IP check for pprof
- /metrics is public (Prometheus scraper needs access)

ACCEPTANCE:
- go build ./internal/interfaces/... compiles
- Integration test: GET /metrics returns Content-Type text/plain with prometheus format
- Integration test: GET /debug/pprof/goroutine?debug=1 returns goroutine dump (from localhost)
- go test -race ./internal/interfaces/... passes
```

### W4-P5: Instrument IngestMarketData

```
CONTEXT:
- Module: internal/core/marketdata
- File: internal/core/marketdata/app/ingest.go
- IngestMarketData is the main usecase: receives market data, validates, builds envelope, publishes
- Currently no metrics instrumentation

TASK:
1. Read internal/core/marketdata/app/ingest.go
2. Add optional metrics instrumentation:
   - IngestMarketData receives an optional metrics observer interface (not prometheus directly!)
   - Define port interface in app/ or ports/:

   type IngestObserver interface {
       OnIngest(venue, instrument, eventType, status string, latency time.Duration)
   }

   - In Execute(): measure latency (clock.Now before/after), call observer.OnIngest
   - status: "ok", "duplicate", "out_of_order", "validation_failed"

3. Create metrics adapter in cmd/ wiring that bridges IngestObserver → Prometheus:

   type prometheusIngestObserver struct{}
   func (o *prometheusIngestObserver) OnIngest(venue, instrument, eventType, status string, latency time.Duration) {
       metrics.IngestTotal.WithLabelValues(venue, instrument, eventType, status).Inc()
       if status == "ok" {
           metrics.IngestLatency.WithLabelValues(venue, instrument, eventType).Observe(latency.Seconds())
       }
   }

CONSTRAINTS:
- Core module (internal/core/marketdata) must NOT import prometheus
- Use port/adapter pattern: observer interface in core, prometheus impl in cmd/
- If observer is nil, skip (no-op, zero overhead in tests)
- Clock for latency measurement: use the injected clock.Clock, not time.Now()

ACCEPTANCE:
- IngestMarketData still works with nil observer (existing tests pass unchanged)
- With mock observer: OnIngest called with correct args
- No prometheus import in internal/core/
- go test -race ./internal/core/marketdata/... passes
```

---

## W5 — Memory Leak Mitigation & Lifecycle Hardening

### W5-P1: Create BoundedMap[K,V]

```
CONTEXT:
- Module: internal/shared
- Target: internal/shared/ds/boundedmap.go
- Used by IngestMarketData (streams map) and UpdateOrderBookFromEvents (books map)
- Requires clock.Clock for TTL

TASK:
Create internal/shared/ds/ package with:

1. boundedmap.go:
   type BoundedMap[K comparable, V any] struct {
       maxSize   int
       ttl       time.Duration
       clock     clock.Clock
       mu        sync.RWMutex
       items     map[K]*entry[V]
       order     *list.List  // front=most-recent, back=least-recent
       onEvict   func(K, V)
   }

   type entry[V any] struct {
       value     V
       expiresAt time.Time
       element   *list.Element
   }

   Methods:
   - NewBoundedMap[K,V](maxSize int, ttl time.Duration, clk clock.Clock) *BoundedMap[K,V]
   - Get(key K) (V, bool) — returns value, moves to front; evicts if expired
   - Put(key K, value V) — inserts at front; evicts LRU if over capacity
   - Delete(key K) — removes entry
   - Len() int — current size
   - Sweep() int — evict all expired entries, return count
   - SetOnEvict(fn func(K, V)) — callback on eviction

2. boundedmap_test.go:
   - TestBoundedMap_Insert_Get — basic insert and retrieve
   - TestBoundedMap_Eviction_MaxSize — insert N+1, oldest evicted, Len==N
   - TestBoundedMap_Eviction_TTL — FakeClock advance past TTL, Get returns miss
   - TestBoundedMap_LRU_Access — access entry, it's not evicted when at capacity
   - TestBoundedMap_OnEvict_Called — callback invoked with correct key/value
   - TestBoundedMap_Sweep — expire multiple entries, sweep removes all
   - TestBoundedMap_Concurrent — 10 goroutines Put/Get/Delete, -race clean
   - TestBoundedMap_Delete — delete entry, Len decreases
   - TestBoundedMap_ZeroTTL — ttl=0 means no expiry (only LRU eviction)

CONSTRAINTS:
- Use container/list (stdlib) for LRU ordering — NOT a third-party LRU library
- Use sync.RWMutex (RLock for Get, Lock for Put/Delete/Sweep)
- clock.Clock for time (enables FakeClock in tests)
- Zero-value ttl (0) means no TTL (entries only evicted by LRU)
- OnEvict callback called INSIDE the lock (keep it fast, no blocking ops)

ACCEPTANCE:
- All tests pass with go test -race -count=3 ./internal/shared/ds/
- No external dependencies (only stdlib + clock from shared)
- BoundedMap is generic (works with any comparable key, any value)
```

### W5-P2: Wire BoundedMap into IngestMarketData

```
CONTEXT:
- Module: internal/core/marketdata
- File: internal/core/marketdata/app/ingest.go
- Current: streams field is map[string]*domain.InstrumentStream (unbounded)
- BoundedMap created in W5-P1

TASK:
1. Read internal/core/marketdata/app/ingest.go
2. Replace map[string]*InstrumentStream with ds.BoundedMap[string, *domain.InstrumentStream]
3. Add IngestConfig parameter:

   type IngestConfig struct {
       MaxStreams int           // default 10000; 0 = unlimited (legacy)
       StreamTTL time.Duration // default 1h; 0 = no TTL
   }

4. Update NewIngestMarketData to accept IngestConfig
5. Set OnEvict to log evicted streams at INFO level
6. Update Execute() to use Get/Put instead of map access
7. Update all tests:
   - Existing tests: pass IngestConfig with generous limits (no behavior change)
   - New test: TestIngestMarketData_StreamEviction_MaxStreams
     → Ingest 11 unique instruments with MaxStreams=10
     → Assert len(streams) == 10, first stream evicted

CONSTRAINTS:
- internal/core/marketdata must NOT import ds directly if ds is in shared
  → Actually, core CAN import shared (it already does via go.mod)
  → Verify: shared/ds/ is importable from core/marketdata
- If MaxStreams==0, create BoundedMap with maxSize=math.MaxInt (effectively unbounded)
- Preserve all existing behavior for default config

ACCEPTANCE:
- All existing tests pass unchanged (with default config)
- New eviction test passes
- go test -race ./internal/core/marketdata/... passes
- streams field is BoundedMap, not raw map
```

### W5-P3: Wire BoundedMap into UpdateOrderBookFromEvents

```
CONTEXT:
- Module: internal/core/aggregation
- File: internal/core/aggregation/app/update_orderbook.go
- Current: books field is map[string]*domain.OrderBook (unbounded)
- Same pattern as W5-P2

TASK:
1. Read internal/core/aggregation/app/update_orderbook.go
2. Replace map[string]*OrderBook with ds.BoundedMap[string, *domain.OrderBook]
3. Add config:

   type AggregationConfig struct {
       MaxBooks int           // default 10000
       BookTTL  time.Duration // default 1h
   }

4. Update constructor and Execute()
5. Test: MaxBooks=10, ingest 11 instruments, assert eviction

CONSTRAINTS:
- Same constraints as W5-P2
- OnEvict logs evicted books

ACCEPTANCE:
- All existing tests pass
- New eviction test passes
- go test -race ./internal/core/aggregation/... passes
```

### W5-P4: Add MaxLevels to OrderBook

```
CONTEXT:
- Module: internal/core/aggregation
- File: internal/core/aggregation/domain/orderbook.go
- Current: bids/asks slices grow with every new price level; zero-qty levels removed
- Need: trim to MaxLevels after each Apply

TASK:
1. Read internal/core/aggregation/domain/orderbook.go
2. Add MaxLevels int field to OrderBook
3. Update constructor: NewOrderBook(instrument string, maxLevels int)
4. After Apply (delta application), call trimLevels():
   - bids sorted descending by price → keep first MaxLevels (best bids)
   - asks sorted ascending by price → keep first MaxLevels (best asks)
   - If MaxLevels <= 0: no trimming (current behavior)
5. Test: Apply 200 levels with MaxLevels=100 → never exceeds 100 per side

CONSTRAINTS:
- trimLevels() is called AFTER delta application and zero-qty removal
- Sorting must already exist (verify current code) — if not, sort before trim
- MaxLevels=0 means no limit (backward compatible)

ACCEPTANCE:
- TestOrderBook_MaxLevels: apply 200 unique price levels, assert len(bids) <= 100
- All existing tests pass (they should use MaxLevels=0 or large value)
- go test -race ./internal/core/aggregation/... passes
```

### W5-P5: Audit consumer.go lifecycle

```
CONTEXT:
- Module: internal/actors
- File: internal/actors/marketdata/ws/consumer.go
- Risk R1 from PRD-0001: partial-setup paths in connectOnce() may leak goroutines
- 3 goroutines per connection: readLoop, keepalive, heartbeat
- All select on donech + quitch + ctx.Done()

TASK:
1. Read internal/actors/marketdata/ws/consumer.go thoroughly
2. Find connectOnce() function
3. Verify: is close(donech) called on ALL exit paths?
   - If NOT: add defer close(donech) at the top of connectOnce
   - If YES: document this finding
4. Find all goroutine spawns (go func)
5. Verify each has a select on at least one cancellation signal
6. Create test: TestConsumer_ConnectDisconnect_NoGoroutineLeak
   - Record runtime.NumGoroutine() before
   - Create consumer, connect, disconnect, 100 times
   - Record runtime.NumGoroutine() after
   - Assert delta <= tolerance (2)
   - Use time.Sleep(100ms) after each cycle for goroutine cleanup

CONSTRAINTS:
- Do NOT restructure consumer.go — minimal surgical fix only
- defer close(donech) is a one-line addition
- The leak test should use the existing test infrastructure (spy/mock server if available)
- If no mock WS server exists, create a minimal one for the leak test

ACCEPTANCE:
- close(donech) is called on ALL exit paths of connectOnce
- Leak test passes: 100 cycles, goroutine delta <= 2
- go test -race ./internal/actors/marketdata/ws/ passes
- No other behavioral changes
```

### W5-P6: Add restart rate limiter to Guardian

```
CONTEXT:
- Module: internal/actors
- File: internal/actors/runtime/guardian.go
- Current: Guardian restarts subsystems on ChildFailed, with SupervisorPolicy backoff
- Need: global rate limiter across ALL subsystems to prevent restart storms

TASK:
1. Read internal/actors/runtime/guardian.go
2. Add restartRateLimiter:

   type restartRateLimiter struct {
       window    time.Duration
       maxPerWin int
       history   []time.Time
       clock     clock.Clock
   }

   func newRestartRateLimiter(window time.Duration, maxPerWin int, clk clock.Clock) *restartRateLimiter
   func (r *restartRateLimiter) Allow() bool  // prune old, check count, record if allowed
   func (r *restartRateLimiter) Reset()

3. Wire into Guardian:
   - Add rateLimiter field to Guardian struct
   - In handleChildFailed: check rateLimiter.Allow() before scheduling restart
   - If denied: log ERROR, increment GuardianRateLimitedTotal metric (if available, else just log)
   - Subsystem stays in degraded state until window expires

4. Add to GuardianConfig:
   - RateLimitWindow time.Duration (default 1min)
   - RateLimitMax int (default 5)

5. Tests:
   - TestGuardian_RateLimiter_AllowsWithinLimit: 5 failures → 5 restarts
   - TestGuardian_RateLimiter_DeniesOverLimit: 6 rapid failures → 5 restarts, 6th denied
   - TestGuardian_RateLimiter_ResetsAfterWindow: after window expires, allows again

CONSTRAINTS:
- Rate limiter uses clock.Clock (FakeClock in tests for deterministic behavior)
- Rate limiter is GLOBAL (across all subsystems), not per-subsystem
- If RateLimitMax == 0, rate limiting is disabled (backward compatible)
- Do NOT change existing SupervisorPolicy behavior — rate limiter is an additional check

ACCEPTANCE:
- 5 rapid ChildFailed → 5 restarts (within limit)
- 6th ChildFailed within window → denied, subsystem stays degraded
- After window expires: allows again
- RateLimitMax=0: all restarts allowed (backward compat)
- go test -race ./internal/actors/runtime/ passes
- All existing Guardian tests pass unchanged
```

---

## Prompt Usage Guide

### Ordering

Execute prompts in this order:

**W4:** P1 → P2 → P3 → P4 → P5 (P1 and P2 can be parallel; P4 depends on P1)

**W5:** P1 → P2 → P3 → P4 → P5 → P6 (P1 first, P2/P3/P4 can be parallel after P1, P5/P6 independent)

### After Each Prompt

1. Run `go test -race ./...` in the affected module
2. Run `make lint` to check for linting issues
3. Commit with descriptive message referencing RFC number

### After All W4 Prompts

Run checkpoint validation:
```bash
curl http://localhost:8080/metrics    # should return Prometheus format
curl http://localhost:8080/debug/pprof/goroutine?debug=1  # should return goroutine dump
make test                             # all green
```

### After All W5 Prompts

Run soak test:
```bash
# Start consumer with 200 tickers
go run cmd/consumer/main.go -config=cmd/consumer/config.jsonc &
PID=$!

# Wait 30 minutes
sleep 1800

# Capture pprof
curl -s http://localhost:8080/debug/pprof/goroutine?debug=1 > goroutines-30min.txt
curl -s http://localhost:8080/debug/pprof/heap?debug=1 > heap-30min.txt

# Compare with baseline (captured at t=0)
# goroutine count delta should be <= 5
# heap alloc should be stable (not growing)

kill $PID
```
