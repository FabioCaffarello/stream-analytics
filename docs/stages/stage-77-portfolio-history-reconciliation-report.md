# Stage 77 — Portfolio History & Reconciliation

**Date:** 2026-03-08
**Status:** COMPLETE
**Branch:** codex/s9-legacy-removal-cutover

## Objective

Add temporal history endpoints and a read-only reconciliation layer for the portfolio subsystem, enabling auditable equity curves and automated divergence detection.

## Deliverables

### 1. Domain Types (`internal/core/portfolio/domain/reconciliation.go`)

- **EquityCurvePointV1**: temporal equity point with drawdown tracking (peak-relative)
- **ReconciliationReportV1**: immutable diagnostic report (never modifies state)
- **ReconciliationFinding**: individual inconsistency with kind, severity, context
- **6 finding kinds**: `seq_gap`, `equity_drift`, `stale_projection`, `orphan_state`, `duplicate_state`, `pnl_mismatch`
- **3 severity levels**: `info`, `warning`, `error`
- **ReconciliationQuery**: scoped check request (account_id, time range)

### 2. Reconciliation Engine (`internal/core/portfolio/app/reconciliation.go`)

Pure domain service — zero IO, zero database calls. Analyzes data passed to it.

**Equity curve builders:**
- `BuildEquityCurve(snapshots)` → account-scoped curve with drawdown
- `BuildEquityCurveFromSummaries(summaries)` → global curve with drawdown
- Both sort input chronologically and track peak-relative drawdown

**Reconciliation checks:**
- `CheckReconciliation(states, snapshots, query, nowMs)` → `ReconciliationReportV1`
- **Seq gap detection**: groups states by venue, sorts by execution_seq, flags gaps > 1
- **Equity drift detection**: flags >10% equity change between consecutive snapshots (>50% = error)
- **Stale projection detection**: flags states >5min older than latest for same account
- **PnL consistency**: cross-checks sum of venue-state realized PnL vs snapshot aggregate (>$1 drift = error)
- Report marked `healthy=false` if any finding has `severity=error`

### 3. HTTP Endpoints (4 new routes)

| Route | Params | Description |
|-------|--------|-------------|
| `GET /api/v1/portfolio/account-snapshots` | account_id (req), from_ms, to_ms, limit | Historical account snapshots |
| `GET /api/v1/portfolio/summaries` | from_ms, to_ms, limit | Historical portfolio summaries |
| `GET /api/v1/portfolio/equity-curve` | account_id (opt), from_ms, to_ms, limit | Equity curve (account or global) |
| `GET /api/v1/portfolio/reconciliation` | account_id (req), from_ms, to_ms | Reconciliation report |

- All support JSON (default) and protobuf content negotiation
- Latency instrumented via slog.Debug/Warn
- Standard error handling: 503 unavailable, 400 bad params, 500 reader errors

### 4. Test Coverage

| Layer | New Tests | Total |
|-------|-----------|-------|
| `portfolio/app` (reconciliation) | 12 | 44 |
| `interfaces/http` (handlers) | 14 | 38+ |
| **Total new** | **26** | |

## Architecture

```
                    ┌──────────────────────┐
                    │   HTTP Handlers       │
                    │  (4 new endpoints)    │
                    └──────┬───────────────┘
                           │
              ┌────────────┼────────────────┐
              │            │                │
    ┌─────────▼──┐  ┌──────▼─────┐  ┌──────▼──────┐
    │ State      │  │ Snapshot   │  │ Summary     │
    │ Reader     │  │ Reader     │  │ Reader      │
    └─────────┬──┘  └──────┬─────┘  └──────┬──────┘
              │            │                │
              └────────────┼────────────────┘
                           │
              ┌────────────▼────────────────┐
              │  Reconciliation Engine       │
              │  (pure domain service)       │
              │  • Equity curve builder      │
              │  • Seq gap checker           │
              │  • Equity drift checker      │
              │  • Stale projection checker  │
              │  • PnL consistency checker   │
              └─────────────────────────────┘
```

## Key Invariants

1. **Reconciliation never alters state** — purely diagnostic, read-only
2. **Deterministic reports** — same inputs produce same findings (report ID is hash-based)
3. **No new storage** — uses existing TimescaleDB tables and readers
4. **No new migrations** — leverages existing `portfolio_account_snapshot` and `portfolio_summary` time-range indexes
5. **No wire changes** — existing protobuf contracts untouched
6. **Zero regressions** — all pre-existing tests pass

## Files Changed

| File | Change |
|------|--------|
| `internal/core/portfolio/domain/reconciliation.go` | NEW — domain types |
| `internal/core/portfolio/app/reconciliation.go` | NEW — reconciliation engine |
| `internal/core/portfolio/app/reconciliation_test.go` | NEW — 12 tests |
| `internal/interfaces/http/portfolio_handlers.go` | MODIFIED — 4 new handlers |
| `internal/interfaces/http/portfolio_handlers_test.go` | MODIFIED — 14 new tests |
| `internal/interfaces/http/server.go` | MODIFIED — 4 new route registrations |
