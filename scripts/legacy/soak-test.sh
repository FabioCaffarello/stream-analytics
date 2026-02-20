#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_FILE=".context/evidence/w5-soak.txt"
GO_CACHE_DIR="/tmp/go-build"
WS_TEST_PATTERN='TestConsumer_ConnectDisconnectCycle_(NoGoroutineLeak|HeapStable)'
BOUNDEDMAP_TEST_PATTERN='TestBoundedMap_(ConcurrentAccess|EvictBySizeLRU|EvictByTTL)'

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
		--ws-pattern)
			WS_TEST_PATTERN="${2:?missing value for --ws-pattern}"
			shift 2
			;;
		--boundedmap-pattern)
			BOUNDEDMAP_TEST_PATTERN="${2:?missing value for --boundedmap-pattern}"
			shift 2
			;;
		*)
			echo "unknown argument: $1" >&2
			echo "usage: $0 [--out-file <path>] [--go-cache <path>] [--ws-pattern <regex>] [--boundedmap-pattern <regex>]" >&2
			exit 2
			;;
	esac
done

mkdir -p "$(dirname "$OUT_FILE")"
 : >"$OUT_FILE"

echo "# W5 soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"
echo "go_cache=$GO_CACHE_DIR" | tee -a "$OUT_FILE"
echo "ws_pattern=$WS_TEST_PATTERN" | tee -a "$OUT_FILE"
echo "boundedmap_pattern=$BOUNDEDMAP_TEST_PATTERN" | tee -a "$OUT_FILE"

echo "Running consumer lifecycle leak checks..." | tee -a "$OUT_FILE"
(
  cd internal/actors
  GOCACHE="$GO_CACHE_DIR" go test ./marketdata/ws -run "$WS_TEST_PATTERN" -count=1 -v
:) 2>&1 | tee -a "$OUT_FILE"

echo "Running bounded map stability checks..." | tee -a "$OUT_FILE"
(
  cd internal/shared
  GOCACHE="$GO_CACHE_DIR" go test ./ds -run "$BOUNDEDMAP_TEST_PATTERN" -count=1 -v
:) 2>&1 | tee -a "$OUT_FILE"

echo "Soak harness completed" | tee -a "$OUT_FILE"
