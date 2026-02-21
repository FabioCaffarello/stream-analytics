#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

ROOT="$repo_root/deploy/gitops"
echo "Scanning $ROOT for plaintext Kubernetes Secret manifests..."

if [ ! -d "$ROOT" ]; then
  echo "deploy/gitops not found at $ROOT"
  exit 0
fi

fail=0

# For each YAML file, check for 'kind: Secret' and absence of 'sops:' and absence of ExternalSecret
while IFS= read -r -d '' file; do
  # skip binary-like files
  if ! grep -Iq . "$file"; then
    continue
  fi

  if grep -Eiq '^\s*kind:\s*Secret\s*$' "$file"; then
    # if file contains sops: it's encrypted => skip
    if grep -Eiq '^\s*sops:\s*$' "$file"; then
      continue
    fi
    # if file contains ExternalSecret or external-secrets apiVersion, skip
    if grep -Eiq 'ExternalSecret|external-secrets|external-secrets.io' "$file"; then
      continue
    fi

    echo "Plaintext Secret detected: $file"
    fail=1
  fi
done < <(find "$ROOT" -type f -name '*.yaml' -o -name '*.yml' -print0)

if [ "$fail" -ne 0 ]; then
  echo "Plaintext Kubernetes Secret manifests are not allowed. Use SOPS or ExternalSecret."
  exit 1
fi

echo "No plaintext Secret manifests found."
