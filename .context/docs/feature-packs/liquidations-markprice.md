# Feature Pack: Liquidations + MarkPrice

## Purpose
- Keep markprice/liquidation contract authority explicit from registry to delivery.
- Preserve deterministic dedup and canonical identity across venues.
- Track runtime gaps as TODO without overstating pipeline completeness.

## Inputs/Outputs
- Authority: [`docs/contracts/event-bus.md`](../../../docs/contracts/event-bus.md), [`docs/contracts/delivery-ws.md`](../../../docs/contracts/delivery-ws.md), [`docs/architecture/liquidations-markprice.md`](../../../docs/architecture/liquidations-markprice.md), [`docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`](../../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md).
- Inputs:
- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`
- Outputs:
- WS stream: `marketdata.markprice/{venue}/{symbol}/{timeframe}`
- WS stream: `marketdata.liquidation/{venue}/{symbol}/{timeframe}`
- `insights.<markprice_liquidation_snapshot>.v1.{venue}.{instrument}` (planned contract token)

## Invariants
- Dedup keys must be deterministic and replay-stable ([`docs/architecture/liquidations-markprice.md`](../../../docs/architecture/liquidations-markprice.md)).
- Canonical venue/instrument mapping must converge across exchanges ([`ADR-0011`](../../../docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md), [`ADR-0017`](../../../docs/adrs/ADR-0017-multi-exchange-normalization.md)).
- Subject taxonomy and partitioning remain deterministic ([`ADR-0014`](../../../docs/adrs/ADR-0014-stream-partitioning-strategy.md)).
- Replay over equivalent input must keep equivalent output series ([`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md)).

## Backpressure
- Bounded queues by `(venue,instrument)` and observable drop reasons ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Priority under overload keeps `markprice` ahead of `liquidation` ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).
- Ack boundary remains commit-based in ingest/disposition path ([`ADR-0013`](../../../docs/adrs/ADR-0013-backpressure-overload-policies.md)).

## Replay
- Replay invariants and time authority follow [`ADR-0015`](../../../docs/adrs/ADR-0015-deterministic-replay-time-invariants.md).
- Reuse replay package (`internal/shared/replay/*`) and ingest replay integration tests.
- Validate deterministic order and dedup via replay + converter completeness tests.

## Evidence Hooks
- `internal/shared/contracts/marketdata_registry.go`
- `internal/shared/contracts/authority_manifest.go`
- `internal/shared/contracts/converter_completeness_test.go`
- `internal/shared/codec/payload_codec.go`
- `internal/adapters/jetstream/ingest_policy.go`
- TODO: `internal/core/marketdata/app/normalize_markprice_liquidation.go`
- TODO: `internal/adapters/storage/timescale/markprice_liquidation_writer.go`

## Acceptance Tests
- `TestConverterCompleteness_MarkPriceTickV1` - `internal/shared/contracts/converter_completeness_test.go`
- `TestConverterCompleteness_LiquidationTickV1` - `internal/shared/contracts/converter_completeness_test.go`
- `TestRegisterMarketDataV1_RegistersAll` - `internal/shared/contracts/marketdata_registry_test.go`
- `TestIngestConformance_AckNakTermGoldenTable` - `internal/adapters/jetstream/ingest_conformance_test.go`
- `TestReplaySourceIntegration_FullDeterministicOrder` - `internal/adapters/jetstream/replay_source_integration_test.go`
- TODO: `TestMarkPriceDedupStrongKey` - `internal/core/marketdata/app/normalize_markprice_liquidation_test.go`
- TODO: `TestMarkPricePriorityOverLiquidationUnderPressure` - `internal/actors/marketdata/runtime/markprice_liquidation_pipeline_test.go`
