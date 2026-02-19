# Feature Pack: Liquidations + MarkPrice

**STATUS:** ACTIVE | **last_reviewed:** 2026-02-17

## Purpose
- Markprice/liquidation constraints only; authority: [liquidations-markprice](../../../docs/architecture/liquidations-markprice.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0011](../../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md).

## Inputs/Outputs
- Inputs: `marketdata.markprice.v1.{venue}.{instrument}`, `marketdata.liquidation.v1.{venue}.{instrument}`.
- Outputs: WS `marketdata.markprice/{venue}/{symbol}/{timeframe}`, WS `marketdata.liquidation/{venue}/{symbol}/{timeframe}`, (planned, not in event-bus.md matrix) `insights.markprice_liquidation_snapshot.v1.{venue}.{instrument}`.
- Mapping refs: [ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md), [delivery-ws](../../../docs/contracts/delivery-ws.md).

## Invariants
- LM-1: Dedup key must be deterministic and replay-stable ([liquidations-markprice](../../../docs/architecture/liquidations-markprice.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- LM-2: Same input message cannot create multiple commits (hot/cold) ([liquidations-markprice](../../../docs/architecture/liquidations-markprice.md)).
- LM-3: Canonical venue/instrument mapping is mandatory ([ADR-0011](../../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md), [ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md)).
- LM-4: Backpressure priority: markprice > liquidation ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- LM-5: Replay of same fixture preserves same output time series ([ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).

## Backpressure
- Bounded queue by `(venue,instrument)` with observable drop reason ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Under overload, keep `markprice` priority over `liquidation` ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Rollout
- Runtime stream activation is controlled by `consumer.enable_markprice_liquidation`.
- Capacity planning invariant:
  - `false` => `consumer.streams_per_ticker=2` (trade + bookdelta).
  - `true` => `consumer.streams_per_ticker=4` (trade + bookdelta + markprice + liquidation).

## Replay
- Replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden replay requirements: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/shared/contracts/marketdata_registry.go:18`
- `internal/shared/contracts/authority_manifest.go:35`
- `internal/shared/codec/payload_codec.go:78`
- `internal/adapters/jetstream/ingest_policy.go:59`
- `internal/core/marketdata/app/ingest.go`
- `internal/core/marketdata/app/normalize_markprice_liquidation.go`
- `internal/core/marketdata/app/dedup_keys_markprice_liquidation.go`
- `internal/adapters/exchange/binance/parser.go`
- `internal/adapters/exchange/bybit/parser.go`
- `internal/actors/marketdata/runtime/backpressure_queue.go`
- `internal/actors/marketdata/runtime/subsystem.go`

## Acceptance Tests
- `TestConverterCompleteness_MarkPriceTickV1` -> `internal/shared/contracts/converter_completeness_test.go:50`
- `TestConverterCompleteness_LiquidationTickV1` -> `internal/shared/contracts/converter_completeness_test.go:65`
- `TestRegisterMarketDataV1_RegistersAll` -> `internal/shared/contracts/marketdata_registry_test.go:10`
- `TestIngestConformance_AckNakTermGoldenTable` -> `internal/adapters/jetstream/ingest_conformance_test.go:15`
- `TestReplaySourceIntegration_FullDeterministicOrder` -> `internal/adapters/jetstream/replay_source_integration_test.go:17`
- `TestIngest_markPricePayloadEncodesAndPublishes` -> `internal/core/marketdata/app/ingest_test.go`
- `TestIngest_liquidationPayloadEncodesAndPublishes` -> `internal/core/marketdata/app/ingest_test.go`
- `TestMarkPriceDedupStrongKey` -> `internal/core/marketdata/app/normalize_markprice_liquidation_test.go`
- `TestLiquidationDedupStrongKey` -> `internal/core/marketdata/app/normalize_markprice_liquidation_test.go`
- `TestMarkPriceLiquidationCanonicalNormalization` -> `internal/core/marketdata/app/normalize_markprice_liquidation_test.go`
- `TestWSQueue_DropDepthKeepTrades_PreserveMarkPriceOverLiquidation` -> `internal/actors/marketdata/runtime/backpressure_queue_test.go`
- `TestMarkPriceLiquidationReplayGolden` -> `internal/actors/marketdata/runtime/markprice_liquidation_pipeline_test.go`
- `TestSubsystem_MarkPriceNormalization_setsCanonicalAndIdempotency` -> `internal/actors/marketdata/runtime/subsystem_test.go`
- `TestSubsystem_LiquidationDuplicateSkippedByNormalizer` -> `internal/actors/marketdata/runtime/subsystem_test.go`
- `TestParseMessage_MarkPriceUpdate` -> `internal/adapters/exchange/binance/parser_test.go`
- `TestParseMessage_ForceOrderLiquidation` -> `internal/adapters/exchange/binance/parser_test.go`
- `TestParseMessage_MarkPrice` -> `internal/adapters/exchange/bybit/parser_test.go`
- `TestParseMessage_Liquidation` -> `internal/adapters/exchange/bybit/parser_test.go`
