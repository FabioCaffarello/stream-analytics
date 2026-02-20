#!/usr/bin/env bash
set -euo pipefail
exec "$(dirname "$0")/../list-tests-by-category.sh" "$@"
