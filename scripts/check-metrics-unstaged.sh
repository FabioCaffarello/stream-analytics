#!/usr/bin/env bash
set -euo pipefail

if ! git rev-parse --git-dir >/dev/null 2>&1; then
	exit 0
fi

if git diff --quiet -- internal/shared/metrics; then
	exit 0
fi

echo "pre-commit blocked: unstaged changes detected under internal/shared/metrics/**." >&2
echo "Stage those files before committing to avoid false gate failures from pre-commit stashing." >&2
exit 1
