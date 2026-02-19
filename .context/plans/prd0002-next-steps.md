---
status: filled
generated: 2026-02-19
updated: 2026-02-19
owner: "codex"
objective: "Concluir os gates remanescentes do PRD-0002 e preparar mudança de status para Active sem regressões de qualidade."
scope:
  include:
    - "G1: CI baseline estável"
    - "G2: soak evidence"
    - "G3: compose smoke"
    - "G7: promtool rules"
    - "G8: clickhouse migrations"
    - "G9: timescale migrations"
    - "G10: promover PRD para Active"
    - "G11: preparar release tag"
  exclude:
    - "Novas features fora dos critérios de gate"
    - "Refactors arquiteturais não exigidos"
references:
  - "docs/prd/PRD-0002-backend-stable-and-odin-ready.md"
  - "docs/architecture/TRUTH-MAP.md"
  - "docs/rfcs/EXECUTION-SEQUENCE.md"
  - "docs/architecture/AUTHORITY-MAP.md"
---

# PRD-0002 Next Steps Plan

## Goal
Fechar os gates remanescentes do PRD-0002 em sequência de risco controlado e deixar o pacote pronto para promoção de status (`Active`) e tag de release.

## Prioridade de Execução
1. G3 (smoke compose) — desbloqueia confiança operacional local.
2. G7 (promtool) — valida observabilidade antes de release.
3. G8/G9 (migrations) — garante readiness de dados em ambiente limpo.
4. G2 (soak evidence) — comprova estabilidade prolongada.
5. G1 (CI full) — validação final integrada.
6. G10/G11 (status + tag) — fechamento formal de release.

## Success Criteria
- `scripts/smoke-compose.sh` implementado e validado (G3).
- `promtool check rules` verde para regras ativas (G7).
- Migrations ClickHouse e Timescale executam sem erro em banco fresco (G8/G9).
- Evidência de soak atualizada e versionada em `.context/evidence/` (G2).
- `make ci` verde sem flake em rodada de fechamento (G1).
- PRD-0002 com status `Active` e checklist alinhado ao estado real (G10).
- Tag `v0.1.0-stable` preparada para criação no branch alvo (G11).

## Agent Lineup
- `feature-developer`: implementar scripts e ajustes mínimos de execução.
- `test-writer`: reforçar validações não-flaky para smoke/migrations quando necessário.
- `code-reviewer`: revisar risco de regressão e aderência de escopo.
- `documentation-writer`: sincronizar PRD/checklists/evidências com estado factual.

## PREVC Phases

### P — Plan (Backlog Executável)
1. Confirmar anchors atuais de G1..G11 no PRD.
2. Mapear dependências de ambiente para compose, promtool e DB migrations.
3. Definir matriz de execução por gate: comando, pré-condição, evidência, dono.

Entregável: backlog ordenado por risco + critérios binários por gate.

### R — Review (Desenho Mínimo)
1. Validar desenho mínimo de `scripts/smoke-compose.sh` (timeout, exit codes, health probes).
2. Revisar se validações de migrations podem ser feitas sem alterar runtime.
3. Definir estratégia de soak evidence (fonte, duração, formato, caminho).

Entregável: decisões de escopo mínimo registradas.

### E — Execute (Gate-by-Gate)
1. G3: implementar/ajustar smoke script + target make necessário.
2. G7: corrigir rules/runbook links apenas se promtool falhar.
3. G8/G9: executar migrations em ambiente limpo e corrigir scripts de bootstrap se necessário.
4. G2: executar soak e publicar evidência no caminho canônico.
5. G10: atualizar PRD para `Active` quando os gates prévios estiverem fechados.
6. G11: preparar instrução/commit de release tag (sem criar tag automaticamente se não solicitado).

Entregável: gates remanescentes fechados com mudanças mínimas.

### V — Validate (Ordem Obrigatória)
1. `make docs-check`
2. `make invariants-check`
3. `make test-workspace`
4. `make lint`
5. `make go-tidy-check`
6. `make ci`

Regra: falhou qualquer gate, interromper e aplicar patch mínimo corretivo antes de seguir.

Entregável: evidência de validação completa sem regressão.

### C — Confirm (Fechamento)
1. Revisar checklist final do PRD-0002.
2. Confirmar anchors de evidência/commands.
3. Registrar changelog final e estado do workflow.

Entregável: pacote pronto para release decision.

## Risks and Mitigations
- Dependência de ambiente local para compose/migrations.
  - Mitigação: pré-check de serviços/ports e scripts idempotentes.
- Soak demandando tempo e recursos.
  - Mitigação: janela controlada com critérios de aceite objetivos.
- Drift entre docs e execução real.
  - Mitigação: atualizar PRD imediatamente após cada gate fechado.

## Rollback Strategy
1. Reverter alterações do gate que falhou.
2. Reexecutar sequência mínima de validação (`docs-check`, `invariants-check`, `test-workspace`).
3. Reaplicar patch reduzido com escopo menor.

## Commit Plan (Conventional)
1. `chore(interfaces): add compose smoke gate script` (G3)
2. `chore(adapters): stabilize migration gate checks` (G8/G9, se necessário)
3. `chore(shared): refresh soak evidence for gate2` (G2)
4. `docs(shared): promote prd0002 to active and close checklist` (G10/G11 readiness)

## Exit Condition
Plano concluído quando G1, G2, G3, G7, G8, G9, G10 e G11 estiverem factualmente verdadeiros no PRD-0002 e `make ci` estiver verde no fechamento.
