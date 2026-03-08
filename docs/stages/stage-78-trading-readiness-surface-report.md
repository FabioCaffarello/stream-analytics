# Stage 78 — Trading Readiness Surface

**Date:** 2026-03-08
**Status:** COMPLETE

## Objective

Integrate portfolio with operational readiness of the execution system, showing per-account/venue trading status (enabled, degraded, disabled, halted), control plane state, safety flags, and portfolio staleness — without placing authorization logic in the portfolio bounded context.

## Architecture

The trading readiness surface is a **composed query** at the HTTP boundary. It reads from two bounded contexts without coupling them:

1. **Execution** (control plane) — state, disabled strategies/adapters, allowlist overrides
2. **Portfolio** (read models) — account summaries, venue snapshots, staleness assessment

Portfolio reflects execution state but never owns authorization decisions.

## Backend Changes

### Domain Types (`internal/core/execution/domain/readiness.go`)

- `TradingStatus` enum: `enabled`, `degraded`, `disabled`, `halted`
- `VenueReadiness`: per-venue status + equity + position count + staleness + restriction flag
- `AccountReadiness`: per-account with nested venue readiness
- `ControlPlaneReadiness`: state + simulation profile + disabled strategies/adapters + allowlist
- `TradingReadinessV1`: composed response (control plane + accounts + safety flags)
- `BaseTradingStatus()`: derives base status from `ControlState` (fail-closed)

### HTTP Handler (`internal/interfaces/http/trading_readiness_handler.go`)

**Endpoint:** `GET /api/v1/trading/readiness`

Composition logic:
1. Read `ControlSnapshot` from control plane (if available)
2. Read `PortfolioSummaryV1` for account list (if available)
3. For each account, read `AccountSnapshotV1` for venue-level detail
4. Derive per-venue `TradingStatus` from control plane state + allowlist + adapter health
5. Assess staleness via `projected_at_ms` vs `now` (threshold: 5 minutes)
6. Compute `SafetyFlags` array from control plane state

Safety flags: `clear`, `simulation`, `paused`, `drained`, `halted`, `strategies_disabled`, `adapters_disabled`, `venue_restricted`, `control_plane_unavailable`

Degrades gracefully — missing control plane or portfolio readers produce partial responses.

### Route Registration (`internal/interfaces/http/server.go`)

Registered when either `controlPlane` or `portfolioReaders` is configured:
```
GET /api/v1/trading/readiness → handleGetTradingReadiness
```

### Tests (`internal/interfaces/http/trading_readiness_handler_test.go`)

4 test cases:
- `TestHandleGetTradingReadiness_Active` — full composition, active state
- `TestHandleGetTradingReadiness_Halted` — halted state + safety flags
- `TestHandleGetTradingReadiness_NoControlPlane` — graceful degradation
- `TestHandleGetTradingReadiness_VenueRestricted` — allowlist enforcement

## Client Changes

### Service Layer (`client/src/core/services/trading_readiness.odin`)

Result types (fixed-capacity, zero-alloc):
- `Trading_Readiness_Result` — top-level parsed result
- `Control_Plane_Readiness` — control plane section
- `Account_Readiness` — per-account with venue array
- `Venue_Readiness` — per-venue status + metrics
- `Trading_Status` enum: `Unknown`, `Enabled`, `Degraded`, `Disabled`, `Halted`

Functions:
- `trading_readiness_parse_json()` — JSON parser
- `trading_status_label()` — display string for status
- `readiness_has_flag()` — flag presence check

### Service Tests (`client/src/core/services/trading_readiness_test.odin`)

7 test cases:
- Full parse with all fields
- Halted state parse
- Nil data / nil result / invalid JSON guards
- Status label mapping
- Flag presence check

### Port (`client/src/core/ports/marketdata.odin`)

Added `fetch_trading_readiness: proc(buf: [^]u8, cap: i32) -> i32` to `Marketdata_Port`.

### App State (`client/src/core/app/app.odin`)

- Extended `Portfolio_Tab` with `Readiness` variant
- Added `readiness`, `readiness_status`, `readiness_frame` to `Portfolio_Data_State`

### Data Layer (`client/src/core/app/portfolio_data.odin`)

- `fetch_trading_readiness()` — fetch + parse + store
- Poll integration: readiness polled alongside portfolio summary (~10s interval)

### UI (`client/src/core/app/build_portfolio.odin`)

**Readiness tab** (4th tab in portfolio page):

Three sections:
1. **CONTROL PLANE** — state badge (color-coded), simulation profile, disabled strategies/adapters, allowlist restrictions
2. **SAFETY FLAGS** — operational flags with color coding (green=clear, cyan=simulation, yellow=warning, red=halted)
3. **VENUE READINESS** — per-account/venue table with columns: Venue, Status, Equity, Positions, Freshness

Status badges: ENABLED (green), DEGRADED (yellow), DISABLED (yellow), HALTED (red)
Restricted venues marked with `[R]` indicator
Stale portfolios flagged with STALE badge

**Detail panel**: shows control plane state alongside existing portfolio metrics.

**Page lifecycle**: readiness status reset to `.Idle` on page enter for immediate fetch.

## Bounded Context Separation

| Concern | Owner | Client Reflects |
|---------|-------|-----------------|
| Control plane state | Execution | Yes — read-only |
| Authorization logic | Execution | No — never in portfolio |
| Portfolio staleness | Portfolio | Yes — via projected_at_ms |
| Trading status | HTTP boundary (composed) | Yes — via readiness endpoint |
| Safety flags | HTTP boundary (computed) | Yes — display only |

## File Summary

| File | Action | Lines |
|------|--------|-------|
| `internal/core/execution/domain/readiness.go` | NEW | 68 |
| `internal/interfaces/http/trading_readiness_handler.go` | NEW | 138 |
| `internal/interfaces/http/trading_readiness_handler_test.go` | NEW | 215 |
| `internal/interfaces/http/server.go` | EDIT | +4 |
| `client/src/core/services/trading_readiness.odin` | NEW | 176 |
| `client/src/core/services/trading_readiness_test.odin` | NEW | 100 |
| `client/src/core/ports/marketdata.odin` | EDIT | +2 |
| `client/src/core/app/app.odin` | EDIT | +5 |
| `client/src/core/app/portfolio_data.odin` | EDIT | +23 |
| `client/src/core/app/build_portfolio.odin` | EDIT | +220 |

## Test Summary

- **Backend:** 4 new tests (18 total in `http` package), all pass
- **Client:** 7 new service tests + check-core (all 10 packages compile)
- **Zero regressions**, zero wire protocol changes

## Validation

```
go test ./http/... -run TestHandleGetTradingReadiness -v  → 4/4 PASS
go build ./...  (execution domain)                        → clean
make check-core (client)                                  → all packages OK
```
