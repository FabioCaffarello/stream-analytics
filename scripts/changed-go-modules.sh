#!/usr/bin/env bash
set -euo pipefail

mode="--auto"
case "${1:-}" in
  ""|--auto|--staged|--worktree)
    if [[ -n "${1:-}" ]]; then
      mode="$1"
    fi
    ;;
  *)
    echo "usage: $0 [--auto|--staged|--worktree]" >&2
    exit 1
    ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

changed_files="$(./scripts/list-changed-files.sh "$mode" || true)"
if [[ -z "$changed_files" ]]; then
  exit 0
fi

modules="$(./scripts/list-modules.sh)"
if [[ -z "$modules" ]]; then
  exit 0
fi

if printf '%s\n' "$changed_files" | rg -q '^(go\.work|go\.work\.sum|\.golangci\.yml)$'; then
  printf '%s\n' "$modules"
  exit 0
fi

while IFS= read -r module; do
  [[ -z "$module" ]] && continue
  module_prefix="${module#./}/"
  if printf '%s\n' "$changed_files" | rg -q "^${module_prefix}.*(\.go|go\.mod|go\.sum)$"; then
    printf '%s\n' "$module"
  fi
done <<< "$modules"
