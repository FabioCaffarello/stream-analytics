---
status: filled
progress: 0
generated: 2026-02-13
title: Retomada Estratégica M1-M3 (Commit Chains)
owner: Release Engineer + Architect
workflow: PREVC
phase: P
lastUpdated: "2026-02-18T00:00:00.000Z"
---

# 1) Program Snapshot

- Módulos reais (`go.work`): `./cmd/consumer`, `./cmd/processor`, `./cmd/server`, `./cmd/store`, `./internal/actors`, `./internal/adapters`, `./internal/core/aggregation`, `./internal/core/delivery`, `./internal/core/insights`, `./internal/core/marketdata`, `./internal/interfaces`, `./internal/shared`.
- Runtime hotspot: `internal/actors/runtime`, `internal/actors/marketdata/runtime`, `internal/actors/aggregation/runtime`, `internal/actors/delivery/runtime`.
- Storage hotspot: `internal/core/aggregation/ports/ports.go`, `internal/core/aggregation/app/update_orderbook.go`; adapters L1/L2 ainda `TODO` em `internal/adapters/storage/*`.
- Delivery hotspot: `internal/core/delivery/domain/subject.go`, `internal/core/delivery/app/session_usecase.go`, `internal/actors/delivery/runtime/{session,router}.go`.
- Replay hotspot: `internal/shared/replay/{player,sequencer,golden_test}.go`, `cmd/consumer/replay_test.go`.
- Gates Make existentes: `docs-check`, `invariants-check`, `test-workspace`, `test-workspace-race`, `soak-check`, `proto-lint`, `proto-breaking`.
- Scripts de gate existentes: `scripts/check-truth-map.sh`, `scripts/check-feature-pack-links.sh`, `scripts/check-pack-subjects-vs-event-bus.sh`, `scripts/check-domain-isolation.sh`, `scripts/soak-test.sh`, `scripts/validate-commit-msg.sh`.
- Estado real `docs-check`: **falha** por headers/metadados ausentes em ADR/RFC legados (38 issues).
- Estado real `invariants-check`: **passa** (domain isolation + determinism/replay-offline + core exchange-purity).
- Estado real TRUTH-MAP: `scripts/check-truth-map.sh` **passa**.
- Estado real feature-packs: `scripts/check-feature-pack-links.sh` e `scripts/check-pack-subjects-vs-event-bus.sh` **passam**.
- Feature-pack storage: baseline + TODO writers/tests (`.context/docs/feature-packs/storage.md`).
- Feature-pack delivery-ws: baseline sessão/roteador + TODO política explícita de backpressure (`.context/docs/feature-packs/delivery-ws.md`).
- Feature-packs heatmap/volume/liquidations: baseline de invariantes + TODO builders/writers/tests (`.context/docs/feature-packs/{heatmap,volume-profiles,liquidations-markprice}.md`).
- Aderência TRUTH-MAP: consistente entre `docs/architecture/TRUTH-MAP.md`, `docs/rfcs/EXECUTION-SEQUENCE.md` e feature-pack guards; dívida concentrada em `docs-check` estrutural.

# 2) Autoridade Arquitetural

Leituras mandatórias concluídas:
- `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`
- `docs/rfcs/EXECUTION-SEQUENCE.md`

Contradição/tensão registrada:
- ~~RFC-0011 GD-02 mantém `aggregation.*` como planejado, enquanto runtime validator aceita roots `marketdata|insights|quarantine`.~~
- **RESOLVIDO (2026-02-18):** runtime validator já aceita `aggregation` root (`internal/adapters/jetstream/subject_validation.go:15`); NOTE-001 superada; GD-02 fechado em RFC-0011.

# 3) Retomada Estratégica — Milestones

## M1 — Storage Writers + idempotência + ack-on-commit

- Módulos Go afetados: `./internal/adapters`, `./internal/core/aggregation`, `./internal/shared`, `./cmd/consumer`, `./cmd/store`.
- Contracts (proto/envelope/subjects): envelope ADR-0002 (`type/version/venue/instrument/ts_ingest/seq/idempotency_key/payload`), inputs `marketdata.*.v1.*.*`, derivados planejados `aggregation.snapshot.v1.{venue}.{instrument}`.
- Storage tables (hot/cold + keys): hot `timescale.marketdata_ticks_hot`, `timescale.aggregation_orderbook_snapshot_hot`; cold `clickhouse.marketdata_ticks_cold`, `clickhouse.aggregation_orderbook_snapshot_cold`; chaves `(venue,instrument,seq[,snapshot_version])` + `idempotency_key`.
- Invariants (ADRs): ADR-0002, ADR-0004, ADR-0005, ADR-0006, ADR-0013, ADR-0015.
- Test plan: golden replay (`internal/shared/replay/golden_test.go`, `cmd/consumer/replay_test.go`), race (`make test-workspace-race`), soak (`make soak-check` para writer path).
- Observability mínima: `storage_writer_queue_depth`, `storage_write_latency_ms`, `storage_commit_total`, `storage_drop_total`, `bus_consumer_lag`.
- Rollback strategy: revert do commit atômico de writer/adaptação; manter ingest path atual sem storage writer ativo; revalidar `invariants-check`.

## M2 — Delivery WS snapshots + backpressure

- Módulos Go afetados: `./internal/core/delivery`, `./internal/actors`, `./internal/interfaces`, `./internal/shared`, `./cmd/server`.
- Contracts (proto/envelope/subjects): WS subject `<stream_type>/<venue>/<symbol>/<timeframe>`; frame atual `type|subject|seq|ts_ingest|payload`; input bus `marketdata.*` + `insights.crossvenue.*` + `aggregation.snapshot` (planejado).
- Storage tables (hot/cold + keys): leitura hot `timescale.aggregation_orderbook_snapshot_hot` (key `(venue,instrument,seq,snapshot_version)`); cold fallback `clickhouse.aggregation_orderbook_snapshot_cold`.
- Invariants (ADRs): ADR-0007, ADR-0013, ADR-0014, ADR-0015.
- Test plan: golden/range determinístico (`getrange`), race (`make test-workspace-race` focando sessão/roteador), soak de cliente lento (`make soak-check`).
- Observability mínima: `delivery_ws_queue_depth`, `delivery_ws_drop_total`, `delivery_ws_slow_client_total`, `delivery_ws_frame_latency_ms`.
- Rollback strategy: revert isolado da policy de backpressure/snapshot delivery; manter rota WS baseline já existente.

## M3 — Heatmap + Volume Profiles + replay rebuild

**NOTE (2026-02-18):** Domain models and builder use cases for heatmap and VP already exist:
- `internal/core/insights/domain/heatmap_bucket.go` (136 LOC) + `internal/core/insights/app/build_heatmap.go` (517 LOC)
- `internal/core/insights/domain/volume_profile.go` (169 LOC) + `internal/core/insights/app/build_volume_profile.go` (433 LOC)
- `internal/core/insights/app/service.go` (InsightsService facade)
- **M3.C2 (feat: builders) is already done.** Remaining: M3.C1 (docs contracts), M3.C3 (writers), M3.C4 (replay tests).

- Módulos Go afetados: `./internal/core/insights`, `./internal/adapters`, `./internal/shared`, `./internal/interfaces`, `./cmd/processor`, `./cmd/consumer`.
- Contracts (proto/envelope/subjects): `insights.<heatmap_event>.v1.{venue}.{instrument}`, `insights.<volume_profile_event>.v1.{venue}.{instrument}` (registry explícito); WS `insights.heatmap/*` e `insights.volume_profile/*`.
- Storage tables (hot/cold + keys): hot `timescale.insights_heatmap_bucket_hot`, `timescale.insights_volume_profile_hot`; cold `clickhouse.insights_heatmap_bucket_cold`, `clickhouse.insights_volume_profile_cold`; keys `(venue,instrument,timeframe,price_bucket,window_start_ts[,seq_max])` + `idempotency_key`.
- Invariants (ADRs): ADR-0013, ADR-0015, ADR-0017, ADR-0006.
- Test plan: golden de janela/hash, replay rebuild equivalence, race em builders/writers, soak em cardinalidade alta.
- Observability mínima: `heatmap_cells_total`, `heatmap_payload_bytes`, `volume_profile_bucket_count`, `*_replay_lag_ms`, `*_drop_total`.
- Rollback strategy: revert por feature (heatmap/VP) sem tocar replay core; manter somente baseline de replay existente.

## M4 — Candle + Stats Aggregation (Product Parity)

**Added 2026-02-18** — covers marketmonkey's `actor/trade/` (candle sampler) and `actor/stat/` (stats aggregator).

- Módulos Go afetados: `./internal/core/aggregation`, `./internal/adapters`, `./internal/shared`, `./internal/interfaces`, `./cmd/processor`.
- Contracts (proto/envelope/subjects): `aggregation.candle.v1.{venue}.{instrument}`, `aggregation.stats.v1.{venue}.{instrument}` (subject root `aggregation` accepted in runtime).
- Architecture authority: `docs/architecture/candle-aggregation.md`, `docs/architecture/stats-aggregation.md`.
- Invariants (ADRs): ADR-0002, ADR-0006, ADR-0013, ADR-0014, ADR-0015.
- Test plan: golden OHLCV determinism, stats determinism, multi-timeframe cascade, partial-input tolerance.
- Observability mínima: `candle_build_latency_ms`, `candle_close_total`, `stats_build_latency_ms`, `stats_partial_total`.
- Dependencies: M1 (storage writers) should land first for persistence path.

# 4) Commit Slicing — COMMIT CHAINS

## M1 Chain

Commit 1:
- Mensagem: `docs(storage): definir contrato de idempotência e ack-on-commit`
- Objetivo: congelar contrato de storage boundary antes de runtime.
- Arquivos (2): `docs/architecture/storage.md`, `docs/contracts/event-bus.md`
- Gates obrigatórios: `make docs-check`, `make invariants-check`, `scripts/validate-commit-msg.sh <msgfile>`
- Risco: drift documental residual (docs-check já falhando no baseline legado).
- Rollback simples: `git revert <sha>`

Commit 2:
- Mensagem: `feat(m1): consolidar contrato de envelope para idempotência forte`
- Objetivo: ajustar somente contrato/envelope antes de runtime.
- Arquivos (2): `internal/shared/envelope/envelope.go`, `internal/shared/envelope/subject.go`
- Gates obrigatórios: sempre + `make test-workspace`
- Risco: quebra de compatibilidade de envelope.
- Rollback simples: `git revert <sha>`

Commit 3:
- Mensagem: `feat(m1): adicionar porta de writer hot/cold no core de agregação`
- Objetivo: introduzir interfaces/uso de writer sem lógica de replay.
- Arquivos (2): `internal/core/aggregation/ports/ports.go`, `internal/core/aggregation/app/update_orderbook.go`
- Gates obrigatórios: sempre + `make test-workspace`
- Risco: quebra de compile por assinatura de porta.
- Rollback simples: `git revert <sha>`

Commit 4:
- Mensagem: `fix(runtime): garantir ack somente após commit do writer`
- Objetivo: mover boundary ACK para pós-commit.
- Arquivos (2): `internal/adapters/jetstream/consumer.go`, `internal/adapters/jetstream/ingest_policy.go`
- Gates obrigatórios: sempre + `make test-workspace` + `make test-workspace-race`
- Risco: regressão de throughput/latência.
- Rollback simples: `git revert <sha>`

Commit 5:
- Mensagem: `test(replay): validar idempotência e ack boundary com golden`
- Objetivo: cobrir determinismo/idempotência sem misturar writer e replay logic no mesmo commit.
- Arquivos (3): `internal/adapters/jetstream/ingest_conformance_test.go`, `internal/shared/replay/golden_test.go`, `cmd/consumer/replay_test.go`
- Gates obrigatórios: sempre + replay golden (`go test ./internal/shared/replay -run TestGoldenReplay`, `go test ./cmd/consumer -run TestReplayIngestGolden1000`)
- Risco: flakiness de teste de integração.
- Rollback simples: `git revert <sha>`

## M2 Chain

Commit 1:
- Mensagem: `docs(delivery): fixar contrato de snapshots WS e política de backpressure`
- Objetivo: separar contrato WS atual vs planejado.
- Arquivos (1): `docs/contracts/delivery-ws.md`
- Gates obrigatórios: sempre
- Risco: drift do pack operacional.
- Rollback simples: `git revert <sha>`

Commit 2:
- Mensagem: `docs(delivery): sincronizar feature-pack operacional de delivery`
- Objetivo: alinhar interface operacional sem tocar autoridade canônica.
- Arquivos (1): `.context/docs/feature-packs/delivery-ws.md`
- Gates obrigatórios: sempre
- Risco: duplicação indevida de conteúdo.
- Rollback simples: `git revert <sha>`

Commit 3:
- Mensagem: `feat(m2): adicionar fila bounded por sessão e keep-latest para stream não crítico`
- Objetivo: implementar policy por sessão sem tocar contratos bus.
- Arquivos (3): `internal/actors/delivery/runtime/session.go`, `internal/actors/delivery/runtime/router.go`, `internal/core/delivery/domain/subject.go`
- Gates obrigatórios: sempre + `make test-workspace` + `make test-workspace-race`
- Risco: drop indevido em stream crítico.
- Rollback simples: `git revert <sha>`

Commit 4:
- Mensagem: `test(replay): cobrir determinismo de getrange e slow-client`
- Objetivo: validar ordering/isolamento de sessão.
- Arquivos (2): `internal/actors/delivery/runtime/session_test.go`, `internal/actors/delivery/runtime/router_test.go`
- Gates obrigatórios: sempre + replay golden quando `getrange` tocar replay source
- Risco: falso positivo por fixture insuficiente.
- Rollback simples: `git revert <sha>`

## M3 Chain

Commit 1:
- Mensagem: `docs(insights): definir contratos heatmap/vp e tabela hot/cold`
- Objetivo: fechar autoridade antes da implementação.
- Arquivos (3): `docs/architecture/heatmap.md`, `docs/architecture/volume-profiles.md`, `docs/architecture/storage.md`
- Gates obrigatórios: sempre
- Risco: drift de naming subject.
- Rollback simples: `git revert <sha>`

Commit 2:
- Mensagem: `feat(m3): introduzir builders determinísticos de heatmap e volume profile`
- Objetivo: criar builders sem acoplar replay rebuild.
- Arquivos (3): `internal/core/insights/domain/volume_profile.go`, `internal/core/insights/app/build_volume_profile.go`, `internal/core/insights/app/build_heatmap.go`
- Gates obrigatórios: sempre + `make test-workspace` + `make test-workspace-race`
- Risco: explosão de cardinalidade.
- Rollback simples: `git revert <sha>`

Commit 3:
- Mensagem: `feat(m3): adicionar writers hot/cold para heatmap e volume`
- Objetivo: persistência idempotente isolada da lógica de replay.
- Arquivos (3): `internal/adapters/storage/timescale/heatmap_writer.go`, `internal/adapters/storage/timescale/volume_profile_writer.go`, `internal/adapters/storage/clickhouse/volume_profile_writer.go`
- Gates obrigatórios: sempre + `make test-workspace`
- Risco: divergência hot/cold.
- Rollback simples: `git revert <sha>`

Commit 4:
- Mensagem: `test(replay): validar replay rebuild hash-equivalence para heatmap e vp`
- Objetivo: garantir rebuild determinístico pós-writer.
- Arquivos (3): `internal/core/insights/app/build_heatmap_test.go`, `internal/core/insights/app/build_volume_profile_test.go`, `internal/shared/replay/golden_test.go`
- Gates obrigatórios: sempre + replay golden + `make soak-check`
- Risco: não-determinismo sob carga.
- Rollback simples: `git revert <sha>`

# 5) Gates Obrigatórios Por Commit

Sempre:
- `make docs-check`
- `make invariants-check`
- `scripts/validate-commit-msg.sh <msgfile>` (commit-msg validation)

Quando aplicável:
- `make test-workspace`
- `make test-workspace-race`
- replay golden:
  - `go test ./internal/shared/replay -run TestGoldenReplay`
  - `go test ./cmd/consumer -run TestReplayIngestGolden1000`
- soak:
  - `make soak-check`

Regra de interrupção:
- falhou qualquer gate => **STOP da cadeia de commits**.

# 6) Red Flags — STOP CONDITIONS

- quebra de determinismo em golden replay (byte/hash mismatch).
- drift de subject/taxonomia fora de `docs/contracts/event-bus.md`.
- ACK emitido antes de commit durável (ack-on-enqueue).
- idempotência fraca (duplicação por `idempotency_key`).
- replay rebuild inconsistente entre hot/cold.

# 7) Entrega Final

## 1️⃣ Checklist ordenado (M1 → M2 → M3)

1. M1.C1 `docs(storage)` -> M1.C2 `feat(m1)` contrato -> M1.C3 `feat(m1)` runtime writer-port -> M1.C4 `fix(runtime)` -> M1.C5 `test(replay)`.
2. M2.C1 `docs(delivery)` canônico -> M2.C2 `docs(delivery)` feature-pack -> M2.C3 `feat(m2)` -> M2.C4 `test(replay)`.
3. M3.C1 `docs(insights)` -> M3.C2 `feat(m3)` -> M3.C3 `feat(m3)` writers -> M3.C4 `test(replay)`.
4. Em cada commit: gates sempre obrigatórios + gates condicionais.
5. Qualquer falha de gate: parar, registrar checkpoint e não avançar cadeia.

## 2️⃣ Tabela curta

| Milestone | Commit | Gate principal | Risco |
|---|---|---|---|
| M1 | C4 `fix(runtime)` | `make test-workspace-race` | ACK fora do commit boundary |
| M2 | C3 `feat(m2)` | `make test-workspace-race` | backpressure degradar stream crítico |
| M3 | C4 `test(replay)` | replay golden | rebuild não determinístico |

## 3️⃣ Registro MCP (decisões/checkpoints/chain/links)

- Decisões registradas em `plan.recordDecision`:
  - conflito GD-02 (`aggregation.*` vs roots atuais) -> runtime congelado até commit chain de contracts.
  - gate baseline: `docs-check` falha estrutural legada; `invariants-check` passa.
- Checkpoints previstos:
  - CP-M1 pós C3/C5
  - CP-M2 pós C2/C3
  - CP-M3 pós C3/C4
- Cadeia de commits registrada neste plano (M1/M2/M3).
- Links canônicos:
  - `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`
  - `docs/rfcs/EXECUTION-SEQUENCE.md`
  - `docs/contracts/event-bus.md`
  - `docs/contracts/delivery-ws.md`
  - `docs/architecture/TRUTH-MAP.md`
  - `.context/docs/feature-packs/storage.md`
  - `.context/docs/feature-packs/delivery-ws.md`
  - `.context/docs/feature-packs/heatmap.md`
  - `.context/docs/feature-packs/volume-profiles.md`
  - `.context/docs/feature-packs/liquidations-markprice.md`

## Execution History

> Last updated: 2026-02-18 | Progress: 0%
> 2026-02-18: GD-02 resolved; M3.C2 (builders) confirmed existing; M4 (candle+stats) milestone added; all docs-check gates passing.
