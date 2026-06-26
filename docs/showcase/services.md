---
description: Service binaries tour — all 7 Go binaries, their roles, actor trees, ports, and health endpoints under the Hollywood Guardian runtime.
---

# Service Binaries

Stream Analytics ships 7 service binaries, each supervised by the Hollywood Guardian actor runtime.
Guardian provides fault isolation, supervised restart, and structured lifecycle management for every
actor tree in the system.

## Actor Supervision Tree

{% include "../architecture/diagrams/actor-supervision-tree.md" %}

---

## `consumer`

| Attribute | Value |
|-----------|-------|
| Role | Exchange data ingestion |
| Port | `:8081` (health check) |
| Key Actors | `ExchangeActor`, `DeduplicationActor` |
| Health Endpoint | `GET /healthz` |
| NATS output | `marketdata.>` |

The consumer maintains persistent WebSocket connections to all 7 configured exchanges. Each
exchange runs in its own `ExchangeActor` with independent reconnect logic — exponential backoff
on disconnect, gap-fill replay on reconnect. `DeduplicationActor` filters duplicate events using
`idempotency_key` before events are canonicalised into the CMM and published to NATS JetStream.

---

## `processor`

| Attribute | Value |
|-----------|-------|
| Role | Aggregation + insights + evidence detection |
| Port | `:8082` (internal) |
| Key Actors | `AggregationActor`, `InsightsActor`, `EvidenceActor` |
| Health Endpoint | `GET /healthz` |
| NATS input | `marketdata.>` |
| NATS output | `aggregation.>`, `insights.>`, `evidence.>` |

The processor is the computation engine. It consumes `marketdata.>` from NATS and produces:

- **OHLCV candles** across 9 timeframes (1s → 4h)
- **Stats aggregation**: funding rate, liquidations, mark price, open interest
- **Heatmap snapshots**: price-level volume distributions
- **VPVR levels**: Volume Profile Volume Rate per price level
- **LEL evidence**: 5 stateful liquidity detection rules (wall detection, sweep, stack, etc.)

---

## `server`

| Attribute | Value |
|-----------|-------|
| Role | HTTP/WebSocket gateway + backfill |
| Port | `:8080` |
| Key Actors | `DeliveryActor`, `BackfillActor` |
| Health Endpoint | `GET /healthz`, `GET /readyz` |
| WebSocket | `ws://localhost:8080/ws` (proxied via nginx on `:8090`) |

The server implements the Terminal_V1 WebSocket protocol. It manages per-client subscriptions,
enforces backpressure policies, and serves historical data via `getrange`, `getlast`, and `resync`
operations. Rate limiting and per-stream coherence run at the delivery layer. Admin endpoints
expose operational controls under `/api/`.

---

## `store`

| Attribute | Value |
|-----------|-------|
| Role | Persistence — TimescaleDB + ClickHouse |
| Port | `:8083` (health check) |
| Key Actors | `StoreActor`, `FederationActor` |
| Health Endpoint | `GET /healthz` |
| NATS input | `aggregation.>` |

The store binary subscribes to `aggregation.>` events and fans out writes to the 3-tier storage
federation: L0 in-memory ring buffer (burst absorption), L1 TimescaleDB (hot queries), L2
ClickHouse (cold analytical scans). The federation layer handles write errors per tier
independently — an L2 error does not block L1.

---

## `migrate`

| Attribute | Value |
|-----------|-------|
| Role | Schema migration |
| Port | None (runs to completion) |
| Health Endpoint | N/A — exit code 0 = success |

A one-shot binary that applies TimescaleDB and ClickHouse schema migrations before the rest of the
stack starts. In Docker Compose, `server`, `processor`, and `store` declare `depends_on: migrate:
condition: service_completed_successfully`.

---

## `emulator`

| Attribute | Value |
|-----------|-------|
| Role | Synthetic event injection |
| Port | Configurable |
| Health Endpoint | `GET /healthz` |

The emulator generates synthetic market data events at configurable rates and injects them into the
NATS bus. Used for load testing, soak harness execution, and development without live exchange
connectivity.

---

## `validator`

| Attribute | Value |
|-----------|-------|
| Role | Contract and schema validation |
| Port | `:8089` (health check) |
| Health Endpoint | `GET /healthz` |

The validator subscribes to event streams and checks that published envelopes conform to versioned
contract schemas. Validation failures are reported as metrics and logged with structured context.
Used in CI (`make invariants-check`) and as a live sidecar in staging environments.
