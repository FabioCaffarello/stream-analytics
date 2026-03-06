# Stage 19 -- Backend Client-Readiness Consolidation

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** IN PROGRESS (Slices 1-4 delivered)

## Executive Summary

Stage 19 consolidates backend-owned composition for first client vertical slices after S18 protocol hardening. The objective is to reduce client bootstrap fan-out, normalize readiness/freshness/resync semantics, and expose widget-oriented read models without expanding trading surfaces or leaking internals.

Delivered increments:
- Slice 1: `GET /api/v1/instrument/overview` (instrument-level composed readiness).
- Slice 2: `GET /api/v1/session/dashboard` (session-level composed readiness dashboard).
- Slice 3: `GET /api/v1/artifacts/summary` (artifact matrix with configurable filters/timeframe).
- Slice 4: Cross-endpoint client-readiness contract suite (enum stability + semantic consistency).

No frozen contract was broken. Existing S15-S18 APIs remain unchanged.

## Current-State Audit

### Post-S18 baseline

The backend already provided:
- bootstrap/discovery surfaces: `GET /api/v1/session`, `GET /api/v1/catalog`, `GET /api/v1/timeline`, `GET /api/v1/freshness`;
- realtime contract hardening: WS-9/10/11 (`snapshot_seq`, `prev_seq`, snapshot-before-delta), resync diagnostics, optional hello gate;
- operational diagnostics: `GET /api/v1/delivery/diagnostics` (localhost-only).

### Gaps vs Stage 19 goals

1. **No widget-oriented composed instrument surface**
   - Client still needed multiple calls (`session` + `freshness` + `timeline`) to render one instrument header/dashboard.
2. **Status semantics fragmented**
   - readiness (`bool`), freshness (`active` + `flowing`), and resync/drop counters were not normalized into stable status enums.
3. **No single contract for per-instrument readiness/freshness/resync**
   - S20 client slices would otherwise duplicate composition logic client-side.

## Stage 19 Architecture

### Backend vs client composition (explicit decision)

**Backend-owned (S19):**
- status normalization (`ready/not_ready`, `flowing/stale/inactive`, `stable/recovering/degraded`);
- deterministic per-instrument diagnostics aggregation from terminal WS state;
- artifact timeline readiness summary from federated readers (without hot/cold leakage).

**Client-owned (S20+):**
- rendering decisions, color semantics, and UX behavior;
- local caching/poll cadence;
- cross-widget choreography.

Rationale: aligns with MMT learn audit guidance to keep heavy/semantic composition in backend and keep client mostly render/state orchestration.

### Stable status contract (born in S19)

`GET /api/v1/instrument/overview` freezes initial normalization enums:
- `readiness.status`: `ready | not_ready`
- `freshness.status`: `flowing | stale | inactive`
- `resync.status`: `stable | recovering | degraded`
- `status` (overall): `ready | degraded | inactive | not_ready`
- artifact timeline status: `available | empty | unavailable`

This avoids S20 churn from ad-hoc status derivations.

## Client-Readiness Surfaces

### Prioritized surfaces

1. **Instrument Overview** (`GET /api/v1/instrument/overview`) -- **Delivered (Slice 1)**
   - Widget-oriented composed payload for one `venue+instrument`.
2. **Session Dashboard** (`GET /api/v1/session/dashboard`) -- **Delivered (Slice 2)**
   - Session-level readiness/freshness/diagnostics envelope for terminal shell screens.
3. **Artifact Summary** (`GET /api/v1/artifacts/summary`) -- **Delivered (Slice 3)**
   - Dedicated artifact availability matrix with optional `venue`/`instrument`/`artifact`/`timeframe` filters.

### Why this order

Instrument Overview is the smallest correct increment with immediate client value and no domain or stream expansion. It reuses existing ports/adapters and preserves Clean Architecture boundaries.

## Code Changes

### Slice 1 delivered

- Added composed handler:
  - `internal/interfaces/http/instrument_overview_handlers.go`
- Registered route:
  - `GET /api/v1/instrument/overview` in `internal/interfaces/http/server.go`
- Added contract/behavior tests:
  - `internal/interfaces/http/instrument_overview_handlers_test.go`

### Slice 2 delivered

- Added composed handler:
  - `internal/interfaces/http/session_dashboard_handlers.go`
- Registered route:
  - `GET /api/v1/session/dashboard` in `internal/interfaces/http/server.go`
- Added contract/behavior tests:
  - `internal/interfaces/http/session_dashboard_handlers_test.go`

### Slice 3 delivered

- Added composed handler:
  - `internal/interfaces/http/artifact_summary_handlers.go`
- Registered route:
  - `GET /api/v1/artifacts/summary` in `internal/interfaces/http/server.go`
- Added contract/behavior tests:
  - `internal/interfaces/http/artifact_summary_handlers_test.go`

### Slice 4 delivered

- Added cross-endpoint contract tests:
  - `internal/interfaces/http/client_readiness_contract_test.go`
- Contract focus:
  - enum stability across `/api/v1/instrument/overview`, `/api/v1/session/dashboard`, `/api/v1/artifacts/summary`;
  - semantic consistency for `ready` path and `degraded` path;
  - drift guard against client-side recomposition mismatches.

### Endpoint contract (summary)

```
GET /api/v1/instrument/overview?venue=binance&instrument=BTCUSDT
```

Returns:
- normalized readiness/freshness/resync statuses;
- channel freshness map (client-safe);
- artifact timeline summary for `candle` and `stats` at default `1m` timeframe.

Additional endpoint:

```
GET /api/v1/session/dashboard
```

Returns:
- normalized session status (`ready|degraded|inactive|not_ready`);
- global freshness summary across configured instruments (`flowing|partial|stale|inactive`);
- global resync/drop posture (`stable|recovering|degraded`);
- compact artifact readiness matrix for `candle` and `stats` with coverage (`available|partial|empty|unavailable`).

Additional endpoint:

```
GET /api/v1/artifacts/summary?timeframe=1m&venue=...&instrument=...&artifact=candle|stats
```

Returns:
- matrix rows per `venue+instrument` with per-artifact status (`available|empty|unavailable`);
- artifact headers with coverage (`available|partial|empty|unavailable`);
- backend-owned compact summary for dynamic widget enablement.

## Validation

Executed:

```bash
go test ./internal/interfaces/http -count=1
```

Result: PASS.

Coverage of new slice includes:
- validation errors (missing params);
- composed happy path;
- degraded path (stale + drops);
- readers-unavailable path;
- JSON shape contract.
- session dashboard route gating, global composition, artifact matrix, and JSON shape contract.
- artifact summary filtering (venue/instrument/artifact/timeframe), invalid input handling, matrix/coverage composition, and JSON shape contract.
- cross-endpoint readiness contract consistency (ready/degraded scenarios + enum guards).

## Risks

1. **Status policy drift**
   - Risk: future teams redefine enum meanings informally.
   - Mitigation: enums documented in contracts (`event-bus`, `delivery-ws`) and stage report.

2. **Timeline default timeframe (`1m`) may not match all widgets**
   - Risk: some widgets need different default windows.
   - Mitigation: keep explicit in contract; evolve with additive field if needed.

3. **Observability global store coupling**
   - Risk: diagnostics depend on terminal WS state population quality.
   - Mitigation: reuse same source already used by `/api/v1/freshness` and `/api/v1/delivery/diagnostics`.

## Recommended Next Slice

1. Add explicit payload-budget guards for matrix responses (cap + deterministic truncation order) before broad market expansion.
2. Add coverage tests for large market-cardinality scenarios to validate bounded response behavior.
3. Keep S20 focused on consuming these surfaces (no new transport/domain semantics before first vertical slice is proven).
