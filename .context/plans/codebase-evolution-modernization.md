---
status: filled
progress: 0
generated: 2026-02-18
title: Codebase Evolution and Modernization Program
owner: Architecture + Core Platform
workflow: PREVC
phase: P
lastUpdated: "2026-02-18T00:00:00Z"
---

# Codebase Evolution and Modernization Program

> Plano mestre para remover legado, consolidar design patterns, fechar lacunas de implementacao planejada e definir a evolucao do Market Raccoon com gates de qualidade.

## 1) Task Snapshot

- Primary goal: reduzir risco arquitetural e operacional, eliminando caminhos legados e padronizando implementacao por contexto de dominio.
- Success signal: backlog de legado priorizado, commit chains definidas por milestone, criterios de aceite por dominio, e gates CI mapeados para cada fase.
- Key references:
  - [Truth Pack](../docs/truth-pack.md)
  - [Start Here](../docs/00-START-HERE.md)
  - [TRUTH-MAP](../../docs/architecture/TRUTH-MAP.md)
  - [Execution Sequence](../../docs/rfcs/EXECUTION-SEQUENCE.md)

## 2) Baseline (2026-02-18)

### 2.1 Evidence of legacy and drift

- Legacy CLI compatibility flag still exposed em `cmd/consumer/main.go` (`-record-path`, marcado como deprecated).
- Placeholders de storage adapter ainda ativos em runtime:
  - `internal/adapters/storage/timescale/writer.go`
  - `internal/adapters/storage/timescale/delivery_range_store.go`
- Volume Profile writer ainda depende de TODO SQL table contract:
  - `internal/adapters/storage/timescale/volume_profile_writer.go`
- Varios docs de arquitetura marcados como baseline + TODO paths (storage/orderbook/heatmap/vpvr/liquidations).
- Planos antigos com template incompleto (muitos TODOs) em `.context/plans/`, gerando ruido de governanca.

### 2.2 Strategic implications

- O repositorio esta funcional, mas com fronteira "Existing vs Planned" extensa.
- O maior risco nao e bug imediato; e acumulacao de caminhos parcialmente implementados sem cadeia unica de entrega.
- A evolucao precisa separar claramente:
  - endurecimento de arquitetura existente;
  - entrega de capacidades planejadas;
  - limpeza de legado/documentacao que atrapalha tomada de decisao.

## 3) Program Principles

1. Contract-first: nenhuma implementacao entra sem contrato em docs/ADR/RFC alinhado ao TRUTH-MAP.
2. Small safe commits: cada passo com gate e rollback simples por `git revert`.
3. Invariants over features: determinismo/replay/ack boundary prevalecem sobre throughput.
4. Bounded complexity: cada modulo precisa limite de responsabilidade (core app/domain, adapters, interfaces).
5. No hidden TODO debt: TODO sem owner, arquivo e milestone vira blocker de conclusao.

## 3.1) Program Constraints (Non-Negotiable)

- Timescale esta fora de escopo neste ciclo de modernizacao.
- Nenhuma implementacao nova de writer/read path Timescale sera desenvolvida nas waves M1-M4.
- Itens Timescale permanecem como `Planned/TODO` por decisao estrategica e devem ser apenas isolados/documentados.

## 4) Modernization Workstreams

## WS1 - Legacy Removal and Compatibility Sunset

Objetivo: remover codigo e fluxos de compatibilidade sem uso real e reduzir bifurcacao de comportamento.

Escopo inicial:
- `cmd/consumer/main.go`: remover flag `-record-path` (deprecated) e manter somente caminho canonico.
- Revisar testes com helpers "legacy equivalence" para decidir o que fica como teste de regressao e o que pode ser simplificado:
  - `internal/shared/envelope/subject_test.go`
  - `internal/actors/insights/runtime/vpvr_policy_test.go`
- Converter placeholders de adapter para implementacao real ou explicitar feature flag disabled:
  - `internal/adapters/storage/timescale/writer.go`
  - `internal/adapters/storage/timescale/delivery_range_store.go`

Definition of Done:
- nenhum caminho deprecated exposto por CLI principal;
- placeholders de runtime removidos ou claramente isolados com guardrails de build;
- cobertura de regressao preservada.

## WS2 - Architectural Boundary Hardening

Objetivo: reforcar ports/adapters e impedir acoplamento cross-domain.

Escopo inicial:
- Revisar portas e contratos entre `internal/core/*/ports` e adapters correspondentes.
- Garantir padrao unico de erro de fronteira (`*problem.Problem`) em paths novos/refatorados.
- Consolidar composition roots em `cmd/server`, `cmd/consumer`, `cmd/processor` para evitar wiring duplicado.

Definition of Done:
- contratos de porta com semantica de erro uniforme;
- sem dependencia indevida de adapters dentro de core;
- invariants-check continua verde apos cada commit chain.

## WS3 - Planned Capability Closure (Insights + Delivery + Storage Boundaries)

Objetivo: transformar "Planned/TODO" critico em capacidade entregue.

Subtracks:
- Storage plane:
  - sem implementacao Timescale neste ciclo.
  - isolar placeholders atuais para evitar falsa percepcao de readiness.
  - fechar contratos e testes de boundary (ack/commit, idempotencia) sem introduzir novo adapter Timescale.
- Insights plane:
  - fechar builders deterministas de heatmap e VPVR.
  - garantir payload budget e bounded cardinality com testes dedicados.
- Liquidations/MarkPrice:
  - pipeline dedicado com dedup forte e normalizacao canonica.
- Delivery WS:
  - fechar politica explicita de backpressure e testes e2e de range deterministico.

Definition of Done:
- cada dominio com "Implementation Matrix" atualizado com estado real e excecao explicita de Timescale fora de escopo;
- testes de aceite listados em docs existentes no codigo;
- replay golden e race tests sem regressao.

## WS4 - Design Pattern Standardization

Objetivo: reduzir variacao de estilo arquitetural e tornar evolucao previsivel.

Padroes alvo:
- Application service por caso de uso (input struct, output struct, invariants explicitas).
- Ports pequenas e orientadas a capability.
- Domain model sem dependencia de infra.
- Observability por modulo com namespace consistente e labels estaveis.
- Checklist de PR orientado a invariants, nao apenas lint/format.

Artefatos:
- guia curto de patterns em `.context/docs/` ligado ao TRUTH-MAP.
- refactors focados por modulo com limites de diff pequenos.

## WS5 - Documentation and Governance Cleanup

Objetivo: remover ruido documental e tornar o plano operacional confiavel.

Escopo:
- eliminar placeholders TODO em planos ativos de `.context/plans/` ou mover para backlog explicitamente versionado.
- manter status `ACTIVE|LEGACY|ARCHIVED` coerente por documento.
- reforcar gate `docs-check` como criterio real de merge readiness.

Definition of Done:
- planos ativos sem placeholders genericos;
- docs-check com backlog conhecido explicitamente rastreado;
- TRUTH-MAP refletindo estado real de implementacao.

## 5) Milestones and Commit Chains

| Milestone | Janela | Objetivo | Saida principal |
|---|---|---|---|
| M0 - Baseline lock | 3-5 dias | congelar inventario de legado + backlog priorizado | `docs/audits/codebase-modernization-baseline.md` |
| M1 - Legacy and boundaries | 1-2 semanas | remover compatibilidade obsoleta + endurecer fronteiras | commit chain WS1+WS2 |
| M2 - Domain closure wave 1 | 2-4 semanas | fechar orderbook/delivery e hardening de boundary de storage sem Timescale | PRs por subdominio com gates completos |
| M3 - Domain closure wave 2 | 2-4 semanas | fechar insights + liquidations/markprice + replay evidence | matriz Existing atualizada em docs |
| M4 - Hardening and closeout | 1 semana | estabilizar, medir, e institucionalizar padroes | RFC/ADR delta + runbooks + scorecard |

Commit chain rule (todas as waves):
- no maximo 2-4 arquivos criticos por commit quando houver mudanca de comportamento;
- cada commit precisa declarar invariants afetadas e rollback;
- falha de gate interrompe a cadeia.

## 6) Gates per Phase

Gates obrigatorios para toda cadeia:
- `make docs-check`
- `make invariants-check`
- `make test-short`

Gates obrigatorios para mudancas de runtime/core:
- `make test`
- `make test-workspace-race`

Gates obrigatorios para fechamento de wave:
- `make ci`

Gates condicionais:
- alterou `docs/` ou `.context/docs/feature-packs/` -> `make docs-check`
- alterou `internal/` -> `make invariants-check`

## 7) Risk Register

| Risk | Probability | Impact | Mitigation | Owner |
|---|---|---|---|---|
| Fechamento de TODOs virar big-bang refactor | High | High | commit slicing + feature flags + rollback por etapa | refactoring-specialist |
| Regressao de determinismo no replay | Medium | High | manter replay golden em todas as waves de dominio | test-writer |
| Drift entre docs e codigo durante migracao | High | Medium | docs-check + TRUTH-MAP update no mesmo PR | documentation-writer |
| Acoplamento acidental core<->adapter | Medium | High | invariants-check + revisao arquitetural obrigatoria | code-reviewer |
| Sobreposicao com plano ativo de shardability | Medium | Medium | backlog separado por prefixo de milestone e ownership claro | feature-developer |
| Postergar Timescale aumentar backlog de durabilidade | Medium | Medium | registrar excecao no baseline/scorecard e reavaliar no proximo ciclo | architecture-owner |

## 8) Execution Plan by PREVC

### Phase P - Plan (semana 1)

- consolidar baseline e backlog priorizado por severidade/impacto;
- definir owners por workstream;
- aprovar ordem das commit chains M1-M4.

Entrega:
- este plano preenchido;
- baseline documentado;
- milestones aprovadas.

### Phase R - Review (semanas 1-2)

- review arquitetural dos contratos a serem mexidos primeiro (storage/delivery/orderbook);
- review de risco para remoção de legado CLI e adapters placeholder;
- validar estrategia de rollout e rollback.

Entrega:
- decisoes arquiteturais registradas;
- checklist de merge para waves M1-M2.

### Phase E - Execute (semanas 2-8)

- executar M1-M3 em PRs pequenas, cada uma com evidencias de gate;
- manter board de TODO->Existing por dominio;
- sincronizar docs + codigo no mesmo ciclo.

Entrega:
- features planejadas priorizadas efetivamente entregues;
- reducao tangivel de legado e placeholders.

### Phase V - Validate (semanas 8-9)

- rodar `make ci` completo em baseline e em cada closeout;
- validar replay deterministico e comportamento de backpressure;
- auditar acoplamento e aderencia a patterns definidos.

Entrega:
- scorecard final com indicadores de melhoria;
- lista residual priorizada para proximo ciclo.

### Phase C - Close (semana 10)

- consolidar aprendizados e atualizar governanca;
- arquivar planos obsoletos e manter apenas queue ativa;
- publicar roadmap seguinte (Q+1) com base no residual.

Entrega:
- closeout report;
- backlog residual com donos e data-alvo.

## 9) Success Metrics

- `legacy_surface_count`: numero de flags/paths deprecated ativos nos binarios principais.
- `planned_to_existing_ratio`: percentual de itens "Planned/TODO" migrados para "Existing" nos docs canônicos.
- `invariant_gate_pass_rate`: taxa de sucesso do `make invariants-check` por PR da wave.
- `replay_stability_rate`: taxa de sucesso dos testes de replay/race por wave.
- `docs_drift_incidents`: quantidade de PRs com mismatch docs vs codigo detectado em gate.

Target de programa:
- reduzir `legacy_surface_count` em >= 70% ate M4;
- converter >= 60% dos TODOs criticos de dominios prioritarios para Existing;
- manter `invariant_gate_pass_rate` >= 95% na media das waves.

## 10) First 10 Actionable Items (ordered)

1. Criar baseline audit dedicado de modernizacao com inventario por arquivo e severidade.
2. Abrir chain M1.1 para remoção do flag deprecated em `cmd/consumer/main.go`.
3. Abrir chain M1.2 para isolamento formal dos adapters placeholder em timescale (sem implementar).
4. Definir matriz de contracts que mudam em WS2 (ports + problem model).
5. Fechar backlog de TODOs nos planos ativos `.context/plans/` (sem placeholder generico).
6. Priorizar fechamento orderbook/delivery e boundary checks de storage (wave M2, sem Timescale).
7. Priorizar fechamento heatmap/vpvr/liquidations (wave M3).
8. Instituir checklist de PR focado em invariants e boundary rules.
9. Executar rodada de validacao full (`make ci`) ao fim de cada wave.
10. Publicar closeout com metricas e proximo roadmap.

## 11) Rollback Strategy

- Nivel commit: `git revert <sha>` imediato para qualquer regressao funcional/invariant.
- Nivel wave: revert da cadeia da wave + retorno ao baseline aprovado anterior.
- Nivel programa: congelar novas entregas e executar sprint de estabilizacao se `make ci` falhar de forma sistemica.

Triggers de rollback:
- quebra de determinismo/replay;
- ACK fora de commit boundary;
- regressao de contrato de subject/envelope;
- degradacao severa de latencia ou consumo de memoria sem budget aprovado.

## 12) Follow-up Artifacts

- `docs/audits/codebase-modernization-baseline.md`
- `docs/audits/codebase-modernization-scorecard.md`
- updates em `docs/architecture/TRUTH-MAP.md`
- updates em `.context/plans/README.md` com fila ativa limpa
- registro de decisoes via MCP plan tracking
