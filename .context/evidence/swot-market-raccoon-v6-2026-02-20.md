L1:# SWOT: Market Raccoon — Full Project Assessment (v6)
L2:
L3> **Data:** 2026-02-20 (v6)
L4> **Autor:** Auditor/Arquiteto principal — Market-Raccoon
L5> **Objetivo:** Reavaliação "zero-tolerance" para backend de ultra-alta performance, escalável, determinístico e sem débito técnico. Foco: backend (actors, DDD, hexagonal). Não inclui client em Odin.
L6:
L7:---
L8:
L9:## Executive summary (máx 20 linhas)
L10:
L11:- Estado atual: v5 solucionou as principais dívidas P0–P3; persistem "residual hot-path allocations" (~100K allocs/sec estimado) e lacunas de testes em pontos críticos (NATS/JetStream, replay IO, shard registry).
L12:- Objetivo v6: eliminar qualquer debt remanescente (zero-tolerance), reforçar determinismo e paridade com MarketMonkey no backend, e dotar a pipeline de orquestração de testes/bench gating.
L13:- Abordagem: P0 = três correções rápidas (W1–W3) para baixar GC pressure; P1 = testes e refactor (consolidar writers/parsers, corrigir TranscodeCache); P2 = completar funding-rate pipeline; P3 = políticas operacionais (budgets, SLOs, pinning).
L14:- Validação: micro-bench + benchmem + soak; CI bench gating (p95 <1% regress; allocs/op não aumentar), e execução de golden replay invariants.
L15:- Observação: onde o material não prova diretamente, marquei como “hipótese” e adicionei passos de validação (ex: diffs/commits recentes e outputs de bench não foram anexados aqui).
L16:
L17:---
L18:
L19:## Strengths
L20:
L21:| # | Strength | Evidência |
L22:|---|---------:|---------|
L23:| **S1** | Arquitetura Hexagonal + DDD + Actor supervisionado | `internal/core/`, `internal/actors/`, `.context/docs/` (v5 audit) |
L24:| **S2** | Hot-paths otimizados (FNV-1a, TopicKey concat, makeCellKey) | Implementações P0 do v5 (`HashFieldsFast`, `makeCellKey`) |
L25:| **S3** | Suite de testes ampla + soak validado | C4 soak 117K evt/s (baseline) e 1,247 testes (v5 evidence) |
L26:| **S4** | Dual-storage (TimescaleDB + ClickHouse) com cold-reader API | `internal/adapters/storage/`, `/api/v1/*` |
L27:| **S5** | Observability e métricas multi-nível | `deploy/observability/`, Prometheus rules |
L28:
L29:---
L30:
L31:## Weaknesses (detalhado)
L32:
L33:> Formato: ID — Evidência (arquivo/linha) → Impacto → Remediação proposta (escopo mínimo) → Prioridade
L34:
L35:| ID | Evidência | Impacto real | Remediação mínima | Prioridade |
L36:|----|----------|-------------|-------------------|-----------|
L37:| **W1** | `processor.go:1359,1407` — `fmt.Sprintf("%d", const)` (v5 note) | 2–3 allocs × insight event → aumento GC e p99 latency sob carga alta | Substituir por `strconv.Itoa()` ou literal precomputado; adicionar micro-bench e unit test | **P0** |
L38:| **W2** | `binance/parser.go:314,321` — `strconv.FormatInt` chamado duas vezes para o mesmo tradeID | Aloc extra × centenas de milhares trades/min → CPU + GC churn | Cache string `tradeID` no objeto parse/payload e reutilizar; bench e soak | **P0** |
L39:| **W3** | `dedup_keys_markprice_liquidation.go:23-31` — `append` sem pre-alocação | Reallocs e cópias frequentes em dedup → allocs/op | `make([]string,0,11)` em `lmDedupBase()` ou uso de buffer pool (`sync.Pool`) | **P0** |
L40:| **W4** | `processor.go` / `handleMarkPrice()` — funding rate não pipelineado; `StatsWindowV1.FundingRateAvg/Last` não populado | Perda funcional crítica; não há storage/aggregation/delivery para funding rate | Implementar `BuildFundingRateFromEvents` em `internal/core/aggregation/app`, writers e routing de delivery | **P2** |
L41:| **W5** | `transcode_cache.go:75-80` — full-clear (`c.cache = make(...)`) | Spike de cache misses sob alta cardinalidade → latência/CPU | Implementar eviction incremental (bounded LRU/clock) e métricas; unit tests | **P1** |
L42:| **W6** | `jetstream/ingest_policy.go` (~397 lines) sem unit tests | Config/validation regressions; risco de stream mal configurada | Adicionar unit tests com mocks para validação de policy e stream config | **P1** |
L43:| **W7** | `replay/reader.go`, `replay/canon.go` sem unit tests | Edge-cases I/O (EOF, truncation, corrupt) não cobertos → risco de replays incorretos | Unit tests para EOF/corrupt/truncated fixtures; invariants de canonicalization | **P1** |
L44:| **W8** | `shardregistry/jetstream_kv.go` (~200 lines) sem tests | Coordenação de shards arriscada; timeouts/misses | Mock NATS KV e testes de coordenação/leadership; integration test | **P1** |
L45:| **W9** | `internal/actors/marketdata/runtime/parse.go` sem unit tests (6 channels) | Parsing/routing sem edge-case coverage → mensagens perdidas em corner cases | Tests concorrentes e fuzz inputs; validate backpressure paths | **P1** |
L46:| **W10** | Duplicação em storage writers (Timescale vs ClickHouse) — ~600-800 LOC | Custo de manutenção, bugs divergentes | Extrair `internal/adapters/storage/writer_helpers.go` (marshalling/validation/common) | **P1** |
L47:
L48:---
L49:
L50:## Opportunities
L51:
L52:- **O1 — Quick P0 fixes**: W1–W3 são pequenas e eliminam ~100K allocs/sec estimado; alto ROI.
L53:- **O2 — Consolidar writers/parsers**: Extrair helpers reduz ~2,500 LOC duplicados; facilita manutenção.
L54:- **O3 — HashFieldsFastRaw**: aceitar inputs numéricos diretametne para evitar strconv.
L55:- **O4 — Completar funding-rate pipeline**: fecha gap funcional crítico para paridade.
L56:- **O5 — Allocation budget infra**: gate de CI e dashboards para prevenir regressões.
L57:
L58:---
L59:
L60:## Threats
L61:
L62:- **T1 — Regressão de determinismo** ao refatorar; golden fixtures/replay invariants devem ser preservados.
L63:- **T2 — Mudanças em Protobuf/subject-registry** sem rollout controlado quebram compatibilidade.
L64:- **T3 — Superfície operacional dual-DB** (Timescale + ClickHouse) aumenta risco.
L65:- **T4 — Mudanças não benchmarked** podem degradar p95/p99 rapidamente.
L66:- **T5 — Dependências externas (NATS/JetStream)** podem introduzir mudanças de comportamento.
L67:
L68:---
L69:
L70:## Top Findings (rank 1–10)
L71:
L72:1. Residual hot-path allocations (W1–W3) — prioridade imediata.
L73:2. Funding rate pipeline incompleto (W4) — gap funcional de paridade.
L74:3. Duplicação significativa em writers/parsers (W10) — custo manutenção.
L75:4. TranscodeCache full-clear (W5) — risco em alta cardinalidade.
L76:5. Arquivos críticos sem testes (W6–W9) — risco de regressão.
L77:6. Determinism enforcement required (golden/replay invariants).
L78:7. Observability faltante para allocation budgets.
L79:8. TimescaleDB image pinning (infra risk).
L80:9. Actor parsing concurrency tests absent.
L81:10. JetStream policy validation untested.
L82:
L83:---
L84:
L85:## Plano Prioritário P0–P3 (Pareto-driven)
L86:
L87:### P0 — Quick wins (low-risk, alto-impact)
L88: - Items: W1, W2, W3
L89: - DoD:
L90:   - PRs pequenos (<1 arquivo modificado cada), unit tests adicionados, micro-bench comparativo (benchmem), soak 5–30 min com baseline.
L91:   - Nenhuma alteração na semântica domain; golden fixtures OK.
L92: - Como medir:
L93:   - `go test -bench BenchmarkHotPath -benchmem` (allocs/op, B/op, ns/op) antes/depois; soak p95/p99 e GC pause metrics.
L94: - Riscos: regressão mínima; mitigação: PRs isolados, bench gating.
L95:
L96:### P1 — Refactor + Tests
L97: - Items: W5, W6, W7, W8, W9, W10
L98: - DoD:
L99:   - Extração de `internal/adapters/storage/writer_helpers.go`; `internal/adapters/exchange/common/`; TranscodeCache eviction; unit tests para jetstream/ingest_policy, replay, shardregistry, parse.
L100:  - Bench regression <=1% p95.
L101: - Como medir: cobertura unitária, benchmarks, code-size reduction metric.
L102:
L103:### P2 — Feature completion
L104: - Items: W4 (funding-rate pipeline), HashFieldsFastRaw
L105: - DoD:
L106:   - `BuildFundingRateFromEvents` use case implementado + writers + delivery routing + acceptance tests que validam `FundingRateAvg/Last`.
L107: - Como medir: integração + soak verificando saída esperada em fixtures.
L108:
L109:### P3 — Ops & Governance
L110: - Items: allocation budgets infra, SLOs, pin TimescaleDB image tag, runbooks
L111: - DoD:
L112:   - Dashboards/alerts para allocs/event, p95/p99; runbooks em `docs/`; CI bench gating.
L113:
L114:---
L115:
L116:## Parity gaps vs MarketMonkey (priorizados)
L117:
L118:1. Funding rate aggregation & storage — implementar em `internal/core/aggregation` (P2).
L119:2. Writer helpers compartilhados — `internal/adapters/storage/writer_helpers.go` (P1).
L120:3. Exchange parser common lib (`ParseMeta`, `streamEnvelope`) — `internal/adapters/exchange/common/` (P1).
L121:4. PgRangeStore / deterministic WS getrange — `internal/adapters/storage/pg_range_store` ou `internal/interfaces/ws` (P3).
L122:5. TranscodeCache bounded eviction (LRU) — `internal/shared/transcode_cache.go` (P1).
L123:6. Allocation budget infra + CI gating — `deploy/observability/` + CI targets (P3).
L124:7. JetStream policy test-suite parity — `internal/adapters/nats/jetstream` (P1).
L125:8. Cold-reader endpoints semantics parity (snapshots/candles) — `internal/interfaces/http/cold_reader` (P2).
L126:9. Actor supervision metrics / backoff parity — `internal/actors/*` (P2).
L127:10. Replay canonicalization/golden fixtures coverage — `replay/*` (P1).
L128:
L129:---
L130:
L131:## "Do not touch" list (preserve)
L132:
L133:- `zip/` — READ-ONLY reference.
L134:- Protobuf subjects & `subject-registry.yaml` — mudanças só via rollout controlado.
L135:- Golden fixtures & replay canonicalization (`replay/golden/*`) — preservar formato e ordem determinística.
L136:- Cold-reader API behavior (`/api/v1/candles`, `/api/v1/snapshots`) — somente mudanças aditivas.
L137:- Storage schema migrations em `sql/` — aplicar via migrator, não mudanças manuais.
L138:
L139:---
L140:
L141:## Evidências e notas (derivadas de v5)
L142:
L143:- Principais locais citados no v5: `processor.go:1359,1407`, `binance/parser.go:314,321`, `dedup_keys_markprice_liquidation.go:23-31`, `transcode_cache.go:75-80`, `jetstream/ingest_policy.go`, `replay/reader.go`, `replay/canon.go`, `shardregistry/jetstream_kv.go`, `internal/actors/marketdata/runtime/parse.go`. Estes são pontos de partida para P0/P1.
L144:- Baseline bench/soak: C4 117K evt/s (v5). Recomenda-se rodar `go test -bench` e soak scripts referenciados ao comparar mudanças.
L145:- Hipóteses: commits/diffs recentes e outputs de bench adicionais não estavam anexados ao documento inicial; validar executando micro-benches e reconciliando com `testdata/bench/baseline.txt`.
L146:
L147:---
L148:
L149:## Medição e validação (resumo operacional)
L150:
L151:- Micro-bench: `go test ./internal/... -run none -bench BenchmarkHotPath -benchmem -count=3` (comparar allocs/op, B/op, ns/op).
L152:- Soak: usar scripts de soak fornecidos por 5–30 minutos no cluster C4 (validar p95/p99, GC).
L153:- CI gate: falhar PRs com regressão >1% p95 ou aumento de allocs/op em hot-paths; exigir revisão de arquiteto para mudanças em `internal/core`/`internal/actors`.
L154:
L155:---
L156:
L157:## Ações imediatas recomendadas
L158:
L159:1. Abrir 3 PRs pequenos para P0 (W1, W2, W3) com bench diffs e testes; rodar soak por 5–30m.
L160:2. Paralelizar criação de testes unitários para W6–W9 (P1).
L161:3. Planejar extração de writer_helpers e exchange/common (P1) em duas etapas com benchmarks.
L162:4. Agendar implementação do funding-rate pipeline (P2) com dados de aceitação (fixtures).
L163:
L164:---
L165:
L166:## Nota final
L167:
L168:- Este documento é baseado no arquivo de evidência v5 (`.context/evidence/swot-market-raccoon-v5-2026-02-20.md`) e na auditoria resumida nele contida. Onde dados diretos (diff/commits recentes, bench outputs) não foram fornecidos, marquei como hipótese e incluí passos de validação. Forneça os diffs e bench outputs adicionais para que eu possa reconciliar e atualizar prioridades/prazos com números concretos.
L169:
L170:*** End Patch
