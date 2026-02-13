#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

has_changes=1

changed_files() {
  git diff --name-only --diff-filter=ACMR HEAD -- proto buf.yaml buf.gen.yaml 2>/dev/null || true
  git diff --name-only --diff-filter=ACMR --cached -- proto buf.yaml buf.gen.yaml 2>/dev/null || true
  git ls-files --others --exclude-standard -- proto buf.yaml buf.gen.yaml 2>/dev/null || true
}

while IFS= read -r f; do
  if [[ -n "$f" ]]; then
    has_changes=0
    break
  fi
done < <(changed_files | sort -u)

# Exit 0 when proto generation is needed; exit 1 when no proto/config changes exist.
exit "$has_changes"
