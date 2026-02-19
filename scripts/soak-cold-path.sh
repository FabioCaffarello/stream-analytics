#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_FILE=".context/evidence/w2-cold-path-soak.txt"
GO_CACHE_DIR="/tmp/go-build"
STORAGE_TEST_PATTERN='TestStorageSoak_Burst10x60s_CommitAckInvariants'
VPVR_TEST_PATTERN='TestIntegrationVPVROverload_AckBoundarySafeAndDeterministic'

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
		--storage-pattern)
			STORAGE_TEST_PATTERN="${2:?missing value for --storage-pattern}"
			shift 2
			;;
		--vpvr-pattern)
			VPVR_TEST_PATTERN="${2:?missing value for --vpvr-pattern}"
			shift 2
			;;
		*)
			echo "unknown argument: $1" >&2
			echo "usage: $0 [--out-file <path>] [--go-cache <path>] [--storage-pattern <regex>] [--vpvr-pattern <regex>]" >&2
			exit 2
			;;
	esac
done

mkdir -p "$(dirname "$OUT_FILE")"
: >"$OUT_FILE"

echo "# W2 cold-path soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"
echo "go_cache=$GO_CACHE_DIR" | tee -a "$OUT_FILE"
echo "storage_pattern=$STORAGE_TEST_PATTERN" | tee -a "$OUT_FILE"
echo "vpvr_pattern=$VPVR_TEST_PATTERN" | tee -a "$OUT_FILE"

echo "Running storage commit/ack soak..." | tee -a "$OUT_FILE"
GOCACHE="$GO_CACHE_DIR" go test ./internal/adapters/storage -run "$STORAGE_TEST_PATTERN" -count=1 -v 2>&1 | tee -a "$OUT_FILE"

echo "Running VPVR deterministic ack boundary integration..." | tee -a "$OUT_FILE"
GOCACHE="$GO_CACHE_DIR" go test ./internal/adapters/storage -run "$VPVR_TEST_PATTERN" -count=1 -v 2>&1 | tee -a "$OUT_FILE"

echo "W2 cold-path soak harness completed" | tee -a "$OUT_FILE"
