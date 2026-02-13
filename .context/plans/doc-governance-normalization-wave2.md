---
status: filled
generated: 2026-02-13
agents:
  - type: "documentation-writer"
    role: "Normalize PRD/RFC/contracts structure and evidence"
  - type: "code-reviewer"
    role: "Reconcile doc claims with code/test truth"
  - type: "test-writer"
    role: "Validate workspace gates"
docs:
  - "TRUTH-MAP.md"
  - "DRIFT-REPORT-W11.md"
  - "PRD-0001-extreme-runtime.md"
  - "event-bus.md"
  - "RFC-0008-W7-nats-jetstream-integration.md"
  - "RFC-0010-W9-multi-exchange-readiness.md"
phases:
  - id: "p"
    name: "Planning"
    prevc: "P"
  - id: "r"
    name: "Review"
    prevc: "R"
  - id: "e"
    name: "Execution"
    prevc: "E"
  - id: "v"
    name: "Validation"
    prevc: "V"
  - id: "c"
    name: "Confirmation"
    prevc: "C"
---

# Doc Governance Normalization Wave 2

## Goal
- Normalizar governança documental de PRD + contrato de bus + RFCs W7/W9.
- Eliminar drift entre docs e estado executável atual sem mudar arquitetura/runtime.

## Scope
- Patch em:
- `docs/prd/PRD-0001-extreme-runtime.md`
- `docs/contracts/event-bus.md`
- `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md`
- `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md`
- Atualização de cross-links de evidência quando necessário.

## PREVC

### P
- Selecionar contradições P0/P1 em `TRUTH-MAP` e `DRIFT-REPORT-W11`.
- Aplicar contrato de documento (`doc-contract-template.md`).

### R
- Para cada doc: mapear `true now` (código/testes) vs `planned`.
- Definir correções preservando histórico.

### E
- PRD: reclassificar status e separar snapshot histórico de estado atual.
- Event-bus: alinhar subject taxonomy a ADR-0014 e validação de subject.
- RFC-0008/0010: normalizar para seções RFC obrigatórias + matrix + evidence + parcial explícito.

### V
- Rodar:
- `make invariants-check`
- `make test-workspace`
- `make test-workspace-race`

### C
- Entregar diff summary por doc, open questions e próximos docs.

## Success Criteria
- Todos os 4 docs com header padronizado e seções mandatórias aplicáveis.
- RFCs parciais com marcador explícito `Status: Partially Implemented` e `Implementation Matrix`.
- Contrato de subject sem inconsistência com ADR-0014/validador atual.
- Gates de validação aprovados.
