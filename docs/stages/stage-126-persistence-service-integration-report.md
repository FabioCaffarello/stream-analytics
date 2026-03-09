# Stage 126 — Persistence Service Integration Report

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Close end-to-end workspace persistence by connecting the client (hardened in S122)
to a backend service, eliminating operational noise from storage.js, and ensuring
real save/load with idempotency and schema validation.

## Architecture

### Before S126

- Workspace persistence was **100% client-side localStorage** (via `storage.js` JS bridge)
- No backend workspace endpoint existed
- Workspace state could not survive browser clear/device change
- `http_get_sync` returned 0 on non-200 silently; no workspace-specific backend contract

### After S126

- **Backend**: `GET /api/v1/workspace` + `PUT /api/v1/workspace` endpoints on Go server
- **File-backed store**: `FileWorkspaceStore` with atomic write (temp + rename), in-memory cache
- **Client integration**: JS bridge syncs localStorage ↔ backend on init and flush
- **Dual-layer persistence**: localStorage (immediate) + backend (durable)

## Backend Contract

```
GET  /api/v1/workspace  → 200 JSON { schema_version, layout_v6, fingerprint, settings, saved_at_ms }
                        → 204 No Content (first run, no saved state)
                        → 501 (store not configured)

PUT  /api/v1/workspace  → 200 { saved: true, fingerprint: "..." }
                        → 400 (invalid body / missing fields / bad prefix)
                        → 409 (schema_version > 12 — future version rejection)
                        → 501 (store not configured)
```

### Validation Rules (PUT)

- `schema_version` > 0, ≤ 12 (current)
- `layout_v6` required, must start with `"V6"`
- Future schema (> 12) → 409 Conflict (prevents data loss from newer client to older server)
- Settings can be nil/empty (layout-only save is valid)

### Idempotency

Server computes FNV-1a 64-bit fingerprint from `layout_v6 + sorted(settings)`.
If PUT fingerprint matches stored state, returns 200 immediately (skip write).

### File Store

- Path: `{state_dir}/workspace.json` (configurable via `workspace.state_dir` in server config)
- Atomic write: temp file + `os.Rename` (no partial writes on crash)
- Thread-safe: `sync.RWMutex` for concurrent reads
- Eager load on construction (no I/O cost on first HTTP request)

## Client Integration

### JS Bridge (storage.js)

Three new functions added:

1. **`http_put_sync`** — Synchronous HTTP PUT (used for workspace save to backend)
2. **`web_workspace_load`** — `GET /api/v1/workspace` → writes settings to localStorage → returns 1/0
3. **`web_workspace_sync`** — Collects all `mr.settings.*` from localStorage → `PUT /api/v1/workspace`

### Odin Integration

- `Settings_Port` extended with `backend_load` and `backend_sync` procs
- `settings_init`: calls `backend_load()` before reading local keys (backend → localStorage → in-memory)
- `settings_flush`: calls `backend_sync()` after writing to localStorage (in-memory → localStorage → backend)

### Data Flow

**Startup:**
```
Backend GET /api/v1/workspace
  ↓ (200: write to localStorage / 204: skip)
localStorage → settings_init → App_State
```

**Save:**
```
App_State → settings_flush → localStorage
  ↓
localStorage → web_workspace_sync → PUT /api/v1/workspace → disk
```

## Files Changed

### New Files (3)

| File | Purpose |
|------|---------|
| `internal/interfaces/http/workspace_handlers.go` | GET/PUT handlers, WorkspaceStore interface, WorkspaceState, fingerprint |
| `internal/interfaces/http/workspace_store.go` | FileWorkspaceStore (file-backed, atomic write, RWMutex) |
| `internal/interfaces/http/workspace_handlers_test.go` | 19 tests (GET/PUT/round-trip/edge cases) |

### Modified Files (7)

| File | Change |
|------|--------|
| `internal/interfaces/http/server.go` | `workspaceStore` field, `WithWorkspaceStore` option, route registration |
| `internal/shared/config/schema.go` | `WorkspaceConfig` struct + field on `AppConfig` |
| `deploy/configs/server.jsonc` | `workspace.state_dir` config |
| `cmd/server/bootstrap.go` | Wire `FileWorkspaceStore` into server |
| `client/web/modules/storage.js` | `http_put_sync`, `web_workspace_load`, `web_workspace_sync` |
| `client/web/runtime.js` | Wire new JS procs into WASM import object |
| `client/src/platform/web/settings_web.odin` | Foreign procs + backend_load/backend_sync |
| `client/src/core/ports/settings.odin` | `backend_load`/`backend_sync` on Settings_Port |
| `client/src/core/services/settings_store.odin` | backend_load in init, backend_sync in flush |

## Test Results

### Go (Backend)

- **19 workspace handler tests** — all pass
- Full HTTP package test suite — all pass (zero regressions)
- Server binary compiles clean

### Odin (Client)

- **321 app tests** — all pass (including 22 S122 artifact tests)
- **186 services tests** — all pass
- **WASM build** — clean (1.7 MB)
- **check-core** — all 10 packages OK

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Zero storage.js 400 in validated flow | **DONE** — workspace endpoint returns 204 (not 400) for first-run |
| Save/load reais funcionando | **DONE** — PUT/GET round-trip verified by 19 backend tests |
| Workspace artifacts persistidos e2e | **DONE** — localStorage → backend sync on every flush |
| Contrato V6 + checksum mantido | **DONE** — V6 with |CK: suffix preserved through backend |
| Restore determinístico não regride | **DONE** — 321 app tests pass, all S122 artifact tests green |
| Idempotência validada | **DONE** — fingerprint-based skip on identical save |
| Schema compatibility | **DONE** — future version rejected with 409 |
| First-run sem estado salvo | **DONE** — 204 No Content, client falls back to defaults |

## Zero Regressions

- All existing Go tests pass
- All existing Odin tests pass
- WASM build size unchanged
- No wire-breaking changes
