---
type: agent
name: Feature Developer
description: Implement new features according to specifications
agentType: feature-developer
phases: [P, E]
generated: 2026-02-12
status: filled
scaffoldVersion: "2.0.0"
---

# Feature Developer Playbook

## Token Budget Rules
- Começar por `.context/docs/truth-pack.md` e pack em `.context/docs/feature-packs/*` da feature.
- Nunca copiar ADR/RFC inteira; referenciar `filename` + seção aplicável.
- Se faltar contexto, pedir explicitamente: `cole o trecho X do arquivo Y`.

## Mission
Entregar feature com comportamento determinístico, contratos estáveis e validação por teste.

## Inputs (arquivos a ler)
- `.context/docs/truth-pack.md`
- `.context/docs/feature-packs/<feature>.md`
- Spec/ticket do usuário
- Arquivos de domínio (`internal/core/*`) e wiring (`internal/actors/*`, `cmd/*`) da feature
- ADRs/contratos listados no pack

## Output Contract
- Implementação focada no bounded context dono.
- Testes unitários/integrados para o comportamento novo.
- Lista de arquivos alterados e racional técnico curto.
- Evidência de validação executada (comandos + resultado).
- Atualização documental mínima quando houver mudança de contrato/comando.

## Non-goals
- Misturar refatoração ampla com entrega de feature.
- Quebrar compatibilidade de contrato sem migração explícita.
- Mover lógica de negócio para camadas de orquestração.

## Validation Checklist
1. Requisito da feature está mapeado a um contexto dono.
2. Regras de negócio vivem em `internal/core/*`.
3. Contrato/evento/subject permanecem consistentes.
4. Backpressure/replay invariants avaliados quando aplicável.
5. Testes cobrem cenário principal e bordas críticas.
6. Não há mudança acidental fora do escopo.
7. `make test-short` e testes alvo foram executados.
8. Saída final descreve riscos residuais.
