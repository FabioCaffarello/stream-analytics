# Stream Analytics — Product Definition

> Canonical product definition as of 2026-03-10.
> Based on codebase evidence (161K LOC) and Week-1 architectural audit.

---

## One-Line Definition

Stream Analytics is a **real-time, multi-exchange cryptocurrency market data platform** with an integrated operational cockpit — ingesting, normalizing, aggregating, and visualizing live market data across 6 exchanges with sub-millisecond latency.

---

## Expanded Vision

### 1. Multi-Exchange Data Ingestion

WebSocket connections to 6 cryptocurrency exchanges (Binance spot + futures, Bybit, Coinbase, HyperLiquid, Kraken spot, Kraken futures). Raw feeds are normalized into canonical envelopes with monotonic sequencing, idempotency keys, and out-of-order/duplicate detection. The consumer layer publishes normalized events to NATS JetStream for durable, at-least-once delivery.

### 2. Real-Time Aggregation Pipeline

A dedicated processor consumes normalized events and builds derived datasets: OHLCV candles across 9 timeframes (1s to 1d), orderbook snapshots with depth and spread tracking, trading statistics (mark price, funding rates, liquidation volume), and tape with buy/sell volume attribution. All aggregation uses fixed-point arithmetic and UTC-aligned windows.

### 3. Cross-Venue Analytics

Insights engine fuses data across venues to produce volume profiles, heatmaps, TPO (Time-Price Opportunity) profiles, and cross-venue trade snapshots. Multi-venue binning with configurable minimum-venue thresholds ensures analytical integrity.

### 4. Operational Cockpit (Client)

A high-performance client written in Odin, compiled to WebAssembly for web and native targets. 13 widget types — candlestick charts, DOM ladder, footprint charts, orderbook, trade tape, heatmap, VPVR, session VPVR, TPO, analytics subplots, and statistics panels. 8 built-in indicators (MA, Bollinger Bands, VWAP, RSI, MACD, Funding, Liquidations, Trade Counter) plus 3 subplot analytics (CVD, Delta Volume, Open Interest). Workspace architecture with split-tree layouts, per-pane state persistence, and compare mode.

### 5. Stream Health & Reliability

Five-layer health pipeline: transport → delivery → snapshot → health → reliability. Seven-state reliability model (Reliable, Desync, Stale_Recoverable, Stale_Unrecoverable, Offline, Snapshot_Gap, Unknown) derived purely from observed stream behavior. Recovery orchestration with operator-visible trust signals — health dots, reliability badges, and recovery attempt counters.

### 6. Actor-Based Runtime

All backend services run as supervised actors (Hollywood v1.0.5). A Guardian per binary orchestrates subsystem actors (marketdata, aggregation, delivery, insights, evidence) with exponential backoff, restart limits, and circuit-breaking. Each subsystem reports liveness, readiness, and error state independently.

### 7. Dual-Tier Storage & Observability

TimescaleDB (hot, operational metadata) and ClickHouse (cold, analytical aggregates). 100+ Prometheus metrics, 5 Grafana dashboards, 13 alerts, 6 runbooks. Structured JSON logging. 3-tier CI with 8 soak harnesses validated at 117K events/sec sustained throughput.

---

## Current Scope (What the Product Is)

| Capability | Status | Evidence |
|---|---|---|
| Live market data ingestion (6 exchanges) | Production-ready | C4 soak: 10M events, 117K evt/sec |
| Real-time aggregation (candles, stats, orderbook, tape) | Production-ready | 9 timeframes, fixed-point arithmetic |
| Cross-venue analytics (VPVR, heatmaps, TPO) | Production-ready | Multi-venue fusion validated |
| WebSocket delivery with backpressure | Production-ready | Snapshot + streaming, subscriber lag handling |
| Operational cockpit (13 widgets, 8 indicators) | Functional | 1,317 client tests |
| Orderflow visualization (DOM, footprint, trades, orderbook) | Functional | Per-stream store isolation, live updates |
| Workspace management (split-tree, panes, compare) | Functional | Schema v12, 31-node split tree |
| Stream health & reliability model | Functional | 7-state model, 5-layer pipeline |
| Cold storage (TimescaleDB + ClickHouse) | Implemented | Idempotent upserts, Goose migrations |
| Deterministic replay | Implemented | Record/replay for testing and post-analysis |
| HTTP cold reader API | Implemented | `/api/v1/candles`, `/api/v1/stats`, `/api/v1/snapshots` |

---

## Near-Term Scope (Next 3 Months)

| Priority | Item | Rationale |
|---|---|---|
| P0 | Client legacy rendering path removal (Entity_World) | Eliminates dual-path maintenance, unblocks App_State decomposition |
| P0 | Backend `shared/contracts` extraction → `internal/contracts` | Resolves the single critical dependency inversion |
| P1 | `/healthz` unconditional 200 OK (logic → `/readyz`) | Prevents Kubernetes probe-induced restart cascades |
| P1 | NATS stream split (single stream → 3+ domain streams) | Prevents cross-domain backpressure cascades at scale |
| P1 | Documentation governance alignment | AUTHORITY-MAP, TRUTH-MAP, PRD status corrections |
| P2 | Footprint memory soak test (10+ instruments) | Validates orderflow memory footprint at scale |
| P2 | Workspace persistence migration (file → TimescaleDB) | Enables multi-instance deployment |
| P3 | DOM scroll/zoom, price grouping, viewport alignment | Orderflow UX refinements |

---

## What the Product Is NOT

These statements must be retired from any active documentation or narrative:

| Retired Claim | Reality |
|---|---|
| "Client at 55% parity" / "phases 6.8–8.0" | Client has 1,317 tests, 13 widgets, workspace tree, orderflow domain. The `client-roadmap-6.8-to-8.0` describes a world that no longer exists. |
| "RCL Golden Render" as active concept | Legacy framing. Client uses pane-based rendering with compile-time widget contracts. |
| "Simple charting client" | The client is an operational cockpit with orderflow analysis, reliability classification, recovery orchestration, and compare mode. |
| "Execution framework" as an active subsystem | The decision pipeline (signals, strategist, executor, portfolio) was retired in S9. The backend is now a pure market data and analytics platform. |
| Stage reports as reference documents | All stage planning docs have been removed. They described work that is now part of the live codebase. |

---

## Quantitative Snapshot

| Dimension | Value |
|---|---|
| Total LOC | ~161K (131K Go + 30K Odin) |
| Go modules | 26 |
| Backend binaries | 7 |
| Core bounded contexts | 6 (marketdata, aggregation, delivery, insights, evidence, workspace) |
| Active actor subsystems | 5 (marketdata, aggregation, delivery, insights, evidence) |
| Exchange adapters | 6 |
| Client widget kinds | 13 |
| Built-in indicators | 8 + 3 subplot analytics |
| Client tests | 1,317 |
| Backend tests | ~1,666 |
| Soak throughput | 117,697 evt/sec (p50=7us, p95=13us, p99=56us) |
| Uptime validation | 10M events, 4 exchanges, 85s continuous |

---

*This document is the single source of truth for "what is Stream Analytics." Update it when capabilities change. Do not create parallel product definitions.*
