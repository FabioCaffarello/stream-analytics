#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="${SERVER_URL:-http://127.0.0.1:8080}"
CONSUMER_URL="${DATAPLANE_CONSUMER_URL:-http://127.0.0.1:8088}"
VALIDATOR_URL="${VALIDATOR_URL:-http://127.0.0.1:8089}"

echo "dataplane-smoke: upserting runtime binding"
curl -fsS -X POST "${SERVER_URL}/api/v1/dataplane/bindings" \
  -H 'Content-Type: application/json' \
  -d '{"name":"orders","kafka_topic":"mr.orders"}' >/dev/null

echo "dataplane-smoke: upserting validation config"
curl -fsS -X POST "${SERVER_URL}/api/v1/dataplane/configs" \
  -H 'Content-Type: application/json' \
  -d '{"name":"orders-required","binding":"orders","version":"v1","rules":[{"field":"account_id","kind":"required"},{"field":"account_id","kind":"not_empty"},{"field":"status","kind":"equals","expected":"OPEN"}]}' >/dev/null

echo "dataplane-smoke: activating validation config"
curl -fsS -X POST "${SERVER_URL}/api/v1/dataplane/configs/activate" \
  -H 'Content-Type: application/json' \
  -d '{"binding":"orders","version":"v1"}' >/dev/null

echo "dataplane-smoke: waiting for dataplane consumer readiness"
for _ in $(seq 1 30); do
  if curl -fsS "${CONSUMER_URL}/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS "${CONSUMER_URL}/readyz" >/dev/null

echo "dataplane-smoke: waiting for validator readiness"
for _ in $(seq 1 30); do
  if curl -fsS "${VALIDATOR_URL}/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS "${VALIDATOR_URL}/readyz" >/dev/null

emit() {
  local scenario="$1"
  curl -fsS -X POST "${SERVER_URL}/api/v1/dataplane/emulator/emit" \
    -H 'Content-Type: application/json' \
    -d "{\"binding\":\"orders\",\"scenario\":\"${scenario}\"}"
}

extract_message_id() {
  sed -n 's/.*"message_id":"\([^"]*\)".*/\1/p'
}

wait_result() {
  local message_id="$1"
  local expected_status="$2"
  local response=""
  for _ in $(seq 1 30); do
    response="$(curl -fsS "${SERVER_URL}/api/v1/dataplane/results?message_id=${message_id}" || true)"
    if printf '%s' "$response" | grep -q "\"status\":\"${expected_status}\""; then
      printf '%s\n' "$response"
      return 0
    fi
    sleep 1
  done
  echo "dataplane-smoke: timed out waiting for ${message_id} status=${expected_status}" >&2
  printf '%s\n' "$response" >&2
  return 1
}

valid_resp="$(emit valid)"
valid_id="$(printf '%s' "$valid_resp" | extract_message_id)"
valid_corr="$(printf '%s' "$valid_resp" | sed -n 's/.*"correlation_id":"\([^"]*\)".*/\1/p')"
if [[ -z "$valid_id" ]]; then
  echo "dataplane-smoke: could not extract valid message_id" >&2
  printf '%s\n' "$valid_resp" >&2
  exit 1
fi
echo "dataplane-smoke: emitted valid message ${valid_id}"
wait_result "$valid_id" "passed" >/dev/null
if [[ -n "$valid_corr" ]]; then
  curl -fsS "${SERVER_URL}/api/v1/dataplane/results?correlation_id=${valid_corr}&limit=1" >/dev/null
fi

invalid_resp="$(emit missing_required)"
invalid_id="$(printf '%s' "$invalid_resp" | extract_message_id)"
if [[ -z "$invalid_id" ]]; then
  echo "dataplane-smoke: could not extract invalid message_id" >&2
  printf '%s\n' "$invalid_resp" >&2
  exit 1
fi
echo "dataplane-smoke: emitted invalid message ${invalid_id}"
wait_result "$invalid_id" "failed" >/dev/null

echo "dataplane-smoke: PASS"
