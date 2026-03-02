#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_DIR="$(cd "${CLIENT_DIR}/.." && pwd)"

DURATION_SEC=300
SAMPLE_SEC=5
SOAK_LOG_MS=1000
BUILD_FIRST=0
SOAK_MULTI=1
RESTART_SERVER_EVERY_SEC=0
RESTART_SERVER_MAX_COUNT=0
MAX_RSS_GROWTH_KB=0
MAX_THREADS_GROWTH=0
OUT_DIR=""
FORWARD_ARGS=()

usage() {
  cat <<'EOF'
Usage: client/scripts/soak-native.sh [options] [-- extra app args]

Options:
  --duration-sec N   Soak duration in seconds (default: 300)
  --sample-sec N     Process sampling interval in seconds (default: 5)
  --log-ms N         App soak log interval in milliseconds (default: 1000)
  --build            Run 'make -C client build-native' before starting
  --no-multi         Do not enable --soak-multi (default is enabled)
  --restart-server-every-sec N  Fault injection: restart compose server every N seconds (default: off)
  --restart-server-max-count N   Max server restarts for fault injection (default: unlimited when enabled)
  --max-rss-growth-kb N          Fail if RSS growth (last-first) exceeds N KB (default: disabled)
  --max-threads-growth N         Fail if thread growth (last-first) exceeds N (default: disabled)
  --out-dir PATH     Output directory for logs/csv (default: client/build/soak-<ts>)
  -h, --help         Show this help

Examples:
  client/scripts/soak-native.sh --duration-sec 900 --sample-sec 2 --build
  client/scripts/soak-native.sh --duration-sec 300 --restart-server-every-sec 30 --restart-server-max-count 3
  client/scripts/soak-native.sh --duration-sec 600 -- --ws-url=ws://127.0.0.1:8080/ws --api-key=prod_key_1
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --duration-sec)
      DURATION_SEC="${2:?missing value}"
      shift 2
      ;;
    --sample-sec)
      SAMPLE_SEC="${2:?missing value}"
      shift 2
      ;;
    --log-ms)
      SOAK_LOG_MS="${2:?missing value}"
      shift 2
      ;;
    --build)
      BUILD_FIRST=1
      shift
      ;;
    --no-multi)
      SOAK_MULTI=0
      shift
      ;;
    --restart-server-every-sec)
      RESTART_SERVER_EVERY_SEC="${2:?missing value}"
      shift 2
      ;;
    --restart-server-max-count)
      RESTART_SERVER_MAX_COUNT="${2:?missing value}"
      shift 2
      ;;
    --max-rss-growth-kb)
      MAX_RSS_GROWTH_KB="${2:?missing value}"
      shift 2
      ;;
    --max-threads-growth)
      MAX_THREADS_GROWTH="${2:?missing value}"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="${2:?missing value}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      FORWARD_ARGS+=("$@")
      break
      ;;
    *)
      FORWARD_ARGS+=("$1")
      shift
      ;;
  esac
done

if [[ -z "${OUT_DIR}" ]]; then
  TS="$(date +"%Y%m%d-%H%M%S")"
  OUT_DIR="${CLIENT_DIR}/build/soak-${TS}"
fi
mkdir -p "${OUT_DIR}"

APP_BIN="${CLIENT_DIR}/build/app"
APP_LOG="${OUT_DIR}/app.log"
FAULT_LOG="${OUT_DIR}/fault.log"
PROC_CSV="${OUT_DIR}/process_samples.csv"
META_TXT="${OUT_DIR}/meta.txt"
SUMMARY_TXT="${OUT_DIR}/summary.txt"

if [[ "${BUILD_FIRST}" == "1" ]]; then
  make -C "${CLIENT_DIR}" build-native
fi

if [[ ! -x "${APP_BIN}" ]]; then
  echo "error: native app not found at ${APP_BIN} (run --build or make -C client build-native)" >&2
  exit 1
fi

if ! [[ "${DURATION_SEC}" =~ ^[0-9]+$ ]] || ! [[ "${SAMPLE_SEC}" =~ ^[0-9]+$ ]] || ! [[ "${SOAK_LOG_MS}" =~ ^[0-9]+$ ]] || \
   ! [[ "${RESTART_SERVER_EVERY_SEC}" =~ ^[0-9]+$ ]] || ! [[ "${RESTART_SERVER_MAX_COUNT}" =~ ^[0-9]+$ ]] || \
   ! [[ "${MAX_RSS_GROWTH_KB}" =~ ^[0-9]+$ ]] || ! [[ "${MAX_THREADS_GROWTH}" =~ ^[0-9]+$ ]]; then
  echo "error: duration/sample/log values must be positive integers" >&2
  exit 1
fi
if [[ "${DURATION_SEC}" -le 0 || "${SAMPLE_SEC}" -le 0 || "${SOAK_LOG_MS}" -le 0 ]]; then
  echo "error: duration/sample/log values must be > 0" >&2
  exit 1
fi

app_args=("--soak-seconds=${DURATION_SEC}" "--soak-log-ms=${SOAK_LOG_MS}")
if [[ "${SOAK_MULTI}" == "1" ]]; then
  app_args+=("--soak-multi")
fi
if [[ ${#FORWARD_ARGS[@]} -gt 0 ]]; then
  app_args+=("${FORWARD_ARGS[@]}")
fi

{
  echo "started_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo "repo_dir=${REPO_DIR}"
  echo "client_dir=${CLIENT_DIR}"
  echo "app_bin=${APP_BIN}"
  echo "duration_sec=${DURATION_SEC}"
  echo "sample_sec=${SAMPLE_SEC}"
  echo "soak_log_ms=${SOAK_LOG_MS}"
  echo "soak_multi=${SOAK_MULTI}"
  echo "restart_server_every_sec=${RESTART_SERVER_EVERY_SEC}"
  echo "restart_server_max_count=${RESTART_SERVER_MAX_COUNT}"
  echo "max_rss_growth_kb=${MAX_RSS_GROWTH_KB}"
  echo "max_threads_growth=${MAX_THREADS_GROWTH}"
  printf 'app_args='
  printf '%q ' "${app_args[@]}"
  printf '\n'
} > "${META_TXT}"

echo "ts_epoch_s,rss_kb,threads" > "${PROC_CSV}"

get_rss_kb() {
  local pid="$1"
  ps -o rss= -p "${pid}" 2>/dev/null | awk '{print $1+0}' | head -n1
}

get_thread_count() {
  local pid="$1"
  local t
  t="$(ps -o thcount= -p "${pid}" 2>/dev/null | awk '{print $1+0}' | head -n1 || true)"
  if [[ -n "${t}" ]]; then
    echo "${t}"
    return
  fi
  t="$(ps -o nlwp= -p "${pid}" 2>/dev/null | awk '{print $1+0}' | head -n1 || true)"
  if [[ -n "${t}" ]]; then
    echo "${t}"
    return
  fi
  if ps -M -p "${pid}" >/dev/null 2>&1; then
    ps -M -p "${pid}" 2>/dev/null | tail -n +2 | wc -l | awk '{print $1+0}'
    return
  fi
  echo "0"
}

app_pid=""
fault_pid=""
cleanup() {
  if [[ -n "${fault_pid}" ]] && kill -0 "${fault_pid}" >/dev/null 2>&1; then
    kill "${fault_pid}" >/dev/null 2>&1 || true
    wait "${fault_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${app_pid}" ]] && kill -0 "${app_pid}" >/dev/null 2>&1; then
    kill "${app_pid}" >/dev/null 2>&1 || true
    wait "${app_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

echo "Starting soak app..."
(
  cd "${CLIENT_DIR}"
  "${APP_BIN}" "${app_args[@]}"
) > >(tee "${APP_LOG}") 2>&1 &
app_pid=$!

echo "app_pid=${app_pid}" | tee -a "${META_TXT}"
echo "logs: ${APP_LOG}"
echo "fault logs: ${FAULT_LOG}"
echo "process samples: ${PROC_CSV}"

if [[ "${RESTART_SERVER_EVERY_SEC}" -gt 0 ]]; then
  echo "Fault injection enabled: restart server every ${RESTART_SERVER_EVERY_SEC}s (max_count=${RESTART_SERVER_MAX_COUNT:-0})" | tee -a "${META_TXT}"
  (
    set -euo pipefail
    count=0
    while kill -0 "${app_pid}" >/dev/null 2>&1; do
      sleep "${RESTART_SERVER_EVERY_SEC}"
      if ! kill -0 "${app_pid}" >/dev/null 2>&1; then
        break
      fi
      if [[ "${RESTART_SERVER_MAX_COUNT}" -gt 0 && "${count}" -ge "${RESTART_SERVER_MAX_COUNT}" ]]; then
        break
      fi
      count=$((count + 1))
      ts="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
      echo "[fault] ${ts} restarting compose server (count=${count})" | tee -a "${FAULT_LOG}"
      docker compose -f "${REPO_DIR}/deploy/compose/docker-compose.yml" --env-file "${REPO_DIR}/deploy/envs/local.env" restart server >> "${APP_LOG}" 2>&1 || true
    done
  ) &
  fault_pid=$!
fi

while kill -0 "${app_pid}" >/dev/null 2>&1; do
  now_s="$(date +%s)"
  rss_kb="$(get_rss_kb "${app_pid}")"
  threads="$(get_thread_count "${app_pid}")"
  rss_kb="${rss_kb:-0}"
  threads="${threads:-0}"
  # Process samplers may briefly report 0 RSS during startup; skip invalid baselines.
  if [[ "${rss_kb}" -le 0 ]]; then
    sleep "${SAMPLE_SEC}"
    continue
  fi
  echo "${now_s},${rss_kb},${threads}" >> "${PROC_CSV}"
  sleep "${SAMPLE_SEC}"
done

wait "${app_pid}" || true
if [[ -n "${fault_pid}" ]]; then
  wait "${fault_pid}" >/dev/null 2>&1 || true
fi
trap - EXIT INT TERM

RESULT="PASS"
FAIL_REASONS=()

csv_stats="$(
  awk -F, '
    NR==2 {first_rss=$2; first_thr=$3}
    NR>1 {
      samples++
      last_rss=$2; last_thr=$3
      if (min_rss=="" || $2<min_rss) min_rss=$2
      if (max_rss=="" || $2>max_rss) max_rss=$2
      if (min_thr=="" || $3<min_thr) min_thr=$3
      if (max_thr=="" || $3>max_thr) max_thr=$3
    }
    END {
      if (samples==0) {
        print "samples=0"
        exit
      }
      print "samples=" samples
      print "rss_first=" first_rss
      print "rss_last=" last_rss
      print "rss_min=" min_rss
      print "rss_max=" max_rss
      print "rss_growth=" (last_rss-first_rss)
      print "thr_first=" first_thr
      print "thr_last=" last_thr
      print "thr_min=" min_thr
      print "thr_max=" max_thr
      print "thr_growth=" (last_thr-first_thr)
    }
  ' "${PROC_CSV}"
)"

while IFS='=' read -r k v; do
  [[ -z "${k}" ]] && continue
  case "${k}" in
    samples) SAMPLES="${v}" ;;
    rss_first) RSS_FIRST="${v}" ;;
    rss_last) RSS_LAST="${v}" ;;
    rss_min) RSS_MIN="${v}" ;;
    rss_max) RSS_MAX="${v}" ;;
    rss_growth) RSS_GROWTH="${v}" ;;
    thr_first) THR_FIRST="${v}" ;;
    thr_last) THR_LAST="${v}" ;;
    thr_min) THR_MIN="${v}" ;;
    thr_max) THR_MAX="${v}" ;;
    thr_growth) THR_GROWTH="${v}" ;;
  esac
done <<< "${csv_stats}"

SOAK_DONE_COUNT="$(grep -c '^\[soak\] done duration_s=' "${APP_LOG}" || true)"
RECONNECT_LINES="$(grep -c '^\[marketdata\] Reconnecting in ' "${APP_LOG}" || true)"
RECONNECT_FAILS="$(grep -c '^\[marketdata\] Reconnect failed ' "${APP_LOG}" || true)"
RECONNECT_SUCCESSES="$(grep -c '^\[marketdata\] Reconnected to ' "${APP_LOG}" || true)"
FAULT_RESTARTS="$(grep -c '^\[fault\].*restarting compose server' "${FAULT_LOG}" 2>/dev/null || true)"
ACK_COUNT="$(grep -E -c '^\[marketdata\] Ack: op=subscribe |^\[md-lifecycle\] ack_recv op=subscribe ' "${APP_LOG}" || true)"
LAST_SOAK_LINE="$(grep '^\[soak\] t_ms=' "${APP_LOG}" | tail -n 1 || true)"

if [[ "${SOAK_DONE_COUNT}" -eq 0 ]]; then
  RESULT="FAIL"
  FAIL_REASONS+=("missing_soak_done_marker")
fi
if [[ "${SAMPLES:-0}" -eq 0 ]]; then
  RESULT="FAIL"
  FAIL_REASONS+=("no_process_samples")
fi
if [[ "${FAULT_RESTARTS}" -gt 0 && "${RECONNECT_SUCCESSES}" -eq 0 ]]; then
  RESULT="FAIL"
  FAIL_REASONS+=("no_reconnect_success_after_faults")
fi
if [[ "${MAX_RSS_GROWTH_KB}" -gt 0 && "${RSS_GROWTH:-0}" -gt "${MAX_RSS_GROWTH_KB}" ]]; then
  RESULT="FAIL"
  FAIL_REASONS+=("rss_growth>${MAX_RSS_GROWTH_KB}kb")
fi
if [[ "${MAX_THREADS_GROWTH}" -gt 0 && "${THR_GROWTH:-0}" -gt "${MAX_THREADS_GROWTH}" ]]; then
  RESULT="FAIL"
  FAIL_REASONS+=("thread_growth>${MAX_THREADS_GROWTH}")
fi

{
  echo "result=${RESULT}"
  echo "finished_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo "out_dir=${OUT_DIR}"
  echo "samples=${SAMPLES:-0}"
  echo "rss_first_kb=${RSS_FIRST:-0}"
  echo "rss_last_kb=${RSS_LAST:-0}"
  echo "rss_min_kb=${RSS_MIN:-0}"
  echo "rss_max_kb=${RSS_MAX:-0}"
  echo "rss_growth_kb=${RSS_GROWTH:-0}"
  echo "threads_first=${THR_FIRST:-0}"
  echo "threads_last=${THR_LAST:-0}"
  echo "threads_min=${THR_MIN:-0}"
  echo "threads_max=${THR_MAX:-0}"
  echo "threads_growth=${THR_GROWTH:-0}"
  echo "fault_restarts=${FAULT_RESTARTS}"
  echo "reconnect_attempt_logs=${RECONNECT_LINES}"
  echo "reconnect_fail_logs=${RECONNECT_FAILS}"
  echo "reconnect_success_logs=${RECONNECT_SUCCESSES}"
  echo "subscribe_ack_logs=${ACK_COUNT}"
  printf 'last_soak_line=%s\n' "${LAST_SOAK_LINE}"
  if [[ "${#FAIL_REASONS[@]}" -gt 0 ]]; then
    printf 'fail_reasons='
    printf '%s,' "${FAIL_REASONS[@]}"
    printf '\n'
  fi
} > "${SUMMARY_TXT}"

echo "Soak finished."
echo "Output dir: ${OUT_DIR}"
echo "Summary: ${SUMMARY_TXT}"

if [[ "${RESULT}" != "PASS" ]]; then
  echo "Soak result: ${RESULT} (${FAIL_REASONS[*]})" >&2
  exit 1
fi
echo "Soak result: PASS"
