---
status: proposed
progress: 0
generated: 2026-02-13
owner: release-engineering
slug: m3-vpvr-plan
lastUpdated: "2026-02-13T20:41:05.043Z"
---

# M3 VPVR Plan (Commit-Driven)

## P) Scope minimo

### 1) MVP VPVR (entra/sai)
- Entra (MVP): `marketdata.trade.v1.{venue}.{instrument}` apenas.
- Fora do MVP: `marketdata.markprice.*`, `marketdata.bookdelta.*`, footprint micro-tick por agressor.
- Artefatos de saida:
  - Event bus snapshot: `insights.volume_profile_snapshot.v1.{venue}.{instrument}`.
  - Event bus delta: `insights.volume_profile_delta.v1.{venue}.{instrument}` (somente apos snapshot estabilizado).
  - Delivery WS read model: snapshot atual por `(venue,instrument,timeframe,window)`.

### 2) Queries WS obrigatorias
- `last snapshot`: ultimo snapshot fechado para `(venue,symbol,timeframe)`.
- `range`: lista paginada por janela para `(venue,symbol,timeframe,start,end)`.
- Resposta deve incluir `seq_min`, `seq_max`, `window_start_ts`, `window_end_ts`, `bucket_count`.

### 3) Subjects + status (sem TBD)
- `insights.volume_profile_snapshot.v1.{venue}.{instrument}`: `draft`.
- `insights.volume_profile_delta.v1.{venue}.{instrument}`: `planned`.
- `marketdata.trade.v1.{venue}.{instrument}`: `stable` (input).

### 4) Cardinalidade (restricao #1, antes de runtime)
- `cap_windows_per_key`: 96 janelas abertas por `(venue,instrument,timeframe)`.
- `cap_buckets_per_window`: 400 buckets.
- `cap_levels_per_snapshot`: 400 niveis max no payload.
- `cap_ws_range_windows`: 200 janelas por query.
- `time_windows`: `1m`, `5m`, `1h`, `4h`, `1d`.
- `window_retention_hot`: 24h por timeframe curto (`1m`,`5m`) e 7d nos demais.

### 5) Dependencia de trades (normalizacao + idempotency)
- Normalizacao obrigatoria: `venue` canonical, `instrument` canonical (`uppercase alnum`) conforme ADR-0017.
- Idempotency key de agregacao VPVR: hash deterministico de `(venue,instrument,timeframe,window_start_ts,window_end_ts,bucket_low,bucket_high,aggregation_version)`.
- Dedup publish: propagar `idempotency_key` como `Nats-Msg-Id` no subject de insights.

## R) Review (riscos)

### 1) Anti-explosao
- Cap buckets: limitar bucketizacao por janela e executar compressao por merge adjacente ao atingir `cap_buckets_per_window`.
- Cap levels: truncar niveis menos relevantes no payload (ordem por volume desc).
- Cap windows: eviction deterministica FIFO por `window_end_ts` quando ultrapassar `cap_windows_per_key`.
- Cap query: rejeitar/paginar query de range acima de `cap_ws_range_windows`.

### 2) Politica sob pressao
- Ordem de acao: `compress -> degrade cadence -> drop deltas`.
- `compress`: aumentar bucket size dinamicamente (deterministico por faixa de preco).
- `degrade`: reduzir frequencia de delta e priorizar fechamento de janela.
- `drop`: descartar somente delta intermediario; nunca descartar fechamento de janela.
- Metricas obrigatorias:
  - `vpvr_bucket_count{venue,instrument,timeframe}`
  - `vpvr_windows_open{venue,instrument,timeframe}`
  - `vpvr_overload_actions_total{action,reason}`
  - `vpvr_drop_total{stage,reason}`
  - `vpvr_queue_depth{venue,instrument}`
  - `vpvr_replay_mismatch_total{kind}`

### 3) Replay determinism
- Deve ser deterministico:
  - atribuicao trade->bucket
  - `poc_price`, `value_area_low/high`
  - ordenacao e bytes do snapshot final por janela
- Como testar:
  - golden replay da mesma fixture 50x com byte-compare identico
  - teste de idempotencia (reprocessar mesma janela nao altera snapshot final)
  - teste de order invariance bloqueando out-of-order por `(ts_ingest,seq)`

## E) Execution (somente plano)

## Commit Chain: VPVR

### C1
- Mensagem: `docs(vpvr): define caps, windows, subjects and invariants for MVP`
- Objetivo: travar cardinalidade/janelas/subjects/invariantes antes de runtime.
- Arquivos:
  - `docs/architecture/volume-profiles.md`
  - `docs/contracts/subject-registry.yaml`
  - `docs/contracts/event-bus.md`
- Gates:
  - `make docs-check-full`
  - sem `TBD` em subjects VPVR
  - caps + `time_windows` declarados
- Rollback:
  - revert commit
  - remover entradas `insights.volume_profile_*` do registry

### C2
- Mensagem: `feat(insights-core): add vpvr domain model and invariants`
- Objetivo: dominio/core com invariantes VP-1..VP-5 e normalizacao de chave canonica.
- Arquivos:
  - `internal/core/insights/domain/volume_profile.go`
  - `internal/core/insights/app/build_volume_profile.go`
  - `internal/core/insights/app/build_volume_profile_test.go`
- Gates:
  - invariantes testaveis (`VP-1..VP-5`)
  - sem `time.Now()` em `internal/core/*`
  - normalizacao de trades validada contra naming canonico
- Rollback:
  - feature flag `enable_vpvr=false`
  - revert commit mantendo docs

### C3
- Mensagem: `feat(storage): implement vpvr hot write path with deterministic upsert`
- Objetivo: storage write path hot (sem detalhar schema fisico no feature-pack).
- Arquivos:
  - `internal/core/insights/ports/storage.go`
  - `internal/adapters/storage/timescale/volume_profile_writer.go`
  - `internal/adapters/storage/volume_profile_writer_test.go`
  - `docs/architecture/storage.md`
- Gates:
  - write idempotente por chave canonica + janela + bucket
  - ACK de pipeline somente apos hot commit
  - nenhuma especificacao fisica extra fora de `docs/architecture/storage.md`
- Rollback:
  - desligar writer VPVR por config
  - fallback para read model em memoria

### C4
- Mensagem: `feat(delivery): add vpvr read path and ws queries (last/range)`
- Objetivo: delivery read path (WS) com `last snapshot` e `range`.
- Arquivos:
  - `internal/interfaces/ws/volume_profile_delivery.go`
  - `internal/interfaces/ws/volume_profile_delivery_test.go`
  - `docs/contracts/delivery-ws.md`
- Gates:
  - enforce `cap_ws_range_windows`
  - payload budget + paginacao
  - filtros por `(venue,symbol,timeframe)` deterministas
- Rollback:
  - desregistrar stream WS VPVR
  - manter somente bus/storage sem exposicao WS

### C5
- Mensagem: `test(replay): add vpvr golden replay and overload behavior tests`
- Objetivo: golden replay + cobertura de pressao e determinismo.
- Arquivos:
  - `internal/shared/replay/fixtures/vpvr/*.jsonl`
  - `internal/core/insights/app/build_volume_profile_test.go`
  - `internal/shared/replay/golden_test.go`
- Gates:
  - replay 50x byte-estavel
  - mismatch counter em zero nos cenarios baseline
  - teste de overload cobrindo `compress/degrade/drop`
- Rollback:
  - remover fixtures/testes VPVR
  - bloquear release por gate de replay ate nova baseline

### C6
- Mensagem: `chore(release): wire feature flags, metrics and runbook for vpvr`
- Objetivo: readiness de release com flags, metricas, runbook e rollback operacional.
- Arquivos:
  - `cmd/consumer/main.go`
  - `deploy/configs/*.jsonc`
  - `docs/architecture/volume-profiles.md`
  - `docs/audits/DRIFT-REPORT-W11.md`
- Gates:
  - flags default off em prod
  - dashboards/alerts para metricas obrigatorias
  - checklist de rollback validado
- Rollback:
  - `enable_vpvr=false`
  - manter stream input sem produzir outputs VPVR

## V) Validation

### Acceptance tests (nomes + paths)
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRBucketDeterminism`
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRCardinalityCap`
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRPointOfControlConsistency`
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRReplayGoldenWindow`
- `internal/core/insights/app/build_volume_profile_test.go:TestVPVRBackpressureGracefulDegrade`
- `internal/interfaces/ws/volume_profile_delivery_test.go:TestVPVRLastSnapshotQuery`
- `internal/interfaces/ws/volume_profile_delivery_test.go:TestVPVRRangeQueryPagination`
- `internal/interfaces/ws/volume_profile_delivery_test.go:TestVPVRPayloadBudgetAndPagination`
- `internal/shared/replay/golden_test.go:TestGoldenReplayVPVRByteStable50Runs`

### TODOs explicitos
- TODO: criar fixture `vpvr_btcusdt_1m_1000trades.jsonl` para golden.
- TODO: adicionar gate CI para negar subject VPVR com status `TBD`.
- TODO: adicionar alarme para `vpvr_overload_actions_total` crescimento continuo.
- TODO: validar budget de payload WS em ambiente com burst real.

## C) Confirmation
- Chain registrada no MCP plan `m3-vpvr-plan` com checkpoint `M3-vpvr-plan`.

## Execution History

> Last updated: 2026-02-13T20:41:05.043Z | Progress: 0%
