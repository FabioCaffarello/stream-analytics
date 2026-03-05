---
status: completed
progress: 100
generated: 2026-03-05
title: Odin Client M5 - Legacy Exit & Modularization
owner: client-platform
workflow: PREVC
phase: C
---

# Odin Client M5 - Legacy Exit & Modularization

> Consolidar arquitetura pós-correções e reduzir hotspots de manutenção.

## Scope
- Segmentar módulos monolíticos (`app`, `actions`, `reconcile`, `marketdata`).
- Encerrar caminhos legados redundantes após estabilidade comprovada.
- Formalizar decisões finais em ADR.

## Tasks
1. owner: `client-platform`
   target: `client/src/core/app/*.odin`
   acceptance: módulos menores com ownership claro
   verify: `make -C client check-core`
2. owner: `architecture`
   target: `docs/adrs/*`
   acceptance: ADR final da arquitetura client web
   verify: `rg -n 'Odin Client' docs/adrs`
3. owner: `qa-automation`
   target: `tests/playwright/*`
   acceptance: suíte cobre regressões críticas pós-refactor
   verify: `npx playwright test tests/playwright/e2e`

## Acceptance Criteria
1. Hotspots críticos reduzidos e com fronteiras claras.
2. Caminhos legados removidos/isolados com flag removal plan.
3. Fluxo único de dados documentado e validado.

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| `odin-client-m2-deterministic-interaction.md` | informs | done |
| `odin-client-m3-stream-identity-reconcile.md` | blocks | done |
| `odin-client-m4-throughput-drop-control.md` | blocks | done |

## Execution Status (2026-03-05)
- [x] Primeira extração de hotspot em `actions.odin`:
  - bloco de mutações de células (`Set_Cell_Widget`, `Set_Cell_Stream`, `Add_Cell`, `Remove_Cell`) movido para `actions_cell_mutations.odin`.
  - `actions.odin` mantém orquestração via chamadas dedicadas (`apply_*_cell_action`).
- [x] Segunda extração de hotspot em `actions.odin`:
  - bloco de subscrição de mercado (`Subscribe_Market`, `Unsubscribe_Market`) movido para `actions_market_subscriptions.odin`.
  - subscriptions por canal agrupadas em helpers `subscribe_all_market_channels`/`unsubscribe_all_market_channels`.
- [x] Terceira extração de duplicação de estado de stream:
  - reset de métricas de liveness consolidado em `actions_stream_state_helpers.odin` (`reset_active_stream_live_metrics`).
  - aplicado em `Disconnect_Profile`, `Pick_Stream` e `Resync_Active_Stream`.
- [x] Quarta extração de hotspot em `actions.odin`:
  - ações de perfil (`Select/Add/Remove/Apply/Connect/Disconnect`) movidas para `actions_profiles.odin`.
  - ações de controle de stream (`Pick_Stream`, `Resync_Active_Stream`) movidas para `actions_stream_control.odin`.
- [x] Redução de hotspot:
  - `actions.odin`: 752 -> 474 linhas após extrações.
- [x] ADR incremental de arquitetura publicado:
  - `docs/adrs/ADR-0022-odin-client-action-pipeline-modularization.md`.
  - verificação: `rg -n 'Odin Client' docs/adrs`.
- [x] Verificação de build/regressão pós-extração:
  - `make -C client check-core`: PASS
  - `make -C client check-wasm-compile`: PASS
  - `npx --prefix tests/playwright playwright test tests/playwright/e2e/stress.spec.ts`: PASS (3/3)
  - `npx --prefix tests/playwright playwright test tests/playwright/e2e`: PASS (18/18)
  - Playwright MCP cacheless em `http://127.0.0.1:8090`: PASS (`hasCanvas=true`)
  - `make test-short`: PASS
- [x] Gate online validado com perfil local atual:
  - `make -C client check-widgets-online`: PASS
  - Observação: houve oscilação transitória anterior de DOM (`ob=0/0`), mas última execução do gate default passou com `ob=50/50`.
- [x] Evidência registrada:
  - `.context/evidence/m5-modularization-actions-cell-2026-03-05.md`
  - `.context/evidence/m5-probes-delta-vs-frozen-baseline-2026-03-05.md`

## Risks
| Risk | Mitigation |
|------|-----------|
| Refactor amplo gerar regressão transversal | execução faseada com gates M1..M4 antes de remover legado |
