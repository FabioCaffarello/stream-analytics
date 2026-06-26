# Workspace Schema Contract

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `internal/core/workspace/domain/workspace.go`, `internal/core/workspace/app/service.go`, `internal/core/workspace/ports/repository.go`, `docs/client/client-architecture.md`

---

## Purpose

Define the Workspace persistence schema contract. The `core/workspace` bounded context
manages client layout persistence. This document governs the schema version invariants,
fingerprint algorithm, and migration policy — both the server-side Go implementation and
the client-side Odin consumer must conform.

---

## Domain Invariants

| Invariant | Rule | Code anchor |
|-----------|------|-------------|
| `SchemaVersion` range | Must be in `[1, MaxSchemaVersion]`. Current max: **12** | `internal/core/workspace/domain/workspace.go:21` |
| `LayoutV6` prefix | Must start with the string `"V6"` | `internal/core/workspace/domain/workspace.go:43` |
| `Fingerprint` format | 16-character lowercase hex string | `internal/core/workspace/domain/workspace.go:126` |
| Forward incompatibility | Server at schema 12 rejects a client presenting `SchemaVersion > 12` with `Conflict` error | `internal/core/workspace/app/service.go` |
| CRC checksum | V12 introduced CRC checksum over layout bytes | `internal/core/workspace/domain/workspace.go` |

---

## Schema Version Migration Policy

| Scenario | Behaviour |
|----------|-----------|
| Client `SchemaVersion <= MaxSchemaVersion` | Server accepts and stores verbatim |
| Client `SchemaVersion > MaxSchemaVersion` | Server returns `Conflict` — client must upgrade or fall back |
| Server schema version bump | Increment `MaxSchemaVersion` in `workspace.go:21`; add migration logic if layout encoding changes |
| Backward compatibility | Server accepts any version ≤ current max without understanding semantic differences |

**When to bump `MaxSchemaVersion`:**
- Workspace field additions that change persistence format
- Layout encoding changes (`LayoutV6` schema)
- Fingerprint algorithm changes
- CRC computation changes

**Do NOT bump for:**
- Client-side-only rendering changes
- Widget settings that are stored in the opaque `settings` map

---

## Split-Tree Layout (V6)

The workspace layout is encoded as a binary split-tree in the `LayoutV6` string:

| Property | Value |
|----------|-------|
| Max tree nodes | 31 |
| Max panes | 16 |
| Node types | Split (horizontal/vertical) + Leaf (pane) |
| Pane roles | `Primary_Chart`, `Auxiliary`, `Context` |

Widget kinds available per pane (13 total): `Candle`, `Stats`, `Counter`, `Heatmap`, `VPVR`,
`Trades`, `Orderbook`, `DOM`, `Empty`, `Analytics`, `Session_VPVR`, `TPO`, `Footprint`.

---

## Fingerprint Algorithm

The fingerprint is a deterministic FNV-1a 64-bit hash formatted as a 16-character lowercase
hex string. It is computed over:

1. `LayoutV6` bytes (as UTF-8)
2. Followed by all `(key, value)` pairs from the `settings` map, sorted alphabetically by key

```go
// Pseudocode (see internal/core/workspace/domain/workspace.go:126)
h := fnv.New64a()
h.Write([]byte(w.LayoutV6))
for _, key := range sortedKeys(w.Settings) {
    h.Write([]byte(key))
    h.Write([]byte(w.Settings[key]))
}
return fmt.Sprintf("%016x", h.Sum64())
```

The fingerprint is used for:
- Change detection (client skips re-render if fingerprint matches)
- Artifact tracking (audit trail when workspace is saved)

---

## HTTP API

The workspace service is consumed by the server's HTTP interface via the `WorkspaceRepository`
port (`internal/core/workspace/ports/repository.go`).

| Endpoint | Method | Description |
|----------|--------|-------------|
| `GET /api/v1/workspace` | GET | Retrieve current workspace for the authenticated session |
| `PUT /api/v1/workspace` | PUT | Upsert workspace; validates schema version and fingerprint |

The HTTP handler delegates to `internal/core/workspace/app/service.go`.
Persistence is implemented by `internal/core/workspace/infra/file_store.go` in local deployment.

---

## Code Anchors

| File | Location | Purpose |
|------|----------|---------|
| `internal/core/workspace/domain/workspace.go:21` | `MaxSchemaVersion = 12` | Current schema version cap |
| `internal/core/workspace/domain/workspace.go:43` | `NewFromPayload` | Validation entry point (schema range, V6 prefix check) |
| `internal/core/workspace/domain/workspace.go:126` | `computeFingerprint` | FNV-1a fingerprint computation |
| `internal/core/workspace/app/service.go` | Application service | Upsert + retrieve orchestration |
| `internal/core/workspace/ports/repository.go` | Port interface | Persistence contract |
| `internal/core/workspace/infra/file_store.go` | Adapter | Local file-based persistence |

---

## Test Anchors

| File | Coverage |
|------|----------|
| `internal/core/workspace/domain/workspace_test.go` | Invariant validation, fingerprint determinism |
| `internal/core/workspace/app/service_test.go` | Upsert + retrieve flows, schema version enforcement |
