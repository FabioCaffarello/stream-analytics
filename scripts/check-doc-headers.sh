#!/usr/bin/env bash
set -euo pipefail

mode="check"
if [[ "${1:-}" == "--fix-hints" ]]; then
  mode="fix-hints"
fi

errors=0

fail_or_hint() {
  local kind="$1"
  local file="$2"
  local message="$3"
  local fix="$4"

  if [[ "$mode" == "check" ]]; then
    echo "docs-check: ${kind}: ${file} ${message}"
    echo "  fix: ${fix}"
    errors=$((errors + 1))
  else
    echo "[ ] ${kind}: ${file} ${message}"
    echo "    sugestao: ${fix}"
  fi
}

has_heading() {
  local file="$1"
  local pattern="$2"
  rg -qi "$pattern" "$file"
}

has_status_metadata() {
  local file="$1"
  rg -qi '^\*\*Status:\*\*[[:space:]]+\S+' "$file"
}

check_adr_file() {
  local file="$1"

  if ! has_status_metadata "$file"; then
    fail_or_hint "ADR" "$file" "missing Status metadata" "add a line like '**Status:** Accepted' near the top of the document."
  fi

  if ! has_heading "$file" '^##+[[:space:]]+(Changelog|Amendment)([[:space:]]|$)'; then
    fail_or_hint "ADR" "$file" "missing Changelog section" "add '## Changelog' (or '## Amendment') with dated entries."
  fi

  if ! has_heading "$file" '^##+[[:space:]]+Evidence([[:space:]]|$)'; then
    fail_or_hint "ADR" "$file" "missing Evidence section" "add '## Evidence' and include code/test anchors (file paths or commands)."
  fi
}

check_rfc_file() {
  local file="$1"

  if ! has_heading "$file" '^##+[[:space:]]+(Acceptance|Criterios de aceite|Crit[eé]rios de aceite)([[:space:]]|$)'; then
    fail_or_hint "RFC" "$file" "missing Acceptance section" "add '## Acceptance' (or equivalent 'Critérios de aceite') with explicit acceptance criteria."
  fi

  if ! has_heading "$file" '^##+[[:space:]]+(Test Plan|Plano de Teste|Plano Executavel|Plano Execut[áa]vel)([[:space:]]|$)'; then
    fail_or_hint "RFC" "$file" "missing Test Plan section" "add '## Test Plan' (or equivalent) with runnable validation commands."
  fi
}

shopt -s nullglob

for file in docs/adrs/ADR-*.md; do
  check_adr_file "$file"
done

for file in docs/rfcs/RFC-*.md; do
  check_rfc_file "$file"
done

if [[ "$mode" == "check" ]]; then
  if (( errors > 0 )); then
    echo "docs-check: doc headers check failed with ${errors} issue(s)."
    exit 1
  fi
  echo "docs-check: doc headers guard passed."
else
  echo "docs-fix: doc headers checklist emitted."
fi
