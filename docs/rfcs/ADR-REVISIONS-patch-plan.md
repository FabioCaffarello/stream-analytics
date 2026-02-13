# ADR Revisions Patch Plan (Historical)

**Status:** Accepted
**Document class:** Historical
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Author:** Chief Architect
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0003-actor-runtime.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0005-sequencing-and-time-normalization.md`, `docs/adrs/ADR-0009-config-jsonc-determinism.md`, `docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`

---

## Objetivo

Preservar o plano cirúrgico original de emendas ADR (rodada 2026-02-12) como registro histórico, sem manter este arquivo como autoridade ativa.

## Escopo

- Registrar o que este plano cobria (lacunas de ADR-0002/0003/0004/0005/0009/0011).
- Direcionar leitores para ADRs finais atualizadas.

## Nao-Escopo

- Governar mudanças atuais de ADR.
- Substituir ADRs já normalizadas.

## Design

Este documento deixa de ser plano operacional ativo e passa a ser referência de histórico de decisão.

Autoridade atual:
- ADRs em `docs/adrs/*.md`
- índice de invariantes em `docs/architecture/system-invariants.md`
- reconciliação doc-vs-runtime em `docs/architecture/TRUTH-MAP.md` e `docs/audits/DRIFT-REPORT-W11.md`

## Rollout

- Nenhum rollout operacional. Documento apenas histórico.

## Test Plan

- N/A (documento histórico sem efeito de runtime).

## Acceptance

- Plano permanece disponível para auditoria histórica.
- Não há contradição com ADRs atuais (autoridade principal).
- Documento está explicitamente marcado como histórico.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Leitor tratar este plano como fonte ativa | Médio | header explícito de histórico + links para ADRs canônicas |

## Evidence

- `docs/adrs/ADR-0002-event-envelope-and-versioning.md`
- `docs/adrs/ADR-0003-actor-runtime.md`
- `docs/adrs/ADR-0004-bus-nats-jetstream.md`
- `docs/adrs/ADR-0005-sequencing-and-time-normalization.md`
- `docs/adrs/ADR-0009-config-jsonc-determinism.md`
- `docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`

## Changelog

- 2026-02-12:
- plano criado para emendas cirúrgicas em ADRs existentes.

- 2026-02-13:
- normalizado como documento histórico (não-autoritativo).
