# 00-START-HERE

**STATUS:** ACTIVE | **last_reviewed:** 2026-02-17

## What Is This Repository?

Market Raccoon is a market intelligence backend. Go, actor-based, deterministic event processing.
Three binaries: **consumer** (ingest), **processor** (aggregation), **server** (HTTP + supervision).

## Find Anything in < 2 Minutes

### I need to understand the architecture
- [Architecture Overview](../../docs/architecture/README.md)
- [System Invariants](../../docs/architecture/system-invariants.md)
- [Competitive Moat](../../docs/prd/moat.md)

### I need to decide/code a feature
- [Feature Packs](./feature-packs/) — constraint specs per domain (inputs, outputs, invariants, backpressure, replay)
- [Truth Pack](./truth-pack.md) — bridge to all authoritative sources

### I need an ADR or RFC
- [TRUTH-MAP](../../docs/architecture/TRUTH-MAP.md) — full inventory with status and code/test anchors
- [ADRs](../../docs/adrs/) (ADR-0000..0018)
- [RFCs](../../docs/rfcs/) (RFC-0001..0011)

### I need to run, test, or build
- [Development Workflow](./development-workflow.md) — make targets, branching, CI parity
- [Testing Strategy](./testing-strategy.md) — unit, domain, runtime, adapter layers
- [Tooling](./tooling.md) — Go workspace, linters, pre-commit, Docker

### I need operational runbooks
- [Observability Runbooks](../../docs/observability/runbooks/) — ingest, guardian, websocket, vpvr-overload, bus, consumer-stall
- [SLO Definitions](../../docs/observability/slo.md)
- [Operations](../../docs/operations/) — degradation, local-dev, sharding, cold-path

### I need contracts
- [Event Bus Contract](../../docs/contracts/event-bus.md)
- [Delivery WS Contract](../../docs/contracts/delivery-ws.md)
- [Subject Registry](../../docs/contracts/subject-registry.yaml)

### I need agent playbooks
- [Agent Handbook](../agents/README.md) — 7 playbooks with standard contract

### I need skills
- [Skills Index](../skills/README.md) — 10 skills, PREVC phase-mapped

### I need plans
- [Plans Index](../plans/README.md) — current plan queue
- [Workflow Status](../workflow/status.yaml) — live project phase

## Validation Gates

```bash
make docs-check        # doc headers + links + truth-map + feature-packs + registry
make invariants-check  # domain isolation + runtime invariants
make test-workspace    # full test suite
make ci                # complete pipeline
```

## Navigation Rules

- Anything an agent needs to decide/code lives in `.context/`
- Human governance docs (ADR/RFC/PRD) live in `docs/`
- Every active doc declares `STATUS: ACTIVE|LEGACY|ARCHIVED` + `last_reviewed`
- [TRUTH-MAP](../../docs/architecture/TRUTH-MAP.md) is the master inventory
