# Market Raccoon Backend — Architectural Audit

**Date:** 2026-03-10
**Scope:** Backend Go codebase (`internal/`, `cmd/`, `proto/`, `deploy/`)
**Frameworks:** Clean Architecture, DDD, Hexagonal, SOLID, Event-Driven Design

---

## 1. Capability Map

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        EXTERNAL INTERFACES                             │
│  internal/interfaces/http  (REST API, control plane, cold reader)      │
│  internal/interfaces/ws    (WebSocket delivery, auth, backpressure)    │
│  client/                   (Odin WASM via nginx)                       │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────────┐
│                     CMD SERVICES (10 binaries)                         │
│  consumer → processor → store    (data pipeline)                       │
│  signals → strategist → executor → portfolio  (trading pipeline)       │
│  server  (delivery hub)   backfill  (REST→DB)   migrate  (schema)      │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────────┐
│                   ACTOR RUNTIME (Hollywood v1.0.5)                     │
│  Guardian → 10 supervised subsystems (restart, heartbeat, cooldown)    │
│  internal/actors/{runtime,marketdata,aggregation,delivery,insights,    │
│                   evidence,signals,signal,strategy,execution,portfolio}│
└──────────────────────────────┬──────────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────────┐
│                     CORE DOMAINS (12 modules)                          │
│  marketdata → aggregation → insights    (observability chain)          │
│                           → evidence    (microstructure detection)     │
│  evidence → signal/signals → strategy → execution → portfolio         │
│  delivery  (routing)    marketmodel (canonical types)                  │
│  workspace (client state persistence)                                  │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────────┐
│                        ADAPTERS                                        │
│  exchange/ (6 venues)   jetstream/ (NATS)   storage/ (Timescale+CH)   │
│  bus/ (in-memory)       execution/ (Binance SafeGuard)                │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────────────┐
│                     SHARED FOUNDATION                                  │
│  problem · result · validation · ids · clock · naming · hash · codec  │
│  envelope · metrics · bootstrap · observability · policykit · ds      │
│  shardregistry · replay · contracts (⚠ domain leakage)               │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Module Table

| Module | Responsabilidade | Entradas | Saídas | Deps (core) | Invariantes |
|--------|-----------------|----------|--------|-------------|-------------|
| **marketdata** | Ingestão WS, sequenciamento, dedup | Raw exchange events | `Envelope` normalizado | marketmodel | Seq monotônica, dedup por IdempotencyKey |
| **aggregation** | Candles, stats, orderbook, tape, OI, CVD | Envelopes (trade, book, mark) | CandleClosed, SnapshotProduced, StatsWindowClosed | marketdata, marketmodel | Book ordenado, checksum determinístico, windows monotônicas |
| **delivery** | Roteamento pub/sub, sessões, backpressure | Envelopes de qualquer domínio | Fan-out por Subject para sessões WS | shared only | SessionID UUID, subscriptions únicas por Subject |
| **insights** | VPVR, heatmap, TPO, cross-venue | Aggregation artifacts | VolumeProfileSnapshot, HeatmapSnapshot | marketmodel | Confidence ∈ [0,1], pure derivation |
| **evidence** | Detecção microestrutura (LEL) | Orderbook, trades | EvidenceEvent (5 tipos) | marketmodel | Determinístico, Features sorted+unique |
| **signal** | Signal engine stateful (regras, features) | MarketEvent, EvidenceEvent | Signal emissions, HandoffData | evidence, marketmodel | State bounded per rule, determinístico |
| **signals** | Composição de sinais compostos | Evidence events | CompositeSignalV1 | evidence | Severity enum, Confidence ∈ [0,1], SourceKinds non-empty |
| **strategy** | Intent planning (signal→intent) | CompositeSignal | StrategyIntentV1 | shared only | IntentID+StrategyRef valid, Sizing>0, PolicyHash non-empty |
| **execution** | Governance, simulation, control plane | StrategyIntent | ExecutionEvent | strategy | FSM 4 estados, AllowlistOverride só restringe |
| **portfolio** | Projeção posições/balances/risco | ExecutionEvent | PortfolioStateV1 | execution | Provenance rastreável, positions non-empty |
| **marketmodel** | Tipos canônicos (Trade, Level, Book) | — | Value objects compartilhados | shared only | Venue/Symbol canônicos, Price>0, Side enum |
| **workspace** | Persistência layout cliente | Client state | Workspace JSON | shared only | SchemaVersion ∈ [1,12], Fingerprint determinístico |

---

## 3. Bounded Contexts & Flow

```
Exchange WS ──→ [consumer] ──→ marketdata.Ingest()
                                    │
                              NATS JetStream
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
              [processor]     [store]          [server]
              aggregation     ClickHouse       delivery→WS
              insights        TimescaleDB      HTTP API
                    │
                    ▼
              [signals/signal]
              evidence→signals→signal
                    │
                    ▼
              [strategist]
              strategy.PlanIntent()
                    │
                    ▼
              [executor]
              execution.GovernedExecutor
                    │
                    ▼
              [portfolio]
              portfolio.SnapshotBuilder
```

---

## 4. Problemas Arquiteturais

### P0 — Inversão de dependência em `shared/contracts`

**Localização:** `internal/shared/go.mod:6-14`, `internal/shared/contracts/*.go` (50 arquivos)

**Problema:** `shared` importa TODOS os módulos core (marketdata, aggregation, insights, evidence, execution, portfolio, signals, strategy, marketmodel). Isso inverte a hierarquia de dependência — a fundação depende dos domínios que ela deveria sustentar.

**Evidência:** `shared/go.mod` declara `require` para 8 módulos core + actors. `contracts/payload_registry.go` registra DTOs de todos os domínios. Conversores em `contracts/aggregation_converters.go`, `insights_converters.go`, `strategy_execution_portfolio_converters.go` etc. importam tipos de domínio.

**Impacto:** Qualquer novo tipo de domínio exige modificação de shared. Impossível compilar/testar shared isoladamente. Violação direta do Dependency Inversion Principle.

**Severidade:** P0 — viola o axioma fundamental da arquitetura hexagonal.

---

### P1 — Naming drift: `signal` vs `signals`

**Localização:** `internal/core/signal/` (13 files), `internal/core/signals/` (9 files), `internal/actors/signal/`, `internal/actors/signals/`

**Problema:** Dois módulos Go separados com nomes quase idênticos e responsabilidades sobrepostas:
- `signal` (singular): SignalEngine stateful, regras, features, store, handoff
- `signals` (plural): CompositeSignalV1, composição, rate limiter

Ambos produzem "sinais" a partir de evidence. A distinção não é evidente pelo nome.

**Impacto:** Cognitive load desnecessário. Novos desenvolvedores não sabem qual usar. Dois `event_catalog.go` separados (`signal/event_catalog.go` e `signals/domain/event_catalog.go`).

---

### P1 — `ownership` e `ticksize` em shared contêm domínio

**Localização:** `internal/shared/ownership/`, `internal/shared/ticksize/`

**Problema:**
- `ownership.Subsystem` enumera nomes de domínio (signals, strategist, execution, portfolio, delivery)
- `ticksize.Registry` contém lógica de display/agrupamento de preço por venue — preocupação de UI/domínio

**Impacto:** Shared absorve conceitos que pertencem a core ou adapters.

---

### P2 — `workspace` com infraestrutura embutida

**Localização:** `internal/core/workspace/infra/file_store.go`

**Problema:** O módulo core contém uma implementação de adapter (FileStore) dentro de `infra/`. Na arquitetura hexagonal, implementações de ports ficam em `adapters/`, não dentro do core.

---

### P2 — `aggregation` depende de `adapters`

**Localização:** `internal/core/aggregation/go.mod`

**Problema:** Um módulo core depende da camada de adapters. Fluxo correto: core define ports → adapters implementa. Core nunca importa adapters.

---

### P2 — Proto-generated code em shared

**Localização:** `internal/shared/proto/gen/` (~30 .pb.go files, 13 domínios)

**Problema:** Código protobuf gerado para TODOS os domínios (aggregation, delivery, execution, insights, portfolio, signals, strategy, evidence, liquidity, marketmodel) reside em shared. Acoplamento semelhante ao de `contracts/`.

---

### P3 — `slo` em shared é operacional, não fundacional

**Localização:** `internal/shared/slo/`

**Problema:** SLO evaluation é uma preocupação operacional/observability, não infraestrutura fundamental. Poderia ser um módulo independente ou parte de observability.

---

### P3 — `legacy_handler.go` em interfaces/ws

**Localização:** `internal/interfaces/ws/legacy_handler.go`

**Problema:** Arquivo com prefixo "legacy" sugere código morto ou em transição. Se não está em uso ativo, é dívida técnica.

---

## 5. Recomendações de Refatoração

| # | Ação | Justificativa | Esforço | Prioridade |
|---|------|---------------|---------|------------|
| R1 | Extrair `shared/contracts/` para `internal/contracts/` (módulo separado com go.mod próprio) | Elimina inversão de dependência P0. Shared volta a ser pura fundação. contracts depende de core (direção correta). | Alto | P0 |
| R2 | Mover `shared/proto/gen/` para `internal/contracts/proto/` ou co-localizar com `proto/` | Proto gerado segue mesmo ownership que contracts — mesma direção de fix. | Médio | P0 |
| R3 | Unificar `signal` + `signals` em um único módulo `internal/core/signal/` com subpacotes `engine/` e `composition/` | Elimina naming drift P1. Um bounded context, um módulo. | Médio | P1 |
| R4 | Mover `shared/ownership/` para `internal/core/marketmodel/` ou novo `internal/core/routing/` | `Subsystem` enum é conceito de domínio. | Baixo | P1 |
| R5 | Mover `shared/ticksize/` para `internal/adapters/exchange/common/` ou `internal/core/marketmodel/` | Tick size é dado de venue, não infraestrutura. | Baixo | P1 |
| R6 | Mover `workspace/infra/file_store.go` para `internal/adapters/storage/workspace/` | Respeitar hexagonal: adapters implementam ports. | Baixo | P2 |
| R7 | Remover dependência de `aggregation` em `adapters` — extrair interface port | Core não importa adapters. Se precisa de exchange metadata, expor via port. | Médio | P2 |
| R8 | Investigar e remover `interfaces/ws/legacy_handler.go` | Dívida técnica potencial. | Baixo | P3 |

---

## 6. Diagnóstico Final

### O que está bem

- **Estrutura hexagonal por módulo:** Cada core module segue `domain/` + `app/` + `ports/` consistentemente. DDD aplicado com aggregates, value objects, domain events e invariantes explícitos.
- **Actor supervision:** Guardian com 10 subsistemas ordenados, backoff exponencial, heartbeat, cooldown — production-grade.
- **Event-driven pipeline:** NATS JetStream com dedup, sharding, replay. Separação clara entre data pipeline (consumer→processor→store) e trading pipeline (signals→strategist→executor→portfolio).
- **Adapter isolation:** 6 exchanges com padrão uniforme (endpoint+parser+backfill). Storage dual-tier (TimescaleDB hot / ClickHouse cold) com federation.
- **Determinismo:** Evidence, signal e aggregation são replay-safe por design. InputWatermark, checksums e idempotency keys throughout.
- **Observability:** 100+ Prometheus metrics, Grafana dashboards, alerting rules, SLO evaluator.
- **Segurança de execução:** ControlPlane FSM (4 estados, 10 comandos), AllowlistOverride que só restringe, GovernedExecutor fail-closed, SimulationEngine determinístico.

### O que precisa de atenção

- **shared/contracts é o calcanhar de Aquiles.** Um único pacote com 50 arquivos que importa todos os domínios e vive na camada errada. Corrigir isso (R1+R2) restaura a integridade arquitetural.
- **signal/signals naming drift** causa confusão desnecessária para um conceito que é claramente um único bounded context.
- **Pequenos domain leaks** em shared (ownership, ticksize) são menores mas acumulam entropia.

### Métricas Estruturais

| Dimensão | Valor |
|----------|-------|
| Go modules (go.work) | 26 |
| Binários executáveis | 10 |
| Core bounded contexts | 12 |
| Actor subsystems | 10 |
| Exchange adapters | 6 |
| Proto schemas | 13 domínios |
| Shared packages (legítimos) | 16 |
| Shared packages (domain leak) | 3 (contracts, ownership, ticksize) |
| Inversões de dependência | 1 crítica (shared→core) |
| Naming drifts | 1 (signal/signals) |

### Veredicto

Arquitetura **sólida e madura** com um único defeito estrutural grave (P0) e um naming issue (P1). O P0 é cirúrgico — extrair `contracts/` para módulo próprio resolve 80% dos problemas sem tocar na lógica de negócio. O restante é refinamento incremental.
