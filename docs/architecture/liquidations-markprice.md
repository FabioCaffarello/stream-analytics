# Liquidations + MarkPrice Architecture (End-to-End)

**Status:** Partially Implemented
**Owner:** Product Architect
**Last updated:** 2026-06-25
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0017-multi-exchange-normalization.md`

## Purpose

Define end-to-end flow for `marketdata.markprice` and `marketdata.liquidation`, with strong dedup keys and deterministic multi-exchange normalization.

## Terminology (canonical)

- `instrument`: canonical instrument identity key in envelope/domain.
- `subject`: bus route key (`marketdata.markprice.v1.binance.BTCUSDT`).
- `stream`: delivery key (`marketdata.markprice/binance/BTCUSDT/raw`).
- `envelope`: ADR-0002 fields (`ts_ingest`, `seq`, `idempotency_key`, etc).
- `payload`: versioned marketdata tick (`MarkPriceTickV1` / `LiquidationTickV1`).

## Data Planes

### Inputs

- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`

### Outputs

- WS stream: `marketdata.markprice/{venue}/{symbol}/{timeframe}`
- WS stream: `marketdata.liquidation/{venue}/{symbol}/{timeframe}`
- Planned derived event: `insights.<markprice_liquidation_snapshot>.v1.{venue}.{instrument}` (TBD registry key)

### Storage

Hot:
- `timescale.marketdata_markprice_hot`
- `timescale.marketdata_liquidation_hot`

Cold:
- `clickhouse.marketdata_markprice_cold`
- `clickhouse.marketdata_liquidation_cold`

Keys/idempotency:
- MarkPrice key: `hash(venue,instrument,ts_exchange,mark_price,index_price,funding_rate,seq)`
- Liquidation key: `hash(venue,instrument,side,price,size,ts_exchange,seq)`

## Contracts

- Event types and version already registered in `proto/registry.json`.
- Mandatory normalization:
- canonical venue identity in domain (`BINANCE`, `BYBIT`, ...), serialized for subject as lowercase token
- canonical key `instrument` (`BTCUSDT`) in envelope/subject partitioning
- canonical subject with lowercase venue + uppercase alnum instrument
- When exchange timestamp is absent, use `ts_ingest` as authoritative time.

## Invariants

- `LM-1`: dedup key must be deterministic and replay-stable.
- `LM-2`: same input message cannot create multiple commits (hot/cold).
- `LM-3`: multi-exchange normalization must converge to same canonical instrument.
- `LM-4`: backpressure priority follows ADR-0013 (`markprice` > `liquidation`).
- `LM-5`: replay of same fixture preserves same output time series.

## Backpressure

- Bounded queues by `(venue,instrument)`.
- Overload policy:
1. preserve `markprice` first;
2. compact liquidation events in short batches;
3. emit per-reason drop alert.
- ACK only on commit.

## Rollout Control

- Stream enablement is gated by `consumer.enable_markprice_liquidation`.
- With `enable_markprice_liquidation=false` (default), runtime baseline remains trade+bookdelta (`consumer.streams_per_ticker=2`).
- With `enable_markprice_liquidation=true`, websocket planning must use `consumer.streams_per_ticker=4` for spot runtimes (trade, depth, markprice, liquidation).

## Implementation Matrix

| Feature | Status | Evidence | Tests |
|---|---|---|---|
| Contract registration for markprice/liquidation | Existing | `proto/registry.json`, `internal/shared/contracts/marketdata_registry.go` | `internal/shared/contracts/marketdata_registry_test.go:TestRegisterMarketDataV1_RegistersAll` |
| Ingest baseline for markprice/liquidation payloads | Existing | `internal/core/marketdata/app/ingest.go` | `internal/core/marketdata/app/ingest_test.go:TestIngest_markPricePayloadEncodesAndPublishes`, `internal/core/marketdata/app/ingest_test.go:TestIngest_liquidationPayloadEncodesAndPublishes` |
| Proto/domain converter completeness | Existing | `internal/shared/contracts/authority_manifest.go` | `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_MarkPriceTickV1`, `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_LiquidationTickV1` |
| Codec compatibility JSON/protobuf | Existing | `internal/shared/codec/payload_codec.go` | `internal/shared/codec/payload_codec_test.go` |
| Ack/nak/term ingest semantics | Existing | `internal/adapters/jetstream/consumer.go` | `internal/adapters/jetstream/ingest_conformance_test.go:TestIngestConformance_AckNakTermGoldenTable` |
| Runtime backpressure priority (`markprice` over `liquidation`) | Existing | `internal/actors/marketdata/runtime/backpressure_queue.go` | `internal/actors/marketdata/runtime/backpressure_queue_test.go:TestWSQueue_DropDepthKeepTrades_PreserveMarkPriceOverLiquidation` |
| Dedicated normalization use case for markprice/liquidation | Existing (baseline) | `internal/core/marketdata/app/normalize_markprice_liquidation.go` | `internal/core/marketdata/app/normalize_markprice_liquidation_test.go:TestMarkPriceDedupStrongKey`, `internal/core/marketdata/app/normalize_markprice_liquidation_test.go:TestLiquidationDedupStrongKey`, `internal/core/marketdata/app/normalize_markprice_liquidation_test.go:TestMarkPriceLiquidationCanonicalNormalization` |
| Dedicated runtime pipeline for markprice/liquidation | Existing (baseline) | `internal/actors/marketdata/runtime/subsystem.go`, `internal/adapters/exchange/binance/parser.go`, `internal/adapters/exchange/bybit/parser.go` | `internal/actors/marketdata/runtime/subsystem_test.go:TestSubsystem_MarkPriceNormalization_setsCanonicalAndIdempotency`, `internal/actors/marketdata/runtime/subsystem_test.go:TestSubsystem_LiquidationDuplicateSkippedByNormalizer`, `internal/adapters/exchange/binance/parser_test.go:TestParseMessage_MarkPriceUpdate`, `internal/adapters/exchange/binance/parser_test.go:TestParseMessage_ForceOrderLiquidation`, `internal/adapters/exchange/bybit/parser_test.go:TestParseMessage_MarkPrice`, `internal/adapters/exchange/bybit/parser_test.go:TestParseMessage_Liquidation` |
| Hot/cold durable writers for markprice/liquidation | TODO | `internal/adapters/storage/timescale/markprice_liquidation_writer.go` (TODO), `internal/adapters/storage/clickhouse/markprice_liquidation_writer.go` (TODO) | `internal/adapters/storage/markprice_liquidation_writer_test.go` (TODO) |

## Storage Strategy

- Timescale: operational risk queries and recent windows.
- ClickHouse: long-term history for correlation and incident analysis.
- Idempotent upsert in hot, deduplicated append in cold.

## Replay Strategy

- Replay ordered by `(ts_ingest,seq)` per partition.
- Golden windows (1m/5m) compare:
1. latest mark price;
2. liquidation count/volume;
3. aggregate hash per window.

## Observability

- `markprice_ingest_lag_ms{venue,instrument}`
- `liquidation_ingest_lag_ms{venue,instrument}`
- `markprice_dedup_total{venue,instrument}`
- `liquidation_dedup_total{venue,instrument}`
- `markprice_liquidation_drop_total{event_type,reason}`
- `markprice_liquidation_queue_depth{venue,instrument}`

Minimum:
- lag
- drop
- queue depth

## Acceptance Tests

Existing tests:
- `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_MarkPriceTickV1`
- `internal/shared/contracts/converter_completeness_test.go:TestConverterCompleteness_LiquidationTickV1`
- `internal/shared/contracts/marketdata_registry_test.go:TestRegisterMarketDataV1_RegistersAll`
- `internal/adapters/jetstream/ingest_conformance_test.go:TestIngestConformance_AckNakTermGoldenTable`
- `internal/adapters/jetstream/replay_source_integration_test.go:TestReplaySourceIntegration_FullDeterministicOrder`
- `internal/core/marketdata/app/ingest_test.go:TestIngest_markPricePayloadEncodesAndPublishes`
- `internal/core/marketdata/app/ingest_test.go:TestIngest_liquidationPayloadEncodesAndPublishes`
- `internal/actors/marketdata/runtime/backpressure_queue_test.go:TestWSQueue_DropDepthKeepTrades_PreserveMarkPriceOverLiquidation`
- `internal/actors/marketdata/runtime/markprice_liquidation_pipeline_test.go:TestMarkPriceLiquidationReplayGolden`

Tests to create for feature parity:
- durable storage writers remain planned.

## Evidence Hooks

Current evidence:
- `internal/shared/contracts/marketdata_registry.go`
- `internal/shared/contracts/authority_manifest.go`
- `internal/core/marketdata/domain/payloads.go`
- `internal/shared/codec/payload_codec_test.go`
- `internal/shared/contracts/converter_completeness_test.go`
- `internal/core/marketdata/app/dedup_keys_markprice_liquidation.go`
- `internal/adapters/exchange/binance/parser.go`
- `internal/adapters/exchange/bybit/parser.go`
- `internal/actors/marketdata/runtime/backpressure_queue.go`
- `internal/actors/marketdata/runtime/subsystem.go`
- `internal/actors/marketdata/runtime/backpressure_queue_test.go`
- `internal/actors/marketdata/runtime/markprice_liquidation_pipeline_test.go`

TODO hooks (skeleton):
- `internal/adapters/storage/timescale/markprice_liquidation_writer.go` (TODO)
- `internal/adapters/storage/clickhouse/markprice_liquidation_writer.go` (TODO)

## Failure Modes

- Cross-exchange timestamp skew:
  - Mitigation: authority by `ts_ingest` + skew metric.
- Weak dedup key causing duplicates:
  - Mitigation: strong key + collision test.
- Overload with silent loss:
  - Mitigation: mandatory drop counters + alerts.
- Poison payload:
  - Mitigation: quarantine (`quarantine.v1.*`) + operational DLQ.
