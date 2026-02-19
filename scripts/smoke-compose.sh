#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TIMEOUT_SECONDS="${SMOKE_TIMEOUT_SECONDS:-60}"
COMPOSE_FILE="deploy/compose/docker-compose.yml"
COMPOSE=(docker compose -f "$COMPOSE_FILE" --profile core)

services=(consumer processor server store)
ports=(8081 8082 8080 8083)
ready=(0 0 0 0)
urls=()

resolve_service_url() {
  local service="$1"
  local port="$2"
  local mapping
  local host
  local mapped_port

  mapping="$("${COMPOSE[@]}" port "$service" "$port" 2>/dev/null | head -n1 || true)"
  if [[ -z "$mapping" ]]; then
    echo "http://127.0.0.1:${port}"
    return 0
  fi

  if [[ "$mapping" =~ ^\[::\]:([0-9]+)$ ]]; then
    host="127.0.0.1"
    mapped_port="${BASH_REMATCH[1]}"
    echo "http://${host}:${mapped_port}"
    return 0
  fi

  mapped_port="${mapping##*:}"
  host="${mapping%:*}"
  if [[ "$host" == "$mapping" ]]; then
    host="127.0.0.1"
    mapped_port="$port"
  fi
  if [[ "$host" == "0.0.0.0" || "$host" == "::" ]]; then
    host="127.0.0.1"
  fi
  echo "http://${host}:${mapped_port}"
}

for i in "${!services[@]}"; do
  urls+=("$(resolve_service_url "${services[$i]}" "${ports[$i]}")")
done

echo "smoke-compose: waiting up to ${TIMEOUT_SECONDS}s for /readyz"
for i in "${!services[@]}"; do
  echo "smoke-compose: ${services[$i]} -> ${urls[$i]}/readyz"
done

for _ in $(seq 1 "$TIMEOUT_SECONDS"); do
  pending=0
  for i in "${!services[@]}"; do
    if [[ "${ready[$i]}" -eq 1 ]]; then
      continue
    fi
    if curl -fsS --max-time 2 "${urls[$i]}/readyz" >/dev/null 2>&1; then
      ready[$i]=1
      echo "smoke-compose: ready ${services[$i]} (${urls[$i]}/readyz)"
    else
      pending=1
    fi
  done
  if [[ "$pending" -eq 0 ]]; then
    echo "smoke-compose: all endpoints are ready"
    exit 0
  fi
  sleep 1
done

echo "smoke-compose: timeout after ${TIMEOUT_SECONDS}s"
failed=0
for i in "${!services[@]}"; do
  if [[ "${ready[$i]}" -ne 1 ]]; then
    failed=1
    echo "smoke-compose: FAIL ${services[$i]} endpoint ${urls[$i]}/readyz did not become ready"
    "${COMPOSE[@]}" ps "${services[$i]}" || true
  fi
done

if [[ "$failed" -ne 0 ]]; then
  exit 1
fi
