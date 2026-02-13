#!/usr/bin/env bash
set -euo pipefail

mode="check"
if [[ "${1:-}" == "--fix-hints" ]]; then
  mode="fix-hints"
fi

errors=0

report_issue() {
  local file="$1"
  local line="$2"
  local link="$3"
  local message="$4"
  local fix="$5"

  if [[ "$mode" == "check" ]]; then
    echo "docs-check: LINKS: ${file}:${line} ${message} -> '${link}'"
    echo "  fix: ${fix}"
    errors=$((errors + 1))
  else
    echo "[ ] LINKS: ${file}:${line} ${message} -> '${link}'"
    echo "    sugestao: ${fix}"
  fi
}

is_external_link() {
  local link="$1"
  [[ "$link" =~ ^[a-zA-Z][a-zA-Z0-9+.-]*: ]]
}

normalize_link_target() {
  local link="$1"
  link="${link%%#*}"
  link="${link%%\?*}"
  printf '%s' "$link"
}

check_markdown_file() {
  local file="$1"
  local line_no=0
  local in_code_block=0

  while IFS= read -r line || [[ -n "$line" ]]; do
    line_no=$((line_no + 1))

    if [[ "$line" =~ ^[[:space:]]*\`\`\` ]]; then
      if (( in_code_block == 0 )); then
        in_code_block=1
      else
        in_code_block=0
      fi
      continue
    fi

    if (( in_code_block == 1 )); then
      continue
    fi

    local rest="$line"
    local link_pattern='\[[^][]+\]\(([^)]+)\)'
    while [[ "$rest" =~ $link_pattern ]]; do
      local full_match="${BASH_REMATCH[0]}"
      local raw_link="${BASH_REMATCH[1]}"
      local link="${raw_link//[[:space:]]/}"

      if [[ -z "$link" || "$link" == \#* ]]; then
        rest="${rest#*"$full_match"}"
        continue
      fi

      if is_external_link "$link"; then
        rest="${rest#*"$full_match"}"
        continue
      fi

      local target
      target="$(normalize_link_target "$link")"
      if [[ -z "$target" ]]; then
        rest="${rest#*"$full_match"}"
        continue
      fi

      if [[ "$target" == /* ]]; then
        report_issue "$file" "$line_no" "$raw_link" "non-relative internal link" "convert to a relative path from this file (e.g. '../architecture/TRUTH-MAP.md')."
        rest="${rest#*"$full_match"}"
        continue
      fi

      local base_dir
      base_dir="$(dirname "$file")"
      local resolved_path="${base_dir}/${target}"
      if [[ ! -e "$resolved_path" ]]; then
        report_issue "$file" "$line_no" "$raw_link" "broken internal relative link" "update the target path relative to '${base_dir}' or remove the stale link."
      fi

      rest="${rest#*"$full_match"}"
    done
  done < "$file"
}

while IFS= read -r file; do
  check_markdown_file "$file"
done < <(find docs -type f -name '*.md' | sort)

if [[ "$mode" == "check" ]]; then
  if (( errors > 0 )); then
    echo "docs-check: internal links check failed with ${errors} issue(s)."
    exit 1
  fi
  echo "docs-check: internal links guard passed."
else
  echo "docs-fix: internal links checklist emitted."
fi
