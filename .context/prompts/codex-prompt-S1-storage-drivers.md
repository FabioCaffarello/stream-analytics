# Codex Prompt S1 — Real Storage Drivers (Timescale + ClickHouse)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture.

---

## Context

ALL storage adapters are currently **in-memory stubs** that lose data on restart:
- `internal/adapters/storage/timescale/writer.go` — `sync.Map` stub, `IsProductionReady()=false`
- `internal/adapters/storage/clickhouse/writer.go` — `sync.Map` stub
- `internal/adapters/storage/timescale/delivery_range_store.go` — in-memory circular buffer
- `internal/adapters/storage/timescale/status.go` — explicitly returns `AdapterModeStubMemory`

The application layer is complete (domain, use cases, actor routing, tests). This wave replaces stubs with real SQL-backed implementations.

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error` in domain/app)
### Connection interface pattern (adapter layer CAN use plain `error` internally, wrap at boundary):
```go
func (w *Writer) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
    if err := w.pool.QueryRow(ctx, ...).Scan(...); err != nil {
        return problem.Wrap(err, problem.Unavailable, "timescale write failed")
    }
    return nil
}
```

### Import order: stdlib → external → monorepo

---

## Task: Implement Real Storage Drivers

### Step 1: Add database driver dependencies

**File:** `internal/adapters/go.mod`

Add dependencies:
```
require (
    github.com/jackc/pgx/v5 v5.7.2
    github.com/ClickHouse/clickhouse-go/v2 v2.30.1
)
```

Run `go mod tidy` after adding.

### Step 2: Define SQL executor interface

**File:** `internal/adapters/storage/sqlport.go` (NEW)

Create a thin interface so tests can use fakes without a real database:

```go
package storage

import (
    "context"
    "github.com/market-raccoon/internal/shared/problem"
)

// SQLExecutor abstracts database operations for testability.
// Production uses pgx.Pool; tests use an in-memory fake.
type SQLExecutor interface {
    Exec(ctx context.Context, sql string, args ...any) (int64, *problem.Problem)
    QueryRow(ctx context.Context, sql string, args ...any) Row
}

// Row abstracts a single-row scan result.
type Row interface {
    Scan(dest ...any) error
}

// BatchInserter abstracts batch insert for ClickHouse.
type BatchInserter interface {
    AppendRow(ctx context.Context, values ...any) *problem.Problem
    Flush(ctx context.Context) (int64, *problem.Problem)
    Close() *problem.Problem
}
```

### Step 3: Implement pgx-backed Timescale pool

**File:** `internal/adapters/storage/timescale/pool.go` (NEW)

```go
package timescale

import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/market-raccoon/internal/shared/problem"
)

type PoolConfig struct {
    DSN             string
    MaxConns        int32
    MinConns        int32
    MaxConnLifetime time.Duration
    MaxConnIdleTime time.Duration
    HealthCheckPeriod time.Duration
}

type Pool struct {
    pool *pgxpool.Pool
}

func NewPool(ctx context.Context, cfg PoolConfig) (*Pool, *problem.Problem) {
    config, err := pgxpool.ParseConfig(cfg.DSN)
    if err != nil {
        return nil, problem.Wrap(err, problem.InvalidConfig, "invalid timescale DSN")
    }
    if cfg.MaxConns > 0 {
        config.MaxConns = cfg.MaxConns
    }
    if cfg.MinConns > 0 {
        config.MinConns = cfg.MinConns
    }
    if cfg.MaxConnLifetime > 0 {
        config.MaxConnLifetime = cfg.MaxConnLifetime
    }
    if cfg.MaxConnIdleTime > 0 {
        config.MaxConnIdleTime = cfg.MaxConnIdleTime
    }
    if cfg.HealthCheckPeriod > 0 {
        config.HealthCheckPeriod = cfg.HealthCheckPeriod
    }
    pool, err := pgxpool.NewWithConfig(ctx, config)
    if err != nil {
        return nil, problem.Wrap(err, problem.Unavailable, "failed to create timescale pool")
    }
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, problem.Wrap(err, problem.Unavailable, "timescale ping failed")
    }
    return &Pool{pool: pool}, nil
}

func (p *Pool) Close() {
    if p.pool != nil {
        p.pool.Close()
    }
}

func (p *Pool) Raw() *pgxpool.Pool { return p.pool }

func (p *Pool) Healthy(ctx context.Context) *problem.Problem {
    if err := p.pool.Ping(ctx); err != nil {
        return problem.Wrap(err, problem.Unavailable, "timescale health check failed")
    }
    return nil
}
```

### Step 4: Rewrite Timescale orderbook snapshot writer

**File:** `internal/adapters/storage/timescale/writer.go` (REWRITE)

Keep the existing in-memory `Writer` as a **fallback** (rename to `MemWriter`). Add a new `PgWriter` that uses real SQL:

```go
// PgWriter is the production Timescale writer for orderbook snapshots.
type PgWriter struct {
    pool *Pool
}

func NewPgWriter(pool *Pool) *PgWriter {
    return &PgWriter{pool: pool}
}

func (w *PgWriter) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
    const upsertSQL = `
        INSERT INTO aggregation_orderbook_snapshot (
            venue, instrument, seq, bids_json, asks_json,
            spread, mid_price, best_bid, best_ask,
            idempotency_key, ts_ingest, created_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
        ON CONFLICT (venue, instrument, seq) DO NOTHING`

    bidsJSON, err := codec.Marshal(snap.Bids)
    if err != nil {
        return problem.Wrap(err, problem.Internal, "marshal bids failed")
    }
    asksJSON, err := codec.Marshal(snap.Asks)
    if err != nil {
        return problem.Wrap(err, problem.Internal, "marshal asks failed")
    }

    _, dbErr := w.pool.Raw().Exec(ctx, upsertSQL,
        snap.BookID.Venue, snap.BookID.Instrument, snap.Seq,
        bidsJSON, asksJSON,
        snap.Spread, snap.MidPrice, snap.BestBid, snap.BestAsk,
        snap.IdempotencyKey, snap.TsIngest,
    )
    if dbErr != nil {
        return problem.Wrap(dbErr, problem.Unavailable, "timescale snapshot upsert failed")
    }
    return nil
}
```

**IMPORTANT:** Check the actual `SnapshotProduced` struct fields in `internal/core/aggregation/domain/events.go` before writing SQL. The column names must match the struct fields exactly.

### Step 5: Implement ClickHouse batch writer

**File:** `internal/adapters/storage/clickhouse/pool.go` (NEW)

```go
package clickhouse

import (
    "context"
    "time"

    "github.com/ClickHouse/clickhouse-go/v2"
    "github.com/market-raccoon/internal/shared/problem"
)

type PoolConfig struct {
    Addrs           []string
    Database        string
    Username        string
    Password        string
    MaxOpenConns    int
    MaxIdleConns    int
    ConnMaxLifetime time.Duration
    DialTimeout     time.Duration
    ReadTimeout     time.Duration
}

type Pool struct {
    conn clickhouse.Conn
}

func NewPool(ctx context.Context, cfg PoolConfig) (*Pool, *problem.Problem) {
    opts := &clickhouse.Options{
        Addr:     cfg.Addrs,
        Auth: clickhouse.Auth{
            Database: cfg.Database,
            Username: cfg.Username,
            Password: cfg.Password,
        },
        MaxOpenConns:    cfg.MaxOpenConns,
        MaxIdleConns:    cfg.MaxIdleConns,
        ConnMaxLifetime: cfg.ConnMaxLifetime,
        DialTimeout:     cfg.DialTimeout,
        ReadTimeout:     cfg.ReadTimeout,
    }
    conn, err := clickhouse.Open(opts)
    if err != nil {
        return nil, problem.Wrap(err, problem.Unavailable, "clickhouse open failed")
    }
    if err := conn.Ping(ctx); err != nil {
        return nil, problem.Wrap(err, problem.Unavailable, "clickhouse ping failed")
    }
    return &Pool{conn: conn}, nil
}

func (p *Pool) Close() error {
    if p.conn != nil {
        return p.conn.Close()
    }
    return nil
}

func (p *Pool) Conn() clickhouse.Conn { return p.conn }

func (p *Pool) Healthy(ctx context.Context) *problem.Problem {
    if err := p.conn.Ping(ctx); err != nil {
        return problem.Wrap(err, problem.Unavailable, "clickhouse health check failed")
    }
    return nil
}
```

### Step 6: Rewrite ClickHouse snapshot writer with real batch insert

**File:** `internal/adapters/storage/clickhouse/writer.go` (REWRITE)

Keep existing `MemWriter` (renamed). Add `ChWriter`:

```go
type ChWriter struct {
    pool *Pool
}

func NewChWriter(pool *Pool) *ChWriter {
    return &ChWriter{pool: pool}
}

func (w *ChWriter) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
    batch, err := w.pool.Conn().PrepareBatch(ctx,
        "INSERT INTO aggregation_orderbook_snapshot_cold")
    if err != nil {
        return problem.Wrap(err, problem.Unavailable, "clickhouse prepare batch failed")
    }
    // Append single row for snapshot
    if err := batch.Append(
        snap.BookID.Venue, snap.BookID.Instrument, snap.Seq,
        snap.Spread, snap.MidPrice, snap.BestBid, snap.BestAsk,
        snap.IdempotencyKey, snap.TsIngest,
    ); err != nil {
        return problem.Wrap(err, problem.Unavailable, "clickhouse append failed")
    }
    if err := batch.Send(); err != nil {
        return problem.Wrap(err, problem.Unavailable, "clickhouse batch send failed")
    }
    return nil
}
```

### Step 7: Add storage config to schema

**File:** `internal/shared/config/schema.go`

Add storage configuration:
```go
type StorageConfig struct {
    Timescale TimescaleConfig `json:"timescale"`
    ClickHouse ClickHouseConfig `json:"clickhouse"`
}

type TimescaleConfig struct {
    DSN             string `json:"dsn"`
    MaxConns        int    `json:"max_conns"`
    MinConns        int    `json:"min_conns"`
    MaxConnLifetime string `json:"max_conn_lifetime"`
    Enabled         bool   `json:"enabled"`
}

type ClickHouseConfig struct {
    Addrs    []string `json:"addrs"`
    Database string   `json:"database"`
    Username string   `json:"username"`
    Password string   `json:"password"`
    Enabled  bool     `json:"enabled"`
}
```

Add `Storage StorageConfig` field to `AppConfig`.

Add defaults in `applyDefaults()`:
```go
if c.Storage.Timescale.MaxConns == 0 {
    c.Storage.Timescale.MaxConns = 10
}
if c.Storage.Timescale.MinConns == 0 {
    c.Storage.Timescale.MinConns = 2
}
if c.Storage.ClickHouse.Database == "" {
    c.Storage.ClickHouse.Database = "market_raccoon"
}
```

### Step 8: Update status.go

**File:** `internal/adapters/storage/timescale/status.go`

Make `IsProductionReady()` and `AdapterMode()` dynamic based on whether a real pool is configured:

```go
var (
    productionReady bool
    adapterMode     = AdapterModeStubMemory
)

func SetProductionReady(mode string) {
    productionReady = true
    adapterMode = mode
}

func IsProductionReady() bool { return productionReady }
func AdapterMode() string     { return adapterMode }
```

### Step 9: Update SnapshotCommitter for real writers

**File:** `internal/adapters/storage/committer.go`

The existing `SnapshotCommitter` does dual-write (hot→cold). Update it to accept the new `PgWriter` and `ChWriter` through the same `HotReadModelStore` and `ColdReadModelStore` interfaces.

Check if the current committer already uses interfaces. If so, the real writers should just satisfy those interfaces — no committer changes needed.

### Step 10: Update bootstrap to conditionally use real storage

**File:** `cmd/processor/bootstrap.go`

```go
// ── storage ──────────────────────────────────────────────────────────
var hotStore aggports.HotReadModelStore
var coldStore aggports.ColdReadModelStore

if cfg.Storage.Timescale.Enabled {
    tsPool, p := timescale.NewPool(ctx, timescale.PoolConfig{
        DSN:      cfg.Storage.Timescale.DSN,
        MaxConns: int32(cfg.Storage.Timescale.MaxConns),
        MinConns: int32(cfg.Storage.Timescale.MinConns),
    })
    if p != nil {
        return fmt.Errorf("timescale pool: %v", p)
    }
    defer tsPool.Close()
    hotStore = timescale.NewPgWriter(tsPool)
    timescale.SetProductionReady("pgx")
    logger.Info("processor: using real Timescale storage")
} else {
    hotStore = &committedHotStore{committer: adapterstorage.NewSnapshotCommitter(
        timescale.NewWriter(), clickhouse.NewWriter(),
    )}
    logger.Warn("processor: using in-memory stub storage (NOT production-ready)")
}
```

Same pattern for ClickHouse and for cmd/store/bootstrap.go.

### Step 11: DDL migration files

**File:** `migrations/001_initial_schema.sql` (NEW)

```sql
-- Timescale hypertable for orderbook snapshots
CREATE TABLE IF NOT EXISTS aggregation_orderbook_snapshot (
    venue        TEXT NOT NULL,
    instrument   TEXT NOT NULL,
    seq          BIGINT NOT NULL,
    bids_json    JSONB NOT NULL,
    asks_json    JSONB NOT NULL,
    spread       DOUBLE PRECISION,
    mid_price    DOUBLE PRECISION,
    best_bid     DOUBLE PRECISION,
    best_ask     DOUBLE PRECISION,
    idempotency_key TEXT NOT NULL,
    ts_ingest    BIGINT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (venue, instrument, seq)
);

SELECT create_hypertable('aggregation_orderbook_snapshot', 'created_at',
    if_not_exists => TRUE,
    migrate_data => TRUE
);

CREATE INDEX IF NOT EXISTS idx_snapshot_venue_instrument
    ON aggregation_orderbook_snapshot (venue, instrument, ts_ingest DESC);
```

**NOTE:** Check the actual `SnapshotProduced` struct fields before finalizing DDL. The columns must match exactly.

### Step 12: Tests

**File:** `internal/adapters/storage/timescale/pg_writer_test.go` (NEW)

Test with a mock SQL executor (don't require real database for unit tests):

```go
func TestPgWriter_Save_Success(t *testing.T) {
    // Use a fake pool or sqlmock to verify SQL execution.
}

func TestPgWriter_Save_DuplicateIdempotent(t *testing.T) {
    // ON CONFLICT DO NOTHING → no error on duplicate.
}

func TestPgWriter_Save_ConnectionError(t *testing.T) {
    // Pool returns error → *problem.Problem with Unavailable code.
}
```

**File:** `internal/adapters/storage/clickhouse/ch_writer_test.go` (NEW)

```go
func TestChWriter_Save_Success(t *testing.T)
func TestChWriter_Save_BatchFlush(t *testing.T)
func TestChWriter_Save_ConnectionError(t *testing.T)
```

**File:** `internal/adapters/storage/storage_integration_test.go` (EXTEND)

Add tests that verify the committer works with real writer interfaces (even if backed by fakes).

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/adapters/storage/timescale/writer.go` | Current stub to replace |
| `internal/adapters/storage/timescale/status.go` | Production-ready flag |
| `internal/adapters/storage/clickhouse/writer.go` | Current stub to replace |
| `internal/adapters/storage/committer.go` | Dual-write committer |
| `internal/core/aggregation/domain/events.go` | SnapshotProduced struct |
| `internal/core/aggregation/ports/ports.go` | HotReadModelStore interface |
| `internal/shared/config/schema.go` | Config to extend |
| `cmd/processor/bootstrap.go` | Composition root |
| `docs/architecture/storage.md` | Storage architecture doc |

---

## Execution Rules

```bash
make test-workspace       # all modules
make test-workspace-race  # with -race
make docs-check
make invariants-check
```

### STOP CONDITIONS:
- Layering violation (core importing adapters)
- External driver import in core/ or shared/ (only adapters may import pgx/clickhouse-go)
- Unbounded connection pool (must have MaxConns cap)
- ACK before commit (if touching ACK boundary)

### Commit:
```
feat(s1): add real Timescale+ClickHouse storage drivers

- pgx pool with health checks and connection lifecycle
- clickhouse-go batch writer with flush semantics
- SQLExecutor interface for testability
- Config schema: storage.timescale and storage.clickhouse
- Conditional bootstrap: real drivers when enabled, stubs as fallback
- DDL migration for orderbook snapshot hypertable

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **External deps only in adapters/** — pgx and clickhouse-go MUST NOT leak to core/ or shared/
2. **Graceful fallback** — if `storage.timescale.enabled=false`, use existing in-memory stubs
3. **Connection lifecycle** — Pool.Close() must be called in shutdown sequence (defer in bootstrap)
4. **Idempotent upserts** — `ON CONFLICT DO NOTHING` for Timescale; ReplacingMergeTree for ClickHouse
5. **No migration runner** — just provide DDL files; migrations are applied externally
6. **Bounded pools** — MaxConns must be configurable and have safe defaults
7. **Health checks** — Pool.Healthy() used by /healthz and /readyz endpoints
8. **Observability** — Wire `metrics.ProcessorCommitTotal` and `metrics.ProcessorCommitLatencySeconds` in real writers
