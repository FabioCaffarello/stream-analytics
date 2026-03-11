# Auditoria: Fluxos Críticos Ponta a Ponta — Market Raccoon

**Data:** 2026-03-10
**Escopo:** 131K LOC backend (Go) + client (Odin), 11 cmd binários, 13 containers
**Método:** Leitura direta de codebase, docker-compose, domain types, actor wiring, HTTP/WS surfaces

---

## Topologia de Serviços

```
┌───────────────────────────── NATS JetStream ─────────────────────────────┐
│  marketdata.>  aggregation.>  evidence.>  signal.>  strategy.>           │
│  execution.>  portfolio.>  insights.>  quarantine.>  liquidity.>         │
└──────────────┬──────────┬──────────┬──────────┬──────────┬───────────────┘
               │          │          │          │          │
┌──────────┐ ┌─┴────────┐ ┌┴─────────┐ ┌┴────────┐ ┌─────┴──┐ ┌──────────┐
│ consumer │→│ processor│→│ signals  │→│strategist│→│executor│→│ portfolio│
│ :8081    │ │ :8082    │ │ :8084    │ │ :8085    │ │ :8086  │ │ :8087    │
└──────────┘ └────┬─────┘ └──────────┘ └──────────┘ └────────┘ └──────────┘
                  │
           ┌──────┴───────┐
           │    store     │     ┌──────────┐
           │   :8083      │     │  server   │─── WS /ws ───→ Client :8090
           │  (ClickHouse)│     │  :8080    │─── HTTP /api/v1/*
           └──────────────┘     └──────────┘

Infra: NATS 2.10.18 │ TimescaleDB 2.25.1 │ ClickHouse 24.8.8 │ Prometheus │ Grafana
```

---

## Fluxo 1: Market Data → Processing → Delivery → Client → UI

### 1.1 Pipeline Completo

```
Exchange WS ──→ Consumer Actor ──→ NATS "marketdata.{type}.v1.{venue}.{symbol}"
                                          │
                                   ┌──────┴──────┐
                                   ▼              ▼
                              Processor       Server/WS
                              (aggregation    (delivery router
                               + storage)      → session actor
                                   │            → client WS)
                                   ▼
                            TimescaleDB (hot)
                            ClickHouse (cold)
```

| Etapa | Ownership | Arquivo(s) chave | Contrato |
|-------|-----------|------------------|----------|
| Exchange WS connect | `internal/adapters/exchange/` | Per-exchange adapters (Binance, Bybit, Coinbase, HyperLiquid, Kraken, KrakenF) | Raw JSON frames |
| Normalize + Publish | `internal/actors/marketdata/ws/` | Manager + Consumer actors | `envelope.Envelope` with `marketdata.trade.v1.*`, `marketdata.orderbook.v1.*` etc. |
| NATS transport | `internal/adapters/jetstream/` | `publisher.go`, `stream.go` | Subject taxonomy: `{root}.{event}.v{n}.{venue}.{instrument}` |
| Aggregation | `internal/actors/aggregation/runtime/` | ProcessorSubsystemActor | `aggregation.candle.v1.*`, `aggregation.stats.v1.*` |
| Hot storage | `internal/adapters/storage/` | TimescaleDB writer | Goose migrations in `sql/timescale/` |
| Cold storage | `internal/adapters/storage/` | ClickHouse batch writer | Goose migrations in `sql/clickhouse/` |
| Delivery router | `internal/actors/delivery/runtime/router.go` | Router actor fans out to sessions | `DeliveryRing` + `TranscodeCache` (16 shards, LRU) |
| WS Session | `internal/actors/delivery/runtime/session.go` | SessionActor per client | JSON or Protobuf envelope, seq policy, backpressure, rate limit |
| WS Server | `internal/interfaces/ws/server.go` | Upgrade + auth + spawn session | gorilla/websocket, auth scopes, connection registry |
| Client receive | `client/src/platform/web/marketdata_web.odin` | JS interop, two-pass JSON | PascalCase domain structs |
| Protocol engine | `client/src/core/md_common/` | `stream_apply_state.odin`, `protocol_engine.odin` | `Stream_Apply_State` with seq tracking, snapshot reconciliation |
| Services layer | `client/src/core/services/` | `dom_store.odin`, candle_store, orderbook_store, trades_store | Per-stream store isolation |
| Layers | `client/src/core/layers/` | `market_store.odin`, `layer_api.odin`, `data_source.odin` | `Cell_Surface_View` (10 fields ceiling) |
| App/render | `client/src/core/app/` | `build_cell.odin`, `layer_canvas.odin`, `layer_marketdata.odin` | `Pane_Visual_State` (8 fields) |

### 1.2 Ambiguidades Semânticas

1. **`envelope.Envelope` é polimórfico**: tipo do payload determinado por `subject_prefix` metadata key em runtime. Não há union type em compile-time — erro de mismatch é silencioso.
2. **Two-pass JSON no client**: JS runtime faz parse parcial → Odin faz parse final. Erro na primeira passagem é invisível ao Odin.
3. **`TranscodeCache` TTL vs freshness**: cache serve payloads pré-serializados sem awareness de stale data — a stale check vive exclusivamente no client.

### 1.3 Pontos de Acoplamento

- **Consumer ↔ Exchange WS**: cada exchange adapter tem protocolo proprietário; reconnection logic per-adapter
- **NATS como single bus**: todos os 10 subject roots numa única stream JetStream — sem isolamento de failure domain
- **Router → Session fan-out**: router mantém subscription map; crash do router = todas as sessões perdem delivery
- **Client `BootstrapPayloadCodecRegistry()`**: se não chamado, deserialização falha silenciosamente

### 1.4 Riscos Operacionais

| Risco | Severidade | Detalhe |
|-------|-----------|---------|
| NATS single stream | **ALTO** | `subjectWildcards` combina 10 roots numa stream; back-pressure num domain afeta todos |
| Consumer shard-aware mas processor assume SHARD_COUNT=1 default | MÉDIO | `docker-compose.yml` l.153/192 — scale mismatch possível |
| TranscodeCache invalidation | MÉDIO | Cache LRU não é invalidada por schema change; deploy com novo envelope version pode servir stale binary |
| WS `CheckOrigin: always true` | MÉDIO | `ws/server.go:206` — qualquer origin aceito; ok para dev, risco em prod |
| Client two-pass JSON | BAIXO | Overhead de parse duplo; não impacta correctness mas impacta latência |

---

## Fluxo 2: Evidence → Signal → Strategy Intent → Execution Event → Portfolio State

### 2.1 Pipeline Completo

```
Market events ──→ Evidence Engine ──→ NATS "evidence.microstructure_evidence.v1.*"
                  (LEL rules:            │
                   spread_explosion,     │
                   liquidity_thinning,   │
                   persistent_imbalance, │
                   absorption, sweep)    │
                                         ▼
                                   Signal Engine ──→ NATS "signal.*.v1.*"
                                   (rules + dedup     │
                                    + rate limit)      │
                                                       ▼
                                                 Strategist ──→ NATS "strategy.intent.v1.*"
                                                 (plan_intent)    │
                                                                  ▼
                                                            Executor ──→ NATS "execution.event.v1.*"
                                                            (GovernedExecutor      │
                                                             + ControlPlane        │
                                                             + SimulationEngine)   │
                                                                                   ▼
                                                                             Portfolio ──→ NATS "portfolio.state.v1.*"
                                                                             (Projector
                                                                              + Reconciliation
                                                                              + SnapshotBuilder)
```

| Etapa | Tipo de Evento | Arquivo de domínio | Campos-chave |
|-------|---------------|-------------------|--------------|
| Evidence | `EvidenceEvent` | `internal/core/evidence/domain/evidence.go` | Type, Severity, Confidence, Features[], InputWatermark |
| Signal | `SignalEvent` (em `marketmodel`) | `internal/core/signal/engine.go` | Type, Scope, Severity, Confidence, Features[], RuleID, CorrelationID, SignalID |
| Intent | `StrategyIntentV1` | `internal/core/strategy/domain/intent.go` | IntentID, Side(buy/sell), Sizing, Constraints, Provenance.ParentSignalIDs |
| Execution | `ExecutionEventV1` | `internal/core/execution/domain/event.go` | Status(8 states), Correlation.IntentID, RequestedQty, fills, GovernanceRef |
| Portfolio | `PortfolioStateV1` | `internal/core/portfolio/domain/state.go` | Scope, Positions[], Balances[], Exposures[], Risk, EquityUSD, PnL |

### 2.2 Ownership

| Binário | Core Domain | Actor Domain |
|---------|-------------|--------------|
| `cmd/signals` | `internal/core/signal/` + `internal/core/signals/` + `internal/core/evidence/` | `internal/actors/signal/` + `internal/actors/signals/` + `internal/actors/evidence/` |
| `cmd/strategist` | `internal/core/strategy/` | `internal/actors/strategy/` |
| `cmd/executor` | `internal/core/execution/` | `internal/actors/execution/` |
| `cmd/portfolio` | `internal/core/portfolio/` | `internal/actors/portfolio/` |

### 2.3 Ambiguidades Semânticas

1. **`internal/core/signal/` vs `internal/core/signals/`**: DOIS módulos com nomes quase idênticos. `signal/` contém `SignalEngine` com rules + dedup. `signals/` contém `CompositeSignal` + `SignalRateLimiter`. Ownership dividido sem fronteira clara.
2. **`SignalEvent` vive em `marketmodel`, não em `signal/domain`**: quebra o padrão hexagonal — domain type do signal BC está num módulo compartilhado.
3. **`EvidenceEvent` produzido pelo `signals` binary**: o serviço `signals` roda tanto evidence engine quanto signal engine. O nome sugere apenas signals.
4. **Execution mode**: `GovernedExecutor` suporta `bootstrap_simulated` (default) e `real_adapter_safe`. Transição entre modos é por config flag, não por state machine — risco de flip acidental em deploy.
5. **Portfolio `Validate()` exige `positions` não vazio**: impossível representar portfolio vazio (pré-primeiro-trade). Força workaround.

### 2.4 Riscos

| Risco | Severidade | Detalhe |
|-------|-----------|---------|
| `signal/` vs `signals/` naming collision | **ALTO** | Confusão garantida para novos contributors; ambos em `go.work`; importadores precisam alias |
| `SignalEvent` em `marketmodel` | MÉDIO | Acoplamento: `marketmodel` importado por signal, strategy, evidence — mudança cascata |
| `bootstrap_simulated` como default execution | MÉDIO | Se config deploy falha em setar mode, cai em simulação — silently não executa |
| `signals` binary faz evidence + signal | BAIXO | Coesão aceitável agora, mas scaling independente impossível |
| Sem retry policy cross-service | MÉDIO | Se NATS drop entre strategist→executor, intent é perdido; sem DLQ configurado |

---

## Fluxo 3: Session / Dashboard / Workspace / Readiness

### 3.1 Pipeline

```
Client boot ──→ GET /api/v1/session ──→ session metadata
                GET /api/v1/session/dashboard ──→ dashboard layout
                GET /api/v1/catalog ──→ available instruments
                GET /api/v1/workspace ──→ persisted workspace (if exists)
                GET /api/v1/markets ──→ market discovery
                                         │
                                         ▼
                            Client Workspace Manager
                            (schema_version=12, RUNTIME_SNAPSHOT_VERSION=3)
                                         │
                                    PUT /api/v1/workspace ──→ persist
                                    DELETE /api/v1/workspace ──→ reset
```

| Componente | Ownership | Arquivo |
|-----------|-----------|---------|
| Session endpoint | `internal/interfaces/http/session_handlers.go` | Returns connected exchanges, subscriptions |
| Dashboard endpoint | `internal/interfaces/http/session_dashboard_handlers.go` | Returns layout configuration |
| Catalog endpoint | `internal/interfaces/http/catalog_handlers.go` | Returns instrument catalog from `MarketsConfig` |
| Workspace domain | `internal/core/workspace/domain/workspace.go` | `MaxSchemaVersion=12`, `layoutPrefix="V6"`, FNV-1a fingerprint |
| Workspace service | `internal/core/workspace/app/service.go` | CRUD operations |
| Workspace storage | `internal/core/workspace/infra/file_store.go` | File-based persistence |
| Client workspace | `client/src/core/app/workspace.odin` | `WORKSPACE_SCHEMA_VERSION=12`, `RUNTIME_SNAPSHOT_VERSION=3` |
| Client readiness | `client/src/core/app/widget_readiness.odin` | `Data_Readiness` (6 variants max) |
| Client components | `client/src/core/app/components.odin` | Widget catalog (13 Widget_Kind variants) |

### 3.2 Ambiguidades

1. **Workspace persistence é file-based**: `file_store.go` — não escala para multi-instance server. Sem locking; race condition possível entre dois servers.
2. **Schema version coupling**: client e server DEVEM concordar em `MaxSchemaVersion=12`. Deploy assimétrico (client novo + server velho) → rejection. Sem negotiation protocol.
3. **Session endpoint vs WS handshake**: informação de "session" vem de dois caminhos disjuntos (HTTP GET e WS upgrade). Sem reconciliação formal.
4. **Dashboard endpoint**: retorna layout estático do server; não reflete workspace personalizado do client — duas fontes de verdade para layout.

### 3.3 Riscos

| Risco | Severidade | Detalhe |
|-------|-----------|---------|
| File-based workspace store | **ALTO** | Não suporta multi-instance; sem locking; corrupção possível |
| Schema version mismatch deploy | MÉDIO | Client deploy vs server deploy order matters; sem graceful degradation |
| Dual layout sources (dashboard vs workspace) | MÉDIO | Dashboard = default factory; workspace = user override; merge logic implicit |

---

## Fluxo 4: Health / Freshness / Degradação

### 4.1 Backend Health Pipeline

```
┌──────────────────────────────────────────┐
│ GET /healthz                             │
│   → Guardian Snapshot                    │
│   → SubsystemMarketData.Connected?       │
│   → last_message_age_ms / last_publish_age│
│   → "ok" | "degraded"                    │
├──────────────────────────────────────────┤
│ GET /readyz                              │
│   → readyGate (optional startup check)   │
│   → Guardian ReadyQuery                  │
│   → Ready bool + Pending subsystems      │
├──────────────────────────────────────────┤
│ GET /api/v1/freshness                    │
│   → per-stream freshness metrics         │
├──────────────────────────────────────────┤
│ GET /api/v1/trading/readiness            │
│   → control plane state + portfolio      │
│   → composite readiness score            │
└──────────────────────────────────────────┘
```

### 4.2 Client Health Pipeline (5 layers — ADR-0034)

```
Transport health ──→ Delivery health ──→ Snapshot health ──→ Health state ──→ Reliability
(WS connected?)     (seq gaps? recv     (snapshot stale?    (Candle_Health:   (Stream_Reliability:
                     age?)               backfill done?)     No_Data/OK/       7-state enum
                                                             Lagging/Stale)    per ADR-0032)
```

| Layer | Ownership | Arquivo | Derivação |
|-------|-----------|---------|-----------|
| Transport | `client/src/core/md_common/` | `stream_apply_state.odin` | WS connected + message recv timestamps |
| Delivery | `client/src/core/md_common/` | `stream_apply_state.odin` | Seq tracking, desync detection |
| Snapshot | `client/src/core/md_common/` | `tf_data_contract.odin` | Backfill lifecycle, snapshot availability |
| Health | `client/src/core/app/` | `health.odin` | `Candle_Health` enum, TF-adaptive thresholds |
| Reliability | `client/src/core/app/` | `widget_readiness.odin` | `Data_Readiness` (6 variants), `Stream_Reliability` (7 states) |

**Invariante-chave:** "Pure derivation only — no cached health/reliability state" (guard rail S158).

### 4.3 Ambiguidades

1. **`/healthz` faz snapshot request ao Guardian**: liveness probe com dependência do actor system. Se Guardian trava, liveness falha → restart loop. Deveria ser unconditional 200.
2. **Health thresholds TF-adaptive mas hardcoded**: `lag_warn_closed = max(2*tf_ms, 5000)` etc. Não configurável sem rebuild.
3. **Backend freshness vs client freshness**: backend `/api/v1/freshness` retorna per-stream metrics; client calcula freshness independentemente. Duas fontes de verdade divergentes.
4. **Desync reasons mapeados manualmente**: `md_desync_reason_to_stream()` em `health.odin` faz switch case 1:1; novo variant no backend = compile break no client. Frágil mas pelo menos fail-fast.

### 4.4 Riscos

| Risco | Severidade | Detalhe |
|-------|-----------|---------|
| `/healthz` com Guardian dependency | **ALTO** | Liveness probe deve ser unconditional; timeout de 5s no snapshot pode causar restart cascata |
| Dual freshness sources | MÉDIO | Backend e client podem divergir em "is this stream stale?" |
| Hardcoded thresholds | BAIXO | Funciona, mas impede operador de tunar sem deploy |

---

## Fluxo 5: Superfícies HTTP/WS Relevantes para Operação

### 5.1 HTTP API Surface (`internal/interfaces/http/server.go`)

#### Liveness/Readiness/Runtime (localhost-only onde indicado)
| Método | Path | Acesso | Propósito |
|--------|------|--------|-----------|
| GET | `/healthz` | Público | Liveness probe (mas faz snapshot — ver risco) |
| GET | `/readyz` | Público | Readiness probe |
| GET | `/runtime/snapshot` | localhost | Guardian state JSON |
| GET | `/runtime/overload` | localhost | PolicyKit overload partitions |
| GET | `/runtime/storage` | localhost | Hot/cold storage health |
| GET | `/runtime/ws` | localhost | WS session introspection |
| GET | `/runtime/terminal` | localhost | Terminal WS state (last 100) |
| GET | `/shardz` | localhost | Shard topology + lag |
| POST | `/runtime/reload` | localhost | Hot reload config |
| GET | `/metrics` | Público | Prometheus metrics |

#### Cold Reader API (ClickHouse)
| Método | Path | Propósito |
|--------|------|-----------|
| GET | `/api/v1/candles` | Historical candles |
| GET | `/api/v1/stats` | Historical stats |
| GET | `/api/v1/snapshots` | Orderbook snapshots |
| GET | `/api/v1/tape` | Trade tape |
| GET | `/api/v1/oi` | Open interest |
| GET | `/api/v1/delta_volume` | Delta volume |
| GET | `/api/v1/cvd` | Cumulative volume delta |
| GET | `/api/v1/bar_stats` | Per-bar statistics |

#### Discovery / Session
| Método | Path | Propósito |
|--------|------|-----------|
| GET | `/api/v1/markets` | Market discovery |
| GET | `/api/v1/catalog` | Instrument catalog |
| GET | `/api/v1/session` | Session metadata |
| GET | `/api/v1/session/dashboard` | Dashboard layout |
| GET | `/api/v1/artifacts/summary` | Artifact summary |
| GET | `/api/v1/freshness` | Per-stream freshness |
| GET | `/api/v1/instrument/overview` | Instrument overview |
| GET | `/api/v1/timeline` | Historical timeline |

#### Workspace
| Método | Path | Propósito |
|--------|------|-----------|
| GET | `/api/v1/workspace` | Load workspace |
| PUT | `/api/v1/workspace` | Save workspace |
| DELETE | `/api/v1/workspace` | Delete workspace |

#### Portfolio
| Método | Path | Propósito |
|--------|------|-----------|
| GET | `/api/v1/portfolio/state/latest` | Latest portfolio state |
| GET | `/api/v1/portfolio/states` | Portfolio state history |
| GET | `/api/v1/portfolio/account-snapshot/latest` | Latest account snapshot |
| GET | `/api/v1/portfolio/summary/latest` | Latest summary |
| GET | `/api/v1/portfolio/account-snapshots` | Account snapshot history |
| GET | `/api/v1/portfolio/summaries` | Summary history |
| GET | `/api/v1/portfolio/equity-curve` | Equity curve |
| GET | `/api/v1/portfolio/reconciliation` | Reconciliation status |

#### Insights
| Método | Path | Propósito |
|--------|------|-----------|
| GET | `/api/v1/insights/session-vp` | Session volume profile |
| GET | `/api/v1/insights/tpo` | Time-price opportunity profile |

#### Control Plane (localhost-only)
| Método | Path | Propósito |
|--------|------|-----------|
| POST | `/api/v1/control` | Apply control command |
| GET | `/api/v1/control/snapshot` | Control plane state |
| GET | `/api/v1/trading/readiness` | Composite trading readiness |

#### Debug
| Método | Path | Propósito |
|--------|------|-----------|
| GET | `/debug/pprof/*` | Go pprof (quando habilitado, localhost-only) |
| GET | `/api/v1/delivery/diagnostics` | Delivery diagnostics (localhost-only) |
| GET | `/api/v1/consistency` | Hot/cold consistency check (localhost-only) |

### 5.2 WebSocket Surface (`internal/interfaces/ws/server.go`)

| Endpoint | Path | Auth |
|----------|------|------|
| WS upgrade | `GET /ws` | API key auth + scope check (`ws:read`) |

**Capabilities:**
- Subscribe/unsubscribe per venue+symbol
- Signal subscriptions (max 20/conn default)
- Rate limiting (per-session + per-IP)
- Protobuf or JSON wire format (client preference via header)
- Compression (permessage-deflate, enabled by default)
- Connection limits: 200/IP, 20/key, 256 subs/conn, 128 symbols/conn
- Slow client drop policy
- Backpressure strategy via `DeliveryRing`
- `SnapshotWireCache` for repeated snapshot requests
- Tenant-specific limit overrides

### 5.3 NATS Subject Taxonomy

```
{domain}.{event_type}.v{version}.{venue}.{instrument}
```

Domains registrados: `marketdata`, `aggregation`, `evidence`, `insights`, `liquidity`, `signal`, `strategy`, `execution`, `portfolio`, `quarantine`

---

## Resumo: 10 Maiores Riscos Estruturais

| # | Risco | Severidade | Impacto | Recomendação |
|---|-------|-----------|---------|--------------|
| **1** | **NATS single-stream para 10 domains** | CRÍTICO | Back-pressure em qualquer domain (ex: `aggregation` burst) afeta delivery de `marketdata` ao client. Failure isolation inexistente. | Separar em pelo menos 3 streams: `marketdata`, `aggregation+evidence+insights+liquidity`, `signal+strategy+execution+portfolio`. |
| **2** | **`/healthz` com Guardian snapshot dependency** | ALTO | Liveness probe faz actor request com 5s timeout. Guardian sobrecarregado → healthz timeout → Kubernetes restart → cascata. | Healthz DEVE ser `200 OK` incondicional. Mover health check para `/readyz`. |
| **3** | **Workspace store file-based sem locking** | ALTO | Multi-instance server (scaling horizontal) → race condition em writes. Corrupção silenciosa. | Migrar para TimescaleDB ou Redis; ou adicionar flock/advisory lock. |
| **4** | **`signal/` vs `signals/` module naming** | ALTO | Dois módulos Go com nomes quase idênticos em `go.work`. Import confusion, onboarding friction, merge conflicts. | Consolidar: `signals/` absorve `signal/` ou renomear um deles (e.g., `signalengine/`). |
| **5** | **`SignalEvent` vive em `marketmodel` (shared)** | MÉDIO-ALTO | Domain type de um bounded context (signal) está num módulo compartilhado importado por 5+ BCs. Mudança em `SignalEvent` ripple-effect em todos. | Mover `SignalEvent` para `internal/core/signal/domain/` e expor via interface. |
| **6** | **Executor default `bootstrap_simulated`** | MÉDIO | Config deploy failure → executor roda simulação silenciosamente. Operador pensa que trades estão executando; na verdade são simulados. | Forçar explicit mode declaration; falhar startup se não definido. |
| **7** | **Sem DLQ/retry cross-service no NATS** | MÉDIO | NATS drop entre services (ex: strategist→executor) perde intent sem trace. Sem dead-letter queue configurada. | Implementar consumer com ack + DLQ subject por domain. |
| **8** | **Client two-pass JSON sem error propagation** | MÉDIO | JS parse failure na primeira passagem é invisível ao Odin. Silent data loss possível — candle aparece com zeros. | Adicionar error channel JS→Odin com contagem de parse failures visível em health. |
| **9** | **TranscodeCache sem invalidação por schema change** | MÉDIO | Deploy com novo envelope version + cache com entries antigas → client recebe formato incompatível. | Adicionar version tag no cache key ou TTL curto (< 5min). |
| **10** | **Dual freshness sources (backend vs client)** | BAIXO-MÉDIO | Backend `/api/v1/freshness` e client health pipeline computam staleness independentemente com thresholds diferentes. Operador vê "healthy" no Grafana; usuário vê "stale" no UI. | Unificar: client consome freshness do backend OU ambos usam mesmos thresholds configuráveis. |

### Riscos Secundários (P2/P3)

| Risco | Nota |
|-------|------|
| `signals` binary faz evidence + signal | Scaling independente impossível; aceitável até 10x throughput |
| WS `CheckOrigin: always true` | Dev-only concern; precisa fix antes de produção pública |
| Hardcoded health thresholds no client | Funciona, mas operador não pode tunar sem rebuild Odin |
| `Portfolio.Validate()` exige positions não vazio | Impossível representar estado pré-trade; workaround necessário |
| Schema version coupling client↔server | Deploy order matters; sem graceful negotiation |
| Docker `processor` com root user e docker.sock | Security concern para prod; necessário para auto-scaling? Revisar |

---

## Apêndice A: Detalhamento — Execution Governance Gate Sequence

O `GovernedExecutor` (`internal/core/execution/app/governed_executor.go`) implementa 5 gates em sequência fail-closed:

```
StrategyIntentV1 ──→ [1. Idempotency] ──→ [2. Control Plane] ──→ [3. Authorization]
                                                                         │
                     [5. Dispatch] ←── [4. Credential] ←── [Adapter Selection]
```

| Gate | Check | Failure Reason (60+ codes) |
|------|-------|---------------------------|
| 1. Idempotency | 4096-entry cache, 30s TTL | `duplicate_intent` |
| 2. Control Plane | State: Active/Paused/Drained/Halted; strategy/adapter enabled | `control_plane_halted`, `strategy_disabled`, etc. |
| 3. Authorization | Grant scope (venue/symbol/account), limits (TTL, qty, notional, slippage) | `governance_denied`, `scope_denied`, `limit_exceeded` |
| 4. Adapter Selection | Mode/capability match + circuit breaker (5 errors → 30s cooldown) | `adapter_circuit_open`, `adapter_unavailable` |
| 5. Credential | Resolver/provider acceptance, lease validation, scope fitness | `credential_lease_expired`, `scope_denied` |

**Retryable vs Permanent**: Credential lease, material missing, paused/drained, adapter circuit → retryable. Governance denial, scope denial, halted → permanent.

**Audit trail**: Cada intent produz `ExecutionDecisionRecord` com gate results + final decision.

---

## Apêndice B: Detalhamento — Storage Federation (Hot/Cold)

O `FederatedCandleReader` (`internal/adapters/storage/federation/`) roteia queries baseado em time boundary:

```
Query(fromMs, toMs) ──→ route(hotWindowMs)
                             │
                    ┌────────┴──────────┐
                    │                   │
             routeHotOnly        routeColdOnly
           (TimescaleDB)        (ClickHouse)
                    │                   │
                    └────────┬──────────┘
                             │
                    routeBoth + mergeByWindowStart()
                    (dedup: hot wins on overlap)
```

**Pattern**: Mesmo para Stats, Heatmap, Tape, OI, CVD readers.

**ClickHouse batch writer**: 5000 rows / 250ms flush. `ReplacingMergeTree` para idempotent upsert por seq.

---

## Apêndice C: Detalhamento — Client State Pipeline

```
Stream_Apply_State (per-stream, md_common/)
    ├── snapshot_seen[Artifact_Kind]bool        ← latch on first snapshot
    ├── has_live[Artifact_Kind]bool             ← latch on first live event
    ├── recovery_attempts: u8                   ← max → Stale
    ├── getrange_seeded/pending                 ← backfill lifecycle
    └── event_count: u64
         │
         ▼
Snapshot_Lifecycle (pure derivation):
    event_count == 0          → Absent
    recovery >= MAX_ATTEMPTS  → Stale
    snapshot gate unsatisfied → Pending
    recovery active / synthetic → Degraded
    else                      → Live
         │
         ▼
Cell_Surface_View (10 fields, per-cell, app/):
    composition: Composition_Stage (Empty|Range_Pending|Backfilled|Live_Only|Composed)
    has_live_data: bool
    artifact_has_live: [Artifact_Kind]bool
    venue, symbol: string
    stream_bound: bool
    health_level: System_Health_Level
    recovery_attempts: u8
    reliability: Stream_Reliability (7 states, ADR-0032)
    backfill_expectation: Backfill_Expectation (TF-aware, ADR-0034)
         │
         ▼
Data_Readiness (6 variants, per-widget):
    Not_Ready → Loading → Snapshot_Pending → Seeding → Partial_Usable → Live_Usable
         │
         ▼
Pane_Visual_State (8 variants, display):
    Active | Loading | Seeding | Snapshot_Pending | Empty | Offline | Error | Degraded
```

**Invariantes (S158):**
- Pure derivation — zero cached health/reliability state
- Cell_Surface_View ceiling: 10 fields
- Data_Readiness: 6 variants max
- Per-stream store isolation (DOM, Footprint on Market_Stream)
- Layer_Context read-only, strategies stateless

---

## Apêndice D: Detalhamento — WS Consumer Backpressure

O `ws.Consumer` (`internal/actors/marketdata/ws/consumer.go`) implementa:

```
Exchange WS → reconnect loop:
    1. Global jitter (hash-based, max 250ms)
    2. Dial (60s timeout)
    3. Send subscription messages
    4. Start keepalive goroutine (ping/1min)
    5. Start heartbeat goroutine (custom)
    6. Read loop → WsMessage to SubsystemActor
    7. On error: classify → exponential backoff (500ms base, 30s cap)
    8. Retry budget: 20/min, then 30s cooldown
```

**Backpressure no SubsystemActor**:
- `wsQueue` bounded buffer entre WS goroutine e ingest worker
- Policies: `drop_oldest` ou `drop_depth_keep_trades`
- Canonicalization per-exchange adapter (price/size precision)
- InstrumentStream dedup window: 1024 entries, 1h TTL, max 10K streams

---

## Apêndice E: Detalhamento — VPVR Overload Policy

O `VPVREmitPolicy` (`internal/core/insights/app/vpvr_overload_policy.go`) implementa adaptive degradation:

| Level | QueueDepth | Action |
|-------|-----------|--------|
| L0 (normal) | < threshold | Full snapshots + deltas |
| L1 | rising | Compress snapshot (fewer price bins) |
| L2 | high | Degrade cadence (drop every Nth snapshot) |
| L3 (critical) | saturated | Drop deltas entirely |

Signals: QueueDepth, QueueCapacity, BoundedMapOccupancy, ProcessingLatencyMs.

---

## Oportunidades de Refatoração

1. **NATS stream isolation**: split em 3+ streams com retention policies independentes → maior blast radius control
2. **Signal module consolidation**: merge `signal/` + `signals/` em single BC com domain/app/ports
3. **Workspace persistence upgrade**: TimescaleDB table com advisory lock → multi-instance safe
4. **Healthz simplification**: healthz = `200 OK`; toda lógica para readyz
5. **Envelope type safety**: code-gen from protobuf → typed dispatch no consumer/processor, eliminando runtime type assertion
6. **TranscodeCache versioning**: incluir schema_version + envelope_version no cache key
7. **Client error channel**: JS→Odin bridge para parse failures com counter exposto em health dashboard
8. **Execution mode safety**: startup fails se `execution_mode` not explicitly set; remove default
