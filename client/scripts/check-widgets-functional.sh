#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

LOG_FILE="${CLIENT_DIR}/build/check-widgets-offline.log"
SOAK_SECONDS="${SOAK_SECONDS:-4}"
SOAK_LOG_MS="${SOAK_LOG_MS:-1000}"
MIN_WIDGET_COUNT="${MIN_WIDGET_COUNT:-1}"
RUNTIME_ERR_RE='panic|fatal error|runtime trap|assertion failed|sig(segv|abrt|bus)|out of bounds|index out of range'

echo "check-widgets: building native client"
make -C "${CLIENT_DIR}" build-native >/dev/null

echo "check-widgets: running offline soak probe"
"${CLIENT_DIR}/build/app" \
  --offline \
  --soak-seconds="${SOAK_SECONDS}" \
  --soak-log-ms="${SOAK_LOG_MS}" >"${LOG_FILE}" 2>&1

if grep -Eiq "${RUNTIME_ERR_RE}" "${LOG_FILE}"; then
  echo "check-widgets: FAIL - runtime error markers found in log"
  echo "log: ${LOG_FILE}"
  exit 1
fi

last_line="$(grep -E "\\[soak\\].*w\\[t=" "${LOG_FILE}" | tail -n 1 || true)"
if [[ -z "${last_line}" ]]; then
  echo "check-widgets: FAIL - did not find widget coverage line in soak output"
  echo "log: ${LOG_FILE}"
  exit 1
fi

if [[ "${last_line}" =~ w\[t=([0-9]+)\ ob=([0-9]+)/([0-9]+)\ st=([0-9]+)\ hm=([0-9]+)\ vp=([0-9]+)\ c=([0-9]+)\] ]]; then
  t="${BASH_REMATCH[1]}"
  ob_a="${BASH_REMATCH[2]}"
  ob_b="${BASH_REMATCH[3]}"
  st="${BASH_REMATCH[4]}"
  hm="${BASH_REMATCH[5]}"
  vp="${BASH_REMATCH[6]}"
  c="${BASH_REMATCH[7]}"
else
  echo "check-widgets: FAIL - could not parse widget coverage from: ${last_line}"
  echo "log: ${LOG_FILE}"
  exit 1
fi

missing=0
for kv in "t:${t}" "ob_a:${ob_a}" "ob_b:${ob_b}" "st:${st}" "hm:${hm}" "vp:${vp}" "c:${c}"; do
  k="${kv%%:*}"
  v="${kv##*:}"
  if [[ -z "${v}" || "${v}" -lt "${MIN_WIDGET_COUNT}" ]]; then
    echo "check-widgets: FAIL - ${k} < ${MIN_WIDGET_COUNT} in: ${last_line}"
    missing=1
  fi
done

if [[ "${missing}" -ne 0 ]]; then
  echo "log: ${LOG_FILE}"
  exit 1
fi

echo "check-widgets: PASS"
echo "coverage: ${last_line}"
