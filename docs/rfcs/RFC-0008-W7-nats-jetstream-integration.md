# RFC-0008 — W7: NATS JetStream Integration

**Status:** Proposed
**Date:** 2026-02-12
**Author:** Chief Architect
**Workflow:** W7 of PRD-0001
**Relates to:** ADR-0004 (JetStream), ADR-0014 (Stream Partitioning), ADR-0016 (Protobuf)

---

## 1. Goal

Replace InMemoryBus with NATS JetStream as the durable event transport. After W7:
- Envelopes are published to JetStream with `Msg-ID` header (dedup)
- Durable consumers provide at-least-once delivery with ordering guarantees
- Crash recovery: stop and restart consumer — zero message loss
- Runtime bus selection via `-bus=inmemory|jetstream` flag
- InMemoryBus remains available for dev/test (default)

## 2. Scope

- Create `internal/adapters/jetstream/` package (publisher, consumer, config)
- Implement `ports.EventPublisher` over JetStream async publish
- Implement event consumption with durable pull consumers
- Subject schema: `{context}.{event_type}.v{version}.{venue}.{instrument}`
- Wire JetStream into `cmd/consumer` and `cmd/processor` with flag selection
- Add JetStream config section to `config.AppConfig`
- Integration tests with testcontainers-go (NATS server)

## 3. Non-Goals

- Protobuf-encoded payloads on JetStream (uses JSON in Phase 1; proto opt-in after W6 Phase 2)
- Multi-cluster NATS deployment
- Custom retention policies per event type
- KV store or Object store features of JetStream

## 4. Affected Modules

| File | Action | Change |
|------|--------|--------|
| `internal/adapters/jetstream/publisher.go` | CREATE | JetStream publisher implementing EventPublisher |
| `internal/adapters/jetstream/consumer.go` | CREATE | JetStream pull consumer with durable subscription |
| `internal/adapters/jetstream/config.go` | CREATE | JetStream connection + stream + consumer config |
| `internal/adapters/jetstream/subject.go` | CREATE | Subject builder from envelope fields |
| `internal/adapters/jetstream/publisher_test.go` | CREATE | Unit tests with mock JetStream |
| `internal/adapters/jetstream/integration_test.go` | CREATE | Testcontainers NATS: publish, consume, dedup, restart |
| `internal/shared/config/schema.go` | ALTER | Add JetStream config section |
| `internal/shared/envelope/subject.go` | ALTER | SubjectFromEnvelope used by publisher |
| `internal/adapters/go.mod` | ALTER | Add nats-io/nats.go dependency |
| `cmd/consumer/main.go` | ALTER | Add -bus flag, wire JetStream publisher |
| `cmd/processor/main.go` | ALTER | Add -bus flag, wire JetStream consumer |
| `cmd/consumer/config.jsonc` | ALTER | Add JetStream config block |
| `cmd/processor/config.jsonc` | ALTER | Add JetStream config block |

## 5. API Design

### JetStream Publisher

```go
package jetstream

import (
    "github.com/market-raccoon/internal/shared/envelope"
    "github.com/market-raccoon/internal/shared/problem"
)

// Publisher implements ports.EventPublisher over NATS JetStream.
type Publisher struct {
    js   nats.JetStreamContext
    cfg  PublisherConfig
}

type PublisherConfig struct {
    StreamName    string        // "MARKETDATA"
    AsyncMaxPend  int           // max pending async publishes (default 256)
    PublishTimeout time.Duration // per-publish timeout (default 5s)
}

func NewPublisher(js nats.JetStreamContext, cfg PublisherConfig) *Publisher

// Publish maps envelope to JetStream subject and publishes with Msg-ID header.
func (p *Publisher) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
```

**Subject mapping:**
```
SubjectFromEnvelope(env) → "{env.Type}.v{env.Version}.{lowercase(env.Venue)}.{env.Instrument}"
Example: "marketdata.trade.v1.binance.BTCUSDT"
```

**Dedup via Msg-ID:**
```go
msg.Header.Set("Nats-Msg-Id", env.IdempotencyKey)
```

### JetStream Consumer

```go
// Consumer wraps NATS pull subscription for durable consumption.
type Consumer struct {
    sub  *nats.Subscription
    cfg  ConsumerConfig
}

type ConsumerConfig struct {
    StreamName     string        // "MARKETDATA"
    DurableName    string        // "processor-agg-v1"
    FilterSubject  string        // "marketdata.bookdelta.v1.>"
    MaxAckPending  int           // 1 (ordering) or N (parallelism)
    AckWait        time.Duration // 30s
    BatchSize      int           // 10
    FetchTimeout   time.Duration // 5s
}

func NewConsumer(js nats.JetStreamContext, cfg ConsumerConfig) (*Consumer, *problem.Problem)

// Subscribe returns a channel of envelopes and a cancel function.
func (c *Consumer) Subscribe(ctx context.Context) (<-chan envelope.Envelope, func(), *problem.Problem)
```

### JetStream Stream Config

```go
type StreamConfig struct {
    Name         string        // "MARKETDATA"
    Subjects     []string      // ["marketdata.>"]
    Retention    string        // "limits"
    MaxAge       time.Duration // 24h
    MaxBytes     int64         // 10GB
    Storage      string        // "file"
    Replicas     int           // 1
    DedupWindow  time.Duration // 5min
}
```

### Config Schema Addition

```go
type JetStreamConfig struct {
    URL          string        `json:"url"`            // "nats://localhost:4222"
    StreamName   string        `json:"stream_name"`    // "MARKETDATA"
    MaxAge       string        `json:"max_age"`        // "24h"
    MaxBytes     int64         `json:"max_bytes"`      // 10737418240 (10GB)
    DedupWindow  string        `json:"dedup_window"`   // "5m"
    DurablePrefix string      `json:"durable_prefix"` // "raccoon"
}
```

## 6. Subject Hierarchy

```
# Full pattern:
{context}.{event_type}.v{version}.{venue_lower}.{instrument}

# Concrete examples:
marketdata.trade.v1.binance.BTCUSDT
marketdata.bookdelta.v1.binance.ETHUSDT
marketdata.markprice.v1.binance.BTCUSDT
aggregation.snapshot.v1.binance.BTCUSDT

# Wildcard subscriptions:
marketdata.trade.v1.*.BTCUSDT       → All venues, BTC trades
marketdata.*.v1.binance.*           → All event types, Binance, all instruments
marketdata.>                        → Everything under marketdata
```

### Subject Builder

```go
// internal/shared/envelope/subject.go
func SubjectFromEnvelope(env Envelope) string {
    return fmt.Sprintf("%s.v%d.%s.%s",
        env.Type,
        env.Version,
        strings.ToLower(env.Venue),
        env.Instrument,
    )
}
```

## 7. Dedup Strategy

1. **Publisher side:** Set `Nats-Msg-Id` header to `env.IdempotencyKey`
2. **JetStream side:** Stream `DedupWindow=5m` — NATS rejects duplicates within window
3. **Consumer side:** Consumers MUST be idempotent (defense in depth)

Dedup window of 5 minutes means:
- Subsystem restart within 5 minutes of last publish → no duplicates (NATS rejects)
- Subsystem restart after 5 minutes → potential duplicates → consumer idempotency key check

## 8. Error Handling

| Error | Action | Metric |
|-------|--------|--------|
| NATS connection lost | Auto-reconnect (nats.go built-in, MaxReconnects=-1) | `jetstream_reconnects_total` |
| Publish timeout | Retry once, then drop + counter | `jetstream_publish_errors_total` |
| Stream not found | Create stream on startup (idempotent) | N/A |
| Consumer ack timeout | Message redelivered by JetStream | `jetstream_redeliveries_total` |
| Decode error on consume | Term + log (poison), sem redelivery infinita | `bus_consumed_total{status="term"}` |

## 8.1 Poison Policy (W7-2)

W7-2 adota classificação explícita por classe de falha no consumo JetStream:

- `OK` -> `Ack()`
- `TRANSIENT` (`Retryable=true`, `SYS_UNAVAILABLE`, `SYS_INTERNAL`) -> `Nak()`
- `POISON` (`VAL_*`, `MD_OUT_OF_ORDER`, `MD_DUPLICATE`, `AGG_INTEGRITY_VIOLATION`, payload inválido/content-type não suportado) -> `Term()`

Trade-off operacional:

- `Term()` evita loop infinito de redelivery para mensagens não recuperáveis.
- Mensagens poison não são silenciosamente descartadas: recebem `term`, log estruturado e contagem em métricas (`bus_consumed_total{status="term"}`).
- Para falhas temporárias preservamos at-least-once com `Nak()` e redelivery até `max_deliver`.

### Quarantine ACL + Failure Semantics

- Poison envelopes são publicados em `quarantine.v1.{venue}.{instrument}` (taxonomia estrita).
- Se publish em quarantine falhar por erro transitório (`timeout`, `disconnected`, `no responders`), a mensagem original recebe `Nak()`.
- Se falhar por erro permanente de ACL/autorização (`Authorization Violation`, `permission denied`, `forbidden`), a mensagem original recebe `Term()` para evitar NAK storm.
- Métricas:
  - `ingest_nak_total{reason="quarantine_publish_failed"}` para transitório.
  - `ingest_term_total{reason="quarantine_publish_failed"}` para permanente.

### Quarantine Storage Bounds

- O subject `quarantine.>` fica no mesmo stream JetStream com retenção `LimitsPolicy`.
- Boundaries seguem os limites já configurados no stream (`MaxAge`, `MaxBytes`, `Duplicates`) e não usam armazenamento ilimitado.
- Risco operacional: ACL que bloqueia publish em `quarantine.v1.*` move poison para `Term()` (sem redelivery infinito). Garantir permissão explícita em produção.

## 9. Migration Strategy

```
Phase 1 (this RFC):
  - JetStream publisher/consumer adapters created
  - cmd/consumer: -bus=inmemory (default) | -bus=jetstream
  - InMemoryBus still works (regression tested)

Phase 2 (post-W7):
  - Switch default to -bus=jetstream for production
  - InMemoryBus reserved for unit tests and local dev

Phase 3 (with W6 Phase 2):
  - Add ContentType header to published messages
  - Consumers auto-detect JSON vs proto from ContentType
```

## 10. Test Plan

| Type | Test | Pass Criteria |
|------|------|---------------|
| Unit | Publisher serializes envelope with correct subject + Msg-ID | Subject matches pattern, Msg-ID == IdempotencyKey |
| Unit | SubjectFromEnvelope produces correct format | Deterministic, lowercase venue |
| Integration | Testcontainers NATS: publish 1000 messages, consume all | Consumed count == 1000, correct order |
| Integration | Stop consumer, publish 100, restart consumer | All 100 received after restart |
| Integration | Publish duplicate Msg-ID | Second publish silently deduped |
| Integration | Consumer with MaxAckPending=1 | Messages processed in order |
| Regression | `-bus=inmemory` still works end-to-end | Existing tests pass unchanged |
| Benchmark | JetStream publish throughput vs InMemoryBus | Document overhead (expected 2-5x slower) |

## 11. Testcontainers Setup

```go
func setupNATS(t *testing.T) (*nats.Conn, func()) {
    ctx := context.Background()
    container, _ := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "nats:2.10-alpine",
            ExposedPorts: []string{"4222/tcp"},
            Cmd:          []string{"-js"}, // enable JetStream
            WaitingFor:   wait.ForLog("Server is ready"),
        },
        Started: true,
    })
    // ...
}
```

## 12. Acceptance Criteria

- [ ] `cmd/consumer -bus=jetstream` publishes envelopes to JetStream with correct subjects
- [ ] `cmd/processor -bus=jetstream` consumes from JetStream with durable consumer
- [ ] Stop and restart processor: zero message loss verified by count comparison
- [ ] Duplicate publish (same IdempotencyKey) silently deduped by NATS
- [ ] `SubjectFromEnvelope` produces `{type}.v{version}.{venue_lower}.{instrument}` format
- [ ] `-bus=inmemory` still works (regression: existing tests pass)
- [ ] JetStream config section in `config.jsonc` with documented defaults
- [ ] `go test -race ./...` green with testcontainers
- [ ] Stream auto-created on startup if not exists (idempotent)
- [ ] Metrics: `jetstream_publish_errors_total`, `jetstream_reconnects_total` registered

## 13. W7-1 Evidence

Date: 2026-02-12

### Command

```bash
go test ./internal/shared/... ./internal/adapters/... ./cmd/consumer/... ./cmd/server/... ./cmd/processor/...
```

### Output (excerpt)

```text
ok  	github.com/market-raccoon/internal/shared/config
ok  	github.com/market-raccoon/internal/shared/envelope
ok  	github.com/market-raccoon/internal/adapters/bus
ok  	github.com/market-raccoon/internal/adapters/jetstream
?   	github.com/market-raccoon/cmd/consumer	[no test files]
?   	github.com/market-raccoon/cmd/server	[no test files]
?   	github.com/market-raccoon/cmd/processor	[no test files]
```

### Command (JetStream integration)

```bash
go test -tags=integration ./internal/adapters/jetstream -count=1 -v
```

### Output (excerpt)

```text
=== RUN   TestPublisherIntegration_Publish100AndConsume
--- PASS: TestPublisherIntegration_Publish100AndConsume
=== RUN   TestPublisherIntegration_DedupByMsgID
--- PASS: TestPublisherIntegration_DedupByMsgID
=== RUN   TestPublisherIntegration_SubjectFromEnvelope
--- PASS: TestPublisherIntegration_SubjectFromEnvelope
PASS
ok  	github.com/market-raccoon/internal/adapters/jetstream
```

### Command (workspace race)

```bash
DEFAULT_MODCACHE="$(go env GOMODCACHE)"; DEFAULT_GOCACHE="$(go env GOCACHE)"; make test-workspace GO_TEST_FLAGS='-race' GOMODCACHE="$DEFAULT_MODCACHE" GOCACHE="$DEFAULT_GOCACHE"
```

### Output (excerpt)

```text
ok  	github.com/market-raccoon/internal/actors/marketdata/runtime
ok  	github.com/market-raccoon/internal/adapters/jetstream
ok  	github.com/market-raccoon/internal/interfaces/http
ok  	github.com/market-raccoon/internal/shared/metrics
```

## 14. W7-2 Evidence

Date: 2026-02-12

### Command (JetStream integration)

```bash
go test -tags=integration ./internal/adapters/jetstream -count=1 -v
```

### Output (excerpt)

```text
=== RUN   TestConsumerIntegration_DurableRestart
--- PASS: TestConsumerIntegration_DurableRestart
=== RUN   TestConsumerIntegration_PoisonMessageTerminated
--- PASS: TestConsumerIntegration_PoisonMessageTerminated
=== RUN   TestConsumerIntegration_TransientNakThenAck
--- PASS: TestConsumerIntegration_TransientNakThenAck
=== RUN   TestConsumerIntegration_StartStopCycles
--- PASS: TestConsumerIntegration_StartStopCycles
PASS
ok  	github.com/market-raccoon/internal/adapters/jetstream
```

### Command (workspace race)

```bash
DEFAULT_MODCACHE="$(go env GOMODCACHE)"; DEFAULT_GOCACHE="$(go env GOCACHE)"; make test-workspace GO_TEST_FLAGS='-race' GOMODCACHE="$DEFAULT_MODCACHE" GOCACHE="$DEFAULT_GOCACHE"
```

### Output (excerpt)

```text
ok  	github.com/market-raccoon/internal/adapters/jetstream
ok  	github.com/market-raccoon/internal/actors/aggregation/runtime
ok  	github.com/market-raccoon/internal/interfaces/http
ok  	github.com/market-raccoon/internal/shared/metrics
ok  	github.com/market-raccoon/internal/shared/config
```

### Ack/Nak/Term behavior validated

- `DurableRestart`: consumer durável retoma backlog após stop/restart sem perda.
- `PoisonMessageTerminated`: payload inválido é classificado como poison e finalizado com `Term()` (sem redelivery infinita).
- `TransientNakThenAck`: handler transiente gera `Nak()` nas primeiras tentativas e converte para `Ack()` após recuperação.

## 15. W7-2.1 E2E Binary Gate

Date: 2026-02-12

### Command

```bash
go test -tags=integration ./internal/adapters/jetstream -count=1 -run TestE2EProcessorJetStream -v
```

### Output (excerpt)

```text
=== RUN   TestE2EProcessorJetStream
--- PASS: TestE2EProcessorJetStream
PASS
ok  	github.com/market-raccoon/internal/adapters/jetstream
```

### What this gate validates

1. Real `cmd/processor` binary starts with `bus.type=jetstream` and becomes ready (`/readyz`).
2. Consumes `N` envelopes from JetStream and increments `bus_consumed_total{bus_type="jetstream",status="ok"}`.
3. Receives `SIGTERM` and exits under timeout (sem hang/loop).
4. Restart with same durable consumer (`processor-e2e-v1`) consumes backlog published while down.
5. Poison envelope is terminated (`status="term"`) without endless redelivery loop.
6. Transient injection in e2e mode causes redelivery (`bus_redelivered_total > 0`) and eventual success (`status="ok"` increments).

## 16. W7-2.2 AckWait Heartbeat Evidence

Date: 2026-02-12

### What changed (localized)

- `internal/adapters/jetstream/consumer.go`
  - Added processing heartbeat controller that calls `msg.InProgress()` on a ticker while handler is running.
  - Heartbeat period is `ack_wait/3` clamped to `[250ms, 5s]`.
  - Heartbeat stops on terminal disposition path (`Ack/Nak/Term`) and on context cancellation.
  - `InProgress()` errors increment low-cardinality consume metric status `heartbeat_error`; processing continues.
- `internal/adapters/jetstream/consumer_test.go`
  - Added unit tests for heartbeat interval clamp, prompt stop, and bounded goroutine delta under repeated slow-handler simulation.
- `internal/adapters/jetstream/consumer_integration_test.go`
  - Added integration test with `AckWait=1s` and `handler sleep=2.5s` validating bounded redelivery and single effective processing per message set.

### Command (targeted heartbeat integration)

```bash
go test -tags=integration ./internal/adapters/jetstream -run Test...Heartbeat... -count=1 -v
```

### Output (excerpt)

```text
=== RUN   TestConsumerIntegration_HeartbeatPreventsAckWaitRedelivery
--- PASS: TestConsumerIntegration_HeartbeatPreventsAckWaitRedelivery
=== RUN   TestHeartbeatIntervalClamp
--- PASS: TestHeartbeatIntervalClamp
=== RUN   TestAckHeartbeatStopsPromptlyAfterDisposition
--- PASS: TestAckHeartbeatStopsPromptlyAfterDisposition
=== RUN   TestAckHeartbeatGoroutineDeltaBounded
--- PASS: TestAckHeartbeatGoroutineDeltaBounded
PASS
ok  	github.com/market-raccoon/internal/adapters/jetstream
```

### Command (workspace race)

```bash
make test-workspace GO_TEST_FLAGS='-race'
```

### Output (excerpt)

```text
ok  	github.com/market-raccoon/internal/adapters/jetstream
ok  	github.com/market-raccoon/internal/core/marketdata/domain
ok  	github.com/market-raccoon/internal/interfaces/http
ok  	github.com/market-raccoon/internal/shared/metrics
ok  	github.com/market-raccoon/internal/shared/replay
```
