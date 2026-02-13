# PRD-0001 - Market Raccoon Extreme Runtime

**Status:** Active
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Author:** Chief Architect
**Relates to:** `docs/architecture/TRUTH-MAP.md`, `docs/audits/DRIFT-REPORT-W11.md`, `docs/rfcs/EXECUTION-SEQUENCE.md`

---

## Objetivo

Manter uma especificacao de produto alinhada ao estado real do runtime: o que ja esta implementado, o que esta parcialmente implementado e o que permanece planejado.

## Escopo

- Definir baseline funcional e operacional do runtime extremo.
- Consolidar SLOs, gates e criterios de aceite de programa.
- Servir como referencia de produto para ADRs/RFCs e checkpoints PREVC.

## Nao-Escopo

- Substituir ADRs e RFCs detalhadas.
- Definir arquitetura nova fora das decisoes ja registradas.
- Mudar comportamento de runtime nesta rodada doc-first.

## Estado Atual (as-of 2026-02-13)

| Capability | Status | Referencia |
|---|---|---|
| Config/validation fail-fast + defaults | Implemented | `internal/shared/config/schema.go:17`, `internal/shared/config/loader_test.go:656` |
| Liveness/readiness HTTP probes | Implemented | `internal/interfaces/http/server_test.go:260` |
| Runtime supervision/restart limiter | Implemented | `internal/actors/runtime/guardian.go:14`, `internal/actors/runtime/guardian_test.go:315` |
| JetStream publish/consume + durable restart | Implemented | `internal/adapters/jetstream/consumer_integration_test.go:21` |
| ACK/NAK/TERM conformance | Implemented | `internal/adapters/jetstream/ingest_conformance_test.go:15` |
| Deterministic replay and golden tests | Implemented | `internal/shared/replay/golden_test.go:18`, `cmd/consumer/replay_test.go:63` |
| Multi-exchange runtime readiness | Partially Implemented | `cmd/consumer/e2e_consumer_integration_test.go:24`, `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md` |
| Protobuf contract layer (tooling+authority) | Partially Implemented | `internal/shared/contracts/authority_test.go:268`, `docs/adrs/ADR-0016-protobuf-contract-layer.md` |
| Operational soak maturity evidence | Partially Implemented | `Makefile:142`, `.context/evidence/w5-soak.txt` |
| Contract-layer runtime completion (W13) | Planned | `docs/rfcs/EXECUTION-SEQUENCE.md` |

## Program Goals (SLO-driven)

| SLI | Target |
|---|---|
| Ingest latency | p50 < 1ms, p99 < 10ms |
| Recovery time | < 5s por subsistema |
| Duplicacao em fluxo operacional | 0 duplicatas observaveis |
| Shutdown graceful | < 10s |
| Replay deterministico | output estavel para fixtures equivalentes |

## Validation Gates

```bash
make invariants-check
make test-workspace
make test-workspace-race
make proto-lint
make proto-breaking
make soak-check
```

Benchmarks operacionais padrao:

```bash
go test -run '^$' -bench BenchmarkIngest ./internal/core/marketdata/app
go test -run '^$' -bench BenchmarkApplyDelta ./internal/core/aggregation/...
```

## Implementation Matrix (Program)

| Work Package | Status | Source of Truth |
|---|---|---|
| W1-W3 | Implemented | `docs/rfcs/RFC-0001-robustness-roadmap.md` |
| W4 | Implemented | `docs/rfcs/RFC-0005-W4-observability-profiling.md` |
| W5 | Implemented | `docs/rfcs/RFC-0006-W5-memory-lifecycle-hardening.md` |
| W6 | Partially Implemented | `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md` |
| W7 | Partially Implemented | `docs/rfcs/RFC-0008-W7-nats-jetstream-integration.md` |
| W8 | Implemented | `docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md` |
| W9 | Partially Implemented | `docs/rfcs/RFC-0010-W9-multi-exchange-readiness.md` |
| W10 | Implemented | `docs/rfcs/EXECUTION-SEQUENCE.md` |
| W11 | Implemented | `docs/architecture/TRUTH-MAP.md`, `docs/audits/DRIFT-REPORT-W11.md` |
| W12 | Implemented | `docs/rfcs/EXECUTION-SEQUENCE.md` |
| W13 | Planned | `docs/rfcs/EXECUTION-SEQUENCE.md` |

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Divergencia entre PRD e evidencias de runtime | Alto | Sincronizar PRD com `TRUTH-MAP` a cada rodada PREVC |
| Taxonomia inconsistente de status | Medio | Manter matriz com `Implemented/Partially Implemented/Planned` |
| Checklist operacional sem comando real | Medio | Referenciar apenas alvos existentes no `Makefile` |

## Evidence

- `docs/architecture/TRUTH-MAP.md`
- `docs/audits/DRIFT-REPORT-W11.md`
- `docs/rfcs/EXECUTION-SEQUENCE.md`
- `internal/interfaces/http/server_test.go:330`
- `internal/adapters/jetstream/consumer_integration_test.go:21`
- `internal/adapters/jetstream/ingest_conformance_test.go:15`
- `internal/shared/replay/golden_test.go:18`
- `cmd/consumer/e2e_consumer_integration_test.go:24`

## Historical Note

O snapshot detalhado anterior foi preservado no historico Git e substituido aqui por uma versao normalizada e anti-drift para governanca operacional.

## Changelog

- 2026-02-12:
- PRD consolidado como draft de programa extremo.

- 2026-02-13:
- Reclassificado para `Active`.
- Snapshot alinhado ao estado real do runtime.
- Matriz de implementacao e gates canonicos consolidados.
