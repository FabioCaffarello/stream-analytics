# Stage 128 — Workspace Backend Architecture Refactor

**Date:** 2026-03-09
**Status:** COMPLETE

## Objective

Refactor the workspace backend capability from a monolithic HTTP handler into a
properly layered bounded context following DDD, Clean Architecture, Hexagonal
Architecture, and SOLID principles.

## Problem Statement

The initial workspace implementation (S126) concentrated all responsibilities
in `internal/interfaces/http/`:

- **Domain model** (`WorkspaceState` struct) lived in the HTTP package
- **Validation rules** (schema version range, layout prefix) were embedded in the handler
- **Fingerprint computation** (business logic) was a package-level function in the handler file
- **Idempotency check** (application concern) was inline in the HTTP handler
- **Repository interface** (`WorkspaceStore`) was defined alongside the handler
- **Persistence implementation** (`FileWorkspaceStore`) was in the same package

This violated separation of concerns and made the workspace logic untestable
without HTTP infrastructure.

## Solution

### New Bounded Context: `internal/core/workspace/`

```
internal/core/workspace/
├── go.mod
├── domain/
│   ├── workspace.go          ← Workspace aggregate + invariants
│   └── workspace_test.go     ← 14 domain tests
├── app/
│   ├── service.go            ← WorkspaceService use cases
│   └── service_test.go       ← 10 application tests
└── ports/
    └── repository.go         ← WorkspaceRepository interface
```

### Domain Layer (`domain/`)

- **Workspace aggregate** with encapsulated fields and accessor methods
- **`NewFromPayload()`** — factory with full validation (schema version range,
  layout prefix, future version rejection)
- **`Reconstitute()`** — bypass validation for loading from persistence
- **Fingerprint computation** — deterministic FNV-1a, owned by the aggregate
- **`HasSameFingerprint()`** — idempotency comparison
- **`StampSaveTime()`** — server-side timestamp if client didn't provide one
- **`MaxSchemaVersion`** — single source of truth (was `currentWorkspaceSchemaVersion`)

### Application Layer (`app/`)

- **`WorkspaceService`** — orchestrates `LoadWorkspace()` and `SaveWorkspace()`
- **Idempotency** handled in the service (not the handler)
- **Timestamping** handled in the service
- Uses `result.Result[T]` and `problem.Problem` per project convention

### Ports Layer (`ports/`)

- **`WorkspaceRepository`** interface — `Load() (*domain.Workspace, error)` / `Save(*domain.Workspace) error`
- Clean dependency inversion: domain defines the contract, adapters implement it

### HTTP Adapter (updated `internal/interfaces/http/`)

- **`workspace_handlers.go`** — thin handlers: decode JSON → call service → encode response
  - Zero business logic, zero validation, zero fingerprint computation
  - `writeWorkspaceProblem()` maps `problem.Code` → HTTP status
- **`workspace_store.go`** — `FileWorkspaceStore` now implements `ports.WorkspaceRepository`
  - Internal DTO (`workspaceStateDTO`) for JSON serialization
  - `dtoFromDomain()` / `dtoToDomain()` converters

### Wiring Changes

- **`server.go`**: `workspaceStore WorkspaceStore` → `workspaceSvc *workspaceapp.WorkspaceService`
- **`WithWorkspaceStore()`** → **`WithWorkspaceRepository()`** — takes a port, creates the service
- **`bootstrap.go`**: creates `FileWorkspaceStore` as repository, passes to `WithWorkspaceRepository()`
- **`go.work`**: added `./internal/core/workspace`
- **`internal/interfaces/go.mod`**: added workspace dependency + replace directive

## What Was Removed

- `WorkspaceStore` interface (replaced by `ports.WorkspaceRepository`)
- `WorkspaceState` struct from HTTP package (replaced by `domain.Workspace`)
- `workspaceFingerprint()` function (moved to aggregate method)
- `currentWorkspaceSchemaVersion` constant (replaced by `domain.MaxSchemaVersion`)
- All validation logic from handlers (moved to domain)
- All idempotency logic from handlers (moved to service)

## Test Results

| Package | Tests | Status |
|---------|-------|--------|
| `workspace/domain` | 14 | PASS |
| `workspace/app` | 10 | PASS |
| `interfaces/http` (workspace) | 18 | PASS |
| **Total** | **42** | **ALL PASS** |

## Architectural Compliance

| Principle | Before | After |
|-----------|--------|-------|
| SRP | Handler did validation, fingerprinting, idempotency, persistence | Each layer has one responsibility |
| OCP | Adding validation required modifying handler | Domain aggregate is extensible |
| DIP | Handler depended on concrete FileWorkspaceStore | Service depends on abstract WorkspaceRepository port |
| Hexagonal | No ports, no adapters | Explicit ports/repository, FileWorkspaceStore as adapter |
| DDD Aggregate | Anemic DTO struct | Rich aggregate with invariants and behavior |

## Migration Impact

- Zero wire-breaking changes (same JSON shape on GET/PUT)
- Zero client changes required
- Zero config changes required
- Backward-compatible persistence format (same workspace.json)
