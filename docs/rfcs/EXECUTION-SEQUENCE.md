# Execution Sequence — W4 through W9

**Date:** 2026-02-12
**PRD:** PRD-0001 (Market Raccoon Extreme Runtime)
**Total Work Packages:** 6 (W4..W9)

---

## Dependency Graph

```
W4 (Observability)  ─────────────────────────────────────────► done
W5 (Lifecycle)      ─────────────────────────────────────────► done
         │ W6 (Protobuf) ───────────────────────────────────► done
         │          │ W7 (JetStream) ───────────────────────► done
         │          │          │ W8 (Replay) ───────────────► done
         │          │          │          │ W9 (Multi-Ex) ──► done
         │          │          │          │
         ▼          ▼          ▼          ▼
      W4 + W5    W6 needs   W7 needs   W8 needs W5 (clock/seq)
      parallel   W4 metrics W6 schemas W9 needs W7 (bus) + W5
```

**Critical path:** W4 → W6 → W7 → W9
**Parallel path:** W5 (runs alongside W4)
**W8** can start after W5 completes (needs deterministic clock/seq)

---

## Week-by-Week Plan

### Week 1: W4 — Observability & Profiling (RFC-0005)

**Goal:** Full Prometheus metrics + pprof endpoints operational.

| Day | Task | Deliverable |
|-----|------|-------------|
| D1 | Create `internal/shared/metrics/` package | registry.go, metrics.go |
| D1 | Define all metric constructors (CounterVec, HistogramVec, GaugeVec) | Compilable, registered |
| D2 | Instrument `IngestMarketData` (latency histogram, messages counter) | Unit test: counter increments |
| D2 | Add `SubjectFromEnvelope()` to envelope package | Unit test: deterministic |
| D3 | Instrument Guardian (restart/degraded/rate-limited counters) | Unit test: counter increments |
| D3 | Instrument WS Consumer (connections, reconnects, messages, errors) | Unit test |
| D4 | Add drop counter to InMemoryBus | Integration test: drop counted |
| D4 | Add `/metrics` endpoint to HTTP server | Integration test: valid Prometheus format |
| D5 | Add `/debug/pprof/` endpoints (localhost-only) | Manual verification |
| D5 | Wire metrics registry in cmd/*/main.go | End-to-end: curl /metrics returns data |

**Checkpoint W4:**
- [ ] `curl localhost:8080/metrics` returns valid Prometheus exposition format
- [ ] `curl localhost:8080/debug/pprof/goroutine?debug=1` returns goroutine dump
- [ ] All metrics from PRD-0001 B.5 registered and emitting
- [ ] `go test -race ./...` green
- [ ] No perf regression > 5% (benchmark before/after ingest)

**pprof Expectations (baseline capture):**
- Record goroutine count at t=0 and t=30min with 2 tickers
- Record heap alloc at t=0 and t=30min
- These become the baseline for W5 soak test

---

### Week 2: W5 — Memory Leak Mitigation & Lifecycle Hardening (RFC-0006)

**Goal:** All state maps bounded, goroutine leaks eliminated, soak test passes.

| Day | Task | Deliverable |
|-----|------|-------------|
| D1 | Create `internal/shared/ds/boundedmap.go` (LRU+TTL) | Unit tests: insert, evict, TTL, concurrent |
| D1 | Create `internal/shared/ds/leaktest.go` | Helper: AssertNoGoroutineLeak |
| D2 | Wire BoundedMap into IngestMarketData | Test: MaxStreams eviction works |
| D2 | Wire BoundedMap into UpdateOrderBookFromEvents | Test: MaxBooks eviction works |
| D3 | Add MaxLevels to OrderBook constructor | Test: never exceeds limit |
| D3 | Audit consumer.go lifecycle (defer close(donech)) | Test: 100 connect/disconnect cycles, 0 leaks |
| D4 | Add global restart rate limiter to Guardian | Test: 6th restart denied |
| D4 | Add soak test script (`scripts/soak-test.sh`) | Script executable |
| D5 | Run 30min soak test with 200 tickers | Report: goroutine delta, heap growth |

**Checkpoint W5:**
- [ ] BoundedMap passes all unit tests including `-race`
- [ ] IngestMarketData with MaxStreams=10: evicts 11th stream
- [ ] OrderBook with MaxLevels=100: never exceeds 100 per side
- [ ] Consumer 100-cycle leak test: 0 goroutine leaks
- [ ] Guardian rate limiter: 6th restart denied within window
- [ ] Soak test (30min, 200 tickers): goroutine delta <= 5, heap growth < 10%
- [ ] `go test -race ./...` green

**pprof Validation (compare vs W4 baseline):**
- Goroutine count at t=30min: should be ≤ baseline + 5
- Heap alloc: should stabilize (growth rate → 0) within 10min
- If not: investigate with `go tool pprof heap` and fix before proceeding

---

### Week 3: W6 — Protobuf Contract Layer (RFC-0007)

**Goal:** Proto schemas defined, Buf CI checks active, generated Go code committed.

| Day | Task | Deliverable |
|-----|------|-------------|
| D1 | Install Buf, create `proto/buf.yaml`, `proto/buf.gen.yaml` | Config files |
| D1 | Create `proto/envelope/v1/envelope.proto` | Schema file |
| D2 | Create `proto/marketdata/v1/trade.proto` | Schema file |
| D2 | Create `proto/marketdata/v1/bookdelta.proto` | Schema file |
| D2 | Create `proto/marketdata/v1/markprice.proto`, `liquidation.proto` | Schema files |
| D3 | Run `buf generate`, commit generated Go code | `internal/shared/proto/gen/` |
| D3 | Create `proto/registry.json` | Schema manifest |
| D4 | Add `make proto-gen`, `make proto-lint`, `make proto-breaking` | Makefile targets |
| D4 | Add `buf lint` + `buf breaking` to CI | CI pipeline |
| D5 | Roundtrip unit tests (proto marshal/unmarshal) | Tests green |

**Checkpoint W6:**
- [ ] `buf lint` passes with 0 errors
- [ ] `buf breaking` FAILS when a field is intentionally removed (negative test)
- [ ] Generated Go code compiles
- [ ] Proto roundtrip: marshal → unmarshal → compare == identical
- [ ] `registry.json` lists all schemas with correct paths
- [ ] No runtime changes (existing behavior unchanged)

---

### Week 4: W7 — NATS JetStream Integration (RFC-0008)

**Goal:** JetStream publisher + consumer operational, crash recovery validated.

| Day | Task | Deliverable |
|-----|------|-------------|
| D1 | Create `internal/adapters/jetstream/publisher.go` | EventPublisher over JetStream |
| D1 | Create `internal/adapters/jetstream/subject.go` | Subject builder |
| D2 | Create `internal/adapters/jetstream/consumer.go` | Durable pull consumer |
| D2 | Create `internal/adapters/jetstream/config.go` | Config structs |
| D3 | Add JetStream config to `shared/config/schema.go` | Config section |
| D3 | Wire `-bus=jetstream` flag in `cmd/consumer` and `cmd/processor` | Flag parsing |
| D4 | Testcontainers integration tests (publish, consume, dedup, restart) | Tests green |
| D5 | Regression: `-bus=inmemory` still works | Existing tests pass |
| D5 | Benchmark: JetStream vs InMemoryBus throughput | Document overhead |

**Checkpoint W7:**
- [ ] Publish 1000 envelopes to JetStream: all consumed with correct ordering
- [ ] Stop/restart consumer: 0 message loss (durable consumer)
- [ ] Duplicate publish (same IdempotencyKey): silently deduped
- [ ] `-bus=inmemory` regression: all existing tests pass
- [ ] JetStream config documented in config.jsonc
- [ ] `go test -race ./...` green (including testcontainers tests)

**Soak Test (extended):**
- 60min with 200 tickers + JetStream
- Consumer count == producer count (0 message loss)
- Goroutine stable, heap stable (same thresholds as W5)

---

### Week 5: W8 — Deterministic Replay & Golden Tests (RFC-0009)

**Goal:** Replay infrastructure operational, golden tests in CI.

| Day | Task | Deliverable |
|-----|------|-------------|
| D1 | Create `internal/shared/replay/fixture.go` (FixtureWriter, FixtureReader) | Unit tests: write/read roundtrip |
| D1 | Create `internal/shared/replay/sequencer.go` (ReplaySequencer) | Unit tests |
| D2 | Create `internal/shared/replay/recorder.go` (wraps EventPublisher) | Unit tests |
| D2 | Create `internal/shared/replay/player.go` (drives replay) | Unit tests |
| D3 | Wire `-record` flag in `cmd/consumer` | Record 1000 envelopes from live stream |
| D3 | Wire `-replay` flag in `cmd/consumer` | Replay fixture file |
| D4 | Create golden tests for ingest + aggregation | Tests green |
| D4 | Add INV-R1 grep check to CI (`time.Now` in core = fail) | CI check |
| D5 | Validate determinism: replay 3x, compare outputs | All identical |

**Checkpoint W8:**
- [ ] Recorder captures envelopes to JSONL without corruption
- [ ] Player replays fixture with FakeClock + ReplaySequencer
- [ ] Golden test: replay 1000 envelopes → output matches golden file
- [ ] Replay 3x: all outputs identical (determinism proof)
- [ ] `grep time.Now internal/core/` returns 0 matches
- [ ] `-record` and `-replay` flags functional in cmd/consumer
- [ ] `go test -race ./...` green

---

### Week 6: W9 — Multi-Exchange Readiness (RFC-0010)

**Goal:** Architecture validated with 2 exchanges, zero core changes.

| Day | Task | Deliverable |
|-----|------|-------------|
| D1 | Extend Guardian: `SubsystemKey` (string) replaces `Subsystem` (enum) | Tests green |
| D1 | Update protocol messages, snapshot, ChildFailed | All references updated |
| D2 | Create Bybit adapter: endpoint builder, trade parser | Unit tests |
| D2 | Create Bybit adapter: bookdelta parser | Unit tests |
| D3 | Create Bybit hardcoded InstrumentCatalog | Unit tests |
| D3 | Add multi-exchange config to config.AppConfig | Config loading tests |
| D4 | Wire two MarketDataSubsystems in cmd/consumer | Integration test: both start |
| D4 | Test: poison one subsystem, other continues | Integration test |
| D5 | Add MEX-4 grep audit to CI | CI check |
| D5 | Cross-venue normalization validation | Unit test: Binance == Bybit canonical |

**Checkpoint W9:**
- [ ] Bybit adapter parses trade + bookdelta correctly
- [ ] Two subsystems run in same process without interference
- [ ] Poison one: other continues running
- [ ] `naming.CanonicalInstrument` same result across exchanges
- [ ] 0 exchange-specific references in `internal/core/`
- [ ] Single-exchange regression: existing tests pass
- [ ] `go test -race ./...` green

---

## Final Acceptance (all W4-W9 complete)

### Soak Test Battery

| Test | Duration | Config | Pass Criteria |
|------|----------|--------|---------------|
| 2 tickers (BTC+ETH) | 30 min | Binance, InMemoryBus | goroutines stable, heap stable, 0 gaps |
| 200+ tickers | 60 min | Binance, InMemoryBus | goroutine Δ ≤ 5, heap < 10%, drops < 0.1% |
| 200+ tickers + JetStream | 60 min | Binance, JetStream | Same + 0 message loss |
| Reconnect stress | 30 min | Force disconnect every 60s | Recovery < 5s, 0 leaks |
| Multi-exchange | 30 min | Binance + Bybit, InMemoryBus | Both stable independently |
| Replay determinism | N/A | 1000-event fixture | Byte-identical output 3x |

### Profile Validation

| Profile | Tool | Pass Criteria |
|---------|------|---------------|
| Heap | pprof | Alloc rate stabilizes within 5min |
| Goroutines | pprof | Count = baseline + N consumers + M sessions |
| CPU | pprof | Top functions: JSON parse, hash, net I/O (expected) |
| Mutex | pprof | No contention > 1% on single mutex |

### CI Pipeline

```
make lint
make test            # all modules, -race
make proto-lint      # buf lint
make proto-breaking  # buf breaking (PR only)
make golden-check    # replay golden tests
make audit-core-purity  # grep for exchange names in core
# INV-R1: grep time.Now in core
```

### SLO Validation

| SLI | SLO | How to Measure |
|-----|-----|----------------|
| Ingest latency | p50 < 1ms, p99 < 10ms | `ingest_latency_seconds` histogram |
| Recovery time | < 5s subsystem | Timer from ChildFailed to first ingest |
| Duplicates | 0 | IdempotencyKey check in consumer |
| Goroutine stability | Δ ≤ 5 in 30min | pprof goroutine |
| Heap stability | Growth < 10% in 30min | pprof heap |
| Shutdown | < 10s | SIGTERM → exit timer |

---

## W10-1.1 Hardening Evidence

**Date:** 2026-02-12

### Scope Proven

- Throttled explicit TTL sweeping added to cross-venue join state:
  - `processor.insights.sweep_every_n` (default `1024`)
  - `processor.insights.sweep_every` (default `30s`)
  - precedence: `sweep_every_n > 0` uses N-based cadence; when `sweep_every_n == 0`, duration cadence applies; `0` duration disables explicit sweep.
- Deterministic tie-break in join state update:
  - higher `seq`
  - then higher `ts_ingest`
  - then higher `ts_exchange`
- Bounded insights metrics (no instrument labels):
  - `insights_snapshots_total{venue_count_bucket}`
  - `insights_state_instruments_active`
  - `insights_state_evictions_total{reason}`
- Opt-in processor E2E gate for insights snapshot emission in test mode.

### Commands + Outputs

```bash
go test ./...
# module: internal/core/insights
ok  	github.com/market-raccoon/internal/core/insights/app
ok  	github.com/market-raccoon/internal/core/insights/domain
```

```bash
go test ./...
# module: internal/shared
ok  	github.com/market-raccoon/internal/shared/config
ok  	github.com/market-raccoon/internal/shared/contracts
ok  	github.com/market-raccoon/internal/shared/metrics
... (all packages green)
```

```bash
go test ./...
# module: internal/actors
ok  	github.com/market-raccoon/internal/actors/aggregation/runtime
... (all packages green)
```

```bash
go test -tags integration ./internal/adapters/jetstream -run TestE2EProcessorJetStream_CrossVenueJoinOptIn -count=1
ok  	github.com/market-raccoon/internal/adapters/jetstream	5.007s
```

```bash
go test -tags integration ./internal/adapters/jetstream -run TestE2EProcessorJetStream -count=1
ok  	github.com/market-raccoon/internal/adapters/jetstream	14.229s
```

```bash
make test-workspace GO_TEST_FLAGS='-race'
# PASS across workspace modules (consumer, actors, adapters, core/*, interfaces, shared)
```

```bash
GOSUMDB=off pre-commit run -a
check yaml...............................................................Passed
fix end of files.........................................................Passed
trim trailing whitespace.................................................Passed
check for merge conflicts................................................Passed
go tidy check............................................................Passed
go fmt check.............................................................Passed
go lint..................................................................Passed
go test short............................................................Passed
```

## W10-2 Spread Metrics + Signal Evidence

**Date:** 2026-02-12

### Scope Proven

- `insights.crossvenue.trade_snapshot` v1 now includes deterministic derived spread fields:
  - `min_price`, `min_price_venue`
  - `max_price`, `max_price_venue`
  - `spread_abs`, `spread_bps`, `mid_price`
- Optional event added:
  - `insights.crossvenue.spread_signal` v1
  - emitted only when:
    - `processor.insights.enable_spread_signal=true`
    - joined venues `>= processor.insights.min_venues`
    - `spread_bps >= processor.insights.min_spread_bps`
- Deterministic rounding mode added and documented:
  - `processor.insights.rounding_mode`: `half_even` (default) | `floor`
- Runtime remains opt-in:
  - default behavior unchanged (`enable_crossvenue_join=false`, `enable_spread_signal=false`)
- JSON codec registry includes snapshot + spread signal payloads (proto remains deferred for insights).

### Commands + Outputs

```bash
go test ./internal/core/insights/...
ok  	github.com/market-raccoon/internal/core/insights/app
ok  	github.com/market-raccoon/internal/core/insights/domain
```

```bash
go test ./internal/shared/config ./internal/shared/contracts ./internal/shared/codec ./internal/actors/aggregation/runtime
ok  	github.com/market-raccoon/internal/shared/config
ok  	github.com/market-raccoon/internal/shared/contracts
ok  	github.com/market-raccoon/internal/shared/codec
ok  	github.com/market-raccoon/internal/actors/aggregation/runtime
```

```bash
go test -tags integration ./internal/adapters/jetstream -run TestE2EProcessorJetStream_CrossVenueJoinOptIn -count=1
ok  	github.com/market-raccoon/internal/adapters/jetstream	4.690s
```

```bash
make test-workspace GO_TEST_FLAGS='-race'
# PASS across workspace modules (consumer, actors, adapters, core/*, interfaces, shared)
```

```bash
pre-commit run -a
check yaml...............................................................Passed
fix end of files.........................................................Passed
trim trailing whitespace.................................................Passed
check for merge conflicts................................................Passed
go tidy check............................................................Passed
go fmt check.............................................................Passed
go lint..................................................................Passed
go test short............................................................Passed
```
