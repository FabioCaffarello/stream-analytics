# Liquidations + MarkPrice Architecture (End-to-End)

**Status:** Draft
**Owner:** Product Architect
**Last updated:** 2026-02-13
**Relates to:** `docs/adrs/ADR-0002-event-envelope-and-versioning.md`, `docs/adrs/ADR-0004-bus-nats-jetstream.md`, `docs/adrs/ADR-0011-marketdata-binance-canonical-instrument-and-event-mapping.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/adrs/ADR-0015-deterministic-replay-time-invariants.md`, `docs/adrs/ADR-0017-multi-exchange-normalization.md`

## Purpose

Define end-to-end flow for `marketdata.markprice` and `marketdata.liquidation`, with strong dedup keys and deterministic multi-exchange normalization.

## Data Planes

### Inputs

- `marketdata.markprice.v1.{venue}.{instrument}`
- `marketdata.liquidation.v1.{venue}.{instrument}`

### Outputs

- WS stream: `marketdata.markprice/{venue}/{symbol}/{timeframe}`
- WS stream: `marketdata.liquidation/{venue}/{symbol}/{timeframe}`
- Planned derived event: `insights.liquidation.markprice_snapshot.v1.{venue}.{instrument}`

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
- canonical uppercase `venue` (`BINANCE`, `BYBIT`, ...)
- canonical key `instrument` (`BTCUSDT`) in envelope
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

Planned test names:
- `TestMarkPriceDedupStrongKey`
- `TestLiquidationDedupStrongKey`
- `TestMarkPriceLiquidationCanonicalNormalization`
- `TestMarkPriceLiquidationReplayGolden`
- `TestMarkPricePriorityOverLiquidationUnderPressure`

Scenarios:
- duplicate envelopes with slight payload variation;
- out-of-order events;
- multi-exchange same instrument;
- burst preserving markprice priority.

## Evidence Hooks

Current evidence:
- `internal/shared/contracts/marketdata_registry.go`
- `internal/shared/contracts/authority_manifest.go`
- `internal/core/marketdata/domain/payloads.go`
- `internal/shared/codec/payload_codec_test.go`

TODO hooks (skeleton):
- `internal/core/marketdata/app/normalize_markprice_liquidation.go` (TODO)
- `internal/core/marketdata/app/dedup_keys_markprice_liquidation.go` (TODO)
- `internal/adapters/storage/timescale/markprice_liquidation_writer.go` (TODO)
- `internal/adapters/storage/clickhouse/markprice_liquidation_writer.go` (TODO)
- `internal/actors/marketdata/runtime/markprice_liquidation_pipeline_test.go` (TODO)

## Failure Modes

- Cross-exchange timestamp skew:
  - Mitigation: authority by `ts_ingest` + skew metric.
- Weak dedup key causing duplicates:
  - Mitigation: strong key + collision test.
- Overload with silent loss:
  - Mitigation: mandatory drop counters + alerts.
- Poison payload:
  - Mitigation: quarantine (`quarantine.v1.*`) + operational DLQ.
