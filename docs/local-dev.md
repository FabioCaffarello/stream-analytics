# Local Development: Full Backend via Docker Compose

Single source of truth for running the Stream Analytics backend locally. All other docs reference this file for setup; runbooks assume the stack described here is running.

## Prerequisites

| Tool | Min Version | Check |
|------|-------------|-------|
| Docker Engine | 24.x | `docker --version` |
| Docker Compose (v2 plugin) | 2.20+ | `docker compose version` |
| Go toolchain | 1.22+ | `go version` |
| Make | any | `make --version` |
| `promtool` (optional, for alert validation) | 2.50+ | `promtool --version` |
| `jq` (optional, for operability checks) | 1.6+ | `jq --version` |

Free ports required: `4222` (NATS), `5432` (TimescaleDB), fixed app ports `8080-8081`, `8083` (store), `8089` (validator), one dynamic host port for `processor:8082`, `8123/9000` (ClickHouse), `8222` (NATS monitor), `9090` (Prometheus), `3000` (Grafana), `19092` (Kafka host). Analytics profile: `8091` (Flink UI), `3001` (Metabase).

## Quick Start

```bash
# Full stack — infra + canonical runtime pipeline + observability
make up

# Or step by step:
make up-infra    # NATS + TimescaleDB + ClickHouse + Prometheus + Grafana
make up-core     # + consumer + processor + server + store + validator
```

Wait ~30-60 s for all services to become healthy, then verify:

```bash
make ps          # all services should show "healthy" or "running"
```

## Architecture (what runs)

```
 exchanges
    │
    ├──▶ WebSocket feeds
    │         │
    ▼         │ [analytics path, best-effort]
 consumer :8081 ──▶ NATS JetStream (marketdata.>)       └──▶ Kafka :9092
    │                       │                                       │
    │               processor :8082 ──▶ aggregation.>, insights.>  │
    │                       │                    │           Flink SQL :8091
    │                   store :8083              │               │
    │                (ClickHouse cold)       server :8080    TimescaleDB
    │                                     (WS delivery)    analytics schema
    │                                                            │
 validator :8089                                          Metabase :3001
 (JetStream schema validation)                           (analytics profile)

 Infra:
 - NATS JetStream :4222 / :8222
 - Kafka (Redpanda) :9092 / :19092
 - TimescaleDB    :5432
 - ClickHouse     :8123 / :9000

Observability: Prometheus :9090 → scrapes runtime services configured in the local stack
               Grafana    :3000 → dashboards auto-provisioned

Note: Kafka starts with every `make up`. Flink + Metabase require `--profile analytics` (see Analytics Profile section below).
```

## Service Endpoints

| Service | Port | Health | Readiness | Metrics |
|---------|------|--------|-----------|---------|
| **server** | 8080 | `/healthz` | `/readyz` | `/metrics` |
| **consumer** | 8081 | `/healthz` | `/readyz` | `/metrics` |
| **processor** | `docker compose port processor 8082` | `/healthz` | `/readyz` | `/metrics` |
| **store** | 8083 | `/healthz` | `/readyz` | `/metrics` |
| **validator** | 8089 | `/healthz` | `/readyz` | — |
| **NATS** | 4222 (client) / 8222 (monitor) | `http://127.0.0.1:8222/healthz` | — | — |
| **Flink UI** | 8091 (analytics profile) | `http://127.0.0.1:8091/overview` | — | — |
| **Metabase** | 3001 (analytics profile) | `http://127.0.0.1:3001/api/health` | — | — |
| **TimescaleDB** | 5432 | `pg_isready -U raccoon -d raccoon` | — | — |
| **ClickHouse** | 8123 (HTTP) / 9000 (native) | `http://127.0.0.1:8123/ping` | — | — |
| **Prometheus** | 9090 | `http://127.0.0.1:9090/-/healthy` | — | — |
| **Grafana** | 3000 | `http://127.0.0.1:3000/api/health` | — | — |

All app binaries also expose `/runtime/snapshot` (guardian state JSON) and `/runtime/reload` (POST, 202).

`processor` publishes container port `8082` on a Docker-assigned host port so `PROCESSOR_REPLICAS>1` can scale without port collisions. Resolve the current host mapping with `docker compose -f deploy/compose/docker-compose.yml --env-file deploy/envs/local.env --profile core port processor 8082`.

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
PROC_URL="http://$(docker compose -f deploy/compose/docker-compose.yml --env-file deploy/envs/local.env --profile core port processor 8082 | head -n1 | sed 's#^0.0.0.0:#127.0.0.1:#; s#^\\[::\\]:#127.0.0.1:#')"

# 1. Canonical pipeline readiness probes pass
curl -sf http://127.0.0.1:8080/readyz && echo "server: OK"
curl -sf http://127.0.0.1:8081/readyz && echo "consumer: OK"
curl -sf "${PROC_URL}/readyz" && echo "processor: OK"
curl -sf http://127.0.0.1:8083/readyz && echo "store: OK"
curl -sf http://127.0.0.1:8089/readyz && echo "validator: OK"

# 2. Infra healthy
curl -sf http://127.0.0.1:8222/healthz  && echo "nats: OK"
curl -sf 'http://127.0.0.1:8123/ping'   && echo "clickhouse: OK"
pg_isready -h 127.0.0.1 -U raccoon -d raccoon && echo "timescale: OK"

# 3. Prometheus scraping targets
curl -s http://127.0.0.1:9090/api/v1/targets | jq '.data.activeTargets | length'
# expect runtime targets configured for the local stack

# 4. Consumer is ingesting (counter should increase)
curl -s http://127.0.0.1:8081/metrics | grep ingest_messages_total

# 5. Processor is aggregating
curl -s "${PROC_URL}/metrics" | grep orderbook_update_total

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
PROC_URL="http://$(docker compose -f deploy/compose/docker-compose.yml --env-file deploy/envs/local.env --profile core port processor 8082 | head -n1 | sed 's#^0.0.0.0:#127.0.0.1:#; s#^\\[::\\]:#127.0.0.1:#')"

# Guardian state — shows active subsystems
curl -s "${PROC_URL}/runtime/snapshot" | jq .

# Key metrics
curl -s "${PROC_URL}/metrics" | grep -E 'orderbook_update_total|candle_|stats_|crossvenue_'
```

Filter subjects default to `marketdata.>` (all event types). Override in `deploy/configs/processor.jsonc` under `jetstream.filter_subjects`.

### Validator (schema validation)

```bash
# Readiness — 200 means JetStream consumer connected and ready
curl -sf http://127.0.0.1:8089/readyz && echo "validator: OK"

# Key metrics
curl -s http://127.0.0.1:8089/metrics | grep -E 'dataplane_validation'
```

JetStream consumer durable: `validator-v1`. Filter subjects: `dataplane.message.>`. See [`docs/operations/validator.md`](operations/validator.md) for full contract.

### Analytics Pipeline (requires `--profile analytics`)

```bash
# Start analytics profile (Flink + Metabase)
make up-analytics

# Check Flink jobmanager
curl -s http://127.0.0.1:8091/overview | jq .taskmanagers

# Check Metabase health
curl -sf http://127.0.0.1:3001/api/health && echo "metabase: OK"

# Check TimescaleDB analytics schema is populated
psql -h 127.0.0.1 -U raccoon -d raccoon -c "SELECT count(*) FROM analytics.fact_trades;"
```

See [`docs/architecture/analytics-pipeline.md`](architecture/analytics-pipeline.md) for full pipeline documentation.

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
| `make up-core` | Start infra + canonical runtime services (no Grafana/Prometheus) |
| `make ps` | Show compose service status |
| `make logs` | Tail all compose logs |
| `make docker-build` | Build images without starting |
| `make up-analytics` | Start infra + core + analytics profile (Flink + Metabase) |
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
docker volume rm stream-analytics-clickhouse-data || true          # force-remove data volume
make up                                                          # reinit from scratch
```

### TimescaleDB tables missing

Same pattern — init scripts in `sql/timescale/migrations/` run only on first `docker-entrypoint-initdb.d` mount:

```bash
docker volume rm stream-analytics-timescale-data || true
make up
```

### Port conflicts

If a port is already in use, compose fails at startup. Check:

```bash
lsof -i :4222 -i :5432 -i :8080 -i :8081 -i :8082 -i :8083 -i :8089 -i :8123 -i :9000 -i :9090 -i :3000
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

If targets show `down`, check that the app containers are on the `stream-analytics-network` and that Prometheus config (`deploy/observability/prometheus/prometheus.yml`) references the correct container names.

### Full reset

When state is inconsistent and targeted fixes don't help:

```bash
make down
docker volume rm stream-analytics-clickhouse-data stream-analytics-timescale-data \
  stream-analytics-nats-data stream-analytics-prometheus-data stream-analytics-grafana-data 2>/dev/null || true
make up
```

## Odin Client Cacheless Debug (Port 8090)

Use this runbook when validating client-side regressions with a clean browser state and no network cache.

```bash
# 1) Rebuild/validate local widget liveness against the local backend
make -C client check-widgets-online

# 2) Generate cacheless runtime probes baseline (writes to .context/evidence/)
MR_URL=http://127.0.0.1:8090 npm --prefix tests/playwright run m1:baseline

# 3) Run full E2E regression pack
npx --prefix tests/playwright playwright test tests/playwright/e2e
```

Expected signals:
- `check-widgets-online: PASS` with `conn=Connected` and `health=OK`.
- Probe report shows deterministic TF deltas and stable `stream_count` across TF switches.
- E2E suite passes without desync failures.

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

## GitOps / Kubernetes Secrets Setup

The `deploy/gitops/clusters/*/` directories contain SOPS-encrypted secret placeholders
(`secrets.enc.yaml`). These are **not encrypted yet** — they carry a `# PLACEHOLDER` header
and must be encrypted before any real deployment.

### Prerequisites

```bash
# Install age (key generation) and sops
brew install age sops        # macOS
apt install age sops         # Debian/Ubuntu
```

### Per-environment key generation

Each cluster environment needs its own age key pair. The public key goes in
`deploy/gitops/.sops.yaml`; the private key is stored outside the repo.

```bash
# Generate a key for local development
age-keygen -o ~/.config/sops/age/keys.txt

# Print the public key to add to .sops.yaml
age-keygen -y ~/.config/sops/age/keys.txt
```

Update `deploy/gitops/.sops.yaml` with the new public key under the correct
`path_regex` entry for the target environment.

### Populating and encrypting secrets

1. Fill in real values in the placeholder file (replace `CHANGE_ME`):

```bash
# Edit the local data-management secrets
$EDITOR deploy/gitops/clusters/local/data-management/secrets/secrets.enc.yaml

# Edit the local app secrets
$EDITOR deploy/gitops/clusters/local/stream-analytics-system/secrets/secrets.enc.yaml
```

2. Encrypt in-place with SOPS:

```bash
sops --encrypt --in-place deploy/gitops/clusters/local/data-management/secrets/secrets.enc.yaml
sops --encrypt --in-place deploy/gitops/clusters/local/stream-analytics-system/secrets/secrets.enc.yaml
```

3. Repeat for `staging/` and `prod/` using the respective environment keys. CI/CD
   stores the staging and prod private keys in GitHub Secrets
   (`SOPS_AGE_KEY_STAGING`, `SOPS_AGE_KEY_PROD`).

> Never commit unencrypted secrets. The `infra-gates` CI job validates that all
> `*.enc.yaml` files are actually encrypted before merge.

### CD pipeline image tag updates

After a version tag push (`v*.*.*`), the CD pipeline builds images and pushes them
to GHCR. To update the image tags in `staging` and `prod` gitops overlays, run:

```bash
VERSION=1.2.3    # match the pushed semver tag
GITOPS=deploy/gitops/apps/stream-analytics-system

for svc in stream-analytics-consumer stream-analytics-processor \
           stream-analytics-server stream-analytics-store stream-analytics-migrate; do
  for env in staging prod; do
    (cd "$GITOPS/$svc/overlays/$env" && kustomize edit set image \
      "ghcr.io/stream-analytics/$svc:$VERSION")
  done
done
```

Commit the result as a `chore(gitops): bump images to v$VERSION` commit to `main`.
ArgoCD will detect the change and roll out the new version automatically.

## Related Documentation

- [Cold-Path Runbook](operations/cold-path-runbook.md) — store alerts, degradation scenarios, ClickHouse operations
- [Degradation Contract](operations/degradation.md) — ClickHouse failure propagation and mitigation
- [Sharding Guide](operations/sharding.md) — horizontal scaling of processors
- [Shard Incidents](operations/shard-incidents.md) — shard-related alert playbooks
- [Cold-Path Runbook](operations/cold-path-runbook.md) — per-subsystem incident response (all assume this stack is running)
