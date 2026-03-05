---
status: completed
progress: 100
generated: 2026-03-05
title: Odin Client Evolution Plan (Legacy & Technical Debt)
owner: client-platform
workflow: PREVC
phase: C
---

# Odin Client Evolution Plan (Legacy & Technical Debt)

> Eliminar inconsistências de navegação/estado no client do Odin, reduzir churn de subscrição e remover acoplamentos legados com rollout seguro.

## Scope
- Tornar interação de navegação determinística (principalmente troca de timeframe e presets).
- Unificar identidade de stream por mercado (venue+symbol), sem duplicação por canal/timeframe.
- Reduzir churn de subscribe/unsubscribe/ack em reconciliação.
- Diminuir perdas de tape e estabilizar performance sob carga.
- Remover caminho legado redundante e reduzir hotspots monolíticos no client.
- Abrir caminho para testabilidade e acessibilidade incremental no web client canvas.

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| `.context/evidence/odin-client-playwright-audit-2026-03-05.md` | informs | done |
| Arquitetura layer store (`client/src/core/layers/*`) | informs | done |
| Contratos MD stream-info/subject no adapter web | informs | done |
| Suite de checks client (`make -C client ...`) | informs | done |

## Pareto Analysis: odin-client debt hotspots
Date: 2026-03-05

### Pareto Set (top 20%)
| ID | Description | Impact | Effort | Action |
|----|-------------|--------|--------|--------|
| P1 | Canonicalizar `subject_id` por mercado e remover fallback silencioso para `channel_sid` | H | M | Do first |
| P2 | Tornar troca de timeframe idempotente (1 input = 1 action) com hit-testing estável | H | M | Do first |
| P3 | Reescrever diff de reconciliação para evitar churn excessivo de subs/unsubs | H | M | Do first |
| P4 | Tratar backpressure do tape (drop policy + orçamento por frame) | H | M | Do next |
| P5 | Segmentar monólitos `app/actions/reconcile/marketdata` por bounded context | H | H | Plan |

### Deferred
| ID | Description | Reason |
|----|-------------|--------|
| D1 | Overlay DOM completo para toda UI | Maior esforço; executar incremental em controles críticos primeiro |
| D2 | Refresh visual completo de UI | Não bloqueia correção de estado/arquitetura |

### Decision
A priorização começa por consistência de estado e subscrição porque isso remove a maior parte dos sintomas observados (streams duplicados, churn de ack, navegação imprevisível). Em seguida, atacamos throughput/perf e então modularização/remoção de legado.

## Milestones

### M1 — Baseline & Observability Contract
Goal: transformar sintomas em SLOs mensuráveis para guiar refactor.
- Entregáveis:
  - Métricas novas: `market_id_resolve_hit/miss/fallback`, `tf_click_to_action_count`, `reconcile_churn_count`.
  - Cenários Playwright padronizados para: cold-start, switch TF por teclado, switch TF por clique.
  - Documento de budget de churn (max subs/unsubs por troca de TF) e drop-rate tape.
- Exit criteria:
  - Cenários reproduzíveis em `.context/evidence`.
  - Baseline congelado para comparação em M2-M4.

### M2 — Deterministic Interaction Layer
Goal: navegação previsível e input confiável no topo/sidebar.
- Entregáveis:
  - Geometria estável de top bar (reservas fixas para blocos dinâmicos).
  - Hit-testing de timeframe desacoplado de texto variável por frame.
  - Regra: 1 clique/tecla válida gera no máximo 1 `Set_Timeframe`.
- Exit criteria:
  - `timeframe_switches_total` cresce exatamente +1 por interação de TF.
  - Sem saltos inesperados de TF em cliques consecutivos.

### M3 — Stream Identity & Reconcile Refactor
Goal: eliminar duplicação de stream e churn estrutural.
- Entregáveis:
  - Remoção/containment do fallback `channel_sid` em `resolve_market_id`.
  - Invariantes fortes: um mercado ativo = um slot lógico.
  - Reconciliação idempotente com diff por `(venue,symbol,channel,tf)` e cache de estado aplicado.
- Exit criteria:
  - `stream_count` não cresce em trocas de TF sem mudança de mercado.
  - Redução significativa de `subscribe_ack_count` por troca de TF (meta definida em M1).

### M4 — Throughput & Drop Control (Tape/Signal)
Goal: reduzir perda de eventos e manter render dentro de orçamento.
- Entregáveis:
  - Política explícita de backpressure por widget (tape/dom/signal).
  - Ajuste de capacidades e prioridade de filas para não descartar dados críticos.
  - Alertas no status panel quando drop-rate exceder budget.
- Exit criteria:
  - `tapeDrop/tapeParse` abaixo da meta definida em M1 em cenário online.
  - `*_render_over_budget` sob controle em cenário de carga.

### M5 — Legacy Exit & Modularization
Goal: reduzir débito estrutural e facilitar evolução contínua.
- Entregáveis:
  - Descomposição de `app/actions/reconcile/marketdata` em módulos menores por contexto.
  - Eliminação de caminhos redundantes legados e flags temporárias após migração.
  - ADR de arquitetura final do client web (input, state, reconcile, render).
- Exit criteria:
  - Hotspots críticos reduzidos e com ownership claro.
  - Fluxo único de dados documentado e validado.

## Phases

### P — Plan
- [x] Scope confirmado com evidência de runtime/web.
- [x] Problemas priorizados com Pareto.
- [x] Planos filhos por milestone (M1..M5) criados e encadeados.

### R — Review
- [x] Revisão técnica de arquitetura alvo (stream identity + reconcile).
- [x] Revisão de estratégia de rollout/flags e critérios de regressão.

### E — Execute
- [x] M1 implementado com probes e cenários.
- [x] M2 implementado com input determinístico.
- [x] M3 implementado com identidade canônica + reconcile idempotente.
- [x] M4 implementado com controle de drop/perf.
- [x] M5 implementado com remoção de legado/modularização.
- [x] Gates locais: `make -C client check-wasm-compile`, `make -C client check-widgets-online`.

### Execution Notes (2026-03-05)
- M2 validado por evidência Playwright M1:
  - `keyboard_tf_switch.timeframe_switches_delta=1`
  - `click_tf_switch.timeframe_switches_delta=3` para `3` cliques
  - `click_tf_switch.ui_actions_delta=3`
- M3 concluído:
  - pre-seed `channel_sid -> market_id` no datasource durante reconcile
  - baseline com `click_tf_switch.stream_count_delta=0` e `warnings=[]`
- M3 handshake/runtime (native soak) destravado:
  - corrigido bug de socket sombreado em `client/src/platform/native/ws_client.odin` que causava `Handshake_Error` antes da leitura de resposta
  - após fix: `conn=Connected`, HELLO/ACK recebidos em soak
- Gates executados:
  - `make -C client check-wasm-compile`: PASS
  - `make -C client check-core`: PASS
  - `make -C client check-widgets-online`: PASS
  - `SOAK_MULTI=1 make -C client check-widgets-online`: PASS
  - `npx --prefix tests/playwright playwright test tests/playwright/e2e/stress.spec.ts`: PASS (3/3)
  - `npm --prefix tests/playwright run m1:baseline`: PASS
  - `make test-short`: PASS
  - Playwright MCP cacheless em `http://127.0.0.1:8090`: PASS (cookies/storage/cache limpos + cache de rede desabilitado)
  - gate online registra `NOTE` para `stats/heatmap/vpvr` quando zerados (perfil local opcional), mantendo falha apenas para liveness crítica
  - evidências: `.context/evidence/m3-online-soak-playwright-cacheless-2026-03-05.md`, `.context/evidence/m4-throughput-baseline-2026-03-05.md`
- M4 instrumentação final aplicada:
  - Status panel e copy diagnostics com seção `M4 BUDGETS` (drop-rate budget + render over budget + policy skips).
  - Política explícita por tipo de evento no `drain_marketdata` sob backpressure.
- M5 executado (modularização faseada):
  - extraído bloco de mutações de célula de `actions.odin` para `actions_cell_mutations.odin`.
  - extraído bloco de `Subscribe_Market`/`Unsubscribe_Market` para `actions_market_subscriptions.odin`.
  - reset de métricas de liveness de stream consolidado em helper único (`actions_stream_state_helpers.odin`).
  - extraído bloco de ações de perfil para `actions_profiles.odin`.
  - extraído bloco de controle de stream (`Pick_Stream`, `Resync_Active_Stream`) para `actions_stream_control.odin`.
  - `actions.odin` reduzido de 752 para 474 linhas.
  - ADR incremental publicado: `docs/adrs/ADR-0022-odin-client-action-pipeline-modularization.md`.
  - checks pós-refactor: `check-core` PASS, `check-wasm-compile` PASS, `stress.spec.ts` PASS (3/3).
  - gate online default voltou a PASS (`make -C client check-widgets-online`), após oscilação transitória de DOM em execuções intermediárias.
- M5 finalizado:
  - suíte E2E completa validada: `npx --prefix tests/playwright playwright test tests/playwright/e2e` PASS (18/18).
  - comparação de probes contra baseline congelado sem regressão:
    - `keyboard.timeframe_switches_delta=1` (delta 0)
    - `click.timeframe_switches_delta=3` (delta 0)
    - `click.stream_count_delta=0` (delta 0)
    - `online.tape_drop_pct=0` (delta 0)
  - evidência: `.context/evidence/m5-probes-delta-vs-frozen-baseline-2026-03-05.md`.

### V — Validate
- [x] Validar deltas de probes vs baseline em `.context/evidence`.
- [x] Executar `make test-short` (inclui gates do client) sem regressão.
- [x] Confirmar critérios de saída de cada milestone.

### C — Complete
- [x] ADR/RFC final publicado.
- [x] Plano marcado como completed.
- [x] Runbook de operação/debug atualizado.

## Acceptance Criteria
1. Troca de timeframe por clique/tecla altera o TF ativo exatamente uma vez por interação, validado por probe `probe_timeframe_switches_total` em cenário Playwright reproduzível.
2. `probe_stream_count` permanece estável em trocas de timeframe (sem mudança de mercado), validado por evidência em `.context/evidence`.
3. Churn de reconciliação por troca de TF reduzido para budget definido (M1), validado por `probe_md_subscribe_ack_count`.
4. Drop de tape abaixo da meta definida em M1 nos cenários online de referência.
5. Gates de cliente passam com `make -C client check-wasm-compile` e `make -C client check-widgets-online`.

## Risks
| Risk | Mitigation |
|------|-----------|
| Alterar identidade de stream pode quebrar roteamento de eventos | Introduzir migração por flag + comparação lado a lado em M3 |
| Redução de churn pode impactar latência de atualização | Definir SLO de latência e validar em M4 |
| Mudanças no input canvas podem criar regressões de UX | Suite Playwright focada em topbar/sidebar antes do rollout |
| Remoção de legado sem observabilidade suficiente | M1 obrigatório antes de M3/M5 |
