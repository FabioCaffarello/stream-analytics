SHELL := /usr/bin/env bash

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK ?= govulncheck
PRE_COMMIT ?= pre-commit

GOLANGCI_LINT_VERSION ?= v2.6.0
GOVULNCHECK_VERSION ?= latest

APP_NAME ?= hello-app
APP_CMD ?= ./cmd/hello-app

DOCKER_IMAGE ?= market-raccoon/hello-app:dev
DOCKERFILE ?= Dockerfile

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

.PHONY: help install-tools modules tidy tidy-check fmt fmt-check lint test test-short vuln build run clean docker-build docker-up docker-down pre-commit-install ci

help:
	@echo "Targets:"
	@echo "  make install-tools      - install golangci-lint and govulncheck"
	@echo "  make modules            - list modules from go.work"
	@echo "  make tidy               - run go mod tidy in workspace modules"
	@echo "  make tidy-check         - fail if go.mod/go.sum are not tidy"
	@echo "  make fmt                - format all Go files (gofmt)"
	@echo "  make fmt-check          - check formatting (gofmt -l)"
	@echo "  make lint               - run golangci-lint in workspace modules"
	@echo "  make test               - run tests with race+coverage"
	@echo "  make test-short         - run short tests"
	@echo "  make vuln               - run govulncheck"
	@echo "  make build              - build hello-app binary"
	@echo "  make run                - run hello-app"
	@echo "  make docker-build       - build container image"
	@echo "  make docker-up          - start docker compose"
	@echo "  make docker-down        - stop docker compose"
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
	$(call RUN_IN_MODULES,$(GOLANGCI_LINT) run --config "$(CURDIR)/.golangci.yml" ./...)

test:
	$(call RUN_IN_MODULES,$(GO) test $(GO_TEST_FLAGS) ./...)

test-short:
	$(call RUN_IN_MODULES,$(GO) test -short ./...)

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
	@mkdir -p bin
	@$(GO) build -trimpath -ldflags "-s -w" -o bin/$(APP_NAME) $(APP_CMD)

run:
	@$(GO) run $(APP_CMD)

clean:
	@rm -rf ./bin ./dist ./.cache

docker-build:
	docker build -f $(DOCKERFILE) -t $(DOCKER_IMAGE) .

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down --remove-orphans

pre-commit-install:
	20 20 12 61 79 80 81 701 33 98 100 204 250 395 398 399 400PRE_COMMIT) install --hook-type pre-commit --hook-type commit-msg

ci: tidy-check fmt-check lint test vuln build
