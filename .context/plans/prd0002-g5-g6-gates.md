---
status: filled
generated: 2026-02-19
updated: 2026-02-19
owner: "codex"
objective: "Fechar os gates G5 e G6 do PRD-0002 com mudanças mínimas, sem novas features, mantendo invariants/layering e zero regressão."
scope:
  include:
    - "G5: parsers Binance/Bybit/Coinbase/HyperLiquid com testes verdes"
    - "G6: remoção de tokens CHANGE_ME em deploy/configs/*.jsonc"
    - "Atualização documental mínima de anchors/checklist no PRD-0002"
  exclude:
    - "Novas features de runtime"
    - "Refactors amplos fora dos pontos de falha dos gates"
    - "Mudanças de contrato wire não exigidas pelo gate"
references:
  - "docs/prd/PRD-0002-backend-stable-and-odin-ready.md"
  - "docs/architecture/TRUTH-MAP.md"
  - "docs/rfcs/EXECUTION-SEQUENCE.md"
  - "docs/rfcs/RFC-0011-product-parity-marketmonkey.md"
  - "docs/architecture/AUTHORITY-MAP.md"
  - "docs/contracts/delivery-ws.md"
---

# PRD-0002 Gates G5+G6 Closure Plan

## Goal
Tornar verdadeiros os gates G5 e G6 do PRD-0002 com o menor patch possível, garantindo que os gates oficiais de validação permaneçam verdes.

## Success Criteria
1. G5: suites de parser por exchange passam sem flakiness:
   - `internal/adapters/exchange/binance/parser_test.go`
   - `internal/adapters/exchange/bybit/parser_test.go`
   - `internal/adapters/exchange/coinbase/parser_test.go`
   - `internal/adapters/exchange/hyperliquid/parser_test.go`
2. G6: nenhum `CHANGE_ME` em `deploy/configs/*.jsonc`.
3. Gates de verificação final verdes:
   - `make invariants-check`
   - `make test-workspace`
   - `make lint`
   - `make go-tidy-check`
4. PRD-0002 atualizado apenas onde houver drift factual de checklist/anchors.

## Agent Lineup
- `feature-developer`: aplicar correções mínimas nos pontos de falha.
- `test-writer`: ajustar/adicionar testes somente quando necessário para fechar critério explícito de gate.
- `code-reviewer`: revisar regressão, layering e aderência ao escopo mínimo.
- `documentation-writer`: sincronizar anchors/checklist no PRD se houver mudança de evidência.

## PREVC Phases

### P — Plan
Objetivo: mapear exatamente o que falta para G5 e G6.

Passos:
1. Ler seção dos gates no PRD-0002 e anchors citados.
2. Listar estado atual dos testes de parser por exchange.
3. Executar varredura de `CHANGE_ME` em `deploy/configs/*.jsonc`.
4. Definir matriz de correção mínima (arquivo, falha, patch mínimo, evidência esperada).

Entregáveis:
- Checklist de pendências G5/G6 com caminhos e comandos.

### R — Review
Objetivo: validar desenho mínimo contra invariants/layering.

Passos:
1. Confirmar que mudanças ficam limitadas a testes/parsers/configs/documentação necessária.
2. Bloquear propostas de feature nova ou refactor amplo.
3. Registrar decisões de trade-off no tracking do workflow.

Entregáveis:
- Decisão explícita de escopo mínimo aprovada para execução.

### E — Execute
Objetivo: implementar as correções mínimas para fechar G5 e G6.

Passos G5:
1. Rodar testes focados por exchange.
2. Corrigir parser/teste apenas no ponto que falhar.
3. Reexecutar a suite focada até verde.

Passos G6:
1. Remover/substituir `CHANGE_ME` em `deploy/configs/*.jsonc`.
2. Garantir formato/config válidos sem introduzir comportamento novo.

Entregáveis:
- Código e/ou testes corrigidos para G5.
- Configs sem `CHANGE_ME` para G6.

### V — Validate
Objetivo: garantir fechamento de gate e ausência de regressão.

Ordem obrigatória:
1. `make invariants-check`
2. `make test-workspace`
3. `make lint`
4. `make go-tidy-check`

Regra de parada:
- Se qualquer comando falhar: parar imediatamente, reportar causa e aplicar patch mínimo corretivo antes de prosseguir.

Entregáveis:
- Evidência de execução dos quatro gates.

### C — Confirm
Objetivo: consolidar evidência e sincronizar documentação de autoridade.

Passos:
1. Atualizar PRD-0002 apenas para remover TODOs/states desatualizados de G5/G6.
2. Verificar `make docs-check` se houver mudança documental.
3. Fechar workflow com resumo de arquivos alterados e comandos executados.

Entregáveis:
- Anchors/checklist alinhados ao estado real do repositório.

## Risks and Mitigations
- Risco: parser quebrar comportamento existente ao ajustar edge case.
  - Mitigação: patch mínimo + foco em testes existentes da exchange afetada.
- Risco: valor de config em G6 gerar acoplamento de ambiente.
  - Mitigação: manter placeholders operacionais válidos sem semântica de segredo real e sem `CHANGE_ME`.
- Risco: regressão lateral fora do escopo.
  - Mitigação: validação completa com os quatro gates oficiais.

## Rollback Strategy
1. Reverter somente commits do fechamento G5/G6.
2. Reexecutar os quatro gates para confirmar restauração do baseline.
3. Manter registro da causa raiz da falha no tracking do workflow.

## Commit Plan (Conventional Commits)
1. `test(adapters): close prd0002 gate5 parser failures` (se houver mudanças de parser/teste)
2. `chore(configs): remove change_me tokens for gate6` (se houver mudanças em configs)
3. `docs(prd): sync gate5-gate6 checklist anchors` (se houver drift documental)

## Exit Condition
Plano concluído quando G5 e G6 estiverem factualmente verdadeiros no PRD-0002 e todos os gates de validação executarem verdes sem regressões.
