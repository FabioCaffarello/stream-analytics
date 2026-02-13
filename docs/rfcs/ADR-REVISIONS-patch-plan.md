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

## ADR-REVISION NOTE (W12 parity docs)

### NOTE-001: subject roots para `aggregation.*` e `delivery.*`

Contexto:
- `docs/adrs/ADR-0014-stream-partitioning-strategy.md` usa exemplos com `aggregation.snapshot`.
- `internal/adapters/jetstream/subject_validation.go` atualmente permite apenas roots `marketdata|insights|quarantine`.

Patch plan:
1. decidir autoridade: manter somente `insights.*` para derivados ou ampliar roots permitidas;
2. se ampliar, revisar ADR-0014 com regra explícita de roots suportadas;
3. alinhar `docs/contracts/event-bus.md` + validator/testes para eliminar drift.

Status: `OPEN QUESTION` (sem alteração de runtime neste ciclo doc-first).
Atualizacao 2026-02-13 (parity patch):
- docs de parity mantem `aggregation.*` como `Planned`, nunca como `Existing`;
- `docs/contracts/delivery-ws.md` e `docs/architecture/storage.md` agora referenciam explicitamente esta NOTE-001 para evitar drift silencioso.

### NOTE-002: definição de hot-path em ADR-0006

Contexto:
- ADR-0006 define hot-path atual como read model em memória.
- Parity docs v1 introduzem conceito de hot durável em Timescale (L1), mantendo memória como L0.

Patch plan:
1. emendar ADR-0006 para explicitar modelo em camadas `L0 memória + L1 Timescale + L2 ClickHouse`;
2. manter regra de que delivery em tempo real continua baseado no L0;
3. registrar critérios de consistência L0/L1 em invariantes de storage.

Status: `OPEN QUESTION` (documentado; sem implementação neste ciclo).
Atualizacao 2026-02-13 (parity patch):
- docs de storage/orderbook foram ajustados para refletir `L0 memoria = Existing`, `L1/L2 = Planned/TODO`;
- nenhum texto de parity afirma adaptadores Timescale/ClickHouse como implementados.

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
