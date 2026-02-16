#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

required_files=(
  "docs/observability/slo.md"
  "docs/observability/runbooks/ingest.md"
  "docs/observability/runbooks/guardian.md"
  "docs/observability/runbooks/bus.md"
  "docs/observability/runbooks/websocket.md"
  "docs/observability/runbooks/vpvr-overload-runbook.md"
  "docs/observability/runbooks/consumer-stall.md"
  "deploy/observability/prometheus/recording.rules.yml"
  "deploy/observability/prometheus/alerts.rules.yml"
  "deploy/observability/prometheus/tests.yml"
  "deploy/observability/grafana/dashboards/overview.json"
  "deploy/observability/grafana/dashboards/ingest.json"
  "deploy/observability/grafana/dashboards/vpvr.json"
  "deploy/observability/grafana/dashboards/delivery.json"
)

for f in "${required_files[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "operability-gates: missing required file: $f"
    exit 1
  fi
done

if ! command -v promtool >/dev/null 2>&1; then
  echo "operability-gates: promtool not found (required)"
  exit 1
fi

promtool check rules deploy/observability/prometheus/recording.rules.yml
promtool check rules deploy/observability/prometheus/alerts.rules.yml
promtool test rules deploy/observability/prometheus/tests.yml

jq -e . deploy/observability/grafana/dashboards/overview.json >/dev/null
jq -e . deploy/observability/grafana/dashboards/ingest.json >/dev/null
jq -e . deploy/observability/grafana/dashboards/vpvr.json >/dev/null
jq -e . deploy/observability/grafana/dashboards/store.json >/dev/null
jq -e . deploy/observability/grafana/dashboards/delivery.json >/dev/null

# Forbidden labels must not be present as raw label keys.
forbidden='instrument|symbol|subject|window_id|seq|request_id'

if rg -n "\"(${forbidden})\"" internal/shared/metrics/metrics.go >/dev/null 2>&1; then
  echo "operability-gates: forbidden raw metric label found in internal/shared/metrics/metrics.go"
  rg -n "\"(${forbidden})\"" internal/shared/metrics/metrics.go
  exit 1
fi

if rg -n "[\{,]\s*(${forbidden})\s*=" deploy/observability/prometheus deploy/observability/grafana >/dev/null 2>&1; then
  echo "operability-gates: forbidden label selector found in observability rules/dashboards"
  rg -n "[\{,]\s*(${forbidden})\s*=" deploy/observability/prometheus deploy/observability/grafana
  exit 1
fi

echo "operability-gates: all checks passed"
