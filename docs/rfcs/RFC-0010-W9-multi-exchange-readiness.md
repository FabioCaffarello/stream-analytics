# RFC-0010 - W9: Multi-Exchange Readiness

**Status:** Draft
**Implementation status:** Partially Implemented
**Partial marker:** Status: Partially Implemented
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W9 of PRD-0001
**Relates to:** ADR-0011, ADR-0017, ADR-0018, `docs/rfcs/EXECUTION-SEQUENCE.md`

---

## Objetivo

Validar readiness multi-exchange no mesmo processo, preservando isolamento de domínio e invariantes de normalização.

## Escopo

- Adapter Bybit (trade/bookdelta) com normalização canônica.
- Config `consumer.exchanges[]` com síntese determinística de legado single-exchange.
- Guardian com readiness para múltiplos subsistemas esperados.
- E2E do consumer com Binance + Bybit no mesmo runtime.

## Nao-Escopo

- Cobertura completa de API Bybit.
- Estratégias de arbitragem cross-venue.
- Criar target separado no Makefile para pureza de core (guard já está no `invariants-check`).

## Design

### Runtime partition
- Cada exchange ativa vira um subsistema próprio no consumer.
- Readiness usa conjunto `ExpectedSubsystems` configurado no guardian.

### Canonicalization
- Chave interna de instrumento: `CanonicalInstrument` (sem separador).
- Identidade de stream inclui `venue + instrument + market_type`.

### Backward compatibility
- Config legada de exchange única continua funcional.
- Campo `consumer.exchanges[]` é o caminho canônico para expansão.

## Rollout

| Fase | Status | Referencia |
|---|---|---|
| W9-1: bybit adapter + wiring multi-exchange | Implemented | `cmd/consumer/e2e_consumer_integration_test.go:24` |
| W9-2: gate E2E consumer multi-exchange | Implemented | `cmd/consumer/e2e_consumer_integration_test.go:24` |
| W9-3: validações de subject para features | Implemented | `internal/shared/config/loader_test.go:610` |
| MEX-4 core purity guard em invariants-check | Implemented | `scripts/check-domain-isolation.sh` |

## Test Plan

```bash
make invariants-check
make test-workspace
make test-workspace-race
go test -tags=integration ./cmd/consumer -run TestE2EConsumerMultiExchange -count=1
```

Validações mínimas:
- runtime sobe com Binance + Bybit e reporta readiness.
- graceful stop/restart sem perda de readiness.
- canonical instrument permanece estável cross-venue.
- modo single-exchange legado permanece sem regressão.

## Acceptance

- Adapter Bybit parseia trade/bookdelta no pipeline atual.
- Consumer com múltiplas exchanges inicia e mantém readiness.
- `ExpectedSubsystems` é respeitado no guardian para estado ready.
- Config legado single-exchange segue suportado.
- Contratos de normalização (`ADR-0017`) permanecem válidos.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Drift documental sobre "SubsystemKey" dinâmico | Medio | alinhar texto ao tipo real `Subsystem string` + chaves configuradas |
| Falso positivo no guard MEX-4 | Medio | manter regex focada em literais/constantes e revisar quando surgir novo adapter |
| Divergência entre canonical display e canonical key interna | Alto | referenciar ADR-0017 e testes de naming/instrument identity |

## Implementation Matrix

| Capability | Status | Reference |
|---|---|---|
| `consumer.exchanges[]` config model | Implemented | `internal/shared/config/schema.go:107` |
| Legacy single-exchange compatibility | Implemented | `cmd/consumer/main_test.go:49` |
| Guardian expected subsystem readiness | Implemented | `internal/actors/runtime/guardian.go:28`, `internal/actors/runtime/guardian_test.go:99` |
| Consumer E2E multi-exchange | Implemented | `cmd/consumer/e2e_consumer_integration_test.go:24` |
| Feature-subject fail-fast validation | Implemented | `internal/shared/config/loader_test.go:610` |
| MEX-4 core purity guard (`internal/core` sem literais exchange-specific) | Implemented | `scripts/check-domain-isolation.sh` |

## Evidence

- `internal/shared/config/schema.go:107`
- `cmd/consumer/main.go:157`
- `cmd/consumer/main_test.go:49`
- `cmd/consumer/e2e_consumer_integration_test.go:24`
- `internal/actors/runtime/guardian.go:28`
- `internal/actors/runtime/guardian_test.go:99`
- `internal/shared/naming/naming_test.go:56`
- `internal/shared/config/loader_test.go:610`

## Changelog

- 2026-02-12:
- RFC criada para readiness multi-exchange W9.

- 2026-02-13:
- Normalização para contrato RFC doc-first.
- Marcador explícito de implementação parcial.
- Remoção de expectativa implícita de target inexistente (`audit-core-purity`) como se já estivesse entregue.
- Guard MEX-4 integrado ao `invariants-check` via `scripts/check-domain-isolation.sh`.
