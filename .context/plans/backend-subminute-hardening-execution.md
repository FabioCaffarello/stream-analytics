---
status: completed
generated: 2026-02-27
updated: 2026-02-27
owner: "codex"
objective: "Executar a evolucao pos-base-1s do backend com foco em semantica de gaps, hardening operacional, rollout gradual e gates de qualidade."
scope:
  include:
    - "Semantica de fechamento para periodos sem trade"
    - "Retencao/custo por timeframe (1s, 5s, 1m+)"
    - "Backpressure e limites por instrumento/venue"
    - "Rollout canario por feature flag"
    - "Observabilidade e SLOs para sub-minute"
  exclude:
    - "Mudancas de UI/client"
    - "Mudancas de schema protobuf"
    - "Troca de banco/infra base"
references:
  - "docs/rfcs/RFC-0015-backend-subminute-hardening-rollout.md"
  - "docs/architecture/candle-aggregation.md"
  - "docs/architecture/stats-aggregation.md"
  - "docs/adrs/ADR-0013-backpressure-overload-policies.md"
  - "docs/adrs/ADR-0015-deterministic-replay-time-invariants.md"
---

# Backend Sub-Minute Hardening Execution

## Goal
Concluir a evolucao operacional apos a mudanca da base para `1s`, garantindo previsibilidade funcional, custo controlado e rollout seguro em producao.

## Scope
- Formalizar semantica de `window close` sem trade.
- Ajustar operacao de storage/retencao para `1s/5s`.
- Endurecer limites de backpressure e degradacao.
- Liberar por canario com rollback por flag.

## Dependencies
| Dependency | Type | Status |
|---|---|---|
| `docs/rfcs/RFC-0015-backend-subminute-hardening-rollout.md` | informs | done |
| `docs/adrs/ADR-0013-backpressure-overload-policies.md` | informs | done |
| `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` | informs | done |
| Feature flags de processor/runtime para rollout | blocks | done |

## Milestones

| Milestone | Deliverables | Dependencies | Gate |
|---|---|---|---|
| M1 - Gap Semantics | Contrato formal de fechamento sem trade + testes de contrato | RFC-0015 | `make test MODULE=./internal/core/aggregation` |
| M2 - Storage Cost Guardrails | Politica de retencao por TF + validacao hot/cold path | M1 | `make test` + `make ci` |
| M3 - Backpressure Hardening | Limites e degradacao com metricas por motivo | M2 | `make test-workspace-race` + soak checks |
| M4 - Canary Rollout | Feature flag por venue/instrument + rollback playbook | M3 | compose smoke + runtime gate |
| M5 - Closeout | Docs/runbooks/SLOs atualizados + evidencias consolidadas | M4 | `make docs-check` + `make lint` + `make ci` |

## Phases

### P - Plan
- [ ] Escopo de M1..M5 confirmado com owner de aggregation/runtime.
- [ ] Contratos e docs de autoridade referenciados.
- [ ] Criticos de custo/latencia definidos por timeframe.

### R - Review
- [ ] Revisao tecnica da semantica de gaps.
- [ ] Revisao de impacto em storage e delivery.
- [ ] Revisao do plano de rollout e rollback.

### E - Execute
- [x] Implementar M1 (semantica + testes).
- [x] Implementar M2 (retencao/custo + validacoes).
- [x] Implementar M3 (backpressure e thresholds).
- [x] Implementar M4 (flag, canario e rollback).
- [x] Implementar M5 (docs e evidencias finais).

### V - Validate
- [x] `make docs-check`
- [x] `make invariants-check`
- [x] `make test MODULE=./internal/core/aggregation`
- [x] `make test`
- [x] `make test-workspace-race`
- [x] `make lint`
- [x] `make ci`
- [x] `make subminute-rollout-gate`
- [x] `make shell-script-check`

### C - Complete
- [x] Evidence pack em `.context/evidence/` por milestone.
- [x] Status do plano atualizado para `completed`.
- [x] Changelog final registrado no RFC-0015.

## Acceptance Criteria
1. Contrato de semantica sem trade documentado e coberto por testes.
2. Timeframes `1s/5s` operam dentro de limites de lag/drop/error acordados.
3. Rollout canario executavel com rollback por configuracao.
4. Gates finais (`docs-check`, `lint`, `ci`) todos verdes.

## Risks
| Risk | Mitigation |
|---|---|
| Drift entre semantica real e documentada | Testes de contrato + docs-check obrigatorio |
| Custo de storage acima do esperado | Retencao por TF + medicao por wave |
| Saturacao em burst de eventos | Hard limits + degradacao progressiva + metricas |
| Rollout sem controle fino | Flag por escopo + playbook de rollback |

## Exit Condition
Plano concluido quando M1..M5 estiverem fechados com evidencias e `make ci` verde na rodada final.

## Execution Log
- 2026-02-27:
  - M1 concluido: semantica event-driven de gaps explicitada em arquitetura de candle/stats e coberta por testes de contrato no modulo de aggregation.
  - Gates executados: `make test MODULE=./internal/core/aggregation` (PASS).
  - Evidencia: `internal/core/aggregation/app/build_candle_test.go:TestBuildCandle_GapEventDriven_NoSyntheticBaseClosures`, `internal/core/aggregation/app/build_stats_test.go:TestBuildStats_GapEventDriven_NoSyntheticWindowClosures`.
  - M2 slice 1 concluido: cold-path ClickHouse com TTL por timeframe (`1s/5s` 14 dias; demais 90 dias) via migration dedicada e teste de contrato.
  - Gates executados: `make test MODULE=./internal/adapters/storage/clickhouse` (PASS), `make test MODULE=./internal/core/aggregation` (PASS).
  - M2 slice 2 concluido: hot-path Timescale com funcao de cleanup operacional por timeframe (`1s/5s` 14 dias; demais 90 dias), indexes de suporte e teste de contrato da migration.
  - Gates executados: `make test MODULE=./internal/adapters/storage/timescale` (PASS).
  - M3 slice 1 concluido: metricas de drop por motivo adicionadas no processor runtime para rotas desabilitadas/desconfiguradas (`candle_route_disabled`, `candle_route_unconfigured`, `stats_route_disabled`, `stats_route_unconfigured`, `orderbook_route_unconfigured`).
  - Gates executados: `make test MODULE=./internal/actors/aggregation/runtime` (PASS), `make test MODULE=./internal/core/aggregation` (PASS), `make test MODULE=./internal/adapters/storage/timescale` (PASS), `make test MODULE=./internal/adapters/storage/clickhouse` (PASS).
  - M3 slice 2 concluido: catch-up hardening expandido para `trade` e `stats` (liquidation/markprice) com thresholds dedicados (`processor.catchup_skip_trade_skew_ms`, `processor.catchup_skip_stats_skew_ms`), telemetria de drop por motivo (`bookdelta_catchup_skip`, `trade_catchup_skip`, `liquidation_catchup_skip`, `markprice_catchup_skip`) e testes de contrato no runtime/config.
  - Gates executados: `go test ./internal/actors/aggregation/runtime ./internal/shared/config ./cmd/processor` (PASS), `make test MODULE=./internal/actors/aggregation/runtime` (PASS), `make test MODULE=./internal/shared/config` (PASS).
  - M4 slice 1 concluido: rollout sub-minute com flag/config por escopo no processor (`processor.subminute_rollout`) para bloquear/liberar publish+persist de `1s/5s` por `venue`/`instrument` sem alterar core builders.
  - Gates executados: `go test ./internal/actors/aggregation/runtime ./internal/shared/config ./cmd/processor` (PASS), `make invariants-check` (PASS).
  - M4 slice 2 concluido: rollout sub-minute estendido para read path no server (range store + hot snapshot fallback + cold reader APIs) preservando coerencia de canario/rollback em `1s/5s`.
  - Gates executados: `go test ./cmd/server ./internal/interfaces/http ./internal/interfaces/ws` (PASS), `make test MODULE=./cmd/server` (PASS), `make invariants-check` (PASS), `make lint` (PASS).
  - M4 slice 3 concluido: gate operacional de rollout com script dedicado e runbook de canario/rollback (`scripts/test/util/subminute-rollout-gate.sh`, `docs/operations/subminute-rollout.md`, targets `make subminute-rollout-gate`/`make subminute-rollout-gate-full`).
  - Gates executados: `make subminute-rollout-gate` (PASS), `make shell-script-check` (PASS).
  - Evidencia consolidada: `.context/evidence/backend-subminute-m4-read-gate-2026-02-27.md`.
  - Evidencia complementar: `.context/evidence/backend-subminute-m4-rollout-gate-2026-02-27.md`, `.context/evidence/subminute-rollout-gate/latest.md`.
  - M5 concluido: closeout documental e operacional com limpeza dos artefatos ruidosos de gate, alinhamento de guard rails de docs (links/truth-map/feature-pack scripts) e rodada completa de validacao.
  - Gates executados: `make docs-check` (PASS), `make invariants-check` (PASS), `make test-workspace-race` (PASS), `make ci` (PASS).
  - Evidencia de fechamento: `.context/evidence/backend-subminute-m5-closeout-2026-02-27.md`.
