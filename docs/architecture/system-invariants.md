# System Invariants Index

**Status:** Active
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-17

---

## Purpose

Este documento e o indice vivo de invariantes operacionais do runtime.
Cada invariante referencia:
- decisao arquitetural (ADR/RFC)
- evidencia de codigo e teste
- gate de validacao executavel

## Live Invariants

| Invariant ID | Rule | Authority | Evidence | Gate |
|---|---|---|---|---|
| INV-DOM-01 | `internal/core/*`, `internal/actors/*`, `internal/interfaces/*` devem permanecer protobuf-free | `docs/adrs/ADR-0016-protobuf-contract-layer.md` | `scripts/ci/check-domain-isolation.sh:13`, `scripts/ci/check-domain-isolation.sh:49` | `make invariants-check` |
| INV-DET-01 | `internal/core/*` nao pode chamar `time.Now()` diretamente | `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` | `scripts/ci/check-domain-isolation.sh:56`, `scripts/ci/check-domain-isolation.sh:73` | `make invariants-check` |
| INV-REP-01 | `internal/shared/replay` deve ficar offline (sem dependencia de NATS) | `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md` | `scripts/ci/check-domain-isolation.sh:83`, `scripts/ci/check-domain-isolation.sh:102` | `make invariants-check` |
| INV-BUS-01 | Subject taxonomy deve manter familia/versionamento validos | `docs/adrs/ADR-0014-stream-partitioning-strategy.md` | `internal/adapters/jetstream/subject_validation.go:24`, `internal/adapters/jetstream/subject_validation_test.go:5` | `make test-workspace` |
| INV-ACK-01 | Fluxo ingest JetStream deve manter semantica ACK/NAK/TERM | `docs/adrs/ADR-0004-bus-nats-jetstream.md` | `internal/adapters/jetstream/ingest_conformance_test.go:15` | `make test-workspace` |
| INV-CONTRACT-01 | Registry de contratos deve ser autoridade de schemas protobuf ativos | `docs/adrs/ADR-0016-protobuf-contract-layer.md` | `proto/registry.json`, `internal/shared/contracts/authority_test.go:268` | `make proto-lint` + `make proto-breaking` |
| INV-TOPO-01 | Guardian deve aplicar readiness por expected subsystems e restart budget | `docs/adrs/ADR-0018-actor-topology-supervision-model.md` | `internal/actors/runtime/guardian_test.go:315`, `internal/actors/runtime/guardian_test.go:436` | `make test-workspace-race` |
| INV-MEX-01 | Identidade de stream deve incluir `venue+instrument+market_type` | `docs/adrs/ADR-0017-multi-exchange-normalization.md` | `internal/core/marketdata/domain/instrument_stream.go:30`, `cmd/consumer/e2e_consumer_integration_test.go:24` | `make test-workspace-race` |
| INV-LAY-01 | `internal/core/*` nao pode importar `internal/actors` | `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md` | `scripts/ci/check-domain-isolation.sh` | `make invariants-check` |
| INV-LAY-02 | `internal/interfaces/*` nao pode importar `internal/adapters` | `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md` | `scripts/ci/check-domain-isolation.sh` | `make invariants-check` |
| INV-LAY-03 | `internal/core/*` nao pode importar `internal/shared/policykit` | `docs/adrs/ADR-0013-backpressure-overload-policies.md` | `scripts/ci/check-domain-isolation.sh` | `make invariants-check` |
| INV-LAY-04 | `internal/core/*` nao pode importar `internal/adapters` | `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md` | `scripts/ci/check-domain-isolation.sh` | `make invariants-check` |
| INV-LAY-05 | `internal/core/*` nao pode importar `internal/interfaces` | `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md` | `scripts/ci/check-domain-isolation.sh` | `make invariants-check` |
| INV-LAY-06 | `internal/actors/*` nao pode importar `internal/interfaces` | `docs/adrs/ADR-0001-bounded-contexts-and-boundaries.md` | `scripts/ci/check-domain-isolation.sh` | `make invariants-check` |

## Standard Validation Gates

```bash
make invariants-check
make test-workspace
make test-workspace-race
make proto-lint
make proto-breaking
make soak-check
```

Replay determinism evidence (quando aplicavel):

```bash
go test ./internal/shared/replay -run TestGoldenReplay
go test ./cmd/consumer -run TestReplayIngestGolden1000
```

## Evidence Maintenance Rules

- Toda nova afirmacao de invariante precisa adicionar pelo menos um anchor de teste ou script.
- Se um item estiver parcialmente implementado, a ADR/RFC correspondente deve usar `Status: Partially Implemented` e `Implementation Matrix`.
- `docs/architecture/TRUTH-MAP.md` e `docs/audits/DRIFT-REPORT-W11.md` devem continuar como baseline de reconciliação doc vs runtime.

## Changelog

- 2026-02-17:
  - Adicionados INV-LAY-04 (core->adapters), INV-LAY-05 (core->interfaces), INV-LAY-06 (actors->interfaces).
  - Adicionados INV-LAY-01 (core->actors), INV-LAY-02 (interfaces->adapters), INV-LAY-03 (core->policykit).
- 2026-02-13:
  - Reescrito como indice vivo de invariantes.
  - Conteudo legacy de bootstrap removido.
  - Cross-links para ADRs, testes e gates reais adicionados.
