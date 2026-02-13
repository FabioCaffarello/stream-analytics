#!/usr/bin/env bash
set -euo pipefail

repo_root="${1:-$(pwd)}"
cd "$repo_root"

targets=(
  "internal/core"
  "internal/actors"
  "internal/interfaces"
)

pattern='"(google\.golang\.org/protobuf|github\.com/golang/protobuf)(/[^"]*)?"'
violations=()

scan_with_rg() {
  local target="$1"
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    violations+=("$line")
  done < <(rg -n --no-heading --glob '*.go' --regexp "$pattern" "$target" || true)
}

scan_with_grep() {
  local target="$1"
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    violations+=("$line")
  done < <(grep -R -n -E --include='*.go' "$pattern" "$target" || true)
}

for target in "${targets[@]}"; do
  if [ ! -d "$target" ]; then
    continue
  fi
  if command -v rg >/dev/null 2>&1; then
    scan_with_rg "$target"
  else
    scan_with_grep "$target"
  fi
done

if [ "${#violations[@]}" -gt 0 ]; then
  echo "protobuf import violates Domain Isolation; move to internal/shared/contracts boundary"
  printf '%s\n' "${violations[@]}"
  exit 1
fi

echo "invariants-check: domain isolation protobuf-free guard passed"
