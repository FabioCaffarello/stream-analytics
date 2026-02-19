# Codex Prompt C3 — Operational Tooling (Backfill, Gap Detection, Cold-Path Readers)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture. Dual storage (TimescaleDB + ClickHouse). 5 bounded contexts: MarketData, Aggregation, Delivery, Insights, Storage.

---

## Context

After C1 (proto + shard + bench) and C2 (4 exchange adapters), the real-time pipeline is production-capable. However, the project has **no operational tooling for historical data**:

1. **`cmd/backfill/` is a stub** — Only a `go.mod` exists. No source code. MarketMonkey has a backfill tool that downloads historical agg trades from Binance's data archive (`data.binance.vision`) and feeds them through the processor pipeline. Raccoon needs an equivalent that leverages its superior replay infrastructure.

2. **No cold-path read ports** — All storage ports (`HotReadModelStore`, `ColdReadModelStore`, `CandleHotReadModelStore`, `StatsHotReadModelStore`) are **write-only**. The only read port is `delivery/ports.RangeStore.GetRange()` for WS delivery. There are zero ClickHouse SELECT queries in the entire codebase. Any gap detection or data verification tool needs read interfaces.

3. **No gap detection tool** — Depth sequence gaps are detected in the marketdata subsystem telemetry (`telemetry.recordDepthSequence`) but only logged. There is no tool to query cold storage for missing candle windows or inconsistent stats coverage.

4. **Replay infrastructure is mature** — `internal/shared/replay/` has a full JSONL fixture format with SHA-256 checksums, `Player` with `FakeClock` + `ReplaySequencer`, `RecorderPublisher`, `WriteFixtureFromEnvelopes()`. The consumer already supports `--replay` mode via `cfg.MarketData.ReplayPath`. The backfill strategy should **produce fixture files** that the existing replay mechanism consumes.

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error` in domain/app)
### Results: `result.Result[T]` for usecase returns
### Import order: stdlib → external → monorepo
### Tests: table-driven, deterministic (use `clock.FakeClock`)
### Normalization: `naming.CanonicalVenue/CanonicalInstrument` at domain boundary
### Existing replay contract: JSONL with `envelope.Envelope` + `payload_json` + `sha256` per line

---

## Task: Three-Part Operational Tooling

### PART 1: Cold-Path Read Ports and Adapters

This part creates read interfaces and ClickHouse query implementations. These are the foundation for gap detection (Part 3) and future data verification tools.

#### Step 1.1: Define cold-path read port interfaces

**File:** `internal/core/aggregation/ports/readers.go` (NEW)

```go
package ports

import (
    "context"
    "github.com/market-raccoon/internal/core/aggregation/domain"
    "github.com/market-raccoon/internal/shared/problem"
)

// CandleRange represents a time-bounded candle query result.
type CandleRange struct {
    Venue      string
    Instrument string
    Timeframe  string
    FromMs     int64
    ToMs       int64
}

// CandleReader queries cold candle storage for historical data.
type CandleReader interface {
    // GetCandleRange returns candles in [fromMs, toMs] ordered by window_start ASC.
    GetCandleRange(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64, limit int) ([]domain.CandleV1, *problem.Problem)

    // GetCandleTimestamps returns only window_start timestamps for gap detection.
    // Much cheaper than GetCandleRange — avoids transferring full candle data.
    GetCandleTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem)

    // GetFirstCandle returns the earliest candle by window_start.
    GetFirstCandle(ctx context.Context, venue, instrument, timeframe string) (*domain.CandleV1, *problem.Problem)

    // GetLastCandle returns the latest candle by window_start.
    GetLastCandle(ctx context.Context, venue, instrument, timeframe string) (*domain.CandleV1, *problem.Problem)
}

// StatsReader queries cold stats storage for historical data.
type StatsReader interface {
    // GetStatsTimestamps returns only window_start timestamps for gap detection.
    GetStatsTimestamps(ctx context.Context, venue, instrument, timeframe string, fromMs, toMs int64) ([]int64, *problem.Problem)

    // GetFirstStats returns the earliest stats window by window_start.
    GetFirstStats(ctx context.Context, venue, instrument, timeframe string) (*domain.StatsWindowV1, *problem.Problem)

    // GetLastStats returns the latest stats window by window_start.
    GetLastStats(ctx context.Context, venue, instrument, timeframe string) (*domain.StatsWindowV1, *problem.Problem)
}

// SnapshotReader queries cold orderbook snapshot storage.
type SnapshotReader interface {
    // GetSnapshotTimestamps returns snapshot timestamps for gap detection.
    GetSnapshotTimestamps(ctx context.Context, venue, instrument string, fromMs, toMs int64) ([]int64, *problem.Problem)
}
```

#### Step 1.2: ClickHouse candle reader

**File:** `internal/adapters/storage/clickhouse/candle_reader.go` (NEW)

```go
package clickhouse

// ChCandleReader implements ports.CandleReader against ClickHouse cold storage.
type ChCandleReader struct {
    pool *Pool
}

func NewChCandleReader(pool *Pool) *ChCandleReader
```

SQL for `GetCandleTimestamps`:
```sql
SELECT window_start FROM aggregation_candle_cold
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
```

SQL for `GetCandleRange`:
```sql
SELECT venue, instrument, timeframe, window_start, window_end,
       open_price, high_price, low_price, close_price,
       volume, buy_volume, sell_volume, trade_count, seq_first, seq_last
FROM aggregation_candle_cold
WHERE venue = ? AND instrument = ? AND timeframe = ?
  AND window_start >= ? AND window_start <= ?
ORDER BY window_start ASC
LIMIT ?
```

SQL for `GetFirstCandle` / `GetLastCandle`:
```sql
SELECT ... FROM aggregation_candle_cold
WHERE venue = ? AND instrument = ? AND timeframe = ?
ORDER BY window_start ASC  -- (DESC for GetLast)
LIMIT 1
```

**IMPORTANT:** ClickHouse uses `ReplacingMergeTree`, so duplicates may exist until `OPTIMIZE FINAL`. Use `FINAL` keyword in SELECT or accept duplicates (the reader should deduplicate by `(venue, instrument, timeframe, window_start)` in application code, or simply accept that `ReplacingMergeTree` handles it lazily).

#### Step 1.3: ClickHouse stats reader

**File:** `internal/adapters/storage/clickhouse/stats_reader.go` (NEW)

Same pattern as candle reader, querying `aggregation_stats_cold` table.

#### Step 1.4: ClickHouse snapshot reader

**File:** `internal/adapters/storage/clickhouse/snapshot_reader.go` (NEW)

Queries `aggregation_orderbook_snapshot_cold` for timestamp-only gap detection.

#### Step 1.5: In-memory readers for tests

**File:** `internal/adapters/storage/clickhouse/candle_reader_test.go` (NEW)

Create an in-memory candle reader for unit tests (same pattern as the in-memory `Writer`). The test should verify:
- GetCandleTimestamps returns sorted timestamps in range
- GetFirstCandle/GetLastCandle boundary conditions
- Empty results when no data matches

**Do NOT add testcontainers** — ClickHouse reader tests use in-memory stubs. Real ClickHouse integration is tested via the existing soak tests.

---

### PART 2: Backfill Binary (`cmd/backfill/`)

The backfill tool downloads historical aggregate trade data from exchange archives and produces JSONL fixture files compatible with the existing replay infrastructure. The operator then replays fixtures through the normal consumer pipeline.

**Design rationale:** Producing fixtures (not writing to DB directly) is intentional:
- Leverages the mature replay infrastructure (Player, FakeClock, ReplaySequencer)
- Fixtures are deterministic, replayable, and auditable (SHA-256 per record)
- Same fixture can be replayed multiple times, replayed through different configs, or archived
- Separation of concerns: download is a pure data acquisition step, processing is a separate step

#### Step 2.1: Backfill library — Binance data archive downloader

**File:** `internal/adapters/exchange/binance/backfill.go` (NEW)

```go
package binance

import (
    "context"
    "github.com/market-raccoon/internal/shared/problem"
)

// BackfillConfig controls what data to download.
type BackfillConfig struct {
    Symbol    string    // e.g. "BTCUSDT"
    From      time.Time // start date (inclusive, day granularity)
    To        time.Time // end date (inclusive, day granularity)
    OutputDir string    // directory for cached downloads (default: "./backfill")
    MarketType string   // "SPOT" or "USD_M_FUTURES"
}

// BackfillResult reports download progress.
type BackfillResult struct {
    DatesDownloaded int
    DatesSkipped    int    // already cached
    TradesParsed    int64
    OutputPath      string // path to generated JSONL fixture
}

// DownloadAggTrades downloads historical aggregate trades from data.binance.vision
// and produces a JSONL fixture file ready for replay.
//
// URL patterns:
//   Spot:    https://data.binance.vision/data/spot/daily/aggTrades/{SYMBOL}/{SYMBOL}-aggTrades-{YYYY-MM-DD}.zip
//   Futures: https://data.binance.vision/data/futures/um/daily/aggTrades/{SYMBOL}/{SYMBOL}-aggTrades-{YYYY-MM-DD}.zip
//
// Each ZIP contains a CSV with columns:
//   agg_trade_id, price, quantity, first_trade_id, last_trade_id, transact_time, is_buyer_maker
//
// The function:
// 1. Enumerates each calendar day in [From, To]
// 2. Downloads the ZIP for each day (skips if CSV already exists in OutputDir)
// 3. Parses CSV rows → domain.TradeTickV1
// 4. Builds envelope.Envelope for each trade (using codec.EncodePayload)
// 5. Writes envelopes to JSONL fixture via replay.Writer
// 6. Returns BackfillResult with counts
func DownloadAggTrades(ctx context.Context, cfg BackfillConfig) (BackfillResult, *problem.Problem)
```

**File-level resume:** Check if `{outputDir}/{SYMBOL}-{YYYY-MM-DD}.csv` exists before downloading. This allows the process to resume after interruption without re-downloading completed days.

**CSV parsing:** Each row maps to a `domain.TradeTickV1`:
```go
trade := domain.TradeTickV1{
    Price:     parseFloat(row[1]),    // price
    Size:      parseFloat(row[2]),    // quantity
    Side:      sideFromIsBuyerMaker(row[6]), // "true" → "sell", "false" → "buy"
    TradeID:   row[0],               // agg_trade_id
    Timestamp: parseInt(row[5]),      // transact_time (unix ms)
}
```

**Envelope construction:** For each trade, build a complete `envelope.Envelope`:
```go
env := envelope.Envelope{
    Type:           "marketdata.trade",
    Version:        1,
    Venue:          "BINANCE",
    Instrument:     naming.CanonicalInstrument(cfg.Symbol),
    ContentType:    "application/json",
    TsExchange:     trade.Timestamp,
    TsIngest:       trade.Timestamp, // for backfill, ingest = exchange time
    Seq:            seq,             // monotonic per fixture
    IdempotencyKey: buildTradeIdempotencyKey("BINANCE", instrument, trade.TradeID),
    Payload:        encodedPayload,
    Meta: map[string]string{
        "instrument_market_type": cfg.MarketType,
        "source":                 "backfill",
    },
}
```

#### Step 2.2: Backfill main binary

**File:** `cmd/backfill/main.go` (NEW)

```go
package main

import (
    "flag"
    "os"
    // ...
)

func main() {
    exchange    := flag.String("exchange", "binance", "exchange: binance")
    symbol      := flag.String("symbol", "BTCUSDT", "trading symbol")
    fromDate    := flag.String("from", "", "start date YYYY-MM-DD (required)")
    toDate      := flag.String("to", "", "end date YYYY-MM-DD (required)")
    marketType  := flag.String("market-type", "USD_M_FUTURES", "SPOT or USD_M_FUTURES")
    outputDir   := flag.String("output-dir", "./backfill", "directory for downloaded files and fixtures")
    fixtureFile := flag.String("fixture", "", "output JSONL fixture path (default: {output-dir}/{symbol}-{from}-{to}.jsonl)")
    flag.Parse()

    // Validate required flags
    // Parse dates
    // Dispatch by exchange
    // Download + generate fixture
    // Print summary
}
```

Usage:
```bash
# Download Binance futures agg trades and produce fixture
./backfill --exchange binance --symbol BTCUSDT --from 2025-01-01 --to 2025-01-07 --market-type USD_M_FUTURES

# Replay the fixture through the consumer pipeline
./consumer --replay ./backfill/BTCUSDT-2025-01-01-2025-01-07.jsonl --record ./backfill/output.jsonl
```

#### Step 2.3: Backfill bootstrap

**File:** `cmd/backfill/bootstrap.go` (NEW)

The `Run(ctx, cfg)` pattern consistent with other binaries. However, backfill is simpler — it's a one-shot tool, not a daemon:
- No actor engine, no Guardian, no HTTP server
- Just: download → parse → write fixture → exit

#### Step 2.4: Register cmd/backfill in go.work

**File:** `go.work` (EXTEND)

Add `./cmd/backfill` to the workspace.

**File:** `cmd/backfill/go.mod` (EXTEND)

Add required dependencies:
```
require (
    github.com/market-raccoon/internal/shared v0.0.0
    github.com/market-raccoon/internal/adapters v0.0.0
    github.com/market-raccoon/internal/core/marketdata v0.0.0
)

replace (
    github.com/market-raccoon/internal/shared => ../../internal/shared
    github.com/market-raccoon/internal/adapters => ../../internal/adapters
    github.com/market-raccoon/internal/core/marketdata => ../../internal/core/marketdata
)
```

#### Step 2.5: Backfill tests

**File:** `internal/adapters/exchange/binance/backfill_test.go` (NEW)

Tests:
- TestDownloadAggTrades_ParsesCSVCorrectly — feed a small embedded CSV, verify domain.TradeTickV1 fields
- TestDownloadAggTrades_SkipsExistingCSV — create a file in temp dir, verify it's not re-downloaded
- TestDownloadAggTrades_ProducesValidFixture — generate fixture, then replay it with replay.Player to verify integrity
- TestDownloadAggTrades_EmptyDateRange — returns zero counts, no error
- TestSideFromIsBuyerMaker — "true"→"sell", "false"→"buy"

**Do NOT test actual HTTP downloads** — mock the download step. The CSV parsing and fixture generation are the critical paths to test.

---

### PART 3: Gap Detection Tool

A subcommand of the backfill binary (or standalone) that queries cold storage and reports missing candle windows.

#### Step 3.1: Gap detection library

**File:** `internal/core/aggregation/app/gap_detector.go` (NEW)

```go
package app

import (
    "context"
    "github.com/market-raccoon/internal/core/aggregation/domain"
    "github.com/market-raccoon/internal/core/aggregation/ports"
    "github.com/market-raccoon/internal/shared/problem"
)

// GapReport describes a contiguous gap in candle coverage.
type GapReport struct {
    Venue      string
    Instrument string
    Timeframe  string
    GapStartMs int64 // first missing window_start
    GapEndMs   int64 // last missing window_start
    Missing    int   // number of missing candles in this gap
}

// GapDetectorConfig controls the detection parameters.
type GapDetectorConfig struct {
    Venue          string
    Instrument     string
    Timeframe      string // e.g. "1m", "5m", "1h"
    FromMs         int64  // 0 = from first stored candle
    ToMs           int64  // 0 = to last stored candle
    ExpectedStepMs int64  // expected interval between candle window_starts (e.g. 60000 for 1m)
}

// DetectCandleGaps queries cold storage timestamps and identifies gaps.
func DetectCandleGaps(ctx context.Context, reader ports.CandleReader, cfg GapDetectorConfig) ([]GapReport, *problem.Problem) {
    // 1. If FromMs == 0: call GetFirstCandle to anchor start
    // 2. If ToMs == 0: call GetLastCandle to anchor end
    // 3. Call GetCandleTimestamps(venue, instrument, timeframe, fromMs, toMs)
    // 4. Walk consecutive timestamps; if diff > ExpectedStepMs, record gap
    // 5. Return sorted []GapReport
}
```

**Algorithm:** Identical to MarketMonkey's `CheckCandleGaps` but:
- Uses `*problem.Problem` not `error`
- Pure function (receives reader interface, not global DB client)
- Returns structured `[]GapReport` (not just stdout logging)
- Configurable expected step (not hardcoded 60s)

#### Step 3.2: Gap detection CLI

**File:** `cmd/backfill/main.go` (EXTEND — add `--mode` flag)

```go
mode := flag.String("mode", "download", "download|gaps")
```

When `mode == "gaps"`:
```go
// 1. Initialize ClickHouse pool from config
// 2. Create ChCandleReader
// 3. Call DetectCandleGaps
// 4. Print structured report (JSON or table)
// 5. Exit with code 1 if gaps found, 0 if clean
```

Usage:
```bash
# Detect candle gaps for BTCUSDT 1-minute timeframe
./backfill --mode gaps --exchange binance --symbol BTCUSDT --timeframe 1m \
    --config config.jsonc

# Output:
# Gap 1: 2025-01-03T14:00:00Z → 2025-01-03T14:15:00Z (15 missing 1m candles)
# Gap 2: 2025-01-05T00:00:00Z → 2025-01-05T02:30:00Z (150 missing 1m candles)
# Total: 2 gaps, 165 missing candles
```

The exit code (0/1) enables CI/cron integration: `./backfill --mode gaps ... || alert`.

#### Step 3.3: Gap detection config

**File:** `internal/shared/config/schema.go` (EXTEND)

Add backfill/gaps config:
```go
type BackfillConfig struct {
    Mode       string `json:"mode"`        // "download" or "gaps"
    Exchange   string `json:"exchange"`
    Symbol     string `json:"symbol"`
    MarketType string `json:"market_type"`
    FromDate   string `json:"from_date"`   // YYYY-MM-DD
    ToDate     string `json:"to_date"`     // YYYY-MM-DD
    OutputDir  string `json:"output_dir"`  // default: ./backfill
    Timeframe  string `json:"timeframe"`   // for gap detection: "1m", "5m", etc.
}
```

CLI flags override config values (same pattern as other binaries).

#### Step 3.4: Gap detection tests

**File:** `internal/core/aggregation/app/gap_detector_test.go` (NEW)

Table-driven tests:
- TestDetectCandleGaps_NoGaps — complete coverage, returns empty
- TestDetectCandleGaps_SingleGap — one 15-minute gap at known position
- TestDetectCandleGaps_MultipleGaps — scattered gaps, verify all reported
- TestDetectCandleGaps_EmptyStorage — no data, returns single gap spanning the full range (or empty with appropriate handling)
- TestDetectCandleGaps_AutoAnchor — FromMs=0 and ToMs=0 uses GetFirstCandle/GetLastCandle

All tests use in-memory `CandleReader` stub — no ClickHouse required.

#### Step 3.5: Timeframe-to-milliseconds helper

**File:** `internal/core/aggregation/domain/timeframe.go` (NEW or EXTEND)

If not already present, add:
```go
// TimeframeToMs converts a human timeframe string to milliseconds.
// Supported: "1m"→60000, "5m"→300000, "15m"→900000, "30m"→1800000, "1h"→3600000, "4h"→14400000, "1d"→86400000.
func TimeframeToMs(tf string) (int64, *problem.Problem)
```

This is used by `DetectCandleGaps` to compute `ExpectedStepMs` from a config-friendly string.

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/shared/replay/writer.go` | JSONL fixture writer — backfill output format |
| `internal/shared/replay/reader.go` | JSONL fixture reader — validates SHA-256 |
| `internal/shared/replay/player.go` | Replay player with FakeClock + ReplaySequencer |
| `internal/shared/replay/recorder.go` | RecorderPublisher wrapping EventPublisher |
| `cmd/consumer/replay.go` | Consumer replay mode — backfill fixtures replayed here |
| `cmd/consumer/main.go` | Consumer CLI flags (--replay, --record) |
| `cmd/store/bootstrap.go` | Store binary — reference for ClickHouse pool wiring |
| `internal/adapters/storage/clickhouse/pool.go` | ClickHouse connection pool |
| `internal/adapters/storage/clickhouse/candle_writer.go` | ClickHouse candle writer — DDL reference |
| `internal/adapters/storage/clickhouse/stats_writer.go` | ClickHouse stats writer |
| `internal/adapters/storage/clickhouse/writer.go` | ChWriter + in-memory Writer pattern |
| `internal/core/aggregation/ports/ports.go` | Existing write-only ports — extend with readers |
| `internal/core/aggregation/domain/candle.go` | CandleV1 domain type |
| `internal/core/aggregation/domain/stats.go` | StatsWindowV1 domain type |
| `internal/core/marketdata/domain/payloads.go` | TradeTickV1 — backfill output payload |
| `internal/adapters/exchange/binance/parser.go` | Binance parser — reference for idempotency key pattern |
| `internal/shared/naming/naming.go` | CanonicalVenue, CanonicalInstrument |
| `internal/shared/config/schema.go` | AppConfig to extend with BackfillConfig |
| `internal/shared/envelope/envelope.go` | Envelope struct for fixture construction |
| `internal/shared/codec/payload_codec.go` | EncodePayload for fixture payload encoding |
| `internal/shared/problem/problem.go` | Problem type |
| `sql/clickhouse/migrations/0005_s4_artifact_tables.sql` | Cold table DDL (column names for SELECT) |
| `sql/clickhouse/migrations/0004_s1_orderbook_cold.sql` | Orderbook cold table DDL |
| `go.work` | Workspace — add cmd/backfill |
| `cmd/backfill/go.mod` | Stub module to extend |
| `testdata/fixtures/ingest-1000.jsonl` | Reference fixture format |

---

## Execution Rules

```bash
# All gates must pass after each part:
make test-workspace
make test-workspace-race
make docs-check
make invariants-check

# After all 3 parts:
make tidy
make test-workspace-race
```

### STOP CONDITIONS:
- Cold-path readers importing from `internal/core/` (layering violation — readers live in `internal/adapters/`)
- Read port interfaces returning `error` instead of `*problem.Problem`
- Backfill binary writing directly to the database (must produce fixture files)
- Fixture files not passing `replay.Player` validation (SHA-256 mismatch, non-monotonic seq)
- Gap detector depending on ClickHouse at test time (must use in-memory stubs)
- `cmd/backfill/go.mod` missing `replace` directives
- `go.work` not including `./cmd/backfill`
- Download functions making real HTTP calls in tests (must mock)

### Commit sequence:
```
feat(c3): add cold-path read ports and ClickHouse reader adapters

- ports.CandleReader: GetCandleRange, GetCandleTimestamps, GetFirst/Last
- ports.StatsReader: GetStatsTimestamps, GetFirst/Last
- ports.SnapshotReader: GetSnapshotTimestamps
- ClickHouse implementations: ChCandleReader, ChStatsReader, ChSnapshotReader
- In-memory stubs for testing
- SELECT queries against aggregation_candle_cold, aggregation_stats_cold, aggregation_orderbook_snapshot_cold

feat(c3): implement backfill binary with Binance data archive download

- cmd/backfill with --exchange, --symbol, --from, --to, --market-type flags
- Downloads from data.binance.vision (spot and futures paths)
- CSV parsing → domain.TradeTickV1 → envelope.Envelope → JSONL fixture
- File-level resume: skips already-downloaded dates
- Produces fixtures compatible with consumer --replay
- Registered in go.work
- Tests: CSV parsing, fixture integrity via replay.Player, resume logic

feat(c3): add candle gap detection tool

- aggregation/app.DetectCandleGaps: queries CandleReader for timestamp gaps
- Returns structured []GapReport with gap start/end/count
- cmd/backfill --mode gaps: CLI interface for gap detection
- Configurable timeframe, auto-anchor from first/last stored candle
- Exit code 1 when gaps found (CI/cron friendly)
- TimeframeToMs helper for human-readable timeframe strings
- Tests: no-gaps, single-gap, multiple-gaps, empty-storage, auto-anchor

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **Backfill produces fixtures, not DB writes** — The backfill binary MUST output JSONL fixture files. Processing happens via `consumer --replay`. This is a deliberate architectural choice for determinism and auditability.
2. **No testcontainers in reader tests** — All ClickHouse reader tests use in-memory stubs. Real integration coverage comes from soak tests.
3. **No real HTTP downloads in tests** — Mock the download step. Test CSV parsing and fixture generation independently.
4. **File-level resume, not trade-level** — Downloaded CSVs are cached in `{outputDir}/`. If the process crashes, it skips already-downloaded days. Within a day, all trades are reprocessed (idempotent via envelope idempotency keys).
5. **go.mod hygiene** — `cmd/backfill/go.mod` needs `require` + `replace` directives for shared, adapters, and core/marketdata. Add `./cmd/backfill` to `go.work`. Run `make tidy`.
6. **Read ports in `aggregation/ports/`** — Cold-path readers implement ports defined in the core layer, consistent with hexagonal architecture. Implementations live in `internal/adapters/storage/clickhouse/`.
7. **`*problem.Problem` at boundaries** — All adapter functions returning errors use `*problem.Problem`.
8. **Metrics cardinality** — Follow `docs/architecture/metrics-budget-label-policy.md`. Backfill operations should not pollute runtime metrics — use separate counters or skip metrics entirely.
9. **ClickHouse `FINAL`** — Reader queries should use `FINAL` keyword or accept lazy deduplication from `ReplacingMergeTree`. Document the choice.
10. **Backfill is Binance-only for now** — The architecture supports future exchange-specific backfill modules, but C3 implements only Binance (spot + futures). Other exchanges can be added in future waves.
