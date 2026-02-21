# PRD-0003 Bug Investigation

## Bug: All aggregation orderbook writes fail with SYS_UNAVAILABLE

### Symptoms
- **When**: After starting with `PROCESSOR_REPLICAS=2`
- **Error**: Continuous `SYS_UNAVAILABLE` errors for all `UpdateOrderBook` operations
- **Affected**: All venues (BINANCE, BYBIT, COINBASE, HYPERLIQUID, etc.)
- **Frequency**: Every orderbook update attempt fails

### Log Evidence
```json
{"time":"2026-02-21T00:38:49.121653422Z","level":"WARN","msg":"aggruntime: UpdateOrderBook failed","venue":"BYBIT","instrument":"ETHUSDT","seq":130079610,"code":"SYS_UNAVAILABLE","retryable":false}
```

### Investigation

#### Evidence Collected
1. ✅ All Docker containers healthy
2. ✅ TimescaleDB table exists with correct schema
3. ✅ Manual INSERTs work from both host and container
4. ✅ Pool initialization succeeds ("processor: using Timescale pgx writer")
5. ✅ TCP connections established (14 total: 2 active, 12 idle / 20 max)
6. ❌ ALL application writes fail with SYS_UNAVAILABLE

#### Error Path
```
processor.go:547 → UpdateBook.Execute()
 → update_orderbook.go:142 → store.Save(ctx, snap)
 → committer.go:48 → hot.Save(ctx, snap)
 → writer.go:75 → PgWriter.Save(ctx, snap)
 → writer_helpers.go:51 → exec.Exec(ctx, upsertSQL, ...)
 → pool.go:94-96 → p.pool.Exec() → wraps error as SYS_UNAVAILABLE
```

### Root Cause: MISSING ERROR DETAILS
The error code `SYS_UNAVAILABLE` is being logged, but the actual pgx error message is NOT being logged.
This makes diagnosis impossible without code changes.

### Action Required
Add detailed error logging in `pool.go:96` to see the actual database error:
```go
if err != nil {
    return 0, problem.Wrap(err, problem.Unavailable, fmt.Sprintf("timescale exec failed: %v", err))
}
```

Current issue: We only log the problem code, not the wrapped error details.

---

## RESOLUTION (2026-02-21)

### Root Cause
**ClickHouse table `aggregation_orderbook_snapshot_cold` was missing.**

The error was NOT with TimescaleDB (hot storage) - it was with ClickHouse (cold storage). The `SnapshotCommitter` writes to BOTH stores and fails if either write fails.

### Why Migrations Didn't Run
1. `make down` removed ClickHouse data volume
2. On restart, ClickHouse created initial directory structure
3. Docker entrypoint only runs init scripts on FIRST initialization
4. Since data directory existed (with basic files), init scripts were skipped
5. Result: No tables created

### Fix Applied
1. **Added detailed error logging** in `pool.go` and `processor.go` to expose actual errors
2. **Manually created ClickHouse table** with proper schema
3. **Restarted processors** to reconnect to ClickHouse with fresh connections

### Commands Used
```bash
# Created missing table
docker exec market-raccoon-clickhouse clickhouse-client --port 9000 \
  --user default --password password --query \
  "CREATE TABLE IF NOT EXISTS aggregation_orderbook_snapshot_cold (...)"

# Restarted processors to reconnect
docker restart compose-processor-1 compose-processor-2
```

### Verification
- ✅ Zero errors in processor logs after restart
- ✅ Data flowing into TimescaleDB (hot): 5+ rows confirmed
- ✅ Data flowing into ClickHouse (cold): 5+ rows confirmed
- ✅ Both processor replicas operational

### Permanent Fix Needed
The `make down` target should NOT remove volumes by default, or we need a proper migration runner that checks and applies migrations on startup regardless of data directory state.

Consider:
- Use `docker compose down` instead of `docker compose down -v`
- Or implement a migration runner that runs on every startup
- Or document manual migration steps after volume wipe
