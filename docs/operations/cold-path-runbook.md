# Cold-Path Store Pipeline — Operational Runbook

**Status:** Active
**Last updated:** 2026-02-19

## Overview

The cold-path pipeline persists aggregation snapshots to ClickHouse for
replay and analytics.  The data flow is:

```
JetStream consumer (store-v1)
  -> handleStoreEnvelope (decode + route)
    -> BatchWriter.Write (flush-on-size / shutdown drain)
      -> Writer.SaveIdempotent (ClickHouse insert)
        -> ACK on success / NAK on transient failure / TERM on permanent failure
```

Key invariant: **ack-on-commit** — the JetStream message is only ACKed
after the underlying writer has committed successfully.

## Operational Tooling (C3)

> **Note:** `cmd/backfill` was retired in S9 (codex/s9-legacy-removal-cutover). The binary no longer exists in the repository. The commands below are preserved for historical reference only.

### Backfill fixture generation (RETIRED)

```bash
# RETIRED — cmd/backfill was removed in S9
go run ./cmd/backfill \
  --mode download \
  --exchange binance \
  --symbol BTCUSDT \
  --from 2025-01-01 \
  --to 2025-01-03 \
  --market-type USD_M_FUTURES \
  --output-dir ./backfill \
  --fixture ./fixtures/binance-btcusdt-2025-01-01-2025-01-03.jsonl
```

Expected result: exit code `0` and a JSONL fixture path printed in stdout.

### Candle gap detection (RETIRED)

```bash
# RETIRED — cmd/backfill was removed in S9
go run ./cmd/backfill \
  --mode gaps \
  --exchange binance \
  --symbol BTCUSDT \
  --timeframe 1m \
  --from 2025-01-01 \
  --to 2025-01-07
```

Expected result:
- exit code `0`: no gaps found in queried range
- exit code `1`: one or more gaps found (gap windows are printed)

## Configuration

Batch settings in `store.jsonc`:

```jsonc
{
  "store": {
    "clickhouse": {
      "dsn": "clickhouse://default:password@localhost:9000/default"
    },
    "batch": {
      "max_rows": 1,           // flush after N rows (1 = write-through)
      "max_bytes": 0,          // flush after N bytes (0 = disabled)
      "flush_interval": "100ms" // time-based flush fallback
    }
  }
}
```

| Field              | Default  | Description                                    |
|--------------------|----------|------------------------------------------------|
| `batch.max_rows`   | `1`      | Flush after N rows. Increase for batch inserts |
| `batch.max_bytes`  | `0`      | Byte-size flush threshold (0 = disabled)       |
| `batch.flush_interval` | `100ms` | Time-based flush fallback                  |

With `max_rows=1` (default), every write flushes immediately — preserving
strict ack-on-commit semantics with the current serial JetStream consumer.

## Schema Migrations

Migrations live in `sql/clickhouse/migrations/`:

| File                           | Table                      | Purpose                |
|--------------------------------|----------------------------|------------------------|
| `0001_m1_writer_skeleton.sql`  | `aggregation_snapshots_v1` | Initial skeleton       |
| `0002_w2_cold_correctness.sql` | `aggregation_snapshots_v2` | Idempotency key + hash |
| `0003_w4_ttl_partition.sql`    | `aggregation_snapshots_v3` | TTL + partitioning     |

The v3 table adds:
- `ts DateTime64(3)` — millisecond-precision timestamp for TTL and partitioning
- `PARTITION BY toYYYYMM(ts)` — monthly partitions for efficient drops
- `TTL toDateTime(ts) + INTERVAL 90 DAY` — automatic 90-day expiry

### Changing TTL

```sql
ALTER TABLE aggregation_snapshots_v3 MODIFY TTL toDateTime(ts) + INTERVAL 180 DAY;
```

### Dropping old partitions manually

```sql
-- List partitions
SELECT partition, count() FROM system.parts
WHERE table = 'aggregation_snapshots_v3' AND active
GROUP BY partition ORDER BY partition;

-- Drop a specific month
ALTER TABLE aggregation_snapshots_v3 DROP PARTITION '202501';
```

## Metrics

| Metric                          | Type      | Labels           | Description                         |
|---------------------------------|-----------|------------------|-------------------------------------|
| `store_consumed_total`          | Counter   | status, reason   | Total envelopes consumed            |
| `store_commit_total`            | Counter   | status           | Commits (ok / failed)               |
| `store_commit_latency_seconds`  | Histogram | —                | Per-commit latency                  |
| `store_quarantine_total`        | Counter   | reason           | Quarantined (decode failure) count  |
| `store_batch_size`              | Histogram | —                | Rows per flushed batch              |
| `store_flush_total`             | Counter   | status           | Batch flushes (ok / failed)         |
| `store_flush_latency_seconds`   | Histogram | —                | Per-flush latency                   |

## Alerts

### StoreCommitLatencyHigh

**Fires when**: p95 commit latency > 1s for 5 minutes.

**Likely cause**: ClickHouse disk I/O saturation, merge backlog, or network
issues between store and ClickHouse.

**Response**:
1. Check ClickHouse system.merges for active merges
2. Check disk I/O with `iostat` or ClickHouse `system.disks`
3. If sustained, consider increasing `batch.max_rows` to amortize insert overhead

### StoreFlushFailures

**Fires when**: `store_flush_total{status="failed"}` is increasing.

**Likely cause**: ClickHouse connection failure, schema mismatch, or
idempotency conflict (payload hash mismatch for same key).

**Response**:
1. Check store logs for specific error messages
2. Verify ClickHouse connectivity: `clickhouse-client --query 'SELECT 1'`
3. Check for schema contract failures in startup logs
4. If idempotency conflict, check for publisher bug emitting different payloads
   with the same (venue, instrument, seq, source_idempotency_key)

### StoreQuarantineRateHigh

**Fires when**: `store_quarantine_total{reason="decode"}` rate > 0 for 15 min.

**Likely cause**: Publisher emitting malformed JSON payloads.

**Response**:
1. Check processor logs for serialization errors
2. Verify envelope schema version matches expected (aggregation.snapshot v1)
3. If widespread, check for protobuf/JSON content-type mismatch

### ConsumerLagGrowingNoProgress

**Fires when**: JetStream consumer lag is increasing while store commit rate
is near zero.

**Likely cause**: Pipeline stall — store process crashed, ClickHouse down,
or all messages failing permanently (TERM).

**Response**:
1. Check store process health: `curl http://store:8083/readyz`
2. Check ClickHouse health: `curl http://clickhouse:8123/ping`
3. Check JetStream consumer info for pending/redelivery counts
4. If store is healthy but lag grows, check for permanent failures in logs

## Troubleshooting

### Store not starting

1. Check schema contract validation in startup logs
2. Verify migration files exist in `sql/clickhouse/migrations/`
3. Check ClickHouse DSN connectivity

### Duplicate snapshots in ClickHouse

The `ReplacingMergeTree` engine deduplicates rows with identical ORDER BY
keys during background merges.  Before merge completes, `SELECT` may return
duplicates.  Use `FINAL` modifier for exact reads:

```sql
SELECT * FROM aggregation_snapshots_v3 FINAL
WHERE venue = 'binance' AND instrument = 'BTCUSDT'
ORDER BY seq DESC LIMIT 10;
```

### High memory usage

1. Check `store_batch_size` histogram — very large batches consume memory
2. Reduce `batch.max_rows` if batches are too large
3. Check ClickHouse buffer table settings if using intermediate buffers

### Redelivery storm

If JetStream redelivers the same messages repeatedly:

1. Check `store_consumed_total{reason="skipped"}` — high skip rate is normal
   for unhandled event types
2. Check `store_flush_total{status="failed"}` — flush failures cause NAK,
   which triggers redelivery
3. Check `max_deliver` setting — messages exceeding max_deliver are TERMed
4. Verify ack_wait is sufficient for ClickHouse write latency
