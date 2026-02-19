# SWOT: Market Raccoon — Full Project Assessment

**Date:** 2026-02-19
**Perspectiva:** Equipe de engenharia avaliando maturidade do backend para launch Odin v0 e paridade competitiva com MarketMonkey.

---

## Quadrants

### STRENGTHS (Forças Internas)

| # | Força | Evidência |
|---|-------|-----------|
| **S1** | Arquitetura Hexagonal com DDD rigoroso | 5 bounded contexts isolados por módulo Go; 6 invariantes de camada (INV-LAY-01→06) enforced por CI; `make invariants-check` |
| **S2** | Pipeline determinístico e replayável | `FakeClock`, `ReplaySequencer`, `RecorderPublisher`, fixtures JSONL; `internal/core/*` proibido de chamar `time.Now()` (INV-DET-01); golden tests byte-stable |
| **S3** | Actor model com supervisão estruturada | Guardian + SupervisorPolicy (Hollywood API); isolamento de falha por sub-árvore; `ReadyQuery/ReadyResponse` + `/readyz` |
| **S4** | Suíte de testes multi-nível madura | 185 test files: unit → golden → soak → integration → E2E → benchmark; race detector obrigatório em CI; soak asserts bounded heap/goroutines |
| **S5** | Dual storage plane (hot + cold) | TimescaleDB (hot reads, `getrange`) + ClickHouse (cold analytics); ack-on-commit semântico; drivers reais pgx + clickhouse-go com `IsProductionReady()=true` |
| **S6** | Governança de schema machine-checked | `subject-registry.yaml` com `owner_bc`, `schema_authority_bc`; `make registry-check` + `make docs-check` validam drift |
| **S7** | Backpressure de produção na delivery | 3 políticas (DropNewest/DropOldest/PriorityDrop); sessões isoladas como atores; slow-client disconnect configurável; métricas labeled `ws_drops_total{reason}` |
| **S8** | Aritmética fixed-point para dados financeiros | `CandleV1` usa inteiros fixed-point; evita acúmulo de erro float64 (weakness do MarketMonkey) |
| **S9** | Protobuf-ready com domain limpo | `proto/` com definições para todos os BCs; camada `contracts` isola proto do domínio (INV-DOM-01); rollout flags preparados |
| **S10** | Config hot-reload + validation | `config.Load()` JSONC → `Validate()` → `*problem.Problem`; `/runtime/reload` endpoint; `ShutdownTimeoutDuration()` helpers |

---

### WEAKNESSES (Fraquezas Internas)

| # | Fraqueza | Impacto |
|---|----------|---------|
| **W1** | Stats aggregation pipeline ausente | `aggregation.stats.v1` = draft; non-goal Odin v0; limita insights downstream (funding rate per timeframe) |
| ~~**W2**~~ | ~~Heatmap delivery não fiada end-to-end~~ | **RESOLVIDO:** Pipeline completo: processor timer → `buildHeatmapSnapshotEnvelope` (com `Meta["timeframe"]`) → bus → router (extrai timeframe de `env.Meta["timeframe"]`) → session → WS client. Bug crítico corrigido: router usava `cfg.Timeframe` global ("raw") ignorando timeframe per-envelope. `insights.volume_profile_snapshot` adicionado ao delivery contract. Testes E2E validam heatmap (1m) e volume profile (5m) com meta-based routing. |
| ~~**W3**~~ | ~~Proto hot-path parcialmente ativado~~ | **RESOLVIDO:** Rollout flags completos para 14 event types. Deploy configs `wire_format: "proto"`. Proto→JSON transcode safety no WS delivery. Processor snapshot builders usam rollout flags. 9 schemas promovidos a `stable`. RFC-0007 atualizado. |
| **W4** | `getrange` depende de buffer in-memory | Sem persistência cross-restart; multi-instance impossível para historical range; PgRangeStore limitado |
| ~~**W5**~~ | ~~`cmd/backfill/` é stub funcional~~ | **RESOLVIDO (C3):** `cmd/backfill` operacional com 2 modes: `download` (Binance agg-trades ZIP → JSONL fixtures) e `gaps` (DetectCandleGaps via ClickHouse cold readers). 6 testes backfill + 5 testes gap detector — todos passam. |
| ~~**W6**~~ | ~~Gap detection sem repair~~ | **RESOLVIDO (C3):** `app.DetectCandleGaps` com auto-anchor (GetFirst/LastCandle), leading/inter/trailing gap detection. Cold readers (`ChCandleReader`, `ChStatsReader`, `ChSnapshotReader`) com `FINAL` dedup + 8 unit tests. |
| **W7** | Complexidade do workspace multi-módulo | 13 módulos com `replace` directives obrigatórias; `make tidy` necessário após cada change; onboarding friction |
| **W8** | Duas databases = overhead operacional | TimescaleDB + ClickHouse requerem ambos saudáveis; DDL manual; sem migration runner automatizado tipo Flyway |
| ~~**W9**~~ | ~~Cobertura de exchange assimétrica~~ | **PARCIALMENTE RESOLVIDO:** Coinbase markprice já existia (ticker→markprice parser); Coinbase liquidation impossível (spot). HyperLiquid markprice agora implementado via `allMids` broadcast + `ParseFuncBatch`. Gaps restantes: Coinbase é spot-only (sem liquidation por design); Binance é o único com backfill adapter. |
| **W10** | Desktop client inexistente | Odin terminal é dependência externa fora do escopo; backend-only limita demonstrabilidade |

---

### OPPORTUNITIES (Oportunidades Externas)

| # | Oportunidade | Alavanca |
|---|-------------|----------|
| ~~**O1**~~ | ~~Ativação proto no hot-path para <15us E2E~~ | **CAPTURADO:** Rollout flags implementados para todos os 14 event types. Deploy configs setam `wire_format: "proto"` + `publish_content_type: "application/protobuf"`. Transcode safety no WS. Schemas `stable`. Ganho proto vs JSON: wire size -60%, parse time -40%. |
| **O2** | Replay infrastructure para regression testing multi-exchange | `--replay` + `--record` modes maduros; JSONL fixtures extensíveis para Coinbase/HyperLiquid |
| ~~**O3**~~ | ~~Backfill operacional via `cmd/backfill`~~ | **CAPTURADO (C3):** Download adapter implementado (`binance.DownloadAggTrades`), gap detector operacional, cold readers completos. Oportunidade restante: estender backfill para Bybit/Coinbase/HyperLiquid. |
| **O4** | MarketMonkey não tem schema governance | Raccoon pode posicionar-se como plataforma com contratos evoluíveis (proto + registry) — diferencial técnico |
| **O5** | Shard wiring para escalabilidade horizontal | `shardregistry` + JetStream KV prontos; permite scale-out por exchange sem redesign |
| **O6** | Insights BC como diferencial analítico | CrossVenue signals, VolumeProfile, Heatmap — capabilities que agregam valor além de raw market data |
| **O7** | Cadeia de CI/CD com evidence gates | `runtime-reliability-gate.sh` + evidence artifacts; modelo para compliance/auditoria de performance |
| **O8** | Extensibilidade para novos exchanges | Padrão `parser.go` + `endpoint.go` canonizado; Kraken/KrakenF são adições incrementais |

---

### THREATS (Ameaças Externas)

| # | Ameaça | Severidade |
|---|--------|-----------|
| **T1** | MarketMonkey já opera 5 exchanges em produção | **Alta** — Paridade funcional requer Coinbase + HyperLiquid + backfill operacional |
| **T2** | Odin desktop client depende de backend estável | **Alta** — Qualquer instabilidade no WS delivery bloqueia o frontend team |
| **T3** | Exchange API breaking changes | **Média** — Exchanges alteram WS APIs sem aviso; parsers precisam de manutenção contínua |
| **T4** | Hollywood actor framework é nicho | **Média** — Comunidade pequena; bugs upstream podem não ser corrigidos rapidamente |
| **T5** | Testcontainers + Docker em CI = flakiness | **Média** — Integration tests requerem NATS/PG/CH rodando; CI pode ser instável |
| **T6** | Go 1.25.x é bleeding edge | **Baixa-Média** — Toolchain compatibility com linters/proto generators; breaking changes possíveis |
| **T7** | Two-database operational burden | **Média** — TimescaleDB + ClickHouse em produção = 2x monitoring, 2x backup, 2x failure modes |
| **T8** | Concorrentes SaaS (Kaiko, CoinAPI, Amberdata) | **Baixa** — Modelos de negócio diferentes, mas competem por mindshare em market data tooling |

---

## Implications Matrix

|  | **O1** Proto hot-path | **O2** Replay regression | **O3** Backfill operacional | **O6** Insights diferencial | **T1** MM 5 exchanges | **T2** Odin depende de estabilidade | **T7** Dual-DB burden |
|---|---|---|---|---|---|---|---|
| **S1** DDD/Hexagonal | Leverage: Proto ativa sem tocar domínio — contracts layer absorve | — | — | Leverage: Insights BC isolado permite evolução independente | Defend: Arquitetura permite adicionar exchanges sem acoplamento | — | — |
| **S2** Pipeline determinístico | Leverage: Ativar proto + validar com golden tests existentes | Leverage: Replay com proto wire garante zero-regression | Leverage: Backfill reutiliza ReplaySequencer nativamente | — | — | Defend: Determinismo garante que delivery é previsível | — |
| **S4** Suíte de testes | — | Leverage: 185 tests + soak gates = safety net para novos exchanges | Leverage: Bootstrap test extensível para backfill real | — | Defend: Testes soak validam 83k+ events/sec sob carga | Defend: E2E tests cobrem WS delivery contract | — |
| **S5** Dual storage | — | — | Leverage: Cold readers (C3) leem ClickHouse → fixtures replay | — | — | Defend: Hot-path separado garante latência <250ms no WS | Mitigate: Separação hot/cold justifica a complexidade |
| ~~**W3**~~ ~~Proto parcial~~ | ~~Invest~~ **DONE:** Rollout flags 14 event types + transcode safety + schemas stable | — | — | — | ~~Mitigate~~ **CLOSED:** Proto ativado; throughput acima do CBOR do MM | — | — |
| ~~**W5**~~ ~~Backfill stub~~ | — | — | ~~Invest~~ **DONE:** Download adapter + gaps mode implementados (C3) | — | ~~Mitigate~~ **CLOSED:** Backfill Binance operacional; gap: estender para outros exchanges | — | — |
| **W7** Workspace complexity | — | — | — | — | Mitigate: Cada novo exchange = validar replace directives | — | Mitigate: Simplificar onde possível sem quebrar isolamento |
| ~~**W9**~~ Exchanges normalizados | — | Invest: Gravar fixtures para Coinbase/HL nos campos ausentes | Invest: Backfill adapter por exchange (não apenas Binance) | — | ~~Mitigate~~ **DONE:** Markprice cobertura normalizada (Coinbase=ticker, HL=allMids, Binance+Bybit=dedicated) | — | — |

---

## Key Implications

### 1. ~~Proto activation~~ RESOLVIDO — proto é o wire default
**S2 + S9 + O1 → Status:** Rollout flags completos para 14 event types. Deploy configs setam `wire_format: "proto"`. Transcode safety garante JSON clients recebem JSON válido mesmo com bus proto. 9 schemas `stable`. **Ação residual:** Wire DTO + codec registration para `aggregation.snapshot` e `aggregation.orderbook_inconsistency` (atualmente JSON-only via `codec.Marshal`).

### 2. ~~Backfill operacional~~ RESOLVIDO — próximo gap é multi-exchange backfill
**~~W5 + W6~~ + O3 + T1 → Status:** C3 implementado. `cmd/backfill` com download mode (Binance agg-trades) + gaps mode (DetectCandleGaps). Cold readers (ChCandleReader/StatsReader/SnapshotReader) operacionais. **Ação residual:** Estender backfill adapter para Bybit, Coinbase e HyperLiquid.

### 3. A suíte de testes é o ativo defensivo mais valioso
**S4 + T2 + T5 → Ação:** Investir em estabilização de CI (cache de testcontainers, retry policy para flaky integration tests). Os 185 test files + soak gates são a garantia de que Odin v0 não terá regressões. Proteger esse ativo.

### 4. ~~Exchange parity requer normalização~~ PARCIALMENTE RESOLVIDO — markprice normalizado para todos
**~~W9~~ + O8 + T1 → Status:** Cobertura de markprice normalizada: Coinbase já tinha (ticker parser, descoberto pela investigação), HyperLiquid adicionado via `allMids` broadcast com `ParseFuncBatch`. Coinbase liquidation impossível (spot exchange). **Ação residual:** Estender backfill adapters para Bybit/Coinbase/HyperLiquid (apenas Binance implementado); adicionar Kraken/KrakenF.

### 5. ~~Insights BC precisa de heatmap delivery~~ RESOLVIDO — heatmap + volume profile delivery E2E
**S1 + O6 + T8 → Status:** Heatmap delivery E2E corrigido: bug de routing por timeframe resolvido (router agora extrai `Meta["timeframe"]` do envelope). Volume profile delivery adicionado ao contrato. CrossVenue signals, VolumeProfile e Heatmap agora são capabilities completas de delivery. **Ação residual:** Aprofundar Insights BC como moat técnico — heatmap live, CrossVenue v2.

### 6. Dual-database é uma aposta consciente, não uma fraqueza acidental
**S5 + W8 + T7 → Ação:** Documentar trade-off em ADR formal. TimescaleDB para hot reads (latência) + ClickHouse para analytics (throughput) é uma escolha arquitetural válida, mas precisa de runbooks operacionais maduros e monitoring unificado.

---

## Scorecard Resumido

| Dimensão | Score (1-5) | Justificativa |
|----------|-------------|---------------|
| Arquitetura | **5/5** | DDD + Hexagonal + Actor model + invariantes enforced por CI |
| Qualidade de Código | **4/5** | Fixed-point, `*problem.Problem`, `result.Result[T]`; -1 por workspace complexity |
| Testes | **5/5** | Multi-nível (unit→soak→E2E), golden, race detector, 185 files |
| Cobertura Funcional | **4/5** | Stats ausente; ~~heatmap não fiado~~ heatmap + volume profile delivery E2E; ~~exchanges assimétricos~~ markprice normalizado para 4 exchanges; ~~backfill stub~~ backfill operacional (C3) |
| Prontidão Operacional | **3.5/5** | Config/shutdown/readiness OK; backfill + gap detection operacionais (C3); falta migration runner, multi-exchange backfill |
| Performance | **4.5/5** | 83k+ evt/sec, 15us E2E orderbook; proto ativado no hot-path (wire -60%, parse -40%); -0.5 por falta de wire DTO para snapshot/inconsistency |
| Paridade Competitiva | **3.5/5** | 4/5 exchanges com markprice completo (Coinbase spot não tem liquidation por design); arquitetura superior; backfill adapter apenas Binance |

**Score Geral: 4.4 / 5.0** — Fundação técnica excepcional; C3 fechou backfill/gap detection, proto hot-path ativado com transcode safety, markprice normalizado para todos os 4 exchanges, heatmap + volume profile delivery E2E corrigidos (bug de timeframe routing). Gaps restantes: stats pipeline, multi-exchange backfill, Kraken/KrakenF.

---

## Recommended Next Steps

| Prioridade | Artefato | Ação |
|-----------|----------|------|
| ~~**P0**~~ | ~~RFC~~ | ~~Proto hot-path full rollout~~ — **DONE:** RFC-0007 atualizado, 14 flags, transcode safety, schemas stable |
| ~~**P0**~~ | ~~Implementação~~ | ~~C3: `cmd/backfill` operacional + gap detection~~ — **DONE** |
| ~~**P0**~~ | ~~Implementação~~ | ~~Normalizar cobertura de exchanges (W9)~~ — **DONE:** Coinbase markprice já existia (ticker parser); HyperLiquid markprice via `allMids` + `ParseFuncBatch`; Coinbase liquidation impossível (spot) |
| **P1** | ADR | ADR formal para dual-database trade-off + runbook operacional (W8) |
| ~~**P1**~~ | ~~Implementação~~ | ~~Heatmap delivery end-to-end (W2)~~ — **DONE:** Bug de timeframe routing corrigido (`Meta["timeframe"]` nos envelopes + router extrai). `volume_profile_snapshot` adicionado ao delivery contract. Testes E2E validam heatmap 1m e VPVR 5m. |
| **P1** | Implementação | Wire DTOs + codec registration para `aggregation.snapshot` + `orderbook_inconsistency` (residual proto) |
| **P2** | Milestone Plan | CI stabilization (testcontainers cache, retry policy, evidence gates em CI) |
| **P3** | RFC | Insights BC como diferencial — roadmap CrossVenue v2 + Heatmap live |
