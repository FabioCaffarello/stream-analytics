# Stage 74 — Client Portfolio Data Layer

**Date:** 2026-03-08
**Status:** COMPLETE
**Scope:** Client data layer for consuming backend portfolio read model APIs (S73)

## Summary

Created the complete client-side data layer for portfolio read models without any UI coupling. Three stores map 1:1 to the backend S73 query endpoints, following the established service/port/poll pattern used by instrument overview (S58) and session health (S59).

## Architecture

### Three Independent Stores

| Store | Backend Endpoint | Scope | Target Required |
|-------|-----------------|-------|-----------------|
| `Portfolio_State_Result` | `GET /api/v1/portfolio/state/latest` | Venue-scoped position & balance | account_id + venue + symbol |
| `Portfolio_Account_Snapshot_Result` | `GET /api/v1/portfolio/account-snapshot/latest` | Account-level aggregation | account_id |
| `Portfolio_Summary_Result` | `GET /api/v1/portfolio/summary/latest` | Global portfolio view | None |

### Design Decisions

1. **JSON-first, protobuf-ready** — Parsers consume JSON (default backend format). Protobuf path can be added by switching `Accept` header and using a binary parser without changing the store types.

2. **Fixed-capacity arrays** — All arrays use compile-time bounded capacity (positions: 32, balances: 16, exposures: 16, venues: 8, accounts: 8) to avoid allocations.

3. **Independent fetch lifecycle** — Each store has its own `Overview_Fetch_Status` (Idle/Success/Error) and `fetch_frame`, allowing partial success (e.g., summary loads but state fails due to missing target).

4. **Polling-based with streaming preparation** — Uses `PORTFOLIO_POLL_INTERVAL` (~10s at 60fps). Target setters reset status to `.Idle` to trigger immediate fetch. Future streaming upgrade only needs to replace the poll path.

5. **UI isolation** — `portfolio_data.odin` contains zero UI imports. Store types live in `services/` package. App state integration via `Portfolio_Data_State` is a pure data struct.

## Files Created

| File | Purpose |
|------|---------|
| `client/src/core/services/portfolio_store.odin` | 3 store types + JSON schemas + parse procs |
| `client/src/core/services/portfolio_store_test.odin` | 18 tests covering all 3 parsers |
| `client/src/core/app/portfolio_data.odin` | Fetch/poll/target-setter logic |

## Files Modified

| File | Change |
|------|--------|
| `client/src/core/ports/marketdata.odin` | Added 3 fetch procs: `fetch_portfolio_state`, `fetch_account_snapshot`, `fetch_portfolio_summary` |
| `client/src/core/app/app.odin` | Added `Portfolio_Data_State` struct, `portfolio` field on `App_State`, `poll_portfolio` in both update loops |

## Port Interface

```odin
// Added to Marketdata_Port:
fetch_portfolio_state:   proc(buf: [^]u8, cap: i32, account_id, venue, symbol: string) -> i32
fetch_account_snapshot:  proc(buf: [^]u8, cap: i32, account_id: string) -> i32
fetch_portfolio_summary: proc(buf: [^]u8, cap: i32) -> i32
```

## App State Integration

```odin
Portfolio_Data_State :: struct {
    // Store 1: venue-scoped
    state, state_status, state_frame
    // Store 2: account-level
    snapshot, snapshot_status, snapshot_frame
    // Store 3: global
    summary, summary_status, summary_frame
    // Target selectors
    account_id, venue, symbol
}
```

### Target Setters

- `portfolio_set_target(pf, account_id, venue, symbol)` — configures all 3 stores
- `portfolio_set_account(pf, account_id)` — configures account + summary stores
- `portfolio_clear(pf)` — resets all state (e.g., on disconnect)

## Test Coverage

18 new tests across 3 parser functions:

- **Portfolio State:** full parse, empty arrays, nil data, nil result, invalid JSON
- **Account Snapshot:** full parse (multi-venue), nil data, nil result, invalid JSON
- **Portfolio Summary:** full parse (multi-account), empty accounts, nil data, nil result, invalid JSON

## Constraints Met

- No business logic in client (pure parse + store)
- Thread-safe (value structs, no shared mutable state)
- Prepared for streaming (poll interval + Idle trigger pattern)
- UI fully isolated (zero UI imports in data layer)
- Zero wire protocol changes

## Next Steps

- S75: Platform-specific HTTP implementations for the 3 new port procs (native + web)
- Future: Portfolio page (UI) consuming these stores
- Future: Protobuf parse path for binary payloads
