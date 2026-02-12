# PRD-0001 — Market Raccoon Extreme Runtime

**Status:** Draft
**Date:** 2026-02-12
**Author:** Chief Architect
**Relates to:** ADR-0000 through ADR-0011, RFC-0001 through RFC-0004

---

## A) Program Snapshot — Estado Atual

### A.1 O que ja esta bom

| Area | Estado | Evidencia |
|------|--------|-----------|
| **Clean Architecture** | Solid | Domain/app/ports/adapters separation enforced across all bounded contexts. No infrastructure leakage into core. |
| **Problem/Result type system** | Solid | `*problem.Problem` everywhere in domain/app; `Result[T]` for all usecases. No plain `error` in core. Composable via `Map`/`Bind`. |
| **Envelope contract** | Good | Versioned, validated, with idempotency keys, `TopicKey()` deterministic routing. `Validate()` enforced before publish. |
| **Guardian + Supervision** | Good | Exponential backoff, degradation/cooldown, generation counters prevent stale retries, `shuttingDown` flag blocks restart storms. |
| **Binance integration** | Good | Trade + bookdelta parsing with normalization. ParseFuncV2 with instrument metadata. Depth gap detection. |
| **Backpressure queue** | Good | Bounded `wsQueue` (default 1024) with `drop_oldest` and `drop_depth_keep_trades` policies. |
| **InMemoryBus** | Adequate | Non-blocking fan-out, thread-safe, subscriber isolation (one slow sub doesn't block others). |
| **Config loading** | Good | JSONC with comment stripping, fail-fast validation, `applyDefaults()`, duration helpers. |
| **Readiness probes** | Good | `/healthz` (liveness) + `/readyz` (readiness) with Guardian expected-subsystem tracking. |
| **Delivery system** | Good | WebSocket sessions as actors, subject-based routing, subscribe/unsubscribe/getrange protocol. |
| **Normalization** | Good | `naming.CanonicalVenue/Instrument` at domain boundary. Idempotent. |
| **Hashing/Dedup** | Good | `hash.HashFields` for deterministic idempotency keys. FIFO bounded dedup window per stream. |
| **Clock abstraction** | Solid | `FakeClock` in tests, `SystemClock` in prod. No `time.Now()` in domain/app. |
| **Testing** | Good | Race-safe tests with `go test -race`. Spy publishers, fake sequencers, parent actor captures. |

### A.2 Lacunas Criticas

| # | Lacuna | Severidade | Impacto |
|---|--------|-----------|---------|
| L1 | **Sem serialization contract formal** — JSON blobs sem schema enforcement, sem breaking change detection | CRITICA | Wire compat impossivel de garantir; consumers podem quebrar silenciosamente em upgrades |
| L2 | **Sem NATS JetStream** — InMemoryBus perde tudo em crash | CRITICA | Zero durabilidade, zero replay, zero recovery |
| L3 | **Sem deterministic replay** — sequencer in-memory, sem event log persistente | CRITICA | Impossivel reconstruir estado, impossivel backtesting |
| L4 | **Sem metricas Prometheus/OpenTelemetry** — telemetria apenas via slog a cada 100 msgs | ALTA | Sem dashboards, sem alertas, sem baseline de performance |
| L5 | **Sem pprof/profiling endpoints** — heap/CPU/goroutine profiling nao exposto | ALTA | Impossivel diagnosticar leaks ou regressoes de performance em producao |
| L6 | **Maps de estado unbounded** — `IngestMarketData.streams` e `UpdateOrderBookFromEvents.books` crescem indefinidamente | ALTA | Memory leak lento; com 10k instruments, GB-level de heap |
| L7 | **Sem circuit breaker formal** — supervisor policy e exponential backoff, mas sem thresholds de "give up" | MEDIA | Subsystem pode ficar num ciclo restart-fail-restart indefinidamente |
| L8 | **InMemoryBus drop silencioso** — sem contador de drops, sem dead-letter | MEDIA | Data loss invisivel; impossivel saber se subscriber esta recebendo tudo |
| L9 | **Sem multi-exchange testado** — apenas Binance; parser/normalization patterns nao validados com 2+ exchanges | MEDIA | Refactor potencialmente grande ao adicionar Bybit/OKX |
| L10 | **Sem soak test** — nenhuma validacao de estabilidade em 30+ min | MEDIA | Leaks e degradacao progressiva nao detectados |

### A.3 Riscos de Producao

#### R1: Goroutine Leaks
**Onde:** `ws/consumer.go` — `connect()` forks 3 goroutines (readLoop, keepalive, heartbeat) por conexao. Se `connectOnce()` retorna error apos partial setup, goroutines podem nao ser sinalizadas.
**Evidencia:** keepalive/heartbeat goroutines usam `select` em `donech` + `quitch` + `ctx.Done()`, o que e defensivo. Mas `donech` e local a `connectOnce()` — se `connectOnce()` sai antes de fechar `donech`, goroutine pode ficar presa ate `quitch` fechar.
**Mitigacao necessaria:** Audit completa de paths em `connectOnce()` para garantir que `close(donech)` ou `cancel()` e chamado em todos os exits.

#### R2: Timer Leaks
**Onde:** `Guardian.scheduleFn` (`time.AfterFunc`) e `Manager.scheduleFn` usam cancelSchedule, mas:
- Se Guardian recebe `ChildFailed` e schedula retry, e entao recebe `Stop` antes do timer disparar, o timer e cancelado em `stopAll()`. Isso parece correto.
- Manager `scheduledPoison` map e limpo em `handleStopped()`. Tambem parece correto.
**Risco residual:** Se Hollywood entrega `actor.Stopped` antes do timer disparar, o cancel pode nao ser chamado se a referencia se perdeu.

#### R3: Unbounded State Growth
**Onde:**
- `IngestMarketData` mantem `map[string]*InstrumentStream` — um entry por (venue, instrument) normalizado. Nunca evicted.
- `UpdateOrderBookFromEvents` mantem `map[string]*OrderBook` — mesmo pattern.
- `OrderBook.bids/asks` slices crescem com cada novo price level; zero-quantity levels sao removidos, mas em mercados com 10k+ levels, isso pode ser GBs.
**Impacto:** Em cenario multi-exchange com centenas de instruments, heap cresce linearmente sem bound.

#### R4: Reconexao sem Backoff Global
**Onde:** Cada `Consumer` tem seu proprio backoff independente. Se 50 consumers reconectam simultaneamente (e.g. apos network blip), todos tentam dial ao mesmo tempo.
**Impacto:** Thundering herd contra exchange API; potencial IP ban.

#### R5: InMemoryBus como Single Point of Failure
**Onde:** `Publish()` e `Subscribe()` operam em memoria. Crash do processo perde todo o estado.
**Impacto:** Pior para aggregation — OrderBook state e perdido sem recovery.

### A.4 Onde Provavelmente ha Leaks/Overhead

| Local | Tipo | Probabilidade |
|-------|------|--------------|
| `consumer.go:connectOnce()` partial setup path | Goroutine leak | Media |
| `IngestMarketData.streams` map | Memory leak (lenta) | Alta (com muitos instruments) |
| `OrderBook.bids/asks` levels com mercados profundos | Memory overhead | Alta |
| `parserTelemetry` maps (`byEvent`, `byWSStream`, `byTicker`, etc.) | Memory growth | Baixa (bounded by instrument count) |
| `ws.Manager` stream rotation com overlap | Goroutine leak (curta) | Baixa |
| `InMemoryBus` subscriber channels nunca drained | Backpressure → GC pressure | Media |

### A.5 Onde Contratos Ainda Estao Frouxos

| Contrato | Problema | Impacto |
|----------|----------|---------|
| **Envelope.Payload** | `[]byte` JSON blob sem schema registry | Consumers decodam "na fe"; breaking change = crash silencioso |
| **Event type names** | String constants (`"marketdata.trade"`) sem geracao de schema | Typos nao detectados em compile time |
| **Subject format** | `"streamType/venue/symbol/timeframe"` — parsing manual | Inconsistencia possivel entre producers e consumers |
| **BookDeltaV1 IDs** | `FirstID/FinalID/PrevFinal` sao int64 sem validacao cruzada entre envelope seq e depth update IDs | Gap detection depende de heuristica na telemetry, nao do domain |
| **Bus drop behavior** | Nao documentado formalmente; consumers nao sabem que perderam mensagem | Silent data loss |
| **WebSocket session limits** | Sem max_connections, sem rate limiting | DoS vector |

---

## B) PRD — Extreme Runtime

### B.1 Visao

Transformar o market-raccoon de um prototipo funcional (W1-W3 completos) em uma plataforma de producao extrema capaz de:

1. Ingerir market data de multiplos exchanges com **zero data loss duravel**
2. Manter **latencia sub-10ms** do WS receive ao envelope publicado no bus
3. Suportar **replay deterministico** de qualquer janela temporal
4. Garantir **memory bounded** em todos os caminhos de dados
5. Expor **observabilidade completa** (metricas, profiling, tracing)
6. Validar **contratos de wire** com schemas formais e breaking change detection

### B.2 Objetivos (SLOs / SLIs)

| SLI | SLO | Metodo de Medicao |
|-----|-----|-------------------|
| Latencia ingest (WS recv → envelope published) | p50 < 1ms, p99 < 10ms, p999 < 50ms | Histogram Prometheus `ingest_latency_seconds` |
| Queda maxima sem perda | 0 mensagens perdidas em crash com JetStream | Replay test: compare published vs consumed count |
| Recovery time (subsystem restart) | < 5s para subsystem, < 30s para full recovery | Timer from ChildFailed to first successful ingest |
| Duplicacao | 0 duplicatas entregues a consumers | IdempotencyKey enforcement + dedup window |
| Gap detection | 100% de gaps > 1 reportados em metricas | `depth_gaps_total` counter |
| Goroutine stability | Δ goroutines = 0 em steady state (30min soak) | `runtime.NumGoroutine()` via pprof |
| Heap stability | Heap alloc rate estabiliza em < 2x baseline apos 30min | pprof heap profile |
| Shutdown graceful | Completa em < 10s com 0 goroutine leaks | SIGTERM → process exit timing |

### B.3 Nao-Objetivos

- **Arbitragem automatica** — nao implementar estrategias; apenas invariantes e hooks
- **Order book completo (L3 depth)** — apenas bookdelta + snapshot ports/flows compativeis
- **Execution engine** — zero ordem real; sistema e decision support
- **Multi-region deployment** — single-region por enquanto
- **Historical backfill** — replay do JetStream sim; backfill de REST API nao
- **UI/dashboard** — sem frontend; apenas APIs e WebSocket

### B.4 Personas

| Persona | Necessidade | SLO Relevante |
|---------|-------------|---------------|
| **Operator** | Deploy, monitor, restart, escalar. Precisa de metricas, alertas, graceful shutdown, runbooks. | Recovery < 5s, shutdown < 10s, metricas completas |
| **Developer** | Adicionar exchange, evento, use case. Precisa de contratos claros, testes deterministicos, replay local. | Replay deterministico, schema validation, golden tests |
| **Quant/Arbitrage Consumer** | Consumir streams normalizados de multiplos exchanges. Precisa de latencia baixa, zero gaps, cross-venue timestamps. | p99 < 10ms, gap detection 100%, dedup 100% |

### B.5 Observabilidade Requerida

#### Metricas (Prometheus)

```
# Ingest pipeline
ingest_messages_total{venue, instrument, event_type, status}
ingest_latency_seconds{venue, instrument, event_type}        # histogram
ingest_sequence_gaps_total{venue, instrument}
ingest_duplicates_total{venue, instrument}

# Backpressure
backpressure_queue_depth{venue}                               # gauge
backpressure_drops_total{venue, policy}
backpressure_entered_total{venue}

# WebSocket
ws_connections_active{exchange}                               # gauge
ws_reconnects_total{exchange, reason}
ws_connection_uptime_seconds{exchange}                        # histogram
ws_messages_received_total{exchange, stream}
ws_errors_total{exchange, kind}

# Bus
bus_published_total{type, venue}
bus_dropped_total{subscriber_id}                              # se InMemoryBus
bus_subscriber_lag{subscriber_id}                             # gauge

# Actor runtime
guardian_restarts_total{subsystem, kind}
guardian_degraded_total{subsystem}
guardian_subsystem_state{subsystem}                           # gauge: 0=stopped, 1=running, 2=degraded

# System
process_goroutines                                            # gauge
process_heap_alloc_bytes                                      # gauge
process_gc_pause_seconds                                      # histogram
```

#### Profiling

- `/debug/pprof/` — standard Go pprof endpoints (heap, goroutine, cpu, mutex, block)
- Protected by admin auth or localhost-only binding

#### Logging

- Structured slog (JSON format in prod, text in dev)
- Levels: ERROR (operator action needed), WARN (degraded but recovering), INFO (lifecycle events), DEBUG (message-level detail)
- Sampled logging for high-frequency events (1 sample per 30s per key) — ja implementado na telemetry

### B.6 Definition of Done

O programa "Extreme Runtime" esta Done quando:

1. **Soak test 60min** com Binance real (2 tickers + 200 tickers) passa sem:
   - Goroutine growth > 5% do baseline
   - Heap growth > 10% do baseline apos GC stabilization
   - Dropped messages (bus drops = 0 em operacao normal)
   - Sequence gaps nao-justificados (exchange-side gaps OK; internal gaps = bug)

2. **`go test -race ./...` verde** em todos os modulos

3. **Replay deterministico funcional**: dado fixture de 1000 envelopes gravados, replay produz output identico

4. **Metricas Prometheus** expostas e scraped com sucesso

5. **pprof endpoints** funcionais e protegidos

6. **Schema validation**: pelo menos `marketdata.trade.v1` e `marketdata.bookdelta.v1` com proto schema, buf lint, breaking change detection

7. **Shutdown graceful < 10s** com verificacao de 0 goroutine leaks

8. **Multi-exchange readiness**: segundo exchange (e.g. Bybit stub) compila e parseia sem mudancas no core

---

## C) ADRs — Revisao + Novas

### C.1 ADRs Existentes que Devem Ser Revisadas

#### ADR-0002 (Envelope Design & Versioning)
**O que falta:**
- Nao define wire format formal (JSON vs protobuf vs CBOR)
- Nao define schema registry ou como consumers descobrem schemas
- Nao define regras de compatibilidade (field reservation, field reuse prohibition)
- `Meta` map[string]string e ad-hoc; precisa de campos padronizados (trace_id, correlation_id, source_stream)
- **Acao:** Revisar para incluir wire format strategy e schema discovery

#### ADR-0003 (Actor Runtime — Hollywood)
**O que falta:**
- Nao documenta lifecycle guarantees (quando `actor.Stopped` e entregue vs filhos)
- Nao documenta `engine.Request()` pattern e fallback `c.Sender()`
- Nao documenta generation counter pattern para stale retry prevention
- Nao documenta max restart limits (atualmente depende de SupervisorPolicy, mas ADR nao menciona)
- **Acao:** Expandir com lifecycle model, request/reply pattern, stale prevention

#### ADR-0004 (NATS JetStream)
**O que falta:**
- Ainda nao implementado; ADR esta muito high-level
- Nao define subject hierarchy concreta com version prefix
- Nao define consumer config (MaxAckPending, DeliverPolicy, AckPolicy)
- Nao define dedup window configuration
- Nao define stream retention policy (limits vs interest vs workqueue)
- **Acao:** Expandir com config concreta antes de implementar W3-JetStream

#### ADR-0005 (Sequencing Strategy)
**O que falta:**
- Sequencer e in-memory; nao define persistencia ou recovery
- Nao define como seq interage com JetStream seq (dois sistemas de sequencia)
- Nao define como replay reseta ou preserva sequence state
- **Acao:** Expandir com persistent sequencer strategy e replay interaction

#### ADR-0009 (Configuration)
**O que falta:**
- Nao define hot-reload strategy (atualmente POST /runtime/reload faz restart, nao hot-reload)
- Nao define config para multi-exchange (como configurar N exchanges com configs diferentes)
- Nao define secrets management (API keys para exchanges que requerem auth)
- **Acao:** Expandir com multi-exchange config pattern

#### ADR-0011 (Binance Canonical Mapping)
**O que falta:**
- Nao define como market type (SPOT vs PERP) afeta canonical instrument
- Nao define mapping table strategy (hardcoded vs dynamic discovery)
- Nao define como novos event types sao adicionados sem changing parser
- **Acao:** Expandir com market type normalization rules

### C.2 ADRs Novas Propostas

#### ADR-0012 — Lifecycle Invariants & Leak Prevention

**Decision:**
1. Every goroutine spawned by an actor MUST have a deterministic cancellation path via `context.Context` OR a `chan struct{}` sentinel.
2. Every `time.AfterFunc` MUST store its `cancelSchedule` function and cancel it in `actor.Stopped`.
3. Every WebSocket connection MUST have explicit close ordering: (1) cancel context, (2) write close frame, (3) close conn, (4) close channels.
4. Actor state maps MUST be bounded with TTL eviction OR max-size eviction.
5. InstrumentStream and OrderBook maps: add `maxInstruments` config with LRU eviction.
6. `runtime.NumGoroutine()` MUST be exposed as a metric and validated in soak tests.

**Tradeoffs:**
- LRU eviction adds complexity and may cause re-initialization cost
- Strict lifecycle ordering may require refactoring consumer.go
- TTL eviction requires clock injection (already available)

#### ADR-0013 — Backpressure & Overload Policies

**Decision:**
1. All queues/channels between pipeline stages are bounded with configurable capacity.
2. Drop policies are documented per queue:
   - `wsQueue` (WS → SubsystemActor): `drop_depth_keep_trades` (default) or `drop_oldest`
   - `InMemoryBus` subscriber channels: drop newest (current behavior) + emit counter
   - JetStream publish: block with timeout + circuit breaker if persistent failure
3. Priority: trades > bookdelta > mark price > liquidation (configurable).
4. Load shedding: when queue depth > 80% capacity, log warning; when 100%, drop + metric.
5. Coalescing: bookdelta events for same instrument within 10ms window may be coalesced (future opt-in).

**Tradeoffs:**
- Trade priority means bookdelta may have higher latency under load
- Coalescing introduces artificial latency for order book freshness
- Silent drops are unacceptable — every drop MUST increment a counter

#### ADR-0014 — Stream Partitioning Strategy

**Decision:**
1. NATS subject hierarchy: `marketdata.{event_type}.v{version}.{venue}.{instrument}`
   - Example: `marketdata.trade.v1.binance.BTCUSDT`
2. Partitioning key: `(venue, instrument)` — all events for one instrument on one partition.
3. Consumer parallelism: one consumer per `(venue, instrument)` group for ordering guarantee.
4. Cross-venue queries: arbitrage consumers subscribe to `marketdata.trade.v1.*.BTCUSDT` (wildcard on venue).
5. Hashing: consistent hash of `(venue, instrument)` for deterministic worker assignment (future).

**Tradeoffs:**
- Per-instrument consumers limit parallelism to instrument count
- Wildcard subscriptions may have higher fan-in cost on NATS
- Consistent hashing adds complexity vs simple round-robin

#### ADR-0015 — Deterministic Replay & Time Invariants

**Decision:**
1. All domain logic is pure: receives `Clock` port, never calls `time.Now()`.
2. Replay mode: inject `FakeClock` with timestamps from recorded envelopes.
3. Sequence invariant: `seq` is monotonic per `(venue, instrument)` — replay preserves original seq.
4. Replay modes:
   - **Full replay**: reprocess all events from JetStream from seq 0
   - **Catchup window**: reprocess events from `now - window` to `now`
5. Timestamp normalization: `TsExchange` is advisory (exchange clock); `TsIngest` is authoritative (our clock).
6. Schema versioning: replay must handle envelopes with version N-1 (backward compat required).
7. Golden tests: record fixture of 1000 envelopes → replay → compare output byte-for-byte.

**Tradeoffs:**
- FakeClock replay requires deterministic sequencer (no random jitter in seq)
- Full replay is slow for large streams — catchup window is practical default
- Golden tests are brittle to intentional output changes — need maintenance

#### ADR-0016 — Protobuf Contract Layer

**Decision:**
1. Directory: `/proto/` at repo root with language-agnostic schemas.
2. Tooling: Buf (buf.build) for lint, breaking change detection, code generation.
3. Schema versioning: `marketdata/v1/trade.proto`, `marketdata/v1/bookdelta.proto`, `envelope/v1/envelope.proto`.
4. Compatibility rules:
   - Fields never reused (use `reserved`)
   - Required fields never removed (only deprecated)
   - `oneof` fields never added to existing messages
   - Wire compatibility validated by `buf breaking` on every PR
5. Coexistence with JSON: envelope metadata stays JSON-friendly (HTTP API); payload bytes can be protobuf on bus.
6. Migration: dual-write period where both JSON and protobuf payloads are published; consumers opt-in to protobuf.
7. Schema registry lite: `proto/registry.json` manifest listing all (type, version, proto_file) tuples.
8. Go code generation: `buf generate` with `go_out` plugin, output to `internal/shared/proto/gen/`.

**Tradeoffs:**
- Buf adds build-time dependency
- Dual-write increases bandwidth temporarily
- Proto-first design may conflict with JSON-friendly external API
- Generated code increases repo size but ensures type safety

#### ADR-0017 — Multi-Exchange Normalization

**Decision:**
1. Canonical instrument format: `BASE-QUOTE` (e.g., `BTC-USDT`), always uppercase.
2. Venue symbols mapped to canonical via `InstrumentCatalog` port (per-exchange adapter).
3. Market type (`SPOT`, `USD_M_FUTURES`, `COIN_M_FUTURES`) is metadata, not part of instrument ID.
4. Two instruments with same `BASE-QUOTE` but different market types are DIFFERENT streams (different stream IDs).
5. Stream ID = `venue:instrument:market_type` (e.g., `BINANCE:BTCUSDT:USD_M_FUTURES`).
6. Exchange adapter responsibility: normalize venue symbol to canonical + resolve market type.
7. Cross-venue normalization: same `BASE-QUOTE` across exchanges enables arbitrage join.

**Tradeoffs:**
- Including market_type in stream ID increases cardinality
- Some exchanges have ambiguous instrument naming (e.g., Bybit BTCUSDT vs BTC/USDT)
- Canonical format must be stable — changing it requires migration

#### ADR-0018 — Actor Topology & Supervision Model

**Decision:**
1. Topology:
   ```
   Guardian (root)
   ├── MarketDataSubsystem (per-exchange? or single with multi-exchange manager?)
   │   ├── WS Manager
   │   │   ├── Consumer[bucket-0]
   │   │   ├── Consumer[bucket-1]
   │   │   └── ...
   │   └── IngestWorker (goroutine, not actor)
   ├── AggregationSubsystem
   │   └── consumeLoop (goroutine)
   ├── DeliverySubsystem
   │   ├── RouterActor
   │   │   └── SessionActor[session-id] (per WS client)
   │   └── ...
   └── InsightsSubsystem (future)
   ```
2. Multi-exchange: ONE MarketDataSubsystem per exchange, each with its own WS Manager.
3. Supervision: Guardian → Subsystem → Workers. Failure at worker level restarts worker only. Failure at subsystem level triggers Guardian policy.
4. Restart policy per error kind:
   - Transient WS errors (dial, read): Consumer-level retry with backoff (no escalation)
   - Non-transient WS errors: Escalate to Guardian → restart subsystem
   - Bus closure: Fatal for aggregation subsystem → Guardian restarts
   - Domain errors (out-of-order, duplicate): Log + skip (no restart)
5. Circuit breaker: after `RestartLimit` failures within `RestartWindow`, enter degraded mode with cooldown.
6. No double-publish guarantee: subsystem tracks `lastPublishedSeq` per stream; on restart, skip messages with seq <= lastPublishedSeq.
7. Restart storm prevention: global restart rate limiter (max 3 subsystem restarts per minute across all subsystems).

**Tradeoffs:**
- Per-exchange subsystem increases actor count but improves isolation
- Global rate limiter may delay recovery of healthy subsystems
- lastPublishedSeq tracking adds state that must survive restarts (or be reconstructed from bus)

---

## D) RFCs — Work Packages (Execucao Incremental)

### D.1 RFC-0005 — W4: Observability & Profiling

**Escopo:**
- Add Prometheus metrics exporter (prometheus/client_golang)
- Add pprof HTTP endpoints (net/http/pprof)
- Instrument ingest pipeline, backpressure queue, bus, guardian, WS consumer
- Add `/metrics` endpoint
- Add `/debug/pprof/` tree (localhost-only or auth-protected)

**Arquivos provaveis a tocar:**
- `internal/shared/metrics/` (CRIAR) — metric registry, helpers
- `internal/core/marketdata/app/ingest.go` — add latency histogram, counter
- `internal/actors/marketdata/runtime/subsystem.go` — expose queue depth, drops, ws metrics
- `internal/actors/runtime/guardian.go` — expose restart/degraded counters
- `internal/adapters/bus/inmemory.go` — add drop counter
- `internal/interfaces/http/server.go` — add `/metrics`, `/debug/pprof/`
- `cmd/*/main.go` — wire metrics registry

**Mudancas de API internas:**
- `IngestMarketData` receives optional `MetricsRegistry`
- `InMemoryBus` receives optional `MetricsRegistry` for drop counting
- `SubsystemActor` exports queue depth/drops as Prometheus gauges/counters

**Test plan:**
- Unit: metric counters increment correctly on ingest/drop/restart
- Integration: `/metrics` endpoint returns valid Prometheus exposition format
- Soak: metrics endpoint scraped every 15s during 30min test

**Metricas/telemetria adicionadas:**
- All metrics listed in B.5

**Criterios de aceite:**
- [ ] `curl localhost:8080/metrics` returns Prometheus format with all defined metrics
- [ ] `curl localhost:8080/debug/pprof/goroutine?debug=1` returns goroutine dump
- [ ] pprof endpoints NOT accessible from public interface (localhost or auth only)
- [ ] `go test -race ./...` verde
- [ ] No performance regression > 5% on ingest throughput (benchmark before/after)

---

### D.2 RFC-0006 — W5: Memory Leak Mitigation & Lifecycle Hardening

**Escopo:**
- Audit and fix all goroutine lifecycle paths in `ws/consumer.go`
- Add TTL/LRU eviction to `IngestMarketData.streams` and `UpdateOrderBookFromEvents.books`
- Bound `OrderBook.bids/asks` to configurable max levels
- Add `runtime.NumGoroutine()` metric and soak-test validation
- Formalize shutdown choreography (quitch → ctx → closeConn → stopRepeaters → cancelTimers)
- Add goroutine leak detector to test suite

**Arquivos provaveis a tocar:**
- `internal/actors/marketdata/ws/consumer.go` — lifecycle audit, explicit donech/cancel paths
- `internal/core/marketdata/app/ingest.go` — add stream eviction (LRU with TTL)
- `internal/core/aggregation/app/update_orderbook.go` — add book eviction
- `internal/core/aggregation/domain/orderbook.go` — add max levels config
- `internal/actors/runtime/guardian.go` — global restart rate limiter
- `internal/shared/ds/` (CRIAR) — generic LRU cache with TTL

**Mudancas de API internas:**
- `IngestConfig` gains `MaxStreams int` and `StreamTTL time.Duration`
- `UpdateOrderBookConfig` gains `MaxBooks int` and `BookTTL time.Duration`
- `OrderBook` constructor gains `MaxLevels int`
- `Consumer` gains explicit `closeResources()` method called from all exit paths

**Test plan:**
- Unit: LRU eviction correctness (insert, access, evict, TTL expire)
- Unit: OrderBook max levels enforced (excess levels trimmed)
- Integration: goroutine count stable after 1000 connect/disconnect cycles
- Soak: 30min with 200 tickers — goroutine count delta < 5, heap growth < 10%

**Criterios de aceite:**
- [ ] `go test -count=1 -race -run TestGoroutineLeak` passes (goroutine count before == after)
- [ ] IngestMarketData with MaxStreams=10 evicts oldest stream when 11th arrives
- [ ] OrderBook with MaxLevels=100 never has > 100 levels on either side
- [ ] Consumer stop/reconnect cycle: 0 goroutine leaks over 100 iterations
- [ ] pprof heap profile shows stable allocations in 30min soak

---

### D.3 RFC-0007 — W6: Protobuf Contract Layer

**Escopo:**
- Create `/proto/` directory with schema definitions
- Set up Buf toolchain (buf.yaml, buf.gen.yaml, buf.lock)
- Define proto schemas for envelope.v1, marketdata.trade.v1, marketdata.bookdelta.v1
- Generate Go code to `internal/shared/proto/gen/`
- Add `buf lint` and `buf breaking` to CI
- Create schema registry manifest (`proto/registry.json`)
- Integrate proto serialization alongside JSON (dual-codec support in envelope)

**Arquivos provaveis a tocar:**
- `proto/envelope/v1/envelope.proto` (CRIAR)
- `proto/marketdata/v1/trade.proto` (CRIAR)
- `proto/marketdata/v1/bookdelta.proto` (CRIAR)
- `proto/buf.yaml` (CRIAR)
- `proto/buf.gen.yaml` (CRIAR)
- `proto/registry.json` (CRIAR)
- `internal/shared/proto/gen/` (GERADO)
- `internal/shared/codec/codec.go` — add proto codec option
- `internal/shared/envelope/envelope.go` — add ContentType field ("application/json" | "application/protobuf")
- `Makefile` — add `proto-gen`, `proto-lint`, `proto-breaking` targets

**Mudancas de API internas:**
- `Envelope` gains `ContentType string` field
- `codec.Marshal/Unmarshal` gains format parameter or auto-detection from ContentType
- `codec.Registry` maps `(EventType, Version, ContentType)` → Decoder

**Migracao:**
1. Phase 1: Define schemas, generate code, run `buf lint` — no runtime impact
2. Phase 2: Add proto codec alongside JSON — producers can opt-in
3. Phase 3: Dual-write (both JSON and proto payloads) for migration window
4. Phase 4: Consumers switch to proto; JSON deprecated

**Test plan:**
- Unit: proto marshal/unmarshal roundtrip matches JSON for all payload types
- Unit: `buf lint` passes
- Integration: `buf breaking --against .git#branch=main` passes on PR
- Golden: recorded JSON fixtures re-encoded as proto produce identical domain objects

**Criterios de aceite:**
- [ ] `buf lint` passes with no errors
- [ ] `buf breaking` detects intentional breaking change (e.g., removed field) and fails
- [ ] Proto-encoded TradeTickV1 decodes identically to JSON-encoded version
- [ ] `make proto-gen` generates Go code without manual intervention
- [ ] `proto/registry.json` lists all schemas with versions
- [ ] Envelope ContentType field defaults to "application/json" (backward compat)

---

### D.4 RFC-0008 — W7: NATS JetStream Integration

**Escopo:**
- Implement `internal/adapters/jetstream/` publisher and consumer adapters
- Publisher implements `ports.EventPublisher` — publishes envelopes to JetStream with `Msg-ID` header
- Consumer implements event consumption with durable consumers
- Subject schema: `marketdata.{event_type}.v{version}.{venue}.{instrument}`
- Consumer registry with refcount (create/destroy NATS consumers on demand)
- Idempotency via NATS dedup window + envelope IdempotencyKey
- Flag `-bus=inmemory|jetstream` for runtime selection

**Arquivos provaveis a tocar:**
- `internal/adapters/jetstream/publisher.go` (CRIAR)
- `internal/adapters/jetstream/consumer.go` (CRIAR)
- `internal/adapters/jetstream/consumer_registry.go` (CRIAR)
- `internal/adapters/jetstream/jetstream_test.go` (CRIAR)
- `internal/shared/config/schema.go` — add JetStream config section
- `cmd/consumer/main.go` — add bus selection flag
- `cmd/processor/main.go` — add bus selection flag
- `go.mod` files — add `nats-io/nats.go` dependency

**Mudancas de API internas:**
- `ports.EventPublisher` unchanged (already correct interface)
- `ports.EventConsumer` (NEW) — `Subscribe(subject) (<-chan Envelope, func(), *problem.Problem)`
- `ConsumerRegistry` manages lifecycle of durable consumers per subject
- `config.AppConfig` gains `JetStream` section (URL, stream name, durable name prefix, dedup window)

**Test plan:**
- Unit: publisher serializes envelope correctly with Msg-ID header
- Unit: consumer receives and deserializes envelopes
- Integration: testcontainers NATS — publish 1000 messages, consume all, verify ordering
- Integration: stop/restart consumer — verify no message loss (durable consumer)
- Integration: duplicate publish (same Msg-ID) — verify dedup

**Criterios de aceite:**
- [ ] `cmd/consumer -bus=jetstream` publishes to JetStream
- [ ] `cmd/processor -bus=jetstream` consumes from JetStream with durable consumer
- [ ] Stop and restart processor — zero message loss
- [ ] Duplicate publish detected and suppressed by NATS dedup
- [ ] Consumer registry creates/destroys consumers as sessions subscribe/unsubscribe
- [ ] `cmd/consumer -bus=inmemory` still works (regression)
- [ ] `go test -race ./...` verde with testcontainers

---

### D.5 RFC-0009 — W8: Deterministic Replay & Golden Tests

**Escopo:**
- Record fixtures: capture N envelopes from live stream to file (JSON-lines)
- Replay engine: read fixtures, inject into domain with FakeClock, compare output
- Golden test framework: replay fixture → serialize output → compare against golden file
- Replay mode in cmd: `-replay=fixtures/binance-1000.jsonl`
- Validate determinism: same input always produces same output

**Arquivos provaveis a tocar:**
- `internal/shared/replay/` (CRIAR) — replay engine, fixture reader/writer
- `internal/shared/replay/recorder.go` — captures envelopes to file during live run
- `internal/shared/replay/player.go` — replays from file with FakeClock
- `internal/core/marketdata/app/ingest_test.go` — golden test for ingest pipeline
- `internal/core/aggregation/app/update_orderbook_test.go` — golden test for aggregation
- `cmd/consumer/main.go` — add `-replay` flag
- `testdata/fixtures/` (CRIAR) — golden fixture files

**Mudancas de API internas:**
- `replay.Recorder` wraps `EventPublisher` — intercepts and writes to file
- `replay.Player` implements `EventPublisher` + feeds clock — replays from file
- `IngestMarketData` already receives Clock port — no change needed
- `Sequencer` must be deterministic in replay (use recorded seq, not new assignment)

**Test plan:**
- Unit: recorder writes valid JSONL; player reads it back identically
- Integration: record 100 envelopes → replay → compare output envelope-by-envelope
- Golden: `go test -update-golden` writes expected output; subsequent runs compare

**Criterios de aceite:**
- [ ] Record 1000 envelopes from live Binance stream to fixture file
- [ ] Replay fixture produces byte-identical output (envelope payload + metadata)
- [ ] FakeClock replay uses timestamps from fixture (not wall clock)
- [ ] Golden test fails if domain logic changes output (intentional detection)
- [ ] `go test -run TestGolden -update-golden` regenerates golden files

---

### D.6 RFC-0010 — W9: Multi-Exchange Readiness

**Escopo:**
- Add second exchange adapter (Bybit or OKX stub)
- Validate normalization patterns work with 2+ exchanges
- Add `InstrumentCatalog` port implementation for exchange instrument discovery
- Configure multi-exchange in single process (N MarketDataSubsystems)
- Validate cross-venue Subject routing for delivery
- Add arbitrage hooks: detect same `BASE-QUOTE` across venues, emit cross-venue events

**Arquivos provaveis a tocar:**
- `internal/adapters/exchange/bybit/` (CRIAR) — parser, endpoint builder
- `internal/core/marketdata/ports/ports.go` — `InstrumentCatalog` interface enhancement
- `internal/actors/runtime/guardian.go` — support N MarketDataSubsystems
- `internal/shared/config/schema.go` — multi-exchange config (array of ConsumerConfig)
- `cmd/consumer/main.go` — spawn multiple MarketDataSubsystems

**Mudancas de API internas:**
- `GuardianConfig.Factories` gains per-exchange keying: `Subsystem` becomes `SubsystemKey` (e.g., "marketdata:binance", "marketdata:bybit")
- OR: single MarketDataSubsystem spawns multiple Managers (one per exchange)
- `InstrumentCatalog` gains `ListInstruments(exchange, marketType) ([]InstrumentMetadata, *problem.Problem)`

**Test plan:**
- Unit: Bybit parser handles trade + bookdelta with different field names
- Integration: two exchanges ingesting simultaneously, both publish to same bus
- Integration: cross-venue Subject subscription receives events from both exchanges
- Regression: single-exchange mode unaffected

**Criterios de aceite:**
- [ ] Bybit adapter parses at least trade + bookdelta events
- [ ] Two exchanges run in same process without interference
- [ ] Subject `marketdata.trade/bybit/BTC-USDT/raw` delivers correctly
- [ ] `naming.CanonicalInstrument` produces same result for Binance "BTCUSDT" and Bybit "BTCUSDT"
- [ ] No changes to `internal/core/*` domain logic
- [ ] `go test -race ./...` verde

---

### D.7 Sequencia de Execucao

```
W4 (Observability)  ──────────────────► done
W5 (Leak Mitigation) ─────────────────► done
         W6 (Protobuf) ───────────────► done
              W7 (JetStream) ─────────► done
                   W8 (Replay) ───────► done
                        W9 (Multi-Ex) ► done
```

**Dependencias:**
- W4 e W5 podem ser paralelos
- W6 pode comecar apos W4 (precisa de metrics para validar codec perf)
- W7 depende de W6 (protobuf schemas para NATS payloads) — ou pode usar JSON first e migrar
- W8 depende de W5 (needs deterministic clock/seq) e W7 (replays from JetStream)
- W9 depende de W7 (needs real bus) e W5 (needs lifecycle hardening)

---

## E) Design Detalhado

### E.1 Memory Leak Mitigation Plan

#### E.1.1 Goroutine Leak Prevention

**Regra:** Nenhum goroutine sem caminho de cancelamento.

**Consumer goroutines (3 por conexao):**
```go
// CURRENT: readLoop, keepalive, heartbeat all select on donech/quitch/ctx.Done()
// PROBLEM: if connectOnce() returns error after spawning keepalive but before readLoop,
//          donech might not be closed

// FIX: defer close(donech) at start of connectOnce()
func (c *Consumer) connectOnce() (string, error) {
    donech := make(chan struct{})
    defer close(donech) // ALWAYS closes, even on early return
    // ... rest of connectOnce
}
```

**SubsystemActor ingest worker:**
```go
// CURRENT: goroutine reads from queue.Pop() until queue.Close()
// PROBLEM: if actor.Stopped is delivered but queue.Close() is not called, goroutine hangs

// FIX: already correct — actor.Stopped calls queue.Close() which broadcasts to Pop()
// VALIDATION: add test that verifies goroutine count before/after subsystem lifecycle
```

**AggregationSubsystem consumeLoop:**
```go
// CURRENT: goroutine selects on ctx.Done() and channel read
// PROBLEM: if channel is never closed and ctx is never cancelled, goroutine hangs
// FIX: already correct — actor.Stopped cancels ctx; bus.Close() closes channel
// VALIDATION: add test
```

#### E.1.2 Timer Leak Prevention

**Regra:** Nenhum timer sem cancelSchedule.

```go
// Guardian: scheduledRetry map tracks all pending timers
// FIX: already correct — stopAll() iterates and cancels all scheduledRetry entries
// VALIDATION: add assertion in guardian_test that scheduledRetry is empty after stopAll()

// Manager: scheduledPoison map tracks overlap timers
// FIX: already correct — handleStopped() cancels all scheduledPoison entries
// VALIDATION: add assertion in manager_test
```

#### E.1.3 WebSocket Connection Lifecycle

**Shutdown choreography (ordered):**
```
1. Set shuttingDown / stopOnce
2. Close quitch channel (signals all goroutines)
3. Cancel context (signals ctx.Done() watchers)
4. Write websocket.CloseMessage with deadline
5. Close conn
6. Wait for goroutines to exit (via donech or ctx)
```

**Current implementation mostly follows this; formalize as invariant.**

#### E.1.4 Actor Restart Finalizer

```go
// PRINCIPLE: when subsystem restarts, all resources of old instance must be finalized
// Hollywood guarantees: actor.Stopped is delivered before new instance's actor.Started
// VALIDATION: add test that old Consumer.Stop() is complete before new Consumer spawns
```

#### E.1.5 Bounded State Maps

```go
// Proposed: generic LRU cache
type BoundedMap[K comparable, V any] struct {
    maxSize int
    ttl     time.Duration
    clock   clock.Clock
    items   map[K]*entry[V]
    order   *list.List // LRU ordering
}

// Usage in IngestMarketData:
type IngestMarketData struct {
    streams *ds.BoundedMap[string, *domain.InstrumentStream]
    // maxSize=10000, ttl=1h → evict streams not seen in 1h
}

// Usage in UpdateOrderBookFromEvents:
type UpdateOrderBookFromEvents struct {
    books *ds.BoundedMap[string, *domain.OrderBook]
    // maxSize=10000, ttl=1h
}
```

#### E.1.6 Instrumentacao

```go
// Expose via /metrics:
process_goroutines          gauge   // runtime.NumGoroutine()
process_heap_alloc_bytes    gauge   // runtime.MemStats.HeapAlloc
process_gc_pause_seconds    hist    // runtime.MemStats.PauseNs
ingest_streams_active       gauge   // len(streams)
aggregation_books_active    gauge   // len(books)
```

---

### E.2 Performance & Backpressure

#### E.2.1 Pipeline Stage Boundaries

```
WS recv → wsQueue (bounded, 1024) → ingestWorker → IngestMarketData → EventPublisher → Bus
                                                                                        ↓
                                                                            subscriber channels (bounded, 1024)
                                                                                        ↓
                                                                            AggregationSubsystem / DeliveryRouter
```

Each `→` is a bounded channel or queue with explicit capacity.

#### E.2.2 Drop Strategy per Stage

| Stage | Queue | Capacity | Drop Policy | Metric |
|-------|-------|----------|-------------|--------|
| WS → Subsystem | wsQueue | 1024 (config) | drop_depth_keep_trades | `backpressure_drops_total{policy}` |
| Bus → Subscriber | chan Envelope | 1024 (config) | drop newest (non-blocking send) | `bus_dropped_total{subscriber_id}` |
| Subsystem → Guardian (heartbeat) | engine.Send (unbounded mailbox) | N/A | No drop (low frequency) | N/A |

#### E.2.3 Priority Ordering

```go
// Trade events are highest priority; never dropped before depth events
// Priority: trade > bookdelta > markprice > liquidation > other
//
// Implementation in wsQueue.Enqueue():
// When full, scan for lowest-priority message and drop it
// If all messages are same priority, drop oldest
```

#### E.2.4 Allocation Reduction

**Current hot paths with allocation:**
1. `json.Unmarshal` in parser — allocates per message
2. `envelope.Envelope` creation — allocates per message
3. `codec.Marshal` payload — allocates per message

**Proposed optimizations (future, profiling-driven):**
- Pool `[]byte` buffers for JSON marshal/unmarshal (sync.Pool)
- Pre-allocate Envelope struct fields (avoid map allocation for Meta when unused)
- Consider CBOR/proto for smaller wire size (less alloc pressure)
- Avoid string conversions in hot path (use []byte comparisons)

**Rule: "Profile first, optimize second." No premature optimization.**

#### E.2.5 Benchmark Plan

```bash
# Micro-benchmarks (per-function)
go test -bench=BenchmarkIngest -benchmem ./internal/core/marketdata/app/
go test -bench=BenchmarkApplyDelta -benchmem ./internal/core/aggregation/domain/
go test -bench=BenchmarkParse -benchmem ./internal/adapters/exchange/binance/
go test -bench=BenchmarkEnqueue -benchmem ./internal/actors/marketdata/runtime/

# CPU profile
go test -bench=BenchmarkIngest -cpuprofile=cpu.prof ./internal/core/marketdata/app/
go tool pprof cpu.prof

# Heap profile
go test -bench=BenchmarkIngest -memprofile=heap.prof ./internal/core/marketdata/app/
go tool pprof heap.prof

# Trace
go test -bench=BenchmarkIngest -trace=trace.out ./internal/core/marketdata/app/
go tool trace trace.out
```

---

### E.3 Actor Topology & Supervision

#### E.3.1 Target Topology

```
Guardian (root supervisor)
│
├── MarketDataSubsystem["binance"]
│   ├── WS Manager (manages consumer pool)
│   │   ├── Consumer[bucket-0] (goroutines: readLoop, keepalive, heartbeat)
│   │   ├── Consumer[bucket-1]
│   │   └── Consumer[bucket-N]
│   └── IngestWorker (goroutine: pops from wsQueue, calls IngestMarketData)
│
├── MarketDataSubsystem["bybit"]  (future: one per exchange)
│   └── ... (same structure)
│
├── AggregationSubsystem
│   └── consumeLoop (goroutine: reads from bus channel)
│
├── DeliverySubsystem
│   └── RouterActor
│       ├── SessionActor[session-1] (per WS client)
│       ├── SessionActor[session-2]
│       └── SessionActor[session-N]
│
└── InsightsSubsystem (future)
    └── DetectorWorkers (future)
```

#### E.3.2 Supervision Strategy per Level

| Level | Actor | Restart Policy | Circuit Breaker |
|-------|-------|---------------|-----------------|
| Guardian | — | N/A (root, never restarts) | N/A |
| MarketDataSubsystem | Guardian restarts | 5 failures in 30s → degraded 30s | Yes (SupervisorPolicy) |
| WS Manager | Subsystem restarts | Part of subsystem restart | Inherits subsystem policy |
| WS Consumer | Manager rotates/respawns | MaxWebsocketLifetime rotation | Budget window (20 retries/min) |
| AggregationSubsystem | Guardian restarts | Same policy as MD | Yes |
| DeliverySubsystem | Guardian restarts | Same policy as MD | Yes |
| RouterActor | DeliverySubsystem restarts | Part of subsystem restart | Inherits |
| SessionActor | Self-poisons on WS close | No restart; client reconnects | N/A |

#### E.3.3 Error Classification

```go
// Transient (no escalation, local retry):
//   WS dial failure, read timeout, ping/pong failure, heartbeat failure
//   → Consumer retries internally with backoff

// Escalatable (subsystem-level restart):
//   Unknown WS error, bus closure, config error
//   → ChildFailed to Guardian → policy decides restart vs degrade

// Fatal (process exit):
//   Config validation failure (startup only)
//   → os.Exit(1) before any actor spawn

// Domain (log + skip):
//   Out-of-order sequence, duplicate, parse error, validation failure
//   → Metric + log, no restart needed
```

#### E.3.4 Restart Storm Prevention

```go
// Global rate limiter in Guardian:
type restartRateLimiter struct {
    window    time.Duration  // 1 minute
    maxPerWin int            // 3 subsystem restarts total
    history   []time.Time
}

func (r *restartRateLimiter) Allow() bool {
    // Prune history outside window
    // Return len(history) < maxPerWin
}

// If rate limiter denies: all pending restarts deferred until window expires
// This prevents cascading restart storms across subsystems
```

#### E.3.5 No Double-Publish Guarantee

```go
// On subsystem restart, IngestMarketData streams map is lost (in-memory)
// With JetStream: NATS dedup window prevents duplicate publish (IdempotencyKey → Msg-ID)
// Without JetStream (InMemoryBus): no guarantee — InMemoryBus is ephemeral anyway
//
// Design choice: rely on NATS dedup for production; accept potential duplicates with InMemoryBus
// Consumers must be idempotent regardless (defense in depth)
```

---

### E.4 Stream Partitioning

#### E.4.1 NATS Subject Hierarchy

```
# Pattern:
{context}.{event_type}.v{version}.{venue}.{instrument}

# Examples:
marketdata.trade.v1.binance.BTCUSDT
marketdata.bookdelta.v1.binance.ETHUSDT
marketdata.markprice.v1.binance.BTCUSDT
aggregation.snapshot.v1.binance.BTCUSDT
insights.divergence.v1.global.BTCUSDT

# Wildcard subscriptions:
marketdata.trade.v1.*.BTCUSDT          # All venues, one instrument
marketdata.*.v1.binance.*               # All event types, one venue
marketdata.trade.v1.binance.*           # All instruments, one venue
```

#### E.4.2 Partition Key

```go
// Partition key = (venue, instrument)
// All events for one (venue, instrument) pair go to the same NATS subject
// This guarantees ordering within a partition
//
// JetStream stream config:
// - Stream name: "MARKETDATA"
// - Subjects: "marketdata.>"
// - Retention: Limits (MaxAge=24h, MaxBytes=10GB)
// - Storage: File
// - Replicas: 1 (single node for now)
// - Dedup window: 5 minutes
```

#### E.4.3 Consumer Groups for Parallelism

```go
// For aggregation processing:
// - One durable consumer per (venue, instrument) pair ensures ordering
// - OR: single consumer with MaxAckPending=1 (slower but simpler)
//
// Recommendation: start with single consumer, profile, then partition
// Partitioning formula: consistent hash of (venue, instrument) → N workers
//
// Example with 4 workers:
// worker-0: hash(binance, BTCUSDT) % 4 = 0
// worker-1: hash(binance, ETHUSDT) % 4 = 1
// ...
```

#### E.4.4 Arbitrage Cross-Venue Joins

```go
// Future: arbitrage consumer subscribes to:
//   marketdata.trade.v1.*.BTCUSDT
//   marketdata.bookdelta.v1.*.BTCUSDT
//
// This delivers events from ALL venues for one instrument
// Consumer must handle interleaving and build cross-venue view
//
// Invariant: canonical instrument (BASE-QUOTE) must be identical across venues
// This is enforced by naming.CanonicalInstrument normalization at adapter boundary
```

---

### E.5 Deterministic Replay

#### E.5.1 Invariantes

1. **Seq monotonica por streamID:** `seq(n+1) > seq(n)` para todo `(venue, instrument)`.
2. **Timestamp normalization:** `TsIngest` = autoridade local (nossa clock); `TsExchange` = advisory (clock da exchange). Em replay, `TsIngest` vem da fixture.
3. **Dedup keys deterministicos:** `hash.HashFields(venue, instrument, eventType, seq)` e puro — mesmo input, mesmo key.
4. **Clock port:** dominio nunca chama `time.Now()`; sempre `clock.Now()`.
5. **Sequencer port:** em replay, sequencer retorna seq da fixture (nao gera novo).

#### E.5.2 Replay Architecture

```go
// Fixture format: JSON-lines (one envelope per line)
// Each line: complete Envelope JSON including all metadata
//
// Recording:
type Recorder struct {
    inner   ports.EventPublisher  // real publisher
    writer  *bufio.Writer         // fixture file
    mu      sync.Mutex
}

func (r *Recorder) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem {
    // 1. Write envelope to fixture file (append)
    // 2. Forward to real publisher
}

// Replaying:
type Player struct {
    fixtures []envelope.Envelope  // loaded from file
    clock    *clock.FakeClock
    seq      map[string]int64     // replay sequencer: returns fixture seq
}

func (p *Player) Play(ingest *app.IngestMarketData) []envelope.Envelope {
    var output []envelope.Envelope
    for _, env := range p.fixtures {
        p.clock.Set(time.UnixMilli(env.TsIngest))
        // Convert envelope back to IngestRequest
        // Call ingest.Execute()
        // Capture output envelope
    }
    return output
}
```

#### E.5.3 Replay Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| **Full replay** | Reprocess all events from start | System rebuild, testing |
| **Catchup window** | Reprocess from `now - duration` | Recovery after downtime |
| **Golden test** | Replay fixture, compare output | CI validation |

#### E.5.4 Schema Version Handling in Replay

```go
// Problem: fixture may contain envelopes with version N-1
// Solution: codec.Registry maps (EventType, Version) → Decoder
// Replay must register decoders for all historical versions
//
// Rule: codec registry is append-only; old decoders never removed
// This ensures any fixture from any point in time can be replayed
```

---

### E.6 Protobuf Contract Layer

#### E.6.1 Directory Organization

```
proto/
├── buf.yaml                    # Buf workspace config
├── buf.gen.yaml                # Code generation config
├── buf.lock                    # Dependency lock
├── registry.json               # Schema manifest
├── envelope/
│   └── v1/
│       └── envelope.proto      # Envelope wire format
└── marketdata/
    └── v1/
        ├── trade.proto         # TradeTickV1
        ├── bookdelta.proto     # BookDeltaV1
        ├── markprice.proto     # MarkPriceTickV1
        └── liquidation.proto   # LiquidationTickV1
```

#### E.6.2 Schema Examples

```protobuf
// proto/envelope/v1/envelope.proto
syntax = "proto3";
package envelope.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/envelope/v1";

message Envelope {
  string type = 1;              // event type (e.g., "marketdata.trade")
  int32 version = 2;            // payload schema version
  string venue = 3;             // canonical venue (e.g., "BINANCE")
  string instrument = 4;        // canonical instrument (e.g., "BTCUSDT")
  int64 ts_exchange = 5;        // Unix ms (exchange clock, advisory)
  int64 ts_ingest = 6;          // Unix ms (our clock, authoritative)
  int64 seq = 7;                // monotonic per (venue, instrument)
  string idempotency_key = 8;   // deterministic dedup key
  map<string, string> meta = 9; // optional metadata
  bytes payload = 10;           // versioned domain payload
  string content_type = 11;     // "application/json" | "application/protobuf"
}

// proto/marketdata/v1/trade.proto
syntax = "proto3";
package marketdata.v1;
option go_package = "github.com/market-raccoon/internal/shared/proto/gen/marketdata/v1";

message TradeTickV1 {
  double price = 1;
  double size = 2;
  string side = 3;              // "buy" | "sell"
  string trade_id = 4;
  int64 timestamp_ms = 5;
}

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

#### E.6.3 Buf Configuration

```yaml
# proto/buf.yaml
version: v2
modules:
  - path: .
lint:
  use:
    - STANDARD
    - COMMENTS
  except:
    - PACKAGE_VERSION_SUFFIX
breaking:
  use:
    - WIRE_JSON
```

```yaml
# proto/buf.gen.yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: ../internal/shared/proto/gen
    opt: paths=source_relative
```

#### E.6.4 Compatibility Rules

1. **Never reuse field numbers** — use `reserved` for removed fields
2. **Never change field types** — add new field with new number instead
3. **Never rename fields** — wire format uses numbers, not names, but JSON mapping would break
4. **`oneof` restrictions** — never add fields to existing `oneof`; create new `oneof` instead
5. **Backward compatibility** — new code must read old messages; old code must not crash on new messages
6. **`buf breaking --against .git#branch=main`** runs in CI on every PR

#### E.6.5 Migration Strategy

```
Phase 1 (W6): Define schemas, generate code, add buf to CI
              Runtime: still JSON only
              Duration: 1 week

Phase 2 (W7): Add content_type field to Envelope
              Codec supports both JSON and proto based on content_type
              Producers default to JSON
              Duration: part of JetStream integration

Phase 3 (post-W7): Opt-in proto publishing (config flag)
                   Consumers auto-detect format from content_type
                   Duration: 1 week

Phase 4 (post-W9): JSON deprecated for bus traffic
                   External HTTP API remains JSON
                   Duration: gradual
```

#### E.6.6 Schema Registry Lite

```json
// proto/registry.json
{
  "schemas": [
    {
      "type": "marketdata.trade",
      "version": 1,
      "proto_file": "marketdata/v1/trade.proto",
      "message": "marketdata.v1.TradeTickV1",
      "status": "stable"
    },
    {
      "type": "marketdata.bookdelta",
      "version": 1,
      "proto_file": "marketdata/v1/bookdelta.proto",
      "message": "marketdata.v1.BookDeltaV1",
      "status": "stable"
    },
    {
      "type": "envelope",
      "version": 1,
      "proto_file": "envelope/v1/envelope.proto",
      "message": "envelope.v1.Envelope",
      "status": "stable"
    }
  ]
}
```

---

## F) Checklist de Validacao & Criterios de "Production Ready"

### F.1 Soak Test (Mandatory)

| Test | Duration | Config | Pass Criteria |
|------|----------|--------|---------------|
| 2 tickers (BTC+ETH) | 30 min | Binance WS, InMemoryBus | goroutines stable, heap stable, 0 internal gaps |
| 200+ tickers | 60 min | Binance WS, InMemoryBus | goroutines stable (±5), heap growth < 10%, drop rate < 0.1% |
| 200+ tickers + JetStream | 60 min | Binance WS, JetStream | Same as above + 0 message loss (consumer count = producer count) |
| Reconnect stress | 30 min | Force disconnect every 60s | Recovery < 5s, 0 goroutine leaks per cycle |

### F.2 Race Detection

```bash
# All modules
go test -race -count=3 ./...

# Critical modules (longer timeout)
go test -race -timeout=5m ./internal/actors/...
go test -race -timeout=5m ./internal/core/marketdata/...
go test -race -timeout=5m ./internal/adapters/...
```

### F.3 Profiling Validation

| Profile | Tool | Pass Criteria |
|---------|------|---------------|
| Heap | `go tool pprof http://localhost:8080/debug/pprof/heap` | Alloc rate stabilizes within 5min; no unbounded growth |
| Goroutines | `go tool pprof http://localhost:8080/debug/pprof/goroutine` | Count matches expected (baseline + N consumers + M sessions) |
| CPU | `go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30` | Top functions are expected (JSON parse, hash, network I/O) |
| Mutex | `go tool pprof http://localhost:8080/debug/pprof/mutex` | No contention > 1% on any single mutex |

### F.4 Metricas Minimas

All metrics from B.5 must be:
- [ ] Registered and emitting values
- [ ] Scrapable from `/metrics`
- [ ] Non-zero after 5 minutes of operation (except error counters)

### F.5 Shutdown Graceful

```bash
# Test: send SIGTERM, measure time to exit, check goroutine leak
# 1. Start process
# 2. Wait for /readyz → 200
# 3. Send SIGTERM
# 4. Measure time to process exit
# 5. Verify: exit code 0, time < 10s, final log "shutdown complete"
# 6. Verify: no "goroutine leak" in logs (if leak detector enabled)
```

### F.6 Replay Deterministico

```bash
# 1. Record fixture from live stream (1000 envelopes)
go run cmd/consumer -record=testdata/fixtures/binance-1000.jsonl -tickers=BTCUSDT -duration=5m

# 2. Replay and compare
go test -run TestGoldenReplay -golden=testdata/fixtures/binance-1000.jsonl ./internal/shared/replay/
# Pass: output matches golden file byte-for-byte
```

### F.7 Schema Validation

```bash
# Lint
buf lint proto/

# Breaking change detection
buf breaking proto/ --against .git#branch=main

# Golden test: encode/decode roundtrip
go test -run TestProtoRoundtrip ./internal/shared/codec/
```

---

## G) Matriz Risco x Mitigacao

| # | Risco | Probabilidade | Impacto | Mitigacao | RFC |
|---|-------|--------------|---------|-----------|-----|
| R1 | Goroutine leak em WS consumer | Media | Alto | Lifecycle audit, leak detector test, goroutine metric | W5 |
| R2 | Restart storm (cascading failures) | Baixa | Alto | Global restart rate limiter, SupervisorPolicy already has backoff | W5 |
| R3 | Duplicate publish apos restart | Media | Medio | NATS Msg-ID dedup + IdempotencyKey; consumer idempotency | W7 |
| R4 | Sequence gaps nao detectados | Baixa | Alto | Depth gap detection (ja impl), seq monotonicity check, gap metric | W4 |
| R5 | Silent data loss (bus drops) | Alta | Alto | Drop counter metric, alerting on drops > 0, JetStream durability | W4, W7 |
| R6 | Memory growth (unbounded maps) | Alta | Medio | LRU eviction, max levels, heap metric + soak test | W5 |
| R7 | Schema breaking change undetected | Media | Alto | buf breaking in CI, golden tests, schema registry | W6 |
| R8 | Overload/backpressure cascade | Baixa | Medio | Bounded queues at every stage, priority drops, load shed metric | W5 |
| R9 | Thundering herd on reconnect | Baixa | Medio | Per-consumer backoff (ja impl), global reconnect rate limiter | W5 |
| R10 | Replay non-determinism | Media | Alto | Clock port, deterministic sequencer, golden test validation | W8 |
| R11 | Multi-exchange normalization bugs | Media | Medio | Canonical naming tests, cross-venue integration test | W9 |
| R12 | JetStream unavailability | Baixa | Alto | Fallback to InMemoryBus (flag), health check, auto-reconnect | W7 |

---

## H) Plano de Instrumentacao

### H.1 Endpoints

| Endpoint | Tipo | Protecao | Descricao |
|----------|------|----------|-----------|
| `/healthz` | HTTP GET | Publico | Liveness probe (200 se processo vive) |
| `/readyz` | HTTP GET | Publico | Readiness probe (200 se subsystems ready) |
| `/metrics` | HTTP GET | Publico (Prometheus) | Metricas Prometheus exposition format |
| `/debug/pprof/` | HTTP GET | Localhost-only ou auth | Standard Go pprof (heap, goroutine, cpu, mutex, block, trace) |
| `/runtime/snapshot` | HTTP GET | Publico | Guardian state snapshot (JSON) |
| `/runtime/reload` | HTTP POST | Auth required | Trigger config reload |

### H.2 Logging Strategy

| Level | Quando | Exemplo |
|-------|--------|---------|
| ERROR | Operator action needed | "failed to connect to JetStream after 5 retries" |
| WARN | Degraded but auto-recovering | "entering backpressure mode", "depth gap detected" |
| INFO | Lifecycle events | "subsystem started", "config loaded", "shutdown complete" |
| DEBUG | Per-message detail | "published envelope", "parsed trade tick" |

**Sampling:** high-frequency events (parse errors, skips) sampled at 1 per 30s per key (ja implementado).

### H.3 Metricas por Componente

| Componente | Metricas | Tipo |
|------------|----------|------|
| IngestMarketData | messages_total, latency_hist, streams_active | counter, histogram, gauge |
| BackpressureQueue | queue_depth, drops_total, entered_total | gauge, counter, counter |
| WS Consumer | connections_active, reconnects_total, errors_total, messages_received | gauge, counter, counter, counter |
| WS Manager | streams_active, rotations_total | gauge, counter |
| InMemoryBus | published_total, dropped_total, subscribers_active | counter, counter, gauge |
| Guardian | restarts_total, degraded_total, subsystem_state | counter, counter, gauge |
| Delivery Router | sessions_active, subscriptions_total, events_routed_total | gauge, gauge, counter |
| Process | goroutines, heap_alloc, gc_pauses | gauge, gauge, histogram |

---

## Appendix: Crossref com ADRs/RFCs Existentes

| Documento Existente | Referenciado em | Acao |
|---------------------|-----------------|------|
| ADR-0000 (Architecture Principles) | C.1, E.3 | No change needed |
| ADR-0001 (Go + DDD) | C.1 | No change needed |
| ADR-0002 (Envelope Design) | C.1, E.6 | REVISAR: add wire format, schema discovery |
| ADR-0003 (Actor Runtime) | C.1, E.3 | REVISAR: add lifecycle model, request pattern |
| ADR-0004 (JetStream) | C.1, E.4 | REVISAR: add concrete config before W7 |
| ADR-0005 (Sequencing) | C.1, E.5 | REVISAR: add persistent seq, replay interaction |
| ADR-0006 (Observability) | B.5, H | REVISAR: expand with concrete metrics |
| ADR-0007 (Delivery WS) | D.5 | No change needed |
| ADR-0008 (Insights Non-Directive) | N/A | No change needed |
| ADR-0009 (Config) | C.1 | REVISAR: add multi-exchange, secrets |
| ADR-0010 (Config JSONC) | N/A | No change needed |
| ADR-0011 (Binance Mapping) | C.1, D.6 | REVISAR: add market type, dynamic discovery |
| RFC-0001 (Roadmap W1/W2/W3) | D | ATUALIZAR: extend with W4-W9 |
| RFC-0002 (W1 Config) | N/A | COMPLETO, no change |
| RFC-0003 (W2 Delivery) | N/A | COMPLETO, no change |
| RFC-0004 (W3 Binance) | N/A | COMPLETO, no change |
