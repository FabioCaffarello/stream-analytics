#!/usr/bin/env bash
# backup-timescaledb.sh — dump TimescaleDB (raccoon) to a compressed SQL file.
#
# Usage:
#   ./scripts/ops/backup-timescaledb.sh [--output DIR] [--container NAME]
#
# Defaults:
#   --output    ./backups/timescaledb
#   --container market-raccoon-timescale
#
# Environment (or deploy/envs/local.env):
#   TIMESCALE_USER     (default: raccoon)
#   TIMESCALE_DB       (default: raccoon)
set -euo pipefail

BACKUP_DIR="./backups/timescaledb"
CONTAINER="market-raccoon-timescale"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)    BACKUP_DIR="$2"; shift 2 ;;
    --container) CONTAINER="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

TSDB_USER="${TIMESCALE_USER:-raccoon}"
TSDB_DB="${TIMESCALE_DB:-raccoon}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
FILENAME="timescaledb-${TSDB_DB}-${TIMESTAMP}.sql.gz"

mkdir -p "$BACKUP_DIR"

echo "backup-timescaledb: dumping ${TSDB_DB}@${CONTAINER} ..."

docker exec "$CONTAINER" \
  pg_dump -U "$TSDB_USER" -d "$TSDB_DB" --no-owner --no-acl \
  | gzip > "${BACKUP_DIR}/${FILENAME}"

SIZE="$(du -h "${BACKUP_DIR}/${FILENAME}" | cut -f1)"
echo "backup-timescaledb: saved ${BACKUP_DIR}/${FILENAME} (${SIZE})"
