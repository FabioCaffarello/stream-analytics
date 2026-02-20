# SWOT: Market Raccoon — Full Project Assessment (v2)

**Date:** 2026-02-19 (revision 2 — post-audit)
**Perspectiva:** Engenharia avaliando robustez, escalabilidade e performance para produção zero-tolerance. Inclui achados de auditoria automatizada de tech debt + arquitetura.

---

## Quadrants

### STRENGTHS (Forcas Internas)

| # | Forca | Evidencia |
|---|-------|-----------|
| **S1** | Arquitetura Hexagonal com DDD rigoroso | 5 bounded contexts isolados por modulo Go; 6 invariantes de camada (INV-LAY-01-06) enforced por CI; `make invariants-check` |
| **S2** | Pipeline deterministico e replayavel | `FakeClock`, `ReplaySequencer`, `RecorderPublisher`, fixtures JSONL; `internal/core/*` proibido de chamar `time.Now()` (INV-DET-01); golden tests byte-stable |
| **S3** | Actor model com supervisao estruturada | Guardian + SupervisorPolicy (Hollywood API); isolamento de falha por sub-arvore; `ReadyQuery/ReadyResponse` + `/readyz`; zero goroutine leaks (auditoria confirma: todos os 28 spawns devidamente joined ou context-cancelled) |
| **S4** | Suite de testes multi-nivel madura | 168 test files cobrindo 166 source files (ratio 1.01:1); unit-golden-soak-integration-E2E-benchmark; race detector obrigatorio em CI; soak asserts bounded heap/goroutines |
| **S5** | Dual storage plane (hot + cold) | TimescaleDB (hot reads, `getrange`) + ClickHouse (cold analytics); ack-on-commit semantico; drivers reais pgx + clickhouse-go com `IsProductionReady()=true`; cold readers com `FINAL` dedup |
| **S6** | Governanca de schema machine-checked | `subject-registry.yaml` com 17 subjects (14 stable, 3 planned); `owner_bc` + `schema_authority_bc`; `make registry-check` + `make docs-check` validam drift; zero discrepancia runtime vs registry (auditoria confirma) |
| **S7** | Backpressure de producao na delivery | 3 politicas (DropNewest/DropOldest/PriorityDrop); sessoes isoladas como atores; slow-client disconnect configuravel; metricas labeled `ws_drops_total{reason}` |
| **S8** | Aritmetica fixed-point para dados financeiros | `CandleV1` usa inteiros fixed-point; evita acumulo de erro float64 (weakness do MarketMonkey) |
| **S9** | Protobuf completo com domain limpo | 11 proto schemas `stable`; 16 event types com dual JSON+Proto codec; `contracts` layer isola proto do dominio (INV-DOM-01); wire size -60%, parse -40% vs JSON |
| **S10** | Config hot-reload + validation rigorosa | `config.Load()` JSONC -> `Validate()` -> `*problem.Problem`; `/runtime/reload` endpoint; fail-fast panic na startup para config invalida (correto) |
| **S11** | Dependencias atualizadas e saudaveis | Hollywood v1.0.5, NATS v1.48.0, pgx v5.8.0, clickhouse-go v2.43.0, protobuf v1.36.11, Go 1.25.6 — tudo current (auditoria confirma zero advisories) |
| **S12** | Concorrencia correta | 15 Mutex (todos apropriados), RWMutex onde read>write, 9 pkgs com atomic, channels sem send-on-closed, zero import cycles (auditoria confirma) |

---

### WEAKNESSES (Fraquezas Internas)

#### Hot-Path Performance (descobertas pela auditoria)

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W1** | `estimatePayloadSize()` faz JSON marshal completo no hot path | A cada write no ClickHouse batch_writer, serializa snapshot inteiro so para estimar tamanho. A 10K Hz = 10K JSON marshals/sec desperdicados | `clickhouse/batch_writer.go:29-31` — `data, _ := json.Marshal(snap)` |
| **W2** | `ProtoRolloutEnabledForEventType()` chama `os.Getenv()` a cada delivery event | `os.Getenv()` adquire mutex global do runtime. Com 10K evt/sec x 50 sessoes = 500K lock acquisitions/sec | `contracts/proto_rollout_flags.go:50-51` — `os.Getenv(name)` dentro de `envBool()` |
| **W3** | String ops no hot-path de delivery: `eventPriority()` | `strings.ToLower(strings.TrimSpace(eventType))` a cada evento enfileirado, gerando alocacao por evento | `session.go:608` — chamado em cada `enqueueDelivery` |
| **W4** | Proto->JSON transcode sem cache | Quando bus usa proto e client quer JSON: decode proto -> Go struct -> re-marshal JSON. Sem cache = overhead por client por evento | `session.go:654-662` — `codec.DecodePayload` + `json.Marshal(decoded)` |
| **W5** | Memory leak em `s.outbound[1:]` (drop_oldest) | Reslice nao libera referencia ao array original; sob backpressure sustentada, objetos dropped nao sao GC'd | `session.go:523` e `session.go:622` — `s.outbound = s.outbound[1:]` |
| **W6** | Registry codec scan O(n) sob lock | `registryHasAnyCodecForTypeVersion()` itera ambos mapas `decoders` + `encoders` sob RLock para cada evento desconhecido | `payload_codec.go:292-309` — loop sob `reg.mu.RLock()` |

#### Storage / Persistence Gaps

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W7** | `DeliveryRangeStore` e in-memory only | Sem persistencia cross-restart; `GetRange` so retorna dados hot-path recentes; multi-instance impossivel | `timescale/delivery_range_store.go:17` — `// TODO(m2): replace with real Timescale reads.` |
| **W8** | `VolumeProfileWriter` sem tabela Timescale | Schema DDL nao existe; upserts nao persistem quando exec pool habilitado | `timescale/volume_profile_writer.go:18` — `// TODO(sql/timescale/...)` |
| **W9** | Erro de DB engolido silenciosamente no DeliveryRangeStore | `pool.Exec()` retorna erro que e descartado com `_, _` — insert falhos nao sao logados | `timescale/delivery_range_store.go:127` — `_, _ = s.pool.Exec(...)` |

#### Architecture / Scalability Gaps

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W10** | Buffer assimetrico bus vs delivery session | Bus = 1024, session outbound = 256 hardcoded. Backlog forma-se inevitavelmente; session cap nao e configuravel | `bus/inmemory.go:18` (1024) vs `session.go:151` (256) |
| **W11** | Router delivery usa 4 maps com lookup O(n) por subscriber | `subjectSessions[subject][sessionID]` = 2 map lookups por subscriber por evento; com 50 subs = 100 lookups/evt | `router.go:29-32` — 4 maps redundantes |
| **W12** | Guardian snapshot reconstruido a cada query | `/runtime/snapshot` e health checks reconstroem mapa completo + `pid.String()` (protobuf encoding) a cada chamada | `runtime/guardian.go:537-564` |
| **W13** | Snapshot fingerprint via float->string (timescale writer) | `strconv.FormatFloat()` para cada level de bid/ask a cada conflict check. Livro de 1000 levels = 6000+ conversoes por update | `timescale/writer.go:117-138` |

#### Coverage / Completeness

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W14** | `internal/shared/observability/` sem testes | 5 arquivos de state machine criticos (overload, ws, shard, storage, bus) com zero test files | Auditoria confirma: grep por `_test.go` retorna vazio neste pacote |
| **W15** | Backfill adapter apenas Binance | Gap detection funciona multi-exchange mas repair automatico so Binance; Bybit/Coinbase/HyperLiquid manual | `cmd/backfill/` — so `binance.DownloadAggTrades` implementado |
| **W16** | Funding rate pipeline incompleto | Config fields existem, stats aggregation suporta, mas consumer/exchange wiring nao finalizado | Config schema presente mas processor logic ausente |
| **W17** | Workspace multi-modulo com 13 `replace` directives | `make tidy` necessario apos cada change; onboarding friction; CI fragility | go.work + 13 go.mod files |
| **W18** | Sem migration runner automatizado | DDL aplicado via docker-entrypoint init scripts; sem versionamento tipo Flyway/goose | Mitigado por ADR-0019 mas nao resolvido |

#### Observability Gaps

| # | Fraqueza | Impacto | Evidencia |
|---|----------|---------|-----------|
| **W19** | Zero metricas no codec hot-path | Sem histograma de latencia Decode/Encode, sem counter de unknown events, sem gauge de registry size | `payload_codec.go` — auditoria confirma 0 metric references |
| **W20** | Session backpressure invisivel no router level | Router nao sabe se todas as sessoes estao backed up; sem feedback loop para slow-down de ingest | `router.go` — fire-and-forget `engine.Send(pid, DeliveryEvent{})` |
| **W21** | BoundedMap sweep cadence nao observavel | Eviction rate, sweep duration, TTL vs LRU breakdown — tudo invisivel para operadores | `ds/boundedmap.go` — sweep silencioso sem metricas |

---

### OPPORTUNITIES (Oportunidades Externas)

| # | Oportunidade | Alavanca |
|---|-------------|----------|
| **O1** | Eliminacao de hot-path bottlenecks = 2-3x throughput | Corrigir W1-W6 (JSON marshal, os.Getenv, string ops, transcode cache, memory leak, registry scan) pode levar de ~10K para ~25K evt/sec sustentado com 50 clients |
| **O2** | Replay infrastructure para regression testing multi-exchange | `--replay` + `--record` modes maduros; JSONL fixtures extensiveis para todos os 4 exchanges |
| **O3** | Ring buffer na delivery session = zero GC pressure | Substituir slice append/reslice por ring buffer (pattern ja existe em `backpressure_queue.go`) elimina W5 e W10 |
| **O4** | MarketMonkey nao tem schema governance | Raccoon posiciona-se como plataforma com contratos evoluiveis (proto + registry) — diferencial tecnico |
| **O5** | Shard wiring para escalabilidade horizontal | `shardregistry` + JetStream KV prontos; permite scale-out por exchange sem redesign |
| **O6** | Insights BC como diferencial analitico | CrossVenue signals, VolumeProfile, Heatmap — capabilities completas de delivery apos correcao de routing |
| **O7** | Proto rollout flags como config (nao env var) = elimina W2 | Mover flags para `config.AppConfig` carregado uma vez na startup, cache em struct imutavel |
| **O8** | Extensibilidade para novos exchanges | Padrao `parser.go` + `endpoint.go` canonizado; Kraken/KrakenF sao adicoes incrementais |
| **O9** | Observability-as-code para todas as caches | Instrumentar BoundedMap + codec registry + delivery queues = capacity planning data-driven |

---

### THREATS (Ameacas Externas)

| # | Ameaca | Severidade |
|---|--------|-----------|
| **T1** | Hot-path bottlenecks limitam throughput a ~10K evt/sec | **Critica** — W1-W6 combinados criam ceiling de performance que impede escala para producao com multiplos exchanges simultaneos |
| **T2** | Memory leak em delivery sessions (W5) sob carga sustentada | **Alta** — Sob backpressure prolongada, heap cresce monotonicamente; em producao com slow clients, OOM eventual |
| **T3** | Hollywood actor framework e nicho | **Media** — Comunidade pequena; bugs upstream podem nao ser corrigidos rapidamente; mitigado pela suite de testes robusta |
| **T4** | Exchange API breaking changes | **Media** — Exchanges alteram WS APIs sem aviso; parsers precisam de manutencao continua |
| **T5** | Two-database operational burden | **Media** — TimescaleDB + ClickHouse em producao = 2x monitoring, 2x backup, 2x failure modes; mitigado por ADR-0019 |
| **T6** | Testcontainers + Docker em CI = flakiness | **Media** — Integration tests requerem NATS/PG/CH rodando; CI pode ser instavel |
| **T7** | Concorrentes SaaS (Kaiko, CoinAPI, Amberdata) | **Baixa** — Modelos de negocio diferentes, mas competem por mindshare em market data tooling |
| **T8** | `os.Getenv()` mutex contention escala com numero de sessoes (W2) | **Alta** — Pathological: 50 sessoes x 10K evt/sec = 500K mutex acquisitions/sec no Go runtime; throughput degrada nao-linearmente |

---

## Implications Matrix

|  | **O1** Hot-path fix | **O3** Ring buffer | **O7** Proto flags config | **O9** Observability | **T1** Throughput ceiling | **T2** Memory leak | **T8** Env mutex |
|---|---|---|---|---|---|---|---|
| **S2** Determinismo | Leverage: golden tests validam que fixes nao regridem | Leverage: ring buffer testavel deterministicamente | — | — | Defend: soak tests expoe ceiling antes de producao | Defend: soak bounded-heap detecta leak | — |
| **S4** Testes 168 files | Leverage: safety net para refactor agressivo de W1-W6 | Leverage: testes existentes cobrem session backpressure | — | Leverage: novos testes para observability (W14) | Defend: benchmark tests validam throughput | Defend: race detector + soak | — |
| **S9** Proto completo | — | — | Leverage: flags ja mapeados; mover de env para config e trivial | — | Leverage: proto -60% wire elimina parte do ceiling | — | Mitigate: eliminar os.Getenv() resolve T8 completamente |
| **W1** JSON marshal batch | **Invest: priority 1** — substituir por estimativa O(1) baseada em level count | — | — | — | **ROOT CAUSE** parcial | — | — |
| **W2** os.Getenv() | — | — | **Invest: priority 1** — cache em sync.Map ou struct imutavel | — | **ROOT CAUSE** parcial | — | **ROOT CAUSE** direto |
| **W5** Memory leak slice | — | **Invest: priority 1** — ring buffer elimina reslice entirely | — | — | — | **ROOT CAUSE** direto | — |
| **W10** Buffer assimetria | — | **Invest** — ring buffer com cap configuravel via config | — | — | **ROOT CAUSE** parcial (backlog) | Agrava leak | — |

---

## Key Implications

### 1. Hot-Path Performance e o Bloqueio #1 para Producao (NOVO)
**W1 + W2 + W3 + W4 + W6 + T1 + T8**

**Diagnostico:** 6 bottlenecks combinados no hot-path criam um throughput ceiling de ~10K evt/sec com 50 delivery clients. O mais critico e `os.Getenv()` (W2/T8) que adquire mutex global do runtime Go a cada delivery event — contention escala quadraticamente com sessoes.

**Acao imediata (Semana 1):**
1. Cache proto rollout flags em `sync.Map` ou struct imutavel na startup (elimina W2 + T8)
2. Substituir `json.Marshal(snap)` em `estimatePayloadSize()` por `len(snap.Bids)*40 + len(snap.Asks)*40` (elimina W1)
3. Normalizar event types uma vez no parse time, armazenar no envelope (elimina W3)

**Acao curto-prazo (Semana 2-3):**
4. Pre-computar set de type+version conhecidos no codec registry — lookup O(1) vs O(n) (elimina W6)
5. Implementar cache proto->JSON para transcoding (reduz W4)

**Throughput estimado apos fixes: 20-30K evt/sec sustentado com 50 clients (2-3x improvement).**

### 2. Memory Leak na Delivery Session Deve Ser Corrigido Antes de Producao (NOVO)
**W5 + W10 + O3 + T2**

**Diagnostico:** `s.outbound[1:]` cria novo slice header mas referencia ao backing array persiste. Sob drop_oldest com 100 msg/sec e 50% drop rate = ~5000 objetos unreachable por sessao por minuto. Em producao com slow clients, heap cresce monotonicamente ate OOM.

**Acao:** Substituir `[]DeliveryEvent` outbound por ring buffer (pattern ja existe em `backpressure_queue.go`). Tornar capacity configuravel via config (elimina W10). Ring buffer e O(1) enqueue/dequeue sem GC pressure.

### 3. Observability Gaps Impedem Capacity Planning (NOVO)
**W14 + W19 + W20 + W21 + O9**

**Diagnostico:** Tres subsistemas criticos operam sem metricas: codec hot-path (latencia/unknown events), delivery router (backpressure agregada), BoundedMap (eviction rate/sweep duration). Impossivel fazer capacity planning ou diagnosticar degradacao em producao.

**Acao:**
1. Adicionar testes para `internal/shared/observability/` (5 state machines, W14)
2. Instrumentar codec: histograma decode latency, counter unknown events
3. Instrumentar BoundedMap: gauge size, counter evictions by reason, histograma sweep duration
4. Adicionar aggregated backpressure signal no router level

### 4. Storage Gaps Sao Debt Aceito Mas Precisam de Timeline (MANTIDO)
**W7 + W8 + W9 + W18**

**Diagnostico:** DeliveryRangeStore in-memory (W7) e VolumeProfileWriter sem DDL (W8) sao TODOs trackeados. O erro silenciado em W9 e um bug real que deve ser corrigido imediatamente (1 linha de log).

**Acao:**
1. **Imediato:** Logar erro em `delivery_range_store.go:127` (W9 — 1 linha)
2. **M2:** Criar tabela `aggregation_volume_profile` (W8)
3. **M2:** Implementar Timescale reads no DeliveryRangeStore (W7)
4. **M3:** Avaliar migration runner (goose) vs init scripts (W18)

### 5. Multi-Exchange Backfill e Barrier para Paridade Competitiva (MANTIDO)
**W15 + W16 + O8**

**Diagnostico:** Gap detection funciona para todos os exchanges mas auto-repair so Binance. Funding rate pipeline (W16) e requerido para insights avancados (stats per timeframe).

**Acao:**
1. **P2:** Backfill adapter para Bybit (formato similar ao Binance)
2. **P2:** Funding rate wiring no consumer + processor
3. **P3:** Backfill adapters para Coinbase + HyperLiquid

### 6. Suite de Testes e o Ativo Defensivo Mais Valioso (MANTIDO)
**S4 + T6**

**Diagnostico:** 168 test files com ratio 1.01:1 sao a garantia de que refactors agressivos (items 1-5 acima) podem ser feitos com seguranca. Proteger esse ativo: CI stabilization, cache de testcontainers, retry policy para flaky integration tests.

---

## Scorecard Resumido

| Dimensao | Score (1-5) | Justificativa |
|----------|-------------|---------------|
| Arquitetura | **5/5** | DDD + Hexagonal + Actor model + invariantes enforced por CI; zero import cycles; concorrencia correta |
| Qualidade de Codigo | **3.5/5** | Fixed-point, `*problem.Problem`, `result.Result[T]` excelentes; **-1 por 6 hot-path bottlenecks**; -0.5 por workspace complexity |
| Testes | **4.5/5** | 168 files, multi-nivel, golden, race detector; **-0.5 por observability pkg sem testes** |
| Cobertura Funcional | **4/5** | Proto 100%, heatmap+VPVR delivery E2E, markprice normalizado 4 exchanges; -1 por funding rate + multi-exchange backfill |
| Prontidao Operacional | **3.5/5** | Config/shutdown/readiness OK; backfill+gap detection; **-1 por observability gaps (W19-W21)**; -0.5 por DeliveryRangeStore in-memory |
| Performance | **3/5** | 83K+ evt/sec teorico; **ceiling real ~10K com 50 clients por W1-W6**; proto ativado mas transcode overhead anula ganho para JSON clients; memory leak sob backpressure (W5) |
| Paridade Competitiva | **3.5/5** | 4/5 exchanges; arquitetura superior; backfill adapter apenas Binance; funding rate ausente |

**Score Geral: 3.9 / 5.0** — Fundacao arquitetural excepcional (S1-S12), mas hot-path performance bottlenecks (W1-W6) e observability gaps (W19-W21) impedem classificacao como production-ready para alta carga. Memory leak em delivery (W5) e bloqueio critico. Correcoes estimadas em 2-3 semanas para atingir 4.5+.

---

## Recommended Next Steps

| Prioridade | Tipo | Acao | Elimina |
|-----------|------|------|---------|
| **P0** | Hot-fix | Cache proto rollout flags em startup (eliminar `os.Getenv()` per-delivery) | W2, T8 |
| **P0** | Hot-fix | Substituir `json.Marshal` em `estimatePayloadSize()` por estimativa O(1) | W1 |
| **P0** | Hot-fix | Ring buffer na delivery session (substituir slice outbound) | W5, T2, W10 |
| **P0** | Hot-fix | Logar erro em `delivery_range_store.go:127` (1 linha) | W9 |
| **P1** | Refactor | Normalizar event types no parse time (eliminar string ops no delivery) | W3 |
| **P1** | Refactor | Pre-computar known type+version set no codec registry (O(1) lookup) | W6 |
| **P1** | Refactor | Simplificar router para `map[Subject][]*actor.PID` direto | W11 |
| **P1** | Testes | Adicionar testes para `internal/shared/observability/` (5 files) | W14 |
| **P1** | Observability | Instrumentar codec, BoundedMap, delivery router com metricas | W19, W20, W21 |
| **P2** | Feature | Proto->JSON transcode cache para eventos frequentes | W4 |
| **P2** | Feature | Cache Guardian snapshot por 1-2 sec, invalidar em state change | W12 |
| **P2** | Feature | Backfill adapters Bybit + funding rate pipeline | W15, W16 |
| **P3** | Infra | DDL para `aggregation_volume_profile` (Timescale) | W8 |
| **P3** | Infra | Timescale reads no DeliveryRangeStore | W7 |
| **P3** | Infra | Avaliar migration runner (goose) | W18 |
| **P3** | Feature | Backfill adapters Coinbase + HyperLiquid | W15 |

---

## Audit Evidence Trail

- **Tech debt audit:** 168 test files, 166 source files, 2 active TODOs, 0 plain error in domain/app, 28 goroutine spawns (all properly managed), 15 mutex (all appropriate), zero import cycles
- **Architecture audit:** 6 critical hot-path findings, 4 high-severity concurrency/config findings, 3 medium optimization findings, 3 observability gaps
- **Files audited:** `batch_writer.go`, `proto_rollout_flags.go`, `session.go`, `router.go`, `payload_codec.go`, `boundedmap.go`, `guardian.go`, `timescale/writer.go`, `delivery_range_store.go`, `volume_profile_writer.go`, all go.mod files, all exchange adapters, subject-registry.yaml
