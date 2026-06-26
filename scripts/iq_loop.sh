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
IQ_PROFILE_REQUIRED="ci-strict"
IQ_PROFILE_EFFECTIVE=""
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

is_release_mode() {
  local run_mode="${RUN_MODE:-${STREAM_ANALYTICS_MODE:-${IQ_RUN_MODE:-${IQ_MODE:-}}}}"
  run_mode="$(printf '%s' "$run_mode" | tr '[:upper:]' '[:lower:]')"
  if [[ "$run_mode" == "prod" || "$run_mode" == "production" || "$run_mode" == "release" ]]; then
    return 0
  fi

  if is_truthy "${RELEASE:-0}" || is_truthy "${IQ_RELEASE:-0}" || is_truthy "${RELEASE_MODE:-0}" || is_truthy "${IQ_RELEASE_MODE:-0}"; then
    return 0
  fi

  return 1
}

profile_mode_label() {
  local ci=0
  local release=0
  if is_truthy "${CI:-0}"; then
    ci=1
  fi
  if is_release_mode; then
    release=1
  fi

  if [[ "$ci" == "1" && "$release" == "1" ]]; then
    printf 'ci+release'
    return 0
  fi
  if [[ "$ci" == "1" ]]; then
    printf 'ci'
    return 0
  fi
  if [[ "$release" == "1" ]]; then
    printf 'release'
    return 0
  fi
  printf 'local'
}

configure_iq_profile() {
  local mode
  mode="$(profile_mode_label)"

  if [[ -z "$IQ_PROFILE_REQUESTED" ]]; then
    if [[ "$mode" == "ci" || "$mode" == "release" || "$mode" == "ci+release" ]]; then
      log "IQ_PROFILE is required in ${mode} mode and must be '${IQ_PROFILE_REQUIRED}'"
      log "Action: run IQ_PROFILE=${IQ_PROFILE_REQUIRED} PROCESSOR_REPLICAS=2 ./scripts/iq_loop.sh"
      return 1
    fi
    IQ_PROFILE_REQUESTED="$IQ_PROFILE_REQUIRED"
    log "IQ_PROFILE not provided; defaulting to '${IQ_PROFILE_REQUIRED}' for local run"
  fi

  if [[ "$IQ_PROFILE_REQUESTED" != "$IQ_PROFILE_REQUIRED" ]]; then
    log "Unsupported IQ_PROFILE='${IQ_PROFILE_REQUESTED}' (expected '${IQ_PROFILE_REQUIRED}')"
    log "Action: run IQ_PROFILE=${IQ_PROFILE_REQUIRED} PROCESSOR_REPLICAS=2 ./scripts/iq_loop.sh"
    return 1
  fi

  export IQ_PROFILE="$IQ_PROFILE_REQUESTED"

  local profile_exports
  if ! profile_exports="$(node scripts/iq/profile_loader.mjs --shell-export 2> >(tee -a "$RUNNER_LOG" >&2))"; then
    log "IQ profile validation failed before stack startup"
    log "Action: remove relax overrides and keep IQ_PROFILE=${IQ_PROFILE_REQUIRED}"
    return 1
  fi

  eval "$profile_exports"

  IQ_PROFILE_EFFECTIVE="${IQ_EFFECTIVE_PROFILE_NAME:-$IQ_PROFILE_REQUIRED}"
  IQ_PROFILE_SOURCE="${IQ_EFFECTIVE_PROFILE_SOURCE:-}"
  PROFILE_OVERRIDES=(
    "IQ_STRICT=${IQ_STRICT:-}"
    "IQ_REQUIRE_STATS_CANONICAL=${IQ_REQUIRE_STATS_CANONICAL:-}"
    "IQ_FALLBACK_STRICT=${IQ_FALLBACK_STRICT:-}"
    "IQ_LEGACY_STRICT=${IQ_LEGACY_STRICT:-}"
    "IQ_ALLOW_BATCHED_FALLBACK=${IQ_ALLOW_BATCHED_FALLBACK:-}"
    "IQ_ALLOW_STATS_FALLBACK=${IQ_ALLOW_STATS_FALLBACK:-}"
    "IQ_ALLOW_UNEXPECTED_SKIPS=${IQ_ALLOW_UNEXPECTED_SKIPS:-}"
    "IQ_WIRE_BUDGET_CHANNELS=${IQ_WIRE_BUDGET_CHANNELS:-}"
    "IQ_WIRE_P95_BUDGET_MS=${IQ_WIRE_P95_BUDGET_MS:-}"
    "IQ_WIRE_P99_BUDGET_MS=${IQ_WIRE_P99_BUDGET_MS:-}"
    "IQ_WIRE_BYTES_P95_BUDGET=${IQ_WIRE_BYTES_P95_BUDGET:-}"
    "IQ_WIRE_BYTES_P99_BUDGET=${IQ_WIRE_BYTES_P99_BUDGET:-}"
    "IQ_ROUTER_STREAM_STATE_MAX=${IQ_ROUTER_STREAM_STATE_MAX:-}"
    "IQ_LAYER_STREAM_STATE_MAX=${IQ_LAYER_STREAM_STATE_MAX:-}"
    "PROCESSOR_REPLICAS=${PROCESSOR_REPLICAS:-}"
  )
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
log "Effective IQ profile: ${IQ_PROFILE_EFFECTIVE} (requested=${IQ_PROFILE_REQUESTED}, source=${IQ_PROFILE_SOURCE})"
log "Effective IQ profile overrides: ${PROFILE_OVERRIDES[*]}"
log "Effective IQ profile fingerprint: ${IQ_EFFECTIVE_PROFILE_FINGERPRINT_HASH:-<missing>}"
log "Bringing stack up with PROCESSOR_REPLICAS=${PROCESSOR_REPLICAS:-2}"

run_step "up" make up PROCESSOR_REPLICAS="${PROCESSOR_REPLICAS:-2}"
run_step "ready-core" make smoke
run_step "ready-client-healthz" wait_http "http://127.0.0.1:8090/healthz" "client /healthz" 120
run_step "ready-client-root" wait_http "http://127.0.0.1:8090/" "client /" 120

run_step \
  "playwright-smoke" \
  env \
  IQ_BASE_URL="http://localhost:8090" \
  IQ_SHOTS_DIR="$SHOTS_DIR" \
  IQ_LOGS_DIR="$LOG_DIR" \
  node tests/playwright/scripts/iq-smoke.mjs

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
log "  effective profile: ${RUN_DIR}/effective-profile.json"
log "  logs:   ${LOG_DIR}"
log "  shots:  ${SHOTS_DIR}"

exit "$OVERALL_RC"
