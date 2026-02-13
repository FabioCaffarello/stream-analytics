# W4/W5 Post-Merge Audit (Historical)

**Status:** Accepted
**Document class:** Historical Audit Record
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Relates to:** `docs/rfcs/RFC-0005-W4-observability-profiling.md`, `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md`, `docs/audits/DRIFT-REPORT-W11.md`

---

## Objetivo

Preservar os achados de auditoria W4/W5 como baseline histórico de hardening, sem competir com a autoridade de auditoria mais recente.

## Escopo

- Riscos e mitigação aplicados na janela W4/W5.
- Evidências de testes/bench da época.

## Nao-Escopo

- Estado atual completo de governança/auditoria.
- Fonte primária de decisão para roadmap atual.

## Design

Este documento é um snapshot histórico.

Autoridade vigente de auditoria cruzada:
- `docs/audits/AUDIT-PACK-W11-finalization.md`
- `docs/audits/DRIFT-REPORT-W11.md`

## Rollout

- Nenhum rollout adicional; registro histórico somente.

## Test Plan

Comandos históricos executados na época (mantidos para rastreabilidade):

```bash
go test ./internal/shared/metrics
go test ./internal/adapters/bus
go test ./internal/shared/ds
go test ./internal/actors/runtime ./internal/actors/marketdata/ws ./internal/interfaces/http
go test ./internal/actors/marketdata/runtime
```

## Acceptance

- Documento identificado como histórico.
- Referência explícita à auditoria mais nova (W11).
- Rastreabilidade preservada para investigações retrospectivas.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Dupla autoridade de auditoria | Médio | declarar W11 como autoridade atual e manter W4/W5 como anexo histórico |

## Evidence

- `.context/evidence/w4w5-audit-boundedmap-bench.txt`
- `.context/evidence/w4w5-audit-boundedmap-cpu.pprof`
- `.context/evidence/w4w5-audit-boundedmap-mutex.pprof`
- `internal/shared/metrics/metrics_test.go:39`
- `internal/adapters/bus/bus_test.go:147`
- `internal/shared/ds/boundedmap_test.go:108`

## Historical Findings Summary

Resumo (preservado):
- mitigação de cardinalidade de labels em métricas de bus;
- contenção reduzida em `BoundedMap` (`OnEvict` fora do lock);
- asserts adicionais de cleanup de timers;
- risco residual documentado para sweep no hot path.

## Changelog

- 2026-02-12:
- auditoria W4/W5 registrada.

- 2026-02-13:
- normalizado como histórico para evitar drift estrutural e dupla autoridade.
