---
status: filled
generated: 2026-02-13
agents:
  - type: "architect-specialist"
    role: "Definir taxonomia canônica e limites de bounded context para subjects"
  - type: "documentation-writer"
    role: "Atualizar contratos e feature-packs com linguagem consistente"
  - type: "code-reviewer"
    role: "Verificar inconsistências, duplicidades e drift de nomenclatura"
docs:
  - "docs/contracts/event-bus.md"
  - "docs/contracts/subject-registry.yaml"
  - ".context/docs/feature-packs/storage.md"
  - ".context/docs/feature-packs/orderbook.md"
  - ".context/docs/feature-packs/heatmap.md"
  - ".context/docs/feature-packs/volume-profiles.md"
  - ".context/docs/feature-packs/liquidations-markprice.md"
  - ".context/docs/feature-packs/delivery-ws.md"
phases:
  - id: "phase-1"
    name: "Discovery & Taxonomy Extraction"
    prevc: "P"
  - id: "phase-2"
    name: "Registry + Documentation Alignment"
    prevc: "E"
  - id: "phase-3"
    name: "Validation + Commit Chain"
    prevc: "V"
---

# Stabilizar taxonomia de subjects Plan

> Consolidar subjects existentes e planejados, criar registry YAML machine-readable, corrigir inconsistências conceituais em packs sem alterar runtime.

## Task Snapshot
- **Primary goal:** Introduzir um registry canônico de subjects em YAML e alinhar todos os feature-packs com a taxonomia de `docs/contracts/event-bus.md`.
- **Scope in:** contratos e documentação em `docs/contracts/` e `.context/docs/feature-packs/`.
- **Scope out:** qualquer implementação runtime, adapters, actors, ou mudanças de comportamento.
- **Success signal:** subject-registry presente, com status (`stable|draft|planned`), inconsistências resolvidas (BC, naming, duplicidade, TBD), e cadeia de 3 commits concluída.

## Agent Lineup
| Agent | Role in this plan | Focus |
| --- | --- | --- |
| Architect Specialist | Definir o modelo de registry e regras de ownership por BC | Taxonomia canônica e limites de contexto |
| Documentation Writer | Atualizar contratos e packs sem alterar runtime | Clareza semântica e rastreabilidade |
| Code Reviewer | Garantir coerência entre fontes e evitar regressão documental | Duplicidades, drift, placeholders TBD |

## Working Phases
### Phase 1 — Discovery & Taxonomy Extraction (P)
1. Extrair todos os subjects reais e planejados de `event-bus.md` + feature-packs.
2. Classificar por BC (`marketdata`, `aggregation`, `insights`, `quarantine`, `delivery/ws`).
3. Identificar divergências: ownership, naming, duplicação, placeholders TBD, instrument vs symbol.

**Deliverables**
- Inventário consolidado de subjects.
- Lista de correções documentais necessárias.

### Phase 2 — Registry + Documentation Alignment (E)
1. Criar `docs/contracts/subject-registry.yaml` com modelo minimalista e status por subject.
2. Corrigir `storage` para remover outputs de insights (storage não emite insights).
3. Alinhar feature-packs para nomenclatura canônica e subjects planejados explícitos (sem `TBD`).

**Deliverables**
- Registry YAML preenchido.
- Feature-packs alinhados com taxonomia única.

### Phase 3 — Validation + Commit Chain (V)
1. Revisar diffs e validar que nenhuma mudança tocou runtime.
2. Executar cadeia de commits:
   - `docs(contracts): introduce subject registry`
   - `docs(storage): remove insights outputs from storage pack`
   - `docs(contracts): align subjects across feature packs`
3. Registrar decisões arquiteturais no MCP plan.

**Deliverables**
- 3 commits no histórico local.
- Registro de decisões no MCP.

## Risks and Mitigations
- **Risco:** conflito entre subjects planejados e ownership de BC.
  **Mitigação:** marcar `planned`/`draft` explicitamente e incluir `owner_bc` no registry.
- **Risco:** drift entre bus subject e WS stream subject.
  **Mitigação:** manter WS fora do registry de bus e documentar `symbol` como representação de delivery.

## Success Criteria
- Existe `docs/contracts/subject-registry.yaml` machine-readable e validável em CI no futuro.
- Não há subject de insight atribuído a storage como output runtime.
- Não há placeholders de subject `TBD` nos feature-packs alterados.
- Taxonomia segue formato canônico `{event}.v{version}.{venue}.{instrument}` para event bus.
