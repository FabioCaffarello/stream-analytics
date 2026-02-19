# ADR-0019 - Dual-Database Operational Strategy

**Status:** Accepted
**Implementation status:** Fully Implemented
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-19
**Date:** 2026-02-19
**Deciders:** Chief Architect
**Relates to:** ADR-0006 (Storage Hot vs Cold), RFC-0001, `sql/timescale/migrations/`, `sql/clickhouse/migrations/`, `internal/shared/config/schema.go`

---

## Contexto

ADR-0006 estabeleceu a decisao de separar hot-path (in-memory) de cold-path (persistent). Desde entao, ambos os drivers de armazenamento foram implementados (S1-S5):

- **TimescaleDB** (pgx driver): orderbook snapshots, candle/stats/heatmap/VPVR artifacts, delivery event range queries.
- **ClickHouse** (clickhouse-go driver): cold analytics, historical aggregations, ReplacingMergeTree com dedup.

Com ambos operacionais em producao, e necessario formalizar a estrategia operacional dual-database: quando usar cada um, como monitorar, como operar em cenarios de falha, e quais invariantes devem ser mantidos.

## Decisao

### 1. Topologia de armazenamento

| Tier | Tecnologia | Finalidade | Latencia alvo |
|------|-----------|-----------|---------------|
| Hot | In-memory (ring buffers) | Real-time delivery, WS snapshot | < 1ms |
| Warm | TimescaleDB (PostgreSQL 16) | Operational reads, GetRange, metadata | < 10ms |
| Cold | ClickHouse 24.8 | Historical analytics, bulk queries, cold artifacts | < 100ms |

### 2. Workload routing

- **Writes**: Todos os artifacts sao gravados em ambos os DBs simultaneamente (dual-write). O ack-on-commit garante durabilidade antes de confirmar ao produtor.
- **Hot reads** (real-time delivery): In-memory ring buffers, nunca consulta DB.
- **Range reads** (`GetRange`): TimescaleDB (indexed por `(venue, instrument, ts_ingest)`).
- **Historical queries** (backfill, gap detection, analytics): ClickHouse (partitioned, TTL-managed).
- **Cold reads** (cold readers): ClickHouse com `FINAL` dedup para ReplacingMergeTree.

### 3. Configuracao independente

Ambos sao opt-in via `config.AppConfig.Storage`:

```jsonc
{
  "storage": {
    "timescale": { "enabled": true, "dsn": "..." },
    "clickhouse": { "enabled": true, "addrs": ["..."] }
  }
}
```

Cada driver pode ser desabilitado independentemente. O sistema degrada gracefully: sem TimescaleDB, `GetRange` retorna vazio; sem ClickHouse, cold readers retornam vazio.

### 4. DDL management

- DDL versionado em `sql/timescale/migrations/` e `sql/clickhouse/migrations/`.
- Auto-init via `docker-entrypoint-initdb.d` volumes no docker-compose.
- Sem migration runner automatizado (manual apply + init scripts).
- Invariante: DDL sempre aplicado em ordem lexicografica (prefixo `0001_`, `0002_`, etc.).

### 5. Failure isolation

- TimescaleDB e ClickHouse sao failure domains independentes.
- Falha em um nao afeta o outro nem o hot-path in-memory.
- Healthchecks separados: `pg_isready` (TimescaleDB), `SELECT 1` via HTTP (ClickHouse).
- `/readyz` do servidor agrega ambos os healthchecks quando habilitados.

## Consequencias

- Positivas:
  - Isolamento de latencia: hot-path nunca bloqueado por cold queries.
  - Separacao de workloads: OLTP (TimescaleDB) vs OLAP (ClickHouse) otimizados para cada caso.
  - Escalabilidade independente: cada DB escala conforme seu workload.
  - Graceful degradation: desabilitar um DB nao derruba o sistema.

- Negativas:
  - 2x overhead operacional: monitoring, backup, capacity planning para dois DBs.
  - DDL coordination: alteracoes de schema precisam ser aplicadas em ambos (quando relevante).
  - Dual-write latencia: write path tem latencia do DB mais lento.
  - Sem migration runner automatizado (debt aceito, mitigado por init scripts).

## Invariantes

- `INV-STO-01`: Todo write de artifact deve ser ack-on-commit em ambos os DBs habilitados antes de confirmar ao produtor.
- `INV-STO-02`: TimescaleDB e ClickHouse sao failure domains independentes — falha em um nao deve afetar o outro.
- `INV-STO-03`: DDL deve ser versionado em `sql/` com prefixo numerico sequencial e aplicado na ordem.
- `INV-STO-04`: Cold readers ClickHouse devem usar `FINAL` para dedup de ReplacingMergeTree.
- `INV-STO-05`: In-memory hot-path nunca deve depender de disponibilidade de DB para real-time delivery.

## Implementation Matrix

| Feature | Status | Referencia |
|---|---|---|
| TimescaleDB pgx driver | Implemented | `internal/adapters/storage/timescale/` |
| ClickHouse driver | Implemented | `internal/adapters/storage/clickhouse/` |
| Opt-in storage config | Implemented | `internal/shared/config/schema.go:StorageConfig` |
| DDL TimescaleDB (orderbook + artifacts) | Implemented | `sql/timescale/migrations/0001_*.sql`, `0002_*.sql` |
| DDL ClickHouse (snapshots + artifacts) | Implemented | `sql/clickhouse/migrations/0001_*` to `0006_*` |
| Docker-compose auto-init | Implemented | `deploy/compose/docker-compose.yml:29-54,208-237` |
| Healthcheck TimescaleDB | Implemented | `pg_isready -U raccoon -d raccoon` |
| Healthcheck ClickHouse | Implemented | `clickhouse-client SELECT 1` |
| Cold readers with FINAL dedup | Implemented | `internal/adapters/storage/clickhouse/readers.go` |
| Ack-on-commit conformance tests | Implemented | `internal/adapters/jetstream/*_test.go` |

## Runbook

### Health monitoring

```bash
# TimescaleDB
pg_isready -h localhost -p 5432 -U raccoon -d raccoon

# ClickHouse
curl -s 'http://localhost:8123/ping'
clickhouse-client --port 9000 --query 'SELECT 1'
```

### Capacity indicators

| Metrica | TimescaleDB | ClickHouse |
|---------|------------|------------|
| Disk usage | `pg_database_size('raccoon')` | `SELECT total_bytes FROM system.tables WHERE database='raccoon'` |
| Active connections | `SELECT count(*) FROM pg_stat_activity` | `SELECT count(*) FROM system.processes` |
| Oldest partition | N/A (no partitioning) | `SELECT partition FROM system.parts WHERE active ORDER BY partition LIMIT 1` |

### Failure scenarios

| Cenario | Impacto | Acao |
|---------|---------|------|
| TimescaleDB down | `GetRange` retorna vazio; writes falham para Timescale; hot-path nao afetado | Restart container; verificar `pg_isready` |
| ClickHouse down | Cold readers retornam vazio; writes falham para ClickHouse; hot-path nao afetado | Restart container; verificar `/ping` |
| Ambos down | Hot-path in-memory continua funcionando; writes falham; range/cold queries vazios | Restart ambos; verificar `/readyz` |
| Disk full (TimescaleDB) | Writes falham com `SQLSTATE 53100` | Expandir disco ou `VACUUM FULL` |
| Disk full (ClickHouse) | Writes falham; TTL nao limpa | Expandir disco; forcar `OPTIMIZE TABLE ... FINAL` |

### Backup strategy

- **TimescaleDB**: `pg_dump -Fc raccoon` (custom format, comprimido).
- **ClickHouse**: `clickhouse-backup` ou `ALTER TABLE ... FREEZE` para snapshots de particao.
- **Frequencia recomendada**: daily para ambos; retention alinhado com TTL do ClickHouse (90 dias).

## Evidence

- Storage drivers:
  - `internal/adapters/storage/timescale/*.go`
  - `internal/adapters/storage/clickhouse/*.go`
- Config schema:
  - `internal/shared/config/schema.go:StorageConfig`
- DDL:
  - `sql/timescale/migrations/`
  - `sql/clickhouse/migrations/`
- Docker integration:
  - `deploy/compose/docker-compose.yml`
- Cold readers:
  - `internal/adapters/storage/clickhouse/readers.go`

## Changelog

- 2026-02-19:
  - ADR criado. Formaliza estrategia dual-database que ja estava operacional desde S1-S5.
  - Documenta invariantes, runbook, failure scenarios.
