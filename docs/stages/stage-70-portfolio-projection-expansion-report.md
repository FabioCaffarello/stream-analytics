# Stage 70 — Portfolio Projection Expansion Report

**Date:** 2026-03-08
**Branch:** `codex/s9-legacy-removal-cutover`
**Status:** COMPLETE

## Objective

Evolve portfolio from minimal projection to rich operational projection, maintaining the clean `strategy.intent → execution.event → portfolio.state` pipeline.

## Changes

### Domain Enrichment (`internal/core/portfolio/domain/`)

**state.go — PositionV1 enriched:**
- `TradeCount` (int32) — number of fills applied to this position
- `VolumeTradedUSD` (float64) — cumulative fill notional in USD
- `LastFillMs` (int64) — timestamp of most recent fill
- `Side` (string) — current position side: "long", "short", or "" (flat)

**state.go — FillSummaryV1 added:**
- `TotalTradeCount`, `TotalVolumeTradedUSD` — aggregate fill metrics
- `WinCount`, `LossCount` — profitable/unprofitable position closures
- `LargestWinUSD`, `LargestLossUSD` — extremes for risk diagnostics
- `TurnoverUSD` — total absolute notional flow

**state.go — PortfolioStateV1 enriched:**
- `FillSummary FillSummaryV1` — aggregate fill metrics per venue-scoped state

**snapshot.go — Read Model Types (NEW):**
- `AccountSnapshotV1` — aggregates all venue-scoped states under one account (per-venue positions/balances, total equity/PnL/margin/leverage, fill summary)
- `VenuePositionV1` — groups positions+balances from a single venue within an account
- `PortfolioSummaryV1` — global operational view across all accounts (per-account summaries, global equity/PnL/margin/leverage, total positions/open orders, fill summary)
- `AccountSummaryV1` — lightweight per-account summary within global view

**event_catalog.go — Event Catalog (NEW):**
- `PortfolioEventContract` struct + `PortfolioEventCatalog` registry
- `PortfolioEventCatalogByType()` for O(1) lookup
- `PortfolioReadModelCatalog` — lists non-wire read model types

### Application Layer (`internal/core/portfolio/app/`)

**bootstrap_projector.go — Enriched:**
- `positionState` now tracks: `tradeCount`, `volumeTradedUSD`, `lastFillMs`, `winCount`, `lossCount`, `largestWinUSD`, `largestLossUSD`, `turnoverUSD`
- `applyFill()` updated to record fill metrics and win/loss classification per position closure
- `Apply()` emits enriched PositionV1 (with trade count, volume, side) and FillSummaryV1

**snapshot_builder.go — Snapshot Builder (NEW):**
- `SnapshotStates()` — returns deterministically ordered venue-scoped states from accumulated projector state
- `BuildAccountSnapshot(accountID, nowMs)` — aggregates all venues for an account into `AccountSnapshotV1`
- `BuildPortfolioSummary(nowMs)` — aggregates all accounts into `PortfolioSummaryV1`
- All outputs are deterministic (sorted by key), replay-safe, and idempotent

### Ports Layer (`internal/core/portfolio/ports/`) — NEW

**ports.go — Write Interfaces:**
- `PortfolioStateWriter` — persists venue-scoped portfolio state projections
- `AccountSnapshotWriter` — persists account-scoped aggregation snapshots
- `PortfolioSummaryWriter` — persists global portfolio summary snapshots

**readers.go — Read Interfaces:**
- `PortfolioStateReader` — query by account/venue/symbol, get latest
- `AccountSnapshotReader` — query by account, time range, get latest
- `PortfolioSummaryReader` — query by time range, get latest

### Proto Schema (`proto/portfolio/v1/state.proto`)

- `PositionV1`: added fields 8-11 (trade_count, volume_traded_usd, last_fill_ms, side)
- `FillSummaryV1`: new message with fields 1-7
- `PortfolioStateV1`: added field 14 (fill_summary)
- Generated Go code regenerated

### Codec Converters (`internal/shared/contracts/`)

- `DomainToProtoPortfolioStateV1`: maps new position fields + FillSummaryV1
- `ProtoToDomainPortfolioStateV1`: maps new position fields + FillSummaryV1

## Test Results

| Suite | Tests | Status |
|-------|-------|--------|
| portfolio/domain | 10 | PASS |
| portfolio/app (projector) | 5 | PASS |
| portfolio/app (snapshot) | 11 | PASS |
| shared/contracts (codec roundtrip) | 2 | PASS |
| actors/runtime (E2E flow) | 4 | PASS |
| **Total** | **32** | **ALL PASS** |

### New Test Coverage

- `TestSnapshotStates_Deterministic` — ordered output across venues
- `TestSnapshotStates_FillMetrics` — trade count, volume, side propagation
- `TestBuildAccountSnapshot_SingleVenue` — single venue aggregation + validation
- `TestBuildAccountSnapshot_MultiVenue` — cross-venue aggregation + sorted output
- `TestBuildAccountSnapshot_EmptyAccount` — no-op for unknown accounts
- `TestBuildAccountSnapshot_BlankAccountID` — rejects blank input
- `TestBuildPortfolioSummary_MultiAccount` — cross-account aggregation
- `TestBuildPortfolioSummary_Empty` — no-op for empty projector
- `TestBuildPortfolioSummary_VenueCountPerAccount` — venue count accuracy
- `TestFillMetrics_WinLossTracking` — long position close → win classification
- `TestFillMetrics_ShortSideLoss` — short position close → loss classification

## Boundary Contracts

| Contract | Status |
|----------|--------|
| execution.event → portfolio.state | Preserved — projector consumes ExecutionEventV1 unchanged |
| portfolio.state schema | Extended — new fields are additive (proto backward-compatible) |
| portfolio → strategy | No coupling — portfolio does not import strategy |
| portfolio → execution governance | No coupling — portfolio reads events only |
| Read models (snapshot/summary) | Not wire types — query-side only, no envelope bus coupling |

## Architecture Notes

1. **Projections are deterministic**: Same event sequence → same portfolio state + snapshots
2. **Replay-safe**: Fill metrics accumulate identically on replay (tradeCount, volumeTraded are monotonic)
3. **Idempotent**: SnapshotStates/BuildAccountSnapshot/BuildPortfolioSummary are pure reads of accumulated state
4. **No strategy leakage**: Portfolio only knows execution events, never strategy intents
5. **No governance leakage**: Portfolio never evaluates grants, credentials, or control plane
6. **Wire changes are additive**: Proto field additions (8-14) are backward-compatible — old consumers ignore new fields

## Files Changed

| File | Action |
|------|--------|
| `internal/core/portfolio/domain/state.go` | Modified — enriched PositionV1, added FillSummaryV1 |
| `internal/core/portfolio/domain/snapshot.go` | Created — AccountSnapshotV1, PortfolioSummaryV1 |
| `internal/core/portfolio/domain/snapshot_test.go` | Created — 7 validation tests |
| `internal/core/portfolio/domain/event_catalog.go` | Created — event catalog |
| `internal/core/portfolio/domain/event_catalog_test.go` | Created — 3 catalog tests |
| `internal/core/portfolio/app/bootstrap_projector.go` | Modified — fill metric tracking |
| `internal/core/portfolio/app/snapshot_builder.go` | Created — SnapshotStates, BuildAccountSnapshot, BuildPortfolioSummary |
| `internal/core/portfolio/app/snapshot_builder_test.go` | Created — 11 snapshot tests |
| `internal/core/portfolio/ports/ports.go` | Created — write interfaces |
| `internal/core/portfolio/ports/readers.go` | Created — read interfaces |
| `proto/portfolio/v1/state.proto` | Modified — new fields + FillSummaryV1 message |
| `internal/shared/proto/gen/portfolio/v1/state.pb.go` | Regenerated |
| `internal/shared/contracts/strategy_execution_portfolio_converters.go` | Modified — new field mappings |
