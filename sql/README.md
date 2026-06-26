# SQL Schema Management

All database schemas live under `sql/` organized by engine.

## Structure

```
sql/
├── timescale/
│   └── migrations/
│       ├── 0001_s1_orderbook_snapshot.sql   -- Hot-path orderbook
│       ├── 0002_s4_artifact_tables.sql      -- Candle, stats, heatmap, VPVR, delivery
│       ├── 0003_delivery_events_unique.sql  -- Unique (subject,seq) for delivery events
│       └── 0004_m2_subminute_retention_policy.sql -- Hot retention cleanup function by timeframe
├── clickhouse/
│   └── migrations/
│       ├── 0001_m1_writer_skeleton.sql      -- Initial cold-path (v1)
│       ├── 0002_w2_cold_correctness.sql     -- Correctness hardening (v2)
│       ├── 0003_w4_ttl_partition.sql        -- TTL + monthly partitioning (v3)
│       ├── 0004_s1_orderbook_cold.sql       -- Orderbook cold aligned with hot
│       ├── 0005_s4_artifact_tables.sql      -- Candle + stats cold
│       ├── 0006_m8_heatmap_cold.sql         -- Heatmap cold table
│       └── 0007_m2_subminute_ttl_policy.sql -- Candle/stats cold TTL by timeframe (1s/5s guardrail)
└── README.md
```

## Application Policy

- Schemas are applied **externally** before the application starts.
- No runtime migration execution — binaries assume tables exist.
- ClickHouse migrations are auto-applied via `docker-entrypoint-initdb.d` mount.
- TimescaleDB migrations are applied via init container in docker-compose.

## Local Development

```bash
# Start infrastructure (includes TimescaleDB + ClickHouse with auto-init)
make up-infra

# Verify tables exist
docker exec stream-analytics-timescale psql -U raccoon -d raccoon -c '\dt'
docker exec stream-analytics-clickhouse clickhouse-client --query 'SHOW TABLES'
```

## Naming Convention

Files follow: `{NNNN}_{wave}_{description}.sql`

- `NNNN` — sequential 4-digit number
- `wave` — origin wave (m1, w2, w4, s1, s4)
- `description` — short snake_case description
