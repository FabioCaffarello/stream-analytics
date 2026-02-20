#!/usr/bin/env bash
# bench-budget.sh — run hot-path benchmarks and enforce per-benchmark allocation budgets.
# Complements bench-check.sh (regression detection) with absolute allocation limits.
#
# Usage:  scripts/bench-budget.sh
#         BENCH_BUDGET_COUNT=3 scripts/bench-budget.sh   # more samples
#
# Requires: go
set -euo pipefail

root_dir="$(builtin cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
builtin cd "$root_dir"

GO="${GO:-go}"
BUDGETS=".benchmarks/budgets.txt"
COUNT="${BENCH_BUDGET_COUNT:-1}"
CURRENT=$(mktemp)
trap 'rm -f "$CURRENT"' EXIT

if [[ ! -f "$BUDGETS" ]]; then
  echo "bench-budget: budget file not found at $BUDGETS"
  echo "  Create it with: BenchmarkName<tab>MaxAllocs per line"
  exit 1
fi

# ── run hot-path benchmarks ─────────────────────────────────────────────────
echo "bench-budget: running hot-path benchmarks (count=${COUNT}) …"
echo ""

$GO test -run='^$' -bench=HotPath -benchmem -count="$COUNT" \
  ./internal/shared/codec ./internal/shared/policykit ./internal/shared/hash \
  > "$CURRENT" 2>&1

$GO test -run='^$' -bench=BenchmarkIngest -benchmem -count="$COUNT" \
  ./internal/core/marketdata/app \
  >> "$CURRENT" 2>&1

$GO test -run='^$' -bench=BenchmarkApplyDelta -benchmem -count="$COUNT" \
  ./internal/core/aggregation/domain \
  >> "$CURRENT" 2>&1

$GO test -run='^$' -bench=BenchmarkE2E -benchmem -count="$COUNT" \
  ./internal/core/aggregation/app \
  >> "$CURRENT" 2>&1

$GO test -run='^$' -bench=BenchmarkDeliveryFanOut -benchmem -count="$COUNT" \
  ./internal/actors/delivery/runtime \
  >> "$CURRENT" 2>&1

$GO test -run='^$' -bench=BenchmarkSessionWrite -benchmem -count="$COUNT" \
  ./internal/interfaces/ws \
  >> "$CURRENT" 2>&1

# ── check allocation budgets ────────────────────────────────────────────────
echo "bench-budget: checking allocation budgets …"
echo ""
printf "  %-6s  %-55s  %s\n" "STATUS" "BENCHMARK" "ALLOCS (measured / budget)"
printf "  %-6s  %-55s  %s\n" "------" "---------" "-------------------------"

FAIL=0
PASS=0
SKIP=0

while IFS=$'\t' read -r bench_name max_allocs || [[ -n "$bench_name" ]]; do
  # skip comments and blank lines
  [[ "$bench_name" =~ ^[[:space:]]*# ]] && continue
  [[ -z "${bench_name// /}" ]] && continue

  # strip inline comments and trailing whitespace from max_allocs
  max_allocs="${max_allocs%%#*}"
  max_allocs="${max_allocs%"${max_allocs##*[! ]}"}"

  # find matching benchmark line(s) in output, take worst-case (max allocs)
  measured=""
  while IFS= read -r line; do
    allocs=$(echo "$line" | grep -oE '[0-9]+ allocs/op' | grep -oE '^[0-9]+' || true)
    if [[ -n "$allocs" ]]; then
      if [[ -z "$measured" ]] || (( allocs > measured )); then
        measured="$allocs"
      fi
    fi
  done < <(grep -E "^${bench_name}-[0-9]+" "$CURRENT" 2>/dev/null || true)

  if [[ -z "$measured" ]]; then
    printf "  %-6s  %-55s  %s\n" "SKIP" "$bench_name" "(not found in output)"
    SKIP=$((SKIP + 1))
    continue
  fi

  if (( measured > max_allocs )); then
    printf "  %-6s  %-55s  %s\n" "FAIL" "$bench_name" "$measured / $max_allocs"
    FAIL=$((FAIL + 1))
  else
    printf "  %-6s  %-55s  %s\n" "PASS" "$bench_name" "$measured / $max_allocs"
    PASS=$((PASS + 1))
  fi
done < "$BUDGETS"

echo ""
echo "bench-budget: $PASS passed, $FAIL failed, $SKIP skipped ($(( PASS + FAIL )) checked)"

if (( FAIL > 0 )); then
  echo "bench-budget: BUDGET EXCEEDED — fix hot-path allocations before merging"
  exit 1
fi

echo "bench-budget: all allocation budgets within limits ✓"
