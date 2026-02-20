SHELL := /usr/bin/env bash

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck
PRE_COMMIT ?= pre-commit

GOLANGCI_LINT_VERSION ?= v2.6.0
GOVULNCHECK_VERSION ?= latest
PROTOC_GEN_GO_VERSION ?= v1.36.11
BUF_VERSION ?= v1.57.2
PROMTOOL_VERSION ?= 3.9.1
BENCHSTAT_VERSION ?= v0.0.0-20260211190930-8161c38c6cdc
PROCESSOR_REPLICAS ?= 1
PROCESSOR_SHARD_COUNT ?= $(PROCESSOR_REPLICAS)

APP_NAME ?= server
APP_CMD ?= ./cmd/server

GO_TEST_FLAGS ?= -covermode=atomic
GO_TEST_RACE_TIMEOUT ?= 10m
GO_TEST_RACE_FLAGS ?= -race -covermode=atomic -timeout=$(GO_TEST_RACE_TIMEOUT)
INTEGRATION_TEST_PATTERN ?= Integration|E2E|Conformance
INTEGRATION_TEST_PKGS ?= ./internal/adapters/jetstream ./cmd/consumer
INTEGRATION_TEST_TRIGGER_REGEX ?= ^(internal/adapters/jetstream/|cmd/consumer/|go\.work$$|go\.work\.sum$$)
TEST_RACE_PKGS ?= ./internal/adapters/jetstream ./internal/shared/replay ./internal/actors/runtime ./cmd/consumer
REPLAY_GOLDEN_PKGS ?= ./internal/shared/replay ./cmd/consumer ./internal/adapters/jetstream
REPLAY_GOLDEN_PATTERN ?= TestGoldenReplay|TestReplayIngestGolden1000|TestReplayHeatmapGolden1000|TestHeatmapReplayByteStable50Runs|TestShardGolden|TestShardReplayInvariant
REPLAY_GOLDEN_TRIGGER_REGEX ?= ^(internal/shared/replay/|internal/shared/envelope/|internal/.*/sequencer|internal/adapters/storage/|internal/adapters/jetstream/shard)
REPLAY_GOLDEN_CHANGED ?=
SOAK_OUT_FILE ?= .context/evidence/w5-soak.txt
SOAK_VPVR_OUT_FILE ?= .context/evidence/vpvr-soak.txt
SOAK_GO_CACHE ?= /tmp/go-build
SOAK_WS_PATTERN ?= TestConsumer_ConnectDisconnectCycle_(NoGoroutineLeak|HeapStable)
SOAK_BOUNDEDMAP_PATTERN ?= TestBoundedMap_(ConcurrentAccess|EvictBySizeLRU|EvictByTTL)
SOAK_VPVR_PATTERN ?= TestVPVROverloadSoakBurstDeterministicBudgets
SOAK_STORE_OUT_FILE ?= .context/evidence/s3-store-soak.txt
SOAK_STORE_PATTERN ?= TestStoreSoak_
SOAK_ROUNDTRIP_OUT_FILE ?= .context/evidence/c4-cold-roundtrip.txt
SOAK_PIPELINE_OUT_FILE ?= .context/evidence/c4-pipeline-soak.txt
SOAK_C4_OUT_FILE ?= .context/evidence/c4-production-soak.txt
RUNTIME_GATE_REPORT_DIR ?= .context/evidence/runtime-gate
VULN_REQUIRED ?= false
MODULE ?=
MSG_FILE ?=
MSG ?= build(local): commit message check sample
PROTOBUF_BIN_DIR ?= $(CURDIR)/bin
BUF ?= $(PROTOBUF_BIN_DIR)/buf
PROTOC_GEN_GO ?= $(PROTOBUF_BIN_DIR)/protoc-gen-go

GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.cache/golangci-lint
export GOCACHE
export GOMODCACHE
export GOLANGCI_LINT_CACHE

MODULE_DIRS := $(shell ./scripts/list-modules.sh)

.PHONY: help install-tools tools modules workspace-check tidy tidy-check go-tidy-check tidy-check-changed fmt fmt-check vet shell-script-check quick ci-local contract-gates operability-gates docs-check docs-check-fast docs-check-full docs-fix check-doc-headers check-doc-links check-doc-links-changed check-truth-map check-feature-pack-links check-pack-subjects-vs-event-bus registry-check invariants-check legacy-check-staged legacy-check lint lint-changed smoke runtime-gate runtime-gate-full test test-root test-workspace test-workspace-race test-unit test-integration test-integration-changed test-race test-partition test-replay-golden test-replay-golden-if-needed replay-trigger-self-check test-soak soak-check soak-vpvr soak-cold-path soak-store soak-roundtrip soak-pipeline soak-ws-delivery soak-c4-production soak-full test-short test-short-changed bench-hotpath bench-budget vuln build run clean docker-build up down up-infra up-core dev-scale-smoke ps logs pre-commit-install commit-msg-check commit-msg-self-check proto-tools proto-lint proto-gen proto-gen-if-needed proto-breaking proto-check proto ci

help:
	@echo "Targets:"
	@echo "  make install-tools      - install golangci-lint and govulncheck"
	@echo "  make tools              - install pinned protobuf generation tools to ./bin"
	@echo "  make modules            - list modules from go.work"
	@echo "  make workspace-check    - validate all go.work modules resolve with go list"
	@echo "  make tidy               - run go mod tidy in workspace modules"
	@echo "  make tidy-check         - fail if go.mod/go.sum are not tidy"
	@echo "  make go-tidy-check      - alias for tidy-check"
	@echo "  make tidy-check-changed - run tidy-check only when staged/worktree includes go.mod/go.sum/go.work"
	@echo "  make fmt                - format all Go files (gofmt)"
	@echo "  make fmt-check          - check formatting (gofmt -l)"
	@echo "  make shell-script-check - syntax-check all scripts/*.sh with bash -n"
	@echo "  make vet                - run go vet in workspace modules"
	@echo "  make legacy-check-staged - scan staged files + key configs for forbidden legacy strings"
	@echo "  make legacy-check       - scan full repository for forbidden legacy strings"
	@echo "  make quick              - fast local loop (fmt-check + vet + invariants-check + short tests)"
	@echo "  make ci-local           - strict local chain (quick -> docs -> invariants -> unit -> integration -> replay -> proto)"
	@echo "  make contract-gates     - W6 contract gate chain (registry -> replay -> proto)"
	@echo "  make docs-check         - strict docs guardrails (alias for docs-check-full)"
	@echo "  make docs-check-fast    - lightweight docs guardrails for local loop"
	@echo "  make docs-check-full    - full strict docs guardrails"
	@echo "  make docs-fix           - print docs fix checklist based on current guardrail findings"
	@echo "  make invariants-check   - enforce domain isolation and runtime invariants checks"
	@echo "  make lint               - run golangci-lint in workspace modules"
	@echo "  make lint-changed       - run invariants + golangci-lint only in changed Go modules"
	@echo "  make test               - alias for make test-root"
	@echo "  make test-root          - workspace-safe root test entrypoint"
	@echo "  make test-workspace     - run all workspace tests module-by-module"
	@echo "  make test-workspace-race - run module-by-module tests with -race"
	@echo "  make test-unit          - run fast short/unit-oriented workspace tests"
	@echo "  make test-integration   - run integration-focused suites in selected packages"
	@echo "  make test-integration-changed - run integration suite only when trigger paths changed (SKIP_INTEGRATION=1 to skip)"
	@echo "  make test-race          - run targeted high-risk race-enabled suites"
	@echo "  make test-partition     - run partitioned suites (unit -> integration -> race -> soak)"
	@echo "  make test-replay-golden - run replay golden tests only (shared/replay + cmd/consumer)"
	@echo "  make test-replay-golden-if-needed - run replay golden only when changed paths match trigger regex"
	@echo "  make replay-trigger-self-check - validate replay trigger include/exclude paths"
	@echo "  make bench-hotpath      - run benchmark harness for codec/policykit hot paths"
	@echo "  make bench-budget      - enforce per-benchmark allocation budgets (<10 allocs/event target)"
	@echo "  make test-soak          - alias for soak-check long-running validation"
	@echo "  make soak-check         - run soak harness checks and emit evidence file"
	@echo "  make soak-vpvr          - run deterministic VPVR burst soak checks"
	@echo "  make soak-cold-path     - run cold-path commit/ack soak harness"
	@echo "  make soak-store         - run store pipeline dedup/batch soak harness"
	@echo "  make soak-roundtrip     - run cold-path candle/stats roundtrip + store write soaks"
	@echo "  make soak-pipeline      - run 10M multi-exchange + pipeline/delivery soaks (MR_ENABLE_SOAK=1)"
	@echo "  make soak-ws-delivery   - run full vertical + ws backpressure + guardian restart soaks"
	@echo "  make soak-c4-production - run C4 production soak (10M/4 exchanges + WS slow clients 50)"
	@echo "  make soak-full          - run all soak harnesses"
	@echo "  make test-short         - run short tests"
	@echo "  make test-short-changed - run short tests only in changed Go modules"
	@echo "  make vuln               - run govulncheck"
	@echo "  make build              - build all binaries under cmd/* (package main)"
	@echo "  make run                - run selected app (default: server)"
	@echo "  make down               - stop full stack"
	@echo "  make up                 - start full stack (nats + timescale + clickhouse + app services + observability)"
	@echo "                           vars: PROCESSOR_REPLICAS=N, PROCESSOR_SHARD_COUNT (defaults to N; consumer fixed at 1 replica)"
	@echo "                           dev/local: SHARD_INDEX is auto-derived from replica hostname when unset"
	@echo "  make up-infra           - start only infrastructure services (nats + timescale + clickhouse + prometheus + grafana)"
	@echo "  make up-core            - start infra + core app services (no observability)"
	@echo "  make smoke              - wait up to 60s for /readyz on core services via docker compose"
	@echo "  make runtime-gate       - run up-core + smoke + soak-check with versioned evidence report"
	@echo "  make runtime-gate-full  - run runtime-gate plus heavy C4 pipeline and ws-delivery soaks"
	@echo "  make dev-scale-smoke    - start core with N processor replicas and print shard-resolution evidence"
	@echo "                           vars: N or PROCESSOR_REPLICAS (default 3 for this target)"
	@echo "  make ps                 - list compose service status"
	@echo "  make logs               - stream compose logs"
	@echo "  make pre-commit-install - install pre-commit hooks"
	@echo "  make commit-msg-check   - validate Conventional Commit message (MSG_FILE or MSG)"
	@echo "  make commit-msg-self-check - run pass/fail commit-msg examples"
	@echo "  make proto-tools        - install/verify local proto tools in ./bin"
	@echo "  make proto-lint         - run buf lint on proto contracts"
	@echo "  make proto-gen          - generate Go code from proto contracts"
	@echo "  make proto-gen-if-needed - generate proto code only when proto/config changed"
	@echo "  make proto-breaking     - check proto breaking changes against main"
	@echo "  make proto-check        - lint + breaking + gen and fail if proto outputs are dirty"
	@echo "  make proto              - run proto-lint + proto-gen"
	@echo "  make ci                 - tidy-check + fmt-check + lint + test + vuln + build"
	@echo ""
	@echo "Optional: MODULE=./pkg/hello-lib to target a single module"

install-tools:
	@$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@$(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

tools:
	@mkdir -p "$(CURDIR)/bin"
	@GOWORK=off GOBIN="$(CURDIR)/bin" $(GO) -C internal/tools install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	@echo "Installed protoc-gen-go@$(PROTOC_GEN_GO_VERSION) to $(CURDIR)/bin/protoc-gen-go"

modules:
	@./scripts/list-modules.sh

workspace-check:
	@./scripts/check-workspace-modules.sh

define RUN_IN_MODULES
	@MODULE='$(MODULE)' ./scripts/for-each-module.sh $(1)
endef

tidy:
	$(call RUN_IN_MODULES,$(GO) mod tidy)

tidy-check:
	@set -euo pipefail; \
	status=0; \
	for mod in $(if $(MODULE),$(MODULE),$(MODULE_DIRS)); do \
		modfile="$$mod/go.mod"; \
		sumfile="$$mod/go.sum"; \
		modtmp="$$(mktemp)"; \
		sumtmp="$$(mktemp)"; \
		cp "$$modfile" "$$modtmp"; \
		if [ -f "$$sumfile" ]; then cp "$$sumfile" "$$sumtmp"; else : > "$$sumtmp"; fi; \
		(cd "$$mod" && $(GO) mod tidy >/dev/null); \
		if ! diff -q "$$modtmp" "$$modfile" >/dev/null; then \
			echo "$$modfile is not tidy"; status=1; \
		fi; \
		if [ -f "$$sumfile" ]; then \
			if ! diff -q "$$sumtmp" "$$sumfile" >/dev/null; then \
				echo "$$sumfile is not tidy"; status=1; \
			fi; \
		fi; \
		rm -f "$$modtmp" "$$sumtmp"; \
	done; \
	if [ "$$status" -ne 0 ]; then \
		echo "Run: make tidy"; \
		exit 1; \
	fi

go-tidy-check:
	@$(MAKE) tidy-check

tidy-check-changed:
	@set -euo pipefail; \
	changed="$$(./scripts/list-changed-files.sh --auto || true)"; \
	if [ -z "$$changed" ]; then \
		echo "tidy-check-changed: no changed files; skipping"; \
		exit 0; \
	fi; \
	if printf "%s\n" "$$changed" | rg -q '(^|/)go\.(mod|sum)$$|^go\.work$$|^go\.work\.sum$$'; then \
		echo "tidy-check-changed: go module/workspace files changed; running tidy-check"; \
		$(MAKE) tidy-check; \
	else \
		echo "tidy-check-changed: no go.mod/go.sum/go.work changes; skipping"; \
	fi

fmt:
	@./scripts/gofmt-all.sh write

fmt-check:
	@./scripts/gofmt-all.sh check

vet:
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) vet $$pkgs; else echo "no packages to vet (skipping)"; fi')

shell-script-check:
	@bash -n scripts/*.sh

quick:
	@$(MAKE) fmt-check
	@$(MAKE) vet
	@$(MAKE) invariants-check
	@$(MAKE) test-unit

ci-local:
	@./scripts/ci-local.sh

contract-gates:
	@$(MAKE) registry-check
	@$(MAKE) invariants-check
	@$(MAKE) test-workspace
	@$(MAKE) lint
	@$(MAKE) test-replay-golden
	@$(MAKE) test-workspace-race
	@$(MAKE) proto-lint
	@$(MAKE) proto-breaking

operability-gates:
	@./scripts/check-operability.sh

docs-check:
	@$(MAKE) docs-check-full

docs-check-fast:
	@$(MAKE) check-doc-links-changed
	@$(MAKE) check-truth-map
	@$(MAKE) check-feature-pack-links
	@$(MAKE) check-pack-subjects-vs-event-bus
	@$(MAKE) registry-check

docs-check-full:
	@$(MAKE) check-doc-headers
	@$(MAKE) check-doc-links
	@$(MAKE) check-truth-map
	@$(MAKE) check-feature-pack-links
	@$(MAKE) check-pack-subjects-vs-event-bus
	@$(MAKE) registry-check

check-doc-headers:
	@./scripts/check-doc-headers.sh

check-doc-links:
	@./scripts/check-doc-links.sh

check-doc-links-changed:
	@./scripts/check-doc-links.sh --changed-only

check-truth-map:
	@./scripts/check-truth-map.sh

check-feature-pack-links:
	@./scripts/check-feature-pack-links.sh

check-pack-subjects-vs-event-bus:
	@./scripts/check-pack-subjects-vs-event-bus.sh

registry-check:
	@./scripts/check-registry.sh

docs-fix:
	@./scripts/check-doc-headers.sh --fix-hints
	@./scripts/check-doc-links.sh --fix-hints
	@./scripts/check-truth-map.sh --fix-hints
	@./scripts/check-feature-pack-links.sh --fix-hints
	@./scripts/check-pack-subjects-vs-event-bus.sh --fix-hints

invariants-check:
	@./scripts/check-domain-isolation.sh "$(CURDIR)"

legacy-check-staged:
	@./scripts/legacy-scan.sh --staged

legacy-check:
	@./scripts/legacy-scan.sh --all

lint: invariants-check
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GOLANGCI_LINT) run --config "$(CURDIR)/.golangci.yml" ./...; else echo "no packages to lint (skipping)"; fi')

lint-changed:
	@set -euo pipefail; \
	mods="$$(./scripts/changed-go-modules.sh --auto || true)"; \
	if [ -z "$$mods" ]; then \
		echo "lint-changed: no changed Go modules; skipping"; \
		exit 0; \
	fi; \
	echo "lint-changed: running invariants-check before module lint"; \
	$(MAKE) invariants-check; \
	status=0; \
	while IFS= read -r mod; do \
		[ -z "$$mod" ] && continue; \
		echo ">>> $$mod: $(GOLANGCI_LINT)"; \
		( cd "$$mod" && pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GOLANGCI_LINT) run --config "$(CURDIR)/.golangci.yml" ./...; else echo "no packages to lint (skipping)"; fi ) || status=$$?; \
	done <<< "$$mods"; \
	exit $$status

test:
	$(MAKE) test-root

test-root:
	@echo "go.work multi-module repository detected: use workspace-aware targets instead of 'go test ./...' at repository root."
	$(MAKE) workspace-check
	$(MAKE) test-workspace

test-workspace: invariants-check workspace-check
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) test $(GO_TEST_FLAGS) $$pkgs; else echo "no packages to test (skipping)"; fi')

test-workspace-race: invariants-check workspace-check
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) test $(GO_TEST_RACE_FLAGS) $$pkgs; else echo "no packages to test (skipping)"; fi')

test-unit: invariants-check
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) test -short $$pkgs; else echo "no packages to test (skipping)"; fi')

test-integration: invariants-check
	@$(GO) test $(GO_TEST_FLAGS) $(INTEGRATION_TEST_PKGS) -run '$(INTEGRATION_TEST_PATTERN)'

test-integration-changed:
	@set -euo pipefail; \
	if [ "${SKIP_INTEGRATION:-0}" = "1" ]; then \
		echo "test-integration-changed: SKIP_INTEGRATION=1; skipping"; \
		exit 0; \
	fi; \
	changed="$$(./scripts/list-changed-files.sh --auto || true)"; \
	if [ -z "$$changed" ]; then \
		echo "test-integration-changed: no changed files; skipping"; \
		exit 0; \
	fi; \
	if printf "%s\n" "$$changed" | rg -q -e '$(INTEGRATION_TEST_TRIGGER_REGEX)'; then \
		echo "test-integration-changed: trigger matched; running integration tests"; \
		$(MAKE) test-integration; \
	else \
		echo "test-integration-changed: trigger not matched; skipping"; \
	fi

BENCH_HOTPATH_PKGS ?= ./internal/shared/codec ./internal/shared/policykit ./internal/shared/hash ./internal/core/marketdata/app ./internal/core/aggregation/domain ./internal/core/aggregation/app ./internal/actors/delivery/runtime ./internal/interfaces/ws

bench-hotpath: invariants-check
	@$(GO) test -run=^$$ -bench=HotPath -benchmem ./internal/shared/codec ./internal/shared/policykit ./internal/shared/hash
	@$(GO) test -run=^$$ -bench=BenchmarkIngest -benchmem ./internal/core/marketdata/app
	@$(GO) test -run=^$$ -bench=BenchmarkApplyDelta -benchmem ./internal/core/aggregation/domain
	@$(GO) test -run=^$$ -bench=BenchmarkE2E -benchmem ./internal/core/aggregation/app
	@$(GO) test -run=^$$ -bench=BenchmarkDeliveryFanOut -benchmem ./internal/actors/delivery/runtime
	@$(GO) test -run=^$$ -bench=BenchmarkSessionWrite -benchmem ./internal/interfaces/ws

bench-budget: invariants-check
	@bash scripts/bench-budget.sh

bench-baseline: invariants-check
	@$(GO) test -run='^$$' -bench=HotPath -benchmem -count=5 ./internal/shared/codec ./internal/shared/policykit ./internal/shared/hash > .benchmarks/baseline.txt 2>&1
	@$(GO) test -run='^$$' -bench=BenchmarkIngest -benchmem -count=5 ./internal/core/marketdata/app >> .benchmarks/baseline.txt 2>&1
	@$(GO) test -run='^$$' -bench=BenchmarkApplyDelta -benchmem -count=5 ./internal/core/aggregation/domain >> .benchmarks/baseline.txt 2>&1
	@$(GO) test -run='^$$' -bench=BenchmarkE2E -benchmem -count=5 ./internal/core/aggregation/app >> .benchmarks/baseline.txt 2>&1
	@$(GO) test -run='^$$' -bench=BenchmarkDeliveryFanOut -benchmem -count=5 ./internal/actors/delivery/runtime >> .benchmarks/baseline.txt 2>&1
	@$(GO) test -run='^$$' -bench=BenchmarkSessionWrite -benchmem -count=5 ./internal/interfaces/ws >> .benchmarks/baseline.txt 2>&1
	@echo "bench-baseline: saved to .benchmarks/baseline.txt"

bench-check: invariants-check
	@bash scripts/bench-check.sh

test-race: invariants-check
	@$(GO) test $(GO_TEST_RACE_FLAGS) $(TEST_RACE_PKGS)

test-partition:
	@$(MAKE) test-unit
	@$(MAKE) test-integration
	@$(MAKE) test-race
	@$(MAKE) soak-check

test-replay-golden: invariants-check
	@$(GO) test $(GO_TEST_FLAGS) $(REPLAY_GOLDEN_PKGS) -run '$(REPLAY_GOLDEN_PATTERN)'

test-replay-golden-if-needed:
	@set -euo pipefail; \
	if [ -z "$(REPLAY_GOLDEN_CHANGED)" ]; then \
		echo "Set REPLAY_GOLDEN_CHANGED with changed paths (e.g. git diff --name-only HEAD~1)"; \
		exit 1; \
	fi; \
	if printf "%s\n" "$(REPLAY_GOLDEN_CHANGED)" | tr ' ' '\n' | rg -q -e '$(REPLAY_GOLDEN_TRIGGER_REGEX)'; then \
		echo "replay trigger matched; running test-replay-golden"; \
		$(MAKE) test-replay-golden; \
	else \
		echo "replay trigger not matched; skipping test-replay-golden"; \
	fi

replay-trigger-self-check:
	@$(MAKE) test-replay-golden-if-needed REPLAY_GOLDEN_CHANGED='internal/shared/replay/foo.go'
	@$(MAKE) test-replay-golden-if-needed REPLAY_GOLDEN_CHANGED='README.md'

test-soak:
	@$(MAKE) soak-check

soak-check: invariants-check
	@./scripts/soak-test.sh \
		--out-file "$(SOAK_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)" \
		--ws-pattern "$(SOAK_WS_PATTERN)" \
		--boundedmap-pattern "$(SOAK_BOUNDEDMAP_PATTERN)"
	@./scripts/soak-vpvr.sh \
		--out-file "$(SOAK_VPVR_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)" \
		--pattern "$(SOAK_VPVR_PATTERN)"
	@./scripts/soak-roundtrip.sh \
		--out-file "$(SOAK_ROUNDTRIP_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)"

soak-vpvr: invariants-check
	@./scripts/soak-vpvr.sh \
		--out-file "$(SOAK_VPVR_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)" \
		--pattern "$(SOAK_VPVR_PATTERN)"

soak-cold-path: invariants-check
	@chmod +x ./scripts/soak-cold-path.sh
	@./scripts/soak-cold-path.sh \
		--out-file ".context/evidence/w2-cold-path-soak.txt" \
		--go-cache "$(SOAK_GO_CACHE)"

soak-store: invariants-check
	@chmod +x ./scripts/soak-store.sh
	@./scripts/soak-store.sh \
		--out-file "$(SOAK_STORE_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)" \
		--pattern "$(SOAK_STORE_PATTERN)"

soak-roundtrip: invariants-check
	@chmod +x ./scripts/soak-roundtrip.sh
	@./scripts/soak-roundtrip.sh \
		--out-file "$(SOAK_ROUNDTRIP_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)"

soak-pipeline: invariants-check
	@chmod +x ./scripts/soak-pipeline.sh
	@./scripts/soak-pipeline.sh \
		--out-file "$(SOAK_PIPELINE_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)"

soak-ws-delivery: invariants-check
	@chmod +x ./scripts/soak-ws-delivery.sh
	@./scripts/soak-ws-delivery.sh \
		--out-file ".context/evidence/c4-ws-delivery-soak.txt" \
		--go-cache "$(SOAK_GO_CACHE)"

soak-c4-production: invariants-check
	@chmod +x ./scripts/soak-c4-production.sh
	@./scripts/soak-c4-production.sh \
		--out-file "$(SOAK_C4_OUT_FILE)" \
		--go-cache "$(SOAK_GO_CACHE)"

soak-full: soak-check soak-store soak-cold-path soak-roundtrip soak-pipeline soak-ws-delivery soak-c4-production

test-short:
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) test -short $$pkgs; else echo "no packages to test (skipping)"; fi')

test-short-changed:
	@set -euo pipefail; \
	mods="$$(./scripts/changed-go-modules.sh --auto || true)"; \
	if [ -z "$$mods" ]; then \
		echo "test-short-changed: no changed Go modules; skipping"; \
		exit 0; \
	fi; \
	status=0; \
	while IFS= read -r mod; do \
		[ -z "$$mod" ] && continue; \
		echo ">>> $$mod: $(GO) test -short"; \
		( cd "$$mod" && pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) test -short $$pkgs; else echo "no packages to test (skipping)"; fi ) || status=$$?; \
	done <<< "$$mods"; \
	exit $$status

vuln:
	@if command -v $(GOVULNCHECK) >/dev/null 2>&1; then \
		if MODULE='$(MODULE)' ./scripts/for-each-module.sh $(GOVULNCHECK) ./...; then \
			:; \
		elif [ "$(VULN_REQUIRED)" = "true" ]; then \
			echo "govulncheck failed and VULN_REQUIRED=true"; \
			exit 1; \
		else \
			echo "warning: govulncheck failed (possibly offline); skipping local vuln gate"; \
		fi; \
	elif [ "$(VULN_REQUIRED)" = "true" ]; then \
		echo "$(GOVULNCHECK) not found. Run: make install-tools"; \
		exit 1; \
	else \
		echo "warning: $(GOVULNCHECK) not found; skipping local vuln scan"; \
	fi

build:
	@set -euo pipefail; \
	mkdir -p bin; \
	built=0; \
	for app_dir in cmd/*; do \
		[ -d "$$app_dir" ] || continue; \
		if [ -f "$$app_dir/main.go" ]; then \
			app_name="$$(basename "$$app_dir")"; \
			echo "Building $$app_name from $$app_dir"; \
			$(GO) build -trimpath -ldflags "-s -w" -o "bin/$$app_name" "./$$app_dir"; \
			built=$$((built + 1)); \
		fi; \
	done; \
	if [ "$$built" -eq 0 ]; then \
		echo "No buildable apps found in cmd/* (expected main.go)."; \
		exit 1; \
	fi

run:
	@$(GO) run $(APP_CMD)

clean:
	@rm -rf ./bin ./dist ./.cache

docker-build:
	docker compose -f deploy/compose/docker-compose.yml --profile core build

up:
	@set -euo pipefail; \
	p_rep="$(PROCESSOR_REPLICAS)"; \
	if [ "$$p_rep" -lt 1 ]; then \
		echo "PROCESSOR_REPLICAS must be >= 1 (got $$p_rep)"; exit 1; \
	fi; \
	PROCESSOR_SHARD_COUNT=$(PROCESSOR_SHARD_COUNT) \
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs up --build -d \
		--scale processor=$$p_rep

down:
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs down -v --remove-orphans

up-infra:
	docker compose -f deploy/compose/docker-compose.yml --profile obs up -d nats timescale clickhouse prometheus grafana

up-core:
	@set -euo pipefail; \
	p_rep="$(PROCESSOR_REPLICAS)"; \
	if [ "$$p_rep" -lt 1 ]; then \
		echo "PROCESSOR_REPLICAS must be >= 1 (got $$p_rep)"; exit 1; \
	fi; \
	PROCESSOR_SHARD_COUNT=$(PROCESSOR_SHARD_COUNT) \
	docker compose -f deploy/compose/docker-compose.yml --profile core up --build -d \
		--scale processor=$$p_rep

smoke: shell-script-check
	@chmod +x ./scripts/smoke-compose.sh
	@./scripts/smoke-compose.sh

runtime-gate: shell-script-check
	@chmod +x ./scripts/runtime-reliability-gate.sh
	@./scripts/runtime-reliability-gate.sh --report-dir "$(RUNTIME_GATE_REPORT_DIR)"

runtime-gate-full: shell-script-check
	@chmod +x ./scripts/runtime-reliability-gate.sh
	@./scripts/runtime-reliability-gate.sh --report-dir "$(RUNTIME_GATE_REPORT_DIR)" --full

dev-scale-smoke:
	@set -euo pipefail; \
	p_rep="$${N:-$${PROCESSOR_REPLICAS:-3}}"; \
	if [ "$$p_rep" -lt 1 ]; then \
		echo "N/PROCESSOR_REPLICAS must be >= 1 (got $$p_rep)"; exit 1; \
	fi; \
	p_shards="$${PROCESSOR_SHARD_COUNT:-$$p_rep}"; \
	echo "[dev-scale-smoke] down previous stack"; \
	$(MAKE) down >/dev/null; \
	echo "[dev-scale-smoke] up core with PROCESSOR_REPLICAS=$$p_rep PROCESSOR_SHARD_COUNT=$$p_shards"; \
	PROCESSOR_SHARD_COUNT="$$p_shards" docker compose -f deploy/compose/docker-compose.yml --profile core up --build -d --scale processor="$$p_rep"; \
	echo "[dev-scale-smoke] waiting for processor replicas to become healthy"; \
	for i in $$(seq 1 90); do \
		healthy="$$(docker compose -f deploy/compose/docker-compose.yml --profile core ps | awk '/compose-processor-[0-9]+/ && /healthy/ {c++} END {print c+0}')"; \
		if [ "$$healthy" -ge "$$p_rep" ]; then break; fi; \
		sleep 2; \
	done; \
	echo ""; \
	echo "=== docker compose ps (core) ==="; \
	docker compose -f deploy/compose/docker-compose.yml --profile core ps; \
	echo ""; \
	echo "=== shard resolution evidence ==="; \
	logs="$$(docker compose -f deploy/compose/docker-compose.yml --profile core logs --since=10m --tail=500 processor)"; \
	for idx in $$(seq 1 "$$p_rep"); do \
		echo "--- processor-$$idx ---"; \
		printf '%s\n' "$$logs" | rg "^processor-$$idx  \\| .*shard resolution applied|^processor-$$idx  \\| .*processor starting" | head -n 4 || true; \
	done

ps:
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs ps

logs:
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs logs -f --tail=200

pre-commit-install:
	$(PRE_COMMIT) install --hook-type pre-commit --hook-type pre-push --hook-type commit-msg

commit-msg-check:
	@set -euo pipefail; \
	if [ -n "$(MSG_FILE)" ]; then \
		./scripts/validate-commit-msg.sh "$(MSG_FILE)"; \
	else \
		tmp="$$(mktemp)"; \
		trap 'rm -f "$$tmp"' EXIT; \
		printf '%s\n' "$(MSG)" > "$$tmp"; \
		./scripts/validate-commit-msg.sh "$$tmp"; \
	fi

commit-msg-self-check:
	@$(MAKE) commit-msg-check MSG='feat(build): sample conventional commit'
	@! $(MAKE) commit-msg-check MSG='bad message' >/dev/null 2>&1 || (echo "expected invalid message to fail" >&2; exit 1)
	@echo "commit-msg-self-check: pass/fail cases validated."

proto-tools:
	@mkdir -p "$(PROTOBUF_BIN_DIR)"
	@set -euo pipefail; \
	if [ ! -x "$(BUF)" ]; then \
		GOWORK=off GOBIN="$(PROTOBUF_BIN_DIR)" $(GO) install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION); \
	fi; \
	if [ ! -x "$(PROTOC_GEN_GO)" ]; then \
		GOWORK=off GOBIN="$(PROTOBUF_BIN_DIR)" $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION); \
	fi

proto-lint: proto-tools
	@"$(BUF)" lint proto

proto-gen: proto-tools
	@cd proto && PATH="$(PROTOBUF_BIN_DIR):$$PATH" "$(BUF)" generate

proto-gen-if-needed: proto-tools
	@set -euo pipefail; \
	if ./scripts/proto-needs-gen.sh; then \
		echo "proto-check: proto/config changes detected; running proto-gen"; \
		$(MAKE) proto-gen; \
	else \
		echo "proto-check: no proto/config changes; skipping proto-gen"; \
	fi

proto-breaking: proto-tools
	@set -euo pipefail; \
	if git ls-tree -r --name-only main -- proto | grep -qE '\.proto$$'; then \
		"$(BUF)" breaking proto --against '.git#branch=main'; \
	else \
		echo "Skipping proto-breaking: main has no proto baseline yet."; \
	fi

proto-check: proto-lint proto-breaking proto-gen-if-needed
	@set -euo pipefail; \
	if ! git diff --quiet -- internal/shared/proto/gen; then \
		echo "proto-check failed: generated protobuf artifacts are dirty."; \
		git --no-pager diff --name-only -- internal/shared/proto/gen; \
		exit 1; \
	fi

proto: proto-lint proto-gen

ci: legacy-check tidy-check fmt-check lint test-workspace-race vuln build
