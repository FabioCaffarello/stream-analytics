SHELL := /usr/bin/env bash

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck
PRE_COMMIT ?= pre-commit

GOLANGCI_LINT_VERSION ?= v2.6.0
GOVULNCHECK_VERSION ?= latest
PROTOC_GEN_GO_VERSION ?= v1.36.11
BUF_VERSION ?= v1.57.2

APP_NAME ?= server
APP_CMD ?= ./cmd/server

GO_TEST_FLAGS ?= -covermode=atomic
GO_TEST_RACE_TIMEOUT ?= 10m
GO_TEST_RACE_FLAGS ?= -race -covermode=atomic -timeout=$(GO_TEST_RACE_TIMEOUT)
INTEGRATION_TEST_PATTERN ?= Integration|E2E|Conformance
INTEGRATION_TEST_PKGS ?= ./internal/adapters/jetstream ./cmd/consumer
TEST_RACE_PKGS ?= ./internal/adapters/jetstream ./internal/shared/replay ./internal/actors/runtime ./cmd/consumer
REPLAY_GOLDEN_PKGS ?= ./internal/shared/replay ./cmd/consumer ./internal/adapters/jetstream
REPLAY_GOLDEN_PATTERN ?= TestGoldenReplay|TestReplayIngestGolden1000|TestReplayHeatmapGolden1000|TestHeatmapReplayByteStable50Runs|TestShardGolden|TestShardReplayInvariant
REPLAY_GOLDEN_TRIGGER_REGEX ?= ^(internal/shared/replay/|internal/shared/envelope/|internal/.*/sequencer|internal/core/storage/|internal/adapters/storage/|internal/adapters/jetstream/shard)
REPLAY_GOLDEN_CHANGED ?=
SOAK_OUT_FILE ?= .context/evidence/w5-soak.txt
SOAK_VPVR_OUT_FILE ?= .context/evidence/vpvr-soak.txt
SOAK_GO_CACHE ?= /tmp/go-build
SOAK_WS_PATTERN ?= TestConsumer_ConnectDisconnectCycle_(NoGoroutineLeak|HeapStable)
SOAK_BOUNDEDMAP_PATTERN ?= TestBoundedMap_(ConcurrentAccess|EvictBySizeLRU|EvictByTTL)
SOAK_VPVR_PATTERN ?= TestVPVROverloadSoakBurstDeterministicBudgets
SOAK_STORE_OUT_FILE ?= .context/evidence/s3-store-soak.txt
SOAK_STORE_PATTERN ?= TestStoreSoak_
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

.PHONY: help install-tools tools modules workspace-check tidy tidy-check fmt fmt-check vet quick ci-local contract-gates operability-gates docs-check docs-check-fast docs-check-full docs-fix check-doc-headers check-doc-links check-doc-links-changed check-truth-map check-feature-pack-links check-pack-subjects-vs-event-bus registry-check invariants-check lint test test-root test-workspace test-workspace-race test-unit test-integration test-race test-partition test-replay-golden test-replay-golden-if-needed replay-trigger-self-check test-soak soak-check soak-vpvr soak-cold-path soak-store test-short bench-hotpath vuln build run clean docker-build up down up-infra ps logs pre-commit-install commit-msg-check commit-msg-self-check proto-tools proto-lint proto-gen proto-gen-if-needed proto-breaking proto-check proto ci

help:
	@echo "Targets:"
	@echo "  make install-tools      - install golangci-lint and govulncheck"
	@echo "  make tools              - install pinned protobuf generation tools to ./bin"
	@echo "  make modules            - list modules from go.work"
	@echo "  make workspace-check    - validate all go.work modules resolve with go list"
	@echo "  make tidy               - run go mod tidy in workspace modules"
	@echo "  make tidy-check         - fail if go.mod/go.sum are not tidy"
	@echo "  make fmt                - format all Go files (gofmt)"
	@echo "  make fmt-check          - check formatting (gofmt -l)"
	@echo "  make vet                - run go vet in workspace modules"
	@echo "  make quick              - fast local loop (fmt-check + vet + invariants-check + short tests)"
	@echo "  make ci-local           - strict local chain (quick -> docs -> invariants -> unit -> integration -> replay -> proto)"
	@echo "  make contract-gates     - W6 contract gate chain (registry -> replay -> proto)"
	@echo "  make docs-check         - strict docs guardrails (alias for docs-check-full)"
	@echo "  make docs-check-fast    - lightweight docs guardrails for local loop"
	@echo "  make docs-check-full    - full strict docs guardrails"
	@echo "  make docs-fix           - print docs fix checklist based on current guardrail findings"
	@echo "  make invariants-check   - enforce domain isolation and runtime invariants checks"
	@echo "  make lint               - run golangci-lint in workspace modules"
	@echo "  make test               - alias for make test-root"
	@echo "  make test-root          - workspace-safe root test entrypoint"
	@echo "  make test-workspace     - run all workspace tests module-by-module"
	@echo "  make test-workspace-race - run module-by-module tests with -race"
	@echo "  make test-unit          - run fast short/unit-oriented workspace tests"
	@echo "  make test-integration   - run integration-focused suites in selected packages"
	@echo "  make test-race          - run targeted high-risk race-enabled suites"
	@echo "  make test-partition     - run partitioned suites (unit -> integration -> race -> soak)"
	@echo "  make test-replay-golden - run replay golden tests only (shared/replay + cmd/consumer)"
	@echo "  make test-replay-golden-if-needed - run replay golden only when changed paths match trigger regex"
	@echo "  make replay-trigger-self-check - validate replay trigger include/exclude paths"
	@echo "  make bench-hotpath      - run benchmark harness for codec/policykit hot paths"
	@echo "  make test-soak          - alias for soak-check long-running validation"
	@echo "  make soak-check         - run soak harness checks and emit evidence file"
	@echo "  make soak-vpvr          - run deterministic VPVR burst soak checks"
	@echo "  make soak-cold-path     - run cold-path commit/ack soak harness"
	@echo "  make soak-store         - run store pipeline dedup/batch soak harness"
	@echo "  make test-short         - run short tests"
	@echo "  make vuln               - run govulncheck"
	@echo "  make build              - build all binaries under cmd/* (package main)"
	@echo "  make run                - run selected app (default: server)"
	@echo "  make down               - stop full stack"
	@echo "  make up                 - start full stack (nats + server + consumer + processor + store + clickhouse + prometheus + grafana)"
	@echo "  make up-infra           - start only infrastructure services (nats + clickhouse + prometheus + grafana)"
	@echo "  make up-core            - start infra + core app services (no observability)"
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

fmt:
	@./scripts/gofmt-all.sh write

fmt-check:
	@./scripts/gofmt-all.sh check

vet:
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) vet $$pkgs; else echo "no packages to vet (skipping)"; fi')

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

lint: invariants-check
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GOLANGCI_LINT) run --config "$(CURDIR)/.golangci.yml" ./...; else echo "no packages to lint (skipping)"; fi')

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

bench-hotpath: invariants-check
	@$(GO) test -run=^$$ -bench=HotPath -benchmem ./internal/shared/codec ./internal/shared/policykit

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

test-short:
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) test -short $$pkgs; else echo "no packages to test (skipping)"; fi')

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
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs up --build -d

down:
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs down -v --remove-orphans

up-infra:
	docker compose -f deploy/compose/docker-compose.yml --profile obs up -d nats clickhouse prometheus grafana

up-core:
	docker compose -f deploy/compose/docker-compose.yml --profile core up --build -d

ps:
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs ps

logs:
	docker compose -f deploy/compose/docker-compose.yml --profile core --profile obs logs -f --tail=200

pre-commit-install:
	$(PRE_COMMIT) install --hook-type pre-commit --hook-type commit-msg

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

ci: tidy-check fmt-check lint test-workspace-race vuln build
