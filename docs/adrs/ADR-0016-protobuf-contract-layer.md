# ADR-0016 - Protobuf Contract Layer

**Status:** Accepted
**Implementation status:** Partially Implemented
**Partial marker:** Status: Partially Implemented
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-13
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 section E.6, `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md`, `docs/rfcs/EXECUTION-SEQUENCE.md`

---

## Contexto

O runtime em producao opera com protobuf como caminho padrao de payload (`bus.wire_format=proto` e `marketdata.publish_content_type=application/protobuf`), com fallback/compatibilidade JSON ainda suportado. A camada de contratos protobuf foi entregue por etapas (W6-1+): schemas versionados, geracao de codigo e gates de compatibilidade.

O drift identificado no W11 e de governanca: o texto misturava "fundacao entregue" com "migracao runtime completa", sem separar claramente o que e verdade hoje vs o que permanece planejado.

## Decisao

1. Manter esta ADR como **decisao proposta** ate a migracao de publish/consume protobuf em runtime estar validada end-to-end.
2. Tratar W6-1 como **escopo implementado** e obrigatorio nos gates:
- `make proto-lint`
- `make proto-gen`
- `make proto-breaking`
3. Preservar o boundary de dominio:
- protobuf permitido apenas em `internal/shared/*`
- proibido em `internal/core/*`, `internal/actors/*`, `internal/interfaces/*`
4. Definir `proto/registry.json` como autoridade do inventario de schemas.
5. Registrar explicitamente itens ainda pendentes para W6-2/W6-3 em matriz de implementacao.

## Consequencias

- Positivas:
- Contratos protobuf passam a ter fonte canonica versionada e auditavel.
- Drift de status entre ADR/RFC e codigo fica explicitado.
- Gates de schema passam a ser rastreaveis em um unico documento.

- Negativas:
- Decisao final continua aberta enquanto runtime protobuf opt-in nao fecha E2E.
- Equipe precisa operar dois caminhos de payload durante a transicao (json/protobuf).

## Invariantes

- `PROTO-1`: `buf lint` deve passar para alteracoes em `proto/*`.
- `PROTO-2`: `buf breaking` deve proteger compatibilidade wire contra `main`.
- `PROTO-3`: serializacao JSON e protobuf devem manter equivalencia semantica dos payloads suportados.
- `PROTO-4`: `proto/registry.json` deve cobrir os contratos versionados ativos.
- `PROTO-5`: boundary protobuf-free no dominio deve ser preservado por `make invariants-check`.

## Compatibility Rules

1. Toda mudanca em `.proto` deve manter backward compatibility por default.
2. Reuso de field number e proibido, mesmo para campo removido.
3. Campo removido deve virar `reserved` (numero e nome).
4. Adicao permitida apenas para campos opcionais/repeated.
5. Renomear campo sem `reserved` e breaking change.
6. Troca de tipo wire-incompatible e breaking change.
7. `oneof` novo so pode adicionar variantes sem remover existentes.
8. `package` e `go_package` sao estaveis por versao major.
9. `type` + `version` em `proto/registry.json` sao autoridade canonica.
10. Conversores `Domain<->Proto` devem preservar equivalencia semantica.
11. `content_type=application/protobuf` e o caminho operacional default.
12. Caminho `application/json` permanece suportado para compatibilidade/fallback.
13. Decoder deve rejeitar `content_type` desconhecido explicitamente.
14. Unknown event type em proto deve falhar com erro de validacao.
15. Unknown event type em json segue fallback policy configurada.
16. Dominio (`internal/core/*`) nao importa protobuf runtime.
17. Gates minimos de contrato: `proto-lint` + `proto-breaking`.
18. Replay/determinismo nao pode depender de codec selecionado.
19. Flags default OFF nao podem alterar golden existente.
20. Qualquer quebra de contrato deve subir `version` explicitamente.

## Implementation Matrix

| Feature | Status | Referencia |
|---|---|---|
| Schemas v1 em `proto/` + manifest authority | Implemented | `proto/envelope/v1/envelope.proto`, `proto/marketdata/v1/trade.proto`, `proto/registry.json` |
| Toolchain Buf (`lint/gen/breaking`) | Implemented | `Makefile:217`, `Makefile:220`, `Makefile:224` |
| Registro de payload com codecs JSON/Proto | Implemented | `internal/shared/contracts/payload_registry.go`, `internal/shared/contracts/semantic_equivalence_test.go:13` |
| `ContentType` com suporte protobuf no envelope/codec | Implemented | `internal/shared/envelope/envelope.go`, `internal/shared/codec/payload_codec_test.go:17` |
| VPVR snapshot proto opt-in (default OFF) | Implemented | `proto/insights/v1/volume_profile.proto`, `internal/shared/contracts/insights_registry.go`, `internal/shared/contracts/insights_registry_test.go` |
| Publish/consume protobuf como caminho operacional padrao | Planned | `docs/rfcs/RFC-0007-W6-protobuf-contract-layer.md`, `docs/rfcs/EXECUTION-SEQUENCE.md` |
| Evidencia negativa formal de `proto-breaking` (campo removido) | Planned | `docs/rfcs/EXECUTION-SEQUENCE.md` |

## Evidence

- Build and schema gates:
- `Makefile:217`
- `Makefile:220`
- `Makefile:224`

- Contract authority and equivalence:
- `internal/shared/contracts/authority_test.go:268`
- `internal/shared/contracts/semantic_equivalence_test.go:13`
- `internal/shared/codec/payload_codec_test.go:17`

- Envelope content type behavior:
- `internal/shared/envelope/envelope.go:14`
- `internal/shared/envelope/envelope_test.go:96`

- Domain boundary guard:
- `scripts/ci/guards/check-domain-isolation.sh:13`
- `scripts/ci/guards/check-domain-isolation.sh:49`

## Changelog

- 2026-02-12:
- ADR criada com decisao de adotar protobuf + Buf.
- W6-1 registrado como fundacao entregue sem corte de runtime.

- 2026-02-13:
- Normalizacao governance doc-first.
- Status explicito para implementacao parcial.
- Inclusao de `Implementation Matrix` e `Evidence` com anchors de codigo/teste.
