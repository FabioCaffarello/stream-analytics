#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

OUT_FILE=".context/evidence/c4-ws-delivery-soak.txt"
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

echo "# C4 WS+delivery soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"
echo "go_cache=$GO_CACHE_DIR" | tee -a "$OUT_FILE"

echo "Running full vertical timer snapshot soak..." | tee -a "$OUT_FILE"
MR_ENABLE_SOAK=1 GOCACHE="$GO_CACHE_DIR" \
	go test ./cmd/processor -run 'TestSoak_FullVertical_TimerSnapshots' \
	-count=1 -v -timeout=15m 2>&1 | tee -a "$OUT_FILE"

echo "Running WS backpressure mixed-client soak..." | tee -a "$OUT_FILE"
MR_ENABLE_SOAK=1 GOCACHE="$GO_CACHE_DIR" \
	go test ./internal/interfaces/ws -run 'TestSoak_WSBackpressure_MixedClients60' \
	-count=1 -v -timeout=20m 2>&1 | tee -a "$OUT_FILE"

echo "Running guardian crash/restart soak..." | tee -a "$OUT_FILE"
MR_ENABLE_SOAK=1 GOCACHE="$GO_CACHE_DIR" \
	go test ./internal/actors/runtime -run 'TestSoak_Guardian_CrashRestart_500' \
	-count=1 -v -timeout=10m 2>&1 | tee -a "$OUT_FILE"

echo "C4 WS+delivery soak harness completed" | tee -a "$OUT_FILE"
