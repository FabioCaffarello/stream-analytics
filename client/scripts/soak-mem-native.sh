#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
SOAK_SCRIPT="${SCRIPT_DIR}/soak-native.sh"

DURATION_SEC=600
SAMPLE_SEC=5
SOAK_LOG_MS=1000
RSS_BUDGET_MB=64
SUSTAINED_BUDGET_MB=24
SUSTAINED_WINDOW_SEC=180
BUILD_FIRST=0
OUT_DIR=""
FORWARD_ARGS=()

usage() {
  cat <<'USAGE'
Usage: client/scripts/soak-mem-native.sh [options] [-- extra app args]

Runs native soak with explicit memory contract checks:
- global RSS growth budget over full run
- sustained growth budget in trailing window

Options:
  --duration-sec N           Soak duration in seconds (default: 600)
  --sample-sec N             RSS/thread sample interval in seconds (default: 5)
  --log-ms N                 App soak log interval in milliseconds (default: 1000)
  --rss-budget-mb N          Max total RSS growth in MB (default: 64)
  --sustained-budget-mb N    Max RSS growth in trailing window in MB (default: 24)
  --sustained-window-sec N   Trailing window size in seconds (default: 180)
  --build                    Run native build before soak
  --out-dir PATH             Output directory (default: client/build/soak-mem-<ts>)
  -h, --help                 Show help

Examples:
  client/scripts/soak-mem-native.sh --build
  client/scripts/soak-mem-native.sh --duration-sec 900 --rss-budget-mb 96 --sustained-budget-mb 32
USAGE
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
    --rss-budget-mb)
      RSS_BUDGET_MB="${2:?missing value}"
      shift 2
      ;;
    --sustained-budget-mb)
      SUSTAINED_BUDGET_MB="${2:?missing value}"
      shift 2
      ;;
    --sustained-window-sec)
      SUSTAINED_WINDOW_SEC="${2:?missing value}"
      shift 2
      ;;
    --build)
      BUILD_FIRST=1
      shift
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

for num in "$DURATION_SEC" "$SAMPLE_SEC" "$SOAK_LOG_MS" "$RSS_BUDGET_MB" "$SUSTAINED_BUDGET_MB" "$SUSTAINED_WINDOW_SEC"; do
  if ! [[ "$num" =~ ^[0-9]+$ ]]; then
    echo "error: numeric options must be positive integers" >&2
    exit 1
  fi
done
if [[ "$DURATION_SEC" -le 0 || "$SAMPLE_SEC" -le 0 || "$SOAK_LOG_MS" -le 0 ]]; then
  echo "error: duration/sample/log must be > 0" >&2
  exit 1
fi

if [[ -z "$OUT_DIR" ]]; then
  TS="$(date +"%Y%m%d-%H%M%S")"
  OUT_DIR="${CLIENT_DIR}/build/soak-mem-${TS}"
fi

RSS_BUDGET_KB=$((RSS_BUDGET_MB * 1024))
SUSTAINED_BUDGET_KB=$((SUSTAINED_BUDGET_MB * 1024))

cmd=(
  "$SOAK_SCRIPT"
  --duration-sec "$DURATION_SEC"
  --sample-sec "$SAMPLE_SEC"
  --log-ms "$SOAK_LOG_MS"
  --max-rss-growth-kb "$RSS_BUDGET_KB"
  --out-dir "$OUT_DIR"
)
if [[ "$BUILD_FIRST" == "1" ]]; then
  cmd+=(--build)
fi
if [[ ${#FORWARD_ARGS[@]} -gt 0 ]]; then
  cmd+=(--)
  cmd+=("${FORWARD_ARGS[@]}")
fi

"${cmd[@]}"

PROC_CSV="${OUT_DIR}/process_samples.csv"
SUMMARY_TXT="${OUT_DIR}/mem-contract-summary.txt"
if [[ ! -f "$PROC_CSV" ]]; then
  echo "error: missing process samples CSV at ${PROC_CSV}" >&2
  exit 1
fi

stats="$(awk -F, -v window_sec="$SUSTAINED_WINDOW_SEC" '
  NR==1 { next }
  {
    ts[n]=$1
    rss[n]=$2
    n++
  }
  END {
    if (n == 0) {
      print "samples=0"
      exit
    }
    first_ts=ts[0]
    first_rss=rss[0]
    last_ts=ts[n-1]
    last_rss=rss[n-1]

    window_start=last_ts-window_sec
    wn=0
    wmin=last_rss
    wfirst=last_rss
    for (i=0; i<n; i++) {
      if (ts[i] >= window_start) {
        if (wn == 0) {
          wfirst=rss[i]
          wmin=rss[i]
        }
        wn++
        if (rss[i] < wmin) wmin=rss[i]
      }
    }
    if (wn == 0) {
      wn=1
      wfirst=last_rss
      wmin=last_rss
    }

    duration_sec=last_ts-first_ts
    if (duration_sec <= 0) duration_sec=1
    slope_kb_min=((last_rss-first_rss) * 60.0) / duration_sec

    window_duration_sec=last_ts-window_start
    if (window_duration_sec <= 0) window_duration_sec=1
    window_slope_kb_min=((last_rss-wfirst) * 60.0) / window_duration_sec

    print "samples=" n
    print "rss_first_kb=" first_rss
    print "rss_last_kb=" last_rss
    print "rss_growth_kb=" (last_rss-first_rss)
    print "rss_slope_kb_min=" slope_kb_min
    print "window_samples=" wn
    print "window_min_kb=" wmin
    print "window_growth_kb=" (last_rss-wmin)
    print "window_slope_kb_min=" window_slope_kb_min
  }
' "$PROC_CSV")"

samples=0
rss_first_kb=0
rss_last_kb=0
rss_growth_kb=0
rss_slope_kb_min=0
window_samples=0
window_min_kb=0
window_growth_kb=0
window_slope_kb_min=0

while IFS='=' read -r key value; do
  [[ -z "$key" ]] && continue
  case "$key" in
    samples) samples="${value:-0}" ;;
    rss_first_kb) rss_first_kb="${value:-0}" ;;
    rss_last_kb) rss_last_kb="${value:-0}" ;;
    rss_growth_kb) rss_growth_kb="${value:-0}" ;;
    rss_slope_kb_min) rss_slope_kb_min="${value:-0}" ;;
    window_samples) window_samples="${value:-0}" ;;
    window_min_kb) window_min_kb="${value:-0}" ;;
    window_growth_kb) window_growth_kb="${value:-0}" ;;
    window_slope_kb_min) window_slope_kb_min="${value:-0}" ;;
  esac
done <<< "$stats"

if [[ "${samples:-0}" -eq 0 ]]; then
  echo "error: no process samples in ${PROC_CSV}" >&2
  exit 1
fi

fail=0
reasons=()
if [[ "${rss_growth_kb:-0}" -gt "$RSS_BUDGET_KB" ]]; then
  fail=1
  reasons+=("total_rss_growth>${RSS_BUDGET_KB}kb")
fi
if [[ "${window_growth_kb:-0}" -gt "$SUSTAINED_BUDGET_KB" ]]; then
  fail=1
  reasons+=("sustained_rss_growth>${SUSTAINED_BUDGET_KB}kb")
fi

{
  echo "result=$([[ $fail -eq 0 ]] && echo PASS || echo FAIL)"
  echo "duration_sec=$DURATION_SEC"
  echo "sample_sec=$SAMPLE_SEC"
  echo "rss_budget_kb=$RSS_BUDGET_KB"
  echo "sustained_budget_kb=$SUSTAINED_BUDGET_KB"
  echo "sustained_window_sec=$SUSTAINED_WINDOW_SEC"
  echo "samples=${samples}"
  echo "rss_first_kb=${rss_first_kb}"
  echo "rss_last_kb=${rss_last_kb}"
  echo "rss_growth_kb=${rss_growth_kb}"
  echo "rss_slope_kb_min=${rss_slope_kb_min}"
  echo "window_samples=${window_samples}"
  echo "window_min_kb=${window_min_kb}"
  echo "window_growth_kb=${window_growth_kb}"
  echo "window_slope_kb_min=${window_slope_kb_min}"
  if [[ ${#reasons[@]} -gt 0 ]]; then
    printf 'fail_reasons=%s\n' "$(IFS=, ; echo "${reasons[*]}")"
  fi
} > "$SUMMARY_TXT"

echo "Memory contract summary: ${SUMMARY_TXT}"
if [[ $fail -ne 0 ]]; then
  echo "soak-mem-native: FAIL (${reasons[*]})" >&2
  exit 1
fi

echo "soak-mem-native: PASS"
