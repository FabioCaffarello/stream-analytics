#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

echo "Running k8s validation for deploy/gitops overlays..."

ROOT="$repo_root/deploy/gitops"

if [ ! -d "$ROOT" ]; then
  echo "deploy/gitops not found at $ROOT"
  exit 0
fi

# require tools
if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required (for 'kubectl kustomize'). Install it and retry."
  exit 1
fi
if ! command -v kubeconform >/dev/null 2>&1; then
  echo "kubeconform is required. Install it and retry."
  exit 1
fi

# uses_external_generators returns 0 if the kustomization uses external
# generator plugins (e.g. KSOPS) that cannot be built locally.
uses_external_generators() {
  local dir="$1"
  local kf
  for kf in "$dir/kustomization.yaml" "$dir/kustomization.yml"; do
    if [ -f "$kf" ]; then
      # Look for a generators: key whose referenced files declare a non-builtin kind.
      # Fast heuristic: check if any referenced generator file contains "kind: ksops".
      if grep -q '^generators:' "$kf" 2>/dev/null; then
        local gen_files
        gen_files=$(grep -E '^\s+- ' "$kf" | sed 's/^[[:space:]]*- //')
        for gf in $gen_files; do
          local resolved="$dir/$gf"
          if [ -f "$resolved" ] && grep -qi 'kind:.*ksops' "$resolved" 2>/dev/null; then
            return 0
          fi
        done
      fi
    fi
  done
  return 1
}

# Find kustomize directories
kustomize_files=()
tmpfile="$(mktemp)"
find "$ROOT" -type f \( -name 'kustomization.yaml' -o -name 'kustomization.yml' \) -print0 > "$tmpfile" || true
if [ ! -s "$tmpfile" ]; then
  rm -f "$tmpfile"
  echo "No kustomization files found under $ROOT"
  exit 0
fi
while IFS= read -r -d '' file; do
  kustomize_files+=("$file")
done < "$tmpfile"
rm -f "$tmpfile"

set +e
failures=0
skipped=0
for kf in "${kustomize_files[@]}"; do
  dir="$(dirname "$kf")"

  # Skip directories that use external generator plugins (e.g. KSOPS).
  # These require the plugin binary and can only be built in-cluster (ArgoCD).
  if uses_external_generators "$dir"; then
    echo "==> Skipping (external generator plugin): $dir"
    skipped=$((skipped+1))
    continue
  fi

  echo "==> Building kustomize: $dir"
  if command -v kustomize >/dev/null 2>&1; then
    if ! kustomize build "$dir" > /tmp/kustomize-output.yaml 2>&1; then
      echo "kustomize build failed for $dir"
      failures=$((failures+1))
      continue
    fi
  else
    if ! kubectl kustomize "$dir" > /tmp/kustomize-output.yaml 2>&1; then
      echo "kustomize build failed for $dir"
      failures=$((failures+1))
      continue
    fi
  fi

  echo "==> Validating with kubeconform: $dir"
  if ! kubeconform -strict -ignore-missing-schemas -summary -schema-location default -exit-on-error < /tmp/kustomize-output.yaml; then
    echo "kubeconform validation failed for $dir"
    failures=$((failures+1))
    continue
  fi
done
set -e

if [ "$skipped" -ne 0 ]; then
  echo "Skipped $skipped directories (external generator plugins)."
fi

if [ "$failures" -ne 0 ]; then
  echo "k8s validation failed ($failures failures)"
  exit 1
fi

echo "All k8s validations passed."
