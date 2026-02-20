# ADR-0018 - Actor Topology and Supervision Model

**Status:** Accepted
**Implementation status:** Partially Implemented
**Partial marker:** Status: Partially Implemented
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 section E.3, ADR-0003, ADR-0012, `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md`

---

## Contexto

A topologia de atores e as politicas de supervisao ja estao parcialmente materializadas em runtime (guardian com readiness e isolamento por subsistema), mas a evidencia operacional de longo prazo ainda nao esta completa para fechar todos os invariantes desta ADR.

O objetivo deste patch e remover ambiguidade: separar claramente o que esta implementado hoje do que segue pendente de validacao operacional.

## Decisao

1. Manter a ADR como decisao proposta ate fechar evidencias operacionais pendentes (principalmente soak longo e evidencias ampliadas de dedup/isolamento).
2. Consolidar como implementado no estado atual:
- guardian com expected subsystems e readiness por conjunto configurado
- suporte a topologia multi-exchange por chaves de subsistema
- rate limit de restart no guardian
3. Preservar boundary de dominio: politicas de lifecycle/supervisao permanecem fora de `internal/core/*`.
4. Usar matriz de implementacao para governar fechamento incremental dos invariantes `TOP-*`.

## Consequencias

- Positivas:
- Estado operacional real fica auditavel sem forcar "Accepted" prematuro.
- Regras de supervisao e isolamento ficam ligadas a testes concretos.
- Menor risco de checklist fantasma em revisoes futuras.

- Negativas:
- Parte da confianca operacional depende de evidencia ainda nao capturada (soak e alguns cenarios de dedup).

## Invariantes

- `TOP-1`: falha em subsistema nao deve derrubar os demais subsistemas ativos.
- `TOP-2`: limite global de restart deve impedir storm de reinicios.
- `TOP-3`: atores de sessao devem encerrar de forma limpa e remover registro do roteador.
- `TOP-4`: ciclos repetidos de restart devem manter estabilidade de goroutines (soak).
- `TOP-5`: deduplicacao por `Msg-ID` no JetStream deve evitar dupla entrega em janela de dedup.

## Implementation Matrix

| Feature | Status | Referencia |
|---|---|---|
| Expected subsystems e readiness dinamico no guardian | Implemented | `internal/actors/runtime/guardian.go:28`, `internal/actors/runtime/guardian_test.go:436` |
| Topologia multi-exchange no runtime | Implemented | `cmd/consumer/main.go:183`, `cmd/consumer/e2e_consumer_integration_test.go:24` |
| Restart rate limiter global | Implemented | `internal/actors/runtime/guardian_test.go:315` |
| Isolamento forte entre subsistemas sob falha | Partially Implemented | `internal/actors/runtime/guardian_test.go:99` |
| Soak longo para estabilidade de restart (`TOP-4`) | Planned | `Makefile:142`, `scripts/test/soak/soak-test.sh` |
| Evidencia completa de dedup em janela operacional (`TOP-5`) | Partially Implemented | `internal/adapters/jetstream/publisher_integration_test.go:41` |

## Evidence

- Runtime supervision and readiness:
- `internal/actors/runtime/guardian.go:28`
- `internal/actors/runtime/guardian_test.go:99`
- `internal/actors/runtime/guardian_test.go:315`
- `internal/actors/runtime/guardian_test.go:436`

- Multi-exchange process wiring:
- `cmd/consumer/main.go:183`
- `cmd/consumer/e2e_consumer_integration_test.go:24`

- Dedup and bus behavior:
- `internal/adapters/jetstream/publisher_integration_test.go:41`
- `internal/adapters/jetstream/consumer_integration_test.go:21`

- Soak gate (operational evidence path):
- `Makefile:142`
- `.context/evidence/w5-soak.txt`

## Changelog

- 2026-02-12:
- ADR criada para formalizar topologia e supervisao de atores.

- 2026-02-13:
- Normalizacao governance doc-first.
- Status explicito de implementacao parcial.
- Inclusao de `Implementation Matrix` e `Evidence` com trilhas verificaveis.
