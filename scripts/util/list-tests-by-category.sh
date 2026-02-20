#!/usr/bin/env bash
set -euo pipefail

# List test files grouped by category keywords: soak, integration, e2e, bench, unit, other.
# Uses ripgrep (rg) when available for speed; falls back to git grep or grep.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RG="$(command -v rg || true)"
GIT="$(command -v git || true)"

match_regex() {
    pattern="$1"
    text="$2"
    if [ -n "$RG" ]; then
        printf '%s\n' "$text" | "$RG" -qi -- "$pattern"
    else
        printf '%s\n' "$text" | grep -qiE "$pattern"
    fi
}

find_test_files() {
    if [ -n "$RG" ]; then
        rg --files --glob '**/*_test.go'
    else
        # fallback: use git ls-files if in a repo, else find
        if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
            git ls-files -- '**/*_test.go'
        else
            find . -type f -name '*_test.go' -print
        fi
    fi
}

printf "Scanning test files...\n\n"

soak_count=0
integration_count=0
e2e_count=0
bench_count=0
unit_count=0
other_count=0

while IFS= read -r f; do
    fname="$(basename "$f")"
    lower="$(echo "$fname" | tr '[:upper:]' '[:lower:]')"
    if match_regex 'soak|vpvr|store-soak|soak_' "$f"; then
        echo "SOAK       : $f"
        soak_count=$((soak_count+1))
    elif match_regex 'integration|e2e|conformance|end2end' "$f"; then
        echo "INTEGRATION: $f"
        integration_count=$((integration_count+1))
    elif match_regex 'e2e|end2end' "$f"; then
        echo "E2E        : $f"
        e2e_count=$((e2e_count+1))
    elif match_regex 'bench|benchmark|_bench' "$f"; then
        echo "BENCH      : $f"
        bench_count=$((bench_count+1))
    elif match_regex 'test_|unit|_unit' "$f" || match_regex '^cmd/|internal/.*' "$f"; then
        echo "UNIT       : $f"
        unit_count=$((unit_count+1))
    else
        echo "OTHER      : $f"
        other_count=$((other_count+1))
    fi
done < <(find_test_files)

printf "\nSummary:\n"
printf "  %10s: %d\n" "soak" "$soak_count"
printf "  %10s: %d\n" "integration" "$integration_count"
printf "  %10s: %d\n" "e2e" "$e2e_count"
printf "  %10s: %d\n" "bench" "$bench_count"
printf "  %10s: %d\n" "unit" "$unit_count"
printf "  %10s: %d\n" "other" "$other_count"

exit 0

exit 0
