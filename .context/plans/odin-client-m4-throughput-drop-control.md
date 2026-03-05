---
status: completed
progress: 100
generated: 2026-03-05
title: Odin Client M4 - Throughput & Drop Control
owner: client-platform
workflow: PREVC
phase: C
---

# Odin Client M4 - Throughput & Drop Control

> Controlar perda de eventos (tape/dom/signal) e manter rendering dentro de orçamento.

## Scope
- Revisão de caps e política de descarte por widget.
- Instrumentação de drop-rate e budget de render no status/telemetria.
- Ajustes de prioridade de pipeline sob pressão.

## Tasks
1. owner: `client-platform`
   target: `client/src/core/services/*store*.odin`, `client/src/core/app/build_status.odin`
   acceptance: drop-rate visível e alertável
   verify: `make -C client check-core`
2. owner: `client-platform`
   target: `client/src/core/app/marketdata.odin`
   acceptance: políticas de backpressure explícitas por tipo de evento
   verify: `make -C client check-wasm-compile`
3. owner: `qa-automation`
   target: `tests/playwright/e2e/stress.spec.ts`
   acceptance: cenário de carga com relatório de drop
   verify: `npx playwright test tests/playwright/e2e/stress.spec.ts`

## Acceptance Criteria
1. `tapeDrop/tapeParse` abaixo do budget definido em M1.
2. `*_render_over_budget` controlado em cenários online de referência.
3. Sem regressão de liveness em stream ativa.

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| `odin-client-m1-observability-baseline.md` | informs | done |
| `odin-client-m3-stream-identity-reconcile.md` | blocks | done |

## Execution Status (2026-03-05)
- [x] Baseline de throughput/drop executado em soak online:
  - `make -C client check-widgets-online`: PASS (`conn=Connected`, `health=OK`, `drop=0`).
  - `SOAK_MULTI=1 make -C client check-widgets-online`: PASS (`streams=3`, `drop=0`).
  - Gate registra `NOTE` explícita para coberturas opcionais (`stats/heatmap/vpvr`) quando perfil local não publica esses canais.
- [x] Cenário de carga Playwright executado:
  - `npx --prefix tests/playwright playwright test tests/playwright/e2e/stress.spec.ts`: PASS (3/3).
- [x] Instrumentação explícita de alerta de drop/render budget no status panel.
  - `build_health_panel` e `copy_diagnostics_to_clipboard` agora incluem seção `M4 BUDGETS` com:
    - `drop/parse` consolidado com budget explícito (`20%`).
    - alerta para `render_over_budget`.
    - contadores de `policy_skips` (heatmap/vpvr/evidence).
- [x] Ajuste de políticas de backpressure por tipo de evento.
  - `drain_marketdata` aplica política explícita por `MD_Event_Kind`:
    - mantém eventos críticos (`trade/orderbook/candle/range/signal/stats/tape`);
    - degrada `heatmap/vpvr` quando assist está ativo;
    - descarta `evidence` em pressão crítica (`bp>=3`).
  - contadores acumulados em `bp_assist` e expostos em `active_metrics`.
- [x] Validação cacheless via Playwright MCP:
  - cookies/storage/cache limpos e `Network.setCacheDisabled(true)` antes de abrir `http://127.0.0.1:8090`.
  - canvas confirmado presente.
- [x] Evidência registrada:
  - `.context/evidence/m4-throughput-baseline-2026-03-05.md`.
- [x] Revalidação pós-modularização:
  - comparação de probes vs baseline congelado manteve `tape_drop_pct=0` e sem regressão de liveness.
  - Evidência: `.context/evidence/m5-probes-delta-vs-frozen-baseline-2026-03-05.md`.

## Risks
| Risk | Mitigation |
|------|-----------|
| Reduzir drop aumentar latência | balancear SLO throughput/latência com budget explícito |
