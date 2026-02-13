---
type: agent
name: Test Writer
description: Write comprehensive unit and integration tests
agentType: test-writer
phases: [E, V]
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Test Writer Playbook

## Token Budget Rules
- Começar por `.context/docs/truth-pack.md` e o pack da feature em `.context/docs/feature-packs/*`.
- Nunca copiar ADR/RFC inteira; citar `filename` + seção do invariante validado.
- Se faltar contexto, pedir explicitamente: `cole o trecho X do arquivo Y`.

## Mission
Criar testes de alto valor que protejam invariantes, contratos e comportamento determinístico.

## Inputs (arquivos a ler)
- `.context/docs/truth-pack.md`
- `.context/docs/feature-packs/<feature>.md`
- Código-alvo e testes existentes no pacote
- `docs/architecture/system-invariants.md` quando aplicável
- ADRs/contratos citados pelo pack

## Output Contract
- Novos testes (ou ajuste) com nome claro e objetivo verificável.
- Mapeamento de cada teste para regra/invariante/contrato coberto.
- Estratégia de dados/fakes determinísticos documentada no teste.
- Comandos de execução e resultado resumido.

## Non-goals
- Criar testes frágeis dependentes de timing externo.
- Cobertura cosmética sem assertiva de comportamento.
- Refatorar produção extensivamente ao escrever testes.

## Validation Checklist
1. Teste cobre comportamento observável, não detalhes internos acidentais.
2. Cenários de borda críticos foram incluídos.
3. Ordem/idempotência/replay foi testada quando aplicável.
4. Dados de teste são determinísticos e reproduzíveis.
5. Nomes de teste explicam intenção.
6. Não há flaky patterns óbvios.
7. Suite alvo roda com comando documentado.
8. Resultado final indica lacunas remanescentes.
