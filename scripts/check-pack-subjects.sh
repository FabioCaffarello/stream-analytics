#!/usr/bin/env bash
set -euo pipefail

mode="check"
if [[ "${1:-}" == "--fix-hints" ]]; then
  mode="fix-hints"
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
packs_dir="${repo_root}/.context/docs/feature-packs"
contract_file="${repo_root}/docs/contracts/event-bus.md"
errors=0

contract_prefixes_file="$(mktemp)"
trap 'rm -f "$contract_prefixes_file"' EXIT

report_issue() {
  local file="$1"
  local line="$2"
  local subject="$3"
  local message="$4"
  local fix="$5"

  if [[ "$mode" == "check" ]]; then
    echo "docs-check: PACK-SUBJECT: ${file}:${line} ${message} -> '${subject}'"
    echo "  fix: ${fix}"
    errors=$((errors + 1))
  else
    echo "[ ] PACK-SUBJECT: ${file}:${line} ${message} -> '${subject}'"
    echo "    sugestao: ${fix}"
  fi
}

normalize_subject() {
  local s="$1"
  s="${s%\`}"
  s="${s#\`}"
  s="${s%%#*}"
  s="${s%%\?*}"
  s="${s//\{venue_lower\}/*}"
  s="${s//\{venue\}/*}"
  s="${s//\{instrument\}/*}"
  s="${s//\{symbol\}/*}"
  s="${s//\{timeframe\}/*}"
  s="${s//\{event\}/*}"
  s="${s//\{version\}/*}"
  s="${s//\{instrument_alnum_upper\}/*}"
  s="${s//<stream_type>/*}"
  s="${s//<venue>/*}"
  s="${s//<symbol>/*}"
  s="${s//<timeframe>/*}"
  s="${s%%[.,;:)]}"
  printf '%s' "$s"
}

subject_prefix() {
  local s="$1"
  local versioned_re='^([a-z0-9_.]+\.v[0-9]+)(\.|$)'
  local root_wc_re='^([a-z0-9_]+)\.>$'

  if [[ "$s" =~ $versioned_re ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  if [[ "$s" =~ $root_wc_re ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
    return 0
  fi
  return 1
}

line_has_required_marker() {
  local line="$1"
  [[ "$line" == *"(planned, not in event-bus.md matrix)"* || "$line" == *"(runtime, not yet in event-bus.md matrix)"* ]]
}

load_contract_prefixes() {
  local raw
  while IFS= read -r raw; do
    local subject
    local prefix

    subject="$(normalize_subject "$raw")"
    [[ -n "$subject" ]] || continue
    [[ "$subject" == */* ]] && continue
    [[ "$subject" == *.* ]] || continue

    if prefix="$(subject_prefix "$subject")"; then
      printf '%s\n' "$prefix" >> "$contract_prefixes_file"
    fi
  done < <(
    {
      rg -o '(marketdata|insights|aggregation|quarantine)\.[A-Za-z0-9_.{}*><-]+' "$contract_file"
      rg -o '\`[^`]+\`' "$contract_file" | sed -E 's/^`//; s/`$//'
    } | sort -u
  )

  sort -u -o "$contract_prefixes_file" "$contract_prefixes_file"
}

validate_candidate() {
  local file="$1"
  local line_no="$2"
  local line_text="$3"
  local raw_subject="$4"

  local subject
  local prefix
  subject="$(normalize_subject "$raw_subject")"

  [[ -n "$subject" ]] || return 0
  [[ "$subject" == */* ]] && return 0
  [[ "$subject" == *.* ]] || return 0

  if ! prefix="$(subject_prefix "$subject")"; then
    return 0
  fi

  if rg -Fxq "$prefix" "$contract_prefixes_file"; then
    return 0
  fi

  if line_has_required_marker "$line_text"; then
    return 0
  fi

  report_issue \
    "$file" \
    "$line_no" \
    "$subject" \
    "subject prefix not found in docs/contracts/event-bus.md and missing explicit drift marker" \
    "align docs/contracts/event-bus.md or add '(planned, not in event-bus.md matrix)' / '(runtime, not yet in event-bus.md matrix)'."
}

check_pack_subjects() {
  local pack="$1"
  local rel_pack="${pack#"${repo_root}/"}"
  local line_no=0

  while IFS= read -r line || [[ -n "$line" ]]; do
    line_no=$((line_no + 1))

    local token
    while IFS= read -r token; do
      [[ -n "$token" ]] || continue
      validate_candidate "$rel_pack" "$line_no" "$line" "$token"
    done < <(
      {
        printf '%s\n' "$line" | rg -o '\`[^`]+\`' | sed -E 's/^`//; s/`$//'
        printf '%s\n' "$line" | rg -o '(marketdata|insights|aggregation|quarantine)\.[A-Za-z0-9_.{}*><-]+'
      } | sort -u
    )
  done < "$pack"
}

if [[ ! -f "$contract_file" ]]; then
  report_issue "docs/contracts/event-bus.md" "1" "(missing)" "contract file not found" "restore docs/contracts/event-bus.md."
fi

if [[ ! -d "$packs_dir" ]]; then
  report_issue ".context/docs/feature-packs" "1" "(missing)" "feature pack directory not found" "create .context/docs/feature-packs."
fi

if [[ -f "$contract_file" ]]; then
  load_contract_prefixes
fi

if [[ -d "$packs_dir" ]]; then
  while IFS= read -r pack; do
    check_pack_subjects "$pack"
  done < <(find "$packs_dir" -type f -name '*.md' | sort)
fi

if [[ "$mode" == "check" ]]; then
  if (( errors > 0 )); then
    echo "docs-check: pack subject guard failed with ${errors} issue(s)."
    exit 1
  fi
  echo "docs-check: pack subject guard passed."
else
  echo "docs-fix: pack subject checklist emitted."
fi
