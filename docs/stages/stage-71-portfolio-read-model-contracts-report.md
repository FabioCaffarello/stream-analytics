# Stage 71 — Portfolio Read Model Contracts (Protobuf-First)

**Date:** 2026-03-08
**Status:** COMPLETE

## Objective

Formalize protobuf wire contracts for portfolio read model queries
(`AccountSnapshotV1`, `PortfolioSummaryV1`) and eliminate JSON ad-hoc
serialization at system boundaries. Ensure compatibility with existing
`portfolio.state` without modifying it.

## Deliverables

### 1. Protobuf Definitions (3 new files)

| File | Messages |
|------|----------|
| `proto/portfolio/v1/account_snapshot.proto` | `AccountSnapshotV1`, `VenuePositionV1` |
| `proto/portfolio/v1/portfolio_summary.proto` | `PortfolioSummaryV1`, `AccountSummaryV1` |
| `proto/portfolio/v1/query.proto` | `PortfolioQueryKind` enum, 3 request/response pairs |

All messages:
- Reuse `PositionV1`, `BalanceV1`, `FillSummaryV1` from `state.proto` (import, no duplication)
- Include `reserved 100 to 199` for future expansion
- Full doc comments on every field and enum value

### 2. Query Messages

| Request | Response |
|---------|----------|
| `PortfolioStateQueryRequest` (account_id, venue, symbol, limit) | `PortfolioStateQueryResponse` (repeated states) |
| `AccountSnapshotQueryRequest` (account_id, from_ms, to_ms, limit) | `AccountSnapshotQueryResponse` (repeated snapshots) |
| `PortfolioSummaryQueryRequest` (from_ms, to_ms, limit) | `PortfolioSummaryQueryResponse` (repeated summaries) |

### 3. Domain ↔ Proto Converters

**New file:** `internal/shared/contracts/portfolio_readmodel_converters.go`

- `DomainToProtoAccountSnapshotV1` / `ProtoToDomainAccountSnapshotV1`
- `DomainToProtoPortfolioSummaryV1` / `ProtoToDomainPortfolioSummaryV1`
- 3 query request converter pairs
- Shared helpers: `domainToProtoVenuePositionV1`, `domainToProtoFillSummaryV1` (avoids duplication with state converter)

### 4. Domain Query Types

**New file:** `internal/core/portfolio/domain/query.go`

Wire-compatible query structs (`int32` limits matching protobuf) mirroring
ports query types. Keeps `contracts` package free of `ports` imports.

### 5. Event Catalog Constants

Added to `event_catalog.go`:
- `AccountSnapshotEventType = "portfolio.account_snapshot"` (version 1)
- `SummaryEventType = "portfolio.summary"` (version 1)
- `PortfolioReadModelCatalog` now references constants instead of string literals

### 6. Codec Registry

`RegisterPortfolioPayloadV1` now registers 3 payload types (was 1):
- `portfolio.state` (existing)
- `portfolio.account_snapshot` (new)
- `portfolio.summary` (new)

Each with dual JSON + Protobuf codecs.

### 7. Tests (10 new)

| Test | Validates |
|------|-----------|
| `TestAccountSnapshotV1_DomainProtoRoundTrip` | Lossless domain → proto → domain |
| `TestAccountSnapshotV1_NilProtoReturnsZero` | Nil safety |
| `TestAccountSnapshotV1_CodecRoundTrip` | JSON + Proto codec encode/decode |
| `TestPortfolioSummaryV1_DomainProtoRoundTrip` | Lossless domain → proto → domain |
| `TestPortfolioSummaryV1_NilProtoReturnsZero` | Nil safety |
| `TestPortfolioSummaryV1_CodecRoundTrip` | JSON + Proto codec encode/decode |
| `TestPortfolioStateQueryRequest_RoundTrip` | Query converter fidelity |
| `TestAccountSnapshotQueryRequest_RoundTrip` | Query converter fidelity |
| `TestPortfolioSummaryQueryRequest_RoundTrip` | Query converter fidelity |
| `TestQueryConverters_NilReturnsZero` | Nil safety for all query converters |

## Invariants

1. **No `portfolio.state` changes** — `state.go` and `state.proto` untouched
2. **No business logic in protos** — messages are pure data contracts
3. **Protobuf-first at boundaries** — all read model types now have stable wire schemas
4. **Backward-compatible** — existing `RegisterPortfolioPayloadV1` signature unchanged
5. **Import boundary respected** — `contracts` imports `domain` only, not `ports`

## Files Changed

| File | Change |
|------|--------|
| `proto/portfolio/v1/account_snapshot.proto` | **NEW** |
| `proto/portfolio/v1/portfolio_summary.proto` | **NEW** |
| `proto/portfolio/v1/query.proto` | **NEW** |
| `internal/shared/proto/gen/portfolio/v1/account_snapshot.pb.go` | **NEW** (generated) |
| `internal/shared/proto/gen/portfolio/v1/portfolio_summary.pb.go` | **NEW** (generated) |
| `internal/shared/proto/gen/portfolio/v1/query.pb.go` | **NEW** (generated) |
| `internal/core/portfolio/domain/query.go` | **NEW** |
| `internal/shared/contracts/portfolio_readmodel_converters.go` | **NEW** |
| `internal/shared/contracts/portfolio_readmodel_converters_test.go` | **NEW** |
| `internal/core/portfolio/domain/event_catalog.go` | Added constants |
| `internal/shared/contracts/strategy_execution_portfolio_registry.go` | Extended registration |

## Test Results

- **10 new tests** — all pass
- **All existing contracts tests** — pass (zero regressions)
- **All portfolio domain/app tests** — pass (zero regressions)
