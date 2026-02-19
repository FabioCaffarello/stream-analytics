# Local Development: Full Backend via Docker Compose

Single source of truth for running the Market Raccoon backend locally. All other docs reference this file for setup; runbooks assume the stack described here is running.

## Prerequisites

| Tool | Min Version | Check |
|------|-------------|-------|
| Docker Engine | 24.x | `docker --version` |
| Docker Compose (v2 plugin) | 2.20+ | `docker compose version` |
| Go toolchain | 1.22+ | `go version` |
| Make | any | `make --version` |
| `promtool` (optional, for alert validation) | 2.50+ | `promtool --version` |
| `jq` (optional, for operability checks) | 1.6+ | `jq --version` |

Free ports required: `4222` (NATS), `5432` (TimescaleDB), `8080-8083` (app), `8123/9000` (ClickHouse), `8222` (NATS monitor), `9090` (Prometheus), `3000` (Grafana).

## Quick Start

```bash
# Full stack — infra + all 4 app binaries + observability
make up

# Or step by step:
make up-infra    # NATS + TimescaleDB + ClickHouse + Prometheus + Grafana
make up-core     # + server + consumer + processor + store (builds images)
```

Wait ~30-60 s for all services to become healthy, then verify:

```bash
make ps          # all services should show "healthy" or "running"
```

## Architecture (what runs)

```
                 ┌─────────────┐
   exchanges ──▶ │  consumer   │:8081  (WS ingest → NATS JetStream)
                 └──────┬──────┘
                        │ JetStream: marketdata.>
                 ┌──────▼──────┐
                 │  processor  │:8082  (aggregation, insights, orderbook)
                 └──┬───────┬──┘
      JetStream:    │       │  hot-path
      aggregation.> │       │  (TimescaleDB :5432)
                 ┌──▼──┐ ┌─▼──────┐
                 │store │ │ server │:8080  (WS delivery, /healthz, /readyz)
                 │:8083 │ └────────┘
                 └──┬───┘
                    │ cold-path
              ┌─────▼──────┐
              │ ClickHouse │:8123/:9000
              └────────────┘

Observability: Prometheus :9090 → scrapes all 4 binaries
               Grafana    :3000 → dashboards auto-provisioned
```

## Service Endpoints

| Service | Port | Health | Readiness | Metrics |
|---------|------|--------|-----------|---------|
| **server** | 8080 | `/healthz` | `/readyz` | `/metrics` |
| **consumer** | 8081 | `/healthz` | `/readyz` | `/metrics` |
| **processor** | 8082 | `/healthz` | `/readyz` | `/metrics` |
| **store** | 8083 | `/healthz` | `/readyz` | `/metrics` |
| **NATS** | 4222 (client) / 8222 (monitor) | `http://127.0.0.1:8222/healthz` | — | — |
| **TimescaleDB** | 5432 | `pg_isready -U raccoon -d raccoon` | — | — |
| **ClickHouse** | 8123 (HTTP) / 9000 (native) | `http://127.0.0.1:8123/ping` | — | — |
| **Prometheus** | 9090 | `http://127.0.0.1:9090/-/healthy` | — | — |
| **Grafana** | 3000 | `http://127.0.0.1:3000/api/health` | — | — |

All app binaries also expose `/runtime/snapshot` (guardian state JSON) and `/runtime/reload` (POST, 202).

## Credentials (local only)

Source: `deploy/envs/local.env` — **never use these outside localhost**.

| Service | User | Password | Database |
|---------|------|----------|----------|
| TimescaleDB | `raccoon` | `raccoon` | `raccoon` |
| ClickHouse | `default` | `password` | `default` |
| Grafana | `admin` | `admin` | — |

## Smoke Checklist

Run after `make up` to confirm the full stack is operational:

```bash
# 1. All readiness probes pass
curl -sf http://127.0.0.1:8080/readyz && echo "server: OK"
curl -sf http://127.0.0.1:8081/readyz && echo "consumer: OK"
curl -sf http://127.0.0.1:8082/readyz && echo "processor: OK"
curl -sf http://127.0.0.1:8083/readyz && echo "store: OK"

# 2. Infra healthy
curl -sf http://127.0.0.1:8222/healthz  && echo "nats: OK"
curl -sf 'http://127.0.0.1:8123/ping'   && echo "clickhouse: OK"
pg_isready -h 127.0.0.1 -U raccoon -d raccoon && echo "timescale: OK"

# 3. Prometheus scraping targets
curl -s http://127.0.0.1:9090/api/v1/targets | jq '.data.activeTargets | length'
# expect >= 4 (server, consumer, processor, store)

# 4. Consumer is ingesting (counter should increase)
curl -s http://127.0.0.1:8081/metrics | grep ingest_messages_total

# 5. Processor is aggregating
curl -s http://127.0.0.1:8082/metrics | grep orderbook_update_total

# 6. Store is committing to ClickHouse
curl -s http://127.0.0.1:8083/metrics | grep store_commit_total

# 7. ClickHouse has tables
curl -s 'http://127.0.0.1:8123/?query=SHOW+TABLES+FROM+default' | grep aggregation

# 8. WS delivery accepts connections
# (requires a valid API key — see deploy/configs/server.jsonc ws.auth.api_keys)
```

## Validating Each Binary

### Consumer (ingest)

```bash
# Guardian state — shows WS connections per exchange
curl -s http://127.0.0.1:8081/runtime/snapshot | jq .

# Key metrics
curl -s http://127.0.0.1:8081/metrics | grep -E 'ingest_messages_total|ingest_drop_total|ws_connections_active'
```

The consumer connects to 5 exchanges by default (see `deploy/configs/consumer.jsonc`). If an exchange WS fails, logs show reconnect attempts with exponential backoff.

### Processor (aggregation)

```bash
# Guardian state — shows active subsystems
curl -s http://127.0.0.1:8082/runtime/snapshot | jq .

# Key metrics
curl -s http://127.0.0.1:8082/metrics | grep -E 'orderbook_update_total|candle_|stats_|crossvenue_'
```

Filter subjects default to `marketdata.>` (all event types). Override in `deploy/configs/processor.jsonc` under `jetstream.filter_subjects`.

### Server (WS delivery)

```bash
# Readiness — 200 means guardian + delivery router ready
curl -sf http://127.0.0.1:8080/readyz

# Key metrics
curl -s http://127.0.0.1:8080/metrics | grep -E 'ws_clients_connected|ws_drops_total|ws_send_latency'
```

WS endpoint: `ws://127.0.0.1:8080/ws`. Auth: `Authorization: Bearer <api_key>` (keys defined in `deploy/configs/server.jsonc` → `ws.auth.api_keys`).

### Store (cold-path)

```bash
# Readiness — 200 means schema validated + JetStream consumer connected
curl -sf http://127.0.0.1:8083/readyz

# Key metrics
curl -s http://127.0.0.1:8083/metrics | grep -E 'store_commit_total|store_quarantine_total|store_commit_latency'

# Query committed rows
curl -s 'http://127.0.0.1:8123/?query=SELECT+count()+FROM+default.aggregation_snapshots_v3'
```

JetStream durable: `store-v2`. Filter subjects: `aggregation.snapshot.v1.>`, `aggregation.candle.v1.>`, `aggregation.stats.v1.>`, `insights.heatmap_snapshot.v1.>`, `insights.volume_profile_snapshot.v1.>`.

## Makefile Targets

| Target | What it does |
|--------|-------------|
| `make up` | Build + start full stack (core + obs profiles) |
| `make down` | Stop all services, remove volumes |
| `make up-infra` | Start only infra (NATS, TimescaleDB, ClickHouse, Prometheus, Grafana) |
| `make up-core` | Start infra + app binaries (no Grafana/Prometheus) |
| `make ps` | Show compose service status |
| `make logs` | Tail all compose logs |
| `make docker-build` | Build images without starting |
| `make dev-scale-smoke N=3` | Scale processor to N replicas with shard evidence |

## Sharding (multi-processor)

```bash
# Scale to 3 processor replicas
make dev-scale-smoke N=3

# Or manually:
PROCESSOR_SHARD_COUNT=3 make up-core
```

Each replica uses `SHARD_INDEX` (0-based) from compose `environment`. Subjects are routed by `FNV-1a(venue+instrument) % SHARD_COUNT`. See `docs/operations/sharding.md` for details.

## Troubleshooting

### ClickHouse auth failure

**Symptom:** Store logs `code: 516, message: default: Authentication failed`.

**Fix:** Credentials in `deploy/configs/store.jsonc` must match `deploy/envs/local.env`. Default: `default` / `password`. Verify:

```bash
clickhouse-client --host 127.0.0.1 --port 9000 --user default --password password --query 'SELECT 1'
```

If using HTTP interface:

```bash
curl 'http://127.0.0.1:8123/?user=default&password=password&query=SELECT+1'
```

### ClickHouse tables missing after restart

ClickHouse auto-init runs `sql/clickhouse/migrations/*.sql` only on first volume creation. If the volume already existed when migrations were added, tables are missing.

```bash
make down                                                        # stop + remove volumes
docker volume rm market-raccoon-clickhouse-data || true          # force-remove data volume
make up                                                          # reinit from scratch
```

### TimescaleDB tables missing

Same pattern — init scripts in `sql/timescale/migrations/` run only on first `docker-entrypoint-initdb.d` mount:

```bash
docker volume rm market-raccoon-timescale-data || true
make up
```

### Port conflicts

If a port is already in use, compose fails at startup. Check:

```bash
lsof -i :4222 -i :5432 -i :8080 -i :8081 -i :8082 -i :8083 -i :8123 -i :9000 -i :9090 -i :3000
```

All ports bind to `127.0.0.1` (loopback only). Kill the conflicting process or adjust ports in `deploy/compose/docker-compose.yml`.

### NATS JetStream stream not found

If the consumer or processor fails with "stream not found", the stream was not yet created. The first consumer to connect creates the `MARKETDATA` stream. Restart the consumer:

```bash
docker compose -f deploy/compose/docker-compose.yml restart consumer
```

### Grafana provisioning errors

If Grafana logs "can't read dashboard provisioning files":

1. Verify `deploy/observability/grafana/provisioning/dashboards/dashboards.yml` exists.
2. Verify compose mounts `../observability/grafana/provisioning:/etc/grafana/provisioning:ro`.
3. `docker compose restart grafana`.

### Prometheus not scraping targets

```bash
curl -s http://127.0.0.1:9090/api/v1/targets | jq '.data.activeTargets[] | {job: .labels.job, health: .health}'
```

If targets show `down`, check that the app containers are on the `market-raccoon-network` and that Prometheus config (`deploy/observability/prometheus/prometheus.yml`) references the correct container names.

### Full reset

When state is inconsistent and targeted fixes don't help:

```bash
make down
docker volume rm market-raccoon-clickhouse-data market-raccoon-timescale-data \
  market-raccoon-nats-data market-raccoon-prometheus-data market-raccoon-grafana-data 2>/dev/null || true
make up
```

## Processor JetStream Routing

The processor routes by `env.Type`:

| Event Type | Handler |
|------------|---------|
| `marketdata.bookdelta` v1 | UpdateOrderBookFromEvents |
| `marketdata.trade` v1 | BuildCandleFromEvents + JoinCrossVenueTrades (if enabled) |
| `marketdata.liquidation` v1 | BuildStatsFromEvents |
| `marketdata.markprice` v1 | BuildStatsFromEvents |
| unknown | log warn + skip |

Filter subjects override in config:

```jsonc
"filter_subjects": ["marketdata.bookdelta.>"]                          // bookdelta only
"filter_subjects": ["marketdata.bookdelta.>", "marketdata.trade.>"]    // both
"filter_subjects": ["marketdata.>"]                                    // all (default)
```

When `enable_crossvenue_join` is true, the `join_trades_subject` is automatically merged into the effective filter list.

## Related Documentation

- [Cold-Path Runbook](../../docs/operations/cold-path-runbook.md) — store alerts, degradation scenarios, ClickHouse operations
- [Degradation Contract](../../docs/operations/degradation.md) — ClickHouse failure propagation and mitigation
- [Sharding Guide](../../docs/operations/sharding.md) — horizontal scaling of processors
- [Shard Incidents](../../docs/operations/shard-incidents.md) — shard-related alert playbooks
- [Observability Runbooks](../../docs/observability/runbooks/) — per-subsystem incident response (all assume this stack is running)
- [SLO Definitions](../../docs/observability/slo.md) — SLO targets and PromQL expressions
