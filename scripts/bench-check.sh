#!/usr/bin/env bash
# bench-check.sh — run hot-path benchmarks and compare against committed baseline.
# Fails if benchstat detects a significant regression ≥ THRESHOLD (default 15%).
#
# Usage:  scripts/bench-check.sh
#         BENCH_THRESHOLD=20 scripts/bench-check.sh   # custom threshold
# Requires: go, benchstat (golang.org/x/perf/cmd/benchstat)
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

BASELINE=".benchmarks/baseline.txt"
THRESHOLD="${BENCH_THRESHOLD:-15}"
CURRENT=$(mktemp)
trap 'rm -f "$CURRENT"' EXIT

if [[ ! -f "$BASELINE" ]]; then
  echo "bench-check: baseline not found at $BASELINE — run 'make bench-baseline' first"
  exit 1
fi

# ── locate benchstat ─────────────────────────────────────────────────────────
BENCHSTAT="${BENCHSTAT:-}"
if [[ -z "$BENCHSTAT" ]]; then
  BENCHSTAT="$(command -v benchstat 2>/dev/null || true)"
fi
if [[ -z "$BENCHSTAT" ]]; then
  BENCHSTAT_VERSION="${BENCHSTAT_VERSION:-v0.0.0-20260211190930-8161c38c6cdc}"
  echo "bench-check: benchstat not in PATH — installing ${BENCHSTAT_VERSION} to /tmp/gobin"
  GOBIN=/tmp/gobin go install "golang.org/x/perf/cmd/benchstat@${BENCHSTAT_VERSION}"
  BENCHSTAT=/tmp/gobin/benchstat
fi

# ── run current benchmarks ───────────────────────────────────────────────────
echo "bench-check: running hot-path benchmarks (count=5) …"
go test -run='^$' -bench=HotPath -benchmem -count=5 \
  ./internal/shared/codec ./internal/shared/policykit ./internal/shared/hash \
  > "$CURRENT" 2>&1
go test -run='^$' -bench=BenchmarkIngest -benchmem -count=5 \
  ./internal/core/marketdata/app \
  >> "$CURRENT" 2>&1
go test -run='^$' -bench=BenchmarkApplyDelta -benchmem -count=5 \
  ./internal/core/aggregation/domain \
  >> "$CURRENT" 2>&1
go test -run='^$' -bench=BenchmarkE2E -benchmem -count=5 \
  ./internal/core/aggregation/app \
  >> "$CURRENT" 2>&1
go test -run='^$' -bench=BenchmarkDeliveryFanOut -benchmem -count=5 \
  ./internal/actors/delivery/runtime \
  >> "$CURRENT" 2>&1
go test -run='^$' -bench=BenchmarkSessionWrite -benchmem -count=5 \
  ./internal/interfaces/ws \
  >> "$CURRENT" 2>&1

echo "bench-check: comparing against baseline …"
echo ""

# benchstat exits 0 even on regression; we parse for "+XX%" with significance.
REPORT=$("$BENCHSTAT" "$BASELINE" "$CURRENT" 2>&1) || true
echo "$REPORT"
echo ""

# Detect regressions: extract "+XX.YY% (p=0.0ZZ)" lines and fail if XX ≥ THRESHOLD.
FAIL=0
while IFS= read -r line; do
  pct=$(echo "$line" | grep -oE '\+([0-9]+)\.[0-9]+%' | head -1 | tr -d '+%')
  if [[ -n "$pct" ]]; then
    int_pct=${pct%%.*}
    if (( int_pct >= THRESHOLD )); then
      echo "bench-check: regression ≥ ${THRESHOLD}% found: $line"
      FAIL=1
    fi
  fi
done <<< "$(echo "$REPORT" | grep -E '\+[0-9]+\.[0-9]+%\s+\(p=0\.0[0-4]')" || true

if (( FAIL )); then
  echo "bench-check: REGRESSION detected — see benchstat output above"
  exit 1
fi

echo "bench-check: no significant regression detected (threshold=${THRESHOLD}%) ✓"
