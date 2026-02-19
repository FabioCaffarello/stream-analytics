#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_FILE=".context/evidence/s3-store-soak.txt"
GO_CACHE_DIR="/tmp/go-build"
PATTERN='TestStoreSoak_'

while [[ $# -gt 0 ]]; do
	case "$1" in
		--out-file)
			OUT_FILE="${2:?missing value for --out-file}"
			shift 2
			;;
		--go-cache)
			GO_CACHE_DIR="${2:?missing value for --go-cache}"
			shift 2
			;;
		--pattern)
			PATTERN="${2:?missing value for --pattern}"
			shift 2
			;;
		*)
			echo "unknown argument: $1" >&2
			echo "usage: $0 [--out-file <path>] [--go-cache <path>] [--pattern <regex>]" >&2
			exit 2
			;;
	esac
done

mkdir -p "$(dirname "$OUT_FILE")"
: >"$OUT_FILE"

echo "# S3 store soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"
echo "go_cache=$GO_CACHE_DIR" | tee -a "$OUT_FILE"
echo "pattern=$PATTERN" | tee -a "$OUT_FILE"

echo "Running store soak tests..." | tee -a "$OUT_FILE"
GOCACHE="$GO_CACHE_DIR" go test ./cmd/store -run "$PATTERN" -count=1 -v -timeout=15m 2>&1 | tee -a "$OUT_FILE"

echo "S3 store soak harness completed" | tee -a "$OUT_FILE"
