SHELL := /usr/bin/env bash

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck
PRE_COMMIT ?= pre-commit

GOLANGCI_LINT_VERSION ?= v2.6.0
GOVULNCHECK_VERSION ?= latest

APP_NAME ?= server
APP_CMD ?= ./cmd/server

GO_TEST_FLAGS ?= -race -covermode=atomic
VULN_REQUIRED ?= false
MODULE ?=

GOCACHE ?= $(CURDIR)/.cache/go-build
GOMODCACHE ?= $(CURDIR)/.cache/go-mod
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.cache/golangci-lint
export GOCACHE
export GOMODCACHE
export GOLANGCI_LINT_CACHE

MODULE_DIRS := $(shell ./scripts/list-modules.sh)

.PHONY: help install-tools modules tidy tidy-check fmt fmt-check lint test test-workspace test-short vuln build run clean docker-build docker-up docker-down up down up-infra ps logs pre-commit-install ci

help:
	@echo "Targets:"
	@echo "  make install-tools      - install golangci-lint and govulncheck"
	@echo "  make modules            - list modules from go.work"
	@echo "  make tidy               - run go mod tidy in workspace modules"
	@echo "  make tidy-check         - fail if go.mod/go.sum are not tidy"
	@echo "  make fmt                - format all Go files (gofmt)"
	@echo "  make fmt-check          - check formatting (gofmt -l)"
	@echo "  make lint               - run golangci-lint in workspace modules"
	@echo "  make test               - alias for make test-workspace"
	@echo "  make test-workspace     - run all workspace tests with race+coverage"
	@echo "  make test-short         - run short tests"
	@echo "  make vuln               - run govulncheck"
	@echo "  make build              - build all binaries under cmd/* (package main)"
	@echo "  make run                - run selected app (default: server)"
	@echo "  make docker-up          - start docker compose"
	@echo "  make docker-down        - stop docker compose"
	@echo "  make up                 - start full stack (nats + server + consumer + processor)"
	@echo "  make down               - stop full stack"
	@echo "  make up-infra           - start only infrastructure services (nats)"
	@echo "  make ps                 - list compose service status"
	@echo "  make logs               - stream compose logs"
	@echo "  make pre-commit-install - install pre-commit hooks"
	@echo "  make ci                 - tidy-check + fmt-check + lint + test + vuln + build"
	@echo ""
	@echo "Optional: MODULE=./pkg/hello-lib to target a single module"

install-tools:
	@$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@$(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

modules:
	@./scripts/list-modules.sh

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

lint:
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GOLANGCI_LINT) run --config "$(CURDIR)/.golangci.yml" ./...; else echo "no packages to lint (skipping)"; fi')

test:
	$(MAKE) test-workspace

test-workspace:
	$(call RUN_IN_MODULES,bash -lc 'pkgs="$$( $(GO) list ./... 2>/dev/null || true )"; if [ -n "$$pkgs" ]; then $(GO) test $(GO_TEST_FLAGS) $$pkgs; else echo "no packages to test (skipping)"; fi')

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

docker-up:
	docker compose -f deploy/compose/docker-compose.yml up --build -d

docker-down:
	docker compose -f deploy/compose/docker-compose.yml down --remove-orphans

up:
	docker compose -f deploy/compose/docker-compose.yml up --build -d

down:
	docker compose -f deploy/compose/docker-compose.yml down --remove-orphans

up-infra:
	docker compose -f deploy/compose/docker-compose.yml up -d nats

ps:
	docker compose -f deploy/compose/docker-compose.yml ps

logs:
	docker compose -f deploy/compose/docker-compose.yml logs -f --tail=200

pre-commit-install:
	$(PRE_COMMIT) install --hook-type pre-commit --hook-type commit-msg

ci: tidy-check fmt-check lint test vuln build
