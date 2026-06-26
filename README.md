# Stream Analytics

[![ci-fast](https://github.com/stream-analytics/stream-analytics/actions/workflows/ci-fast.yml/badge.svg)](https://github.com/stream-analytics/stream-analytics/actions/workflows/ci-fast.yml)

Real-time, multi-exchange cryptocurrency market data platform with an integrated operational cockpit.
Ingests, normalizes, aggregates, and visualizes live market data across 6 exchanges with sub-millisecond latency.

## Quick Start

```bash
make up          # full local stack (NATS/JetStream + consumer + processor + server + store)
make up-infra    # infra only (NATS/JetStream)
make ps
make logs
make down
```

Compose assets: `deploy/compose/docker-compose.yml`, `deploy/nats/nats-server.conf`, `deploy/configs/*.jsonc`, `deploy/docker/*.Dockerfile`.

## Architecture at a Glance

```
Exchange WS (6 venues: Binance spot/futures, Bybit, Coinbase, HyperLiquid, Kraken spot/futures)
    │
    ▼
[consumer]  ──(marketdata.*)──► NATS JetStream ──► [processor]
                                                         │
                                              (aggregation.* / insights.*)
                                                         │
                                                  [store]   [server]
                                                               │
                                                          WS / HTTP
                                                               │
                                                          [client]
```

**Backend (Go, ~131K LOC):** Hexagonal architecture, DDD bounded contexts, Hollywood actor model. 7 binaries, NATS JetStream event bus, TimescaleDB + ClickHouse storage.

**Client (Odin, ~30K LOC):** Cross-platform operational cockpit (WASM + native). 13 widget types, 8 indicators, 3 subplot analytics, orderflow visualization, workspace split-tree.

Full documentation: [`docs/`](docs/README.md).

## Binaries

| Binary | Role |
|--------|------|
| `consumer` | Exchange WebSocket → NATS JetStream ingester |
| `processor` | NATS → Aggregation pipeline (candles, orderbook, stats, tape, heatmaps, VPVR) |
| `server` | HTTP + WebSocket gateway |
| `store` | Storage lifecycle manager (TimescaleDB + ClickHouse) |
| `migrate` | Database migrations (Goose) |
| `emulator` | Test event emitter (Kafka/NATS scenarios) |
| `validator` | JetStream event validator with HTTP endpoint |

## Exchanges

Binance (spot + futures), Bybit, Coinbase, HyperLiquid, Kraken (spot + futures).

Each adapter in `internal/adapters/exchange/{name}/` implements: endpoint, parser, backfill.

## Dev Gates

```bash
make docs-check        # header format, internal links, truth-map integrity
make invariants-check  # domain isolation leak scan
make test-workspace    # full module-based test suite
```
