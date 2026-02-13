# ADR-0016 — Protobuf Contract Layer

**Status:** Proposed
**Date:** 2026-02-12
**Deciders:** Chief Architect
**Relates to:** PRD-0001 section E.6, RFC-0007 (W6)

---

## Amendment (2026-02-12)

W6-1 foundation was implemented with zero runtime wire changes:
- `proto/` scaffolding and v1 contracts were added.
- Buf lint/breaking/generate toolchain and CI gates were enabled.
- Go code generation is committed under `internal/shared/proto/gen/`.
- Runtime publish/consume migration remains deferred to W6-2/W6-3.

## Current Scope Boundary

Phase 1 is complete and accepted as documentation+tooling scope:
- Proto contracts and manifest are present under `proto/`.
- Buf lint/breaking/generate workflow is active via Make targets and CI wiring.
- Generated Go artifacts are committed in `internal/shared/proto/gen/`.

Runtime wire migration is explicitly deferred:
- No forced migration of live publish/consume paths to protobuf in this phase.
- Envelope `ContentType` support exists for compatibility, but runtime rollout remains W6-2/W6-3.
- Existing JSON runtime behavior remains the default production path.

## Context

All serialization currently uses JSON via `codec.Marshal/Unmarshal`. There is no formal schema definition, no breaking change detection, and no type-safe wire contract between producers and consumers. PRD-0001 section A.5 flagged "Envelope.Payload — `[]byte` JSON blob sem schema registry" as a critical gap.

We need:
- Schema definitions that are language-agnostic and versionable
- Breaking change detection in CI
- Wire-format efficiency (smaller payloads on bus)
- Type-safe generated Go code for payloads

## Decision

### 1. Tooling: Buf (not raw protoc)

We adopt [Buf](https://buf.build) for:
- **Linting:** enforces style and correctness rules on `.proto` files
- **Breaking change detection:** `buf breaking --against .git#branch=main` in CI
- **Code generation:** `buf generate` with `protocolbuffers/go` plugin
- **Dependency management:** `buf.lock` for proto imports

**Why not raw protoc:** protoc requires manual plugin management, has no built-in lint/breaking checks, and needs shell scripts for CI integration. Buf provides all of this as a single binary with declarative config.

### 2. Directory Layout

```
proto/
├── buf.yaml              # Workspace + lint/breaking config
├── buf.gen.yaml           # Code generation config
├── buf.lock               # Dependency lock file
├── registry.json          # Schema manifest (type → proto file → message)
├── envelope/
│   └── v1/
│       └── envelope.proto
└── marketdata/
    └── v1/
        ├── trade.proto
        ├── bookdelta.proto
        ├── markprice.proto
        └── liquidation.proto
```

Generated Go code output: `internal/shared/proto/gen/`

### 3. Schema Versioning Convention

- Package path includes version: `marketdata.v1`
- Message names include version suffix: `TradeTickV1`
- New incompatible versions get new package: `marketdata.v2`
- Old versions remain available (append-only registry)

### 4. Compatibility Rules

| Rule | Enforcement |
|------|-------------|
| Never reuse field numbers | `buf breaking` (WIRE rule set) |
| Never change field types | `buf breaking` |
| Never remove required fields | `reserved` directive + `buf breaking` |
| Never add to existing `oneof` | `buf breaking` |
| New optional fields only | Code review |

### 5. Envelope ContentType

`Envelope` gains a `ContentType` field:
- `"application/json"` (default, backward compatible)
- `"application/protobuf"` (opt-in)

Codec auto-detects format based on ContentType and dispatches to JSON or proto decoder.

### 6. Migration Strategy

| Phase | Scope | Runtime Impact |
|-------|-------|----------------|
| Phase 1 (W6) | Define schemas, generate code, add `buf lint` + `buf breaking` to CI | None — schemas are documentation only |
| Phase 2 (W7) | Add ContentType to Envelope. Codec supports both JSON and proto. | Backward compatible — default is JSON |
| Phase 3 (post-W7) | Opt-in proto publishing via config flag `encoding: protobuf` | Dual-codec consumers |
| Phase 4 (post-W9) | Deprecate JSON for bus traffic. HTTP API remains JSON. | Proto-only on bus |

### 7. Schema Registry Lite

`proto/registry.json` — static manifest mapping event types to proto definitions:

```json
{
  "schemas": [
    {"type": "marketdata.trade", "version": 1, "proto": "marketdata/v1/trade.proto", "message": "marketdata.v1.TradeTickV1", "status": "stable"},
    {"type": "marketdata.bookdelta", "version": 1, "proto": "marketdata/v1/bookdelta.proto", "message": "marketdata.v1.BookDeltaV1", "status": "stable"}
  ]
}
```

No external registry service needed. This file is versioned in git alongside schemas.

## Rationale

Protobuf provides:
- **Wire compatibility guarantees** via field numbering (not names)
- **CI-enforceable breaking change detection** via `buf breaking`
- **Smaller payloads** (2-10x smaller than JSON for numeric-heavy market data)
- **Generated type-safe code** eliminating manual serialization bugs

Buf specifically is preferred because it eliminates protoc toolchain management and provides integrated lint+breaking+generate in one tool.

## Contract Authority Model

- Proto schemas are canonical contract authority for market data events.
- Domain structs are projections of proto fields, not independent schema definitions.
- Proto-to-domain and domain-to-proto converters are mandatory boundaries for all contract payloads.
- Breaking schema changes are blocked in CI via Buf breaking checks.
- Deterministic replay and cross-exchange normalization depend on schema immutability.

## Alternatives Considered

1. **Stay JSON-only:** Rejected — no schema enforcement, no breaking change detection, no wire compatibility guarantee.
2. **Avro:** Rejected — smaller ecosystem in Go, requires schema registry service.
3. **FlatBuffers:** Rejected — more complex API, smaller community.
4. **CBOR:** Already planned as codec option (codec.go is CBOR-ready). CBOR is complementary to proto, not a replacement for schema definition.
5. **Raw protoc (no Buf):** Rejected — see Decision section 1.

## Consequences

### Positive
- Breaking changes caught in CI before merge
- Wire format efficiency reduces bus bandwidth
- Generated code eliminates manual marshal/unmarshal bugs
- Schema documentation is machine-readable (`.proto` files)

### Negative
- Buf is a build-time dependency (single binary, ~50MB)
- Generated code adds to repo size (~10KB per message)
- Dual-codec support adds complexity to codec package
- Team must learn proto3 syntax and Buf workflow

### Invariants (testable)
- `PROTO-1`: `buf lint` passes with 0 errors (CI check)
- `PROTO-2`: `buf breaking --against .git#branch=main` passes on PR (CI check)
- `PROTO-3`: Proto-encoded message decodes identically to JSON-encoded version (roundtrip unit test)
- `PROTO-4`: `registry.json` lists all schemas with correct proto file paths (CI validation script)
- `PROTO-5`: Removing a field from `.proto` causes `buf breaking` to fail (negative test in CI docs)

## Rollout Plan

1. Install Buf in dev environment and CI (RFC-0007/W6)
2. Create `proto/` directory with buf.yaml, buf.gen.yaml (RFC-0007/W6)
3. Define envelope.v1 and marketdata.v1 schemas (RFC-0007/W6)
4. Add `make proto-gen`, `make proto-lint`, `make proto-breaking` targets (RFC-0007/W6)
5. Generate Go code, add to `internal/shared/proto/gen/` (RFC-0007/W6)
6. Add ContentType field to Envelope (RFC-0008/W7)
7. Wire proto codec alongside JSON in codec package (RFC-0008/W7)
