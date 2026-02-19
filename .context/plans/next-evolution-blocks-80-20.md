# Market-Raccoon — Next Evolution Blocks (80/20)

**Status:** Proposal
**Date:** 2026-02-17
**Authors:** Principal Architect + Staff SRE
**Authority:** `docs/architecture/TRUTH-MAP.md`, `docs/architecture/system-invariants.md`

---

## 1. State of the System (10 lines)

- **Boring and proven:** Foundation (problem/result/codec/envelope/hash/naming), actor supervision (Guardian+policies), deterministic replay (golden fixtures+SHA256 verification), policykit overload engine (L0–L3, soak-tested), subject registry governance, ACK-on-commit boundary (`internal/adapters/storage/committer.go:72`), Binance ingest pipeline (WS→parse→sequence→publish), InMemoryBus fan-out, Prometheus SLO burn-rate alerts.
- **Boring and proven (ops):** Docker Compose orchestration with health gates, 4 executables with readiness probes, JSONC config+validation, domain isolation scripts, `make ci-local` chain, 50+ Makefile targets.
- **Production unknowns:**
  - Cold-path ClickHouse: only skeleton schemas (`sql/clickhouse/migrations/0002*.sql`), no batch pipeline, no retention policy, no query path under load.
  - Shard-aware consumer topology: `internal/adapters/jetstream/shard.go` is tested but **not wired** into `cmd/consumer` or `cmd/processor`. Single consumer = throughput ceiling.
  - No committed p95/p99 baselines — `scripts/bench-check.sh` runs benchmarks but no baseline file exists in `testdata/benchmarks/`.
  - Protobuf hot-path activation stalled at W6-1 (ADR-0016 still Proposed); runtime publish/consume still JSON.
  - Second exchange (Bybit) is skeleton only (`internal/adapters/exchange/bybit/`).

---

## 2. Top 5 Evolution Blocks (80/20)

### Block 1 — Cold-Path ClickHouse Maturation

**Goal:** Production-grade durable storage with batch writes, retention, and read-back under load.

**Why 80/20:**
- Without working cold-path, the system has zero durability beyond JetStream retention (24h/10GB).
- Schema + committer already exist; gap is batch writer, retention policy, and soak under realistic throughput.
- Unlocks replay-from-cold (disaster recovery) and historical queries (insights V2).

**Primary risks mitigated:**
- Data loss beyond JetStream retention window.
- Unbounded disk growth without TTL/partitioning.
- ACK-before-commit regression under batch write failure.

**Where in repo:**
- `internal/adapters/storage/clickhouse/writer.go` — current skeleton writer
- `internal/adapters/storage/committer.go` — dual-path commit + ACK boundary
- `sql/clickhouse/migrations/` — schema DDL (v1, v2)
- `internal/adapters/storage/soak_commit_ack_test.go` — existing soak harness
- `deploy/clickhouse/` — server config
- `deploy/observability/prometheus/alerts.rules.yml` — cold-path alerts (commit latency, errors)

**DoD:**
- [ ] Batch writer with configurable flush interval and batch size (≤1000 rows or ≤500ms)
- [ ] `ReplacingMergeTree` partitioned by `toYYYYMMDD(ts_ingest)` with 30-day TTL
- [ ] Soak test: 10k events/sec sustained for 60s, zero ACK-before-commit violations
- [ ] p95 commit latency ≤ 10ms measured via `processor_commit_latency_seconds` histogram
- [ ] `make soak-cold-path` gate green with new batch writer
- [ ] Read-back query for latest N snapshots per instrument (hot-cache miss path)

**Non-goals:**
- Analytical query engine or OLAP views (V2).
- Multi-datacenter replication.

---

### Block 2 — Shard-Aware Consumer/Processor Topology

**Goal:** Horizontal scaling via JetStream consumer groups partitioned by `(venue, instrument)`.

**Why 80/20:**
- `shard.go` (FNV-1a partitioning) is fully tested with golden values and partition invariants — the hard part is done.
- Wiring into `cmd/consumer` and `cmd/processor` is config-driven (shard index + group count).
- Immediately doubles throughput ceiling per additional instance.

**Primary risks mitigated:**
- Single-consumer SPOF under multi-instrument load.
- OrderBook aggregate consistency across shards (same instrument always same shard — guaranteed by `ShardKey`).
- Uneven load distribution (FNV-1a + golden tests cover distribution).

**Where in repo:**
- `internal/adapters/jetstream/shard.go` — ShardKey, ShardGroup, topology filter
- `internal/adapters/jetstream/shard_test.go` — partition invariants + golden values
- `internal/adapters/jetstream/consumer.go` — needs shard-filter injection
- `cmd/consumer/main.go` — needs `--shard-index` / `--shard-count` flags
- `cmd/processor/main.go` — same
- `internal/shared/config/schema.go` — needs `Shard` config block
- `deploy/compose/docker-compose.yml` — needs `scale: N` support

**DoD:**
- [ ] `AppConfig.Shard` block: `{ "index": 0, "count": 1 }` (default = no sharding)
- [ ] Consumer/Processor filter subjects to own shard via `subjectBelongsToOtherShard`
- [ ] Integration test: 2 consumers, 10 instruments, all events delivered exactly once
- [ ] Replay golden test: sharded replay produces identical output to non-sharded
- [ ] `docker compose up --scale consumer=2 --scale processor=2` works with health gates
- [ ] Zero OrderBook corruption under concurrent shard operation (race test)

**Non-goals:**
- Dynamic rebalancing (static partition assignment is sufficient for M1).
- Auto-scaling based on lag metrics.

---

### Block 3 — Locked Performance Baselines + Regression Gates

**Goal:** Committed p95/p99 budgets with CI gate that fails on regression.

**Why 80/20:**
- `scripts/bench-check.sh` already runs benchmarks + benchstat comparison — only missing committed baseline file.
- Hot-path benchmarks exist: codec decode, policykit apply, hash, ingest, orderbook delta.
- One baseline file + one threshold = permanent regression protection.

**Primary risks mitigated:**
- Silent latency regression from codec/allocation changes.
- Memory growth from unbounded maps (already capped but needs budget assertion).
- Ingest throughput degradation from upstream dependency changes.

**Where in repo:**
- `scripts/bench-check.sh` — benchmark runner + regression detection (15% threshold)
- `internal/shared/codec/bench_hotpath_test.go` — envelope decode benchmark
- `internal/shared/policykit/bench_applier_test.go` — policy apply benchmark
- `internal/shared/hash/bench_hotpath_test.go` — idempotency hash benchmark
- `internal/core/marketdata/app/ingest_bench_test.go` — ingest pipeline benchmark
- `internal/core/aggregation/domain/orderbook_bench_test.go` — delta apply benchmark
- `testdata/benchmarks/` — **empty** (needs baseline)

**DoD:**
- [ ] `testdata/benchmarks/baseline.txt` committed with 5-run benchstat output
- [ ] `make bench-gate` target that fails CI if any hot-path benchmark regresses >10%
- [ ] Add `BenchmarkE2E_IngestToOrderbook` (ingest → bus → aggregation → hash, full pipeline)
- [ ] Memory allocation budget: ingest ≤ 4 allocs/op, codec decode ≤ 2 allocs/op
- [ ] Document budgets in `docs/perf/month1-baseline.md` with metric→file mapping

**Non-goals:**
- Micro-optimizing individual benchmarks before baseline is locked.
- Latency profiling under contention (Block 2 prerequisite).

---

### Block 4 — Protobuf Hot-Path Activation

**Goal:** JetStream publish/consume uses protobuf wire format; JSON fallback for replay/debug.

**Why 80/20:**
- Proto schemas exist (`proto/marketdata/v1/`, `proto/envelope/v1/`), codegen works (`make proto-gen`), dual-codec infra in `internal/shared/codec/` ready.
- `internal/shared/contracts/` has payload registry + authority manifest + converter completeness tests.
- ADR-0016 boundary rule (proto only in `shared/`, never in `core/`) is already enforced by `make invariants-check`.
- Estimated 40-60% reduction in hot-path allocation from JSON→proto on envelope codec.

**Primary risks mitigated:**
- JSON allocation pressure on GC under burst (current bottleneck).
- Schema drift between JSON payloads and proto definitions (registry gate covers).
- Replay determinism break (dual-codec must produce identical domain objects).

**Where in repo:**
- `proto/` — 6 proto definitions
- `internal/shared/proto/gen/` — generated Go code
- `internal/shared/codec/proto_codec.go` — proto codec implementation
- `internal/shared/contracts/payload_registry.go` — content-type routing
- `internal/shared/contracts/converter_completeness_test.go` — round-trip validation
- `internal/shared/contracts/authority_manifest.go` — schema authority
- `internal/adapters/jetstream/publisher.go` — needs proto marshal path

**DoD:**
- [ ] `envelope.ContentType` = `application/x-protobuf` on JetStream publish path
- [ ] Consumer/Processor auto-detect content-type and decode accordingly
- [ ] Golden replay test: proto-encoded fixture produces bit-identical domain output vs JSON fixture
- [ ] Benchmark: proto codec ≤ 50% allocs/op vs JSON codec (validated by `make bench-gate`)
- [ ] `make proto-breaking` gate remains green
- [ ] ADR-0016 status updated to Accepted

**Non-goals:**
- Migrating WS delivery to protobuf (Block 5 / delivery concern, opt-in per session).
- CBOR support (deferred; proto supersedes this path).

---

### Block 5 — Multi-Exchange Production Path (Bybit)

**Goal:** Second exchange live with identical pipeline guarantees (determinism, replay, sharding).

**Why 80/20:**
- Normalization layer (`naming.Canonical*`, `ADR-0017` invariants MEX-1→MEX-5) is designed and tested for Binance.
- `internal/adapters/exchange/bybit/` skeleton exists with parser structure.
- Consumer already parameterized by exchange config; Bybit needs adapter + golden fixtures.
- Proves the system is genuinely multi-exchange, not a Binance wrapper.

**Primary risks mitigated:**
- Architectural assumptions baked to single-exchange semantics.
- Canonical identity collision between venues (MEX-1 determinism invariant).
- Replay non-determinism from venue-specific timestamp/sequence semantics.

**Where in repo:**
- `internal/adapters/exchange/bybit/` — parser + endpoint skeleton
- `internal/adapters/exchange/binance/` — reference implementation
- `internal/core/marketdata/domain/instrument_stream.go` — stream identity (venue+instrument+market_type)
- `docs/adrs/ADR-0017-multi-exchange-normalization.md` — invariants
- `scripts/check-domain-isolation.sh` — MEX-4 guard
- `cmd/consumer/e2e_consumer_integration_test.go` — integration harness

**DoD:**
- [ ] Bybit parser for `aggTrade` + `depth` WS streams (matching Binance parity)
- [ ] Golden fixture: `testdata/golden/bybit-ingest-1000.jsonl` with SHA256 verification
- [ ] Replay test: Bybit fixture through full pipeline (ingest → aggregate → snapshot)
- [ ] MEX-1→MEX-5 invariant tests pass for Bybit canonical keys
- [ ] Config: `exchange: bybit` in `deploy/configs/consumer.jsonc` template
- [ ] No Binance-specific assumptions in `internal/core/*` (enforced by `make invariants-check`)

**Non-goals:**
- Bybit perpetual/options markets (spot only for M1).
- Cross-exchange arbitrage insights (V2 scope).

---

## 3. Ordering / Sequencing

```
Block 3 ──→ Block 1 ──→ Block 2 ──→ Block 4 ──→ Block 5
(baselines)  (storage)   (sharding)   (proto)     (bybit)
```

**Recommended order: 3 → 1 → 2 → 4 → 5**

| Step | Block | Rationale |
|------|-------|-----------|
| 1st  | **3 — Baselines** | Zero-cost gate; everything after needs regression detection. Without baselines, Blocks 1/2/4 could regress hot-path without detection. |
| 2nd  | **1 — Cold-Path** | Storage is the #1 production blocker. Soak harness already exists; work is batch writer + retention + read-back. |
| 3rd  | **2 — Sharding** | Depends on baselines (to measure per-shard throughput) and benefits from cold-path (sharded consumers need durable commit). |
| 4th  | **4 — Protobuf** | Needs baselines locked (Block 3) to prove allocation improvement. Needs sharding (Block 2) because proto reduces per-message cost, amplifying shard throughput. |
| 5th  | **5 — Bybit** | Needs sharding (new venue = new partition keys), proto (wire format stable), and cold-path (bybit data must persist). |

**Dependencies:**
- Block 2 soft-depends on Block 1 (sharded processor needs commit path)
- Block 4 hard-depends on Block 3 (need baseline to prove improvement)
- Block 5 hard-depends on Block 2 + Block 4 (multi-venue needs stable partition + wire format)
- Block 3 has **zero** dependencies — start immediately

---

## 4. Performance + Scalability Hard Numbers

### Budgets

| Metric | Budget | Where to Measure | Gate |
|--------|--------|------------------|------|
| **Ingest p95** | ≤ 500 µs (WS parse → envelope published) | `ingest_latency_seconds{quantile="0.95"}` on consumer:8081/metrics | `make bench-gate` (BenchmarkIngest) |
| **Ingest p99** | < 10 ms (PRD-0001 authority; ≤ 2 ms stretch goal) | `ingest_latency_seconds{quantile="0.99"}` | `make bench-gate` + soak (`scripts/soak-test.sh`) |
| **Delivery WS p95** | ≤ 5 ms (envelope → WS frame sent) | `ws_send_latency_seconds{quantile="0.95"}` on server:8080/metrics | Recording rule `slo:delivery:latency:burn_rate` → alert on >250ms |
| **Delivery WS p99** | ≤ 15 ms | `ws_send_latency_seconds{quantile="0.99"}` | Same SLO burn-rate alert |
| **Cold-path write p95** | ≤ 10 ms (commit hot+cold) | `processor_commit_latency_seconds{quantile="0.95"}` on processor:8082/metrics | `make soak-cold-path` (TestStorageSoak) |
| **Cold-path write p99** | ≤ 50 ms | `processor_commit_latency_seconds{quantile="0.99"}` | Alert: `ColdPathCommitLatencyHigh` in `alerts.rules.yml` |
| **Memory budget (consumer)** | ≤ 256 MB heap under burst (50 instruments × 100 msg/s) | `go_memstats_heap_inuse_bytes` | Alert: `RuntimeHeapHigh` (>500MB) in `alerts.rules.yml` |
| **Goroutine budget** | ≤ 500 goroutines (consumer), ≤ 300 (processor) | `go_goroutines` | Alert: `RuntimeGoroutinesHigh` (>10k) in `alerts.rules.yml`; soak: `TestConsumerLifecycle_*` |
| **Replay throughput** | ≥ 50k events/sec (golden replay from fixture) | `make test-replay-golden` wall-clock / fixture line count | `make bench-gate` (add BenchmarkReplay if needed) |

### Allocation Budgets (per hot-path op)

| Operation | Max allocs/op | Benchmark file |
|-----------|---------------|----------------|
| Envelope decode (JSON) | 4 | `internal/shared/codec/bench_hotpath_test.go` |
| Envelope decode (proto) | 2 | Same (after Block 4) |
| PolicyKit ApplySingle | 1 | `internal/shared/policykit/bench_applier_test.go` |
| HashFields (5 fields) | 1 | `internal/shared/hash/bench_hotpath_test.go` |
| OrderBook ApplyDelta (1000 levels) | 0 | `internal/core/aggregation/domain/orderbook_bench_test.go` |
| Ingest 1000 envelopes (amortized) | 6/envelope | `internal/core/marketdata/app/ingest_bench_test.go` |

---

## 5. Top 3 Most Dangerous Regressions

### Regression 1 — ACK Before Commit

**What:** JetStream message acknowledged before both hot + cold storage writes succeed.
**Impact:** Silent data loss; messages removed from JetStream but not persisted.
**Guard:**
- `internal/adapters/storage/committer.go:72` — `CommitAndAck` enforces order
- `internal/adapters/storage/soak_commit_ack_test.go` — `TestStorageSoak_Burst10x60s_CommitAckInvariants`
- `internal/adapters/storage/vpvr_overload_integration_test.go` — `TestIntegrationVPVROverload_AckBoundarySafeAndDeterministic`
- **Gate:** `make soak-cold-path`

### Regression 2 — SLO Burn-Rate OR-Condition in Alerts

**What:** Alert expression accidentally uses `or` instead of `and` between fast-burn + slow-burn conditions, causing false-positive pages.
**Impact:** Alert fatigue → real incidents missed; or conversely, suppressed real fires.
**Guard:**
- `deploy/observability/prometheus/alerts.rules.yml` — all SLO alerts use `and` composition
- `deploy/observability/prometheus/tests.yml` — Prometheus unit tests for alert rules
- **Gate:** `promtool test rules deploy/observability/prometheus/tests.yml` (should be in `make ci-local`)

### Regression 3 — Registry ↔ Contracts ↔ Runtime Drift

**What:** New subject added to code but not to `subject-registry.yaml`, or proto schema changed without `proto-breaking` gate.
**Impact:** Consumers silently drop unknown subjects; replay fixtures become non-deterministic.
**Guard:**
- `scripts/check-registry.sh` — validates registry vs runtime subjects
- `internal/shared/contracts/authority_test.go:268` — contract authority completeness
- `internal/adapters/jetstream/subject_validation.go` — runtime subject filter
- `make proto-breaking` — detects backwards-incompatible proto changes
- **Gate:** `make contract-gates` (registry → replay → proto)

---

## 6. Recommendation for Codex Execution Plan

### Plan A — Block 3 (Locked Baselines)

**Commits:**

| # | Commit | Objective |
|---|--------|-----------|
| 3-1 | `perf(bench): commit hot-path baseline file` | Run 5× benchstat, commit `testdata/benchmarks/baseline.txt` |
| 3-2 | `feat(bench): add E2E ingest→orderbook pipeline benchmark` | Single benchmark covering parse→sequence→publish→aggregate→hash |
| 3-3 | `ci(gate): add make bench-gate with 10% regression threshold` | New Makefile target; fails if any benchmark regresses >10% vs baseline |
| 3-4 | `docs(perf): lock allocation budgets in month1-baseline.md` | Document allocs/op budgets per operation with file:line references |

**Gates per commit:**
- 3-1: `make test-workspace` green, baseline file parseable by `benchstat`
- 3-2: `make bench-hotpath` includes new benchmark, green
- 3-3: `make bench-gate` passes against committed baseline
- 3-4: `make docs-check` green

**Tests to add:**
- `TestBenchmarkBaselineFileExists` — ensures `testdata/benchmarks/baseline.txt` is committed and non-empty
- `BenchmarkE2E_IngestToOrderbook` — full pipeline benchmark (new)
- Update `scripts/bench-check.sh` to also check allocs/op regression (not just ns/op)

---

### Plan B — Block 1 (Cold-Path ClickHouse)

**Commits:**

| # | Commit | Objective |
|---|--------|-----------|
| 1-1 | `feat(storage): batch ClickHouse writer with flush interval` | Configurable batch size (1000) + flush interval (500ms) + context-aware shutdown |
| 1-2 | `feat(storage): add TTL partition and retention DDL` | Migration `0003_partitioned_ttl.sql`: `PARTITION BY toYYYYMM(ts_ingest)`, 30-day TTL |
| 1-3 | `feat(storage): read-back query for latest snapshots` | `LatestSnapshots(venue, instrument, limit)` query via ClickHouse reader |
| 1-4 | `test(soak): cold-path batch soak 10k/s for 60s` | Extend `soak_commit_ack_test.go` with batch writer, validate zero ACK-before-commit |
| 1-5 | `ci(gate): cold-path p95 ≤ 10ms assertion in soak` | Soak test asserts `p95 ≤ 10ms`, `p99 ≤ 50ms` from histogram |

**Gates per commit:**
- 1-1: `make test-workspace` green, `make invariants-check` green
- 1-2: ClickHouse container starts with new migration, `make up-infra` green
- 1-3: Integration test with testcontainers-go ClickHouse (existing pattern in `storage_integration_test.go`)
- 1-4: `make soak-cold-path` green with batch writer
- 1-5: `make soak-cold-path` fails if latency budget exceeded

**Tests to add:**
- `TestBatchWriter_FlushOnSize` — batch flushes at configured size
- `TestBatchWriter_FlushOnInterval` — batch flushes on timer
- `TestBatchWriter_ShutdownFlushesRemaining` — graceful drain on context cancel
- `TestBatchWriter_AckOnlyAfterFlush` — ACK-on-commit invariant with batch semantics
- `TestLatestSnapshots_ReturnsCorrectOrder` — read-back correctness
- Extend `TestStorageSoak_Burst10x60s_CommitAckInvariants` for batch path
