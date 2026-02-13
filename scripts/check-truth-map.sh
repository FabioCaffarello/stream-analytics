#!/usr/bin/env bash
set -euo pipefail

mode="check"
if [[ "${1:-}" == "--fix-hints" ]]; then
  mode="fix-hints"
fi

truth_map_file="docs/architecture/TRUTH-MAP.md"
execution_sequence_file="docs/rfcs/EXECUTION-SEQUENCE.md"
errors=0

report_issue() {
  local check="$1"
  local message="$2"
  local fix="$3"

  if [[ "$mode" == "check" ]]; then
    echo "docs-check: TRUTH-MAP: ${check} ${message}"
    echo "  fix: ${fix}"
    errors=$((errors + 1))
  else
    echo "[ ] TRUTH-MAP: ${check} ${message}"
    echo "    sugestao: ${fix}"
  fi
}

extract_doc_paths_from_code_spans() {
  local file="$1"
  rg -o '`docs/[A-Za-z0-9._/\-]+\.md(:[0-9]+)?`' "$file" \
    | sed -E 's/^`//; s/`$//' \
    | sed -E 's/:[0-9]+$//' \
    | sort -u
}

check_file_exists() {
  local path="$1"
  [[ -f "$path" ]]
}

check_truth_map_references_exist() {
  local doc
  while IFS= read -r doc; do
    [[ -n "$doc" ]] || continue
    if ! check_file_exists "$doc"; then
      report_issue "reference" "missing document '${doc}' referenced in ${truth_map_file}" "update ${truth_map_file} to point to an existing doc path."
    fi
  done < <(extract_doc_paths_from_code_spans "$truth_map_file")
}

check_truth_map_inventory_coverage() {
  local doc
  for doc in docs/adrs/ADR-*.md docs/rfcs/RFC-*.md; do
    [[ -f "$doc" ]] || continue
    if ! rg -Fq "\`${doc}\`" "$truth_map_file"; then
      report_issue "inventory" "document '${doc}' is not mapped in ${truth_map_file}" "add '${doc}' to the inventory section in ${truth_map_file}."
    fi
  done
}

check_execution_sequence_orphans() {
  local anchor
  while IFS= read -r anchor; do
    [[ -n "$anchor" ]] || continue
    local path="${anchor%%:*}"
    if [[ ! -e "$path" ]]; then
      report_issue "execution-sequence" "orphan reference '${anchor}' in ${execution_sequence_file}" "replace '${anchor}' with an existing path anchor."
    fi
  done < <(rg -o '`(Makefile|scripts/[A-Za-z0-9._/\-]+|docs/[A-Za-z0-9._/\-]+\.md|internal/[A-Za-z0-9._/\-]+|cmd/[A-Za-z0-9._/\-]+|\.context/[A-Za-z0-9._/\-]+)(:[^`]+)?`' "$execution_sequence_file" \
    | sed -E 's/^`//; s/`$//' \
    | sort -u)
}

check_docs_check_gate_in_execution_sequence() {
  if ! rg -Fq 'make docs-check' "$execution_sequence_file"; then
    report_issue "execution-sequence" "'make docs-check' gate missing from ${execution_sequence_file}" "add 'make docs-check' as the first mandatory governance gate."
  fi
}

if [[ ! -f "$truth_map_file" ]]; then
  report_issue "file" "missing ${truth_map_file}" "restore ${truth_map_file} and re-run docs-check."
fi

if [[ ! -f "$execution_sequence_file" ]]; then
  report_issue "file" "missing ${execution_sequence_file}" "restore ${execution_sequence_file} and re-run docs-check."
fi

if [[ -f "$truth_map_file" ]]; then
  check_truth_map_references_exist
  check_truth_map_inventory_coverage
fi

if [[ -f "$execution_sequence_file" ]]; then
  check_execution_sequence_orphans
  check_docs_check_gate_in_execution_sequence
fi

if [[ "$mode" == "check" ]]; then
  if (( errors > 0 )); then
    echo "docs-check: truth-map consistency check failed with ${errors} issue(s)."
    exit 1
  fi
  echo "docs-check: TRUTH-MAP and EXECUTION-SEQUENCE guard passed."
else
  echo "docs-fix: truth-map checklist emitted."
fi
