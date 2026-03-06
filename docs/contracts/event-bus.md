# Event Bus Contract

**Status:** Active
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-03-06
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0014-stream-partitioning-strategy.md`, `docs/adrs/ADR-0016-protobuf-contract-layer.md`, `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/credentials-broker-hardening-stage9b.md`, `docs/contracts/subject-registry.yaml`, `docs/contracts/canonical-market-model.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`

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

### CMM Envelope Contract

- CMM is the only model in the hot path.
- Typed canonical payload envelope is available as `marketmodel.v1.MarketEvent` in `proto/marketmodel/v1/market_event.proto`.

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
- `signal.event.v1.binance.BTCUSDT`
- `strategy.intent.v1.binance.BTCUSDT`
- `execution.event.v1.binance.BTCUSDT`
- `portfolio.state.v1.binance.BTCUSDT`
- `quarantine.v1.binance.BTCUSDT`

Regras:
- `event` pode ter múltiplos segmentos (`marketdata.trade`, `insights.crossvenue.trade_snapshot`).
- `version` deve ser `v{int}`.
- `venue` sem espaços, lowercase no subject.
- `instrument` normalizado para alfanumérico uppercase no subject.

## Pattern Taxonomy (filters)

Patterns com wildcard são válidos para subscription/filter quando respeitam raiz permitida (`aggregation`, `insights`, `liquidity`, `marketdata`, `quarantine`, `signal`, `strategy`, `execution`, `portfolio`) e regras de token.

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
| `aggregation.tape.v1.{venue}.{instrument}` | `aggregation` runtime (`BuildTapeFromTrades` via `ArtifactPublisher`) | `delivery`, `storage` | stable | `proto/aggregation/v1/tape.proto`, `internal/core/aggregation/ports/ports.go:16` |

### Signal / Strategy / Execution / Portfolio Subjects

| Subject | Producer (BC/runtime) | Consumer(s) esperados | Status | Referencia |
|---|---|---|---|---|
| `signal.event.v1.{venue}.{instrument}` | `signal` runtime (`cmd/signals`) | `delivery`, `strategy` | stable | `docs/contracts/signal-engine.md`, `proto/marketmodel/v1/market_event.proto`, `docs/contracts/subject-registry.yaml` |
| `signal.composite.v1.{venue}.{instrument}` | retired from strategist runtime (legacy compatibility/replay only) | `delivery` (historical compatibility) | draft (deprecated operationally) | `docs/architecture/semantic-hardening-stage1.md`, `proto/signals/v1/composite.proto`, `docs/contracts/subject-registry.yaml`, `docs/operations/signals-strategist-cutover.md` |
| `strategy.intent.v1.{venue}.{instrument}` | `strategy` runtime (`cmd/strategist`) | `execution`, `delivery` | draft (runtime hardened Stage 6 canonical intake; storage not yet wired) | `docs/contracts/strategy-execution-portfolio-contracts.md`, `proto/strategy/v1/intent.proto`, `docs/contracts/subject-registry.yaml`, `docs/architecture/runtime-bootstrap-stage4.md` |
| `execution.event.v1.{venue}.{instrument}` | `execution` runtime (`cmd/executor`) | `portfolio`, `delivery` | draft (Stage 7 safe real-adapter cut-in behind boundary; storage not yet wired) | `docs/contracts/strategy-execution-portfolio-contracts.md`, `proto/execution/v1/event.proto`, `docs/contracts/subject-registry.yaml`, `docs/architecture/real-adapter-integration-stage7.md` |
| `portfolio.state.v1.{venue}.{instrument}` | `portfolio` runtime (`cmd/portfolio`) | `delivery` | draft (runtime hardened Stage 6+ chain; storage not yet wired) | `docs/contracts/strategy-execution-portfolio-contracts.md`, `proto/portfolio/v1/state.proto`, `docs/contracts/subject-registry.yaml`, `docs/architecture/runtime-bootstrap-stage4.md` |

### Runtime Controls (Stage 9A/9B Canonical + Governed Safe Cut-In)

- Canonical strategist intake default: `signal.event.>`.
- `signal.composite` strategist intake is retired and must not be enabled.
- Executor rejection lifecycle is explicit (`execution.event` with `status=rejected` and deterministic `reason`).
- Executor boundary metadata is explicit (`execution_boundary`, `execution_adapter`, `execution_mode`).
- Executor rejection metadata now includes `execution_reason_category`:
  - `governance_denied`
  - `credentials_unavailable`
  - `credentials_invalid`
  - `credentials_scope_denied`
  - `credentials_lease_denied`
  - `adapter_selection_denied`
  - `execution_policy_rejected`
  - `venue_runtime_failure`
- Executor default mode remains `bootstrap_simulated`.
- Real adapter path is explicit and restricted: `execution.mode=real_adapter_safe`, `execution.adapter=binance.spot`, `execution.real.binance.trade_api.endpoint_mode=test_order`.
- Stage 9A/9B governance is explicit and fail-closed:
  - `CapabilityAuthorizer` evaluates grant/scope/limit posture;
  - `AdapterSelector` chooses the concrete boundary route;
  - `CredentialResolver` / broker checks required trade-only credential availability, provenance, and lease fitness.
- Stage 8 lifecycle expansion remains opt-in:
  - `execution.real.binance.trade_api.endpoint_mode=safe_order_lifecycle`
  - bounded reconciliation polling (`reconcile_enabled`, `reconcile_poll_interval`, `reconcile_max_polls`)
  - deterministic mapping from observed venue status to canonical `execution.event` lifecycle transitions.
- Real adapter must remain trade-only with allowlists and fail-closed guardrails.
- Portfolio projector consumes lifecycle transitions (`accepted/placed/partially_filled/filled/rejected/canceled/expired/failed`) and emits `source_execution_status` in envelope metadata.

## Evidence

- `internal/shared/envelope/subject.go:9`
- `internal/shared/envelope/envelope_test.go:207`
- `internal/adapters/jetstream/subject_validation.go:24`
- `internal/adapters/jetstream/subject_validation.go:50`
- `internal/adapters/jetstream/publisher_integration_test.go:64`
- `internal/adapters/jetstream/ingest_conformance_test.go:15`

## Changelog

- 2026-03-06:
- Stage 9B credentials-broker hardening:
  - credential resolution is now modeled with explicit availability, provenance, and lease semantics;
  - runtime rejection metadata now distinguishes unavailable vs invalid vs scope-denied vs lease-denied credentials;
  - real adapter wiring uses a broker boundary instead of consuming env providers directly.
- 2026-03-06:
- Stage 9A governance-first hardening:
  - execution governance is now explicit before adapter invocation;
  - rejection metadata now includes `execution_reason_category`;
  - missing credentials and adapter selection denials are separated from execution-policy and venue-runtime failures.
- 2026-03-06:
- Stage 8 safe lifecycle expansion:
  - real adapter now supports opt-in lifecycle reconciliation mode (`safe_order_lifecycle`) behind the same boundary;
  - observed venue statuses are translated deterministically to canonical `execution.event` lifecycle transitions;
  - portfolio projector remains event-derived and lifecycle-aware with partial-fill progression.
- 2026-03-06:
- Stage 7 safe real-adapter cut-in:
  - execution runtime now supports explicit mode gating (`bootstrap_simulated` vs `real_adapter_safe`);
  - real adapter is constrained to trade-only Binance test-order endpoint behind boundary;
  - canonical `execution.event`/`portfolio.state` contract flow remains unchanged.
- 2026-03-06:
- Stage 6 legacy retirement:
  - strategist intake narrowed to canonical `signal.event` only;
  - `signal.composite` reclassified as deprecated operationally (historical compatibility only);
  - execution publish metadata now includes explicit adapter boundary markers.
- 2026-03-06:
- Added Stage 3 contract taxonomy for `strategy.intent`, `execution.event`, and `portfolio.state`.
- Added explicit signal contract lineage (`signal.event` canonical, `signal.composite` transitional).
- Expanded allowed pattern roots to include `signal`, `strategy`, `execution`, and `portfolio`.
- 2026-03-06:
- Stage 5 transitional cutover markers:
  - `signal.composite` reclassified to draft transitional legacy stream;
  - strategist canonical intake default set to `signal.event.>` with legacy opt-in;
  - execution/portfolio runtime matrix marked as Stage 5 hardened bootstrap.
- 2026-03-06:
- Updated runtime matrix with Stage 4 bootstrap producers (`cmd/strategist`, `cmd/executor`, `cmd/portfolio`).
- 2026-03-04:
- Added `aggregation.tape.v1.{venue}.{instrument}` to aggregation runtime subject matrix.
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
