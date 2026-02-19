#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
usage: $0 [--staged|--all]

--staged  scan staged files plus key config/docs files
--all     scan all tracked files in repository
USAGE
}

mode="${1:-}"
if [[ "$mode" != "--staged" && "$mode" != "--all" ]]; then
  usage >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

tmp_files="$(mktemp)"
tmp_hits="$(mktemp)"
trap 'rm -f "$tmp_files" "$tmp_hits"' EXIT

if [[ "$mode" == "--staged" ]]; then
  {
    git diff --cached --name-only --diff-filter=ACMR || true
    printf '%s\n' Makefile
    printf '%s\n' deploy/compose/docker-compose.yml
    find docs/operations -maxdepth 1 -type f -name '*.md' -print 2>/dev/null || true
  } | sed '/^$/d' | sort -u > "$tmp_files"
else
  git ls-files > "$tmp_files"
fi

rg -v '^scripts/legacy-scan\.sh$' "$tmp_files" > "${tmp_files}.filtered"
mv "${tmp_files}.filtered" "$tmp_files"

if [[ ! -s "$tmp_files" ]]; then
  echo "legacy-scan: no files selected; pass"
  exit 0
fi

scan_files=()
while IFS= read -r file; do
  if [[ -e "$file" ]]; then
    scan_files+=("$file")
  fi
done < "$tmp_files"

if [[ "${#scan_files[@]}" -eq 0 ]]; then
  echo "legacy-scan: no existing files selected; pass"
  exit 0
fi

check_pattern() {
  local label="$1"
  local regex="$2"
  if rg -n --color=never -e "$regex" -- "${scan_files[@]}" > "$tmp_hits"; then
    echo "legacy-scan: forbidden pattern matched [$label]"
    cat "$tmp_hits"
    return 1
  fi
  return 0
}

status=0
check_pattern "processor-r1" 'processor-r1' || status=1
check_pattern "scale-processor-r1" '(?i)(scal(e|ing)[^\n]{0,80}processor-r1|processor-r1[^\n]{0,80}scal(e|ing))' || status=1
check_pattern "replicas-1-or-2" 'PROCESSOR_REPLICAS must be 1 or 2' || status=1
check_pattern "legacy-compose-generator" '(?i)(compose[^\n]{0,40}generat(or|ion)|generat(or|ion)[^\n]{0,40}compose)' || status=1

today_utc="$(date -u +%F)"
if rg -n --color=never -e 'DEPRECATED:' -- "${scan_files[@]}" > "$tmp_hits"; then
  while IFS= read -r hit; do
    file="${hit%%:*}"
    rest="${hit#*:}"
    line="${rest%%:*}"
    content="${rest#*:}"
    remove_by="$(printf '%s\n' "$content" | sed -nE 's/.*remove-by=([0-9]{4}-[0-9]{2}-[0-9]{2}).*/\1/p')"
    if [[ -z "$remove_by" ]]; then
      echo "legacy-scan: DEPRECATED missing remove-by=YYYY-MM-DD at ${file}:${line}"
      status=1
      continue
    fi
    if [[ "$today_utc" > "$remove_by" ]]; then
      echo "legacy-scan: expired DEPRECATED marker at ${file}:${line} (remove-by=${remove_by}, today=${today_utc})"
      status=1
    fi
  done < "$tmp_hits"
fi

if [[ "$status" -ne 0 ]]; then
  exit 1
fi

echo "legacy-scan: pass (${mode#--})"
