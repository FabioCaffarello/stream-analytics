#!/usr/bin/env bash
# reset-processor-durables.sh — delete local JetStream durables used by the processor.
#
# Purpose:
#   Prevent local dev sessions from inheriting stale processor durables/backlog
#   (e.g. processor-v4, processor-v4-s0, processor-v4-s1) when scaling or
#   restarting processors.
#
# Usage:
#   ./scripts/ops/reset-processor-durables.sh [--base processor-v4] [--shards 2]
#   DRY_RUN=1 ./scripts/ops/reset-processor-durables.sh --shards 2
#
# Environment:
#   NATS_URL    (default: nats://127.0.0.1:4222)
#   STREAM_NAME (default: MARKETDATA)
#   DRY_RUN     (default: 0)
set -euo pipefail

BASE="processor-v4"
SHARDS=""
NATS_URL="${NATS_URL:-nats://127.0.0.1:4222}"
STREAM_NAME="${STREAM_NAME:-MARKETDATA}"
DRY_RUN="${DRY_RUN:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base)
      BASE="${2:-}"
      shift 2
      ;;
    --shards)
      SHARDS="${2:-}"
      shift 2
      ;;
    --stream)
      STREAM_NAME="${2:-}"
      shift 2
      ;;
    --nats-url)
      NATS_URL="${2:-}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    *)
      echo "reset-processor-durables: unknown arg: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "${BASE}" ]]; then
  echo "reset-processor-durables: --base must not be empty" >&2
  exit 1
fi
if [[ -n "${SHARDS}" ]] && ! [[ "${SHARDS}" =~ ^[0-9]+$ ]]; then
  echo "reset-processor-durables: --shards must be an integer (got ${SHARDS})" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

cat > "${tmpdir}/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/nats-io/nats.go"
)

func main() {
	base := strings.TrimSpace(os.Getenv("RESET_BASE"))
	if base == "" {
		fatalf("RESET_BASE must not be empty")
	}
	stream := strings.TrimSpace(os.Getenv("RESET_STREAM"))
	if stream == "" {
		fatalf("RESET_STREAM must not be empty")
	}
	natsURL := strings.TrimSpace(os.Getenv("RESET_NATS_URL"))
	if natsURL == "" {
		fatalf("RESET_NATS_URL must not be empty")
	}
	dryRun := strings.TrimSpace(os.Getenv("RESET_DRY_RUN")) == "1"

	names := []string{base}
	if raw := strings.TrimSpace(os.Getenv("RESET_SHARDS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			fatalf("RESET_SHARDS invalid: %q", raw)
		}
		for i := 0; i < n; i++ {
			names = append(names, fmt.Sprintf("%s-s%d", base, i))
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)

	fmt.Printf("reset-processor-durables: stream=%s nats=%s dry_run=%v\n", stream, natsURL, dryRun)
	for _, name := range names {
		fmt.Printf("  target: %s\n", name)
	}
	if dryRun {
		return
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer func() { _ = nc.Drain() }()

	js, err := nc.JetStream()
	if err != nil {
		fatalf("jetstream: %v", err)
	}

	for _, name := range names {
		if err := js.DeleteConsumer(stream, name); err != nil {
			// Keep going for missing/non-existent consumers; fail at end for other errors.
			fmt.Printf("skip %s: %v\n", name, err)
			continue
		}
		fmt.Printf("deleted %s\n", name)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "reset-processor-durables: "+format+"\n", args...)
	os.Exit(1)
}
EOF

cat > "${tmpdir}/go.mod" <<'EOF'
module resetdurables

go 1.25

require github.com/nats-io/nats.go v1.47.0
EOF

RESET_BASE="${BASE}" \
RESET_SHARDS="${SHARDS}" \
RESET_STREAM="${STREAM_NAME}" \
RESET_NATS_URL="${NATS_URL}" \
RESET_DRY_RUN="${DRY_RUN}" \
GOWORK=off \
go -C "${tmpdir}" mod tidy >/dev/null

RESET_BASE="${BASE}" \
RESET_SHARDS="${SHARDS}" \
RESET_STREAM="${STREAM_NAME}" \
RESET_NATS_URL="${NATS_URL}" \
RESET_DRY_RUN="${DRY_RUN}" \
GOWORK=off \
go -C "${tmpdir}" run .
