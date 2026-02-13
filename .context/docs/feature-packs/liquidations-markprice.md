# Feature Pack: Liquidations + MarkPrice

## Purpose
- Markprice/liquidation constraints only; authority: [liquidations-markprice](../../../docs/architecture/liquidations-markprice.md), [event-bus](../../../docs/contracts/event-bus.md), [ADR-0011](../../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md).

## Inputs/Outputs
- Inputs: `marketdata.markprice.v1.{venue}.{instrument}`, `marketdata.liquidation.v1.{venue}.{instrument}`.
- Outputs: WS `marketdata.markprice/{venue}/{symbol}/{timeframe}`, WS `marketdata.liquidation/{venue}/{symbol}/{timeframe}`, planned `insights.<markprice_liquidation_snapshot>.v1.{venue}.{instrument}` (planned, not in event-bus.md matrix).
- Mapping refs: [ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md), [delivery-ws](../../../docs/contracts/delivery-ws.md).

## Invariants
- Dedup key must be deterministic and replay-stable ([liquidations-markprice](../../../docs/architecture/liquidations-markprice.md), [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).
- Canonical venue/instrument mapping is mandatory ([ADR-0011](../../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md), [ADR-0017](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md)).
- Subject taxonomy remains deterministic ([ADR-0014](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).

## Backpressure
- Bounded queue by `(venue,instrument)` with observable drop reason ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Under overload, keep `markprice` priority over `liquidation` ([ADR-0013](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Replay
- Replay authority: [ADR-0015](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Golden replay requirements: [RFC-0009](../../../docs/rfcs/RFC-0009-W8-deterministic-replay-golden-tests.md).

## Evidence Hooks
- `internal/shared/contracts/marketdata_registry.go:18`
- `internal/shared/contracts/authority_manifest.go:35`
- `internal/shared/codec/payload_codec.go:78`
- `internal/adapters/jetstream/ingest_policy.go:59`
- TODO: `internal/core/marketdata/app/normalize_markprice_liquidation.go`

## Acceptance Tests
- `TestConverterCompleteness_MarkPriceTickV1` -> `internal/shared/contracts/converter_completeness_test.go:50`
- `TestConverterCompleteness_LiquidationTickV1` -> `internal/shared/contracts/converter_completeness_test.go:65`
- `TestRegisterMarketDataV1_RegistersAll` -> `internal/shared/contracts/marketdata_registry_test.go:10`
- `TestIngestConformance_AckNakTermGoldenTable` -> `internal/adapters/jetstream/ingest_conformance_test.go:15`
- `TestReplaySourceIntegration_FullDeterministicOrder` -> `internal/adapters/jetstream/replay_source_integration_test.go:17`
- TODO: `TestMarkPriceDedupStrongKey` -> `internal/core/marketdata/app/normalize_markprice_liquidation_test.go`
