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

mkdir -p "$LOG_DIR" "$SHOTS_DIR"

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

run_step() {
  local name="$1"
  shift
  local log_file="${LOG_DIR}/${name}.log"
  log "START ${name}: $*"
  if "$@" > >(tee "$log_file") 2>&1; then
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
  if make down > >(tee "${LOG_DIR}/down.log") 2>&1; then
    log "Compose stack stopped"
  else
    log "WARN make down failed"
  fi
}

log "IQ loop run directory: ${RUN_DIR}"
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

run_step "collect-artifacts" collect_artifacts
run_step "analyze-iq" node scripts/iq/analyze_iq_run.mjs "$RUN_DIR"

teardown_stack

if [[ -f "${RUN_DIR}/summary.json" ]]; then
  if ! node -e 'const fs=require("fs");const p=process.argv[1];const s=JSON.parse(fs.readFileSync(p,"utf8"));if(!s.overall_pass)process.exit(1);' "${RUN_DIR}/summary.json"; then
    OVERALL_RC=1
  fi
else
  OVERALL_RC=1
fi

log "IQ artifacts:"
log "  report: ${RUN_DIR}/report.md"
log "  logs:   ${LOG_DIR}"
log "  shots:  ${SHOTS_DIR}"

exit "$OVERALL_RC"
