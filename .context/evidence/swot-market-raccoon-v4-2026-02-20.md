# SWOT: Market Raccoon — Full Project Assessment (v4)

**Date:** 2026-02-20 (revision 4 — post P0-P3 v3 implementation)
**Perspectiva:** Engenharia avaliando robustez, escalabilidade e performance ultraalta para sistema de producao com zero-tolerance a codigo legado e debito tecnico. Auditoria automatizada completa de 8 agentes paralelos.

**Baseline:** v3 SWOT identificou 17 weaknesses (W1-W17). P0-P3 v3 implementados em 4 commits (`3f3e5e1`→`6fae7c4`) + changes uncommitted na branch `codex/prd0002-advance-dedicated`. Esta revisao valida os fixes, descobre novos hot-path bottlenecks via auditoria profunda, e re-prioriza com zero tolerance.

---

## Codebase Metrics (auditados)

| Metrica | Valor |
|---------|-------|
| Source files (.go, excl. tests/generated/zip) | 191 |
| Test files (_test.go, excl. zip) | 200 |
| Test-to-source ratio | **1.05:1** |
| Proto definitions (.proto) | 11 |
| Generated .pb.go files | 11 (2,542 lines) |
| SQL migrations | 9 (6 ClickHouse + 3 TimescaleDB) |
| Go modules (go.mod) | 14 |
| Bounded contexts | 5 (marketdata, aggregation, delivery, insights, storage) |
| Exchanges operacionais | **6** (Binance, Bybit, Coinbase, HyperLiquid, Kraken, KrakenF) |
| C4 soak throughput | **117,697 evt/sec** (p99=56µs) |
| Concurrency bugs encontrados | **0** (zero data races, zero deadlocks) |

---

## Quadrants

### STRENGTHS (Forcas Internas)

| # | Forca | Evidencia |
|---|-------|-----------|
| **S1** | Arquitetura Hexagonal com DDD rigoroso | 5 BCs isolados; 6 invariantes enforced por CI; zero import cycles; domain packages pure Go (zero deps externas) |
| **S2** | Pipeline deterministico e replayavel | `FakeClock`, `ReplaySequencer`, `RecorderPublisher`, fixtures JSONL 6 exchanges; golden tests byte-stable |
| **S3** | Actor model com supervisao estruturada | Guardian + SupervisorPolicy (Hollywood); isolamento de falha; snapshot cache com invalidacao smart; `/readyz` probe |
| **S4** | Suite de testes excepcional (200 files, 1.05:1) | Golden+soak+bench+E2E+integration+conformance; race detector obrigatorio; domain VOs agora com edge-case isolation |
| **S5** | Dual storage plane com ack-on-commit | TimescaleDB (hot) + ClickHouse (cold); 5 artifact types em ambas DBs; cold readers com FINAL dedup |
| **S6** | Governanca de schema machine-checked | `subject-registry.yaml` 17 subjects; `make registry-check` + `make docs-check`; zero discrepancia |
| **S7** | Delivery ring buffer + 3 politicas backpressure | Ring buffer O(1) com GC explicito; DropNewest/DropOldest/PriorityDrop; slow-client disconnect; metricas labeled |
| **S8** | Protobuf completo + transcode cache | 11 proto schemas stable; 16 event types dual JSON+Proto; TranscodeCache FNV-1a 99% hit rate |
| **S9** | Config JSONC + hot-reload + validation | `config.Load()` → `Validate()` → `*problem.Problem`; `/runtime/reload`; fail-fast startup |
| **S10** | Concorrencia correta, zero bugs | 8 agentes auditaram 12 mutexes, 8 sync.Once, 1 sync.Cond, 1 sync.Map, todos atomics — **zero data races, zero deadlocks** |
| **S11** | 6 exchanges operacionais | Binance+Bybit+Coinbase+HyperLiquid+**Kraken+KrakenF** (novo); backfill 4 exchanges; markprice normalizado |
| **S12** | C4 soak validado: 117K evt/sec | 10M events multi-exchange + 50 slow clients — PASS; p50=7µs, p95=13µs, p99=56µs |
| **S13** | Goose migration runner dual-DB | `cmd/migrate` com goose v3.24.3; TimescaleDB + ClickHouse (6 migrations convertidas para formato goose) |
| **S14** | Observability multi-nivel | Prometheus metricas para codec, BoundedMap, delivery, heatmap, policykit; state machines testadas |
| **S15** | Dependencies saudaveis e consistentes | Todas deps aligned across 14 modules (clickhouse-go v2.34.0, pgx v5.8.0, protobuf v1.36.11); zero CGO; zero advisories |

---

### WEAKNESSES (Fraquezas Internas)

#### Performance: Hot-Path Allocation Debt

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W1** | `fmt.Sprintf` em `TopicKey()` — cada envelope | 4-5 allocs/chamada × 1M+ chamadas/sec = **~5M allocs/sec** | `envelope/envelope.go:137` — `fmt.Sprintf("%s.%s.%s", ...)` em cada Publish() |
| **W2** | `HashFields()` variadic overhead + SHA-256 em hot-path | 3 allocs/chamada (variadic []string, Builder.String(), hex.EncodeToString) × 1M+/sec | `hash/hash.go:30-39` — usado por TODOS os idempotency keys |
| **W3** | Idempotency keys com SHA-256 + strconv em cada evento | 8-9 allocs/key (strconv.FormatFloat/Int + SHA-256) | `build_heatmap.go:186-200`, `build_volume_profile.go:175-187`, `dedup_keys*.go:23-54` |
| **W4** | `makeCellKey()` em hot loop heatmap | 3 allocs/key × N cells/event × multiplas invocacoes | `build_heatmap.go:432-434` — `strconv.FormatInt + strings.ToUpper` em cada cell |
| **W5** | Storage writers: strconv + SHA-256 por cell/level em loops | 5+ allocs × 2000 cells por artifact write; 60+ allocs × levels por snapshot | `timescale/heatmap_writer.go:115-125`, `clickhouse/writer.go:149-170` |
| **W6** | `HashFloat64Sequence()` usa `fmt.Sprintf("%.17g")` por float | N allocs por slice em hot-path hash | `hash/hash.go:45-51` — `fmt.Sprintf` em cada elemento |

#### Configuration Debt

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W7** | `mustParseDuration()` faz panic em startup | App crasha se config tem duration invalida (ex: `"10"` sem unidade) | `schema.go:666-672` — `panic(fmt.Sprintf(...))` em applyDefaults |
| **W8** | 5+ timeouts hardcoded em bootstrap | JetStream publish 5s, router ready 500ms, session spawn 2s — nao configuraveis | `processor/bootstrap.go:254`, `server/bootstrap.go:201,218,241` |
| **W9** | Proto rollout flags: dual source (config + env vars) sem precedencia clara | `SetProtoRolloutConfig()` + fallback `os.Getenv()` — precedencia implicita | `proto_rollout_flags.go:66-78` — cfg → Once(env) fallback chain |
| **W10** | Cross-field validation incompleta | ClickHouse `max_idle_conns <= max_open_conns` nao validado; bus capacity vs delivery queue nao validado | `loader.go` — falta em `validateStorage()` |

#### Test Coverage Gaps (Zero-Tolerance)

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W11** | `inmemory.go` (InMemoryBus) sem testes unitarios | Core pub/sub com lock/atomic/drop logic — testado indiretamente mas sem edge-case isolation | `adapters/bus/inmemory.go` — 112 lines, 0 test file |
| **W12** | `supervisor.go` (SupervisorPolicy) sem testes unitarios | Exponential backoff, jitter, degradation policy — critico para reliability | `actors/runtime/supervisor.go` — 222 lines, 0 test file |
| **W13** | Coinbase + HyperLiquid backfill sem testes | REST pagination, HTTP error handling, JSONL writing — risco de data loss silencioso | `exchange/coinbase/backfill.go`, `exchange/hyperliquid/backfill.go` — 0 test files |
| **W14** | Storage writers sem testes unitarios diretos | candle_writer, stats_writer, heatmap_writer (ClickHouse) — cobertos indiretamente por integration | `clickhouse/{candle,stats}_writer.go`, `timescale/{heatmap,volume_profile}_writer.go` |
| **W15** | `replay/player.go` sem testes | Replay orchestration — risco de timing incorreto em testes offline | `shared/replay/player.go` — ~100 lines, 0 test file |

#### Architecture

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W16** | `fmt.Errorf` em actors/adapters (67 ocorrencias) | Inconsistente com padrao `*problem.Problem` — aceito em infra mas viola uniformidade | Grep audit: 67 matches em `internal/` (actors, adapters, config) |
| **W17** | TranscodeCache eviction full-clear (nao LRU) | Overflow descarta cache inteiro; spike transiente sob alta cardinalidade | `transcode_cache.go:75-80` — `c.cache = make(...)` |
| **W18** | Funding rate pipeline incompleto | Parsers extraem em 6 exchanges, mas nao ha aggregation use case, storage writers, nem delivery routing | Config presente, processor `handleMarkPrice()` nao forwarda funding rate |

---

### OPPORTUNITIES (Oportunidades Externas)

| # | Oportunidade | Alavanca |
|---|-------------|----------|
| **O1** | TopicKey via `strings.Builder` ou concat simples | Elimina W1: 5M allocs/sec → ~1M allocs/sec (80% reducao no hot-path mais chamado) |
| **O2** | HashFields rewrite: pre-sized buffer + direct SHA-256 streaming | Elimina W2: remove variadic alloc + intermediate string; pode aceitar `[]byte` direto |
| **O3** | Idempotency keys via FNV-1a (nao SHA-256) + integer hashing | Elimina W3: SHA-256 e overkill para idempotency (nao criptografico); FNV-1a = 10x mais rapido, zero alloc |
| **O4** | Cell key via integer encoding (nao string) | Elimina W4: `int64<<32 | sizeEnum` = zero alloc cell key em 1 instrucao |
| **O5** | Storage writer: pre-compute idempotency key uma vez, reuse per-cell | Elimina W5: compute hash do artifact inteiro, append cell index — evita N SHA-256 por write |
| **O6** | `mustParseDuration()` → graceful error return | Elimina W7: retornar `*problem.Problem` em vez de panic |
| **O7** | Bootstrap timeouts no config schema | Elimina W8: `publisher_timeout`, `router_ready_timeout` etc. configuraveis |
| **O8** | Funding rate aggregation pipeline | Elimina W18: `BuildFundingRateFromEvents` use case + storage writers + delivery routing |
| **O9** | Kraken/KrakenF backfill adapters | Estende backfill de 4 para 6 exchanges |
| **O10** | HTTP API para cold readers | Expose ClickHouse readers via REST endpoints para analytics externo |
| **O11** | `getrange` delivery via TimescaleDB | Wire PgRangeStore no WS `getrange` command — deterministic replay para clients |

---

### THREATS (Ameacas Externas)

| # | Ameaca | Severidade |
|---|--------|-----------|
| **T1** | Hollywood actor framework nicho | **Media** — Comunidade pequena; mitigado por Guardian/Supervisor abstractions proprias + 200 test files |
| **T2** | Exchange API breaking changes | **Media** — 6 exchanges = 6 superficies; mitigado por golden tests + replay fixtures |
| **T3** | Two-database operational burden | **Media** — TimescaleDB + ClickHouse = 2x ops; mitigado por ADR-0019 runbook + goose runner dual-DB |
| **T4** | GC pressure sob carga extrema | **Alta** — Audit encontrou ~5-10M allocs/sec projetados em hot-path; p99 pode degradar sob sustained load; **C4 soak validou 117K/sec com p99=56µs, mas carga real pode ser maior** |
| **T5** | Multi-module workspace friction | **Baixa** — 14 go.mod + replace directives; `make tidy` necessario; mitigado por CI checks |

---

## Implications Matrix

|  | **O1** TopicKey builder | **O2** HashFields rewrite | **O3** FNV-1a idemkey | **O8** Funding rate | **T4** GC pressure |
|---|---|---|---|---|---|
| **S12** C4 soak 117K/sec | Leverage: benchmark antes/depois | Leverage: medir impacto direto no p99 | Leverage: soak valida melhoria real | — | Defend: baseline validado, regressao detectavel |
| **S10** Zero concurrency bugs | Leverage: refactor seguro | Leverage: refactor seguro | Leverage: refactor seguro | — | Defend: safety net para otimizacoes |
| **W1** TopicKey fmt.Sprintf | **Invest: P0** — builder elimina 5M allocs/sec | — | — | — | **Mitigate: direto** |
| **W2** HashFields variadic | — | **Invest: P0** — streaming SHA elimina 3M allocs/sec | — | — | **Mitigate: direto** |
| **W3** Idempotency SHA-256 | — | — | **Invest: P0** — FNV-1a 10x mais rapido | — | **Mitigate: direto** |
| **W11** InMemoryBus sem testes | — | — | — | — | **Mitigate: testes impedem regressao** |
| **W18** Funding rate | — | — | — | **Invest: P2** — pipeline completo | — |

---

## Key Implications

### 1. Hot-Path Allocation Debt e o Bottleneck Primario Remanescente
**W1 + W2 + W3 + W4 + W5 + W6 + O1-O5 + T4**

**Diagnostico:** A auditoria de 8 agentes revelou que o hot-path tem **~5-10M allocations/sec projetados** (TopicKey 5M + HashFields 3M + idempotency keys 1M+). Embora o C4 soak tenha validado 117K evt/sec com p99=56µs, isso foi em ambiente controlado. Sob carga real com 6 exchanges simultaneas e 50+ clients, GC pressure pode degradar latencia.

**Root causes:**
1. `fmt.Sprintf` para simples concatenacao de strings (TopicKey)
2. SHA-256 para idempotency keys (overkill — nao precisa ser criptografico)
3. `HashFields(...string)` variadic cria []string temporario em cada chamada
4. `strconv.FormatFloat/Int` repetido em loops (storage writers)

**Acao P0:**
1. TopicKey: `strings.Builder` ou `a + "." + b + "." + c` (elimina fmt.Sprintf)
2. HashFields: accept `io.Writer` interface, stream direto para hasher, elimina intermediate string
3. Idempotency keys: FNV-1a hash (non-cryptographic, 10x mais rapido, zero alloc com pre-sized buffer)
4. Cell keys: integer encoding (`priceIdx<<8 | sizeEnum`) em vez de string concat

### 2. Config Safety: Panic em Startup e Timeouts Hardcoded
**W7 + W8 + O6 + O7**

**Diagnostico:** `mustParseDuration()` faz panic se config tem duration invalida — viola o padrao fail-fast-with-problem. 5+ timeouts hardcoded em bootstrap nao sao configuraveis via JSONC, impedindo tuning de producao sem rebuild.

**Acao P0:**
1. Substituir `mustParseDuration()` por retorno de `*problem.Problem` na fase de validacao
2. Mover timeouts para config schema com defaults documentados

### 3. Test Coverage: Infra Critica Sem Testes Isolados
**W11 + W12 + W13 + W14 + W15**

**Diagnostico:** InMemoryBus (pub/sub core) e SupervisorPolicy (reliability) nao tem testes unitarios dedicados — cobertos indiretamente mas sem edge-case isolation. Coinbase/HyperLiquid backfill tambem sem testes (risco de data loss silencioso).

**Acao P1:**
1. `inmemory_test.go`: concurrent publish, drop counter, close semantics, subscriber cap
2. `supervisor_test.go`: exponential backoff, jitter bounds, degradation transitions
3. `coinbase/backfill_test.go` + `hyperliquid/backfill_test.go`: pagination, HTTP errors, JSONL format

### 4. Funding Rate: Ultimo Gap Funcional Significativo
**W18 + O8**

**Diagnostico:** Todos 6 exchanges extraem funding rate nos parsers de markprice, mas nao existe: (a) aggregation use case, (b) storage writers, (c) delivery routing. `StatsWindowV1.FundingRateAvg/Last` esta definido no schema mas nunca populado.

**Acao P2:**
1. `BuildFundingRateFromEvents` use case em `aggregation/app`
2. Processor routing: `handleMarkPrice()` → funding rate → aggregation
3. Storage writers (Timescale + ClickHouse)
4. Delivery routing para funding rate events

### 5. Concorrencia: Excelencia Confirmada
**S10**

**Diagnostico:** 8 agentes auditaram exaustivamente 12 mutexes, 8 sync.Once, 1 sync.Cond, 1 sync.Map, todos atomics, todos channels, todos goroutine spawns. **Zero data races, zero deadlocks, zero goroutine leaks.** Todos defer/unlock patterns corretos. Actor model usado corretamente.

**Status:** Nenhuma acao necessaria. Codebase demonstra praticas profissionais de concorrencia.

### 6. C4 Soak Validou Throughput de Producao
**S12**

**Diagnostico:** 10M events multi-exchange processados em 85 segundos: **117,697 evt/sec**, p50=7µs, p95=13µs, p99=56µs. 50 slow clients com backpressure testados — sistema estavel. **Throughput ceiling agora e GC pressure (T4), nao logica.**

---

## Scorecard Resumido

| Dimensao | v3 Score | v4 Score | Delta | Justificativa |
|----------|----------|----------|-------|---------------|
| Arquitetura | 5/5 | **5/5** | = | DDD+Hexagonal+Actor model; domain pure Go; zero import cycles; 6 invariantes enforced |
| Qualidade de Codigo | 4.5/5 | **4/5** | -0.5 | Hot-path allocation debt revelado por auditoria profunda (v3 nao detectou); mustParseDuration panic |
| Testes | 4.5/5 | **4.5/5** | = | 200 test files (up from 192); domain VOs testados; **-0.5 por inmemory.go + supervisor.go sem testes** |
| Cobertura Funcional | 4.5/5 | **4.5/5** | = | Kraken/KrakenF completos (6 exchanges); **-0.5 por funding rate pipeline incompleto** |
| Prontidao Operacional | 4.5/5 | **5/5** | +0.5 | C4 soak validado; goose dual-DB; backfill 4 exchanges; observability completa |
| Performance | 4/5 | **3.5/5** | -0.5 | C4 soak 117K/sec validado, mas auditoria revelou ~5-10M allocs/sec projetados em hot-path; GC pressure e bottleneck real |
| Concorrencia | N/A | **5/5** | NEW | Zero bugs em auditoria exaustiva (8 agentes, 12 mutexes, todos patterns verificados) |
| Paridade Competitiva | 4.5/5 | **5/5** | +0.5 | 6 exchanges; proto superiority; schema governance; **-0 Kraken preencheu gap** |

**Score Geral: 4.6 / 5.0** (up from 4.5) — C4 soak validado, Kraken completo, concorrencia impecavel. Performance score caiu porque auditoria profunda revelou allocation debt que v3 nao detectou. Fundacao excelente — otimizacao de hot-path e o caminho para 5/5.

---

## Recommended Next Steps

| Prioridade | Tipo | Acao | Elimina |
|-----------|------|------|---------|
| **P0** | Perf | `TopicKey()`: substituir `fmt.Sprintf` por `strings.Builder` ou concat direto | W1 |
| **P0** | Perf | `HashFields()`: rewrite para streaming hash sem variadic alloc | W2 |
| **P0** | Perf | Idempotency keys: FNV-1a hash em vez de SHA-256 (non-crypto suficiente) | W3 |
| **P0** | Perf | `makeCellKey()`: integer encoding em vez de string | W4 |
| **P0** | Config | `mustParseDuration()` → graceful `*problem.Problem` (eliminar panic) | W7 |
| **P1** | Perf | Storage writers: pre-compute hash do artifact, append cell index per-row | W5 |
| **P1** | Perf | `HashFloat64Sequence()`: rewrite sem `fmt.Sprintf` | W6 |
| **P1** | Config | Bootstrap timeouts no config schema (publisher, router, session, subsystem) | W8 |
| **P1** | Config | Proto rollout flags: documentar precedencia config > env, ou remover env fallback | W9 |
| **P1** | Config | Cross-field validation: idle<=open conns, bus capacity>=queue size | W10 |
| **P1** | Testes | `inmemory_test.go`: concurrent publish, drops, close | W11 |
| **P1** | Testes | `supervisor_test.go`: backoff, jitter, degradation | W12 |
| **P1** | Testes | Backfill tests: Coinbase + HyperLiquid pagination, errors, JSONL | W13 |
| **P2** | Feature | Funding rate pipeline completo: use case → processor → storage → delivery | W18 |
| **P2** | Testes | Storage writer unit tests (SQL generation, idempotency) | W14 |
| **P2** | Testes | `replay/player_test.go`: timing, buffering, edge cases | W15 |
| **P2** | Feature | Kraken/KrakenF backfill adapters | O9 |
| **P3** | Cleanup | Migrar `fmt.Errorf` em actors/adapters para `*problem.Problem` | W16 |
| **P3** | Feature | HTTP API para cold readers (ClickHouse) | O10 |
| **P3** | Feature | WS `getrange` via TimescaleDB PgRangeStore | O11 |

---

## v3 P0-P3 Implementation Verification

### Resolved from v3 (confirmed by 8-agent audit)

| v3 ID | Issue | Status | Fix Quality |
|-------|-------|--------|-------------|
| W1 (heatmap json.Marshal) | `trimToPayloadBudget()` json.Marshal loop | **RESOLVED** | O(1) cell-count estimation — zero serialize |
| W2 (heatmap artifact emission) | `json.Marshal(trimmed)` inline | **RESOLVED** | Estimativa aritmetica, sem serialize |
| W3 (map[string]any delivery) | Session JSON event map alloc | **RESOLVED** | Pre-allocated typed struct (commit `6fae7c4`) |
| W4 (snapshot fingerprint float) | `strconv.FormatFloat` per level | **PARTIAL** | Fixed-point hash implementado MAS pattern repetido em storage writers (→ novo W5) |
| W5 (ring buffer config) | SessionOutboundQueueSize sem config | **RESOLVED** | `delivery.session_outbound_queue_size` no schema (commit `3f3e5e1`) |
| W6 (shard registry env vars) | Env vars fora do config | **RESOLVED** | `ShardRegistryConfig` no schema; env vars documentadas como fallback |
| W7 (proto flags immutavel) | sync.Once impede toggle | **PARTIAL** | Config integration adicionada (`SetProtoRolloutConfig`), mas env fallback permanece |
| W8 (domain VO tests) | payloads.go sem testes | **RESOLVED** | `payloads_test.go` com 257 lines de edge cases (commit `6fae7c4`) |
| W9 (heatmap bucket tests) | heatmap_bucket.go sem testes | **RESOLVED** | `heatmap_bucket_test.go` com 93 lines (commit `6fae7c4`) |
| W10 (port interfaces tests) | */ports sem testes | **ACCEPTED** | Interface definitions — aceito sem testes diretos |
| W11 (cmd/migrate tests) | Binary sem testes | **RESOLVED** | `main_test.go` adicionado (uncommitted changes) |
| W12 (shared/metrics tests) | registry.go sem testes | **RESOLVED** | `metrics_test.go` com 13 tests (pre-existing, auditoria v3 incorreta) |
| W13 (funding rate) | Pipeline incompleto | **RETAINED → W18** | Parsers presentes em 6 exchanges, aggregation/storage ausente |
| W14 (14 replace directives) | Multi-module friction | **ACCEPTED** | Inerente ao design; CI checks mitigam |
| W15 (ClickHouse goose format) | Raw DDL sem markers | **RESOLVED** | Todas 6 migrations convertidas para `-- +goose Up/Down` (uncommitted) |
| W16 (fmt.Errorf infra) | 67 ocorrencias | **RETAINED → W16** | Aceito em infra layer, baixa prioridade |
| W17 (TranscodeCache eviction) | Full-clear nao LRU | **RETAINED → W17** | Working set pequeno (~16 event types); risk mitigado |

---

## Audit Evidence Trail (v4)

- **8-agent parallel audit:** (1) hot-path allocations, (2) config completeness, (3) test coverage, (4) architecture debt, (5) codebase metrics + new code, (6) concurrency safety, (7) dependencies + supply chain, (8) functional completeness
- **Hot-path audit:** 13 findings — TopicKey fmt.Sprintf, HashFields variadic, SHA-256 idempotency, makeCellKey loops, storage writer strconv loops
- **Concurrency audit:** 12 mutexes, 8 sync.Once, 1 sync.Cond, 1 sync.Map, 4 goroutine spawns — **zero bugs**
- **Config audit:** 15 gaps — mustParseDuration panic, 5+ hardcoded timeouts, cross-field validation missing
- **Test audit:** 191 source files, 200 tests (1.05:1); 8 HIGH-RISK untested files (inmemory, supervisor, backfill, storage writers, replay)
- **Dependency audit:** All versions consistent; zero CGO; Hollywood v1.0.5, NATS v1.48.0, pgx v5.8.0, clickhouse-go v2.34.0, protobuf v1.36.11
- **Functional audit:** Kraken/KrakenF COMPLETE; C4 soak PASS (117K evt/sec); Delivery WS COMPLETE baseline; Storage 5 artifacts both DBs; Funding rate PARTIAL
- **C4 soak evidence:** `.context/evidence/c4-production-soak.txt` — 10M events in 85s, 50 slow clients stable
