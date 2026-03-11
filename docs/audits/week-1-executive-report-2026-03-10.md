# Market Raccoon — Week 1 Executive Report

**Date:** 2026-03-10
**Scope:** 131K LOC backend (Go) + ~30K LOC client (Odin) + 271 docs
**Sources:** 5 audits (architectural, client, end-to-end, documentation, semantic)
**Purpose:** Actionable base for Month 1 replanning

---

## Executive Summary

Market Raccoon is a **production-grade trading platform** with 12 backend bounded contexts, 10 actor subsystems, 6 exchange adapters, a 13-widget operational cockpit client, and 1,666 tests. The architecture is hexagonal, event-driven, and well-layered — overall score **8.4/10** on the client and structurally solid on the backend.

**The system works.** The critical gaps are not in logic or features — they are in:

1. **One structural inversion** (shared/contracts depends on all core domains — violates the foundation axiom)
2. **Operational fragility** (NATS single-stream, /healthz blocking on Guardian, file-based workspace store)
3. **Documentation drift** (governance docs blind to 60% of PRDs and 12 ADRs; one actively misleading client roadmap)
4. **Naming entropy** (signal/signals collision, intent semantic divergence, Session_Health misnomer)
5. **Client dual-path debt** (Entity_World + pane-based rendering coexist, doubling maintenance)

None of these block current operation. All of them compound over time. The Month 1 plan should resolve the documentation crisis, start the single structural P0, and eliminate the client dual-path debt.

---

## 1. Product Reality Today

| Dimension | Status |
|-----------|--------|
| **Data pipeline** | Production-ready. 6 exchanges, 117K evt/sec validated (C4 soak), dual-tier storage (Timescale hot / ClickHouse cold), NATS JetStream transport. |
| **Trading pipeline** | Complete chain: evidence → signal → strategy → execution → portfolio. GovernedExecutor with 5-gate fail-closed governance. SimulationEngine deterministic. Control plane FSM (4 states, 10 commands). |
| **Client** | Operational cockpit. 13 widget kinds, workspace tree (split/pane/stack), 20+ shortcuts, compare mode, 5-layer health pipeline, 1,317 tests. WASM + native targets. |
| **Observability** | 100+ Prometheus metrics, 5 Grafana dashboards, 13 alerts, 6 runbooks, SLO evaluator. |
| **Infrastructure** | Docker-based: NATS 2.10.18, TimescaleDB 2.25.1, ClickHouse 24.8.8, Prometheus, Grafana. CI 3-tier with 8 soak harnesses. |
| **MM parity** | All P0/P1 resolved (2026-02-20). P2 deferred: CBOR encoding, per-market stats flag. |

**What the product IS:** A multi-exchange market data platform with signal detection, strategy planning, execution governance, and portfolio projection — delivered through a professional-grade operational cockpit.

**What the product IS NOT yet:** Production-deployed with real capital. Execution mode defaults to simulation. Security hardening (TLS, JWT, RBAC) is in progress, not complete.

---

## 2. Architecture Reality Today

### Backend

```
shared (16 packages + 3 domain leaks)
  ← core (12 bounded contexts, hexagonal per module)
    ← adapters (6 exchanges, NATS, Timescale, ClickHouse)
      ← actors (10 supervised subsystems, Hollywood v1.0.5)
        ← interfaces (HTTP + WS)
          ← cmd (10 binaries)
```

**Strengths:** Strict hexagonal per module. DDD with aggregates, value objects, domain events. Actor supervision production-grade. Event-driven with replay-safe determinism. Storage federation (hot/cold) with idempotent upsert.

**Single critical flaw:** `shared/contracts/` (50 files) imports ALL core domains, inverting the dependency hierarchy. The foundation depends on what it should support.

### Client

```
ports → services → layers → app → platform (native | web)
                  ↘ md_common ↗
```

**Strengths:** Zero circular dependencies. Strict unidirectional state pipeline (Stream_Apply_State → Cell_Surface_View → Data_Readiness → Pane_Visual_State). Guard rails hold (10-field ceiling, 6-variant max, pure derivation). 9 stateless render strategies.

**Primary debt:** App_State god object (~1000+ fields) + dual-path rendering (Entity_World legacy + pane-based target).

### Scores

| Dimension | Backend | Client |
|-----------|---------|--------|
| Dependency direction | 8/10 (P0 inversion) | 9/10 |
| Separation of concerns | 8/10 | 7/10 (god object) |
| Domain modeling | 9/10 | 9/10 |
| Operational readiness | 9/10 | 10/10 |
| Testability | 9/10 (1,666 tests) | 9/10 (1,317 tests) |
| Scalability | 7/10 (NATS single-stream) | 8/10 |

---

## 3. Desalinhamentos Codebase vs Documentacao

| # | Conflito | Severidade | Impacto |
|---|---------|-----------|---------|
| D1 | **AUTHORITY-MAP cego a 60% dos PRDs** — lista apenas PRD-0001/0002; ignora PRD-0003 (validado), PRD-0004, PRD-0006. Usa path errado `docs/prd/` vs `docs/prds/`. Referencia `.context/` que esta sendo deletado. | CRITICO | Decisoes consultando o mapa de autoridade ignoram 3 PRDs ativos |
| D2 | **TRUTH-MAP cobre metade da arquitetura** — inventaria ADRs 0000-0023 apenas; ADRs 0024-0035 (workspace, health, orderflow) sem ancoras de verdade. | CRITICO | Metade da arquitetura client sem cadeia de verdade |
| D3 | **Client roadmap 6.8-8.0 descreve mundo que nao existe** — "55% parity", fases 6.8-8.0, RCL Golden Render. Cliente esta em S158 com 1,317 testes e 13 widgets. | CRITICO | Qualquer novo leitor recebe imagem completamente errada |
| D4 | **PRD-0006 status "Draft" mas G1 parcialmente entregue** — S147-S157 implementaram DOM, footprint, trades stores. LOC count "15,947" desatualizado (agora ~30K+). | ALTO | Documento subestima progresso real |
| D5 | **PRD-0002 sem status "Implemented"** — backend stable e Odin prontos desde S158. | ALTO | Status nao reflete realidade |
| D6 | **10 RFCs W-series (0001-0010) poluem espaco ativo** — todos implementados e superseded pelo sistema de stages. | MEDIO | Risco de consultar planos desatualizados |

---

## 4. Desalinhamentos Backend vs Client

| # | Conflito | Severidade | Evidencia |
|---|---------|-----------|-----------|
| B1 | **`signal/` vs `signals/` naming collision** — dois modulos Go com nomes quase identicos para deteccao vs composicao. Client usa `MD_Channel.Signals` sem distinguir. | ALTO | `internal/core/signal/` (13 files) vs `internal/core/signals/` (9 files) |
| B2 | **`intent` divergencia semantica** — backend = trade directive (StrategyIntentV1); client = UI pane purpose (comentario em workspace.odin:58). | MEDIO | Viola linguagem ubiqua |
| B3 | **`Session_Health` no client nao representa saude da sessao** — queries `/api/v1/session/dashboard` (delivery health), nao WS session health. | MEDIO | `services/session_health.odin` vs `delivery/domain/session.go` |
| B4 | **`StreamState` (backend anomalia) vs `Stream_State` (client transporte)** — mesmo nome, semanticas incompativeis. | BAIXO | Pilhas diferentes, sem bug runtime, mas viola linguagem ubiqua |
| B5 | **Dual freshness sources** — backend `/api/v1/freshness` e client health pipeline computam staleness independentemente com thresholds diferentes. | MEDIO | Operador ve "healthy" no Grafana; usuario ve "stale" no UI |
| B6 | **Two-pass JSON sem error propagation** — JS parse failure na primeira passagem e invisivel ao Odin. | MEDIO | Silent data loss possivel |
| B7 | **Schema version coupling** — client e server DEVEM concordar em `MaxSchemaVersion=12`. Deploy assimetrico sem negotiation protocol. | MEDIO | `workspace/domain/workspace.go` vs `app/workspace.odin` |

---

## 5. Top 10 Riscos Arquiteturais (3 meses)

| # | Risco | Prob. | Impacto | Mitigacao |
|---|-------|-------|---------|-----------|
| **R1** | **NATS single-stream para 10 domains** — back-pressure em qualquer domain afeta todos. Failure isolation inexistente. | Alta | CRITICO | Separar em 3+ streams com retention policies independentes |
| **R2** | **shared/contracts inversao de dependencia** — fundacao depende de todos os dominios. Impossivel compilar/testar shared isoladamente. | Certa | ALTO | Extrair para `internal/contracts/` com go.mod proprio |
| **R3** | **`/healthz` bloqueia no Guardian** — liveness probe com 5s timeout no actor system. Guardian sobrecarregado = restart cascata. | Media | CRITICO | Healthz = 200 OK incondicional; logica para /readyz |
| **R4** | **Workspace file-based sem locking** — multi-instance impossivel. Race condition silenciosa. | Media | ALTO | Migrar para TimescaleDB/Redis com advisory lock |
| **R5** | **Client dual-path rendering** — Entity_World + pane-based coexistem. Cada UI change requer trabalho dobrado. | Certa | ALTO | Completar remocao Entity_World |
| **R6** | **App_State god object** — ~1000+ fields. Onboarding dificil, reasoning sobre state ownership impossivel. | Certa | MEDIO | Decompor em subsystem contexts |
| **R7** | **Executor default `bootstrap_simulated`** — config deploy failure = simulacao silenciosa. | Baixa | CRITICO | Forcar explicit mode; fail startup se nao definido |
| **R8** | **Sem DLQ/retry cross-service no NATS** — intent perdido entre strategist→executor sem trace. | Media | ALTO | Consumer com ack + DLQ subject por domain |
| **R9** | **TranscodeCache sem invalidacao por schema** — deploy com novo envelope version serve formato incompativel. | Baixa | MEDIO | Version tag no cache key |
| **R10** | **Documentacao governance quebrada** — AUTHORITY-MAP e TRUTH-MAP desatualizados; decisoes baseadas em info incompleta. | Certa | ALTO | Fix imediato (< 1 dia de trabalho) |

---

## 6. Backlog Consolidado

### P0 — Blocking / Structural (resolve este mes)

| ID | Item | Origem | Esforco Est. |
|----|------|--------|-------------|
| **P0-1** | Extrair `shared/contracts/` para `internal/contracts/` (modulo separado) | Backend Arch Audit | Alto |
| **P0-2** | Mover `shared/proto/gen/` para `internal/contracts/proto/` | Backend Arch Audit | Medio |
| **P0-3** | Fix AUTHORITY-MAP (paths, PRDs 0003/0004/0006, remover .context ref) | Doc Audit | Baixo |
| **P0-4** | Expandir TRUTH-MAP (ADRs 0024-0035) | Doc Audit | Baixo |
| **P0-5** | Retirar `client-roadmap-6.8-to-8.0.md` (move para archive) | Doc Audit | Trivial |
| **P0-6** | Completar remocao Entity_World (dual-path rendering) | Client Arch Audit | Alto |

### P1 — Architecture Hygiene (proximo mes)

| ID | Item | Origem | Esforco Est. |
|----|------|--------|-------------|
| **P1-1** | Unificar `signal/` + `signals/` em modulo unico | Backend Arch + Semantic Audit | Medio |
| **P1-2** | Decompor App_State em subsystem contexts | Client Arch Audit | Medio |
| **P1-3** | Fix `/healthz` — 200 incondicional, logica para `/readyz` | E2E Audit | Baixo |
| **P1-4** | Migrar workspace store para DB com locking | E2E Audit | Medio |
| **P1-5** | Mover `shared/ownership/` e `shared/ticksize/` para core | Backend Arch Audit | Baixo |
| **P1-6** | Renomear `Session_Health` → `Delivery_Health` no client | Semantic Audit | Baixo |
| **P1-7** | Agrupar action handlers por dominio (stream/layout/widget) | Client Arch Audit | Medio |
| **P1-8** | Atualizar PRD-0002 → Implemented, PRD-0006 → Partially Implemented | Doc Audit | Trivial |
| **P1-9** | Mover RFCs 0001-0010 para `docs/rfcs/archive/` | Doc Audit | Trivial |

### P2 — Refinement (backlog)

| ID | Item | Origem | Esforco Est. |
|----|------|--------|-------------|
| P2-1 | Separar NATS em 3+ streams | E2E Audit | Alto |
| P2-2 | Implementar DLQ/retry cross-service | E2E Audit | Alto |
| P2-3 | Forcar explicit execution mode (fail on missing) | E2E Audit | Baixo |
| P2-4 | Adicionar version tag ao TranscodeCache | E2E Audit | Baixo |
| P2-5 | Split `layer_strategies.odin` em per-strategy files | Client Arch Audit | Baixo |
| P2-6 | Error channel JS→Odin para parse failures | E2E Audit | Medio |
| P2-7 | Unificar freshness sources (backend + client) | E2E Audit | Medio |
| P2-8 | Renomear `layer_marketdata.odin` → `drain_marketdata.odin` | Client Arch Audit | Trivial |
| P2-9 | Criar glossario canonico (ADR) com 12 termos + regras N1-N7 | Semantic Audit | Baixo |
| P2-10 | Remover `aggregation` → `adapters` dependency | Backend Arch Audit | Medio |
| P2-11 | Mover `workspace/infra/file_store.go` para adapters | Backend Arch Audit | Baixo |
| P2-12 | Investigar/remover `interfaces/ws/legacy_handler.go` | Backend Arch Audit | Baixo |

---

## 7. Recomendacao: Semanas 2, 3 e 4

### Semana 2 — Documentation Fix + Client Legacy Exit

**Objetivo:** Eliminar debt de documentacao e iniciar remocao da dualidade de rendering.

| Dia | Entrega |
|-----|---------|
| D1-D2 | P0-3 (AUTHORITY-MAP), P0-4 (TRUTH-MAP), P0-5 (retire client roadmap), P1-8 (PRD status), P1-9 (archive RFCs) |
| D3-D5 | P0-6 inicio: mapear todos os code paths que dependem de Entity_World; definir sequencia de remocao |

**Criterio de saida:** Governance docs refletem realidade. Plano de remocao Entity_World documentado com lista de arquivos e ordem.

### Semana 3 — Client Legacy Exit + Contracts Extraction Plan

**Objetivo:** Executar remocao Entity_World. Planejar extracao de contracts.

| Dia | Entrega |
|-----|---------|
| D1-D3 | P0-6 execucao: remover Entity_World, components.odin legacy paths, simplificar actions.odin |
| D4-D5 | P0-1/P0-2 planejamento: mapear dependencias de `shared/contracts/`, definir novo `internal/contracts/` go.mod, sequencia de migracao |

**Criterio de saida:** Client com path unico (pane-based). Todos os 1,317 testes passando. Plano de extracao contracts documentado.

### Semana 4 — Contracts Extraction + Quick Wins

**Objetivo:** Executar extracao P0. Colher quick wins P1.

| Dia | Entrega |
|-----|---------|
| D1-D3 | P0-1 execucao: extrair `shared/contracts/` para `internal/contracts/` |
| D3-D4 | P0-2: mover `shared/proto/gen/` para `internal/contracts/proto/` |
| D5 | P1-3 (healthz fix), P1-5 (ownership/ticksize move), P1-6 (Session_Health rename) — quick wins de baixo esforco |

**Criterio de saida:** `shared/` sem inversao de dependencia. Compila e testa isoladamente. Quick wins P1 entregues.

---

## Decisions Propostas

| # | Decisao | Justificativa | Alternativa Rejeitada |
|---|---------|---------------|----------------------|
| **D1** | Resolver documentation governance ANTES de qualquer code change | Decisoes tomadas sobre informacao incompleta geram retrabalho. 3 conflitos criticos em governance docs. | Ignorar docs e focar so em codigo — risco de repetir erros |
| **D2** | Entity_World removal como primeiro code change | Maior ROI: elimina dual-path, simplifica 3+ arquivos, desbloqueia App_State decomposition. | Contracts extraction primeiro — mas client debt bloqueia velocity |
| **D3** | Contracts extraction como segundo code change | Restaura integridade arquitetural do backend. Cirurgico — nao toca logica de negocio. | Adiar para mes 2 — risco de entropia acumulada |
| **D4** | NATS stream split adiado para Mes 2 | Alto esforco, requer planejamento cuidadoso de retention policies e consumer migration. | Fazer agora — mas compete com P0s de maior ROI |
| **D5** | DLQ/retry adiado para Mes 2 | Depende de decisao sobre NATS topology. Sem incidente reportado ate agora. | Fazer agora — mas sem o split, DLQ fica num single stream |

---

## Metricas Estruturais (Snapshot)

| Dimensao | Valor |
|----------|-------|
| Go modules (go.work) | 26 |
| Binarios executaveis | 10 |
| Core bounded contexts | 12 |
| Actor subsystems | 10 |
| Exchange adapters | 6 |
| Client widget kinds | 13 |
| Client tests | 1,317 |
| Backend tests | ~350 (est.) |
| Total LOC | ~161K (131K Go + 30K Odin) |
| Total docs | 271 (17% canonical, 53% historical, 15% obsolete) |
| Inversoes de dependencia | 1 critica (shared→core) |
| Boundary violations | 0 (client), 0 (backend exceto shared/contracts) |
| Circular dependencies | 0 |
| Guard rails holding | 9/9 (S158) |
| Naming conflicts cross-stack | 4 (signal/signals, intent, Session_Health, StreamState) |
| Operational risks (ALTO+) | 4 (NATS single-stream, healthz, workspace store, executor default) |

---

## Changelog

- 2026-03-10: Initial consolidated report from 5 Week 1 audits.
