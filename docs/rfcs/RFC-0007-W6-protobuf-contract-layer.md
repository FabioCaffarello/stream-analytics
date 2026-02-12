# RFC-0007 — W6: Protobuf Contract Layer (W6-1 Foundation)

**Status:** Implemented (W6-1)
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W6 of PRD-0001
**Relates to:** ADR-0016 (Protobuf Contract Layer), PRD-0001 section E.6

---

## 1. Goal (W6-1)

Establish protobuf contract foundations without changing runtime wire behavior:
- protobuf scaffolding under `proto/`
- Buf lint/breaking/generate toolchain
- generated Go code under `internal/shared/proto/gen/`
- CI gates for compatibility and generated-code drift
- schema registry manifest (`proto/registry.json`)

## 2. Scope Implemented

- Created `proto/` with Buf v2 config and initial v1 contracts.
- Added comments for all fields to satisfy `COMMENTS` lint.
- Configured `breaking` with `WIRE_JSON` for strong compatibility checks.
- Generated Go code into `internal/shared/proto/gen`.
- Added Makefile targets: `proto-lint`, `proto-gen`, `proto-breaking`, `proto`.
- Added CI gates for buf lint/breaking and generated-file drift detection.
- Added lightweight schema registry (`proto/registry.json`) with `draft` status.

## 3. Explicit Non-Goals (Still Deferred)

- No runtime publish/consume migration to protobuf.
- No envelope runtime codec switching by `content_type`.
- No NATS JetStream/replay changes.

## 4. Contracts Added

- `envelope.v1.Envelope`
- `marketdata.v1.TradeTickV1`
- `marketdata.v1.PriceLevel`
- `marketdata.v1.BookDeltaV1`
- `marketdata.v1.MarkPriceTickV1`
- `marketdata.v1.LiquidationTickV1`

Notes:
- `BookDeltaV1` keeps existing domain fields (`first_update_id`, `final_update_id`, `prev_final_update_id`) to stay 1:1 with current payload shape.
- `instrument` format remains whatever the current domain emits (`BTCUSDT` or `BTC-USDT`), with no domain normalization changes in W6-1.

## 5. W6-1 Completed Evidence

### Commands executed

```bash
make proto-lint
make proto-breaking
make proto-gen
make proto-gen && git diff --exit-code -- internal/shared/proto/gen
cd internal/shared && go mod tidy
cd internal/shared && go test -race ./...
```

### Output summary

- `make proto-lint`: passed with zero violations.
- `make proto-breaking`: bootstrap-skipped locally because `main` has no `.proto` baseline yet; gate is active automatically once baseline exists in `main`.
- `make proto-gen`: succeeded and generated `.pb.go` files into `internal/shared/proto/gen/...`.
- `make proto-gen && git diff --exit-code -- internal/shared/proto/gen`: passed (no generated drift).
- `go mod tidy` (`internal/shared`): completed; protobuf runtime dependency is present.
- `go test -race ./...` (`internal/shared`): passed; generated packages compile.

### Checklist

- [x] `buf lint` ok
- [x] `buf breaking` gate wired to `main` (bootstrap skip until baseline exists)
- [x] `buf generate` ok
- [x] `internal/shared` compiles/tests with generated code
- [x] CI gate includes buf lint/breaking + generated drift check (`git diff --exit-code`)
- [x] Runtime publish/consume behavior unchanged

## 6. Files Delivered in W6-1

- `proto/buf.yaml`
- `proto/buf.gen.yaml`
- `proto/registry.json`
- `proto/envelope/v1/envelope.proto`
- `proto/marketdata/v1/trade.proto`
- `proto/marketdata/v1/bookdelta.proto`
- `proto/marketdata/v1/markprice.proto`
- `proto/marketdata/v1/liquidation.proto`
- `internal/shared/proto/gen/envelope/v1/envelope.pb.go`
- `internal/shared/proto/gen/marketdata/v1/trade.pb.go`
- `internal/shared/proto/gen/marketdata/v1/bookdelta.pb.go`
- `internal/shared/proto/gen/marketdata/v1/markprice.pb.go`
- `internal/shared/proto/gen/marketdata/v1/liquidation.pb.go`
- `Makefile` (proto targets)
- `.github/workflows/ci.yml` (buf + generated-check gate)

## 7. W6-2 Completed Evidence (Codec Infra + Registry)

### Commands executed

```bash
cd internal/shared && go test ./...
cd internal/shared && go test -race ./...
cd internal/core/delivery && go test ./...
```

### Output summary

- `go test ./...` (`internal/shared`): passed with new codec registry, JSON/proto codecs, contracts bootstrap, and envelope content-type helpers.
- `go test -race ./...` (`internal/shared`): passed for all packages, including `codec`, `contracts`, and `envelope`.
- `go test ./...` (`internal/core/delivery`): passed; `SubjectFromEnvelope` routing remains unchanged when envelope `content_type` is empty.

### W6-2 checklist

- [x] Envelope runtime has `content_type` with default fallback to `application/json`.
- [x] Envelope validation accepts empty `content_type` and rejects unsupported values.
- [x] Typed codec registry added with `(type, version, format)` schema key.
- [x] Generic JSON and protobuf codecs added (protobuf marshal deterministic).
- [x] Contracts bootstrap registers marketdata v1 for JSON + protobuf formats.
- [x] Envelope protobuf capability registration added.
- [x] Cross-format semantic equivalence tests added for `TradeTickV1` and `BookDeltaV1`.
- [x] Runtime publish/consume paths remain unchanged (no dual-write activation in W6-2).

## W6-3 Evidence

### Config flag

- Added shared config flag: `marketdata.publish_content_type`.
- Allowed values: `application/json` (default) and `application/protobuf` (opt-in).
- Default remains `application/json`.
- Deploy examples in `deploy/configs/*.jsonc` keep JSON default and show protobuf as a commented opt-in.

### Unit tests

- Added shared codec payload selector tests in `internal/shared/codec/payload_codec_test.go`:
  - trade payload encode/decode works in JSON and protobuf
  - bookdelta payload encode/decode works in JSON and protobuf
  - semantic equivalence holds (`JSON -> domain == PROTO -> domain`)
  - unknown content type is rejected with `ValidationFailed`
- Added marketdata app tests in `internal/core/marketdata/app/ingest_test.go`:
  - `PublishContentType=application/json` produces envelope `content_type=application/json` and payload decodes through JSON codec path
  - `PublishContentType=application/protobuf` produces envelope `content_type=application/protobuf` and payload decodes through protobuf codec path
  - default constructor path remains JSON

### Runtime behavior statement

- Runtime defaults remain unchanged: with no config override, producer envelopes are JSON.
- No actor topology, bus semantics, or routing behavior changed in W6-3.
- Consumer path was intentionally not migrated in this step; publish/consume behavior only changes when producer config explicitly opts into protobuf.
