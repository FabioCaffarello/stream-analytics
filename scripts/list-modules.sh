#!/usr/bin/env bash
set -euo pipefail

# Prints module directories from go.work, one per line.
if command -v jq >/dev/null 2>&1; then
  go work edit -json | jq -r '.Use[].DiskPath'
else
  awk '
    BEGIN { in_use = 0 }
    /^[[:space:]]*use[[:space:]]*\(/ { in_use = 1; next }
    in_use && /^[[:space:]]*\)/ { in_use = 0; next }
    in_use {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", $0)
      if ($0 != "") print $0
      next
    }
    /^[[:space:]]*use[[:space:]]+\.[^[:space:]]*/ {
      line = $0
      sub(/^[[:space:]]*use[[:space:]]+/, "", line)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
      if (line != "") print line
    }
  ' go.work
fi | sort -u
