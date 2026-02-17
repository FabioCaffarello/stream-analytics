#!/usr/bin/env bash
set -euo pipefail

repo_root="${1:-$(pwd)}"
cd "$repo_root"

targets=(
  "internal/core"
  "internal/actors"
  "internal/interfaces"
)

pattern='"(google\.golang\.org/protobuf|github\.com/golang/protobuf)(/[^"]*)?"'
violations=()

scan_with_rg() {
  local target="$1"
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    violations+=("$line")
  done < <(rg -n --no-heading --glob '*.go' --regexp "$pattern" "$target" || true)
}

scan_with_grep() {
  local target="$1"
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    violations+=("$line")
  done < <(grep -R -n -E --include='*.go' "$pattern" "$target" || true)
}

for target in "${targets[@]}"; do
  if [ ! -d "$target" ]; then
    continue
  fi
  if command -v rg >/dev/null 2>&1; then
    scan_with_rg "$target"
  else
    scan_with_grep "$target"
  fi
done

if [ "${#violations[@]}" -gt 0 ]; then
  echo "protobuf import violates Domain Isolation; move to internal/shared/contracts boundary"
  printf '%s\n' "${violations[@]}"
  exit 1
fi

echo "invariants-check: domain isolation protobuf-free guard passed"

time_now_violations=()
scan_time_now_with_rg() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    time_now_violations+=("$line")
  done < <(rg -n --no-heading --glob '*.go' --glob '!**/*_test.go' --regexp 'time\.Now\(' internal/core || true)
}

scan_time_now_with_grep() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    time_now_violations+=("$line")
  done < <(grep -R -n -E --include='*.go' --exclude='*_test.go' 'time\.Now\(' internal/core || true)
}

if command -v rg >/dev/null 2>&1; then
  scan_time_now_with_rg
else
  scan_time_now_with_grep
fi

if [ "${#time_now_violations[@]}" -gt 0 ]; then
  echo "determinism invariant violation: internal/core must not call time.Now() directly"
  printf '%s\n' "${time_now_violations[@]}"
  exit 1
fi

replay_nats_violations=()
scan_replay_nats_with_rg() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    replay_nats_violations+=("$line")
  done < <(rg -n --no-heading --glob '*.go' --regexp 'github\.com/nats-io/nats\.go' internal/shared/replay || true)
}

scan_replay_nats_with_grep() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    replay_nats_violations+=("$line")
  done < <(grep -R -n -E --include='*.go' 'github\.com/nats-io/nats\.go' internal/shared/replay || true)
}

if [ -d "internal/shared/replay" ]; then
  if command -v rg >/dev/null 2>&1; then
    scan_replay_nats_with_rg
  else
    scan_replay_nats_with_grep
  fi
fi

if [ "${#replay_nats_violations[@]}" -gt 0 ]; then
  echo "replay package must remain offline; move NATS dependencies outside internal/shared/replay"
  printf '%s\n' "${replay_nats_violations[@]}"
  exit 1
fi

exchange_specific_core_violations=()
scan_exchange_specific_core_with_rg() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    exchange_specific_core_violations+=("$line")
  done < <(rg -n --no-heading --glob '*.go' --glob '!**/*_test.go' --regexp '"(binance|bybit|okx)"|exchange\.(Binance|Bybit|OKX)' internal/core || true)
}

scan_exchange_specific_core_with_grep() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    exchange_specific_core_violations+=("$line")
  done < <(grep -R -n -E --include='*.go' --exclude='*_test.go' '"(binance|bybit|okx)"|exchange\.(Binance|Bybit|OKX)' internal/core || true)
}

if [ -d "internal/core" ]; then
  if command -v rg >/dev/null 2>&1; then
    scan_exchange_specific_core_with_rg
  else
    scan_exchange_specific_core_with_grep
  fi
fi

if [ "${#exchange_specific_core_violations[@]}" -gt 0 ]; then
  echo "multi-exchange purity invariant violation: internal/core must not embed exchange-specific literals/constants"
  printf '%s\n' "${exchange_specific_core_violations[@]}"
  exit 1
fi

echo "invariants-check: determinism, replay-offline, and core exchange-purity guards passed"

# --- Guard: internal/core must not import internal/actors ---
core_actors_violations=()
scan_core_actors_with_rg() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    core_actors_violations+=("$line")
  done < <(rg -n --no-heading --glob '*.go' --glob '!**/*_test.go' --regexp '"github\.com/market-raccoon/internal/actors(/[^"]*)?"' internal/core || true)
}

scan_core_actors_with_grep() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    core_actors_violations+=("$line")
  done < <(grep -R -n -E --include='*.go' --exclude='*_test.go' '"github\.com/market-raccoon/internal/actors(/[^"]*)?"' internal/core || true)
}

if [ -d "internal/core" ]; then
  if command -v rg >/dev/null 2>&1; then
    scan_core_actors_with_rg
  else
    scan_core_actors_with_grep
  fi
fi

if [ "${#core_actors_violations[@]}" -gt 0 ]; then
  echo "layering violation: internal/core must not import internal/actors"
  printf '%s\n' "${core_actors_violations[@]}"
  exit 1
fi

# --- Guard: internal/interfaces must not import internal/adapters ---
interfaces_adapters_violations=()
scan_interfaces_adapters_with_rg() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    interfaces_adapters_violations+=("$line")
  done < <(rg -n --no-heading --glob '*.go' --glob '!**/*_test.go' --regexp '"github\.com/market-raccoon/internal/adapters(/[^"]*)?"' internal/interfaces || true)
}

scan_interfaces_adapters_with_grep() {
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    interfaces_adapters_violations+=("$line")
  done < <(grep -R -n -E --include='*.go' --exclude='*_test.go' '"github\.com/market-raccoon/internal/adapters(/[^"]*)?"' internal/interfaces || true)
}

if [ -d "internal/interfaces" ]; then
  if command -v rg >/dev/null 2>&1; then
    scan_interfaces_adapters_with_rg
  else
    scan_interfaces_adapters_with_grep
  fi
fi

if [ "${#interfaces_adapters_violations[@]}" -gt 0 ]; then
  echo "layering violation: internal/interfaces must not import internal/adapters directly"
  printf '%s\n' "${interfaces_adapters_violations[@]}"
  exit 1
fi

echo "invariants-check: layering guards (core->actors, interfaces->adapters) passed"
