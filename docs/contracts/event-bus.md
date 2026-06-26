# Event Bus Contract

**Status:** Active
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-03-06
**Relates to:** `docs/contracts/subject-registry.yaml`, `docs/contracts/canonical-market-model.md`, `internal/adapters/jetstream/subject_validation.go`

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
- `quarantine.v1.binance.BTCUSDT`

Regras:
- `event` pode ter múltiplos segmentos (`marketdata.trade`, `insights.crossvenue.trade_snapshot`).
- `version` deve ser `v{int}`.
- `venue` sem espaços, lowercase no subject.
- `instrument` normalizado para alfanumérico uppercase no subject.

## Pattern Taxonomy (filters)

Patterns com wildcard são válidos para subscription/filter quando respeitam raiz permitida (`aggregation`, `insights`, `liquidity`, `marketdata`, `quarantine`) e regras de token.

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

## HTTP Discovery APIs

### Timeline API

```
GET /api/v1/timeline?venue=X&instrument=Y&timeframe=Z&artifact=candle|stats
→ {"venue":"X","instrument":"Y","timeframe":"Z","artifact":"candle","first_ts":N,"last_ts":M}
```

Returns the earliest and latest `window_start` timestamps for the requested artifact, enabling clients to discover available data ranges without transferring full payloads. The response hides hot/cold/federation details — the caller sees a single unified time range.

- Artifact defaults to `candle` when omitted.
- Uses existing federated readers (`GetFirstCandle/GetLastCandle`, `GetFirstStats/GetLastStats`).
- Registered only when `coldReaders` is configured.

### Stream Catalog API

```
GET /api/v1/catalog?venue=X&instrument=Y
→ {"entries":[{"venue":"X","instrument":"Y","artifacts":[{"name":"candle","endpoint":"/api/v1/candles","timeframes":["1s","5s","1m",...]},...]}]}
```

Returns available artifacts, their supported timeframes, and HTTP endpoints for configured markets. Config-derived — no storage queries.

- Both `venue` and `instrument` are optional filters.
- Results are sorted by venue then instrument.
- Registered only when `markets` config is present.

### Session Overview API

```
GET /api/v1/session
-> {"server_time_ms":N,"ready":bool,"markets":[{"venue":"X","instruments":["Y",...]}],"capabilities":{"artifacts":[{"name":"candle","endpoint":"/api/v1/candles","timeframes":["1s",...]}]}}
```

Returns a composed bootstrap payload combining server time, guardian readiness, available markets, and artifact capabilities. Replaces multiple startup calls (/readyz + /markets + /catalog) with a single request. Config-derived markets, domain-constant artifacts — no storage queries.

- Registered only when `markets` config is present.
- Readiness is best-effort (returns false on guardian timeout).

### Session Dashboard API

```
GET /api/v1/session/dashboard
-> {
     "server_time_ms":N,
     "status":"ready|degraded|inactive|not_ready",
     "readiness":{"status":"ready|not_ready"},
     "freshness":{"status":"flowing|partial|stale|inactive","active_instruments":A,"stale_instruments":S,"flowing_channels":C1,"stale_channels":C2,"checked_at":N},
     "resync":{"status":"stable|recovering|degraded","connections_active":K,"streams":M,"resync_total":R,"drops_total":D,"max_lag_ms":L},
     "artifacts":[
       {"name":"candle","endpoint":"/api/v1/candles","timeframes":[...],"default_timeframe":"1m","coverage":{"status":"available|partial|empty|unavailable","total_instruments":T,"available_instruments":A1,"empty_instruments":E1,"unavailable_instruments":U1}},
       {"name":"stats","endpoint":"/api/v1/stats","timeframes":[...],"default_timeframe":"1m","coverage":{"status":"available|partial|empty|unavailable","total_instruments":T,"available_instruments":A2,"empty_instruments":E2,"unavailable_instruments":U2}}
     ],
     "summary":{"venues":V,"instruments":I}
   }
```

Returns a backend-owned session-level readiness dashboard composed from guardian readiness, terminal WS state, and best-effort timeline coverage for configured markets.

- Registered only when `markets` config is present.
- Coverage is computed from timeline availability without exposing hot/cold/federation internals.
- Status enums are contract-level and must remain backward-compatible.

### Artifact Summary API

```
GET /api/v1/artifacts/summary?timeframe=1m&venue=X&instrument=Y&artifact=candle|stats
-> {
     "timeframe":"1m",
     "status":"available|partial|empty|unavailable",
     "checked_at":N,
     "filters":{"venue":"X","instrument":"Y","artifact":"candle"},
     "artifacts":[
       {"name":"candle","endpoint":"/api/v1/candles","timeframes":[...],"default_timeframe":"1m","coverage":{"status":"available|partial|empty|unavailable","total_instruments":T,"available_instruments":A,"empty_instruments":E,"unavailable_instruments":U}}
     ],
     "entries":[
       {"venue":"X","instrument":"Y","artifacts":{"candle":"available"}}
     ],
     "summary":{"venues":V,"instruments":I,"entries":R}
   }
```

Returns a dedicated backend-owned artifact matrix for dynamic widget enablement with optional filters and deterministic sorting.

- Registered only when `markets` config is present.
- `timeframe` defaults to `1m`; unsupported timeframes return `400`.
- `artifact` is optional (`candle|stats`); unsupported artifacts return `400`.
- Per-entry artifact statuses are `available|empty|unavailable`.

### Instrument Freshness API

```
GET /api/v1/freshness?venue=X&instrument=Y
-> {"venue":"X","instrument":"Y","active":bool,"channels":{"candle":{"last_event_ts":N,"lag_ms":M,"flowing":bool},...},"checked_at":N}
```

Returns per-channel data flow health for the requested instrument. Derived from terminal WS stream state — no storage queries. A channel is "flowing" if its last event is within 30 seconds.

- Always available (no config gate).
- Case-insensitive venue/instrument matching.
- Hides internal stream IDs and observability details.

### Instrument Overview API

```
GET /api/v1/instrument/overview?venue=X&instrument=Y
-> {
     "venue":"X",
     "instrument":"Y",
     "status":"ready|degraded|inactive|not_ready",
     "checked_at":N,
     "readiness":{"status":"ready|not_ready"},
     "freshness":{"status":"flowing|stale|inactive","active":bool,"channels":{...}},
     "resync":{"status":"stable|recovering|degraded","resync_total":N,"drops_total":M,"streams":K,"max_lag_ms":L},
     "artifacts":[
       {"name":"candle","endpoint":"/api/v1/candles","timeframes":[...],"timeline":{"timeframe":"1m","first_ts":N,"last_ts":M,"status":"available|empty|unavailable"}},
       {"name":"stats","endpoint":"/api/v1/stats","timeframes":[...],"timeline":{"timeframe":"1m","first_ts":N,"last_ts":M,"status":"available|empty|unavailable"}}
     ]
   }
```

Returns a backend-owned composed read model for one instrument, normalizing readiness/freshness/resync semantics for client widget bootstrap without leaking hot/cold/federation internals.

- Always available (no config gate).
- Case-insensitive venue/instrument matching.
- Timeline summaries are best-effort and marked as `unavailable` when readers are not wired.
- Status enums are contract-level and must remain backward-compatible.

### Wire Format Contract

All aggregation domain types (CandleV1, StatsWindowV1, TapeWindowV1, DeltaVolumeWindowV1, CVDWindowV1, BarStatsWindowV1, OpenInterestWindowV1, SnapshotProduced) have explicit `json` tags preserving PascalCase field names. Wire format is frozen — any field rename requires a version bump.

Insights domain types (HeatmapArtifactV1, VolumeProfileSnapshotV1, CrossVenueTradeSnapshotV1) use snake_case `json` tags. These two conventions coexist by design (aggregation types were stabilized in-place; insights types were designed with explicit tags from the start).

Evidence: `internal/core/aggregation/domain/wire_format_golden_test.go`

## Evidence

- `internal/shared/envelope/subject.go:9`
- `internal/shared/envelope/envelope_test.go:207`
- `internal/adapters/jetstream/subject_validation.go:24`
- `internal/adapters/jetstream/subject_validation.go:50`
- `internal/adapters/jetstream/publisher_integration_test.go:64`
- `internal/adapters/jetstream/ingest_conformance_test.go:15`

## Changelog

- 2026-06-25: S9 legacy retirement — removed signal/strategy/execution/portfolio subjects and both Runtime Controls sections; removed stale references to strategy-execution-portfolio-contracts.md, credentials-broker-hardening-stage9b.md, and deleted ADRs.
- 2026-03-06:
- Stage 19 slice 1:
  - added Instrument Overview API (`GET /api/v1/instrument/overview`);
  - froze initial normalized status enums for readiness/freshness/resync and overall instrument status;
  - added artifact timeline summary contract (`available|empty|unavailable`) for candle/stats.
- 2026-03-06:
- Stage 19 slice 2:
  - added Session Dashboard API (`GET /api/v1/session/dashboard`);
  - froze session-level normalized statuses for readiness/freshness/resync and overall dashboard status;
  - added compact artifact coverage matrix contract (`available|partial|empty|unavailable`) for candle/stats.
- 2026-03-06:
- Stage 19 slice 3:
  - added Artifact Summary API (`GET /api/v1/artifacts/summary`);
  - added filter/timeframe contract (`venue`, `instrument`, `artifact`, `timeframe`) for artifact matrix consumption;
  - froze artifact summary status semantics (`available|partial|empty|unavailable`) and per-entry artifact status semantics (`available|empty|unavailable`).
- 2026-03-06:
- Stage 19 slice 4:
  - added cross-endpoint client-readiness contract suite for:
    - `GET /api/v1/instrument/overview`
    - `GET /api/v1/session/dashboard`
    - `GET /api/v1/artifacts/summary`
  - codified enum stability and semantic consistency checks (`ready` and `degraded` paths) to prevent contract drift before S20 client slices.
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
