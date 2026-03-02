#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

LOG_FILE="${CLIENT_DIR}/build/check-widgets-online.log"
SOAK_SECONDS="${SOAK_SECONDS:-10}"
SOAK_LOG_MS="${SOAK_LOG_MS:-1000}"
SOAK_MULTI="${SOAK_MULTI:-0}"
MIN_WIDGET_COUNT="${MIN_WIDGET_COUNT:-1}"
MIN_TRADES_COUNT="${MIN_TRADES_COUNT:-${MIN_WIDGET_COUNT}}"
MIN_ORDERBOOK_ASK_COUNT="${MIN_ORDERBOOK_ASK_COUNT:-${MIN_WIDGET_COUNT}}"
MIN_ORDERBOOK_BID_COUNT="${MIN_ORDERBOOK_BID_COUNT:-${MIN_WIDGET_COUNT}}"
MIN_STATS_COUNT="${MIN_STATS_COUNT:-${MIN_WIDGET_COUNT}}"
MIN_HEATMAP_COUNT="${MIN_HEATMAP_COUNT:-${MIN_WIDGET_COUNT}}"
MIN_VPVR_COUNT="${MIN_VPVR_COUNT:-${MIN_WIDGET_COUNT}}"
MIN_CANDLES_COUNT="${MIN_CANDLES_COUNT:-4}"
RUNTIME_ERR_RE='panic|fatal error|runtime trap|assertion failed|sig(segv|abrt|bus)|out of bounds|index out of range'

echo "check-widgets-online: building native client"
make -C "${CLIENT_DIR}" build-native >/dev/null

echo "check-widgets-online: running online soak probe"
app_args=(
  "--soak-seconds=${SOAK_SECONDS}"
  "--soak-log-ms=${SOAK_LOG_MS}"
)
if [[ "${SOAK_MULTI}" == "1" ]]; then
  app_args+=("--soak-multi")
fi
"${CLIENT_DIR}/build/app" "${app_args[@]}" >"${LOG_FILE}" 2>&1

if grep -Eiq "${RUNTIME_ERR_RE}" "${LOG_FILE}"; then
  echo "check-widgets-online: FAIL - runtime error markers found in log"
  echo "log: ${LOG_FILE}"
  exit 1
fi

if ! grep -q "conn=Connected" "${LOG_FILE}"; then
  echo "check-widgets-online: FAIL - no connected state observed"
  echo "log: ${LOG_FILE}"
  exit 1
fi

last_line="$(grep -E "\\[soak\\].*w\\[t=" "${LOG_FILE}" | tail -n 1 || true)"
if [[ -z "${last_line}" ]]; then
  echo "check-widgets-online: FAIL - did not find widget coverage line in soak output"
  echo "log: ${LOG_FILE}"
  exit 1
fi

if [[ "${last_line}" != *"health=OK"* ]]; then
  echo "check-widgets-online: FAIL - last soak coverage line is not health=OK"
  echo "line: ${last_line}"
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
  echo "check-widgets-online: FAIL - could not parse widget coverage from: ${last_line}"
  echo "log: ${LOG_FILE}"
  exit 1
fi

missing=0
if [[ -z "${t}" || "${t}" -lt "${MIN_TRADES_COUNT}" ]]; then
  echo "check-widgets-online: FAIL - trades(${t}) < MIN_TRADES_COUNT(${MIN_TRADES_COUNT}) in: ${last_line}"
  missing=1
fi
if [[ -z "${ob_a}" || "${ob_a}" -lt "${MIN_ORDERBOOK_ASK_COUNT}" ]]; then
  echo "check-widgets-online: FAIL - orderbook_ask(${ob_a}) < MIN_ORDERBOOK_ASK_COUNT(${MIN_ORDERBOOK_ASK_COUNT}) in: ${last_line}"
  missing=1
fi
if [[ -z "${ob_b}" || "${ob_b}" -lt "${MIN_ORDERBOOK_BID_COUNT}" ]]; then
  echo "check-widgets-online: FAIL - orderbook_bid(${ob_b}) < MIN_ORDERBOOK_BID_COUNT(${MIN_ORDERBOOK_BID_COUNT}) in: ${last_line}"
  missing=1
fi
if [[ -z "${st}" || "${st}" -lt "${MIN_STATS_COUNT}" ]]; then
  echo "check-widgets-online: FAIL - stats(${st}) < MIN_STATS_COUNT(${MIN_STATS_COUNT}) in: ${last_line}"
  missing=1
fi
if [[ -z "${hm}" || "${hm}" -lt "${MIN_HEATMAP_COUNT}" ]]; then
  echo "check-widgets-online: FAIL - heatmap(${hm}) < MIN_HEATMAP_COUNT(${MIN_HEATMAP_COUNT}) in: ${last_line}"
  missing=1
fi
if [[ -z "${vp}" || "${vp}" -lt "${MIN_VPVR_COUNT}" ]]; then
  echo "check-widgets-online: FAIL - vpvr(${vp}) < MIN_VPVR_COUNT(${MIN_VPVR_COUNT}) in: ${last_line}"
  missing=1
fi
if [[ -z "${c}" || "${c}" -lt "${MIN_CANDLES_COUNT}" ]]; then
  echo "check-widgets-online: FAIL - candles(${c}) < MIN_CANDLES_COUNT(${MIN_CANDLES_COUNT}) in: ${last_line}"
  missing=1
fi

if [[ "${missing}" -ne 0 ]]; then
  echo "log: ${LOG_FILE}"
  exit 1
fi

echo "check-widgets-online: PASS"
echo "coverage: ${last_line}"
