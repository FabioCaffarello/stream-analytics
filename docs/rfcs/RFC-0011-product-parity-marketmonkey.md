# RFC-0011 - Product Parity v1 (MarketMonkey-Inspired)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Date:** 2026-02-13
**Author:** Product Architect
**Relates to:** `docs/prd/PRD-0001-extreme-runtime.md`, `docs/audits/AUDIT-PACK-W11-finalization.md`, `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0006-storage-hot-vs-cold.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0016-protobuf-contract-layer.md`, `docs/adrs/ADR-0018-actor-topology-supervision-model.md`, `docs/contracts/event-bus.md`

## Objetivo

Consolidar parity v1 em modo doc-first, alinhando arquitetura de storage/orderbook/heatmap/volume/liquidations/markprice/delivery com:
- ADRs e PRD vigentes;
- evidencias reais de codigo/testes;
- plano incremental sem implementar novas features nesta rodada.

## Escopo

- Corrigir drift documental e explicitar limites `Existing/Planned/TODO`.
- Padronizar invariantes, acceptance tests e evidence hooks nos docs de parity.
- Definir gates de aceite P0/P1/P2 com criterios testaveis.

## Nao-Escopo

- Implementacao runtime de storage adapters, novos contratos ou novas pipelines.
- Mudanca silenciosa de ADR: conflitos permanecem rastreados em `docs/rfcs/ADR-REVISIONS-patch-plan.md`.
- Alteracao de postura de risco regulatorio do produto.

## Gap & Drift Checklist (P0/P1/P2)

| ID | Sev | Gap/Drift | Impacto | Status nesta rodada |
|---|---|---|---|---|
| GD-01 | P0 | Contrato WS documentava `content_type` no frame de evento, mas runtime atual nao envia esse campo | risco de contrato wire incorreto para cliente | RESOLVIDO: docs WS agora refletem frame atual + extensao planejada opcional |
| GD-02 | P0 | `aggregation.*` aparece como subject planejado, mas validator atual aceita roots `marketdata|insights|quarantine` | risco de contradicao ADR/runtime em rollout de novos tipos | OPEN QUESTION: mantido com ADR-REVISION NOTE (NOTE-001) |
| GD-03 | P0 | Storage docs podiam sugerir L1/L2 como existentes, conflitante com ADR-0006 (L0 memoria ativo) | risco operacional de expectativa incorreta | RESOLVIDO: docs marcam L0 `Existing`, L1/L2 `Planned/TODO` |
| GD-04 | P0 | Campos obrigatorios de envelope estavam incompletos em alguns textos de parity | risco de contrato parcial | RESOLVIDO: obrigatorios alinhados a ADR-0002 |
| GD-05 | P1 | Acceptance tests sem path/test id padronizado | baixa auditabilidade de aceite | RESOLVIDO: todos docs parity incluem testes existentes e/ou lista TODO com nome+path |
| GD-06 | P1 | Falta de matriz uniforme `Feature -> Status -> Evidence -> Tests` | baixa rastreabilidade de implementacao | RESOLVIDO em todos docs de parity |
| GD-07 | P1 | Failure modes sem separar estado atual vs mitigacao planejada | risco de observabilidade insuficiente | RESOLVIDO com reforco de mitigacao e boundaries |
| GD-08 | P1 | Backpressure e ack boundary descritos genericamente | risco de politica nao testavel | RESOLVIDO com referencias diretas a ADR-0013 e testes existentes |
| GD-09 | P2 | Terminologia misturada (instrument/symbol/subject/stream/envelope/payload) | ambiguidade de leitura | RESOLVIDO com secoes de terminologia em cada doc |
| GD-10 | P2 | TRUTH-MAP sem amarracao explicita por tema parity | governanca incompleta para W12/W13 | RESOLVIDO com atualizacao de temas parity + gates |

## Review Matrix (Padrao por Documento)

| Documento | Terminologia | Contratos | Data Planes | Invariantes | Observability | Acceptance Tests |
|---|---|---|---|---|---|---|
| `docs/architecture/storage.md` | Alinhado | ADR-0002 + ACK boundary explicito | L0 Existing; L1/L2 Planned | single-writer, boundedness, determinism | lag/drop/queue + TODO storage metrics | Existing + TODO with paths |
| `docs/architecture/orderbook.md` | Alinhado | snapshot/inconsistent contract separando runtime vs planned bus | input/output/storage explicitos | seq monotonic, crossed-book, bounded levels | queue/degrade policy | Existing + TODO with paths |
| `docs/architecture/heatmap.md` | Alinhado | payload budget e bucket contract | input/derived/storage explicitos | deterministic buckets, closed-window immutability | lag/drop/queue com TODO metrics | Existing baseline + TODO with paths |
| `docs/architecture/volume-profiles.md` | Alinhado | VPVR contract v1 explicitado | input/derived/storage explicitos | additivity, POC consistency, cardinality cap | lag/drop/queue | Existing baseline + TODO with paths |
| `docs/architecture/liquidations-markprice.md` | Alinhado | dedup and normalization contracts | input/output/storage explicitos | priority + dedup deterministico + replay | lag/drop/queue | Existing + TODO with paths |
| `docs/contracts/delivery-ws.md` | Alinhado | wire contract corrigido (frame atual vs extensao) | bus vs ws plane separados | WS session isolation + routing invariants | metrics required + implementation gap explicit | Existing + TODO with paths |

## Design

### Feature Set v1 (required order)

1. Storage foundation (L0 active + L1/L2 contract readiness)
2. Orderbook snapshots + delivery contract hardening
3. Heatmap derivation + persistence plan
4. Volume profile (VPVR) + range aggregations
5. Liquidations/MarkPrice end-to-end plan

### Dependencies by Feature

| Feature | ADR/RFC Dependencies |
|---|---|
| Storage | ADR-0002, ADR-0004, ADR-0006, ADR-0013, ADR-0014, ADR-0015 |
| Orderbook | ADR-0002, ADR-0005, ADR-0013, ADR-0014, ADR-0015 |
| Heatmap | ADR-0002, ADR-0006, ADR-0013, ADR-0014, ADR-0015 |
| Volume Profiles | ADR-0002, ADR-0006, ADR-0013, ADR-0014, ADR-0015, ADR-0017 |
| Liquidations/MarkPrice | ADR-0002, ADR-0004, ADR-0011, ADR-0013, ADR-0015, ADR-0017 |
| Delivery WS | ADR-0002, ADR-0007, ADR-0013, ADR-0014, RFC-0003 |

### Implementation Matrix (Parity v1)

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Bus/event envelope authority and subject validation | Existing | `docs/contracts/event-bus.md`, `internal/adapters/jetstream/subject_validation.go` | `internal/adapters/jetstream/subject_validation_test.go` |
| Replay determinism baseline | Existing | `internal/shared/replay/player.go`, `internal/shared/replay/sequencer.go` | `internal/shared/replay/golden_test.go`, `cmd/consumer/replay_test.go:TestReplayIngestGolden1000` |
| Delivery session/router baseline | Existing | `internal/actors/delivery/runtime/session.go`, `internal/actors/delivery/runtime/router.go` | `internal/actors/delivery/runtime/session_test.go`, `internal/actors/delivery/runtime/router_test.go` |
| Storage L1/L2 adapters | TODO | `internal/adapters/storage/` (TODO) | `internal/adapters/storage/*_test.go` (TODO) |
| Heatmap/VPVR builders and writers | TODO | `internal/core/insights/app/build_heatmap.go` (TODO), `internal/core/insights/app/build_volume_profile.go` (TODO) | dedicated TODO tests by path in each architecture doc |
| MarkPrice/Liquidation dedicated pipeline | TODO | `internal/core/marketdata/app/normalize_markprice_liquidation.go` (TODO) | `internal/actors/marketdata/runtime/markprice_liquidation_pipeline_test.go` (TODO) |

## Incremental Commit Plan

1. `docs(parity): add gap and drift checklist with severity and review matrix`
2. `docs(storage): align storage planes and contracts with ADR-0002/0006`
3. `docs(delivery): align ws contract wire fields and backpressure status`
4. `docs(invariants): refresh orderbook heatmap volume liquidations docs with implementation matrices`
5. `docs(rfc-0011): define gates dependencies and definition of done by feature`

## Rollout

### P0 - Contract and Storage Foundation

Scope:
- `docs/architecture/storage.md`
- `docs/contracts/delivery-ws.md`
- `docs/rfcs/ADR-REVISIONS-patch-plan.md` (apenas notas de conflito)

Gates (must pass):
- `make invariants-check`
- `make test-workspace`

Acceptance gates:
- contratos wire e envelope sem contradicao com ADR-0002/0004/0006;
- ack boundary documentado como `ack-on-commit`;
- Implementation Matrix completa em storage/delivery.

### P1 - Orderbook + Heatmap

Scope:
- `docs/architecture/orderbook.md`
- `docs/architecture/heatmap.md`

Gates (must pass):
- `make invariants-check`
- `make test-workspace`
- `make test-workspace-race`

Acceptance gates:
- invariantes de determinismo/replay/backpressure explicitos;
- acceptance tests citam testes existentes e TODOs com path;
- observabilidade minima (lag/drop/queue) definida.

### P2 - Volume Profiles + Liquidations/MarkPrice

Scope:
- `docs/architecture/volume-profiles.md`
- `docs/architecture/liquidations-markprice.md`
- `docs/architecture/TRUTH-MAP.md`

Gates (must pass):
- `make invariants-check`
- `make test-workspace`
- `make test-workspace-race`

Acceptance gates:
- dedup/cardinality boundaries documentados e testaveis;
- failure modes com mitigacao operacional;
- TRUTH-MAP atualizado por tema parity.

## Definition of Done by Feature

### Storage
- `Implementation Matrix` com `Existing/Planned/TODO`.
- acceptance tests com paths para existentes/TODO.
- divergencias com ADR-0006 resolvidas no texto ou rastreadas em ADR-REVISION NOTE.

### Orderbook
- invariantes de monotonicidade/crossed-book/max-level explícitos.
- replay golden e ack boundary descritos com testes existentes.
- contratos planejados de snapshot/inconsistent marcados como planned/TODO.

### Heatmap
- contrato de bucket/timeframe/payload budget definido.
- backpressure degradacao progressiva definida.
- lista de testes a criar com paths reais.

### Volume Profiles
- contrato de bucket/POC/value area definido.
- cardinality cap e replay deterministico descritos.
- lista de testes a criar com paths reais.

### Liquidations/MarkPrice
- dedup keys e prioridade de overload documentadas.
- normalizacao multi-exchange sem conflito com envelope/subject taxonomy.
- lista de testes existentes e TODO com paths.

### Delivery WS
- contrato wire atual refletindo implementacao.
- extensoes futuras marcadas como opcionais/TODO.
- testes de sessao/roteador existentes citados + gaps de backpressure/range e2e mapeados.

## Test Plan

Mandatory cross-cut validation:
```bash
make invariants-check
make test-workspace
make test-workspace-race
```

Schema compatibility (only when new proto contracts are materialized):
```bash
make proto-lint
make proto-breaking
```

## Acceptance

- Gap & Drift checklist P0/P1/P2 publicado e classificado.
- Revisao padronizada por documento concluida.
- Plano de commits incrementais definido (5 pequenos commits).
- Gates de aceite e Definition of Done por feature consolidados.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| subject root conflict for planned `aggregation.*` | High | manter ADR-REVISION NOTE ate decisao de root support |
| docs declararem feature como existente sem evidencia | High | obrigatoriedade de `Implementation Matrix` com path de teste |
| drift entre WS frame doc e runtime | High | contrato separado em current vs planned e revisao em cada rodada PREVC |
| cold-path assumptions sem adapter real | Medium | manter L1/L2 como planned/TODO e validar apenas gates existentes |

## Evidence

- `docs/architecture/storage.md`
- `docs/architecture/orderbook.md`
- `docs/architecture/heatmap.md`
- `docs/architecture/volume-profiles.md`
- `docs/architecture/liquidations-markprice.md`
- `docs/contracts/delivery-ws.md`
- `docs/architecture/TRUTH-MAP.md`

## Changelog

- 2026-02-13:
- RFC atualizado para parity doc-hardening em PREVC.
- Gap & Drift checklist P0/P1/P2 adicionado.
- Review matrix padronizada por documento adicionada.
- Commit plan incremental, gates por prioridade e DoD por feature consolidados.
