# Stage 72 — Portfolio Persistence & Read Model Materialization

## Summary

Implemented concrete TimescaleDB storage adapters for all three portfolio read models
(state, account snapshot, portfolio summary) with idempotent upsert semantics,
deterministic replay guarantees, and full test coverage.

## Deliverables

### 1. SQL Migration (`0007_s72_portfolio_read_models.sql`)

Three tables with JSONB columns for nested domain objects:

| Table | Primary Key | Indexes |
|---|---|---|
| `portfolio_state` | `(account_id, venue, state_id)` | `projected_at_ms DESC` |
| `portfolio_account_snapshot` | `(account_id, snapshot_id)` | `projected_at_ms DESC` |
| `portfolio_summary` | `(summary_id)` | `projected_at_ms DESC` |

Design decisions:
- **JSONB for nested arrays** (positions, balances, exposures, venues, accounts) — preserves
  document-oriented read model semantics without join complexity
- **Scalar columns for filterable/aggregatable fields** (equity, PnL, timestamps)
- **Descending indexes on projected_at_ms** — optimized for "latest first" query pattern

### 2. Writer Adapters

| Adapter | Port Interface | Upsert Policy |
|---|---|---|
| `PgPortfolioStateWriter` | `PortfolioStateWriter` | `ON CONFLICT DO UPDATE` (14 args) |
| `PgAccountSnapshotWriter` | `AccountSnapshotWriter` | `ON CONFLICT DO UPDATE` (10 args) |
| `PgPortfolioSummaryWriter` | `PortfolioSummaryWriter` | `ON CONFLICT DO UPDATE` (11 args) |

All writers:
- Validate domain objects before persistence (fail-fast)
- Use `ON CONFLICT ... DO UPDATE` for idempotent upsert
- Nil-safe (returns `ValidationFailed` if writer/exec is nil)
- Dual constructor pattern: `New*Writer(pool)` + `New*WriterWithExecutor(exec)` for tests

### 3. Reader Adapters

| Adapter | Port Interface | Query Capabilities |
|---|---|---|
| `PgPortfolioStateReader` | `PortfolioStateReader` | by account+venue+symbol, latest, JSONB `@>` filter |
| `PgAccountSnapshotReader` | `AccountSnapshotReader` | by account, time range, latest |
| `PgPortfolioSummaryReader` | `PortfolioSummaryReader` | time range, latest |

All readers:
- Dynamic query building with parameterized args (no SQL injection)
- Default limit of 100 when unspecified
- `ORDER BY projected_at_ms DESC` for temporal queries
- JSONB deserialization back to domain structs

### 4. Test Coverage (18 tests)

| Test File | Tests | Coverage |
|---|---|---|
| `portfolio_state_writer_test.go` | 5 | success, duplicate, validation, nil, connection error |
| `portfolio_snapshot_writer_test.go` | 5 | success, duplicate, validation, nil, connection error |
| `portfolio_summary_writer_test.go` | 5 | success, duplicate, validation, nil, connection error |
| `portfolio_replay_test.go` | 3 | determinism, idempotent upsert, serialization roundtrip |

Key test scenarios:
- **Replay determinism**: Same event sequence → identical state IDs, equity, realized PnL
- **Idempotent upsert**: Double-write produces no error
- **Serialization roundtrip**: JSON marshal/unmarshal preserves all nested fields
- **Validation gates**: Invalid domain objects rejected before SQL execution

## Architecture

```
ExecutionEventV1 → BootstrapProjector → PortfolioStateV1 → PgPortfolioStateWriter → portfolio_state
                   └→ SnapshotBuilder  → AccountSnapshotV1 → PgAccountSnapshotWriter → portfolio_account_snapshot
                                       → PortfolioSummaryV1 → PgPortfolioSummaryWriter → portfolio_summary
```

No business logic in storage layer — adapters are pure persistence with validation.

## Files

### New (10)
- `sql/timescale/migrations/0007_s72_portfolio_read_models.sql`
- `internal/adapters/storage/timescale/portfolio_state_writer.go`
- `internal/adapters/storage/timescale/portfolio_snapshot_writer.go`
- `internal/adapters/storage/timescale/portfolio_summary_writer.go`
- `internal/adapters/storage/timescale/portfolio_state_reader.go`
- `internal/adapters/storage/timescale/portfolio_snapshot_reader.go`
- `internal/adapters/storage/timescale/portfolio_summary_reader.go`
- `internal/adapters/storage/timescale/portfolio_state_writer_test.go`
- `internal/adapters/storage/timescale/portfolio_snapshot_writer_test.go`
- `internal/adapters/storage/timescale/portfolio_summary_writer_test.go`
- `internal/adapters/storage/timescale/portfolio_replay_test.go`
- `internal/adapters/storage/timescale/portfolio_test_helpers_test.go`

### Unchanged
- All existing adapter, domain, and port files

## Metrics

- 18 new tests, 0 regressions
- 0 wire protocol changes
- 0 domain logic changes
- Full interface compliance verified via `var _ ports.* = (*Pg*)(nil)` compile-time checks
