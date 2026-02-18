# Codex Prompt S4 — Artifact Storage Writers (Candle/Stats/Heatmap)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

---

## Context

After S1-S3, the storage infrastructure is real (pgx + clickhouse-go) and the delivery subsystem is complete. However, only **orderbook snapshots** have real storage writers. The following artifact types need writers:

| Artifact | Domain Event | Hot Store Interface | Current Status |
|----------|-------------|---------------------|----------------|
| Candle OHLCV | `CandleClosed` | `CandleHotReadModelStore` | `logCandleHotStore` (log only) |
| Stats Window | `StatsWindowClosed` | `StatsHotReadModelStore` | `logStatsHotStore` (log only) |
| Heatmap Bucket | `HeatmapWindowClosed` | (in insights ports) | `heatmap_writer.go` (stub) |
| Volume Profile | `VPVRWindowClosed` | (in insights ports) | `volume_profile_writer.go` (stub) |

**Goal:** Replace log/stub writers with real Timescale INSERT + ClickHouse batch insert for all artifact types.

---

## Mandatory Patterns

### Port interfaces (already defined):
```go
// internal/core/aggregation/ports/ports.go
type CandleHotReadModelStore interface {
    SaveCandle(ctx context.Context, evt domain.CandleClosed) *problem.Problem
}
type StatsHotReadModelStore interface {
    SaveStats(ctx context.Context, evt domain.StatsWindowClosed) *problem.Problem
}
```

### Writer pattern (established in S1):
```go
type PgCandleWriter struct {
    pool *timescale.Pool
}

func (w *PgCandleWriter) SaveCandle(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
    const upsertSQL = `INSERT INTO ... ON CONFLICT DO NOTHING`
    _, err := w.pool.Raw().Exec(ctx, upsertSQL, ...)
    if err != nil {
        return problem.Wrap(err, problem.Unavailable, "timescale candle upsert failed")
    }
    return nil
}
```

---

## Task: Implement All Artifact Writers

### Step 1: Candle writer (Timescale)

**File:** `internal/adapters/storage/timescale/candle_writer.go` (NEW)

DDL:
```sql
CREATE TABLE IF NOT EXISTS aggregation_candle (
    venue          TEXT NOT NULL,
    instrument     TEXT NOT NULL,
    timeframe      TEXT NOT NULL,
    window_start   BIGINT NOT NULL,
    window_end     BIGINT NOT NULL,
    open_price     DOUBLE PRECISION NOT NULL,
    high_price     DOUBLE PRECISION NOT NULL,
    low_price      DOUBLE PRECISION NOT NULL,
    close_price    DOUBLE PRECISION NOT NULL,
    volume         DOUBLE PRECISION NOT NULL,
    buy_volume     DOUBLE PRECISION NOT NULL,
    sell_volume    DOUBLE PRECISION NOT NULL,
    trade_count    BIGINT NOT NULL,
    seq_first      BIGINT NOT NULL,
    seq_last       BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);
```

Writer implementation:
```go
func (w *PgCandleWriter) SaveCandle(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
    c := evt.Candle
    const upsertSQL = `
        INSERT INTO aggregation_candle (
            venue, instrument, timeframe, window_start, window_end,
            open_price, high_price, low_price, close_price,
            volume, buy_volume, sell_volume, trade_count,
            seq_first, seq_last, idempotency_key
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
        ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`

    _, err := w.pool.Raw().Exec(ctx, upsertSQL,
        c.Venue, c.Instrument, c.Timeframe, c.WindowStartTs, c.WindowEndTs,
        c.Open, c.High, c.Low, c.ClosePrice,
        c.Volume, c.BuyVolume, c.SellVolume, c.TradeCount,
        c.SeqFirst, c.SeqLast,
        hash.HashFields(c.Venue, c.Instrument, c.Timeframe, fmt.Sprintf("%d", c.WindowStartTs)),
    )
    if err != nil {
        return problem.Wrap(err, problem.Unavailable, "timescale candle upsert failed")
    }
    return nil
}
```

### Step 2: Candle writer (ClickHouse)

**File:** `internal/adapters/storage/clickhouse/candle_writer.go` (NEW)

Same fields, batch insert pattern from S1.

### Step 3: Stats writer (Timescale)

**File:** `internal/adapters/storage/timescale/stats_writer.go` (NEW)

DDL:
```sql
CREATE TABLE IF NOT EXISTS aggregation_stats (
    venue              TEXT NOT NULL,
    instrument         TEXT NOT NULL,
    timeframe          TEXT NOT NULL,
    window_start       BIGINT NOT NULL,
    window_end         BIGINT NOT NULL,
    liq_buy_volume     DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_sell_volume    DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_total_volume   DOUBLE PRECISION NOT NULL DEFAULT 0,
    liq_count          BIGINT NOT NULL DEFAULT 0,
    markprice_open     DOUBLE PRECISION,
    markprice_high     DOUBLE PRECISION,
    markprice_low      DOUBLE PRECISION,
    markprice_close    DOUBLE PRECISION,
    funding_rate_avg   DOUBLE PRECISION,
    funding_rate_last  DOUBLE PRECISION,
    seq_first          BIGINT NOT NULL,
    seq_last           BIGINT NOT NULL,
    idempotency_key    TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, timeframe, window_start)
);
```

### Step 4: Stats writer (ClickHouse)

**File:** `internal/adapters/storage/clickhouse/stats_writer.go` (NEW)

### Step 5: Heatmap writer completion

**File:** `internal/adapters/storage/timescale/heatmap_writer.go` (REWRITE)

Currently in-memory stub. Replace with real pgx INSERT. Check the existing heatmap domain events:
- Read `internal/core/insights/domain/heatmap_bucket.go` for the data structure
- Read `internal/core/insights/ports/` (if exists) for the store interface

### Step 6: VPVR writer upgrade

**File:** `internal/adapters/storage/timescale/volume_profile_writer.go` (REWRITE)

Currently in-memory stub (197 LOC). Replace the internal map with real pgx INSERT.
- The writer already has the right interface shape
- Replace `sync.Map` operations with SQL upserts

### Step 7: Wire writers in bootstrap

**File:** `cmd/processor/bootstrap.go`

Replace log stubs with real writers when storage is enabled:

```go
var candleStore aggports.CandleHotReadModelStore
var statsStore aggports.StatsHotReadModelStore

if cfg.Storage.Timescale.Enabled {
    candleStore = timescale.NewPgCandleWriter(tsPool)
    statsStore = timescale.NewPgStatsWriter(tsPool)
} else {
    candleStore = &logCandleHotStore{logger: logger}
    statsStore = &logStatsHotStore{logger: logger}
}

aggSvc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
    Update:      aggapp.UpdateConfig{MaxBooks: cfg.Processor.MaxInstruments},
    Candle:      aggapp.BuildCandleConfig{MaxCandles: cfg.Processor.Candle.MaxCandles},
    Stats:       aggapp.BuildStatsConfig{MaxWindows: cfg.Processor.Stats.MaxWindows},
    Publisher:   artifactPub,
    Store:       hotStore,
    CandleStore: candleStore,
    StatsStore:  statsStore,
})
```

### Step 8: DDL migration file

**File:** `migrations/002_artifact_tables.sql` (NEW)

Include all DDL for candle, stats, heatmap, volume_profile tables.

### Step 9: Tests

**File:** `internal/adapters/storage/timescale/candle_writer_test.go` (NEW)

```go
func TestPgCandleWriter_Save_Success(t *testing.T)
func TestPgCandleWriter_Save_DuplicateIdempotent(t *testing.T)
func TestPgCandleWriter_Save_AllTimeframes(t *testing.T)
```

**File:** `internal/adapters/storage/timescale/stats_writer_test.go` (NEW)

```go
func TestPgStatsWriter_Save_Success(t *testing.T)
func TestPgStatsWriter_Save_PartialInputs(t *testing.T)
func TestPgStatsWriter_Save_NullableFields(t *testing.T)
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/core/aggregation/domain/candle.go` | CandleV1 struct fields |
| `internal/core/aggregation/domain/stats.go` | StatsWindowV1 struct fields |
| `internal/core/aggregation/ports/ports.go` | Store interfaces |
| `internal/core/insights/domain/heatmap_bucket.go` | Heatmap data model |
| `internal/core/insights/domain/volume_profile.go` | VPVR data model |
| `internal/adapters/storage/timescale/writer.go` | Pattern from S1 |
| `internal/adapters/storage/timescale/volume_profile_writer.go` | VPVR stub to rewrite |
| `internal/adapters/storage/timescale/heatmap_writer.go` | Heatmap stub to rewrite |
| `cmd/processor/bootstrap.go` | Wiring |

---

## Execution Rules

```bash
make test-workspace
make test-workspace-race
make docs-check
make invariants-check
```

### Commit sequence:
```
feat(s4): add Timescale+ClickHouse candle writers
feat(s4): add Timescale+ClickHouse stats writers
feat(s4): rewrite heatmap+VPVR writers with real SQL
feat(s4): wire all artifact writers in processor bootstrap
```

---

## Important Constraints

1. **Idempotent upserts** — `ON CONFLICT (venue, instrument, timeframe, window_start) DO NOTHING`
2. **Nullable fields** — Stats markprice/funding fields can be NULL (partial window ST-6)
3. **Fixed-point to float** — CandleV1 exports float64 fields; store those directly
4. **Idempotency keys** — `hash.HashFields(venue, instrument, timeframe, windowStart)` for candle/stats
5. **No schema migrations at runtime** — DDL is applied externally before app starts
6. **Writers behind feature flags** — `cfg.Storage.Timescale.Enabled` gates real vs stub
