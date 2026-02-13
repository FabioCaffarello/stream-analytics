#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

step=0
run_step() {
  local target="$1"
  step=$((step + 1))
  local started
  started=$(date +%s)
  echo "ci-local: [${step}] start ${target}"
  if ! make "$target"; then
    local ended
    ended=$(date +%s)
    echo "ci-local: [${step}] FAIL ${target} ($((ended-started))s)" >&2
    exit 1
  fi
  local ended
  ended=$(date +%s)
  echo "ci-local: [${step}] ok ${target} ($((ended-started))s)"
}

run_step quick
run_step docs-check-full
run_step invariants-check
run_step test-unit
run_step test-integration
run_step test-replay-golden
run_step proto-check

echo "ci-local: completed all steps"
