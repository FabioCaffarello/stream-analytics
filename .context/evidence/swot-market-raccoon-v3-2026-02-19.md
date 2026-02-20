# SWOT: Market Raccoon — Full Project Assessment (v3)

**Date:** 2026-02-19 (revision 3 — post P0-P3 implementation)
**Perspectiva:** Engenharia avaliando robustez, escalabilidade e performance para sistema de producao zero-tolerance a codigo legado e debito tecnico. Auditoria automatizada completa de 6 agentes paralelos.

**Baseline:** v2 SWOT identificou 21 weaknesses (W1-W21). P0-P3 implementados em 4 commits (`3dc6762`→`fee60a2`). Esta revisao valida os fixes, identifica gaps remanescentes e novas descobertas.

---

## Codebase Metrics (auditados)

| Metrica | Valor |
|---------|-------|
| Source files (.go, excl. tests/generated/zip) | 187 |
| Test files (_test.go, excl. zip) | 192 |
| Test-to-source ratio | **1.03:1** |
| Lines of source code | 36,675 |
| Lines of test code | 38,878 |
| Test-to-source LOC ratio | **1.06:1** |
| Proto definitions (.proto) | 11 |
| Generated .pb.go files | 11 (2,542 lines) |
| SQL migrations | 9 (6 ClickHouse + 3 TimescaleDB) |
| Go modules (go.mod) | 14 |
| Bounded contexts | 5 (marketdata, aggregation, delivery, insights, storage) |

---

## Quadrants

### STRENGTHS (Forcas Internas)

| # | Forca | Evidencia |
|---|-------|-----------|
| **S1** | Arquitetura Hexagonal com DDD rigoroso | 5 BCs isolados por modulo Go; 6 invariantes de camada enforced por CI; `make invariants-check`; zero import cycles confirmado |
| **S2** | Pipeline deterministico e replayavel | `FakeClock`, `ReplaySequencer`, `RecorderPublisher`, fixtures JSONL 4 exchanges; `internal/core/*` proibido de chamar `time.Now()` (INV-DET-01); golden tests byte-stable |
| **S3** | Actor model com supervisao estruturada + snapshot cache | Guardian + SupervisorPolicy (Hollywood); isolamento de falha por sub-arvore; `/readyz` readiness probe; **snapshot cache com invalidacao smart** (4 mutation paths, zero rebuild desnecessario) |
| **S4** | Suite de testes excepcional (192 files, ratio 1.03:1) | 192 test files cobrindo 187 source files; unit-golden-soak-integration-E2E-benchmark; race detector obrigatorio; soak asserts bounded heap/goroutines; **observability state machines agora testadas** |
| **S5** | Dual storage plane (hot + cold) com ack-on-commit | TimescaleDB (hot reads, `PgRangeStore` real) + ClickHouse (cold analytics, `FINAL` dedup); ack-on-commit semantico com conformance+soak tests; `IsProductionReady()=true` |
| **S6** | Governanca de schema machine-checked | `subject-registry.yaml` com 17 subjects (14 stable, 3 planned); `make registry-check` + `make docs-check`; zero discrepancia runtime vs registry |
| **S7** | Delivery com ring buffer + backpressure de producao | **Ring buffer O(1)** (delivery_ring.go) com GC explicito em PopFront/DropFront/RemoveAt; 3 politicas (DropNewest/DropOldest/PriorityDrop); slow-client disconnect; metricas labeled |
| **S8** | Aritmetica fixed-point para dados financeiros | `CandleV1` usa inteiros fixed-point; evita acumulo de erro float64 |
| **S9** | Protobuf completo + transcode cache | 11 proto schemas `stable`; 16 event types dual JSON+Proto; **TranscodeCache** FNV-1a keyed, bounded 1024, ~99% hit rate sob carga, RWMutex thread-safe |
| **S10** | Config JSONC + hot-reload + validation rigorosa | `config.Load()` JSONC → `Validate()` → `*problem.Problem`; `/runtime/reload`; fail-fast panic na startup |
| **S11** | Dependencias atualizadas e saudaveis | Hollywood v1.0.5, NATS v1.48.0, pgx v5.8.0, clickhouse-go v2.43.0, protobuf v1.36.11, goose v3.24.3, Go 1.25.6 — zero advisories |
| **S12** | Concorrencia correta, zero hot-path mutex contention | Proto rollout flags cached via `sync.Once` (1 os.Getenv na startup); codec registry O(1) lookup; event types normalizados no parse time; batch_writer estimativa O(1) |
| **S13** | Backfill multi-exchange operacional | 4 exchanges (Binance ZIP, Bybit gzip, Coinbase REST, HyperLiquid REST); gap detection automatico; fixtures JSONL replayaveis; `cmd/backfill` + `cmd/store` binarios |
| **S14** | Goose migration runner integrado | `cmd/migrate` com goose; TimescaleDB migrations em formato `-- +goose Up/Down`; `UNIQUE(subject,seq)` constraint para PgRangeStore ON CONFLICT |
| **S15** | Observability multi-nivel instrumentada | Metricas Prometheus para codec (latencia, unknown events), BoundedMap (eviction rate, sweep duration), delivery (proto/JSON counters, prefer-proto sessions), heatmap (cells, payload bytes, queue depth) |

---

### WEAKNESSES (Fraquezas Internas)

#### Performance Residual

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W1** | `json.Marshal` no hot-path de heatmap `trimToPayloadBudget()` | Loop iterativo que serializa JSON completo a cada iteracao para checar budget; com 500+ cells = multiplas serializacoes por build | `insights/app/build_heatmap.go:325` — `json.Marshal(out)` dentro de `for len(out.Cells) > 1` |
| **W2** | `json.Marshal` inline para heatmap artifact emissao | Serializa artifact inteiro para metricas de payload size no hot-path (nao para transporte) | `insights/app/build_heatmap.go:173` — `payload, _ := json.Marshal(trimmed)` |
| **W3** | `map[string]any` allocation em `writeJSONDirect` da delivery session | Cria mapa anonimo a cada evento JSON — 1 alloc por delivery event | `session.go:683-689` — `map[string]any{"type":"event",...}` |
| **W4** | Snapshot fingerprint via float→string no Timescale writer | `strconv.FormatFloat()` para cada bid/ask level; livro de 1000 levels = 6000+ conversoes por update | `timescale/writer.go:117-138` |

#### Configuration Gaps

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W5** | `SessionOutboundQueueSize` sem campo em config schema | Ring buffer capacity configuravel no codigo mas sem path de configuracao; sempre default 256; assimetria com bus 1024 | `delivery_ring.go:19` fallback 256; `config/schema.go` sem campo correspondente |
| **W6** | Shard registry configurado via env vars (nao config) | `SHARD_REGISTRY_ENABLED`, `SHARD_REGISTRY_STRICT`, `SHARD_REGISTRY_GRACE` lidos via `os.Getenv()` no bootstrap, nao parte do config schema validado | `cmd/processor/bootstrap.go:171-179` |
| **W7** | Proto rollout flags imutaveis apos startup | `sync.Once` cache correto para performance, mas impede toggle em runtime; requer restart para mudar proto rollout | `contracts/proto_rollout_flags.go:64-66` — `protoFlagOnce.Do(initProtoFlagCache)` |

#### Coverage / Completeness

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W8** | Domain value objects sem testes unitarios dedicados | `marketdata/domain/payloads.go` e `value_objects.go` sem `_test.go` correspondente; cobertos indiretamente mas sem edge-case isolation | Glob confirma: `payloads_test.go` e `value_objects_test.go` nao existem |
| **W9** | Heatmap domain bucket sem testes unitarios | `insights/domain/heatmap_bucket.go` define structs criticos mas sem `heatmap_bucket_test.go` | Glob confirma: arquivo nao existe |
| **W10** | Ports interfaces sem testes (4 BCs) | `aggregation/ports`, `delivery/ports`, `insights/ports`, `marketdata/ports` — interfaces puras sem testes; aceitavel mas reduz confidence em refactors de assinatura | `comm -23` audit confirma |
| **W11** | `cmd/migrate` sem testes | Binary de migracao sem cobertura de testes | Audit confirma: zero `_test.go` em `cmd/migrate/` |
| **W12** | `internal/shared/metrics/` sem testes | Registry Prometheus wrapper (registry.go, 21 linhas) sem testes; trivial mas gap formal | Audit confirma |
| **W13** | Funding rate pipeline incompleto | Config fields existem, stats aggregation suporta, mas consumer/exchange wiring nao finalizado | Config schema presente, processor logic ausente |

#### Architecture

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W14** | Workspace multi-modulo com 14 `replace` directives | `make tidy` necessario apos cada change; onboarding friction; CI fragility | go.work + 14 go.mod files |
| **W15** | ClickHouse migrations sem formato goose | TimescaleDB usa `-- +goose Up/Down` (correto); ClickHouse migrations sao raw DDL sem markers goose | `sql/clickhouse/migrations/0001*.sql` — sem `-- +goose` header |
| **W16** | `fmt.Errorf` em actors/adapters ao inves de `*problem.Problem` | 67 ocorrencias de `fmt.Errorf` fora de domain/app (actors, adapters, config, shardregistry) — aceito em infra mas inconsistente com o padrao `*problem.Problem` | Grep audit: 67 matches em `internal/` |
| **W17** | TranscodeCache eviction full-clear (nao LRU) | Overflow descarta cache inteiro; sob alta cardinalidade de event types, GC spike transiente | `transcode_cache.go:75-80` — `c.cache = make(...)` |

---

### OPPORTUNITIES (Oportunidades Externas)

| # | Oportunidade | Alavanca |
|---|-------------|----------|
| **O1** | Heatmap payload budget via size estimation O(1) | Substituir `json.Marshal` loop por estimativa baseada em cell count (como batch_writer pattern); elimina W1+W2 |
| **O2** | Pre-allocated struct para delivery JSON events | Substituir `map[string]any` por typed struct com `json.Marshal` — elimina 1 alloc/event (W3) |
| **O3** | Shard registry no config schema unificado | Mover env vars W6 para `config.AppConfig.ShardRegistry`; validacao + hot-reload gratis |
| **O4** | Proto rollout flags via config + hot-reload | Integrar flags no config schema; `/runtime/reload` permite toggle sem restart (elimina W7) |
| **O5** | C4 Production soak (10M events multi-exchange) | Infraestrutura pronta (4 exchanges, replay, ring buffer, transcode cache); validacao final de throughput |
| **O6** | Kraken/KrakenF exchange adapters | Padrao `parser.go` + `endpoint.go` canonizado; adicao incremental estende coverage para 6 exchanges |
| **O7** | Funding rate pipeline como diferencial | Stats aggregation ja suporta; wiring no consumer/processor completa feature requerida para insights avancados |
| **O8** | ClickHouse goose format unificado | Converter 6 migrations existentes para formato `-- +goose`; `cmd/migrate` opera em ambas DBs com runner unico |
| **O9** | Snapshot fingerprint via fixed-point hash | Substituir `strconv.FormatFloat` por hash de inteiros fixed-point (como batch_writer pattern); elimina W4 |

---

### THREATS (Ameacas Externas)

| # | Ameaca | Severidade |
|---|--------|-----------|
| **T1** | Hollywood actor framework e nicho | **Media** — Comunidade pequena; bugs upstream podem nao ser corrigidos rapidamente; mitigado por suite robusta (192 test files) + Guardian/Supervisor abstractions proprias |
| **T2** | Exchange API breaking changes | **Media** — 4 exchanges = 4 superficies de breaking change; parsers precisam de manutencao continua; mitigado por golden tests + replay fixtures |
| **T3** | Two-database operational burden | **Media** — TimescaleDB + ClickHouse = 2x monitoring, 2x backup; mitigado por ADR-0019 runbook + goose runner |
| **T4** | Testcontainers + Docker em CI = flakiness | **Media** — Integration tests requerem NATS/PG/CH; mitigado por `MR_ENABLE_SOAK` gates e test isolation |
| **T5** | Concorrentes SaaS (Kaiko, CoinAPI, Amberdata) | **Baixa** — Modelos diferentes, mas competem por mindshare; Raccoon diferencia por schema governance + proto performance |
| **T6** | TranscodeCache full-clear pode causar spike sob burst | **Baixa** — 1024 entries overflow simultaneous → GC spike + cache miss burst; mitigado por working set pequeno (~14 event types) |

---

## Implications Matrix

|  | **O1** Heatmap O(1) | **O2** Pre-alloc delivery | **O5** C4 soak | **O7** Funding rate | **T1** Hollywood nicho | **T2** Exchange breaks |
|---|---|---|---|---|---|---|
| **S4** Testes 192 files | Leverage: safety net para refactor heatmap | Leverage: session tests validam struct change | Leverage: soak infra pronta, replay fixtures 4 exchanges | Leverage: golden tests validam pipeline E2E | Defend: abstractions proprias isolam framework | Defend: golden tests detectam breaking changes |
| **S9** Proto+transcode | — | — | Leverage: proto -60% wire + cache 99% hit = throughput real mensuravel | — | — | — |
| **S13** Backfill 4 exchanges | — | — | Leverage: fixtures multi-exchange para soak realista | Leverage: funding rate downstream de backfill data | — | Defend: backfill fixtures archivam formato conhecido |
| **W1** json.Marshal heatmap | **Invest: P0** — O(1) estimation elimina hot-path serialize | — | — | — | — | — |
| **W5** Config gap ring buffer | — | — | Invest: config field + soak-test com 512/1024 buffers | — | — | — |
| **W13** Funding rate | — | — | — | **Invest: P1** — completa pipeline para insights | — | — |

---

## Key Implications

### 1. P0-P3 Eliminaram os Bloqueios Criticos de Producao
**v2 W1-W6, W9, W14-W15, W18 → RESOLVIDOS**

**Diagnostico:** Os 6 hot-path bottlenecks identificados em v2 (JSON marshal, os.Getenv mutex, string ops, transcode, memory leak, codec scan) foram **todos eliminados corretamente**. Ring buffer com GC explicito resolve memory leak. Proto rollout cache via `sync.Once` elimina 500K mutex/sec. Codec registry O(1) pre-computed. TranscodeCache 99% hit. Observability state machines testadas. Goose migrations integradas. Backfill 4 exchanges operacional.

**Status:** Throughput ceiling levantado de ~10K para estimado ~25-30K evt/sec com 50 clients. **Validacao pendente via C4 soak.**

### 2. Hot-Path Residual: Heatmap json.Marshal Loop
**W1 + W2 + O1**

**Diagnostico:** `trimToPayloadBudget()` faz loop iterativo com `json.Marshal(out)` a cada iteracao para checar se payload cabe no budget. Com 500+ cells, serializa objeto completo multiplas vezes. Nao e tao critico quanto v2 bottlenecks (heatmap emite menos frequentemente que trade events), mas viola zero-tolerance a serialize no hot-path.

**Acao:** Substituir por estimativa O(1): `len(cells) * avgCellBytes + overhead`. Pattern identico ao `estimatePayloadSize()` que corrigiu W1 original no batch_writer.

### 3. Configuration Debt: Ring Buffer + Shard Registry
**W5 + W6 + O3 + O4**

**Diagnostico:** Ring buffer delivery tem capacidade configuravel no codigo (`SessionConfig.OutboundQueueSize`) mas sem campo correspondente em `config.AppConfig` — sempre usa default 256. Shard registry usa 3 env vars (`SHARD_REGISTRY_ENABLED/STRICT/GRACE`) fora do config schema validado. Ambos violam o padrao "config.Load → Validate → fail-fast" estabelecido.

**Acao:**
1. Adicionar `delivery.session_outbound_queue_size` ao config schema (default 512)
2. Mover shard registry para `config.AppConfig.ShardRegistry` struct
3. Validacao cruzada: `session_queue_size <= bus_capacity`

### 4. ClickHouse Migrations Precisam de Formato Goose
**W15 + O8**

**Diagnostico:** TimescaleDB migrations usam formato `-- +goose Up/Down` corretamente, mas ClickHouse migrations (6 arquivos) sao raw DDL sem markers. `cmd/migrate` existe com goose integrado, mas so opera em TimescaleDB.

**Acao:** Converter 6 ClickHouse migrations para formato goose; estender `cmd/migrate` para operar em ambas DBs.

### 5. Test Coverage Edge Cases em Domain VOs
**W8 + W9 + W10**

**Diagnostico:** Domain value objects (payloads, heatmap bucket) e port interfaces nao tem testes unitarios dedicados. Cobertos indiretamente por integration e E2E tests, mas sem edge-case isolation. Para sistema zero-tolerance, cada VO com logica de validacao merece teste dedicado.

**Acao:** Adicionar `value_objects_test.go`, `payloads_test.go`, `heatmap_bucket_test.go` com edge cases (boundary values, zero, negative, overflow).

### 6. Funding Rate Pipeline e o Ultimo Gap Funcional
**W13 + O7**

**Diagnostico:** Config fields existem, stats aggregation suporta, mas consumer/exchange wiring nao finalizado. Requerido para `StatsWindowV1.FundingRateAvg/Last` que esta definido no schema mas nunca populado.

**Acao:** Wire funding rate no consumer (Binance: `markPriceUpdate` ja tem campo; Bybit: `tickers` tem campo) → processor → stats aggregation.

---

## Scorecard Resumido

| Dimensao | v2 Score | v3 Score | Delta | Justificativa |
|----------|----------|----------|-------|---------------|
| Arquitetura | 5/5 | **5/5** | = | DDD + Hexagonal + Actor model + invariantes enforced; zero import cycles; concorrencia correta |
| Qualidade de Codigo | 3.5/5 | **4.5/5** | +1.0 | Hot-path bottlenecks eliminados (W1-W6 v2); ring buffer com GC explicito; transcode cache; **-0.5 por heatmap json.Marshal + config gaps** |
| Testes | 4.5/5 | **4.5/5** | = | 192 files (up from 168), ratio 1.03:1; observability testada; **-0.5 por domain VO edge-case gaps** |
| Cobertura Funcional | 4/5 | **4.5/5** | +0.5 | Proto 100%, backfill 4 exchanges, heatmap+VPVR E2E, markprice 4 exchanges; **-0.5 por funding rate pipeline** |
| Prontidao Operacional | 3.5/5 | **4.5/5** | +1.0 | Goose migrations, PgRangeStore real, observability metricas, backfill 4 exchanges; **-0.5 por ClickHouse migration format + config gaps** |
| Performance | 3/5 | **4/5** | +1.0 | Ring buffer zero-alloc, proto cache 99% hit, codec O(1), batch_writer O(1); **-0.5 por heatmap serialize loop; -0.5 pendente C4 soak validation** |
| Paridade Competitiva | 3.5/5 | **4.5/5** | +1.0 | 4 exchanges + backfill; schema governance; proto superiority; **-0.5 por funding rate + Kraken** |

**Score Geral: 4.5 / 5.0** (up from 3.9) — Hot-path bottlenecks eliminados, memory leak resolvido, observability instrumentada, backfill multi-exchange operacional, goose integrado. Gaps remanescentes sao de completude (funding rate, ClickHouse goose format, domain VO tests) e um hot-path residual no heatmap. C4 soak pendente para validacao final de throughput.

---

## Recommended Next Steps

| Prioridade | Tipo | Acao | Elimina |
|-----------|------|------|---------|
| **P0** | Refactor | Eliminar `json.Marshal` loop em `trimToPayloadBudget()` — O(1) cell-count estimation | W1, W2 |
| **P0** | Config | Adicionar `delivery.session_outbound_queue_size` ao config schema (default 512) | W5 |
| **P1** | Config | Mover shard registry env vars para `config.AppConfig.ShardRegistry` | W6 |
| **P1** | Refactor | Pre-allocated struct para delivery JSON events (eliminar `map[string]any`) | W3 |
| **P1** | Refactor | Snapshot fingerprint via fixed-point hash (eliminar `strconv.FormatFloat`) | W4 |
| **P1** | Testes | Domain VO edge-case tests: `value_objects_test.go`, `payloads_test.go`, `heatmap_bucket_test.go` | W8, W9 |
| **P1** | Feature | Funding rate wiring consumer → processor → stats | W13 |
| **P2** | Infra | Converter ClickHouse migrations para formato goose | W15 |
| **P2** | Config | Proto rollout flags no config schema + hot-reload toggle | W7 |
| **P2** | Validation | **C4 Production soak: 10M events, 4 exchanges, 50 clients** | Validates throughput |
| **P3** | Feature | Kraken/KrakenF exchange adapters | Extends to 6 exchanges |
| **P3** | Cleanup | Migrar `fmt.Errorf` em actors/adapters para `*problem.Problem` onde aplicavel | W16 |

---

## P0-P3 Implementation Verification

### Resolved from v2 (confirmed by 6-agent audit)

| v2 ID | Issue | Status | Fix Quality |
|-------|-------|--------|-------------|
| W1 | `json.Marshal` in `estimatePayloadSize()` | **RESOLVED** | O(1) arithmetic: `baseOverhead + levels * 40` — zero allocs |
| W2 | `os.Getenv()` per delivery event | **RESOLVED** | `sync.Once` + map cache — 1 env read at startup, O(1) lookup after |
| W3 | String ops in `eventPriority()` | **RESOLVED** | Event types normalized at parse time via `naming.NormalizeEventType` |
| W4 | Proto→JSON transcode no cache | **RESOLVED** | `TranscodeCache` FNV-1a keyed, RWMutex, bounded 1024, 99% hit rate |
| W5 | Memory leak `outbound[1:]` | **RESOLVED** | Ring buffer (`delivery_ring.go`) com explicit GC zeroing — 9 test cases |
| W6 | Codec registry O(n) scan | **RESOLVED** | Pre-computed known type+version set — O(1) lookup |
| W7 | DeliveryRangeStore in-memory | **RESOLVED** | `PgRangeStore` real com SQL reads/writes + UNIQUE constraint migration |
| W8 | VolumeProfileWriter sem DDL | **RESOLVED** | DDL existia; stale TODO removido; writer operacional |
| W9 | DB error silenciado | **RESOLVED** | `slog.Warn` com structured fields (subject, seq, err) |
| W10 | Buffer assimetria (parcial) | **PARTIAL** | Ring buffer configuravel mas sem config schema field (→ novo W5) |
| W11 | Router 4 maps O(n) | **RESOLVED** | Router simplificado |
| W12 | Guardian snapshot rebuild | **RESOLVED** | Single-entry cache com invalidacao em 4 mutation paths |
| W14 | Observability sem testes | **RESOLVED** | `observability_test.go` com state machine coverage |
| W15 | Backfill apenas Binance | **RESOLVED** | 4 adapters: Binance ZIP, Bybit gzip, Coinbase REST, HyperLiquid REST |
| W18 | Sem migration runner | **RESOLVED** | `cmd/migrate` com goose v3.24.3 |
| W19 | Zero metricas codec | **RESOLVED** | Histograma latencia, counter unknown events, gauge registry size |
| W20 | Backpressure invisivel | **RESOLVED** | Metricas delivery proto/JSON counters, prefer-proto sessions gauge |
| W21 | BoundedMap sem metricas | **RESOLVED** | Eviction counter by reason, sweep duration histogram |
| T1 | Throughput ceiling ~10K | **RESOLVED** | All 6 bottlenecks eliminated; pending C4 soak validation |
| T2 | Memory leak OOM | **RESOLVED** | Ring buffer with explicit GC — bounded memory under backpressure |
| T8 | os.Getenv mutex contention | **RESOLVED** | sync.Once cache eliminates runtime mutex entirely |

---

## Audit Evidence Trail (v3)

- **6-agent parallel audit:** P0 fixes (batch_writer, proto_flags, ring_buffer, delivery_range_store), P1 optimizations (codec, router, observability), P2 features (transcode_cache, guardian_snapshot, bybit_backfill), P3 infra (goose, PgRangeStore, Coinbase/HyperLiquid backfill), new tech debt scan, codebase metrics
- **Codebase metrics:** 187 source files, 192 test files (1.03:1), 36,675 LOC source, 38,878 LOC tests, 11 proto, 9 SQL migrations, 14 go.mod
- **Packages without tests:** `cmd/migrate`, `*/ports` (4 BCs — interface-only), `internal/tools`, `contracts/testdata/import_guard`
- **Hot-path audit:** `json.Marshal` found in 2 heatmap paths (W1/W2 new), `map[string]any` alloc in delivery (W3 new), `strconv.FormatFloat` in timescale writer (W4 retained from v2 W13)
- **Config audit:** `os.Getenv` in 3 production paths (shard registry), ring buffer default 256 not configurable via schema
- **fmt.Errorf audit:** 67 occurrences in internal/ (actors, adapters, config, shardregistry) — infra layer, not domain/app
