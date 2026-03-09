# Stage 137 — Workspace Persistence Backend Completion

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Conclude workspace backend persistence with full DDD/hexagonal architecture adherence, closing all remaining gaps from S126/S128/S129.

## Changes

### 1. FileWorkspaceStore relocated to bounded context

**Before:** `internal/interfaces/http/workspace_store.go` (HTTP adapter layer — wrong layer)
**After:** `internal/core/workspace/infra/file_store.go` (infrastructure adapter within workspace BC)

This consolidates the entire workspace bounded context under `internal/core/workspace/`:
```
internal/core/workspace/
├── domain/workspace.go          — root aggregate + invariants
├── domain/workspace_test.go     — 15 domain tests
├── app/service.go               — application service (3 use cases)
├── app/service_test.go          — 15 service tests
├── ports/repository.go          — driven port (Load/Save/Delete)
└── infra/file_store.go          — file-backed adapter (crash-safe)
    └── file_store_test.go       — 13 file I/O tests
```

### 2. Crash-safe persistence hardened

- **fsync before rename:** `f.Sync()` ensures data hits disk before atomic rename. Previous version only did temp+rename (insufficient for power-loss durability).
- **Corrupt file detection:** If `workspace.json` exists but contains invalid JSON, `Load()` now returns `(nil, error)` instead of silently treating it as first-run `(nil, nil)`.
- **Error recovery:** A successful `Save()` clears any prior load error, allowing recovery from corruption.

### 3. DELETE endpoint added

`DELETE /api/v1/workspace` → 204 No Content

Resets workspace to first-run state (deletes file + clears cache). Useful for troubleshooting and workspace reset UX.

Added through all layers:
- **Port:** `Delete() error` on `WorkspaceRepository`
- **Service:** `ResetWorkspace() result.Result[bool]`
- **Handler:** `handleDeleteWorkspace` → 204/500/501

### 4. PUT response enriched

`saved_at_ms` now included in PUT response body, so the client knows the server-stamped timestamp:
```json
{"saved": true, "fingerprint": "abc123", "saved_at_ms": 1709971200000}
```

### 5. Request body size limit

PUT handler now uses `http.MaxBytesReader` with a 1 MiB limit. Oversized payloads receive 413 Request Entity Too Large.

### 6. Bootstrap updated

`cmd/server/bootstrap.go` now imports `workspaceinfra` from the workspace BC instead of creating the store via `httpserver.NewFileWorkspaceStore()`.

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `internal/core/workspace/infra/file_store.go` | **NEW** | FileWorkspaceStore (relocated + hardened) |
| `internal/core/workspace/infra/file_store_test.go` | **NEW** | 13 file I/O tests |
| `internal/core/workspace/ports/repository.go` | Modified | Added `Delete() error` |
| `internal/core/workspace/app/service.go` | Modified | Added `ResetWorkspace()`, `SavedAtMs` in `SaveResult` |
| `internal/core/workspace/app/service_test.go` | Modified | 15 tests (+5 reset tests) |
| `internal/interfaces/http/workspace_handlers.go` | Modified | DELETE handler, body limit, saved_at_ms |
| `internal/interfaces/http/workspace_handlers_test.go` | Modified | 29 tests (+5 DELETE + 1 saved_at_ms) |
| `internal/interfaces/http/workspace_store.go` | **DELETED** | Moved to workspace/infra |
| `internal/interfaces/http/server.go` | Modified | Register DELETE route |
| `cmd/server/bootstrap.go` | Modified | Import workspaceinfra |

## Test Summary

| Package | Tests | Status |
|---------|-------|--------|
| `workspace/domain` | 15 | PASS |
| `workspace/app` | 15 | PASS |
| `workspace/infra` | 13 | PASS |
| `interfaces/http` (workspace) | 29 | PASS |
| **Total** | **72** | **ALL PASS** |

## API Surface

| Method | Endpoint | Status Codes | Description |
|--------|----------|--------------|-------------|
| GET | `/api/v1/workspace` | 200, 204, 500, 501 | Load workspace |
| PUT | `/api/v1/workspace` | 200, 400, 409, 413, 500, 501 | Save workspace |
| DELETE | `/api/v1/workspace` | 204, 500, 501 | Reset workspace |

## Acceptance Criteria

- [x] Real persistence functioning (file-backed, crash-safe)
- [x] Zero semantic errors (corrupt detection, proper error propagation)
- [x] Backend adherent to DDD + Clean Architecture + Hexagonal
- [x] First-run without saved workspace → 204
- [x] Idempotent overwrite via fingerprint
- [x] Version/schema mismatch → 409
- [x] Clear semantic responses (400/409/413/500/501)
- [x] Integration tests across all layers
- [x] FileWorkspaceStore consolidated within workspace bounded context
- [x] 72 tests, zero regressions, zero wire-breaking changes
