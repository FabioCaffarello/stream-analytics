---
status: filled
generated: 2026-02-19
updated: 2026-02-19
owner: "codex"
objective: "Fechar Gate 11 do PRD-0002 em main com checklist final alinhado e tag v0.1.0-stable publicada no remoto."
scope:
  include:
    - "Sincronizar status do item 11 no checklist do PRD-0002"
    - "Criar tag v0.1.0-stable no commit final em main"
    - "Push do branch main e da tag"
  exclude:
    - "Mudanças de feature ou refactors"
references:
  - "docs/prd/PRD-0002-backend-stable-and-odin-ready.md"
---

# PRD-0002 Gate 11 Release Tag Plan

## Goal
Concluir o último item pendente do PRD-0002 com rastreabilidade de release no branch `main`.

## Success Criteria
1. Checklist do PRD mostra item 11 como `Done`.
2. Tag `v0.1.0-stable` existe localmente e no `origin` apontando para `main`.
3. `make docs-check` verde após ajuste documental.

## PREVC
### P
- Confirmar branch atual `main` e ausência/presença da tag.
- Definir patch mínimo de documentação.

### E
- Atualizar PRD item 11 para `Done`.
- Commitar alteração documental.
- Criar e publicar tag `v0.1.0-stable`.

### V
- Rodar `make docs-check`.
- Verificar `git show-ref --tags v0.1.0-stable` e `git ls-remote --tags origin`.

## Rollback
- Se tag errada: `git tag -d v0.1.0-stable` e `git push origin :refs/tags/v0.1.0-stable`.
- Se doc inconsistente: reverter commit documental e refazer patch mínimo.
