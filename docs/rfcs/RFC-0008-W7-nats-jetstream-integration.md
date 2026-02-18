# RFC-0008 - W7: NATS JetStream Integration

**Status:** Accepted
**Implementation status:** Partially Implemented
**Partial marker:** Status: Partially Implemented
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W7 of PRD-0001
**Relates to:** ADR-0004, ADR-0014, ADR-0016, `docs/rfcs/EXECUTION-SEQUENCE.md`

---

## Objetivo

Formalizar integração JetStream como backbone durável de eventos, com dedup por `Msg-ID`, recuperação por consumer durável e política de ingest ACK/NAK/TERM verificável.

## Escopo

- Publisher e consumer JetStream.
- Subject taxonomy e validação em publish/filter.
- Dedup por `idempotency_key`.
- Conformidade de ingest (ACK/NAK/TERM + quarantine).
- Compatibilidade com `-bus=inmemory` para dev/test.

## Nao-Escopo

- Mudar default de produção para JetStream nesta rodada.
- Completar migração protobuf runtime end-to-end.
- Multi-cluster NATS e topologias avançadas de infra.

## Design

### Subject model
- Subject de publish: `{event}.v{version}.{venue_lower}.{instrument_alnum_upper}`.
- Validação obrigatória no publisher e nos filtros de consumer.

### Delivery semantics
- Mensagem válida processada: `Ack()`.
- Falha transitória: `Nak()`.
- Mensagem poison/irrecuperável: `Term()` + quarantine quando aplicável.

### Deduplication
- `idempotency_key` propagado como `Nats-Msg-Id`.
- Janela de dedup controlada pelo stream (`DedupWindow`).

### Compatibility
- Runtime mantém opção `inmemory` para regressão/local.
- JetStream e InMemory coexistem sob escolha de configuração.

## Rollout

| Fase | Status | Referencia |
|---|---|---|
| W7-1: publisher/consumer + durable restart | Implemented | `internal/adapters/jetstream/consumer_integration_test.go:21` |
| W7-2: política ACK/NAK/TERM + quarantine | Implemented | `internal/adapters/jetstream/ingest_conformance_test.go:15` |
| Default production switch para jetstream | Planned | `docs/rfcs/EXECUTION-SEQUENCE.md` |
| Fechamento protobuf runtime com JetStream | Planned | `docs/adrs/ADR-0016-protobuf-contract-layer.md` |

## Test Plan

Obrigatório para mudanças W7:

```bash
make invariants-check
make test-workspace
make test-workspace-race
go test -tags=integration ./internal/adapters/jetstream -count=1
```

Validações específicas:
- durable restart sem perda de mensagens.
- dedup por `Msg-ID`.
- subject gerado e validado no formato canônico.
- política ACK/NAK/TERM conforme tabela de conformidade.

## Acceptance

- Publisher publica com subject canônico e `Nats-Msg-Id`.
- Consumer durável suporta restart com recuperação de mensagens.
- Semântica ACK/NAK/TERM é estável e testada.
- `-bus=inmemory` permanece funcional para regressão.
- Config JetStream validada no loader sem quebrar defaults legados.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Drift entre doc e taxonomia real de subject | Alto | usar `SubjectFromEnvelope` + `ValidateSubjectTaxonomy` como autoridade |
| Retry infinito em poison | Alto | `Term()` explícito e quarantine com classificação de erro |
| Regressão em modo `inmemory` | Medio | manter testes de regressão e caminho de configuração legado |

## Implementation Matrix

| Capability | Status | Reference |
|---|---|---|
| Publish subject validation | Implemented | `internal/adapters/jetstream/publisher.go:86` |
| Subject derivation from envelope | Implemented | `internal/shared/envelope/subject.go:9`, `internal/adapters/jetstream/publisher_integration_test.go:64` |
| Durable consumer restart | Implemented | `internal/adapters/jetstream/consumer_integration_test.go:21` |
| Dedup by Msg-ID | Implemented | `internal/adapters/jetstream/publisher_integration_test.go:41` |
| ACK/NAK/TERM conformance table | Implemented | `internal/adapters/jetstream/ingest_conformance_test.go:15` |
| Quarantine ACL failure classification | Implemented | `internal/adapters/jetstream/ingest_conformance_test.go:129` |
| Default production switch to JetStream | Planned | `docs/rfcs/EXECUTION-SEQUENCE.md` |
| Proto payload as operational default on bus | Planned | `docs/adrs/ADR-0016-protobuf-contract-layer.md` |

## Evidence

- `internal/shared/envelope/subject.go:9`
- `internal/adapters/jetstream/subject_validation.go:24`
- `internal/adapters/jetstream/consumer_integration_test.go:21`
- `internal/adapters/jetstream/publisher_integration_test.go:41`
- `internal/adapters/jetstream/publisher_integration_test.go:64`
- `internal/adapters/jetstream/ingest_conformance_test.go:15`
- `internal/shared/config/schema.go:37`

## Changelog

- 2026-02-12:
- RFC criada para integração JetStream W7.

- 2026-02-13:
- Normalização para contrato RFC doc-first.
- Marcador explícito de implementação parcial.
- Matriz e evidências alinhadas ao estado executável atual.
