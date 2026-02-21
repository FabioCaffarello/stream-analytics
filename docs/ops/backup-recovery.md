# Backup and Recovery

> Status: **Active** | Last updated: 2026-02-20

Market Raccoon uses dual storage (TimescaleDB hot + ClickHouse cold). Both require regular backups.

## Quick Reference

```bash
# Backup
make backup                        # both databases
make backup-timescaledb            # TimescaleDB only
make backup-clickhouse             # ClickHouse only

# Restore
make restore-timescaledb FILE=backups/timescaledb/timescaledb-raccoon-20260220T120000Z.sql.gz
make restore-clickhouse  DIR=backups/clickhouse/20260220T120000Z
```

## Backup Scripts

### TimescaleDB

**Script:** `scripts/ops/backup-timescaledb.sh`

Produces a `pg_dump` compressed SQL file. Includes all tables, hypertables, continuous aggregates, and indexes.

```bash
./scripts/ops/backup-timescaledb.sh [--output DIR] [--container NAME]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `./backups/timescaledb` | Destination directory |
| `--container` | `market-raccoon-timescale` | Docker container name |

**Output:** `timescaledb-{db}-{timestamp}.sql.gz`

### ClickHouse

**Script:** `scripts/ops/backup-clickhouse.sh`

Exports each table in ClickHouse Native format (efficient binary). Also saves schema definitions.

```bash
./scripts/ops/backup-clickhouse.sh [--output DIR] [--container NAME]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output` | `./backups/clickhouse` | Destination directory |
| `--container` | `market-raccoon-clickhouse` | Docker container name |

**Output:** `{timestamp}/{table}.native.gz` + `_schema.tsv`

## Restore Scripts

### TimescaleDB

**Script:** `scripts/ops/restore-timescaledb.sh`

Restores a backup file into the running TimescaleDB container. Uses `--single-transaction` for atomicity.

```bash
./scripts/ops/restore-timescaledb.sh BACKUP_FILE [--container NAME]
```

**Warning:** Overwrites existing data. 5-second abort window provided.

### ClickHouse

**Script:** `scripts/ops/restore-clickhouse.sh`

Re-creates tables from schema and inserts data from native format backups. Truncates existing tables before insert.

```bash
./scripts/ops/restore-clickhouse.sh BACKUP_DIR [--container NAME]
```

**Warning:** Truncates and replaces table data. 5-second abort window provided.

## Environment Variables

All scripts read from the environment (or `deploy/envs/local.env`):

| Variable | Default | Used By |
|----------|---------|---------|
| `TIMESCALE_USER` | `raccoon` | TimescaleDB scripts |
| `TIMESCALE_DB` | `raccoon` | TimescaleDB scripts |
| `CLICKHOUSE_USER` | `default` | ClickHouse scripts |
| `CLICKHOUSE_PASSWORD` | (required) | ClickHouse scripts |
| `CLICKHOUSE_DB` | `default` | ClickHouse scripts |

## Backup Strategy

| Aspect | TimescaleDB (hot) | ClickHouse (cold) |
|--------|-------------------|-------------------|
| Data | Order books, candles, stats, snapshots | Historical aggregates, heatmaps |
| Format | pg_dump SQL (gzipped) | Native binary (gzipped per table) |
| Frequency | Daily recommended | Weekly recommended |
| Retention | 7 daily + 4 weekly | 4 weekly |
| Recovery time | Minutes (single transaction) | Minutes (parallel table inserts) |

## Scheduled Backups

For production, add cron entries:

```cron
# Daily TimescaleDB backup at 02:00 UTC
0 2 * * * cd /opt/market-raccoon && ./scripts/ops/backup-timescaledb.sh --output /backups/timescaledb

# Weekly ClickHouse backup on Sundays at 03:00 UTC
0 3 * * 0 cd /opt/market-raccoon && ./scripts/ops/backup-clickhouse.sh --output /backups/clickhouse
```

## Recovery Procedure

1. **Stop application services** (consumer, processor, server, store)
2. **Ensure database containers are healthy** (`make ps`)
3. **Run restore script** with the backup file/directory
4. **Run migrations** to ensure schema is current: `make migrate`
5. **Restart application services**: `make up-core`
6. **Verify** via smoke test: `make smoke`
