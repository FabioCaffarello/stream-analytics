#!/usr/bin/env bash
set -euo pipefail

mode="auto"
case "${1:-}" in
  ""|--auto)
    mode="auto"
    ;;
  --staged)
    mode="staged"
    ;;
  --worktree)
    mode="worktree"
    ;;
  *)
    echo "usage: $0 [--auto|--staged|--worktree]" >&2
    exit 1
    ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

list_staged() {
  git diff --cached --name-only --diff-filter=ACMR
}

list_worktree() {
  {
    git diff --name-only --diff-filter=ACMR
    git ls-files --others --exclude-standard
  } | sed '/^$/d' | sort -u
}

case "$mode" in
  staged)
    list_staged | sed '/^$/d' | sort -u
    ;;
  worktree)
    list_worktree
    ;;
  auto)
    staged="$(list_staged | sed '/^$/d' | sort -u || true)"
    if [[ -n "$staged" ]]; then
      printf '%s\n' "$staged"
    else
      list_worktree
    fi
    ;;
esac
