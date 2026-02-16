# Local development: observability and services

This guide explains how to run the repository locally with lightweight observability (ClickHouse, Prometheus, Grafana) for development and debugging.

Prerequisites:
- Docker & Docker Compose

Start services:

```bash
make up
```

Important local URLs:
- **Server API (ready/metrics)**: http://127.0.0.1:8080
- **Consumer API**: http://127.0.0.1:8081
- **Processor API**: http://127.0.0.1:8082
- **Store API (cold-path)**: http://127.0.0.1:8083
- **NATS monitoring**: http://127.0.0.1:8222
- **Prometheus**: http://127.0.0.1:9090
- **Grafana**: http://127.0.0.1:3000 (admin/admin)
- **ClickHouse HTTP**: http://127.0.0.1:8123

Grafana:
- Dashboards are provisioned from `deploy/observability/grafana/provisioning`.
- Default admin password is `admin` (change for non-local use).

Prometheus:
- Config is at `deploy/observability/prometheus/prometheus.yml` and scrapes the local services and Prometheus itself.

Stopping:

```bash
cd deploy/compose
docker compose down -v
```

Notes:
- The compose setup mounts files from the repository to make iteration fast; changes to dashboards or provisioning are picked up on Grafana restart.
- For operability checks run: `./scripts/check-operability.sh` (requires `promtool` and `jq`).
- For docs link checks run: `./scripts/check-doc-links.sh`

Sanity checks
-------------

After `docker compose up -d` verify the stack is healthy:

- Prometheus healthy: `curl -sSf http://127.0.0.1:9090/-/healthy`
- Prometheus rules loaded: open `http://127.0.0.1:9090/` -> Status -> Rules (or `curl -s http://127.0.0.1:9090/api/v1/rules`).
- Grafana health: `curl -sSf http://127.0.0.1:3000/api/health`
- Grafana login: admin / `GF_SECURITY_ADMIN_PASSWORD` (default `admin`)
- ClickHouse query: `clickhouse-client --host 127.0.0.1 --query 'SELECT 1'` or `curl -sSf 'http://127.0.0.1:8123/?query=SELECT+1'`

Makefile shortcuts
------------------

This repository exposes convenient Makefile targets that wrap common local dev tasks. Use these instead of calling `docker compose` directly when possible.

- Start the full stack (build images): `make up`
- Stop the stack: `make down`
- Start only infra (NATS + ClickHouse): `make up-infra`
- Start infra + app services (no observability): `make up-core`
- Show compose status: `make ps`
- Tail logs: `make logs`
- Bring up the full stack with automatic rebuild: `make up` (includes profiles `core` + `obs`)

Developer checks and gates:

- Run operability checks (Prometheus rules, etc): `make operability-gates` (alias for `./scripts/check-operability.sh`)
- Run docs link checks: `make check-doc-links`
- Install pre-commit hooks locally: `make pre-commit-install`
- Run linter: `make lint`
- Run all workspace tests: `make test-workspace`

Examples:

```bash
# start infra and full stack
make up-infra
make up

# tail logs while debugging
make logs

# run the standard gates locally
./scripts/check-doc-links.sh
./scripts/check-operability.sh
make lint
make test-workspace
```

Reset and troubleshooting
-------------------------

Clean reset (use when ClickHouse skipped init or state is inconsistent):

```bash
make down
docker volume rm market-raccoon-clickhouse-data market-raccoon-prometheus-data market-raccoon-grafana-data || true
make up
```

Grafana provisioning issues
 - If Grafana logs "can't read dashboard provisioning files from directory /etc/grafana/provisioning/dashboards":
   - Ensure `deploy/observability/grafana/provisioning/dashboards/dashboards.yml` exists and references `/var/lib/grafana/dashboards`.
   - Ensure the compose mounts `../observability/grafana/provisioning:/etc/grafana/provisioning:ro` and `../observability/grafana/dashboards:/var/lib/grafana/dashboards:ro`.
   - Restart Grafana: `docker compose restart grafana` (or `make down && make up` if provisioning changed).

Prometheus troubleshooting
 - Confirm Prometheus is scraping targets: `curl -s http://127.0.0.1:9090/targets`
 - Confirm rules loaded: `curl -s http://127.0.0.1:9090/api/v1/rules` or open UI -> Status -> Rules.

Helpful endpoints & creds
 - Grafana: `http://127.0.0.1:3000` — default admin user `admin`, password from `GF_SECURITY_ADMIN_PASSWORD` (default `admin`)
 - Prometheus: `http://127.0.0.1:9090` (`/-/healthy` for health)
 - ClickHouse HTTP: `http://127.0.0.1:8123` (use `clickhouse-client` for reliable checks)

Processor JetStream filter subjects
------------------------------------

The processor's JetStream consumer uses `filter_subjects` to control which
subjects are delivered from the MARKETDATA stream.

Default: `["marketdata.>"]` — receives **all** marketdata event types
(bookdelta, trade, raw, markprice, liquidation, etc.).

The processor actor routes by `env.Type`:
- `marketdata.bookdelta` v1 → UpdateOrderBookFromEvents
- `marketdata.trade` v1 → JoinCrossVenueTrades (when `enable_crossvenue_join: true`)
- `marketdata.raw` v1 → skip (no structured payload)
- anything else → log warn + skip

To restrict delivery to a specific event type, override in config:

```jsonc
"filter_subjects": ["marketdata.bookdelta.>"]   // bookdelta only
"filter_subjects": ["marketdata.bookdelta.>", "marketdata.trade.>"]  // both
"filter_subjects": ["marketdata.>"]              // all (default)
```

When `enable_crossvenue_join` is true, the runtime automatically merges
`join_trades_subject` into the effective filter list (see `effectiveJetStreamFilters`
in `cmd/processor/main.go`).

For sharding implications, see `docs/operations/sharding.md` — the shard key
is derived from `venue + instrument`, so all event types for the same instrument
always go to the same processor replica regardless of filter breadth.

Store (cold-path ClickHouse authority)
--------------------------------------

The store binary is the cold-path writer. It consumes aggregation events from
JetStream and commits them to ClickHouse with ack-on-commit semantics.

- **Port**: 8083 (config: `deploy/configs/store.jsonc`)
- **JetStream durable**: `store-v1`
- **Filter subjects**: `aggregation.snapshot.v1.>`, `aggregation.orderbook_inconsistency.v1.>`

Endpoints:

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` | GET | Liveness probe (always 200) |
| `/readyz` | GET | Readiness gate (503 until schema validated + consumer connected) |
| `/metrics` | GET | Prometheus exposition |
| `/runtime/snapshot` | GET | Guardian state JSON |
| `/runtime/reload` | POST | Reload signal (202) |

Debug checklist:

```bash
# 1. Verify store is healthy
curl -sSf http://127.0.0.1:8083/healthz

# 2. Verify store is ready (schema validated + consumer started)
curl -sSf http://127.0.0.1:8083/readyz

# 3. Check Prometheus is scraping store
curl -s http://127.0.0.1:9090/targets | grep store

# 4. Check commit metrics (should increase when aggregation events flow)
curl -s http://127.0.0.1:8083/metrics | grep store_commit_total

# 5. Check for quarantine (decode failures / poison messages)
curl -s http://127.0.0.1:8083/metrics | grep store_quarantine_total

# 6. Verify ClickHouse has the expected table
curl -s 'http://127.0.0.1:8123/?query=SHOW+TABLES+FROM+default' | grep aggregation

# 7. Query committed snapshots
curl -s 'http://127.0.0.1:8123/?query=SELECT+count()+FROM+default.aggregation_snapshots_v2'
```

Grafana dashboard: **Market-Raccoon Store** (uid: `market-raccoon-store`) — covers
commit rate, commit latency p50/p95, quarantine, flush rate/latency, batch size.
