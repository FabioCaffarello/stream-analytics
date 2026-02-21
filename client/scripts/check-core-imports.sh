#!/usr/bin/env bash
# check-core-imports.sh — Architecture gate: core must never import platform or deps.
#
# Rule: src/core/** may only import:
#   - "core:*"    (Odin stdlib)
#   - "mr:*"      (sibling core packages via collection)
#   - "base:*"    (Odin base)
#
# Forbidden:
#   - Any path containing "platform", "deps", "vendor" (local vendor wrappers)
#   - Relative imports to ../platform or ../deps

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="${SCRIPT_DIR}/../src/core"

if [ ! -d "$CORE_DIR" ]; then
    echo "check-core-imports: src/core not found; skipping"
    exit 0
fi

status=0

# Match Odin import statements: import "foo" or import ident "foo"
# Forbidden patterns in the import path.
forbidden='platform/|/deps/|"deps:|vendor/'

while IFS= read -r file; do
    # Extract import paths from .odin files.
    # Odin imports look like:  import "path"  or  import ident "path"
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        # Extract the quoted path from the import line.
        path=$(echo "$line" | grep -oE '"[^"]+"' | tr -d '"')
        [ -z "$path" ] && continue

        if echo "$path" | grep -qE "$forbidden"; then
            echo "VIOLATION: $file"
            echo "  import: $path"
            echo "  core packages must not import platform, deps, or vendor"
            status=1
        fi
    done < <(grep -nE '^\s*import\s' "$file" 2>/dev/null || true)
done < <(find "$CORE_DIR" -name '*.odin' -type f)

if [ "$status" -eq 0 ]; then
    echo "check-core-imports: OK — no forbidden imports in src/core"
else
    echo ""
    echo "check-core-imports: FAILED — fix violations above"
    exit 1
fi
