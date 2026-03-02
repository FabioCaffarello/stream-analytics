# Backend Sub-Minute M4 Read/Write Gate (2026-02-27)

## Objective
Concluir rollout canario/rollback de `1s/5s` com gate por configuracao no backend, cobrindo write path (processor) e read path (server).

## What Was Added
- Processor gate (`publish/persist`):
  - `processor.subminute_rollout.enabled`
  - `processor.subminute_rollout.venues[]`
  - `processor.subminute_rollout.instruments[]`
  - arquivos: `cmd/processor/bootstrap.go`, `cmd/processor/bootstrap_subminute_rollout_test.go`
- Server gate (`getrange/hot fallback/cold readers`):
  - mesmo contrato de config (`processor.subminute_rollout`)
  - wrappers de `RangeStore`, `CandleReader`, `StatsReader`
  - arquivo: `cmd/server/bootstrap.go`
  - testes: `cmd/server/bootstrap_test.go`
- Catch-up hardening adicional (M3 slice 2):
  - `processor.catchup_skip_trade_skew_ms`
  - `processor.catchup_skip_stats_skew_ms`
  - métricas por motivo para skip de stale events
  - arquivos: `internal/actors/aggregation/runtime/processor.go`, `internal/shared/config/schema.go`

## Validation
```bash
go test ./internal/actors/aggregation/runtime ./internal/shared/config ./cmd/processor
go test ./cmd/server ./internal/interfaces/http ./internal/interfaces/ws
make test MODULE=./cmd/server
make invariants-check
make lint
```

Resultados:
- Todos os comandos acima: `PASS`.

## Known Limitation
`make docs-check` continua falhando por pendencia pre-existente e fora do escopo em:
- `docs/adrs/ADR-0020-gitops-secrets-management.md` (faltam seções `Evidence` e `Changelog`).

## Rollback Lever
Rollback operacional imediato de sub-minute:
- `processor.subminute_rollout.enabled=false`

Efeito:
- bloqueia `1s/5s` em write path e read path
- `1m+` permanece ativo
