#!/usr/bin/env bash
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_DIR="artifacts/iq/${TS}"
LOG_DIR="${RUN_DIR}/logs"
SHOTS_DIR="${RUN_DIR}/shots"
RUNNER_LOG="${RUN_DIR}/runner.log"
KEEP_STACK="${IQ_KEEP_STACK:-0}"
OVERALL_RC=0
IQ_PROFILE_RAW="${IQ_PROFILE:-}"
IQ_PROFILE_REQUESTED="$(printf '%s' "$IQ_PROFILE_RAW" | tr '[:upper:]' '[:lower:]')"
IQ_PROFILE_EFFECTIVE="default"
IQ_PROFILE_SOURCE=""
PROFILE_OVERRIDES=()
LATEST_PASS_PTR="artifacts/iq/latest_pass"
SUMMARY_PASS=0

mkdir -p "$LOG_DIR" "$SHOTS_DIR"
if [[ ! -e "$LATEST_PASS_PTR" && ! -L "$LATEST_PASS_PTR" ]]; then
  : > "$LATEST_PASS_PTR"
fi

COMPOSE=(
  docker compose
  -f deploy/compose/docker-compose.yml
  --env-file deploy/envs/local.env
  --profile core
  --profile obs
  --profile client
)

log() {
  local msg="$1"
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$msg" | tee -a "$RUNNER_LOG"
}

is_truthy() {
  local raw="${1:-}"
  raw="$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')"
  [[ "$raw" == "1" || "$raw" == "true" || "$raw" == "yes" || "$raw" == "on" ]]
}

load_profile_env_file() {
  local profile_file="$1"
  local fail_on_conflict="$2"
  local line
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%"${line##*[![:space:]]}"}"
    line="${line#"${line%%[![:space:]]*}"}"
    [[ -z "$line" || "${line:0:1}" == "#" ]] && continue
    if [[ ! "$line" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
      log "Invalid profile line in ${profile_file}: ${line}"
      return 1
    fi
    local key="${line%%=*}"
    local value="${line#*=}"
    if [[ "$fail_on_conflict" == "1" && -n "${!key+x}" && "${!key}" != "$value" ]]; then
      log "RELAX-CONFLICT ${key}: env='${!key}' profile='${value}'"
      return 1
    fi
    export "${key}=${value}"
    PROFILE_OVERRIDES+=("${key}=${value}")
  done < "$profile_file"
}

configure_iq_profile() {
  if [[ -z "$IQ_PROFILE_REQUESTED" ]]; then
    if is_truthy "${CI:-0}"; then
      IQ_PROFILE_REQUESTED="release"
    else
      IQ_PROFILE_REQUESTED="default"
    fi
  fi

  case "$IQ_PROFILE_REQUESTED" in
    release|releaselike)
      IQ_PROFILE_EFFECTIVE="release"
      IQ_PROFILE_SOURCE="${ROOT_DIR}/scripts/iq/profiles/release.env"
      if [[ ! -f "$IQ_PROFILE_SOURCE" ]]; then
        log "Missing IQ profile source file: ${IQ_PROFILE_SOURCE}"
        return 1
      fi
      if ! load_profile_env_file "$IQ_PROFILE_SOURCE" "1"; then
        return 1
      fi
      ;;
    *)
      IQ_PROFILE_EFFECTIVE="default"
      IQ_PROFILE_SOURCE=""
      ;;
  esac

  export IQ_PROFILE="$IQ_PROFILE_REQUESTED"
  export IQ_EFFECTIVE_PROFILE_NAME="$IQ_PROFILE_EFFECTIVE"
  export IQ_EFFECTIVE_PROFILE_SOURCE="$IQ_PROFILE_SOURCE"
}

run_step() {
  local name="$1"
  shift
  local log_file="${LOG_DIR}/${name}.log"
  log "START ${name}: $*"
  if "$@" 2>&1 | tee "$log_file"; then
    log "PASS ${name}"
    return 0
  fi
  log "FAIL ${name}"
  OVERALL_RC=1
  return 1
}

wait_http() {
  local url="$1"
  local label="$2"
  local timeout="${3:-120}"
  local i
  for i in $(seq 1 "$timeout"); do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      printf "ready: %s (%s)\n" "$label" "$url"
      return 0
    fi
    sleep 1
  done
  printf "timeout: %s (%s)\n" "$label" "$url" >&2
  return 1
}

collect_artifacts() {
  "${COMPOSE[@]}" ps > "${LOG_DIR}/compose.ps.log" 2>&1 || true
  "${COMPOSE[@]}" logs --no-color --timestamps > "${LOG_DIR}/compose.all.log" 2>&1 || true

  local services=(
    nats timescale clickhouse migrate server consumer processor store client prometheus grafana
  )
  local svc
  for svc in "${services[@]}"; do
    "${COMPOSE[@]}" logs --no-color --timestamps "$svc" > "${LOG_DIR}/compose.${svc}.log" 2>&1 || true
  done

  curl -fsS "http://127.0.0.1:8080/metrics" > "${LOG_DIR}/server.metrics.prom" 2>"${LOG_DIR}/server.metrics.err" || true
  curl -fsS "http://127.0.0.1:8080/readyz" > "${LOG_DIR}/server.readyz.log" 2>&1 || true
  curl -fsS "http://127.0.0.1:8090/healthz" > "${LOG_DIR}/client.healthz.log" 2>&1 || true
}

teardown_stack() {
  if [[ "$KEEP_STACK" == "1" ]]; then
    log "IQ_KEEP_STACK=1: keeping compose stack up"
    return 0
  fi
  log "Stopping compose stack (make down)"
  if make down 2>&1 | tee "${LOG_DIR}/down.log"; then
    log "Compose stack stopped"
  else
    log "WARN make down failed"
  fi
}

log "IQ loop run directory: ${RUN_DIR}"
if ! configure_iq_profile; then
  OVERALL_RC=1
  log "Aborting IQ loop due to profile configuration failure"
  exit "$OVERALL_RC"
fi
if [[ "$IQ_PROFILE_EFFECTIVE" == "release" ]]; then
  log "Effective IQ profile: ${IQ_PROFILE_EFFECTIVE} (requested=${IQ_PROFILE_REQUESTED}, source=${IQ_PROFILE_SOURCE})"
  log "Effective IQ profile overrides: ${PROFILE_OVERRIDES[*]}"
else
  log "Effective IQ profile: default (requested=${IQ_PROFILE_REQUESTED})"
fi
log "Bringing stack up with PROCESSOR_REPLICAS=2"

run_step "up" make up PROCESSOR_REPLICAS=2
run_step "ready-core" make smoke
run_step "ready-client-healthz" wait_http "http://127.0.0.1:8090/healthz" "client /healthz" 120
run_step "ready-client-root" wait_http "http://127.0.0.1:8090/" "client /" 120

run_step \
  "playwright-smoke" \
  env \
  IQ_BASE_URL="http://localhost:8090" \
  IQ_SHOTS_DIR="$SHOTS_DIR" \
  IQ_LOGS_DIR="$LOG_DIR" \
  node tests/playwright/iq-smoke.mjs

run_step \
  "legacy-negative" \
  env \
  LEGACY_NEGATIVE_SERVER_BASE="http://127.0.0.1:8080" \
  LEGACY_NEGATIVE_WS_URL="ws://127.0.0.1:8080/ws?api_key=prod_key_1" \
  LEGACY_NEGATIVE_PLAYWRIGHT_SMOKE="${LOG_DIR}/playwright-smoke.json" \
  LEGACY_NEGATIVE_LOG="${LOG_DIR}/legacy-negative.log" \
  LEGACY_NEGATIVE_JSON="${LOG_DIR}/legacy-negative.json" \
  LEGACY_NEGATIVE_METRICS_OUT="${LOG_DIR}/legacy-negative.server.metrics.prom" \
  node scripts/iq/legacy_negative_test.mjs

run_step "collect-artifacts" collect_artifacts
run_step "analyze-iq" node scripts/iq/analyze_iq_run.mjs "$RUN_DIR"

teardown_stack

if [[ -f "${RUN_DIR}/summary.json" ]]; then
  if node -e 'const fs=require("fs");const p=process.argv[1];const s=JSON.parse(fs.readFileSync(p,"utf8"));if(!s.overall_pass)process.exit(1);' "${RUN_DIR}/summary.json"; then
    SUMMARY_PASS=1
  else
    OVERALL_RC=1
  fi
else
  OVERALL_RC=1
fi

if [[ "$SUMMARY_PASS" == "1" ]]; then
  if ln -sfn "$TS" "$LATEST_PASS_PTR" 2>/dev/null; then
    log "Updated latest pass pointer: ${LATEST_PASS_PTR} -> ${TS}"
  else
    printf '%s\n' "$RUN_DIR" > "$LATEST_PASS_PTR"
    log "Updated latest pass pointer file: ${LATEST_PASS_PTR} = ${RUN_DIR}"
  fi
fi

log "IQ artifacts:"
log "  report: ${RUN_DIR}/report.md"
log "  logs:   ${LOG_DIR}"
log "  shots:  ${SHOTS_DIR}"

exit "$OVERALL_RC"
