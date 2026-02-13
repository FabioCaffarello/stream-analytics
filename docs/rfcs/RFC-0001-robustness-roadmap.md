# RFC-0001 - Robustness Roadmap

**Status:** Accepted
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-11
**Author:** Chief Architect
**Relates to:** ADR-0003, ADR-0004, ADR-0007, ADR-0009, `docs/rfcs/EXECUTION-SEQUENCE.md`

---

## Objetivo

Manter um roadmap unificado de robustez (W1..W13) alinhado ao estado real do codigo, com checkpoints verificaveis e sem drift entre planejamento e runtime.

## Escopo

- Consolidar estado de implementacao dos work packages W1..W13.
- Fixar contrato de validacao por gates reais do workspace.
- Direcionar backlog remanescente para fechamento operacional (sem alterar arquitetura nesta rodada).

## Nao-Escopo

- Introduzir novas decisoes arquiteturais fora de ADR/RFC ja existentes.
- Alterar runtime neste patch de governanca.
- Substituir RFCs detalhadas por pacote (RFC-0002..RFC-0010).

## Design

- Este RFC funciona como roadmap de alto nivel.
- A trilha detalhada de execucao permanece em `docs/rfcs/EXECUTION-SEQUENCE.md`.
- A verdade operacional de doc-vs-code e reconciliada com:
- `docs/architecture/TRUTH-MAP.md`
- `docs/audits/DRIFT-REPORT-W11.md`

Ciclo de governanca exigido:
- Planejar (P): selecionar escopo e contrato documental.
- Revisar (R): reconciliar "true today" vs "planned".
- Executar (E): patch incremental com evidencias.
- Validar (V): gates workspace-safe.
- Confirmar (C): diff summary, perguntas abertas, proxima onda.

## Rollout

| Wave | Scope | Output |
|---|---|---|
| Wave A | W1-W3 hardening base | Config, readiness/liveness, delivery BC base, JetStream base |
| Wave B | W4-W10 runtime hardening | observability, lifecycle, protobuf foundation, replay, multi-exchange, insights hardening |
| Wave C | W11-W13 governance + contract completion | truth/drift map + fechamento da camada de contratos |

## Test Plan

Gates canonicos:

```bash
make invariants-check
make test-workspace
make test-workspace-race
make proto-lint
make proto-breaking
make soak-check
```

Checks de replay e determinismo (quando aplicavel):

```bash
go test ./internal/shared/replay -run TestGoldenReplay
go test ./cmd/consumer -run TestReplayIngestGolden1000
```

## Acceptance

- Roadmap deve refletir status real dos pacotes W1..W13.
- Todo item parcial precisa estar explicitamente marcado com `Partially Implemented`.
- Cada macro-area deve apontar para evidencia de codigo/teste ou RFC detalhada.
- Nenhuma linha deste RFC pode contradizer `docs/architecture/TRUTH-MAP.md`.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Status inflado (marcar como "done" sem evidencia) | Alto | exigir links de teste/gate em matriz |
| Checklist fantasma em RFCs de sequencia | Medio | manter apenas gates e itens realmente existentes no Makefile/codigo |
| Drift entre roadmap e RFCs detalhadas | Medio | usar `EXECUTION-SEQUENCE` como trilha detalhada unica |

## Implementation Matrix

| Work Package | Status | Referencia |
|---|---|---|
| W1 - Config/Shutdown hardening | Implemented | `docs/rfcs/RFC-0002-w1-config-shutdown-hardening.md`, `internal/interfaces/http/server_test.go:260` |
| W2 - Delivery BC base | Implemented | `docs/rfcs/RFC-0003-W2-DELIVERY-BC.md` |
| W3 - Sources / adapter base | Implemented | `docs/rfcs/RFC-0004-W3-SOURCES-MARKETDATA-BINANCE.md` |
| W4 - Observability | Implemented | `docs/rfcs/RFC-0005-W4-observability-profiling.md`, `internal/interfaces/http/server_test.go:1` |
| W5 - Lifecycle hardening | Implemented | `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md`, `internal/actors/runtime/guardian_test.go:315` |
| W6 - Protobuf contract foundation | Partially Implemented | `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md`, `docs/adrs/ADR-0016-protobuf-contract-layer.md` |
| W7 - JetStream integration | Implemented | `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md`, `internal/adapters/jetstream/consumer_integration_test.go:21` |
| W8 - Deterministic replay | Implemented | `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md`, `internal/shared/replay/golden_test.go:1` |
| W9 - Multi-exchange readiness | Partially Implemented | `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md`, `cmd/consumer/e2e_consumer_integration_test.go:24` |
| W10 - Insights hardening | Implemented | `docs/rfcs/EXECUTION-SEQUENCE.md` |
| W11 - Truth/drift reconciliation | Implemented | `docs/architecture/TRUTH-MAP.md`, `docs/audits/DRIFT-REPORT-W11.md` |
| W12 - Operational maturity gate | Implemented | `docs/rfcs/EXECUTION-SEQUENCE.md` |
| W13 - Contract layer completion | Planned | `docs/rfcs/EXECUTION-SEQUENCE.md` |

## Evidence

- Governance baselines:
- `docs/architecture/TRUTH-MAP.md`
- `docs/audits/DRIFT-REPORT-W11.md`

- Runtime validation anchors:
- `Makefile:123`
- `Makefile:136`
- `Makefile:139`
- `Makefile:217`
- `Makefile:224`
- `Makefile:142`

- Key integration tests:
- `internal/adapters/jetstream/consumer_integration_test.go:21`
- `cmd/consumer/e2e_consumer_integration_test.go:24`
- `internal/shared/replay/golden_test.go:1`

## Changelog

- 2026-02-11:
- RFC criado como roadmap inicial de robustez.

- 2026-02-13:
- Normalizacao para contrato RFC doc-first.
- Matriz de implementacao passou a marcar explicitamente itens `Partially Implemented`.
- Inclusao de `Implementation Matrix`, `Evidence` e alinhamento com `TRUTH-MAP`.
