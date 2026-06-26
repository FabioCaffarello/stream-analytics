#!/usr/bin/env bash
# restore-timescaledb.sh — restore a TimescaleDB dump.
#
# Usage:
#   ./scripts/ops/restore-timescaledb.sh BACKUP_FILE [--container NAME]
#
# BACKUP_FILE: path to a .sql.gz file produced by backup-timescaledb.sh
#
# Environment (or deploy/envs/local.env):
#   TIMESCALE_USER     (default: raccoon)
#   TIMESCALE_DB       (default: raccoon)
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 BACKUP_FILE [--container NAME]" >&2
  exit 1
fi

BACKUP_FILE="$1"; shift
CONTAINER="stream-analytics-timescale"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --container) CONTAINER="$2"; shift 2 ;;
    *) echo "Unknown flag: $1" >&2; exit 1 ;;
  esac
done

TSDB_USER="${TIMESCALE_USER:-raccoon}"
TSDB_DB="${TIMESCALE_DB:-raccoon}"

if [[ ! -f "$BACKUP_FILE" ]]; then
  echo "restore-timescaledb: file not found: $BACKUP_FILE" >&2
  exit 1
fi

echo "restore-timescaledb: restoring ${BACKUP_FILE} into ${TSDB_DB}@${CONTAINER} ..."
echo "  WARNING: this will overwrite existing data in ${TSDB_DB}."
echo "  Press Ctrl+C within 5 seconds to abort."
sleep 5

gunzip -c "$BACKUP_FILE" \
  | docker exec -i "$CONTAINER" \
      psql -U "$TSDB_USER" -d "$TSDB_DB" --single-transaction --set ON_ERROR_STOP=on

echo "restore-timescaledb: restore complete."
