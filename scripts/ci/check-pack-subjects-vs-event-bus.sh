#!/usr/bin/env bash
set -euo pipefail
exec "$(dirname "$0")/../check-pack-subjects-vs-event-bus.sh" "$@"
