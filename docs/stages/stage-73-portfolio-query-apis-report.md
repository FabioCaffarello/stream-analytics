# Stage 73 — Portfolio Query APIs

## Objective

Expose portfolio read models (state, account snapshot, summary) via HTTP query
endpoints with protobuf-first wire support and JSON fallback.

## Endpoints

| Route | Parameters | Reader Method |
|-------|-----------|---------------|
| `GET /api/v1/portfolio/state/latest` | `account_id` (req), `venue` (req), `symbol` (req) | `GetLatestPortfolioState` |
| `GET /api/v1/portfolio/states` | `account_id` (req), `venue` (opt), `symbol` (opt), `limit` (opt, default 100) | `GetPortfolioStates` |
| `GET /api/v1/portfolio/account-snapshot/latest` | `account_id` (req) | `GetLatestAccountSnapshot` |
| `GET /api/v1/portfolio/summary/latest` | (none) | `GetLatestPortfolioSummary` |

## Architecture

### New Types

- **`PortfolioReaders`** struct — groups `PortfolioStateReader`, `AccountSnapshotReader`,
  `PortfolioSummaryReader` interfaces for injection.
- **`WithPortfolioReaders(readers)`** — functional option on `Server`.

### Content Negotiation

- `Accept: application/x-protobuf` → protobuf envelope response
- Default → JSON response
- Delegates to existing `writeResponse()` / `writeProtoEnvelope()` helpers

### Latency Instrumentation

- `time.Since(start)` measured per request
- Logged via `slog.Debug` (success) and `slog.Warn` (error) with `elapsed_ms`
- No new Prometheus metrics (consistent with other handlers)

### Error Handling

| Condition | HTTP Status |
|-----------|-------------|
| Reader nil / not configured | 503 Service Unavailable |
| Missing required params | 400 Bad Request |
| Reader returns `*problem.Problem` | 500 Internal Server Error |
| Routes not registered (nil readers) | 404 Not Found |

## Files

| File | Change |
|------|--------|
| `internal/interfaces/http/portfolio_handlers.go` | NEW — 4 handlers + `PortfolioReaders` + `WithPortfolioReaders` |
| `internal/interfaces/http/portfolio_handlers_test.go` | NEW — 19 tests |
| `internal/interfaces/http/server.go` | Modified — added `portfolioReaders` field + 4 route registrations |

## Test Coverage (19 tests)

### State Latest (5)
- Success, Missing params (4 variations), Reader unavailable, Reader error, Proto negotiation

### States (4)
- Success with filters, Default limit, Missing account_id, Reader error

### Account Snapshot Latest (5)
- Success, Missing account_id, Reader unavailable, Reader error, Proto negotiation

### Summary Latest (4)
- Success, Reader unavailable, Reader error, Proto negotiation

### Integration (1)
- Nil readers → routes not registered → 404

## Constraints Verified

- No domain logic in handlers — pure delegation to reader interfaces
- Idempotent GET endpoints — safe for retry
- Protobuf-first with JSON fallback
- Zero new dependencies
- Zero regressions (full interface test suite passes)
