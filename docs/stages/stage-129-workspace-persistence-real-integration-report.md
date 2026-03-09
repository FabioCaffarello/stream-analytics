# Stage 129 — Workspace Persistence Service Real Integration

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Close the real save/load workspace flow between client and backend, eliminating graceful degradation dependency and removing operational noise from missing persistence.

## Architecture

### Domain-Driven Workspace Persistence

The workspace persistence is implemented as a proper bounded context following MR domain patterns:

```
internal/core/workspace/
├── domain/workspace.go       — Root aggregate (invariants, fingerprint, validation)
├── domain/workspace_test.go  — 15 domain tests
├── ports/repository.go       — WorkspaceRepository interface
├── app/service.go            — Application service (idempotency, orchestration)
├── app/service_test.go       — 11 service tests
└── go.mod                    — Module with shared dependency
```

### HTTP Layer

```
internal/interfaces/http/
├── workspace_handlers.go       — GET/PUT /api/v1/workspace
├── workspace_handlers_test.go  — 20 handler/integration tests
└── workspace_store.go          — FileWorkspaceStore (atomic file I/O)
```

### Client Integration

```
client/
├── web/modules/storage.js              — web_workspace_load() + web_workspace_sync()
├── src/platform/web/settings_web.odin  — backend_load + backend_sync port bindings
├── src/core/services/settings_store.odin — settings_init → backend_load → preload
├── src/core/app/layout_persist.odin    — persist_layout_v6 → flush → backend_sync
└── src/core/app/workspace_schema.odin  — WORKSPACE_SCHEMA_VERSION = 12
```

## HTTP Contract

### GET /api/v1/workspace

| Status | Meaning |
|--------|---------|
| 200    | Workspace found, JSON body with schema_version, layout_v6, fingerprint, settings, saved_at_ms |
| 204    | First run — no saved state |
| 501    | Workspace service not configured |

### PUT /api/v1/workspace

| Status | Meaning |
|--------|---------|
| 200    | Save successful → `{ "saved": true, "fingerprint": "..." }` |
| 400    | Invalid payload (missing/invalid schema_version or layout_v6) |
| 409    | Schema version from newer client (server cannot downgrade) |
| 501    | Workspace service not configured |

## Flow

### Startup (Client → Backend)

1. WASM boot → `settings_init(store, port)`
2. `port.backend_load()` → `GET /api/v1/workspace` (synchronous XHR)
3. If 200: populate localStorage with backend state (layout_v6 + all settings)
4. If 204: first run, proceed with defaults
5. Preload 54 known settings keys from localStorage into in-memory store
6. `restore_workspace()` → parse V6, apply layout

### Persist (Client → Backend)

1. User modifies workspace → dirty flag set
2. `persist_layout_v6()` → builds V6 string + CRC suffix + fingerprint stamp
3. `settings_flush()` → writes all entries to localStorage
4. `port.backend_sync()` → `PUT /api/v1/workspace` (synchronous XHR)
5. Backend validates, computes fingerprint, checks idempotency, persists to disk

### First-Run

1. GET returns 204 → no localStorage population
2. `restore_workspace()` returns `No_Data`
3. `layout_from_panels()` creates default 1-cell Candle layout
4. Cell 0 gets `binance/BTCUSDT:SPOT` default binding (PRD-0009)
5. First flush syncs defaults to backend

## Changes

### Backend (Go)

**Refactored from S126 inline to domain-driven architecture:**

- `workspace_handlers.go`: Delegates to `WorkspaceService` instead of inline validation/fingerprinting
- `workspace_store.go`: Uses `domain.Workspace` with DTO conversion layer
- `server.go`: `workspaceSvc *WorkspaceService` field, `WithWorkspaceRepository()` option
- `bootstrap.go`: `NewFileWorkspaceStore(wsStateDir)` → `WithWorkspaceRepository()`

**New domain model (`internal/core/workspace/`):**

- `domain.Workspace` — root aggregate with invariant enforcement
- `domain.NewFromPayload()` — validates schema version, layout prefix, computes fingerprint
- `domain.Reconstitute()` — loads from storage without re-validation
- `ports.WorkspaceRepository` — Load/Save interface
- `app.WorkspaceService` — orchestrates with idempotency check

### Client (Odin)

**Bug fix: Missing indicator parameter keys in known_keys preload.**

Added 9 indicator parameter settings to `known_keys` in `settings_init()`:
- `ma_period_0`, `ma_period_1`, `ma_period_2`
- `bb_period`, `bb_sigma`
- `rsi_period`
- `macd_fast`, `macd_slow`, `macd_signal`

Without this fix, indicator parameters would not survive page refresh or round-trip through backend persistence.

## Invariants

- Schema version validated: [1, 12] (MaxSchemaVersion)
- Layout prefix validated: must start with "V6"
- Fingerprint: deterministic FNV-1a 64-bit hash of layout + sorted settings
- Idempotency: skip write if fingerprint matches existing state
- Save timestamp: server stamps if client didn't provide
- Atomic writes: temp file + rename for crash safety
- Defensive copy: `Settings()` returns a copy, not reference

## Test Coverage

| Layer | Tests | Status |
|-------|-------|--------|
| Domain (workspace_test.go) | 15 | PASS |
| Service (service_test.go) | 11 | PASS |
| Handlers (workspace_handlers_test.go) | 20 | PASS |
| **Total Go tests** | **46** | **ALL PASS** |

### Key test scenarios:

- First-run: GET 204 → PUT → GET 200
- Round-trip: PUT payload → GET returns identical state
- Idempotency: identical PUTs produce same fingerprint, skip write
- Overwrite: different content produces different fingerprint
- Validation: schema_version ≤ 0 → 400, empty/invalid layout → 400
- Future schema: version > 12 → 409
- Null settings: accepted with empty map
- Large payload: 500 cells accepted
- Nil service: returns 501

## Verification

```
go build ./internal/core/workspace/...     ✅
go build ./internal/interfaces/...         ✅
go build ./cmd/server/...                  ✅
go test ./internal/core/workspace/... -count=1  ✅ (26 tests)
go test ./internal/interfaces/http/... -run Workspace -count=1  ✅ (20 tests)
odin check src/platform/web -target:js_wasm32   ✅
make check-core                            ✅
```

## Files Modified

| File | Change |
|------|--------|
| `internal/core/workspace/domain/workspace.go` | NEW — Root aggregate |
| `internal/core/workspace/domain/workspace_test.go` | NEW — 15 domain tests |
| `internal/core/workspace/ports/repository.go` | NEW — Repository interface |
| `internal/core/workspace/app/service.go` | NEW — Application service |
| `internal/core/workspace/app/service_test.go` | NEW — 11 service tests |
| `internal/core/workspace/go.mod` | NEW — Module definition |
| `internal/interfaces/http/workspace_handlers.go` | REFACTORED — Domain-driven handlers |
| `internal/interfaces/http/workspace_handlers_test.go` | REFACTORED — Uses domain types |
| `internal/interfaces/http/workspace_store.go` | REFACTORED — DTO + domain.Workspace |
| `internal/interfaces/http/server.go` | MODIFIED — workspaceSvc field + WithWorkspaceRepository |
| `cmd/server/bootstrap.go` | MODIFIED — WithWorkspaceRepository wiring |
| `client/src/core/services/settings_store.odin` | FIXED — Added 9 missing indicator param keys |

Zero regressions. Zero wire-breaking changes.
