# Market Raccoon — Canonical Capability Map

**Date:** 2026-03-10
**Scope:** Full system (backend + client), post-S158 consolidation audit
**Bounded Contexts:** 12 (10 backend + 2 client) + shared foundation + adapters

---

## Bounded Contexts

### 1. MARKET DATA

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Ingestão WS de exchanges, normalização canônica, dedup, sequenciamento monotônico, publicação de envelopes |
| **Ownership** | `internal/core/marketdata` + `internal/actors/marketdata` |
| **Entradas** | Raw WS frames (6 exchanges: Binance spot+futures, Bybit, Coinbase, HyperLiquid, Kraken, KrakenF) |
| **Saídas** | `Envelope` (TradeTickV1, BookDeltaV1, etc.) via NATS JetStream |
| **Deps permitidas** | `shared`, `adapters/exchange` |
| **Deps proibidas** | Qualquer outro core/\*, actors/\*, interfaces/\* |
| **Riscos** | Nenhum |

### 2. AGGREGATION

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Consome marketdata.\*; constrói read models: orderbook, candles, stats, tape, OI, delta volume, CVD, bar stats |
| **Ownership** | `internal/core/aggregation` + `internal/actors/aggregation` |
| **Entradas** | Envelopes de marketdata (NATS) |
| **Saídas** | Artifacts (snapshot, candle closed, stats closed, etc.) → hot store + cold store |
| **Deps permitidas** | `shared`, `marketdata` (event source) |
| **Deps proibidas** | delivery, insights, signal, strategy, execution, portfolio |
| **Riscos** | Nenhum |

### 3. DELIVERY

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Gateway WS; fanout de envelopes, subscrições, backfill, backpressure, coerência por stream |
| **Ownership** | `internal/core/delivery` + `internal/actors/delivery` + `internal/interfaces/ws` |
| **Entradas** | Artifacts de aggregation; comandos subscribe/unsubscribe |
| **Saídas** | Envelopes JSON via WS (Terminal_V1), backfill snapshots |
| **Deps permitidas** | `shared`, `RangeStore` port |
| **Deps proibidas** | marketdata, aggregation (core), insights, signal, execution |
| **Riscos** | TranscodeCache (16-shard LRU) no actor — monitorar vazamento de lógica para cache |

### 4. INSIGHTS

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | VPVR, heatmaps, TPO profiles, session volume profiles, cross-venue fusion |
| **Ownership** | `internal/core/insights` + `internal/actors/insights` |
| **Entradas** | Envelopes de trade/orderbook |
| **Saídas** | VolumeProfileSnapshot, HeatmapSnapshot, TPOProfile, SessionVolumeProfile |
| **Deps permitidas** | `shared` |
| **Deps proibidas** | marketdata, aggregation, delivery, signal, execution |
| **Riscos** | Nenhum |

### 5. EVIDENCE

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Detecção determinística de padrões de liquidez: 5 regras (spread explosion, persistent imbalance, liquidity thinning, absorption, sweep) |
| **Ownership** | `internal/core/evidence` + `internal/actors/evidence` |
| **Entradas** | Orderbook snapshots + trade ticks |
| **Saídas** | EvidenceEvent (confidence, regime, features) |
| **Deps permitidas** | `shared` |
| **Deps proibidas** | Qualquer outro core/\* |
| **Riscos** | Nenhum |

### 6. SIGNAL

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Consome evidence; regras determinísticas + rate limiting (token bucket); composição de sinais atômicos em compostos |
| **Ownership** | `internal/core/signal` (atômico) + `internal/core/signals` (composição) + `internal/actors/signals` |
| **Entradas** | EvidenceEvent |
| **Saídas** | Signal emissions (atômicas + compostas) |
| **Deps permitidas** | `shared`, `evidence` (event source) |
| **Deps proibidas** | marketdata, aggregation, delivery, execution |
| **Riscos** | **Leve:** dois pacotes adjacentes (`signal/` + `signals/`). Sem vazamento de tipos, mas merge futuro recomendado |

### 7. STRATEGY

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Consome signal.event; regras de estratégia; emite strategy.intent (planner puro, sem execução) |
| **Ownership** | `internal/core/strategy` + `internal/actors/strategy` |
| **Entradas** | Signal events |
| **Saídas** | Intent (signal\_id, venue, symbol, side, size, reason\_code) |
| **Deps permitidas** | `shared` |
| **Deps proibidas** | marketdata, aggregation, execution, portfolio |
| **Riscos** | Nenhum |

### 8. EXECUTION

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Executor fail-closed: FSM 4 estados (Idle→Authorizing→Executing→Terminal), governance, credential lease, audit trail, simulation |
| **Ownership** | `internal/core/execution` + `internal/actors/execution` |
| **Entradas** | strategy.intent, ControlPlane commands |
| **Saídas** | execution.event (order lifecycle), audit events |
| **Deps permitidas** | `shared`, `strategy` (intent), `portfolio` (state check) |
| **Deps proibidas** | marketdata, aggregation, delivery, insights |
| **Riscos** | Nenhum — fail-closed por design |

### 9. PORTFOLIO

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Projeção de posições com proveniência; reconciliação; read model portfolio.state |
| **Ownership** | `internal/core/portfolio` + `internal/actors/portfolio` |
| **Entradas** | execution.event |
| **Saídas** | PortfolioState (venue, symbol, size, entry, P&L, provenance) |
| **Deps permitidas** | `shared` |
| **Deps proibidas** | marketdata, aggregation, delivery, signal, strategy |
| **Riscos** | Nenhum |

### 10. STORAGE

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Persistência dual: TimescaleDB (hot) + ClickHouse (cold), migrations, federation cost-based routing |
| **Ownership** | `internal/adapters/storage` + `internal/actors/storage` |
| **Entradas** | Artifacts de aggregation/insights |
| **Saídas** | Dados persistidos para cold readers (HTTP API) |
| **Deps permitidas** | `shared`, ports de hot/cold store |
| **Deps proibidas** | Qualquer core/\* diretamente |
| **Riscos** | Nenhum |

### 11. CLIENT RUNTIME (State Pipeline + Transport)

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Streams WS, pipeline de estado 5 camadas (transport→delivery→snapshot→health→reliability), Terminal\_V1, stores fixos, reducers |
| **Ownership** | `md_common/` + `services/` + `ports/` + `layers/` (data\_source, market\_store, reducers) + `platform/web/` |
| **Entradas** | WS frames (Terminal\_V1 JSON), HTTP backfill |
| **Saídas** | Stores populados, Stream\_Reliability (7 estados), Cell\_Surface\_View (10 campos) |
| **Deps permitidas** | `ports` (DI), `util` |
| **Deps proibidas** | `app/` — services/layers nunca importam app |
| **Riscos** | **`layer_strategies.odin` com 68K LOC monolítico** — split futuro recomendado |

### 12. CLIENT APP (Workspace + Widgets + Interaction)

| Aspecto | Detalhe |
|---|---|
| **Responsabilidade** | Frame loop, workspace (Split\_Tree 31 nós), 13 widget kinds, 30+ actions, ECS components, readiness policy, chart interaction, persistence (schema V12) |
| **Ownership** | `app/` (74 files, ~40K LOC) |
| **Entradas** | Input\_State (per-frame), stores via Layer\_Context/Widget\_Data\_Context |
| **Saídas** | Render commands (Canvas2D), workspace persistence, WS subscribe/unsubscribe |
| **Deps permitidas** | `layers/`, `services/`, `md_common/`, `ports/`, `ui/`, `util/` |
| **Deps proibidas** | Nenhum import circular — app é topo da hierarquia |
| **Riscos** | Nenhum — guard rails S158 validados |

---

## Shared Foundation

| Camada | Módulo | Pacotes |
|---|---|---|
| Backend shared | `internal/shared/` | 22 pkgs: problem, result, validation, ids, clock, envelope, codec, hash, naming, metrics, observability, policykit, replay, ds, ticksize, bootstrap, ownership, shardregistry, slo, config, proto, contracts |
| Client util | `client/src/core/util/` | 4: util, protocol, subject |
| Client UI | `client/src/core/ui/` | 9: primitives, commands, layout, controls, styles, fonts, hotkeys, drag\_drop, grid |

---

## Orderflow — Cross-Cutting Concern (NOT a separate BC)

Per ADR-0033/0035, orderflow spans 4 tiers across existing BCs:

| Tier | BC Owner | Data | Client Store | Widget |
|---|---|---|---|---|
| T0 Raw | MarketData | TradeTickV1, BookDeltaV1 | Trades\_Store | Trades Tape |
| T1 Aggregates | Aggregation | TapeWindowV1, OrderBookSnapshotV2, DeltaVolumeWindowV1 | DOM\_Store, Orderbook\_Store, Analytics\_Store | DOM, Orderbook, CVD |
| T2 Derived | Insights | VolumeProfileSnapshotV1, HeatmapCellV1, FootprintCandleV1 (P4) | Footprint\_Store, VPVR\_Store, Heatmap\_Store | Footprint, VPVR, Heatmap |
| T3 Evidence | Evidence | LiquidityEvidence (5 rules) | Evidence ring | Imbalance Overlay |

---

## Dependency Graph

```
                    ┌─────────────┐
                    │   shared    │  ← all BCs depend on this
                    └──────┬──────┘
                           │
         ┌─────────────────┼─────────────────────┐
         │                 │                      │
    ┌────▼────┐     ┌──────▼──────┐        ┌─────▼─────┐
    │marketdata│     │  evidence   │        │  insights  │
    └────┬────┘     └──────┬──────┘        └───────────┘
         │                 │
    ┌────▼────┐     ┌──────▼──────┐
    │aggregation│    │   signal    │
    └────┬────┘     └──────┬──────┘
         │                 │
    ┌────▼────┐     ┌──────▼──────┐
    │ delivery │     │  strategy   │
    └─────────┘     └──────┬──────┘
                           │
                    ┌──────▼──────┐     ┌───────────┐
                    │  execution  │────▶│ portfolio  │
                    └─────────────┘     └───────────┘

    ┌─────────────┐
    │   storage   │  ← adapters (implements ports)
    └─────────────┘

    CLIENT: ports → services → layers → app
            md_common bridges services↔layers
```

---

## Risks & Open Conflicts

| # | Risk | Severity | Context | Suggested Action |
|---|---|---|---|---|
| 1 | `signal/` vs `signals/` — two adjacent packages | Low | Backend. Atomic rules vs composition. No type leakage today | Monitor; merge on next significant change |
| 2 | `layer_strategies.odin` — 68K LOC monolith | Medium | Client layers/. All rendering strategies in one file | Split by Layer\_ID on next rendering refactor |
| 3 | FootprintCandleV1 backend — P4 deferred | Low | Client uses T0 trade ticks directly. Backend T2 aggregation missing | Implement when volume justifies |
| 4 | TranscodeCache in actor | Low | Delivery actor has cache logic (16-shard LRU) | Not urgent — functional and tested |

**No active boundary erosion.** Import invariants (`make invariants-check`) + S158 guard rails enforce boundaries.

---

## Data Pipeline (End-to-End)

```
Exchange WS → [MarketData] → NATS → [Aggregation] → Hot/Cold Store
                                          │                │
                                          ▼                ▼
                                     [Delivery] ──► WS Client
                                          │
                                     [Insights] → VPVR/Heatmap/TPO
```

## Decision Pipeline (End-to-End)

```
Orderbook+Trades → [Evidence] → [Signal] → [Strategy] → [Execution] → [Portfolio]
     5 rules        rate limit    planner    fail-closed    projector
                    composition              FSM 4 states   reconciliation
```

## Client Pipeline

```
WS Terminal_V1 → [ports] → [services/stores] → [layers/reducers] → [app/widgets]
                    │              │                    │                  │
               Input_State    Fixed-cap           Market_Store      Widget_Contract
               MD_Event       zero-alloc          Data_Source       Split_Tree
                              ring buffers        Layer_Strategy    ECS components
```

---

## Metrics Summary

| Metric | Value |
|---|---|
| Backend bounded contexts | 10 |
| Client bounded contexts | 2 |
| Shared foundation packages | 22 (backend) + 13 (client) |
| Exchange adapters | 6 |
| Actor subsystems | 10 (Guardian-supervised) |
| Contract types | 70+ (versioned) |
| Widget kinds | 13 |
| Backend tests | 1,666 |
| Client tests | 1,317 |
| Total LOC | ~131K (backend) + ~122K (client) |
| Throughput (C4 soak) | 117,697 evt/sec |
| Latency p50/p95/p99 | 7us / 13us / 56us |
