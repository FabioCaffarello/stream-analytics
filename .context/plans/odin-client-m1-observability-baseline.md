---
status: completed
progress: 100
generated: 2026-03-05
title: Odin Client M1 - Observability & Baseline
owner: client-platform
workflow: PREVC
phase: V
---

# Odin Client M1 - Observability & Baseline

> Definir baseline reprodutível para navegação e stream lifecycle antes de mudanças estruturais.

## Scope
- Cenários reprodutíveis Playwright para cold-start, online baseline, switch TF por teclado e por clique.
- Coleta de probes críticos (`tf`, `stream_count`, `ack_count`, `ui_actions`, `drop` métricas).
- Registro versionado de evidências em `.context/evidence`.

## Tasks
1. owner: `client-platform`
   target: `tests/playwright/scripts/m1-baseline-probes.mjs`
   acceptance: script gera JSON + Markdown + screenshots com cache/storage limpos
   verify: `node tests/playwright/scripts/m1-baseline-probes.mjs`
2. owner: `client-platform`
   target: `tests/playwright/package.json`
   acceptance: comando para executar baseline M1 documentado
   verify: `npm --prefix tests/playwright run m1:baseline`
3. owner: `client-platform`
   target: `.context/evidence/*`
   acceptance: artefato baseline disponível e referenciado no plano macro
   verify: `ls .context/evidence | rg 'm1-playwright-baseline'`

## Acceptance Criteria
1. Artefato baseline contém métricas antes/depois para teclado e clique de TF.
2. Execução usa cache de rede desabilitado e storage limpo.
3. Evidência fica versionada e comparável em execuções futuras.

## Execution Status
- [x] Runner M1 implementado em `tests/playwright/scripts/m1-baseline-probes.mjs`.
- [x] Comando `m1:baseline` adicionado em `tests/playwright/package.json`.
- [x] Baseline gerado em `.context/evidence/m1-playwright-baseline-2026-03-05.{json,md}`.
- [x] Screenshots M1 gerados em `.context/evidence/screenshots/m1/`.

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| `odin-client-tech-debt-evolution-plan.md` | informs | done |

## Risks
| Risk | Mitigation |
|------|-----------|
| Ambiente local sem backend WS ativo | Registrar falha no relatório sem mascarar erro |
