#!/usr/bin/env bash
set -euo pipefail

mode="check"
if [[ "${1:-}" == "--fix-hints" ]]; then
  mode="fix-hints"
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
packs_dir="${repo_root}/.context/docs/feature-packs"
errors=0

report_issue() {
  local file="$1"
  local line="$2"
  local link="$3"
  local message="$4"
  local fix="$5"

  if [[ "$mode" == "check" ]]; then
    echo "docs-check: FEATURE-PACK: ${file}:${line} ${message} -> '${link}'"
    echo "  fix: ${fix}"
    errors=$((errors + 1))
  else
    echo "[ ] FEATURE-PACK: ${file}:${line} ${message} -> '${link}'"
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

check_pack() {
  local file="$1"
  local rel_file="${file#"${repo_root}/"}"
  local line_no=0
  local in_code_block=0
  local link_count=0

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
        report_issue "$rel_file" "$line_no" "$raw_link" "non-relative link" "replace with a relative link to docs/** from this pack."
        rest="${rest#*"$full_match"}"
        continue
      fi

      if [[ "$link" == /* ]]; then
        report_issue "$rel_file" "$line_no" "$raw_link" "absolute link" "replace with a relative link to docs/** from this pack."
        rest="${rest#*"$full_match"}"
        continue
      fi

      local target
      target="$(normalize_link_target "$link")"
      if [[ -z "$target" ]]; then
        rest="${rest#*"$full_match"}"
        continue
      fi

      local base_dir
      base_dir="$(dirname "$file")"
      local resolved="${base_dir}/${target}"
      if [[ ! -e "$resolved" ]]; then
        report_issue "$rel_file" "$line_no" "$raw_link" "broken relative link" "point to an existing file under docs/**."
        rest="${rest#*"$full_match"}"
        continue
      fi

      local canonical
      canonical="$(cd "$(dirname "$resolved")" && pwd -P)/$(basename "$resolved")"
      if [[ "$canonical" != "${repo_root}/docs/"* ]]; then
        report_issue "$rel_file" "$line_no" "$raw_link" "link outside docs/**" "use only relative links targeting docs/** files."
      fi

      link_count=$((link_count + 1))
      rest="${rest#*"$full_match"}"
    done
  done < "$file"

  if (( link_count == 0 )); then
    report_issue "$rel_file" "1" "(none)" "pack has no docs/** links" "add relative links to authoritative docs/** sources."
  fi
}

if [[ ! -d "$packs_dir" ]]; then
  report_issue ".context/docs/feature-packs" "1" "(missing)" "feature pack directory not found" "create .context/docs/feature-packs with markdown packs."
fi

while IFS= read -r pack; do
  check_pack "$pack"
done < <(find "$packs_dir" -type f -name '*.md' | sort)

subject_guard_status=0
if [[ -x "${repo_root}/scripts/ci/check-pack-subjects-vs-event-bus.sh" ]]; then
  if [[ "$mode" == "check" ]]; then
    if ! "${repo_root}/scripts/ci/check-pack-subjects-vs-event-bus.sh"; then
      subject_guard_status=1
    fi
  else
    "${repo_root}/scripts/ci/check-pack-subjects-vs-event-bus.sh" --fix-hints || true
  fi
else
  report_issue "scripts/ci/check-pack-subjects-vs-event-bus.sh" "1" "(missing)" "pack subject checker not executable" "chmod +x scripts/ci/check-pack-subjects-vs-event-bus.sh."
fi

if [[ "$mode" == "check" ]]; then
  if (( subject_guard_status != 0 )); then
    errors=$((errors + 1))
  fi
  if (( errors > 0 )); then
    echo "docs-check: feature-pack link guard failed with ${errors} issue(s)."
    exit 1
  fi
  echo "docs-check: feature-pack link guard passed."
else
  echo "docs-fix: feature-pack links checklist emitted."
fi
