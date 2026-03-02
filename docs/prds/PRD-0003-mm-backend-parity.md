# PRD-0003 — MarketMonkey Backend Parity

**Status:** Implemented — functional milestones M1–M5 validated (2026-02-20). Remaining NF benchmarks pending (soak/benchmem).
**Date:** 2026-02-20
**Owner:** Chief Architect
**Relates to:** `docs/prd/PRD-0002-backend-stable-and-odin-ready.md`, `docs/rfcs/RFC-0011-product-parity-marketmonkey.md`, `.context/evidence/swot-market-raccoon-v7-2026-02-20.md`, `docs/architecture/TRUTH-MAP.md`

---

## Problem

Market Raccoon achieves architecture score 5/5 and code quality 5/5 (SWOT v7), but scores only 3.5/5 on functional coverage when compared to the MarketMonkey reference. Three critical feature gaps prevent dashboard parity: (1) no multi-timeframe candle aggregation from trades, (2) no multi-TF stats with liquidation volume, funding rate, and mark price, and (3) volume/heatmap binning algorithms differ from MM, producing incompatible grid layouts. Additionally, the WS `getrange` historical query contract exists as a port but has no backend wiring.

## Goals

1. **G1 — Multi-TF candle pipeline.** Candles are aggregated from trade events across 6 timeframes (1m, 5m, 15m, 1h, 4h, 1d) and published to both realtime delivery and cold storage. Golden tests validate output matches MM reference for identical input trades.
2. **G2 — Multi-TF stats pipeline.** Stats windows include liquidation buy/sell volume, funding rate average/last, and mark price per timeframe. `StatsWindowV1.FundingRateAvg/Last` fields are populated and delivered end-to-end.
3. **G3 — Binning alignment.** Volume profile uses 0.5% price-percentage grouping and heatmap uses 2.5% price-percentage grouping, matching MarketMonkey's `common.CalculateBinSize()` algorithm. Golden tests validate bin boundaries match MM for reference price/tickSize pairs.
4. **G4 — Liquidation pipeline end-to-end.** Liquidation events flow from parser through aggregation, storage, and delivery. Liquidation volume per TF is tracked in stats.
5. **G5 — GetRange historical queries.** WS sessions can request historical candle and stats ranges; the `DeliveryRangeStore` port is wired to TimescaleDB backend.
6. **G6 — Storage writer consolidation.** Duplicated writer logic is extracted to shared helpers, reducing ~600-800 LOC and unifying error handling across TimescaleDB and ClickHouse.
7. **G7 — Test coverage hardened.** `jetstream/ingest_policy.go` (397 lines) has dedicated unit tests. All critical files >100 LOC have isolated test coverage.

## Non-Goals

- **Odin client implementation.** Backend-only scope.
- **CBOR wire encoding.** Protobuf remains the primary encoding. CBOR support is deferred to a future PRD.
- **Per-market stats enable flag.** All markets compute stats uniformly. Selective enabling is deferred.
- **L1/L2 cold storage integration.** ClickHouse writers exist and are tested; full tiered migration pipeline is deferred per ADR-0006 amendment.
- **New exchange integrations.** 6-exchange parity is sufficient. No new exchanges in this PRD.
- **CI benchmark gating.** Allocation budget infrastructure is tracked but not gated in CI within this PRD.

---

## Requirements

### Functional

| ID | Requirement | Priority | Acceptance Criteria |
|----|-------------|----------|---------------------|
| **FR-1** | Multi-TF candle rollup from 1m base candle | P0 | `RollupCandle(source []CandleV1, toInterval) CandleV1` produces correct OHLCV for 5m/15m/1h/4h/1d. Golden test with 1000 trades matches expected output per TF. |
| **FR-2** | Processor emits candles per configured TF | P0 | Processor actor publishes `aggregation.candle` envelopes for each of 6 TFs. Soak test confirms all TFs emitted within 5% of expected count. |
| **FR-3** | Candle storage writers accept multi-TF artifacts | P0 | TimescaleDB and ClickHouse candle writers store TF-tagged candles. Roundtrip read-after-write test passes for each TF. |
| **FR-4** | Candle delivery per TF via WS | P0 | WS session subscribed to `aggregation.candle/{venue}/{symbol}/{tf}` receives candles for that TF only. Contract test validates filtering. |
| **FR-5** | Funding rate aggregation from mark price events | P0 | `BuildFundingRateFromEvents` produces `StatsWindowV1` with `FundingRateAvg` and `FundingRateLast` populated. Unit test with 100 mark price events validates calculation. |
| **FR-6** | Liquidation volume tracking per TF | P0 | `StatsWindowV1.LiquidationBuyVolume` and `LiquidationSellVolume` populated from liquidation events. Unit test validates buy/sell separation. |
| **FR-7** | Mark price per TF in stats | P0 | `StatsWindowV1.MarkPrice` reflects latest mark price within the stats window. Unit test validates. |
| **FR-8** | Stats storage writers include funding/liq/mark fields | P0 | TimescaleDB and ClickHouse stats writers persist all new fields. Roundtrip read-after-write test passes. |
| **FR-9** | Stats delivery per TF via WS | P0 | WS session receives stats with funding/liq/mark fields populated. Wire contract test validates payload shape. |
| **FR-10** | Volume profile binning uses 0.5% grouping | P1 | `CalculateVolumeBinSize(price, tickSize)` returns bin size matching MM's `binFactorV=0.005`. Golden test with 20 reference price/tickSize pairs validates exact match. |
| **FR-11** | Heatmap binning uses 2.5% grouping | P1 | `CalculateHeatmapBinSize(price, tickSize)` returns bin size matching MM's `binFactorP=0.025`. Golden test with 20 reference price/tickSize pairs validates exact match. |
| **FR-12** | Binning step alignment (tick-size divisible) | P1 | Bin size is always a multiple of tick size. If computed bin < tickSize, bin = tickSize. Matches MM `common.go:64`. |
| **FR-13** | Liquidation events flow from parser to storage | P1 | Liquidation events are aggregated into per-TF volume, stored in TimescaleDB+ClickHouse, and delivered via WS. E2E integration test with fixture validates. |
| **FR-14** | GetRange WS handler wired to TimescaleDB | P1 | WS `getrange` request for candles returns data from `DeliveryRangeStore` (TimescaleDB). Integration test validates response payload for a known fixture. |
| **FR-15** | GetRange WS handler for stats | P1 | WS `getrange` request for stats returns data from TimescaleDB. Integration test validates. |
| **FR-16** | Storage writer helpers extracted | P1 | `internal/adapters/storage/writer_helpers.go` contains shared marshaling, validation, and error wrapping. All 8 writers use helpers. No behavior change; all existing tests pass. |
| **FR-17** | `ingest_policy_test.go` unit tests | P1 | 15+ unit tests for `jetstream/ingest_policy.go` covering policy validation, stream config, and error paths. |
| **FR-18** | TimescaleDB image version pinned | P2 | `docker-compose.yml` uses specific TimescaleDB image tag (not `latest-pg16`). |

### Non-Functional

| ID | Requirement | Metric |
|----|-------------|--------|
| **NF-1** | Multi-TF candle rollup must not degrade ingest throughput | Soak: >= 100K evt/sec (vs baseline 117K) |
| **NF-2** | Stats aggregation adds < 5 us p95 per event | `BenchmarkStatsAggregation` benchmem |
| **NF-3** | Binning calculation adds < 100 ns per call | `BenchmarkCalculateBinSize` |
| **NF-4** | GetRange query responds within 50ms p95 for 1000 candles | `BenchmarkGetRange` |
| **NF-5** | Writer helper refactor introduces zero new allocations | `go test -benchmem` before/after comparison |
| **NF-6** | All new code follows zero-`fmt.Sprintf` policy in core/actors | Grep verification: zero matches |
| **NF-7** | All new domain code uses `*problem.Problem`, never bare `error` | Import guard test |
| **NF-8** | All new code passes `-race` detector | `make test-workspace-race` |

---

## Milestones

### M1 — Multi-TF Candle Pipeline (P0)

**Deliverables:**
- `aggregation/domain/candle_rollup.go` — TF rollup logic
- `aggregation/app/build_candle.go` — multi-TF orchestration (extend existing)
- `aggregation/runtime/processor.go` — emit candles per TF
- Storage writers accept TF-tagged candles (TimescaleDB + ClickHouse)
- WS delivery routes candles per TF
- Golden tests comparing with MM candle output

**Gate:**
```bash
make test-workspace            # FR-1 through FR-4 tests pass
make test-workspace-race       # zero data races
go test -bench BenchmarkCandleRollup -benchmem  # NF-1 baseline
```

**Acceptance:**
- 6 TFs (1m, 5m, 15m, 1h, 4h, 1d) emitted, stored, and delivered
- Golden test: 1000 trade fixture produces byte-identical candle output per TF
- Soak: >= 100K evt/sec sustained with multi-TF enabled

---

### M2 — Multi-TF Stats + Funding + Liquidation Pipeline (P0)

**Deliverables:**
- `aggregation/app/build_funding_rate.go` — funding rate use case
- `aggregation/app/build_stats.go` — extend with liq/funding/mark fields
- `aggregation/runtime/processor.go` — wire mark price → funding rate → stats
- Stats storage writers include new fields
- Liquidation aggregation into per-TF volume
- WS delivery includes funding/liq/mark in stats payload

**Gate:**
```bash
make test-workspace            # FR-5 through FR-9 tests pass
make test-workspace-race
go test -bench BenchmarkStatsAggregation -benchmem  # NF-2
```

**Acceptance:**
- `StatsWindowV1.FundingRateAvg`, `FundingRateLast`, `LiquidationBuyVolume`, `LiquidationSellVolume`, `MarkPrice` populated
- Unit tests with 100 mark price + 50 liquidation events validate correct calculation
- Roundtrip storage test: write stats → read stats → fields match
- WS wire contract test: stats payload includes all new fields

---

### M3 — Binning Alignment (P1)

**Deliverables:**
- `insights/domain/binning.go` — `CalculateBinSize(price, tickSize, groupingFactor)` matching MM algorithm
- `insights/domain/volume_profile.go` — update `AssignVPVRBucket` to use 0.5% grouping
- `insights/app/build_heatmap.go` — update to use 2.5% grouping
- Golden test fixtures: 20 price/tickSize pairs with expected bin sizes from MM

**Gate:**
```bash
make test-workspace            # FR-10 through FR-12 tests pass
go test -bench BenchmarkCalculateBinSize -benchmem  # NF-3
```

**Acceptance:**
- `CalculateVolumeBinSize(50000, 0.01)` returns same value as MM `common.CalculateVolumeBinSize(50000, 0.01)`
- `CalculateHeatmapBinSize(50000, 0.01)` returns same value as MM `common.CalculateHeatmapBinSize(50000, 0.01)`
- All 20 golden test pairs pass exact match
- Bin size always >= tickSize
- Bin size always tick-divisible

---

### M4 — Liquidation E2E + GetRange Integration (P1)

**Deliverables:**
- Liquidation event routing: parser → aggregation → storage → delivery
- `DeliveryRangeStore` wired to TimescaleDB backend for candles and stats
- WS `getrange` handler implementation
- Integration tests for historical queries

**Gate:**
```bash
make test-workspace            # FR-13 through FR-15 tests pass
go test -bench BenchmarkGetRange -benchmem  # NF-4
```

**Acceptance:**
- Liquidation events from fixture flow through full pipeline; stored volume matches expected buy/sell split
- WS `getrange` for candles returns correct data from TimescaleDB for a known fixture
- WS `getrange` for stats returns correct data including funding/liq fields
- GetRange p95 < 50ms for 1000-candle query

---

### M5 — Refactor + Hardening (P1)

**Deliverables:**
- `internal/adapters/storage/writer_helpers.go` — shared writer utilities
- All 8 writers refactored to use helpers (no behavior change)
- `internal/adapters/jetstream/ingest_policy_test.go` — 15+ unit tests
- TimescaleDB docker image pinned
- Insights string normalization audit (remove redundant ToUpper/TrimSpace)

**Gate:**
```bash
make test-workspace            # FR-16 through FR-18 tests pass
make test-workspace-race
go test -benchmem ./internal/adapters/storage/...  # NF-5: zero new allocs
```

**Acceptance:**
- `writer_helpers.go` used by all 8 writers; ~600-800 LOC reduction measured
- `ingest_policy_test.go` covers: valid policy, invalid policy, stream config overrides, error propagation, edge cases
- TimescaleDB image tag is specific version (e.g., `2.16.1-pg16`)
- All existing tests pass unchanged
- Zero `fmt.Sprintf` in core/actors (grep verified)

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Multi-TF rollup introduces calculation drift vs MM | High — candle values diverge | Golden tests per TF with MM-derived fixtures; exact floating-point comparison |
| Binning algorithm edge cases (very small/large prices) | Medium — bin boundaries incorrect at extremes | Port MM's exact algorithm including floor/ceil logic; test with BTC (50K), ETH (3K), SHIB (0.00001) |
| Funding rate averaging window semantics differ from MM | Medium — dashboard shows wrong funding | Document window semantics explicitly; validate with real exchange data fixtures |
| Storage schema migration for new stats fields | Medium — breaking change on existing data | Additive-only columns (nullable); goose migration with zero downtime |
| Writer refactor introduces regression | Medium — data loss or corruption | Refactor is pure mechanical extraction; all 8 existing writer test suites must pass unchanged |
| GetRange query performance under cold cache | Low — slow first query | Add connection pool warm-up; budget NF-4 is generous (50ms p95) |
| GC pressure from multi-TF candle state | Low — baseline is 117K evt/sec | FieldHasher pattern for new code; benchmem CI gate; budget NF-1 >= 100K |

---

## Success Metrics

- **Multi-TF candle correctness:** 100% golden test pass rate across 6 TFs
- **Stats completeness:** `FundingRateAvg`, `FundingRateLast`, `LiquidationBuyVolume`, `LiquidationSellVolume`, `MarkPrice` non-zero in soak output
- **Binning alignment:** 100% exact match with MM for 20 reference price/tickSize pairs
- **GetRange latency:** p95 < 50ms for 1000-candle historical query
- **Throughput retention:** >= 100K evt/sec with all new pipelines active (vs baseline 117K)
- **Code reduction:** >= 500 LOC removed via writer helper extraction
- **Test count:** >= 1,400 total tests (up from 1,333)
- **Zero regressions:** All existing 1,333 tests continue to pass

---

## Dependencies

| Dependency | Type | Status |
|------------|------|--------|
| `aggregation/domain/candle.go` (CandleV1) | Code | Existing |
| `aggregation/domain/stats.go` (StatsWindowV1) | Code | Existing (fields defined, unpopulated) |
| `aggregation/app/build_candle.go` | Code | Existing (1m only) |
| `aggregation/app/build_stats.go` | Code | Existing (basic OHLCV) |
| `insights/domain/volume_profile.go` | Code | Existing (tick-size bucketing) |
| `insights/app/build_heatmap.go` | Code | Existing (implicit rounding) |
| `delivery/ports/ports.go` (RangeStore) | Code | Existing (interface defined) |
| `adapters/storage/timescale/delivery_range_store.go` | Code | Existing (partial) |
| `adapters/storage/timescale/candle_writer.go` | Code | Existing |
| `adapters/storage/clickhouse/candle_writer.go` | Code | Existing |
| TimescaleDB + ClickHouse | Infra | Running (docker-compose) |
| NATS JetStream | Infra | Running |
| MarketMonkey `common/common.go` | Reference | Read-only (`zip/`) |
| MarketMonkey `actor/trade/trade.go` | Reference | Read-only (`zip/`) |
| MarketMonkey `actor/stat/stat.go` | Reference | Read-only (`zip/`) |

---

## References

- [SWOT v7 Analysis](../../.context/evidence/swot-market-raccoon-v7-2026-02-20.md)
- [RFC-0011 — Product Parity v1](../rfcs/RFC-0011-product-parity-marketmonkey.md)
- [PRD-0002 — Backend Stable & Odin-Ready](../prd/PRD-0002-backend-stable-and-odin-ready.md)
- [ADR-0006 — Storage Hot vs Cold](../adrs/ADR-0006-storage-hot-vs-cold.md)
- [ADR-0002 — Event Envelope and Versioning](../adrs/ADR-0002-event-envelope-and-versioning.md)
- [ADR-0013 — Backpressure Overload Policies](../adrs/ADR-0013-backpressure-overload-policies.md)
- [Architecture TRUTH-MAP](../architecture/TRUTH-MAP.md)
- [MarketMonkey Audit Pack](../../zip/01-marketmonkey-files/marketmonkey/docs/architecture/MARKETMONKEY-AUDIT-PACK.md)

---

## Validation

Validation evidence for PRD-0003 implementation is recorded in `.context/evidence/prd0003-validation-report.md` (2026-02-20). Summary:

- **M2 (Multi‑TF stats + funding + liquidation):** IMPLEMENTED and validated (unit, golden, storage roundtrip, WS delivery).
- **M4 (Liquidation E2E + GetRange):** IMPLEMENTED and validated (parser → aggregation → storage → delivery; `PgRangeStore` + `executeGetRange`).
- **M5 (Writer helpers + Hardening):** IMPLEMENTED and validated (`writer_helpers.go` refactor used by all writers; ingest policy tests added; TimescaleDB image pinned).

Remaining validation actions (non-blocking):

- Run soak/bench workloads to confirm NF targets (NF-1, NF-2, NF-4, NF-5). See validation report for command list.

See `.context/evidence/prd0003-validation-report.md` for full test outputs, benchmarks and E2E logs.
