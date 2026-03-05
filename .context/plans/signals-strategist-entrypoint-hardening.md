---
status: completed
progress: 100
generated: 2026-03-05
title: Signals/Strategist Entrypoint Hardening
owner: runtime-platform
workflow: PREVC
phase: C
---

# Signals/Strategist Entrypoint Hardening

> Definir e executar uma implementação robusta de `cmd/signals` e `cmd/strategist` com refatoração segura de bootstrap e contratos de runtime.

## Scope
- Mapear contratos dos entrypoints maduros (`server`, `consumer`, `processor`, `store`, `backfill`, `migrate`) e derivar padrão único de bootstrap.
- Eliminar crash loop de `signals/strategist` sem quebrar boundedness, monotonicidade e ownership multi-réplica.
- Definir decisão arquitetural de topologia: serviços dedicados vs subsistemas embutidos (ou híbrido com feature flag/cutover).
- Introduzir gates de validação operacional e evidências de execução (compose + logs + WS/UI).

## Dependencies
| Dependency | Type | Status |
|-----------|------|--------|
| `.context/evidence/swot-runtime-2026-03-05.md` | informs | done |
| `docs/architecture/subsystems.md` | informs | done |
| `docs/architecture/sequencing-model.md` | informs | done |
| ADR de topologia para `signals/strategist` (novo) | blocks | done |

## Milestones

### M1 — Entrypoint Contract Map
- Deliverable: matriz comparativa `main.go` + `Run()` + lifecycle de cada `cmd/*`.
- Deliverable: inventário de contratos obrigatórios (config, logger, signal handling, readiness, graceful shutdown, observabilidade).
- Deliverable: diagnóstico de lacunas (`cmd/signals`, `cmd/strategist`, e diretórios vazios `credentials-broker/executor/portfolio`).
- Gate: artefato em `.context/evidence/entrypoint-contract-map-YYYY-MM-DD.md`.
- Status: completed
- Evidence: `.context/evidence/entrypoint-contract-map-2026-03-05.md`

### M2 — ADR + Refactor Blueprint
- Deliverable: ADR com decisão de topologia:
- Opção A: manter `signal`/`signals` embutidos em `processor/server` e remover serviços dedicados do compose.
- Opção B: extrair para binários dedicados (`cmd/signals`, `cmd/strategist`) com isolamento operacional.
- Opção C: híbrido (dual-run com flag + cutover progressivo).
- Deliverable: plano de refatoração por fronteira de módulo (apenas estrutura, sem mudança comportamental misturada).
- Gate: ADR revisada e aprovada.
- Status: completed
- Evidence: `docs/adrs/ADR-0021-signals-strategist-dedicated-topology-cutover.md`
- Evidence draft: `.context/evidence/adr-input-signals-strategist-topology-2026-03-05.md`

### M3 — Bootstrap Framework + Implementação Inicial
- Deliverable: skeleton robusto para `cmd/signals` e `cmd/strategist`:
- `main.go` com `bootstrap.LoadAndValidate`, overrides, logger e `Run(ctx, cfg, configPath?)`.
- `bootstrap.go` com composição explícita (engine, guardian, envelopes, publisher, HTTP server, shutdown determinístico).
- health/readiness e logs estruturados equivalentes aos entrypoints maduros.
- Deliverable: normalização de config file path e mounts (resolver estado atual de `deploy/configs/signals.jsonc` e `strategist.jsonc`).
- Gate: serviços sobem sem restart loop em compose.
- Status: completed
- Evidence: `.context/evidence/m3-runtime-validation-2026-03-05.md`

### M4 — Runtime Cutover + Hardening
- Deliverable: habilitação controlada (feature flags) para `signals/strategist` na topologia escolhida.
- Deliverable: testes de regressão focados em ownership/monotonicidade/dedup/rate-limit para ambos.
- Deliverable: atualização de runbooks e guardrails (`smoke`/runtime gate com falha em crash loop).
- Gate: validação fim-a-fim com `PROCESSOR_REPLICAS=2` e navegação client/WS.
- Status: completed
- Evidence: `scripts/test/util/smoke-compose.sh` agora falha em `Restarting` no profile `core`
- Evidence: `.context/evidence/m3-runtime-validation-2026-03-05.md`
- Evidence: `.context/evidence/m4-cutover-hardening-2026-03-05.md`
- Evidence: `docs/operations/signals-strategist-cutover.md`

## Phases

### P — Plan
- [x] Escopo de topologia confirmado como problema central a decidir em ADR.
- [x] Contratos de runtime dos entrypoints maduros identificados como base da análise.
- [x] Dependências ADR/RFC registradas.

### R — Review
- [x] Revisão de arquitetura e fronteiras de refatoração (sem misturar mudança comportamental).
- [x] Validação de contratos de bus/event type (`signal.event`, `signal.composite`) e ownership cross-subsystem.

### E — Execute
- [x] Implementar M1 (contract map + lacunas + riscos).
- [x] Implementar M2 (ADR + blueprint por módulo).
- [x] Implementar M3 (bootstrap robusto para `cmd/signals` e `cmd/strategist`).
- [x] Implementar M4 (cutover com flags e hardening).
- [x] Guardrail de smoke iniciado (falha em `Restarting` para serviços core).
- [x] Testes passam: `make fmt-check`, `make lint`, `make test-short`, `make smoke`.

### V — Validate
- [x] Gate runtime: `make up PROCESSOR_REPLICAS=2 && make smoke`.
- [x] Verificar ausência de restart loop: `make ps` (signals/strategist `Up ... (healthy)`).
- [x] Verificar cliente/WS em `:8090` (WASM + `/api/v1/markets` + `ws://127.0.0.1:8090/ws`).
- [x] Evidências registradas em `.context/evidence/`.

### C — Complete
- [x] Docs atualizados (`docs/architecture/subsystems.md`, runbooks, ADR).
- [x] Plano atualizado para `completed` com links de evidência.

## Acceptance Criteria
1. `cmd/signals` e `cmd/strategist` deixam de reiniciar em loop e passam readiness em compose (`make up PROCESSOR_REPLICAS=2`, `make ps`, `make smoke`).
2. Invariantes de ordenação/ownership permanecem válidos sob 2 réplicas (testes de regressão + logs sem anomalias críticas novas).
3. Topologia final fica documentada e rastreável em ADR + evidências operacionais.

## Risks
| Risk | Mitigation |
|------|-----------|
| Duplicação de responsabilidades entre `processor/server` e novos binários | Decidir topologia em ADR antes de implementar cutover |
| Refatoração estrutural alterar comportamento de negócio | Separar commits de estrutura vs comportamento; validar a cada etapa |
| Regressões de monotonicidade/ownership multi-réplica | Testes dedicados por `(venue,instrument,seq)` + gate com `PROCESSOR_REPLICAS=2` |
| Config/mount inconsistente para `signals/strategist` | Normalizar arquivos de config e validar compose no M3 |
