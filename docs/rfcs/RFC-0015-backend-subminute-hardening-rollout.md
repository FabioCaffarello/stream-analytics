# RFC-0015 - Backend Sub-Minute Hardening and Rollout (1s/5s)

**Status:** Draft
**Owner:** Aggregation / Runtime
**Date:** 2026-02-27
**Last updated:** 2026-02-27
**Author:** Codex
**Relates to:** `docs/architecture/candle-aggregation.md`, `docs/architecture/stats-aggregation.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`

---

## Objetivo

Definir as evolucoes necessarias apos a mudanca da base de candles para `1s`, para operar com seguranca, previsibilidade e custo controlado no conjunto de timeframes `1s, 5s, 1m, 5m, 15m, 30m, 1h, 4h, 1d`.

## Escopo

- Fechar decisoes de semantica de janelas sub-minute (especialmente periodos sem eventos).
- Endurecer persistencia, backpressure e observabilidade para taxa maior de fechamentos.
- Definir rollout gradual com feature flags por venue/instrument.
- Consolidar gates de validacao (tests, soak, ci, docs, invariants).

## Nao-Escopo

- Alterar schema protobuf (timeframe ja e string).
- Rework de UI/client.
- Troca de banco ou arquitetura de storage.

## Contexto e Avaliacao

A base `1m` foi trocada para `1s` e a cascata agora propaga de `1s` para timeframes maiores. Isso fecha o gap funcional do client para `1s` e `5s`, mas aumenta a pressao operacional.

Avaliacao de impacto:
- Taxa de fechamento por instrumento passa de ~1 close/min para ~1 close/s.
- Para 100 instrumentos: ~100 closes/s no base candle, mais cascata para janelas superiores.
- O throughput validado do pipeline continua acima da demanda prevista, mas storage e observabilidade precisam ajuste fino para evitar custo e ruido.

## Design

### D1. Semantica de fechamento em ausencia de trade

Decisao pendente principal:
- Modo A: `event-driven` (fecha quando chega novo evento).
- Modo B: `timer/watermark-driven` (fecha por tempo, inclusive sem trade).

Direcao proposta:
- Manter `event-driven` como default nesta rodada.
- Introduzir design de watermark atras de flag para experimento controlado.
- Documentar comportamento para consumidores (o que e considerado "janela fechada" sem trade).

### D2. Storage e retencao por timeframe

- Definir retencao diferenciada: `1s/5s` com janela hot menor que `1m+`.
- Garantir batching e particionamento sem explosao de cardinalidade.
- Medir custo por timeframe no hot e no cold path.

### D3. Backpressure e protecao de hot path

- Revisar limites de filas e bounded maps no caminho de agregacao/delivery.
- Priorizar close events sob overload e manter contadores de drop por motivo.
- Definir limites operacionais por instrumento/venue.

### D4. Rollout seguro

- Feature flag para `1s/5s` por venue/instrument.
- Canario antes de habilitacao global.
- Plano de rollback objetivo (desativar sub-minute por config, sem rollback de codigo).

### D5. Observabilidade e SLOs

Adicionar/ajustar sinais por timeframe:
- `close_total`, `build_latency_ms`, `queue_depth`, `drop_total`, `persist_error_total`.
- Alertas para regressao de lag e aumento de drop/error.
- Dashboard com split por `timeframe`.

### D6. Determinismo e replay

- Expandir golden/replay para cenarios sub-minute, gaps e bursts.
- Incluir cenarios de cargas mistas (trade + liquidation + mark + funding).
- Preservar determinismo para mesma sequencia de entrada.

## Rollout

| Wave | Scope | Output | Gate |
|---|---|---|---|
| W0 | Baseline e observabilidade minima | Baseline de taxa/lag/drop com `1s` ativo em ambiente de teste | `make test MODULE=./internal/core/aggregation` + evidence em `.context/evidence/` |
| W1 | Semantica de gaps | Especificacao de comportamento sem trade + testes E2E correspondentes | `make test MODULE=./internal/core/aggregation` |
| W2 | Storage/retencao sub-minute | Politica de retencao por TF + validacao de write path | `make test` + `make ci` |
| W3 | Backpressure hardening | Limites e degradacao sob overload com metricas por motivo | `make test-workspace-race` + soak check |
| W4 | Rollout com flag/canario | Habilitacao gradual por venue/instrument e plano de rollback | compose smoke + runtime gate |
| W5 | Fechamento e promocao | Docs, runbooks, contratos e evidencias sincronizados | `make docs-check` + `make lint` + `make ci` |

## Test Plan

Base:
```bash
make test MODULE=./internal/core/aggregation
make test
make lint
make ci
```

Para mudancas em `internal/`:
```bash
make invariants-check
make test-workspace-race
```

Para mudancas documentais:
```bash
make docs-check
```

Para operacao:
```bash
make up
make smoke
make runtime-gate
```

## Acceptance

1. Backend publica candles e stats em todos os 9 timeframes sem fallback sintetico no client.
2. Semantica de gaps esta documentada, testada e nao ambigua.
3. Lag, drops e erros de persistencia de `1s/5s` ficam dentro dos budgets acordados.
4. Rollout por flag permite habilitar/desabilitar sub-minute por escopo sem downtime.
5. `make ci` verde no fechamento.

## Risks

| Risk | Mitigation |
|---|---|
| Ambiguidade de janelas sem trade | Decisao formal D1 + testes de contrato |
| Custo alto de persistencia em `1s` | Retencao diferenciada + batching + medicao por TF |
| Saturacao em bursts por instrumento | Limites bounded + prioridade de close + counters |
| Ruido operacional por alta frequencia | Dashboards/alertas por TF com thresholds revisados |
| Regressao em consumidores downstream | Rollout canario + compat checks + rollback por flag |

## Implementation Matrix

| Capability | Status | Reference |
|---|---|---|
| Timeframes `1s/5s` no dominio candle/stats | Implemented | `internal/core/aggregation/domain/candle.go`, `internal/core/aggregation/domain/stats.go` |
| Base candle `1s` com cascata flat | Implemented | `internal/core/aggregation/app/build_candle.go` |
| Lista de timeframes no builder de stats (9 TFs) | Implemented | `internal/core/aggregation/app/build_stats.go` |
| Semantica formal de gaps sem trade | Implemented | `internal/core/aggregation/app/build_candle_test.go`, `internal/core/aggregation/app/build_stats_test.go` |
| Politica de retencao/custo por timeframe | Implemented | `sql/clickhouse/migrations/0007_m2_subminute_ttl_policy.sql`, `sql/timescale/migrations/0004_m2_subminute_retention_policy.sql` |
| Rollout controlado por flag de sub-minute | Implemented (W4 slices 1-3) | `internal/shared/config/schema.go`, `internal/shared/config/loader.go`, `cmd/processor/bootstrap.go`, `cmd/processor/bootstrap_subminute_rollout_test.go`, `cmd/server/bootstrap.go`, `cmd/server/bootstrap_test.go`, `scripts/test/util/subminute-rollout-gate.sh`, `docs/operations/subminute-rollout.md`, `Makefile` |
| Pacote de observabilidade/SLO para `1s/5s` | Partially Implemented (W3 slices 1-2) | `internal/actors/aggregation/runtime/processor.go`, `internal/actors/aggregation/runtime/processor_test.go`, `internal/shared/config/schema.go`, `internal/shared/config/loader_test.go` |

## Evidence

- `internal/core/aggregation/app/build_candle.go`
- `internal/core/aggregation/app/build_stats.go`
- `internal/core/aggregation/app/build_candle_golden_test.go`
- `internal/core/aggregation/app/build_stats_test.go`
- `docs/architecture/candle-aggregation.md`
- `docs/architecture/stats-aggregation.md`
- `.context/evidence/` (artefatos de rollout por wave)
- `.context/evidence/backend-subminute-m4-read-gate-2026-02-27.md`
- `.context/evidence/backend-subminute-m4-rollout-gate-2026-02-27.md`
- `.context/evidence/subminute-rollout-gate/latest.md`
- `.context/evidence/backend-subminute-m5-closeout-2026-02-27.md`

## Changelog

- 2026-02-27:
  - RFC criado para governar hardening e rollout da mudanca sub-minute (`1s/5s`).
  - W1/M1 iniciado e fechado no modulo aggregation: semantica `event-driven` de gaps formalizada em docs e testes de contrato adicionados.
  - W2/M2 slice 1: politica de TTL diferenciada por timeframe aplicada no ClickHouse cold path (`1s/5s` 14 dias, demais 90 dias), com teste de contrato da migration e validacao em `make test MODULE=./internal/adapters/storage/clickhouse`.
  - W2/M2 slice 2: hot-path Timescale recebeu politica de retencao por timeframe via funcao de cleanup operacional (`cleanup_aggregation_hot_retention`) e teste de contrato da migration.
  - W3/M3 slice 1: telemetria de drop por motivo adicionada para rotas desabilitadas/desconfiguradas de candle/stats/orderbook no processor runtime, com validacao por testes de actor runtime.
  - W3/M3 slice 2: hardening de catch-up com skip configuravel para `trade` e `stats` (liquidation/markprice), mantendo defaults em `0` (desligado), com contadores `ingest_drop_total` por motivo e validacao em runtime/config tests.
  - W4/M4 slice 1: gate de rollout sub-minute por configuracao no processor (`processor.subminute_rollout`) aplicado em publish/persist de candle/stats (`1s`,`5s`), com allow-list opcional por `venue`/`instrument`, rollback rapido por `enabled=false` e teste dedicado.
  - W4/M4 slice 2: coerencia no read path do server com gate de rollout sub-minute aplicado em `getrange`/hot-snapshot fallback/cold readers (ClickHouse), mantendo retorno vazio para `1s/5s` bloqueados e passthrough para `1m+`.
  - W4/M4 slice 3: gate operacional consolidado com script `subminute-rollout-gate`, alvo de Makefile e runbook de operacao para canario/rollback, com evidencias em `.context/evidence/subminute-rollout-gate/latest.md`.
  - W5/M5 closeout: docs-check estabilizado (headers, links, truth-map e feature-pack guard), artefatos de evidencias de gate saneados e rodada final de validacao completa com `make test-workspace-race` + `make ci` verdes.
