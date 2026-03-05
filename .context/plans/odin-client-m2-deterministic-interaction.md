---
status: completed
progress: 100
generated: 2026-03-05
title: Odin Client M2 - Deterministic Interaction Layer
owner: client-platform
workflow: PREVC
phase: V
---

# Odin Client M2 - Deterministic Interaction Layer

> Garantir que cada interação do usuário no topo/sidebar resulte em uma transição de estado previsível.

## Scope
- Estabilizar hit-testing no top bar (timeframes/presets/actions).
- Eliminar múltiplos `Set_Timeframe` por um único input.
- Tornar feedback visual de seleção explícito e consistente.

## Tasks
1. owner: `client-platform`
   target: `client/src/core/app/top_bar.odin`, `client/src/core/ui/controls.odin`
   acceptance: interação de TF determinística e sem overshoot
   verify: `make -C client check-wasm-compile`
2. owner: `client-platform`
   target: `client/src/core/app/actions.odin`
   acceptance: fila de ações com de-dup/idempotência para ações críticas
   verify: `make -C client check-core`
3. owner: `qa-automation`
   target: `tests/playwright/*`
   acceptance: teste de regressão “1 input -> 1 switch”
   verify: `npx playwright test tests/playwright/e2e -g 'timeframe'`

## Acceptance Criteria
1. `timeframe_switches_total` cresce +1 por interação válida.
2. Não há salto de TF não solicitado em cliques sequenciais.
3. Regressão coberta por teste automatizado.

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| `odin-client-m1-observability-baseline.md` | informs | done |

## Execution Status (2026-03-05)
- [x] Blocked click-through from zen compact topbar into workspace (`client/src/core/app/build_ui.odin`).
- [x] Preserved action determinism in apply cycle (`client/src/core/app/actions.odin` guard already active).
- [x] Gate `make -C client check-wasm-compile` passed.
- [x] Gate `make -C client check-core` passed.
- [x] Regression scenario `npm --prefix tests/playwright run m1:baseline` passed with:
  - `keyboard_tf_switch.timeframe_switches_delta = 1`
  - `click_tf_switch.timeframe_switches_delta = 3` for `3` clicks
  - `click_tf_switch.ui_actions_delta = 3`
  - `warnings = []`

## Risks
| Risk | Mitigation |
|------|-----------|
| Alterar geometria quebrar UX existente | Validar visualmente com screenshots baseline M1 |
