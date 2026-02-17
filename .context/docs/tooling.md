---
type: doc
name: tooling
description: Toolchain setup, automation commands, and productivity workflows
category: tooling
generated: 2026-02-12
status: filled
docStatus: ACTIVE
last_reviewed: "2026-02-17"
scaffoldVersion: "2.0.0"
---

# Tooling & Productivity Guide

This guide documents the canonical tooling path for consistent local and CI behavior.

## Core Toolchain
- Go workspace (`go.work`) with multiple modules.
- Make as top-level task runner.
- `golangci-lint` for static analysis.
- `govulncheck` for vulnerability scanning.
- pre-commit for local guardrails.
- Docker/Docker Compose for container workflows.

## Environment Setup
1. Ensure Go is installed and on PATH.
2. Install lint/vuln tooling:
```bash
make install-tools
```
3. (Optional) Install git hooks:
```bash
make pre-commit-install
```

## High-Value Commands
- Inspect modules:
```bash
make modules
```
- Keep dependencies clean:
```bash
make tidy
make tidy-check
```
- Format and lint:
```bash
make fmt
make fmt-check
make lint
```
- Validate behavior:
```bash
make test-short
make test
```
- Security gate:
```bash
make vuln
```
- Build binaries:
```bash
make build
```

## Module-Scoped Execution
Most Make targets support `MODULE=...` for focused runs, example:
```bash
make test MODULE=./internal/core/aggregation
make lint MODULE=./cmd/consumer
```
Use this for fast iteration, then run full `make ci` before review.

## Caching and Reproducibility
The `Makefile` exports local cache directories under `.cache/`:
- `GOCACHE`
- `GOMODCACHE`
- `GOLANGCI_LINT_CACHE`

Benefits:
- Predictable local cache scope.
- Better CI/local parity in ephemeral environments.
- Easier cleanup (`make clean`).

## Container Tooling
- Build image:
```bash
make docker-build
```
- Start stack:
```bash
make docker-up
```
- Stop stack:
```bash
make docker-down
```

## Troubleshooting
- Missing linters/scanners: rerun `make install-tools`.
- Unexpected dependency diffs: run `make tidy` and re-check.
- Lint noise from generated code: confirm generated artifacts are intentional before suppressing findings.
- Vulnerability command fails offline: local runs may warn unless `VULN_REQUIRED=true`; CI enforces strict mode.
