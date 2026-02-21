#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"

TF_ENVS=("local" "staging" "prod")

echo "Running terraform validation for envs: ${TF_ENVS[*]}"

for env in "${TF_ENVS[@]}"; do
  env_dir="$repo_root/deploy/infra/terraform/envs/$env"
  if [ ! -d "$env_dir" ]; then
    echo "Warning: env dir not found: $env_dir (skipping)"
    continue
  fi

  echo "==> [$env] terraform fmt -check"
  (cd "$env_dir" && terraform fmt -check -diff) || { echo "terraform fmt failed for $env"; exit 1; }

  echo "==> [$env] terraform init (no backend)"
  (cd "$env_dir" && terraform init -backend=false -input=false >/dev/null)

  echo "==> [$env] terraform validate"
  (cd "$env_dir" && terraform validate) || { echo "terraform validate failed for $env"; exit 1; }

  if command -v tflint >/dev/null 2>&1; then
    echo "==> [$env] tflint"
    (cd "$env_dir" && tflint) || { echo "tflint failed for $env"; exit 1; }
  else
    echo "tflint not found; skipping tflint (install locally or in CI)"
  fi

  if command -v tfsec >/dev/null 2>&1; then
    echo "==> [$env] tfsec"
    (cd "$env_dir" && tfsec .) || { echo "tfsec found issues in $env"; exit 1; }
  else
    echo "tfsec not found; skipping tfsec (install locally or in CI)"
  fi
done

echo "All terraform checks passed."
