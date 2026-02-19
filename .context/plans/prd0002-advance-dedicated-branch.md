---
status: filled
generated: 2026-02-19
updated: 2026-02-19
owner: "codex"
objective: "Executar o próximo avanço do PRD-0002 em branch dedicada e abrir PR via gh CLI ao concluir o workflow PREVC."
scope:
  include:
    - "Levantamento dos gaps atuais do PRD-0002"
    - "Implementação mínima para o próximo bloco de gates/itens de checklist"
    - "Validação por gates oficiais do repositório"
    - "Abertura de PR com gh CLI no fechamento"
  exclude:
    - "Mudanças de feature fora do escopo do PRD"
    - "Refactors amplos sem vínculo de gate"
references:
  - "docs/prd/PRD-0002-backend-stable-and-odin-ready.md"
  - "docs/architecture/TRUTH-MAP.md"
  - "docs/rfcs/EXECUTION-SEQUENCE.md"
---

# PRD-0002 Advance Plan (Dedicated Branch)

## Branch Strategy
- Branch de trabalho: `codex/prd0002-advance-dedicated`
- Base esperada: `main` atualizado
- Merge target: `main`

## Success Criteria
1. Itens do PRD selecionados para este ciclo passam de `Pending` para `Done` com evidência verificável.
2. Validação completa verde:
   - `make docs-check`
   - `make invariants-check`
   - `make test-workspace`
   - `make lint`
   - `make go-tidy-check`
3. PR aberto com `gh pr create` contendo resumo de escopo, evidência e riscos residuais.

## PREVC Execution

### P — Plan
1. Ler estado atual do PRD-0002 e mapear os próximos itens de maior prioridade.
2. Confirmar dependências de execução e riscos técnicos.
3. Definir patch mínimo por item (arquivo, comando de validação, evidência esperada).

### R — Review
1. Revisar desenho mínimo e aderência a layering/invariants.
2. Eliminar propostas fora de escopo.
3. Registrar decisões principais no tracking do plano.

### E — Execute
1. Implementar somente mudanças necessárias para os itens selecionados do PRD.
2. Atualizar documentação/anchors apenas quando houver mudança factual.
3. Preparar commit(s) convencionais com escopo claro.

### V — Validate
1. Rodar gates na ordem: docs-check, invariants-check, test-workspace, lint, go-tidy-check.
2. Se qualquer gate falhar: parar imediatamente, reportar causa e aplicar patch mínimo corretivo.

### C — Complete (via PR)
1. Consolidar changelog e checklist do PRD para o escopo entregue.
2. Abrir PR com `gh`:
```bash
gh pr create \
  --base main \
  --head codex/prd0002-advance-dedicated \
  --title "chore(prd): advance PRD-0002 next block" \
  --body-file .context/workflow/docs/prd0002-advance-pr.md
```
3. No corpo do PR incluir: objetivo, mudanças, comandos executados, resultado dos gates, riscos residuais.

## Rollback
- Reverter apenas commits do ciclo atual na branch dedicada.
- Reexecutar gates mínimos (`docs-check`, `invariants-check`, `test-workspace`).

## Exit Condition
Workflow concluído quando o bloco selecionado do PRD estiver factual e tecnicamente validado, com PR aberto via `gh` para revisão.
