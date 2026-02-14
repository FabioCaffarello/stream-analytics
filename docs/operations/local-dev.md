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

- Start the full stack (build images): `make docker-up` or `make up`
- Stop the stack: `make docker-down` or `make down`
- Start only infra (NATS): `make up-infra`
- Show compose status: `make ps`
- Tail logs: `make logs`
- Bring up the full stack with automatic rebuild: `make up` (equivalent to `docker compose up --build -d`)

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
