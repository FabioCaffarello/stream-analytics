# Local development: observability and services

This guide explains how to run the repository locally with lightweight observability (ClickHouse, Prometheus, Grafana) for development and debugging.

Prerequisites:
- Docker & Docker Compose

Start services:

```bash
cd deploy/compose
docker compose up -d
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
