#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo "usage: $0 <command...>" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -n "${MODULE:-}" ]]; then
  echo ">>> ${MODULE}: $*"
  (
    cd "${MODULE}"
    "$@"
  )
  exit 0
fi

modules=()
while IFS= read -r module; do
  modules+=("$module")
done < <("$ROOT_DIR/scripts/list-modules.sh")
if [[ ${#modules[@]} -eq 0 ]]; then
  echo "no modules resolved from go.work" >&2
  exit 1
fi

for module in "${modules[@]}"; do
  [[ -z "$module" ]] && continue
  echo ">>> ${module}: $*"
  (
    cd "$module"
    "$@"
  )
done
