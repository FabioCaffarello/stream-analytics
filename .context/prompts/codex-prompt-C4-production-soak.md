# Codex Prompt C4 — Production Soak (Multi-Exchange 10M Events, Cold-Path Roundtrip, Full System Endurance)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture. Dual storage (TimescaleDB + ClickHouse). 5 bounded contexts: MarketData, Aggregation, Delivery, Insights, Storage.

---

## Context

After C1 (proto + shard), C2 (4 exchange adapters), and C3 (backfill + gap detection + cold readers), the full pipeline is feature-complete. Existing soak coverage is good but has nine specific gaps:

### What soak tests already exist:

| Test | What it covers | Event count | Limitation |
|------|---------------|-------------|------------|
| `TestSoak_FullPipeline_1M_Messages` | AggregationService (book+candle+stats) | 1M | Single venue "binance" only, no parse step, no delivery |
| `TestSoak_WSDelivery_SlowClients` | WS delivery backpressure | 100k | Not connected to aggregation pipeline |
| `TestStoreSoak_ColdPathBurst10k_*` | Store binary ack-on-commit | ~600k | In-memory stubs, snapshot only (no candle/stats) |
| `TestStoreSoak_ColdPathLatencyBudgets` | Store latency p95/p99 | ~600k | In-memory stubs |
| `TestBuildCandle_Soak_HighCardinality` | Candle eviction | 2k | Isolated use case |
| `TestBuildStats_Soak_HighCardinality` | Stats eviction | 2k | Isolated use case |
| `TestVPVROverloadSoakBurstDeterministicBudgets` | VPVR determinism + latency | 8k | Isolated subsystem |
| `TestStorageSoak_Burst10x60s_CommitAckInvariants` | Dual-write committer | 600 | In-memory stubs |

### Gaps this wave closes:

1. **No multi-exchange parse→aggregate soak** — Existing 1M test uses synthetic requests, not real parser output from Binance+Bybit+Coinbase+HyperLiquid.
2. **No cold-path write+read roundtrip** — All store soaks use in-memory stubs. No test writes to cold storage and reads back to verify zero data loss.
3. **No combined aggregation+delivery soak** — The 1M pipeline soak and the WS delivery soak are independent.
4. **No candle/stats cold-write soak** — Store soaks only exercise snapshot routing; candle and stats handlers have no dedicated soak.
5. **No soak harness script for the full pipeline** — `make soak-check` runs WS lifecycle + VPVR only. The 1M pipeline soak requires `MR_ENABLE_SOAK=1` and has no Makefile target.

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error` in domain/app)
### Tests: soak tests behind `testing.Short()` skip + `MR_ENABLE_SOAK=1` env gate
### Memory: `runtime.GC()` + `runtime.ReadMemStats` before/after for heap delta
### Goroutines: `runtime.NumGoroutine()` before/after for leak detection
### Latency: track p50/p95/p99 using sorted-slice or histogram, assert budgets
### Fixture format: use existing exchange parser JSON fixtures for realistic payloads

---

## Task: Five-Part Production Soak

### PART 1: Multi-Exchange Pipeline Soak (10M Events)

Extends the existing `TestSoak_FullPipeline_1M_Messages` to exercise all 4 exchanges with realistic parser output.

#### Step 1.1: Multi-exchange soak test

**File:** `cmd/processor/soak_multi_exchange_test.go` (NEW)

```go
func TestSoak_MultiExchange_10M_Messages(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test in short mode")
    }
    if os.Getenv("MR_ENABLE_SOAK") != "1" {
        t.Skipf("set MR_ENABLE_SOAK=1 to run soak tests")
    }

    // 1. Create AggregationService with production-like limits:
    //    maxBooks=4096, maxCandles=50000, maxWindows=50000
    //    soakArtifactPublisher, soakHotStore, soakCandleStore, soakStatsStore

    // 2. Define exchange mix (realistic distribution):
    //    - "BINANCE" spot: 30% of traffic (trade + bookdelta)
    //    - "BINANCE" futures: 30% (trade + bookdelta + markprice + liquidation)
    //    - "BYBIT": 20% (trade + bookdelta + markprice + liquidation)
    //    - "COINBASE": 10% (trade + bookdelta, no markprice/liquidation)
    //    - "HYPERLIQUID": 10% (trade + bookdelta full snapshot, no markprice)

    // 3. For each exchange, use real parser payloads:
    //    - Pre-build arrays of realistic JSON payloads for each event type
    //    - Parse through the actual exchange parser (binance.ParseMessage, etc.)
    //    - Feed the resulting app.IngestRequest to AggregationService

    // 4. Generate 10M events across 200 instruments (50 per exchange):
    //    instruments := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", ...}
    //    For each event:
    //      a. Select exchange based on distribution
    //      b. Select instrument (round-robin within exchange)
    //      c. Select event type based on exchange capabilities
    //      d. Build payload with advancing timestamps
    //      e. Call appropriate AggregationService method

    // 5. Verify invariants:
    //    - ActiveBooks() <= maxBooks
    //    - ActiveCandles() <= maxCandles
    //    - ActiveWindows() <= maxWindows
    //    - Published candle count == expectedCandleClosed
    //    - Published stats count > 0 (stats were triggered)
    //    - Goroutine drift <= 32
    //    - Heap delta <= 1 GB (10x of 1M test budget, linear scaling)
    //    - Runtime: < 120 seconds (target: process rate > 83k events/sec)
}
```

**Key difference from existing 1M soak:** Uses real exchange parser output (not synthetic `UpdateRequest`/`BuildCandleRequest`), covers all 4 venues simultaneously, 10x scale (10M vs 1M), exercises the markprice→stats and liquidation→stats paths from all futures-capable exchanges.

#### Step 1.2: Exchange payload generators

**File:** `cmd/processor/soak_fixtures_test.go` (NEW)

Helper functions that produce realistic raw JSON payloads for each exchange:

```go
// buildBinanceTradePayload returns a raw Binance combined-stream JSON message.
func buildBinanceTradePayload(symbol string, price float64, qty float64, ts int64, tradeID int64) []byte

// buildBinanceDepthPayload returns a raw Binance depthUpdate message.
func buildBinanceDepthPayload(symbol string, bids, asks [][]string, ts int64, finalID int64) []byte

// buildBinanceMarkPricePayload returns a raw Binance markPriceUpdate message.
func buildBinanceMarkPricePayload(symbol string, markPrice, fundingRate float64, ts int64) []byte

// buildBinanceLiquidationPayload returns a raw Binance forceOrder message.
func buildBinanceLiquidationPayload(symbol string, side string, price, qty float64, ts int64) []byte

// buildBybitTradePayload returns a raw Bybit trade envelope JSON message.
func buildBybitTradePayload(symbol string, price float64, qty float64, ts int64, tradeID string) []byte

// buildBybitDepthPayload, buildBybitTickerPayload, buildBybitLiquidationPayload ...

// buildCoinbaseMatchPayload returns a raw Coinbase "match" message.
func buildCoinbaseMatchPayload(productID string, price, size float64, side string, ts time.Time, tradeID int64) []byte

// buildCoinbaseL2UpdatePayload, buildCoinbaseTickerPayload ...

// buildHyperLiquidTradePayload returns a raw HyperLiquid trades message.
func buildHyperLiquidTradePayload(coin string, side string, price, size float64, ts int64, hash string) []byte

// buildHyperLiquidL2BookPayload returns a raw HyperLiquid l2Book full snapshot.
func buildHyperLiquidL2BookPayload(coin string, bids, asks [][]string, ts int64) []byte
```

These generate valid JSON matching real exchange wire formats (based on the existing parser test fixtures). They are reused across multiple soak tests.

---

### PART 2: Cold-Path Write+Read Roundtrip Soak

Verifies that data written to cold storage can be read back correctly. Uses ClickHouse readers from C3.

#### Step 2.1: Store candle/stats cold-write soak

**File:** `cmd/store/soak_candle_stats_test.go` (NEW)

```go
func TestStoreSoak_CandleColdWrite_10k(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test in short mode")
    }

    // 1. Create 10,000 synthetic candle envelopes
    //    - 10 instruments × 1000 candle windows each
    //    - Realistic CandleV1 payloads with OHLCV data
    //    - Encode via codec.EncodePayload

    // 2. Route through handleStoreEnvelope → handleAggregationCandle
    //    - Uses in-memory ChCandleWriter (no real ClickHouse)

    // 3. Verify:
    //    - All 10,000 candles committed (store_commit_total{status="ok"} = 10000)
    //    - No decode errors (store_quarantine_total = 0)
    //    - p95 commit latency < 1ms (in-memory writer)
}

func TestStoreSoak_StatsColdWrite_10k(t *testing.T) {
    // Same pattern for stats: 10 instruments × 1000 stats windows
    // Includes nullable fields (markprice, funding rate)
}
```

#### Step 2.2: Cold-path roundtrip verification test

**File:** `internal/adapters/storage/clickhouse/roundtrip_test.go` (NEW)

```go
func TestColdPath_CandleWriteReadRoundtrip(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping roundtrip test in short mode")
    }

    // 1. Create in-memory candle writer + in-memory candle reader
    //    (both backed by the same in-memory data store)

    // 2. Write 1000 candles for 5 instruments across 3 timeframes

    // 3. Read back via GetCandleRange for each instrument/timeframe

    // 4. Verify:
    //    - Read count == write count per instrument/timeframe
    //    - All field values match (OHLCV, timestamps, seq, trade_count)
    //    - GetCandleTimestamps returns sorted unique timestamps
    //    - GetFirstCandle/GetLastCandle return correct boundary candles

    // 5. Run gap detection on the written data → expect zero gaps
}

func TestColdPath_StatsWriteReadRoundtrip(t *testing.T) {
    // Same pattern for stats with nullable field verification
}
```

**IMPORTANT:** This test does NOT require a real ClickHouse instance. Create an `inMemoryColdStore` that implements both the writer and reader interfaces, backed by a `[]domain.CandleV1` slice. This proves the reader SQL logic is correct relative to the writer logic.

---

### PART 3: Combined Aggregation + Delivery Soak

Exercises the pipeline from parse→aggregate→deliver, verifying both aggregation correctness and delivery backpressure under load.

#### Step 3.1: Pipeline + delivery combined soak

**File:** `cmd/processor/soak_pipeline_delivery_test.go` (NEW)

```go
func TestSoak_PipelineWithDelivery_100k(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping soak test in short mode")
    }
    if os.Getenv("MR_ENABLE_SOAK") != "1" {
        t.Skipf("set MR_ENABLE_SOAK=1 to run soak tests")
    }

    // 1. Create AggregationService + InMemoryBus
    // 2. Subscribe 10 "fast" consumers and 5 "slow" consumers to the bus
    // 3. Feed 100,000 trade events → candle close events are published to bus
    // 4. Slow consumers sleep 1ms per message (simulating WS write latency)

    // 5. Verify:
    //    - AggregationService invariants (books, candles bounded)
    //    - Fast consumers received all published events
    //    - Slow consumers received a subset (drops counted)
    //    - Total drops > 0 (backpressure activated)
    //    - Goroutine drift <= 48
    //    - Heap delta <= 256 MB
    //    - No panics (test completes successfully)
}
```

This closes the gap between the isolated 1M pipeline soak and the isolated WS delivery soak.

---

### PART 4: Soak Harness Scripts and Make Targets

Wire all soak tests into reproducible scripts and Makefile targets.

#### Step 4.1: Full pipeline soak script

**File:** `scripts/soak-pipeline.sh` (NEW)

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_FILE=".context/evidence/c4-pipeline-soak.txt"
# ... argument parsing (same pattern as soak-store.sh) ...

echo "# C4 pipeline soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"

# Run 10M multi-exchange soak
echo "Running multi-exchange pipeline soak (10M events)..." | tee -a "$OUT_FILE"
MR_ENABLE_SOAK=1 GOCACHE="$GO_CACHE_DIR" \
    go test ./cmd/processor -run 'TestSoak_MultiExchange_10M_Messages' \
    -count=1 -v -timeout=30m 2>&1 | tee -a "$OUT_FILE"

# Run pipeline+delivery combined soak
echo "Running pipeline+delivery combined soak (100k events)..." | tee -a "$OUT_FILE"
MR_ENABLE_SOAK=1 GOCACHE="$GO_CACHE_DIR" \
    go test ./cmd/processor -run 'TestSoak_PipelineWithDelivery_100k' \
    -count=1 -v -timeout=15m 2>&1 | tee -a "$OUT_FILE"

echo "C4 pipeline soak completed" | tee -a "$OUT_FILE"
```

#### Step 4.2: Cold-path roundtrip soak script

**File:** `scripts/soak-roundtrip.sh` (NEW)

```bash
#!/usr/bin/env bash
set -euo pipefail
# ... standard header ...

# Run cold-path roundtrip tests
echo "Running cold-path candle/stats roundtrip..." | tee -a "$OUT_FILE"
GOCACHE="$GO_CACHE_DIR" go test ./internal/adapters/storage/clickhouse \
    -run 'TestColdPath_.*Roundtrip' -count=1 -v -timeout=10m 2>&1 | tee -a "$OUT_FILE"

# Run store candle/stats cold-write soak
echo "Running store candle+stats cold-write soak..." | tee -a "$OUT_FILE"
GOCACHE="$GO_CACHE_DIR" go test ./cmd/store \
    -run 'TestStoreSoak_(Candle|Stats)ColdWrite' -count=1 -v -timeout=10m 2>&1 | tee -a "$OUT_FILE"

echo "C4 cold-path roundtrip completed" | tee -a "$OUT_FILE"
```

#### Step 4.3: Update `make soak-check` to include new soaks

**File:** `Makefile` (EXTEND)

Add new targets:

```makefile
.PHONY: soak-pipeline
soak-pipeline: ## Run multi-exchange pipeline soak (10M events, requires MR_ENABLE_SOAK=1)
	@scripts/soak-pipeline.sh

.PHONY: soak-roundtrip
soak-roundtrip: ## Run cold-path write+read roundtrip soak
	@scripts/soak-roundtrip.sh

.PHONY: soak-full
soak-full: soak-check soak-store soak-cold-path soak-roundtrip soak-pipeline ## Run ALL soak tests
```

Update `soak-check` to include roundtrip:
```makefile
soak-check: ## Run standard soak gate (WS lifecycle + VPVR + roundtrip)
	@scripts/soak-test.sh
	@scripts/soak-vpvr.sh
	@scripts/soak-roundtrip.sh
```

**NOTE:** `soak-pipeline` (10M events) is intentionally NOT in `soak-check` because it requires `MR_ENABLE_SOAK=1` and takes >2 minutes. It's available via `make soak-pipeline` or `make soak-full`.

---

### PART 5: Evidence Collection and Performance Budget Documentation

#### Step 5.1: Document performance budgets

**File:** `docs/architecture/performance-budgets.md` (NEW)

```markdown
# Performance Budgets

## Pipeline Throughput
- Target: ≥ 83,000 events/sec (10M events in < 120s)
- Measured: [to be filled after soak run]

## Memory Budgets
| Component | Budget | Soak Test |
|-----------|--------|-----------|
| 1M pipeline (single exchange) | ≤ 512 MB heap delta | TestSoak_FullPipeline_1M |
| 10M pipeline (4 exchanges) | ≤ 1 GB heap delta | TestSoak_MultiExchange_10M |
| Pipeline + delivery (100k) | ≤ 256 MB heap delta | TestSoak_PipelineWithDelivery_100k |

## Goroutine Budgets
| Component | Max Drift | Soak Test |
|-----------|-----------|-----------|
| Pipeline (no delivery) | ≤ 32 | TestSoak_FullPipeline_1M |
| Pipeline (4 exchanges) | ≤ 48 | TestSoak_MultiExchange_10M |
| Pipeline + delivery | ≤ 48 | TestSoak_PipelineWithDelivery_100k |
| WS delivery (50 clients) | ≤ 96 | TestSoak_WSDelivery_SlowClients |

## Latency Budgets
| Path | p95 | p99 | Source |
|------|-----|-----|--------|
| Ingest (parse→envelope) | ≤ 500 µs | ≤ 1 ms | PRD-0001 |
| E2E (ingest→orderbook snapshot) | ≤ 15 µs/op | — | BenchmarkE2E_IngestToOrderbookSnapshot |
| E2E (trade→candle) | ≤ 20 µs/op | — | BenchmarkE2E_TradeToCandle |
| Cold-path commit | ≤ 10 ms | ≤ 25 ms | TestStoreSoak_ColdPathLatencyBudgets |
| VPVR policy decision | ≤ 2 ms | ≤ 5 ms | TestVPVROverloadSoakBurstDeterministic |

## Cardinality Budgets
| Resource | Max | Enforced By |
|----------|-----|-------------|
| Active orderbooks | 4,096 | BoundedMap eviction |
| Active candles | 50,000 | BoundedMap eviction |
| Active stats windows | 50,000 | BoundedMap eviction |
| Active instrument streams | 4,096 | BoundedMap eviction |
```

#### Step 5.2: Evidence directory for soak results

All soak scripts write evidence to `.context/evidence/`. After running the full soak suite, the evidence directory should contain:

```
.context/evidence/
├── c4-pipeline-soak.txt      (10M multi-exchange + delivery combined)
├── c4-cold-roundtrip.txt     (write+read roundtrip)
├── s3-store-soak.txt         (store binary soaks)
├── w5-soak.txt               (WS lifecycle + bounded map)
├── vpvr-soak.txt             (VPVR determinism)
└── cold-path-soak.txt        (dual-write committer)
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `cmd/processor/soak_pipeline_test.go` | Existing 1M soak — extend pattern for 10M |
| `cmd/store/soak_test.go` | Existing store soaks — extend with candle/stats |
| `internal/interfaces/ws/soak_delivery_test.go` | Existing WS delivery soak — reference for combined test |
| `internal/core/aggregation/app/build_candle_soak_test.go` | Candle eviction soak |
| `internal/core/aggregation/app/build_stats_soak_test.go` | Stats eviction soak |
| `internal/adapters/storage/soak_commit_ack_test.go` | Dual-write committer soak |
| `internal/actors/insights/runtime/vpvr_soak_test.go` | VPVR determinism soak |
| `internal/adapters/exchange/binance/parser.go` | Binance parser for payload generation |
| `internal/adapters/exchange/bybit/parser.go` | Bybit parser |
| `internal/adapters/exchange/coinbase/parser.go` | Coinbase parser |
| `internal/adapters/exchange/hyperliquid/parser.go` | HyperLiquid parser |
| `internal/adapters/storage/clickhouse/candle_reader.go` | ClickHouse candle reader (C3) |
| `internal/adapters/storage/clickhouse/stats_reader.go` | ClickHouse stats reader (C3) |
| `internal/core/aggregation/ports/readers.go` | Cold-path read ports (C3) |
| `scripts/soak-store.sh` | Existing soak script — reference pattern |
| `scripts/soak-test.sh` | Existing soak script |
| `Makefile` | Soak targets to extend |
| `.benchmarks/baseline.txt` | Performance baselines |
| `docs/architecture/metrics-budget-label-policy.md` | Metrics cardinality policy |

---

## Execution Rules

```bash
# Standard gates after each part:
make test-workspace
make test-workspace-race
make docs-check
make invariants-check

# Soak gates (after all 5 parts):
make soak-check           # includes roundtrip now
make soak-store           # candle+stats cold-write
make soak-pipeline        # 10M multi-exchange (MR_ENABLE_SOAK=1 required)

# Full soak (all tests):
make soak-full
```

### STOP CONDITIONS:
- 10M soak exceeding 120 seconds (throughput regression)
- Heap delta exceeding budget (512MB for 1M, 1GB for 10M)
- Goroutine drift exceeding budget (32 for pipeline, 48 for combined)
- Cold-path roundtrip data mismatch (write≠read)
- Candle/stats store soak producing decode errors
- Any soak test producing a race condition under `-race`
- Exchange payload generators producing invalid JSON (parser returns skip=true)
- Evidence files not written to `.context/evidence/`

### Commit sequence:
```
test(c4): add multi-exchange 10M pipeline soak with realistic parser payloads

- TestSoak_MultiExchange_10M_Messages: 4 exchanges, 200 instruments, 10M events
- Exchange payload generators for Binance/Bybit/Coinbase/HyperLiquid
- Realistic traffic distribution: 30/30/20/10/10 by exchange
- Covers trade, bookdelta, markprice, liquidation across all capable exchanges
- Invariants: bounded books/candles/stats, goroutine drift ≤ 48, heap ≤ 1GB

test(c4): add cold-path write+read roundtrip and store candle/stats soaks

- TestColdPath_CandleWriteReadRoundtrip: in-memory write → read → verify
- TestColdPath_StatsWriteReadRoundtrip: nullable field verification
- TestStoreSoak_CandleColdWrite_10k: 10 instruments × 1000 windows
- TestStoreSoak_StatsColdWrite_10k: full stats routing coverage

test(c4): add combined aggregation+delivery soak

- TestSoak_PipelineWithDelivery_100k: aggregation → bus → fast+slow consumers
- Verifies backpressure activation, fast consumer completeness, bounded resources

feat(c4): add soak harness scripts and make targets

- scripts/soak-pipeline.sh: 10M multi-exchange + combined delivery
- scripts/soak-roundtrip.sh: cold-path roundtrip verification
- make soak-pipeline, make soak-roundtrip, make soak-full
- make soak-check updated to include roundtrip
- Evidence files written to .context/evidence/

docs(c4): document performance budgets and soak coverage matrix

- docs/architecture/performance-budgets.md: throughput, memory, goroutine, latency budgets
- Cardinality budgets for active resources

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **All soak tests behind `testing.Short()`** — Must not slow CI. Heavy soaks (10M, combined delivery) also require `MR_ENABLE_SOAK=1`.
2. **No real infrastructure required** — All soaks use in-memory stubs for storage. Integration tests with real ClickHouse/NATS remain separate (`//go:build integration`).
3. **Realistic payloads** — Payload generators must produce valid JSON matching real exchange wire formats. Verify by parsing through the actual exchange parser before feeding to AggregationService.
4. **Evidence collection** — All soak scripts must write evidence to `.context/evidence/` with timestamps. This creates an auditable trail.
5. **Budget assertions are hard failures** — If a soak exceeds its memory/goroutine/throughput budget, the test MUST fail (not just warn).
6. **10M soak timeout** — Set `-timeout=30m` on the go test command. The target is <120s runtime, but allow generous timeout for CI variability.
7. **In-memory roundtrip tests** — Cold-path roundtrip tests use `inMemoryColdStore` implementing both writer and reader interfaces. This tests logic correctness without requiring a real database.
8. **No new `go.mod` files** — All new test files live in existing modules.
9. **Soak-full is opt-in** — `make soak-full` runs everything but is not part of `make ci`. Operators run it before releases.
10. **Goroutine/memory measurement protocol** — Always: `runtime.GC()` → `runtime.ReadMemStats()` before AND after the hot loop. Goroutine count via `runtime.NumGoroutine()`. This ensures consistent measurement.
