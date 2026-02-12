# RFC-0007 — W6: Protobuf Contract Layer

**Status:** Proposed
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W6 of PRD-0001
**Relates to:** ADR-0016 (Protobuf Contract Layer), PRD-0001 section E.6

---

## 1. Goal

Introduce formal schema definitions for all wire-format payloads, breaking change detection in CI, and generated Go code for type-safe serialization. After W6:
- `.proto` schemas exist for envelope, trade, and bookdelta
- `buf lint` enforces style rules on every commit
- `buf breaking` detects incompatible schema changes on every PR
- Generated Go code exists in `internal/shared/proto/gen/`
- Schema registry manifest (`proto/registry.json`) documents all schemas

## 2. Scope

- Install and configure Buf toolchain
- Create `proto/` directory with schemas for envelope.v1, marketdata.v1
- Define `buf.yaml` (lint + breaking rules) and `buf.gen.yaml` (code generation)
- Generate Go code via `buf generate`
- Create schema registry manifest
- Add Makefile targets: `proto-gen`, `proto-lint`, `proto-breaking`
- **Phase 1 only:** schemas as documentation + CI checks. No runtime changes.

## 3. Non-Goals

- Runtime proto serialization (Phase 2, part of W7)
- Dual-codec support in Envelope (Phase 2)
- ContentType field in Envelope (Phase 2)
- Protobuf for HTTP API responses (stays JSON)

## 4. Affected Modules

| File | Action | Change |
|------|--------|--------|
| `proto/buf.yaml` | CREATE | Buf workspace config with lint + breaking rules |
| `proto/buf.gen.yaml` | CREATE | Code generation config (protocolbuffers/go plugin) |
| `proto/envelope/v1/envelope.proto` | CREATE | Envelope wire format schema |
| `proto/marketdata/v1/trade.proto` | CREATE | TradeTickV1 schema |
| `proto/marketdata/v1/bookdelta.proto` | CREATE | BookDeltaV1 schema |
| `proto/marketdata/v1/markprice.proto` | CREATE | MarkPriceTickV1 schema |
| `proto/marketdata/v1/liquidation.proto` | CREATE | LiquidationTickV1 schema |
| `proto/registry.json` | CREATE | Schema manifest (type → proto → message) |
| `internal/shared/proto/gen/` | GENERATED | Go code from buf generate |
| `Makefile` | ALTER | Add proto-gen, proto-lint, proto-breaking targets |
| `.github/workflows/ci.yml` (or equivalent) | ALTER | Add buf lint + buf breaking to CI pipeline |

## 5. Schema Definitions

### envelope/v1/envelope.proto

```protobuf
syntax = "proto3";
package envelope.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/envelope/v1";

message Envelope {
  string type = 1;
  int32 version = 2;
  string venue = 3;
  string instrument = 4;
  int64 ts_exchange = 5;
  int64 ts_ingest = 6;
  int64 seq = 7;
  string idempotency_key = 8;
  map<string, string> meta = 9;
  bytes payload = 10;
  string content_type = 11;
}
```

### marketdata/v1/trade.proto

```protobuf
syntax = "proto3";
package marketdata.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1";

message TradeTickV1 {
  double price = 1;
  double size = 2;
  string side = 3;
  string trade_id = 4;
  int64 timestamp_ms = 5;
}
```

### marketdata/v1/bookdelta.proto

```protobuf
syntax = "proto3";
package marketdata.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1";

message PriceLevel {
  double price = 1;
  double size = 2;
}

message BookDeltaV1 {
  repeated PriceLevel bids = 1;
  repeated PriceLevel asks = 2;
  int64 first_update_id = 3;
  int64 final_update_id = 4;
  int64 prev_final_update_id = 5;
  int64 timestamp_ms = 6;
}
```

### marketdata/v1/markprice.proto

```protobuf
syntax = "proto3";
package marketdata.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1";

message MarkPriceTickV1 {
  double mark_price = 1;
  double index_price = 2;
  double funding_rate = 3;
  int64 next_funding_time_ms = 4;
  int64 timestamp_ms = 5;
}
```

### marketdata/v1/liquidation.proto

```protobuf
syntax = "proto3";
package marketdata.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1";

message LiquidationTickV1 {
  string instrument = 1;
  string side = 2;
  double price = 3;
  double size = 4;
  int64 timestamp_ms = 5;
}
```

## 6. Buf Configuration

### buf.yaml

```yaml
version: v2
modules:
  - path: .
lint:
  use:
    - STANDARD
  except:
    - PACKAGE_VERSION_SUFFIX
breaking:
  use:
    - WIRE_JSON
```

### buf.gen.yaml

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: ../internal/shared/proto/gen
    opt: paths=source_relative
```

## 7. Schema Registry Manifest

```json
{
  "version": "1.0",
  "schemas": [
    {
      "type": "envelope",
      "version": 1,
      "proto": "envelope/v1/envelope.proto",
      "message": "envelope.v1.Envelope",
      "status": "stable"
    },
    {
      "type": "marketdata.trade",
      "version": 1,
      "proto": "marketdata/v1/trade.proto",
      "message": "marketdata.v1.TradeTickV1",
      "status": "stable"
    },
    {
      "type": "marketdata.bookdelta",
      "version": 1,
      "proto": "marketdata/v1/bookdelta.proto",
      "message": "marketdata.v1.BookDeltaV1",
      "status": "stable"
    },
    {
      "type": "marketdata.markprice",
      "version": 1,
      "proto": "marketdata/v1/markprice.proto",
      "message": "marketdata.v1.MarkPriceTickV1",
      "status": "stable"
    },
    {
      "type": "marketdata.liquidation",
      "version": 1,
      "proto": "marketdata/v1/liquidation.proto",
      "message": "marketdata.v1.LiquidationTickV1",
      "status": "stable"
    }
  ]
}
```

## 8. Makefile Targets

```makefile
.PHONY: proto-gen proto-lint proto-breaking

proto-gen:
	cd proto && buf generate

proto-lint:
	cd proto && buf lint

proto-breaking:
	cd proto && buf breaking --against '.git#branch=main'
```

## 9. Migration Strategy

**Phase 1 (this RFC):** Schema definitions + CI only. Zero runtime impact.
- Schemas document the existing JSON wire format
- `buf lint` prevents style violations
- `buf breaking` prevents incompatible changes
- Generated Go code is committed but not imported by any production code

**Phase 2 (W7/RFC-0008):** Add ContentType to Envelope. Codec dispatches based on content type.

**Phase 3 (post-W7):** Opt-in proto publishing via config flag `encoding: protobuf`.

**Phase 4 (post-W9):** Deprecate JSON for bus traffic. HTTP API remains JSON.

## 10. Test Plan

| Type | Test | Pass Criteria |
|------|------|---------------|
| CI | `buf lint proto/` | 0 errors |
| CI | `buf breaking proto/ --against .git#branch=main` | 0 breaking changes on PR |
| Unit | Proto roundtrip: marshal TradeTickV1, unmarshal, compare fields | Identical |
| Unit | Proto roundtrip: marshal BookDeltaV1, unmarshal, compare fields | Identical |
| Unit | Generated code compiles (`go build ./internal/shared/proto/gen/...`) | No errors |
| Negative | Remove field from .proto, run `buf breaking` | Fails with error |
| Validation | `registry.json` entries match existing .proto files | Script validates all paths exist |

## 11. Acceptance Criteria

- [ ] `proto/` directory exists with all schema files
- [ ] `buf lint` passes with 0 errors
- [ ] `buf breaking --against .git#branch=main` passes on clean branch
- [ ] `buf breaking` FAILS when a field is intentionally removed (negative test documented)
- [ ] `make proto-gen` generates Go code in `internal/shared/proto/gen/` without errors
- [ ] Generated code compiles: `go build ./internal/shared/proto/gen/...`
- [ ] `proto/registry.json` lists all 5 schemas with correct proto file paths
- [ ] Proto-encoded `TradeTickV1` decodes identically to JSON-encoded version (roundtrip test)
- [ ] No runtime changes — existing behavior unchanged
