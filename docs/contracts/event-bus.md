# Event Bus Contract

**Status:** Active
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-19
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0016-protobuf-contract-layer.md`, `docs/contracts/subject-registry.yaml`

---

## Objetivo

Definir contrato canônico de envelope e subject para publicacao/consumo no bus, garantindo compatibilidade, rastreabilidade e validação determinística.

## Escopo

- Estrutura do envelope de eventos.
- Taxonomia de subject para publish e filtros.
- Regras de versionamento e deduplicação.

## Nao-Escopo

- Política de retenção detalhada por stream (tratada em RFC de JetStream).
- Semântica de negócio de cada payload (tratada por contratos de domínio).

## Envelope Canonico

```json
{
  "type": "marketdata.trade",
  "version": 1,
  "venue": "BINANCE",
  "instrument": "BTC-USDT",
  "ts_exchange": 1710000000,
  "ts_ingest": 1710000005,
  "seq": 9283749823,
  "idempotency_key": "binance-BTCUSDT-123456",
  "content_type": "application/protobuf",
  "payload": {}
}
```

Campos obrigatórios:
- `type`, `version`, `venue`, `instrument`, `seq`, `idempotency_key`, `payload`.

## Subject Taxonomy

Subject de publish concreto:

```text
{event}.v{version}.{venue_lower}.{instrument_alnum_upper}
```

Exemplos válidos:
- `aggregation.snapshot.v1.binance.BTCUSDT`
- `aggregation.orderbook_inconsistency.v1.binance.BTCUSDT`
- `marketdata.trade.v1.binance.BTCUSDT`
- `marketdata.bookdelta.v1.bybit.ETHUSDT`
- `marketdata.markprice.v1.binance.BTCUSDT`
- `marketdata.liquidation.v1.bybit.ETHUSDT`
- `insights.heatmap_snapshot.v1.binance.BTCUSDT`
- `insights.heatmap_delta.v1.binance.BTCUSDT`
- `quarantine.v1.binance.BTCUSDT`

Regras:
- `event` pode ter múltiplos segmentos (`marketdata.trade`, `insights.crossvenue.trade_snapshot`).
- `version` deve ser `v{int}`.
- `venue` sem espaços, lowercase no subject.
- `instrument` normalizado para alfanumérico uppercase no subject.

## Pattern Taxonomy (filters)

Patterns com wildcard são válidos para subscription/filter quando respeitam raiz permitida (`aggregation`, `insights`, `marketdata`, `quarantine`) e regras de token.

Exemplos:
- `marketdata.>`
- `marketdata.trade.v1.*.BTCUSDT`
- `quarantine.v1.>`

## Versioning Rules

Permitido:
- adicionar campos opcionais.
- introduzir novo `version` mantendo compatibilidade de consumo.

Proibido:
- renomear campos sem mudança de versão.
- reaproveitar semântica de campo existente silenciosamente.

## Deduplication Rule

- `idempotency_key` é obrigatório e determinístico.
- Em JetStream, deve ser propagado como `Nats-Msg-Id` para dedup na janela configurada.

## Implementation Matrix

| Capability | Status | Referencia |
|---|---|---|
| Subject canônico derivado do envelope | Implemented | `internal/shared/envelope/subject.go:9`, `internal/shared/envelope/envelope_test.go:207` |
| Validação de subject concreto | Implemented | `internal/adapters/jetstream/subject_validation.go:24`, `internal/adapters/jetstream/subject_validation_test.go:5` |
| Validação de pattern com wildcard | Implemented | `internal/adapters/jetstream/subject_validation.go:50`, `internal/adapters/jetstream/subject_validation_test.go:35` |
| Enforcement em publish path | Implemented | `internal/adapters/jetstream/publisher.go:86` |
| Enforcement em ingest/quarantine path | Implemented | `internal/adapters/jetstream/ingest_policy.go:300`, `internal/adapters/jetstream/consumer_test.go:393` |
| Proto content-type opt-in no payload | Partially Implemented | `internal/shared/envelope/envelope.go:14`, `docs/adrs/ADR-0016-protobuf-contract-layer.md` |

### Runtime Subjects Matrix

| Subject | Producer (BC/runtime) | Consumer(s) esperados | Storage relevance | Replay note | Referencia |
|---|---|---|---|---|---|
| `insights.crossvenue.trade_snapshot.v1.global.{instrument}` | `aggregation` runtime (`ProcessorSubsystemActor` via `JoinCrossVenueTrades` + `PublishEnvelope`) | `delivery` WS contract input plane (`insights.crossvenue.trade_snapshot` stream quando subscrito) | Hot: L0 read-model em memória; L1/L2 planejado | Determinismo coberto por golden bytes (snapshot+signal) | `internal/actors/aggregation/runtime/processor.go:420`, `internal/actors/aggregation/runtime/processor_test.go:500`, `internal/adapters/jetstream/publisher_test.go:83`, `docs/contracts/delivery-ws.md:30`, `docs/architecture/storage.md:35`, `internal/shared/contracts/insights_registry.go:20`, `internal/core/insights/app/join_crossvenue_trades_test.go:529` |
| `insights.crossvenue.spread_signal.v1.global.{instrument}` | `aggregation` runtime (`ProcessorSubsystemActor` com `EnableSpreadSignal`) | `delivery` WS contract input plane (`insights.crossvenue.spread_signal` stream quando subscrito) | Hot: L0 read-model em memória; L1/L2 planejado | Determinismo coberto por golden bytes (snapshot+signal) | `internal/actors/aggregation/runtime/processor.go:471`, `internal/actors/aggregation/runtime/processor_test.go:564`, `docs/contracts/delivery-ws.md:31`, `docs/architecture/storage.md:36`, `internal/shared/contracts/insights_registry.go:29`, `internal/core/insights/app/join_crossvenue_trades_test.go:529` |

### Heatmap Subjects (`insights.heatmap_*`)

| Subject | Producer (BC/runtime) | Consumer(s) esperados | Status | Referencia |
|---|---|---|---|---|
| `insights.heatmap_snapshot.v1.{venue}.{instrument}` | `aggregation` runtime (`ProcessorSubsystemActor` + `BuildHeatmap`) | `delivery`, `storage` | stable | `docs/architecture/heatmap.md`, `docs/contracts/subject-registry.yaml` |
| `insights.heatmap_delta.v1.{venue}.{instrument}` | `insights` runtime (`BuildHeatmap`) | `delivery`, `storage` | planned | `docs/architecture/heatmap.md`, `docs/contracts/subject-registry.yaml` |

### Stable Subjects (`insights.volume_profile_*`)

| Subject | Producer (BC/runtime) | Consumer(s) esperados | Status | Referencia |
|---|---|---|---|---|
| `insights.volume_profile_snapshot.v1.{venue}.{instrument}` | `insights` runtime (`BuildVolumeProfile` + overload emit policy) | `delivery` | stable | `internal/core/insights/app/build_volume_profile.go`, `internal/core/insights/app/vpvr_overload_policy.go`, `docs/architecture/volume-profiles.md`, `docs/contracts/subject-registry.yaml` |
| `insights.volume_profile_delta.v1.{venue}.{instrument}` | `insights` runtime (`BuildVolumeProfile` + overload emit policy) | `delivery` | stable | `internal/core/insights/app/build_volume_profile.go`, `internal/core/insights/app/vpvr_overload_policy.go`, `docs/architecture/volume-profiles.md`, `docs/contracts/subject-registry.yaml` |

### VPVR Codec Status (Current vs Planned)

| Payload | Current (default flags) | Planned / Opt-in |
|---|---|---|
| `insights.volume_profile_snapshot` | `application/json` path when VPVR proto rollout flag is disabled (`enable_volume_profile_snapshot_proto=false`) | `application/protobuf` available via explicit feature flag (`enable_volume_profile_snapshot_proto=true`) |
| Replay golden impact | unchanged with default flags OFF | separate VPVR proto golden tracked in contracts testdata |

### Aggregation Subjects (`aggregation.*`)

| Subject | Producer (BC/runtime) | Consumer(s) esperados | Status | Referencia |
|---|---|---|---|---|
| `aggregation.snapshot.v1.{venue}.{instrument}` | `aggregation` runtime (`UpdateOrderBookFromEvents` via `ArtifactPublisher`) | `storage`, `delivery` (via hot read-model) | draft | `.context/docs/feature-packs/orderbook.md`, `internal/core/aggregation/ports/ports.go:12` |
| `aggregation.orderbook_inconsistency.v1.{venue}.{instrument}` | `aggregation` runtime (`UpdateOrderBookFromEvents` on crossed book) | `storage` | draft | `.context/docs/feature-packs/orderbook.md`, `internal/core/aggregation/ports/ports.go:13` |
| `aggregation.candle.v1.{venue}.{instrument}` | `aggregation` runtime (`BuildCandleFromEvents` via `ArtifactPublisher`) | `delivery`, `storage` | stable | `.context/docs/feature-packs/candle-aggregation.md`, `internal/core/aggregation/ports/ports.go:14` |
| `aggregation.stats.v1.{venue}.{instrument}` | `aggregation` runtime (`BuildStatsFromEvents` via `ArtifactPublisher`) | `delivery`, `storage` | stable | `.context/docs/feature-packs/stats-aggregation.md`, `internal/core/aggregation/ports/ports.go:15` |

## Evidence

- `internal/shared/envelope/subject.go:9`
- `internal/shared/envelope/envelope_test.go:207`
- `internal/adapters/jetstream/subject_validation.go:24`
- `internal/adapters/jetstream/subject_validation.go:50`
- `internal/adapters/jetstream/publisher_integration_test.go:64`
- `internal/adapters/jetstream/ingest_conformance_test.go:15`

## Changelog

- 2026-02-19:
- `aggregation.candle.v1` promoted to `stable` after M6 production readiness (runtime + storage + WS contract + latency evidence).
- `aggregation.stats.v1` promoted to `stable` after M7 production readiness (multi-timeframe tests + cross-source consistency + stream observability evidence).
- `insights.heatmap_snapshot.v1` promoted to `stable` after M8 production readiness (runtime + store cold-path + WS contract evidence).
- 2026-02-18:
- Adicionados subjects `aggregation.candle` e `aggregation.stats` na matriz de runtime.
- Referencias alinhadas com feature packs de candle/stats.
- 2026-02-13:
- Contrato alinhado à taxonomia real de subject (`{event}.v{version}.{venue}.{instrument}`).
- Drift removido em relação ao padrão antigo de quatro tokens fixos.
- Matriz e evidências adicionadas.
