#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

OUT_DIR=".context/evidence"
mkdir -p "$OUT_DIR"
OUT_FILE="$OUT_DIR/w5-soak.txt"
: > "$OUT_FILE"

echo "# W5 soak harness $(date -u +%Y-%m-%dT%H:%M:%SZ)" | tee -a "$OUT_FILE"
echo "Running consumer lifecycle leak checks..." | tee -a "$OUT_FILE"
(
  cd internal/actors
  GOCACHE=/tmp/go-build go test ./marketdata/ws -run 'TestConsumer_ConnectDisconnectCycle_(NoGoroutineLeak|HeapStable)' -count=1 -v
) 2>&1 | tee -a "$OUT_FILE"

echo "Running bounded map stability checks..." | tee -a "$OUT_FILE"
(
  cd internal/shared
  GOCACHE=/tmp/go-build go test ./ds -run 'TestBoundedMap_(ConcurrentAccess|EvictBySizeLRU|EvictByTTL)' -count=1 -v
) 2>&1 | tee -a "$OUT_FILE"

echo "Soak harness completed" | tee -a "$OUT_FILE"
