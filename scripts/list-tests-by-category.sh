#!/usr/bin/env bash
set -euo pipefail

# List test files grouped by category keywords: soak, integration, e2e, bench, unit, other.
# Uses ripgrep (rg) when available for speed; falls back to git grep or grep.

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

RG="$(command -v rg || true)"
GREP="$(command -v git || true)"

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

declare -A buckets
buckets=( ["soak"]=0 ["integration"]=0 ["e2e"]=0 ["bench"]=0 ["unit"]=0 ["other"]=0 )

while IFS= read -r f; do
    fname="$(basename "$f")"
    lower="$(echo "$fname" | tr '[:upper:]' '[:lower:]')"
    if echo "$f" | rg -qi 'soak|vpvr|store-soak|soak_' >/dev/null 2>&1; then
        echo "SOAK       : $f"
        buckets[soak]=$((buckets[soak]+1))
    elif echo "$f" | rg -qi 'integration|e2e|conformance|end2end' >/dev/null 2>&1; then
        echo "INTEGRATION: $f"
        buckets[integration]=$((buckets[integration]+1))
    elif echo "$f" | rg -qi 'e2e|end2end' >/dev/null 2>&1; then
        echo "E2E        : $f"
        buckets[e2e]=$((buckets[e2e]+1))
    elif echo "$f" | rg -qi 'bench|benchmark|_bench' >/dev/null 2>&1; then
        echo "BENCH      : $f"
        buckets[bench]=$((buckets[bench]+1))
    elif echo "$f" | rg -qi 'test_|unit|_unit' >/dev/null 2>&1 || echo "$f" | rg -qi '^cmd/|internal/.*' >/dev/null 2>&1; then
        echo "UNIT       : $f"
        buckets[unit]=$((buckets[unit]+1))
    else
        echo "OTHER      : $f"
        buckets[other]=$((buckets[other]+1))
    fi
done < <(find_test_files)

printf "\nSummary:\n"
for k in "${!buckets[@]}"; do
    printf "  %10s: %d\n" "$k" "${buckets[$k]}"
done

exit 0
