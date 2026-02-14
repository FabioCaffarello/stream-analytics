# Execution Sequence - W4 through W13

**Status:** Accepted
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-13
**PRD:** `docs/prd/PRD-0001-extreme-runtime.md`
**Relates to:** `docs/architecture/TRUTH-MAP.md`, `docs/audits/DRIFT-REPORT-W11.md`

---

## Objetivo

Registrar a sequencia de execucao W4..W13 com gates reais, evidencia verificavel e status sem checklist fantasma.

## Escopo

- Consolidar status de entrega dos work packages W4..W13.
- Definir gates canonicos por tipo de mudanca (core, replay, schema, soak).
- Preservar o historico de evidencias sem reabrir decisoes arquiteturais.

## Nao-Escopo

- Replanejar o roadmap de produto.
- Alterar runtime nesta rodada de governanca.
- Inventar novos gates que nao existam no workspace.

## Design

O documento segue tres principios:

1. **Truth-first:** status so e `Implemented` quando existe anchor de codigo/teste/gate.
2. **Gate-by-scope:** nem todo gate roda em todo patch; cada pacote ativa gates especificos.
3. **No ghost checklist:** comandos listados precisam existir no `Makefile` ou em testes identificados.

Gate authority (workspace atual):
- `make docs-check`
- `make invariants-check`
- `make test-workspace`
- `make test-workspace-race`
- `make proto-lint`
- `make proto-breaking`
- `make soak-check`

## Rollout

| Package | Goal | Status | Primary Evidence |
|---|---|---|---|
| W4 | Observability + profiling | Implemented | `internal/interfaces/http/server_test.go:1` |
| W5 | Lifecycle hardening + boundedness | Implemented | `internal/actors/runtime/guardian_test.go:315` |
| W6 | Protobuf foundation | Partially Implemented | `internal/shared/contracts/semantic_equivalence_test.go:13`, `docs/adrs/ADR-0016-protobuf-contract-layer.md` |
| W7 | JetStream integration | Implemented | `internal/adapters/jetstream/consumer_integration_test.go:21` |
| W8 | Deterministic replay | Implemented | `internal/shared/replay/golden_test.go:1`, `cmd/consumer/replay_test.go:63` |
| W9 | Multi-exchange readiness | Partially Implemented | `cmd/consumer/e2e_consumer_integration_test.go:24`, `docs/adrs/ADR-0017-multi-exchange-normalization.md` |
| W10 | Insights hardening evidence | Implemented | `docs/rfcs/EXECUTION-SEQUENCE.md` (historico consolidado em Git) |
| W12 | Operational maturity gate | Implemented | `Makefile:156`, `scripts/soak-test.sh` |
| W13 | Contract layer completion | Planned | `docs/adrs/ADR-0016-protobuf-contract-layer.md` |

## Test Plan

### Mandatory for governance/runtime safety

```bash
make docs-check
make invariants-check
make test-workspace
make test-workspace-race
```

### Required when proto/contracts are touched

```bash
make proto-lint
make proto-breaking
```

### Required when replay behavior is touched

```bash
go test ./internal/shared/replay -run TestGoldenReplay
go test ./cmd/consumer -run TestReplayIngestGolden1000
```

### Required for operational maturity checkpoints

```bash
make soak-check
```

### W2 commit-driven hard-order gates (cold-path correctness)

```bash
make commit-msg-check
make docs-check-full
make invariants-check
make registry-check
```

When C1+ touches `internal/adapters/storage/**`, also run:

```bash
make test-unit
make test-workspace
```

## Acceptance

- Todos os itens ja implementados estao marcados como `Implemented`.
- Itens incompletos estao marcados como `Partially Implemented` ou `Planned`.
- Nao existe referencia a comandos inexistentes no workspace.
- Gates refletem estado real de `Makefile` e suites de teste atuais.
- `make docs-check` e o gate inicial de governanca documental.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Tratar evidencia historica como "done" sem reteste local | Medio | manter anchors de teste e gates obrigatorios por escopo |
| Misturar backlog futuro com pacote entregue | Medio | separar `Partially Implemented` e `Planned` na matriz |
| Divergencia com TRUTH-MAP em rodadas futuras | Alto | sincronizar este RFC sempre que `TRUTH-MAP` mudar estado canonicamente |

## Implementation Matrix

| Capability | Status | Reference |
|---|---|---|
| Documentation guardrails (headers, links, truth-map) | Implemented | `Makefile`, `scripts/check-doc-headers.sh`, `scripts/check-doc-links.sh`, `scripts/check-truth-map.sh` |
| Domain isolation + determinism guards | Implemented | `Makefile:123`, `scripts/check-domain-isolation.sh:49`, `scripts/check-domain-isolation.sh:107` |
| Workspace-wide tests (`go test` all modules) | Implemented | `Makefile:136`, `Makefile:139` |
| JetStream durability/restart | Implemented | `internal/adapters/jetstream/consumer_integration_test.go:21` |
| JetStream dedup by `Msg-ID` | Implemented | `internal/adapters/jetstream/publisher_integration_test.go:41` |
| Replay determinism golden | Implemented | `internal/shared/replay/golden_test.go:1` |
| Multi-exchange process validation | Implemented | `cmd/consumer/e2e_consumer_integration_test.go:24` |
| Protobuf schema authority/toolchain | Partially Implemented | `Makefile:217`, `Makefile:224`, `internal/shared/contracts/authority_test.go:268` |
| Long-run soak evidence policy | Partially Implemented | `Makefile:156`, `scripts/soak-test.sh` |
| Contract-layer runtime completion (W13) | Planned | `docs/adrs/ADR-0016-protobuf-contract-layer.md` |

## Evidence

- Truth and drift baselines:
- `docs/architecture/TRUTH-MAP.md`
- `docs/audits/DRIFT-REPORT-W11.md`

- Gate anchors:
- `Makefile`
- `Makefile:123`
- `Makefile:136`
- `Makefile:139`
- `Makefile:142`
- `Makefile:217`
- `Makefile:224`

- Key runtime tests:
- `internal/actors/runtime/guardian_test.go:315`
- `internal/actors/runtime/guardian_test.go:436`
- `internal/adapters/jetstream/ingest_conformance_test.go:15`
- `internal/shared/replay/golden_test.go:1`
- `cmd/consumer/e2e_consumer_integration_test.go:24`

## Changelog

- 2026-02-13:
- Documento normalizado para contrato RFC.
- Gates reais confirmados com `Makefile`.
- Gate inicial de docs adicionado (`make docs-check`) para prevenir drift documental.
- Checklist fantasma removido (`golden-check`, `audit-core-purity`).
- Matriz de implementacao adicionada para diferenciar entregue/parcial/planejado.
