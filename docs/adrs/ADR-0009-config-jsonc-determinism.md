# ADR-0009 — Config & Operations: JSONC Settings + Deterministic Pipelines

**Status:** Accepted  
**Date:** 2026-02-10

## Context

We need human-editable configuration, versioning, and deterministic operational behavior for reproducibility.

## Decision

- Configuration is stored as JSONC (`config.jsonc`) with schema versioning.
- Defaults and validations live in `core/*/domain` and `core/*/app`.
- Runtime uses deterministic pipelines where possible; non-determinism must be explicit (time, randomness, IO).
- All operational stages can emit artifacts/logs suitable for debugging and replay.

## Consequences

- Easier config management and migrations.
- Improved supportability and debugging.

## Alternatives

- YAML only (rejected: JSONC + schema validation is more robust for agents).

## Amendment — 2026-02-12

### Lifecycle/Hot Reload Boundaries

`/runtime/reload` segue como mecanismo de reload controlado via restart de subsistemas quando necessario.

### Multi-Exchange Config Model (Placeholder)

Modelo de configuracao para multiplas exchanges e definido como lista de blocos por exchange (name/enabled/tickers/market_type/ws), sem implementacao nesta fase.

### Secrets Handling

Credenciais de exchange autenticada devem vir de env vars e nao de JSONC plano. Validacao fail-fast ao iniciar quando referencia existir e valor estiver ausente.
