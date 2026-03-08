# Stage 79 — Contract Hardening, Determinism & UI Validation Harness

**Date:** 2026-03-08
**Status:** COMPLETE

## Objective

Harden contracts between backend, client and UI. Eliminate drift, reinforce boundedness, payload budgets and snapshot-before-delta invariants. Establish UI validation harness via Playwright MCP at port 8090.

## Architecture Risks Addressed

### R1. Event Catalog Governance Gap (HIGH → RESOLVED)

`PortfolioEventCatalog` only registered `StateEventType` — `AccountSnapshotEventType` and `SummaryEventType` were declared as constants but missing from the catalog. Fixed by registering all three types.

### R2. Payload Budget Enforcement (MEDIUM → RESOLVED)

HTTP list endpoints accepted unbounded `limit` values. Added `MaxStatesLimit=500`, `MaxSnapshotsLimit=200`, `MaxSummariesLimit=200`, `MaxEquityCurveLimit=1000` with `clampLimit()` enforcing budgets.

### R3. Client Silent Truncation (MEDIUM → RESOLVED)

Parser functions silently truncated arrays exceeding bounded capacity. Added `Truncation_Flags` bitmask (`Positions|Balances|Exposures|Venues|Accounts`) returned from all three parsers. Flags surfaced in `Portfolio_Data_State.truncation_flags`.

### R4. SQL Symbol Filter Safety (MEDIUM → RESOLVED)

Symbol filter in `portfolio_state_reader.go` used string concatenation for JSONB parameter value. Replaced with `symbolJSONBFilter()` using `json.Marshal` for proper JSON escaping.

### R5. Staleness Threshold Drift (LOW → RESOLVED)

`StalenessThresholdMs` hardcoded independently in backend and client. Added `staleness_threshold_ms` field to `TradingReadinessV1` response. Client reads server-provided value with fallback to local constant when absent (backward compat).

### R6. Client Ordering Non-Determinism (LOW → RESOLVED)

Client rendered venues in JSON array order (backend-dependent). Added deterministic insertion sort by venue name in `portfolio_account_snapshot_parse_json`.

### R7. Proto Schema Governance (LOW → RESOLVED)

Portfolio schemas marked `draft` without graduation criteria. Added `stability_criteria` and `stability_target` fields to all 6 portfolio entries in `registry.json`.

## Backend Changes

### Domain: Event Catalog (`internal/core/portfolio/domain/event_catalog.go`)

- Registered `AccountSnapshotEventType` (v1) and `SummaryEventType` (v1) in `PortfolioEventCatalog`
- Catalog now has 3 entries matching all declared constants

### Domain: Trading Readiness (`internal/core/execution/domain/readiness.go`)

- Added `StalenessThresholdMs int64` field to `TradingReadinessV1`
- JSON tag: `staleness_threshold_ms`

### HTTP: Portfolio Handlers (`internal/interfaces/http/portfolio_handlers.go`)

- Added `MaxStatesLimit`, `MaxSnapshotsLimit`, `MaxSummariesLimit`, `MaxEquityCurveLimit` constants
- Added `clampLimit()` helper
- Applied clamping to all 4 list endpoints (states, account-snapshots, summaries, equity-curve)

### HTTP: Trading Readiness Handler (`internal/interfaces/http/trading_readiness_handler.go`)

- Set `StalenessThresholdMs: StalenessThresholdMs` in response construction

### Storage: State Reader (`internal/adapters/storage/timescale/portfolio_state_reader.go`)

- Added `symbolJSONBFilter()` — properly escapes symbol via `json.Marshal`
- Replaced both string-concat usages in `GetPortfolioStates` and `GetLatestPortfolioState`

### Proto Registry (`proto/registry.json`)

- Added `stability_criteria` and `stability_target` to all 6 portfolio schema entries

## Client Changes

### Services: Portfolio Store (`client/src/core/services/portfolio_store.odin`)

- Added `Truncation_Flags` bit_set type with 5 flags: `Positions`, `Balances`, `Exposures`, `Venues`, `Accounts`
- Changed all 3 parser signatures: `-> bool` → `-> (ok: bool, truncated: Truncation_Flags)`
- Added `sort_venues()` (insertion sort) + `str_less()` for deterministic venue ordering
- Snapshot parser now sorts venues alphabetically after parse

### Services: Trading Readiness (`client/src/core/services/trading_readiness.odin`)

- Added `staleness_threshold_ms: i64` field to `Trading_Readiness_Result` and `Trading_Readiness_JSON`
- Parser wires `staleness_threshold_ms` from response

### App: Portfolio Data (`client/src/core/app/portfolio_data.odin`)

- Updated all 3 fetch procs to handle `(ok, truncated)` return
- Accumulates truncation flags in `pf.truncation_flags`

### App: State (`client/src/core/app/app.odin`)

- Added `truncation_flags: services.Truncation_Flags` to `Portfolio_Data_State`

## Test Summary

### Backend Tests (New)

| Test | File | Status |
|------|------|--------|
| `TestPortfolioEventCatalogCompleteness` | `domain/event_catalog_test.go` | PASS |
| `TestEventCatalogMatchesReadModelCatalog` | `domain/event_catalog_test.go` | PASS |
| `TestEventCatalogByTypeLookup` | `domain/event_catalog_test.go` | PASS |
| `TestPayloadBudget_ClampLimit` (6 subtests) | `http/portfolio_handlers_test.go` | PASS |
| `TestPayloadBudget_StatesEndpoint` | `http/portfolio_handlers_test.go` | PASS |
| `TestHandleGetTradingReadiness_StalenessThresholdIncluded` | `http/trading_readiness_handler_test.go` | PASS |

### Client Tests (Updated + New)

| Test | Status | Notes |
|------|--------|-------|
| All existing portfolio_store tests | PASS | Updated for `(bool, Truncation_Flags)` return |
| All existing trading_readiness tests | PASS | Unchanged |
| `test_portfolio_snapshot_venue_truncation` | NEW | Verifies venue truncation flag set when >8 venues |
| `test_portfolio_snapshot_venue_sort_determinism` | NEW | Verifies alphabetical sort (kraken,coinbase,binance → binance,coinbase,kraken) |
| `test_trading_readiness_staleness_threshold_from_server` | NEW | Verifies 300000ms parsed |
| `test_trading_readiness_staleness_threshold_absent` | NEW | Verifies 0 fallback |

### Regression

- `go test ./internal/core/portfolio/domain/...` → 13/13 PASS
- `go test ./internal/interfaces/http/...` → all PASS
- `go build ./internal/adapters/storage/timescale/...` → clean
- `make -C client check-core` → all 10 packages OK

## UI Validation Harness (Playwright MCP)

### Checklist

```
PRE-REQUISITOS:
[ ] make up PROCESSOR_REPLICAS=2
[ ] Client at http://127.0.0.1:8090/

SMOKE:
[ ] Dashboard → "Connected" badge
[ ] Portfolio → 4 summary cards (Equity, Realized, Unrealized, Leverage)
[ ] Tab: Positions → venue groups + positions
[ ] Tab: Exposure → venue + symbol exposure tables
[ ] Tab: Fill Stats → 9 metric cards
[ ] Tab: Readiness → control plane + safety flags + venue table

DATA INTEGRITY:
[ ] Position count in header == sum across venue groups
[ ] Equity sum consistent between summary and accounts
[ ] Stale/Fresh indicators consistent with projected_at_ms

RECONNECT:
[ ] docker compose restart server → reconnect within 20s
[ ] Portfolio data repopulates within 30s
[ ] 3x sequential restart → no degradation

TRUNCATION (when applicable):
[ ] With >8 venues, UI should display first 8 (sorted alphabetically)
[ ] No crash or memory growth on truncated payloads
```

## File Summary

| File | Action | Delta |
|------|--------|-------|
| `internal/core/portfolio/domain/event_catalog.go` | EDIT | +2 lines |
| `internal/core/portfolio/domain/event_catalog_test.go` | EDIT | +42 lines |
| `internal/core/execution/domain/readiness.go` | EDIT | +1 field |
| `internal/interfaces/http/portfolio_handlers.go` | EDIT | +20 lines |
| `internal/interfaces/http/portfolio_handlers_test.go` | EDIT | +42 lines |
| `internal/interfaces/http/trading_readiness_handler.go` | EDIT | +1 line |
| `internal/interfaces/http/trading_readiness_handler_test.go` | EDIT | +25 lines |
| `internal/adapters/storage/timescale/portfolio_state_reader.go` | EDIT | +11 lines |
| `proto/registry.json` | EDIT | +12 lines |
| `client/src/core/services/portfolio_store.odin` | EDIT | +45 lines |
| `client/src/core/services/portfolio_store_test.odin` | EDIT | +55 lines |
| `client/src/core/services/trading_readiness.odin` | EDIT | +3 lines |
| `client/src/core/services/trading_readiness_test.odin` | EDIT | +22 lines |
| `client/src/core/app/portfolio_data.odin` | EDIT | +6 lines |
| `client/src/core/app/app.odin` | EDIT | +2 lines |

## Validation

```
go test ./internal/core/portfolio/domain/... -v  → 13/13 PASS
go test ./internal/interfaces/http/... -v         → all PASS
go build ./internal/adapters/storage/timescale/...→ clean
make -C client check-core                         → all packages OK
```
