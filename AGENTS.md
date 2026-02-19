# AGENTS.md

## Dev environment
- Install Go toolchain compatible with workspace and tools: `make install-tools`
- Run tests: `make test` (full suite with race detector)
- Build binaries: `make build`
- Full CI pipeline: `make ci`
- Format + lint: `make fmt && make lint`

## Testing instructions
- `make test` — all modules via go.work with race detector
- `make test-short` — fast local cycle (no integration tests)
- `make test MODULE=./internal/core/aggregation` — focused module
- `make test-workspace-race` — explicit race detection pass
- Always run `make ci` before opening a PR to match CI gates

## PR instructions
- Follow Conventional Commits (enforced by pre-commit hook)
- Scopes: `marketdata`, `aggregation`, `delivery`, `insights`, `actors`, `adapters`, `interfaces`, `shared`
- Run `make docs-check` if modifying `docs/` or `.context/docs/feature-packs/`
- Run `make invariants-check` if modifying `internal/`
- Include `make ci` status in PR notes

## Repository map
- `bin/` — build outputs from `make build`
- `cmd/` — service entrypoints (`server`, `consumer`, `processor`)
- `deploy/compose/` — Docker Compose manifests for local orchestration
- `deploy/docker/` — multi-stage Dockerfiles for each service
- `deploy/nats/` — NATS server configuration (JetStream enabled)
- `deploy/configs/` — container-mounted JSONC runtime configs
- `deploy/observability/` — Prometheus rules and Grafana dashboards
- `docs/` — ADRs, RFCs, architecture docs, contracts, runbooks
- `internal/core/` — domain and application use cases by bounded context
- `internal/actors/` — actor runtime and subsystem orchestration
- `internal/adapters/` — infrastructure adapter implementations
- `internal/interfaces/` — HTTP and boundary-facing interfaces
- `internal/shared/` — shared primitives (problem, result, envelope, naming, ids)
- `.context/` — AI agent context (feature-packs, agents, skills, plans, workflow)
- `scripts/` — validation and utility scripts used by Make targets
- `proto/` — protobuf definitions and registry

## AI Context References
- Start here: `.context/docs/00-START-HERE.md`
- Documentation index: `.context/docs/README.md`
- Agent playbooks: `.context/agents/README.md`
- Truth pack: `.context/docs/truth-pack.md`
