---
status: filled
generated: 2026-02-19
updated: 2026-02-19
owner: "codex"
objective: "Elevar o backend ao mais alto grau de maturidade operacional e arquitetural e só liberar o início do client Odin após fechamento dos gaps C3 e das capabilities avançadas antes tratadas como non-goals de Odin v0."
scope:
  include:
    - "Hardening do WS delivery para slow-client disconnect por threshold"
    - "Cold-path read ports (SELECT) com contratos e testes"
    - "Implementação de cmd/backfill (C3) com fixtures válidas"
    - "Implementação de gap detector CLI (C3) com exit-code semântico"
    - "Gates contínuos de compose smoke e evidência de soak"
    - "Rastreabilidade via PREVC + artefatos em .context/evidence"
    - "Candle aggregation (OHLCV) com pipeline backend e contratos de entrega"
    - "Stats aggregation por timeframe (liq/funding/markprice)"
    - "Heatmap delivery pipeline (writers, wiring de delivery e storage)"
    - "Fonte durável de getrange em Timescale para histórico além de buffer"
    - "Pipeline standalone de funding rate com subject próprio e contrato explícito"
  exclude:
    - "Novos venues além dos 5 já suportados neste ciclo"
    - "Mudanças de UI/UX do client Odin"
    - "Escopo mobile/web fora do backend"
    - "Replatform total de storage sem vínculo com os gaps acima"
references:
  - "docs/prd/PRD-0002-backend-stable-and-odin-ready.md"
  - "docs/contracts/delivery-ws.md"
  - "docs/contracts/event-bus.md"
  - "docs/adrs/ADR-0006-storage-hot-vs-cold.md"
  - "docs/adrs/ADR-0013-backpressure-overload-policies.md"
  - "docs/operations/cold-path-runbook.md"
  - "docs/architecture/system-invariants.md"
---

# Odin v0 Capability Maturity Plan

## Goal
Consolidar o backend em um estado "production-ready" com foco em maturidade real: previsibilidade operacional, contratos estáveis, testes de regressão orientados a risco e governança de evidência.

## Current State Baseline (2026-02-19)
1. Implementado sem gap funcional: 5-exchange consumer, auth/rate-limit, orderbook, VPVR, cross-venue signals, markprice/liquidation, cold-path writer.
2. Implementado com gap de maturidade: WS delivery sem disconnect por slow-client threshold.
3. Gap C3 aberto: cold-path read ports (SELECT), `cmd/backfill` (stub), gap detector (missing).
4. Governança: compose já implementado; precisa gate de smoke/soak contínuo e rastreável.
5. Capabilities avançadas pendentes para launch gate robusto: candle aggregation, stats aggregation, heatmap delivery, getrange durável e funding standalone.

## Launch Policy
1. `Odin client start = BLOCKED` até conclusão de todos os milestones deste plano.
2. Os antigos non-goals de Odin v0 passam a ser `mandatory pre-launch capabilities`.
3. Somente após sign-off final de robustez (arquitetura, testes, observabilidade e operação) o backend é elegível para início do client.

## Maturity Principles (Architecture-First)
1. Nenhuma entrega quebra invariantes de envelope/subject/versioning.
2. Contrato WS prioriza segurança operacional sobre completude funcional (drop + disconnect controlado).
3. Cold-path mantém semântica ack-on-commit, com leitura tratada como porta explícita de domínio.
4. Toda capability evolui com paridade: código + teste + observabilidade + runbook.
5. Todo fechamento de milestone exige evidência objetiva e reproduzível.
6. Critério de lançamento do Odin é de robustez máxima, não apenas de viabilidade funcional mínima.

## Milestone Chain

| Milestone | Objetivo | Dependências | Exit Criteria | Evidências |
|---|---|---|---|---|
| M0 — Baseline Lock | Congelar baseline real de capabilities e non-goals | — | Matriz de capabilities validada contra PRD-0002 | Atualização deste plano + checklist de baseline |
| M1 — Delivery Hardening | Implementar slow-client disconnect threshold com telemetria | M0 | FR-2.5/FR-2.6 fortalecidos + testes de threshold verdes | testes WS + métrica/alerta `ws_drops_total` |
| M2 — Cold-Path Read Ports | Introduzir portas de leitura (SELECT) sem romper boundaries | M0 | Read ports definidos, adapters implementados, testes de contrato verdes | testes de adapter + integração roundtrip |
| M3 — C3 Tooling Operacional | Entregar `cmd/backfill` e gap detector CLI | M2 | FR-5.3 e FR-5.4 verdes com fixtures reais | binários + testes + exemplos CLI |
| M4 — Runtime Reliability Gate | Tornar smoke/soak gate contínuo e auditável | M1, M2, M3 | compose smoke e soak com evidência atualizada | `.context/evidence/*` + comandos make |
| M5 — Core Maturity Sign-off | Consolidar aderência arquitetural e readiness operacional do core já implementado | M4 | `make ci` + `make test-workspace-race` + docs/runbooks alinhados | pacote final de validação do core |
| M6 — Candle Aggregation Production | Entregar pipeline backend de OHLCV com contratos e persistência definidos | M5 | testes de candle + contrato de entrega + budget de latência aprovados | evidências de teste e runbook de operação |
| M7 — Stats Aggregation Production | Entregar agregações por timeframe para liq/funding/markprice | M6 | testes por TF + consistência cross-source + observabilidade pronta | evidências de regressão e SLO por stream |
| M8 — Heatmap Delivery Production | Entregar heatmap fim-a-fim (writer, delivery, storage e contrato) | M7 | pipeline heatmap ativo + testes e contrato WS verdes | evidências de carga, queda e recuperação |
| M9 — Durable History & Funding Standalone | Tornar `getrange` durável em Timescale e criar pipeline standalone de funding rate | M8 | consultas históricas fora de buffer + subject funding dedicado com contrato e testes | evidências de roundtrip histórico + contrato funding |
| M10 — Odin Launch Authorization | Autorizar início do client Odin com backend em maturidade máxima | M9 | todos os milestones M0..M9 fechados sem risco crítico aberto | ata de sign-off técnico e operacional |

## PREVC Execution Plan

### P — Plan
1. Confirmar baseline factual das capabilities e gaps remanescentes (somente itens ainda não fechados no PRD-0002).
2. Mapear cada gap para bounded context e owner técnico:
   - delivery/runtime: slow-client threshold.
   - core/aggregation + adapters/storage: read ports.
   - cmd/backfill e tooling: C3.
3. Definir matriz de aceite por milestone: comando, artefato, owner, risco.
4. Promover explicitamente os antigos non-goals para backlog mandatório pré-launch.
5. Definir contratos-alvo para candle, stats, heatmap, getrange durável e funding standalone.

Entregável P:
- backlog priorizado M0→M10
- critérios binários de aceite por milestone
- mapa de dependências e riscos

### R — Review
1. Revisar design das mudanças de delivery sob ADR-0013 (backpressure e overload).
2. Revisar desenho de read ports sob ADR-0006 (hot vs cold boundaries).
3. Revisar UX/semântica de CLI para backfill/gap detector (saída, códigos de erro, idempotência).
4. Revisar impacto de observabilidade e runbook para operação.
5. Revisar desenho de capacidade e storage para suportar histórico durável e novos streams sem quebrar budgets.

Entregável R:
- decisões de design mínimas aprovadas
- alternativas rejeitadas registradas
- escopo final sem ambiguidades

### E — Execute
1. M1: implementar disconnect por threshold configurável (ex.: drops acumulados por sessão em janela), métricas e testes de regressão.
2. M2: criar read ports no domínio e adapters de SELECT para cold-path com testes de contrato.
3. M3: implementar `cmd/backfill` para geração JSONL válida + gap detector CLI com exit non-zero quando houver gaps.
4. M4: consolidar smoke/soak como gate recorrente, com coleta de evidência padronizada.
5. M6: implementar candle aggregation backend com contratos e testes.
6. M7: implementar stats aggregation por timeframe (liq/funding/markprice).
7. M8: implementar heatmap delivery pipeline fim-a-fim.
8. M9: implementar getrange durável (Timescale) e funding rate standalone.
9. Atualizar docs e runbooks somente quando mudança factual for entregue.

Entregável E:
- features/gaps fechados por milestone
- evidência técnica por item
- commits convencionais por scope

### V — Validate
Ordem obrigatória de validação:
1. `make fmt && make lint`
2. `make test-short`
3. `make test MODULE=./internal/core/aggregation`
4. `make test-workspace-race`
5. `make docs-check` (obrigatório se docs/.context forem alterados)
6. `make invariants-check` (obrigatório se `internal/` for alterado)
7. `make ci`
8. suites específicas das novas capabilities (candle/stats/heatmap/getrange/funding), com evidência versionada

Regra:
- qualquer falha interrompe avanço de milestone.
- aplicar patch mínimo corretivo e reexecutar desde o último gate falho.

Entregável V:
- validação integral verde
- evidência anexada em `.context/evidence/`

### C — Complete
1. Consolidar relatório final de maturidade por capability:
   - estado inicial
   - mudança aplicada
   - estado final
   - evidência
2. Atualizar plano para `completed` quando todos os exit criteria de M0..M10 estiverem fechados.
3. Registrar riscos residuais e backlog pós-launch Odin (itens além da maturidade máxima definida neste plano).

Entregável C:
- plano encerrado com rastreabilidade completa
- pacote de handoff para manutenção/operação

## Work Packages by Capability Gap

| Gap | Pacote de trabalho | Testes mínimos | Risco principal | Mitigação |
|---|---|---|---|---|
| Slow-client threshold ausente | Política de disconnect por limiar + métricas + alerta | session/backpressure tests + contract tests | desconexões agressivas indevidas | threshold configurável + soak direcionado |
| Read ports (SELECT) ausentes | Portas no domínio + adapters CH/TS + query constraints | contract tests + integração de leitura | vazamento de infraestrutura para core | interfaces explícitas + invariants-check |
| `cmd/backfill` stub | Implementar pipeline de coleta/normalização/JSONL | `TestBackfill_ProducesValidFixture` | fixtures inválidas/inconsistentes | schema validation + golden fixtures |
| Gap detector missing | CLI de detecção e exit semantics | `TestGapDetector_ReturnsGaps` | falso positivo/negativo | dataset canônico + thresholds documentados |
| Smoke gate contínuo | Pipeline recorrente de smoke + soak evidence | `make up-core`, `make smoke`, `make soak-pipeline` | flakiness operacional | timeouts explícitos + retries controlados |
| Candle aggregation não entregue | Pipeline OHLCV backend + contratos de entrega | suites de candle + contrato WS | inconsistência TF e drift de OHLCV | invariantes de timeframe + golden datasets |
| Stats aggregation não entregue | Agregações de liq/funding/markprice por TF | suites stats por TF + regressão | discrepância entre fontes e janelas | normalização temporal e validação cruzada |
| Heatmap pipeline não entregue | Writers + storage + delivery contract | suites heatmap + soak específico | pressão de memória e latência | políticas de backpressure + limites de cardinalidade |
| Getrange durável ausente | Porta de leitura histórica em Timescale | roundtrip histórico + integração | query lenta e inconsistência temporal | índices, limites e budget por consulta |
| Funding standalone ausente | Subject e pipeline dedicados de funding | testes de parser, contrato e delivery | duplicidade com fluxo embutido atual | dedup por chave canônica e contrato único |

## Agent Lineup
- `feature-developer`: implementação de M1, M2, M3.
- `feature-developer`: implementação de M1..M3 e M6..M9.
- `test-writer`: reforço de testes de contrato, integração e soak.
- `code-reviewer`: revisão de regressão, boundaries e compatibilidade.
- `performance-optimizer`: budgets de latência/memória em M4..M10.
- `documentation-writer`: PRD/docs/runbooks e evidências de fechamento em todas as waves.

## Commit Strategy (Conventional + Scope)
1. `feat(delivery): enforce slow-client disconnect threshold`
2. `feat(aggregation): add cold-path read ports and adapters`
3. `feat(adapters): implement backfill binary and gap detector cli`
4. `chore(shared): enforce smoke and soak evidence gates`
5. `feat(aggregation): implement candle aggregation pipeline`
6. `feat(insights): implement stats aggregation by timeframe`
7. `feat(insights): implement heatmap delivery pipeline`
8. `feat(interfaces): add durable getrange and standalone funding stream`
9. `docs(shared): finalize full maturity evidence and launch sign-off`

## Risks and Controls
| Risk | Impact | Likelihood | Control |
|---|---|---|---|
| Scope creep fora da matriz de maturidade | Alto | Médio | freeze de escopo no M0 + review obrigatório no R |
| Flake em soak/smoke | Médio | Médio | critérios de retry/timebox + evidência versionada |
| Drift entre docs e código | Médio | Médio | docs-check + atualização factual por milestone |
| Acoplamento indevido core/adapters | Alto | Baixo | invariants-check + code review orientado a boundaries |
| Complexidade cumulativa das novas capabilities | Alto | Médio | milestones independentes com gates binários e rollback por wave |
| Regressão de performance em histórico e heatmap | Alto | Médio | budgets explícitos + otimização orientada por métrica |

## Exit Condition
Este plano conclui somente quando todos os gaps reais listados no estado de 2026-02-19, incluindo candle aggregation, stats aggregation, heatmap delivery, getrange durável e funding standalone, estiverem fechados com evidência verificável, sem regressão dos invariantes arquiteturais e com autorização formal para início do client Odin.

## Execution Log
- 2026-02-19: `M0` concluído (baseline lock) com evidência em `.context/evidence/odin-m0-baseline-lock-2026-02-19.md`.
- 2026-02-19: `M1` concluído (slow-client threshold disconnect) com evidência em `.context/evidence/odin-m1-slow-client-threshold-2026-02-19.md`.
- 2026-02-19: `M2` concluído (cold-path read ports com contrato de snapshot reader) com evidência em `.context/evidence/odin-m2-cold-read-ports-2026-02-19.md`.
- 2026-02-19: `M3` concluído (tooling C3: `cmd/backfill` + gap detector CLI) com evidência em `.context/evidence/odin-m3-c3-tooling-2026-02-19.md`.
- 2026-02-19: `M4` concluído (runtime reliability gate contínuo e auditável) com evidências em `.context/evidence/runtime-gate/latest.md` e `.context/evidence/odin-m4-runtime-reliability-2026-02-19.md`.
- 2026-02-19: `M5` concluído (core maturity sign-off) com evidência em `.context/evidence/odin-m5-core-maturity-signoff-2026-02-19.md`.
- 2026-02-19: `M6` concluído (candle aggregation production) com toggles de runtime respeitando `processor.candle.enabled`/`processor.stats.enabled`, contrato WS validado e budget de latência comprovado em `.context/evidence/odin-m6-candle-aggregation-production-2026-02-19.md`.
- 2026-02-19: `M7` concluído (stats aggregation production) com cobertura por timeframe, consistência cross-source, benchmark E2E `markprice->stats` dentro do budget e contrato/documentação alinhados em `.context/evidence/odin-m7-stats-aggregation-production-2026-02-19.md`.
- 2026-02-19: `M8` concluído (heatmap delivery production) com snapshot stream estável fim-a-fim (runtime + cold-path store + WS contract), migração ClickHouse dedicada e evidência consolidada em `.context/evidence/odin-m8-heatmap-delivery-production-2026-02-19.md`.
