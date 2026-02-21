#!/usr/bin/env bash
# restore-clickhouse.sh — restore ClickHouse tables from native format backup.
#
# Usage:
#   ./scripts/ops/restore-clickhouse.sh BACKUP_DIR [--container NAME]
#
# BACKUP_DIR: directory produced by backup-clickhouse.sh (contains *.native.gz + _schema.tsv)
#
# Environment (or deploy/envs/local.env):
#   CLICKHOUSE_USER     (default: default)
#   CLICKHOUSE_PASSWORD (required)
#   CLICKHOUSE_DB       (default: default)
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 BACKUP_DIR [--container NAME]" >&2
  exit 1
fi

BACKUP_DIR="$1"; shift
CONTAINER="market-raccoon-clickhouse"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --container) CONTAINER="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

CH_USER="${CLICKHOUSE_USER:-default}"
CH_PASSWORD="${CLICKHOUSE_PASSWORD:?Set CLICKHOUSE_PASSWORD}"
CH_DB="${CLICKHOUSE_DB:-default}"

if [[ ! -d "$BACKUP_DIR" ]]; then
  echo "restore-clickhouse: directory not found: $BACKUP_DIR" >&2
  exit 1
fi

SCHEMA_FILE="${BACKUP_DIR}/_schema.tsv"
if [[ ! -f "$SCHEMA_FILE" ]]; then
  echo "restore-clickhouse: schema file not found: $SCHEMA_FILE" >&2
  exit 1
fi

echo "restore-clickhouse: restoring from ${BACKUP_DIR} into ${CH_DB}@${CONTAINER} ..."
echo "  WARNING: this will TRUNCATE and re-insert data for each table."
echo "  Press Ctrl+C within 5 seconds to abort."
sleep 5

# Re-create tables from schema if they don't exist
while IFS=$'\t' read -r table_name create_query; do
  echo "  ensuring table ${CH_DB}.${table_name} exists ..."
  docker exec "$CONTAINER" \
    clickhouse-client --user "$CH_USER" --password "$CH_PASSWORD" \
      --query "$create_query" 2>/dev/null || true
done < "$SCHEMA_FILE"

# Restore data
for gz_file in "${BACKUP_DIR}"/*.native.gz; do
  [[ -f "$gz_file" ]] || continue
  table="$(basename "$gz_file" .native.gz)"
  echo "  restoring ${CH_DB}.${table} ..."

  docker exec "$CONTAINER" \
    clickhouse-client --user "$CH_USER" --password "$CH_PASSWORD" \
      --query "TRUNCATE TABLE IF EXISTS ${CH_DB}.${table}"

  gunzip -c "$gz_file" \
    | docker exec -i "$CONTAINER" \
        clickhouse-client --user "$CH_USER" --password "$CH_PASSWORD" \
          --query "INSERT INTO ${CH_DB}.${table} FORMAT Native"
done

echo "restore-clickhouse: restore complete."
