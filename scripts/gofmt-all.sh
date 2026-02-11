#!/usr/bin/env bash
set -euo pipefail

mode="${1:-write}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

status=0
if [[ -n "${MODULE:-}" ]]; then
  module="${MODULE}"
  while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    if [[ "$mode" == "check" ]]; then
      if [[ -n "$(gofmt -l "$file")" ]]; then
        echo "$file"
        status=1
      fi
    else
      gofmt -w "$file"
    fi
  done < <(find "$module" -type f -name '*.go' -not -path '*/vendor/*' | sort)
else
  while IFS= read -r module; do
    [[ -z "$module" ]] && continue
    while IFS= read -r file; do
      [[ -z "$file" ]] && continue
      if [[ "$mode" == "check" ]]; then
        if [[ -n "$(gofmt -l "$file")" ]]; then
          echo "$file"
          status=1
        fi
      else
        gofmt -w "$file"
      fi
    done < <(find "$module" -type f -name '*.go' -not -path '*/vendor/*' | sort)
  done < <("$ROOT_DIR/scripts/list-modules.sh")
fi

if [[ "$mode" == "check" && $status -ne 0 ]]; then
  echo "Files above are not gofmt-formatted. Run: make fmt" >&2
  exit 1
fi
