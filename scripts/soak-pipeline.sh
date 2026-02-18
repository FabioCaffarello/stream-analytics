#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_FILE=".context/evidence/c4-pipeline-soak.txt"
GO_CACHE_DIR="/tmp/go-build"

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
		*)
			echo "unknown argument: $1" >&2
			echo "usage: $0 [--out-file <path>] [--go-cache <path>]" >&2
			exit 2
			;;
	esac
done

mkdir -p "$(dirname "$OUT_FILE")"
: >"$OUT_FILE"

echo "# C4 pipeline soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"
echo "go_cache=$GO_CACHE_DIR" | tee -a "$OUT_FILE"

echo "Running multi-exchange pipeline soak (10M events)..." | tee -a "$OUT_FILE"
MR_ENABLE_SOAK=1 GOCACHE="$GO_CACHE_DIR" \
	go test ./cmd/processor -run 'TestSoak_MultiExchange_10M_Messages' \
	-count=1 -v -timeout=30m 2>&1 | tee -a "$OUT_FILE"

echo "Running pipeline+delivery combined soak (100k events)..." | tee -a "$OUT_FILE"
MR_ENABLE_SOAK=1 GOCACHE="$GO_CACHE_DIR" \
	go test ./cmd/processor -run 'TestSoak_PipelineWithDelivery_100k' \
	-count=1 -v -timeout=15m 2>&1 | tee -a "$OUT_FILE"

echo "C4 pipeline soak completed" | tee -a "$OUT_FILE"
