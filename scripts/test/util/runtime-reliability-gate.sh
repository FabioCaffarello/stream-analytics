#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT_DIR"

echo "running runtime reliability gate (wrapper) — delegating to existing logic"
exec ./scripts/runtime-reliability-gate.sh "$@"
