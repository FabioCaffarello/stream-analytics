# Codex Prompt C1 — Consolidation Baseline (Proto Activation + Shard Wiring + E2E Bench)

## Project Identity

Market Raccoon is a high-performance market intelligence platform. Go 1.25+, Hollywood actor model, NATS JetStream, DDD hexagonal architecture. Dual storage (TimescaleDB + ClickHouse). 5 bounded contexts: MarketData, Aggregation, Delivery, Insights, Storage.

---

## Context

After completing S1-S5 production readiness waves, the infrastructure is largely production-grade:
- Real storage drivers (pgx + clickhouse-go) with dual-write committer
- All artifact writers (candle, stats, heatmap, vpvr) for both databases
- Auth (API key), TLS, rate limiting on WS
- ACK-on-commit boundary enforced with soak tests
- Proto codec infrastructure ready (Registry + ProtoCodec[T] + converters + round-trip tests)
- Performance baselines committed at `.benchmarks/baseline.txt`
- Shard partitioning tested (`internal/adapters/jetstream/shard.go`)

**Three gaps remain before multi-exchange expansion:**

1. **Proto hot-path NOT activated** — JetStream publisher still uses JSON. The codec registry supports `FormatProto`, the envelope has `ContentType` field, converters exist, but no code sets `ContentType = "application/protobuf"` on the publish path. Runtime still does JSON encode → JSON decode, missing ~40-60% allocation savings.

2. **Shard topology NOT wired** — `shard.go` has full FNV-1a partitioning with golden value tests, but `cmd/consumer` and `cmd/processor` have no `--shard-index` / `--shard-count` flags. Single-consumer deployment only.

3. **No E2E pipeline benchmark** — Individual benchmarks exist (codec, hash, policykit, ingest, orderbook delta), but no single benchmark covers the full path: parse → ingest → bus → aggregate → hash → snapshot. This gap means latency regression across the pipeline boundary is undetectable.

---

## Mandatory Patterns

### Errors: `*problem.Problem` (never plain `error` in domain/app)
### Import order: stdlib → external → monorepo
### Tests: table-driven, deterministic (use `clock.FakeClock`)
### Proto boundary: proto types ONLY in `internal/shared/proto/gen/` and `internal/shared/contracts/`. Never in `internal/core/`.

---

## Task: Three-Part Consolidation

### PART 1: Activate Protobuf Hot-Path on JetStream

#### Step 1.1: Add ContentType to publish config

**File:** `internal/shared/config/schema.go`

Add wire format configuration:
```go
type BusConfig struct {
    // ... existing fields ...
    WireFormat string `json:"wire_format"` // "json" or "proto" (default: "json")
}
```

Add validation in existing `Validate()`:
```go
switch c.Bus.WireFormat {
case "", "json", "proto":
    // ok
default:
    return problem.Newf(problem.ValidationFailed, "bus.wire_format must be 'json' or 'proto', got %q", c.Bus.WireFormat)
}
```

Add default in `applyDefaults()`:
```go
if c.Bus.WireFormat == "" {
    c.Bus.WireFormat = "json"
}
```

#### Step 1.2: Set ContentType on envelope at publish boundary

**File:** `internal/adapters/jetstream/publisher.go`

Find the function that creates/publishes envelopes to JetStream. Before marshaling:

```go
// Determine wire format from config
contentType := codec.FormatJSON
if cfg.Bus.WireFormat == "proto" {
    contentType = codec.FormatProto
}
env.ContentType = string(contentType)
```

Then the existing `envelope.MarshalBinary(env)` already serializes the envelope with the ContentType field set. The payload bytes inside the envelope should be encoded via `codec.EncodePayload(eventType, version, string(contentType), domainPayload)`.

**IMPORTANT:** Trace the actual publish path. The publisher may already call `codec.EncodePayload()` somewhere — if so, just pass the configured content type instead of hardcoded empty string (which defaults to JSON).

#### Step 1.3: Auto-detect ContentType on consume path

**File:** `internal/adapters/jetstream/consumer.go` (or wherever envelopes are decoded)

After `envelope.UnmarshalBinary(msg.Data)`:
```go
// ContentType comes from the envelope — consumer auto-routes to correct decoder
payload, p := codec.DecodePayload(env.EventType, env.Version, env.ContentType, env.Payload)
```

This should already work because `DecodePayload` reads the ContentType parameter. Verify the existing consume path passes `env.ContentType` (not a hardcoded empty string). If it passes `""`, the codec defaults to JSON — which is the current behavior. Simply ensure `env.ContentType` is forwarded.

#### Step 1.4: Register proto encoders/decoders in bootstrap

**File:** `cmd/consumer/bootstrap.go` and `cmd/processor/bootstrap.go`

Verify that `contracts.BootstrapPayloadCodecRegistry()` is called at startup. This function registers both JSON and Proto codecs for all event types. If it's already called, this step is a no-op.

#### Step 1.5: Golden replay test with proto wire format

**File:** `internal/shared/replay/proto_golden_test.go` (NEW)

```go
func TestGoldenReplay_ProtoWireFormat_DeterministicOutput(t *testing.T) {
    // 1. Load existing golden fixture (JSON-encoded envelopes)
    // 2. Re-encode all envelopes with ContentType = "application/protobuf"
    // 3. Decode proto envelopes back to domain objects
    // 4. Compare domain objects from JSON path vs Proto path
    // 5. Assert: identical domain output (use reflect.DeepEqual or domain Equals)
    //
    // This proves: JSON fixture → domain objects == Proto fixture → domain objects
    // Which is the replay determinism invariant for wire format migration.
}
```

#### Step 1.6: Benchmark proto vs JSON allocation

**File:** `internal/shared/codec/bench_proto_vs_json_test.go` (NEW)

```go
func BenchmarkProtoVsJSON_EncodeDecode(b *testing.B) {
    // Setup: create a representative TradeTickV1 payload

    b.Run("JSON", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            data, _ := codec.EncodePayload("marketdata.trade", 1, "application/json", trade)
            codec.DecodePayload("marketdata.trade", 1, "application/json", data)
        }
    })

    b.Run("Proto", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            data, _ := codec.EncodePayload("marketdata.trade", 1, "application/protobuf", trade)
            codec.DecodePayload("marketdata.trade", 1, "application/protobuf", data)
        }
    })
}
```

Run and include results in commit message to demonstrate allocation improvement.

#### Step 1.7: Update deploy configs

**Files:** `cmd/consumer/config.jsonc`, `cmd/processor/config.jsonc`

Add wire format (default JSON for safety, proto as opt-in):
```jsonc
{
    "bus": {
        // ... existing fields ...
        "wire_format": "json"  // Change to "proto" when ready for production
    }
}
```

---

### PART 2: Wire Shard-Aware Consumer/Processor Topology

#### Step 2.1: Add shard config to schema

**File:** `internal/shared/config/schema.go`

Check if `ShardConfig` already exists. Based on the codebase, `internal/shared/config/schema.go` should already have:
```go
type ShardConfig struct {
    Index int `json:"index"`
    Count int `json:"count"`
}
```

If not, add it. Add to `AppConfig`:
```go
Shard ShardConfig `json:"shard"`
```

Add validation:
```go
if c.Shard.Count > 0 && (c.Shard.Index < 0 || c.Shard.Index >= c.Shard.Count) {
    return problem.Newf(problem.ValidationFailed,
        "shard.index must be in [0, shard.count), got index=%d count=%d",
        c.Shard.Index, c.Shard.Count)
}
```

Add defaults:
```go
if c.Shard.Count == 0 {
    c.Shard.Count = 1 // no sharding by default
}
```

#### Step 2.2: Add CLI flags for shard assignment

**File:** `cmd/consumer/main.go`

Add flags:
```go
shardIndex := flag.Int("shard-index", 0, "shard index for this instance")
shardCount := flag.Int("shard-count", 1, "total number of shards (1 = no sharding)")
```

Apply as config overrides (using existing `bootstrap.ConfigOverride` pattern):
```go
overrides := []bootstrap.ConfigOverride{
    func(cfg *config.AppConfig) {
        if *shardIndex > 0 || *shardCount > 1 {
            cfg.Shard.Index = *shardIndex
            cfg.Shard.Count = *shardCount
        }
    },
}
```

Same for `cmd/processor/main.go`.

#### Step 2.3: Filter JetStream subjects by shard

**File:** `internal/adapters/jetstream/consumer.go`

Find where the consumer subscribes to JetStream subjects. Add shard filter:

```go
import "internal/adapters/jetstream" // for shard.go

// In consumer setup, after getting the subject filter:
if cfg.Shard.Count > 1 {
    // Use shard.go's ShardKey to determine if a message belongs to this shard
    originalHandler := handler
    handler = func(msg jetstream.Msg) {
        env, _ := envelope.UnmarshalBinary(msg.Data())
        key := ShardKey(env.Venue, env.Instrument)
        if key.Partition(cfg.Shard.Count) != cfg.Shard.Index {
            msg.Ack() // not our shard, ack and skip
            return
        }
        originalHandler(msg)
    }
}
```

**IMPORTANT:** Read `internal/adapters/jetstream/shard.go` first to understand the exact `ShardKey` and `Partition` API. Use the existing implementation — do not reinvent.

#### Step 2.4: Integration test for sharded consumers

**File:** `internal/adapters/jetstream/shard_integration_test.go` (NEW)

```go
func TestShardedConsumers_AllEventsDeliveredExactlyOnce(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    // 1. Create 2 sharded consumers (shard 0/2, shard 1/2)
    // 2. Publish 100 envelopes for 10 instruments
    // 3. Each consumer receives disjoint subset
    // 4. Union of both = all 100 envelopes
    // 5. No duplicates across shards
}

func TestShardedReplay_IdenticalOutputToNonSharded(t *testing.T) {
    // 1. Replay golden fixture with shard_count=1 → collect output A
    // 2. Replay same fixture with shard_count=2, shard_index=0 → collect output B0
    // 3. Replay same fixture with shard_count=2, shard_index=1 → collect output B1
    // 4. Assert: output A == union(output B0, B1) (order-independent comparison)
}
```

#### Step 2.5: Update deploy configs

**Files:** `cmd/consumer/config.jsonc`, `cmd/processor/config.jsonc`

```jsonc
{
    "shard": {
        "index": 0,
        "count": 1  // 1 = no sharding; change to N for N-way partition
    }
}
```

#### Step 2.6: Docker Compose scaling support

**File:** `docker-compose.yml` (or `deploy/compose/docker-compose.yml`)

Add environment variable support for shard assignment:
```yaml
  consumer:
    # ... existing config ...
    environment:
      - SHARD_INDEX=${SHARD_INDEX:-0}
      - SHARD_COUNT=${SHARD_COUNT:-1}
    command: /app/consumer --shard-index=${SHARD_INDEX:-0} --shard-count=${SHARD_COUNT:-1}
```

Document in config comment: `docker compose up --scale consumer=2` with env vars.

---

### PART 3: Add E2E Pipeline Benchmark

#### Step 3.1: Create full pipeline benchmark

**File:** `internal/core/aggregation/app/bench_e2e_pipeline_test.go` (NEW)

```go
func BenchmarkE2E_IngestToOrderbookSnapshot(b *testing.B) {
    // Setup:
    // 1. Create InMemoryBus
    // 2. Create AggregationService with in-memory hot stores
    // 3. Create 1000 trade envelopes for a single instrument

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // For each envelope:
        // a. Parse envelope (simulate consumer decode)
        // b. Run IngestMarketData use case (normalize, sequence, publish to bus)
        // c. Bus delivers to subscriber
        // d. Run UpdateOrderBookFromEvents (aggregate delta → snapshot)
        // e. Hash snapshot (idempotency key generation)
        // This covers the FULL hot-path: parse → ingest → bus → aggregate → hash
    }
}

func BenchmarkE2E_TradeToCandle(b *testing.B) {
    // Same pattern but for trade → candle pipeline:
    // Parse → Ingest → Bus → BuildCandleFromEvents → CandleClosed
}
```

#### Step 3.2: Update baseline with new benchmarks

After running the new benchmarks, update `.benchmarks/baseline.txt` to include the E2E results.

**IMPORTANT:** Run `scripts/bench-check.sh` to regenerate baseline:
```bash
make bench-baseline
```

#### Step 3.3: Verify bench-gate includes new benchmarks

**File:** `scripts/bench-check.sh`

Ensure the benchmark runner includes the new E2E packages:
```bash
# Add to benchmark packages list:
go test -run='^$' -bench=. -benchmem -count=5 \
    ./internal/core/aggregation/app/... \
    >> "$CURRENT"
```

---

## Reference Files

| File | Purpose |
|------|---------|
| `internal/shared/codec/proto_codec.go` | ProtoCodec[T] implementation |
| `internal/shared/codec/payload_codec.go` | EncodePayload/DecodePayload with content-type routing |
| `internal/shared/codec/registry.go` | Registry + FormatJSON/FormatProto constants |
| `internal/shared/contracts/payload_registry.go` | BootstrapPayloadCodecRegistry() dual-format registration |
| `internal/shared/contracts/converter_completeness_test.go` | Proto round-trip tests |
| `internal/shared/envelope/envelope.go` | Envelope.ContentType field |
| `internal/adapters/jetstream/publisher.go` | JetStream publish path (currently JSON) |
| `internal/adapters/jetstream/consumer.go` | JetStream consume path |
| `internal/adapters/jetstream/shard.go` | ShardKey + Partition + golden tests |
| `internal/adapters/jetstream/shard_test.go` | Shard invariant tests |
| `internal/shared/config/schema.go` | AppConfig to extend |
| `internal/shared/bootstrap/bootstrap.go` | ConfigOverride pattern |
| `cmd/consumer/main.go` | Consumer CLI flags |
| `cmd/processor/main.go` | Processor CLI flags |
| `cmd/consumer/bootstrap.go` | Consumer composition root |
| `cmd/processor/bootstrap.go` | Processor composition root |
| `internal/shared/replay/golden_test.go` | Existing golden replay test pattern |
| `internal/core/aggregation/app/golden_replay_test.go` | Aggregation golden replay |
| `.benchmarks/baseline.txt` | Current performance baseline |
| `scripts/bench-check.sh` | Benchmark regression gate |
| `internal/shared/codec/bench_hotpath_test.go` | Existing codec benchmark |
| `internal/core/marketdata/app/ingest_bench_test.go` | Existing ingest benchmark |
| `docs/adrs/ADR-0016-protobuf-contract-layer.md` | Proto architecture decision |
| `docs/adrs/ADR-0014-stream-partitioning-strategy.md` | Shard/partition design |

---

## Execution Rules

```bash
# All gates must pass after each part:
make test-workspace
make test-workspace-race
make docs-check
make invariants-check

# Proto-specific gates (Part 1):
make proto-lint
make proto-breaking

# Benchmark gates (Part 3):
make bench-hotpath
# Verify new E2E benchmark is included in bench-check output
```

### STOP CONDITIONS:
- Proto types leaking into `internal/core/` (layering violation)
- Existing golden replay tests failing after proto activation
- Shard filter dropping messages that belong to the shard
- `make bench-gate` regression on existing benchmarks
- ACK-before-commit violation in sharded consumer
- Cross-shard OrderBook corruption (same instrument routed to different shards)

### Commit sequence:
```
feat(c1): activate protobuf wire format on JetStream publish/consume path

- BusConfig.WireFormat: "json" (default) or "proto"
- Publisher sets envelope.ContentType based on config
- Consumer auto-detects content type from envelope
- Golden replay test proves proto/JSON produce identical domain output
- Benchmark: proto codec ~40-60% fewer allocs than JSON
- ADR-0016 status: Proposed → Accepted
- Deploy configs: wire_format="json" (opt-in proto via config)

feat(c1): wire shard-aware consumer/processor topology

- ShardConfig: index + count in AppConfig (default: count=1, no sharding)
- CLI flags: --shard-index, --shard-count for consumer and processor
- JetStream consumer filters messages by shard partition
- Integration test: 2 shards, all events delivered exactly once
- Replay test: sharded output == non-sharded output (order-independent)
- Docker Compose: SHARD_INDEX/SHARD_COUNT env vars

perf(c1): add E2E pipeline benchmark and update baseline

- BenchmarkE2E_IngestToOrderbookSnapshot: parse→ingest→bus→aggregate→hash
- BenchmarkE2E_TradeToCandle: parse→ingest→bus→candle_close
- Baseline updated with E2E numbers
- bench-check.sh includes new benchmark packages

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
```

---

## Important Constraints

1. **Proto activation is opt-in** — default remains JSON. Config `bus.wire_format=proto` switches. This allows gradual rollout.
2. **No breaking change** — existing JSON-encoded messages in JetStream must still be consumable. The consumer reads ContentType from each envelope individually.
3. **Shard assignment is static** — no dynamic rebalancing. ShardKey(venue, instrument) guarantees same instrument always goes to same shard.
4. **Shard filter acks non-matching messages** — messages for other shards are ACKed (not NAKed) so JetStream doesn't redeliver.
5. **E2E benchmark must be reproducible** — use fixed seed / deterministic fixtures, not random data.
6. **Baseline update is manual** — after adding new benchmarks, regenerate with `make bench-baseline`.
7. **Proto in shared/ only** — `internal/shared/proto/gen/`, `internal/shared/contracts/`, `internal/shared/codec/` are the only packages allowed to import protobuf types.
8. **go.mod hygiene** — any new `require` must have corresponding `replace` directive. Run `make tidy` after changes.
