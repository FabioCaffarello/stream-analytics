#!/usr/bin/env bash
# backup-clickhouse.sh — export ClickHouse tables to compressed native format.
#
# Usage:
#   ./scripts/ops/backup-clickhouse.sh [--output DIR] [--container NAME]
#
# Defaults:
#   --output    ./backups/clickhouse
#   --container market-raccoon-clickhouse
#
# Environment (or deploy/envs/local.env):
#   CLICKHOUSE_USER     (default: default)
#   CLICKHOUSE_PASSWORD (required)
#   CLICKHOUSE_DB       (default: default)
set -euo pipefail

BACKUP_DIR="./backups/clickhouse"
CONTAINER="market-raccoon-clickhouse"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)    BACKUP_DIR="$2"; shift 2 ;;
    --container) CONTAINER="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

CH_USER="${CLICKHOUSE_USER:-default}"
CH_PASSWORD="${CLICKHOUSE_PASSWORD:?Set CLICKHOUSE_PASSWORD}"
CH_DB="${CLICKHOUSE_DB:-default}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTDIR="${BACKUP_DIR}/${TIMESTAMP}"

mkdir -p "$OUTDIR"

echo "backup-clickhouse: discovering tables in ${CH_DB}@${CONTAINER} ..."

TABLES="$(docker exec "$CONTAINER" \
  clickhouse-client --user "$CH_USER" --password "$CH_PASSWORD" \
    --query "SELECT name FROM system.tables WHERE database = '${CH_DB}' AND engine NOT IN ('View', 'MaterializedView') FORMAT TabSeparated")"

if [[ -z "$TABLES" ]]; then
  echo "backup-clickhouse: no tables found in database ${CH_DB}"
  exit 0
fi

for table in $TABLES; do
  echo "  exporting ${CH_DB}.${table} ..."
  docker exec "$CONTAINER" \
    clickhouse-client --user "$CH_USER" --password "$CH_PASSWORD" \
      --query "SELECT * FROM ${CH_DB}.${table} FORMAT Native" \
    | gzip > "${OUTDIR}/${table}.native.gz"
done

echo ""
echo "backup-clickhouse: schema dump ..."
docker exec "$CONTAINER" \
  clickhouse-client --user "$CH_USER" --password "$CH_PASSWORD" \
    --query "SELECT name, create_table_query FROM system.tables WHERE database = '${CH_DB}' FORMAT TabSeparated" \
  > "${OUTDIR}/_schema.tsv"

SIZE="$(du -sh "$OUTDIR" | cut -f1)"
echo "backup-clickhouse: saved to ${OUTDIR}/ (${SIZE} total)"
