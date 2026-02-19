# Codebase Modernization Baseline

**STATUS:** ACTIVE | **last_reviewed:** 2026-02-18

## 1) Objective

Estabelecer baseline executavel para evolucao da codebase, com backlog priorizado por impacto e risco, alinhado ao plano:

- `/Volumes/OWC Express 1M2/Develop/market-raccoon/.context/plans/codebase-evolution-modernization.md`

## 2) Strategic Constraint

- Timescale nao sera implementado neste ciclo de modernizacao.
- Todos os itens `internal/adapters/storage/timescale/*` ficam fora de escopo de entrega funcional.
- O foco e remover legado, endurecer boundaries e fechar gaps de dominio sem criar novo path Timescale.

## 3) Baseline Evidence (Current State)

Sinais objetivos levantados no repositorio:

- Flag deprecated ainda exposta em `cmd/consumer/main.go` (`-record-path`).
- Placeholders em storage adapter:
  - `internal/adapters/storage/timescale/writer.go`
  - `internal/adapters/storage/timescale/delivery_range_store.go`
  - `internal/adapters/storage/timescale/volume_profile_writer.go`
- Alta quantidade de TODOs documentais em arquitetura e planos de `.context/plans/`.
- Lacunas planejadas relevantes em:
  - `docs/architecture/storage.md`
  - `docs/architecture/orderbook.md`
  - `docs/architecture/volume-profiles.md`
  - `docs/architecture/liquidations-markprice.md`
  - `docs/contracts/delivery-ws.md`

## 4) Prioritized Backlog

| Priority | Stream | Problem | Scope | Expected outcome |
|---|---|---|---|---|
| P0 | Legacy removal | CLI deprecated flag exposto | `cmd/consumer/main.go` + testes de CLI | superfice legado reduzida sem regressao |
| P0 | Governance cleanup | Planos ativos com placeholders genericos | `.context/plans/*.md` (ativos) | backlog rastreavel, sem ruido de template |
| P0 | Boundary hardening | Acoplamento potencial core/adapters em evolucoes futuras | `internal/core/*/ports`, `internal/adapters/*` | contratos estaveis e revisao segura |
| P1 | Delivery closure | Lacunas de backpressure/range deterministico em WS | `internal/core/delivery/*`, `internal/interfaces/ws/*`, `docs/contracts/delivery-ws.md` | contrato e comportamento consistentes |
| P1 | Insights closure | Gaps em heatmap/VPVR e liquidations/markprice | `internal/core/insights/*`, `internal/core/marketdata/*`, docs de arquitetura | maior cobertura de dominio sem drift |
| P1 | Storage boundary only | Necessidade de clareza sem implementar Timescale | `docs/architecture/storage.md`, `internal/adapters/storage/timescale/*` | placeholders isolados + escopo explicito |
| P2 | Pattern standardization | Variacao de design patterns entre modulos | `internal/core/*`, `.context/docs/*` | base arquitetural previsivel para novas features |

## 5) Explicit Out of Scope (This Cycle)

- Implementacao de writer/read path Timescale.
- Criacao de schema SQL para Timescale.
- Ativacao de fluxo produtivo dependente de Timescale.

## 6) Execution Sequence (Immediate)

1. M1.1: remover caminho deprecated de CLI e atualizar testes.
2. M1.2: isolar placeholders Timescale (sem implementacao), com documentacao de excecao.
3. M1.3: limpar planos ativos com placeholders genericos em `.context/plans/`.
4. M2: fechar delivery/orderbook e boundary checks com gates completos.
5. M3: fechar insights e liquidations/markprice com replay/race green.

## 7) Go/No-Go Gates for Each Wave

- `make docs-check`
- `make invariants-check`
- `make test-short-changed`
- Mudou runtime/core: `make test` + `make test-workspace-race`
- Fechamento de wave: `make ci`

## 8) Success Criteria for Baseline Phase

- Baseline aprovado e versionado.
- Backlog P0/P1/P2 com escopo e arquivos definidos.
- Restricao "sem Timescale" registrada no plano e no audit baseline.
- Sequencia de execucao M1-M3 pronta para abertura de PRs pequenas.
