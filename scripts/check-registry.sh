#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
registry="${repo_root}/docs/contracts/subject-registry.yaml"
validation_src="${repo_root}/internal/adapters/jetstream/subject_validation.go"
errors=0

fail() {
  echo "registry-check: $1"
  errors=$((errors + 1))
}

# 1. Registry file must exist.
if [[ ! -f "$registry" ]]; then
  fail "docs/contracts/subject-registry.yaml not found"
  echo "registry-check: failed with ${errors} issue(s)."
  exit 1
fi

# 2. Extract allowed roots from runtime code.
runtime_roots_file="$(mktemp)"
trap 'rm -f "$runtime_roots_file"' EXIT

if [[ -f "$validation_src" ]]; then
  if command -v rg >/dev/null 2>&1; then
    rg -o '"([a-z_]+)":\s*\{\}' --replace '$1' "$validation_src" | sort -u > "$runtime_roots_file"
  else
    grep -oE '"[a-z_]+":\s*\{\}' "$validation_src" | sed -E 's/"([^"]+)".*/\1/' | sort -u > "$runtime_roots_file"
  fi
else
  fail "subject_validation.go not found at ${validation_src}"
fi

# 3. Parse subjects from registry (line-by-line, predictable structure).
valid_statuses="stable draft planned"
valid_bcs="marketdata aggregation insights delivery storage"
required_fields="id pattern root owner_bc producer_bc schema_authority_bc consumer_bcs status"

current_id=""
found_fields=""
line_no=0
in_subjects=false

check_subject() {
  local sid="$1"
  local fields="$2"
  [[ -n "$sid" ]] || return 0

  for field in $required_fields; do
    if ! echo " $fields " | grep -q " ${field} "; then
      fail "${sid}: missing required field '${field}'"
    fi
  done
}

while IFS= read -r line || [[ -n "$line" ]]; do
  line_no=$((line_no + 1))

  # Detect subjects section.
  if [[ "$line" == "subjects:" ]]; then
    in_subjects=true
    continue
  fi

  # Detect end of subjects section (non-indented key after subjects).
  if $in_subjects && [[ "$line" =~ ^[a-z] ]]; then
    check_subject "$current_id" "$found_fields"
    in_subjects=false
    current_id=""
    found_fields=""
    continue
  fi

  $in_subjects || continue

  # New subject entry.
  if [[ "$line" =~ ^[[:space:]]*-[[:space:]]*id:[[:space:]]*(.+) ]]; then
    check_subject "$current_id" "$found_fields"
    current_id="${BASH_REMATCH[1]}"
    current_id="${current_id%% *}"
    found_fields="id"
    continue
  fi

  # Field within current subject.
  if [[ -n "$current_id" && "$line" =~ ^[[:space:]]+([a-z_]+): ]]; then
    field_name="${BASH_REMATCH[1]}"
    found_fields="$found_fields $field_name"

    # Extract field value (trim whitespace).
    field_value="${line#*: }"
    field_value="${field_value## }"
    field_value="${field_value%% }"

    case "$field_name" in
      root)
        if [[ -s "$runtime_roots_file" ]] && ! grep -Fxq "$field_value" "$runtime_roots_file"; then
          fail "${current_id}: root '${field_value}' not in allowedSubjectRoots (${validation_src##*/})"
        fi
        ;;
      status)
        if ! echo " $valid_statuses " | grep -q " ${field_value} "; then
          fail "${current_id}: invalid status '${field_value}' (expected: ${valid_statuses})"
        fi
        ;;
      producer_bc|owner_bc|schema_authority_bc)
        if ! echo " $valid_bcs " | grep -q " ${field_value} "; then
          fail "${current_id}: invalid ${field_name} '${field_value}' (expected: ${valid_bcs})"
        fi
        ;;
    esac
  fi
done < "$registry"

# Check last subject if file ends inside subjects section.
check_subject "$current_id" "$found_fields"

if (( errors > 0 )); then
  echo "registry-check: failed with ${errors} issue(s)."
  exit 1
fi

echo "registry-check: subject registry validation passed."
