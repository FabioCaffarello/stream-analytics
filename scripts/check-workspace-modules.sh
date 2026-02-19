#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

modules=()
while IFS= read -r line; do
  [ -n "$line" ] && modules+=("$line")
done < <(./scripts/list-modules.sh)
if [ "${#modules[@]}" -eq 0 ]; then
  echo "workspace-check: no modules found from go.work" >&2
  exit 1
fi

status=0
for mod in "${modules[@]}"; do
  if [ ! -d "$mod" ]; then
    echo "workspace-check: missing module directory: $mod" >&2
    status=1
    continue
  fi
  if ! (cd "$mod" && go list ./... >/dev/null 2>&1); then
    echo "workspace-check: go list failed for module: $mod" >&2
    status=1
  fi
done

if [ "$status" -ne 0 ]; then
  echo "workspace-check: failed" >&2
  exit 1
fi

echo "workspace-check: all go.work modules resolved (${#modules[@]} modules)."
