#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_FILE=".context/evidence/vpvr-soak.txt"
GO_CACHE_DIR="/tmp/go-build"
TEST_PATTERN='TestVPVROverloadSoakBurstDeterministicBudgets'

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
			TEST_PATTERN="${2:?missing value for --pattern}"
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

echo "# VPVR soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"
echo "go_cache=$GO_CACHE_DIR" | tee -a "$OUT_FILE"
echo "test_pattern=$TEST_PATTERN" | tee -a "$OUT_FILE"

echo "Running VPVR deterministic burst soak..." | tee -a "$OUT_FILE"
(
	cd internal/actors
	GOCACHE="$GO_CACHE_DIR" go test ./insights/runtime -run "$TEST_PATTERN" -count=1 -v
) 2>&1 | tee -a "$OUT_FILE"

echo "VPVR soak harness completed" | tee -a "$OUT_FILE"
