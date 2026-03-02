# PRD-0004 — Backend Evolution Phase 1: Production Hardening & Wire Evolution

**Status:** Draft
**Date:** 2026-02-28
**Owner:** Chief Architect
**Relates to:** `docs/prds/PRD-0003-mm-backend-parity.md`, `docs/adrs/ADR-0020-gitops-secrets-management.md`, `docs/adrs/ADR-0007-delivery-ws-sessions.md`, `docs/adrs/ADR-0013-backpressure-overload-policies.md`, `docs/rfcs/RFC-0015-backend-subminute-hardening-rollout.md`, `docs/architecture/TRUTH-MAP.md`

---

## Problem

Market Raccoon achieves 5/5 on architecture, code quality, functionality, and performance (SWOT v8, PRD-0003 validated). However, the platform scores **2/5 on security** and **3/5 on production operations**, creating blockers for professional deployment. Specifically:

1. **No TLS termination** — all WS and HTTP traffic is plaintext.
2. **Primitive authentication** — static API keys in JSONC config; no token expiry, rotation, or RBAC.
3. **Hardcoded credentials** — TimescaleDB (`raccoon:raccoon`) and ClickHouse (`default:password`) in config files.
4. **No backup automation** — manual `pg_dump` only; no scheduled restore-tested snapshots.
5. **JSON-only wire protocol** — WS delivery uses JSON; no binary encoding option, costing 40-60% extra bandwidth vs CBOR.
6. **O(n) orderbook lookups** — map-based price level storage; B-Tree would give O(log n) with range queries.

Competitive analysis shows TradingView, Bookmap, and CoinGlass all deploy TLS + JWT + binary protocols as baseline. Without hardening, MR cannot serve a professional audience or prepare for the Intelligence Layer (Phase 2).

## Goals

1. **G1 — Security baseline.** All traffic encrypted (TLS). Authentication uses JWT with expiry and rotation. Secrets sourced from encrypted store (SOPS/Vault). Database credentials hardened.
2. **G2 — Wire protocol evolution.** CBOR encoding option for WS delivery, reducing payload size 40-60% vs JSON. Per-message deflate compression available.
3. **G3 — Operational maturity.** Automated daily backups (TimescaleDB + ClickHouse) with tested restore procedure. Rolling update automation with zero-downtime. Hot-reload expanded to all config paths.
4. **G4 — Orderbook performance.** B-Tree-based order book with O(log n) lookups and efficient range queries for depth bands. Depth rebalancing (prune levels beyond configurable % from mid-price).
5. **G5 — Per-market configurability.** Stats computation can be selectively enabled/disabled per venue/instrument. Reduces unnecessary aggregation cost for markets without derivatives data.
6. **G6 — Client version management.** Server reports its version and minimum compatible client version on session handshake, enabling coordinated upgrades.

## Non-Goals

- **Order routing or execution.** MR is decision infrastructure, not a trading platform.
- **New exchange integrations.** 6-exchange parity is sufficient for this PRD.
- **Protobuf-on-the-wire for WS delivery.** CBOR is chosen for WS; Protobuf remains for internal bus.
- **Full Kubernetes deployment.** K8s manifests exist (ADR-0020); this PRD hardens the application, not the orchestrator.
- **Intelligence Layer features.** Cross-exchange OB, anomaly detection, alerting are deferred to PRD-0005.
- **Indicator library.** Deferred to PRD-0006 (Analytics Engine).

---

## Requirements

### Functional

| ID | Requirement | Priority | Acceptance Criteria |
|----|-------------|----------|---------------------|
| **FR-1** | TLS termination for all HTTP/WS endpoints | P0 | All connections via `wss://` and `https://`. Plaintext HTTP redirects to HTTPS. nginx config or Go-native TLS with cert/key path config. |
| **FR-2** | JWT authentication replacing static API keys | P0 | `POST /auth/token` issues JWT with configurable expiry (default 24h). WS handshake accepts `Authorization: Bearer <jwt>`. Expired tokens rejected with 401. API key auth remains as fallback for backward compat. |
| **FR-3** | Token refresh without reconnect | P1 | WS message `{"op":"refresh_token","token":"<new_jwt>"}` updates session auth inline. Session not dropped during refresh. |
| **FR-4** | Secrets loaded from SOPS-encrypted files or env vars | P0 | Database credentials, JWT signing key, and API keys loaded from `SOPS_*` env vars or `*.enc.yaml` files. Zero plaintext secrets in JSONC config. Fallback to env vars for docker-compose dev. |
| **FR-5** | Database credentials hardened | P0 | TimescaleDB and ClickHouse use strong passwords (min 32 chars, random). Per-service DB users with least-privilege (processor: write-only; server: read-only + write for getrange ack). |
| **FR-6** | CORS explicitly configured for WS origins | P1 | `ws.cors.allowed_origins` config field. Default: deny all. Dev override: `["*"]`. Production: explicit origin list. |
| **FR-7** | CBOR encoding option for WS delivery | P1 | `ws.encoding` config field accepts `"json"` (default) or `"cbor"`. CBOR uses `fxamacker/cbor/v2`. Per-session negotiation via subscribe message field `"encoding":"cbor"`. |
| **FR-8** | Per-message WS compression (permessage-deflate) | P2 | `ws.compression.enabled` config field. Uses RFC 7692 permessage-deflate. Configurable compression level (1-9, default 6). Context takeover optional. |
| **FR-9** | B-Tree order book with depth rebalancing | P1 | `OrderBook` uses `tidwall/btree` for price levels. `RebalanceDepth(midPrice, bandPct)` prunes levels outside +-N% of mid. Range query `LevelsInRange(low, high)` returns sorted levels in O(log n + k). |
| **FR-10** | Depth band configurable per instrument | P2 | `processor.orderbook.depth_band_pct` config field (default 10%). Instruments can override via `markets[].depth_band_pct`. |
| **FR-11** | Per-market stats enable flag | P1 | `processor.stats.markets` config field: allowlist of `venue/instrument` pairs. Empty = all enabled (current behavior). Non-matching markets skip stats aggregation. |
| **FR-12** | Automated daily backup (TimescaleDB) | P1 | `scripts/ops/backup-timescale.sh` runs `pg_dump` with compression to configured path (local or S3). Cron-compatible. Retention: 7 daily + 4 weekly. Restore script `restore-timescale.sh` tested. |
| **FR-13** | Automated daily backup (ClickHouse) | P1 | `scripts/ops/backup-clickhouse.sh` runs `clickhouse-client --query "BACKUP"` or table export. Same retention policy. Restore script tested. |
| **FR-14** | Rolling update support | P1 | `scripts/ops/rolling-update.sh` performs zero-downtime update: drains connections, waits for in-flight ACKs, restarts service, verifies health before proceeding to next replica. |
| **FR-15** | Hot-reload expanded to all config paths | P2 | `POST /runtime/reload` accepts changes to: backpressure thresholds, rate limits, log level, stats enable flag, orderbook depth band. Validated against running state before apply. |
| **FR-16** | Server version handshake | P1 | Session ack includes `"server_version":"x.y.z"` and `"min_client_version":"x.y.z"`. Client can check compat on connect. |
| **FR-17** | Rate limiting per API key | P2 | `ws.rate_limit.per_key` config: max subscriptions, max messages/sec per authenticated key. Exceeding returns `429 Too Many Requests`. |
| **FR-18** | Audit logging for auth events | P1 | All auth attempts (success/failure), token refresh, session connect/disconnect logged as structured JSON with timestamp, client IP, key/user ID. |

### Non-Functional

| ID | Requirement | Metric |
|----|-------------|--------|
| **NF-1** | TLS handshake overhead < 5ms p95 | `BenchmarkTLSHandshake` |
| **NF-2** | JWT validation < 50us p95 per request | `BenchmarkJWTValidation` |
| **NF-3** | CBOR encoding reduces payload size >= 40% vs JSON | Benchmark with 1000 candle frames: `len(cbor) / len(json) <= 0.60` |
| **NF-4** | CBOR marshal/unmarshal < 500ns p95 per frame | `BenchmarkCBORMarshal` |
| **NF-5** | B-Tree orderbook lookup < 200ns p95 | `BenchmarkBTreeLookup` |
| **NF-6** | B-Tree range query < 1us p95 for 100 levels | `BenchmarkBTreeRangeQuery` |
| **NF-7** | Throughput retention >= 100K evt/sec with all hardening enabled | Soak test (C4 equivalent) |
| **NF-8** | Zero new `fmt.Sprintf` in core/actors paths | Grep verification |
| **NF-9** | All new domain code uses `*problem.Problem` | Import guard test |
| **NF-10** | All new code passes `-race` detector | `make test-workspace-race` |
| **NF-11** | Backup completes within 5 minutes for 10GB dataset | Soak: timed backup run |
| **NF-12** | Restore completes within 10 minutes for 10GB dataset | Soak: timed restore run |
| **NF-13** | Rolling update completes with zero dropped WS connections | Soak: 50 active clients during update |

---

## Milestones

### M1 — Security Baseline (P0)

**Deliverables:**
- TLS config in nginx reverse proxy (`deploy/nginx/`) and/or Go-native TLS
- JWT auth middleware (`internal/interfaces/ws/auth_jwt.go`)
- Token endpoint (`POST /auth/token`)
- Secrets loading from env vars / SOPS files
- Database credential hardening (per-service users)
- CORS configuration
- Audit logging for auth events

**Gate:**
```bash
make test-workspace             # FR-1 through FR-6, FR-18 tests pass
make test-workspace-race        # zero data races
make invariants-check           # layer isolation preserved
curl -k https://localhost:8443  # TLS responds
```

**Acceptance:**
- All WS connections via `wss://`; plaintext rejected
- JWT issued with 24h expiry; expired token returns 401
- Zero plaintext secrets in config files or environment dump
- Database users have least-privilege grants
- Auth events logged as structured JSON

---

### M2 — Wire Protocol Evolution (P1)

**Deliverables:**
- `internal/shared/codec/cbor_codec.go` — CBOR marshal/unmarshal using `fxamacker/cbor/v2`
- Per-session encoding negotiation in subscribe message
- `codec.Registry` extended with CBOR option
- Permessage-deflate support in WS upgrade handler
- Benchmark suite comparing JSON vs CBOR payload sizes

**Gate:**
```bash
make test-workspace             # FR-7, FR-8 tests pass
go test -bench BenchmarkCBOR -benchmem ./internal/shared/codec/...  # NF-3, NF-4
```

**Acceptance:**
- CBOR encodes candle frame in <= 60% of JSON size
- CBOR marshal < 500ns p95
- Client can request CBOR via `"encoding":"cbor"` in subscribe
- Permessage-deflate reduces bandwidth further (measured)
- JSON remains default; CBOR is opt-in

---

### M3 — B-Tree Order Book (P1)

**Deliverables:**
- `internal/core/aggregation/domain/orderbook_btree.go` — B-Tree-backed OrderBook
- `RebalanceDepth(midPrice, bandPct)` — prune stale levels
- `LevelsInRange(low, high)` — efficient range query
- Depth band config per instrument
- Existing orderbook tests migrated; all pass unchanged

**Gate:**
```bash
make test-workspace             # FR-9, FR-10 tests pass
go test -bench BenchmarkBTree -benchmem ./internal/core/aggregation/domain/...  # NF-5, NF-6
make test-workspace-race
```

**Acceptance:**
- `OrderBook.Apply(delta)` uses B-Tree internally
- `LevelsInRange` returns sorted slice in O(log n + k)
- `RebalanceDepth` removes levels outside band
- Golden replay test produces identical orderbook snapshots
- Benchmark: lookup < 200ns, range < 1us for 100 levels

---

### M4 — Operational Maturity (P1)

**Deliverables:**
- `scripts/ops/backup-timescale.sh` + `scripts/ops/restore-timescale.sh`
- `scripts/ops/backup-clickhouse.sh` + `scripts/ops/restore-clickhouse.sh`
- `scripts/ops/rolling-update.sh` — zero-downtime rolling restart
- Per-market stats enable flag in processor config
- Hot-reload expansion (`POST /runtime/reload` for all config paths)
- Server version handshake in session ack
- Rate limiting per API key

**Gate:**
```bash
make test-workspace             # FR-11 through FR-17 tests pass
scripts/ops/backup-timescale.sh && scripts/ops/restore-timescale.sh  # NF-11, NF-12
make soak-check                 # NF-7: throughput retained
```

**Acceptance:**
- Backup scripts run unattended; retention policy enforced
- Restore produces byte-identical data for fixture set
- Rolling update tested with 50 active WS clients; zero drops
- Per-market stats flag respected; disabled markets skip aggregation
- Hot-reload accepts backpressure/rate-limit/log-level changes
- Session ack includes server version
- Per-key rate limiting enforced; 429 on exceed

---

### M5 — Soak Validation & Documentation (P1)

**Deliverables:**
- Full soak test with all hardening enabled (TLS + JWT + CBOR + B-Tree + backup)
- Updated runbooks: TLS cert rotation, JWT key rotation, backup/restore, rolling update
- Updated architecture docs: TRUTH-MAP, AUTHORITY-MAP
- Evidence artifacts in `.context/evidence/prd0004-*`
- Production deployment checklist (validated)

**Gate:**
```bash
make ci                         # full pipeline green
make soak-check                 # NF-7, NF-13
make docs-check                 # documentation gates pass
make invariants-check           # all invariants hold
```

**Acceptance:**
- Soak: >= 100K evt/sec with all hardening enabled
- Soak: 50 slow clients stable with backpressure + TLS + JWT
- All runbooks tested against docker-compose stack
- Production deployment checklist: all items green
- Zero regressions in existing 1,404+ tests

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| TLS overhead degrades WS delivery latency | Medium — could add 1-5ms per handshake | TLS termination at reverse proxy (nginx); Go-native TLS as fallback. Benchmark before/after. |
| JWT library introduces CVE | High — auth bypass | Pin version; govulncheck in CI; use well-maintained `golang-jwt/jwt/v5`. |
| CBOR encoding breaks client compatibility | High — existing clients fail | CBOR is opt-in per session; JSON remains default. Feature flag. |
| B-Tree migration changes orderbook behavior | High — delivery regression | Golden replay tests validate identical output. Parallel run (old + new) in soak. |
| Backup script fails silently | Medium — data loss | Backup script returns non-zero on failure; Prometheus alert on missing daily backup. |
| Rolling update drops in-flight ACKs | Medium — duplicate processing | Drain phase waits for all pending ACKs (configurable timeout). JetStream handles redelivery. |
| CBOR library allocation pressure | Low — perf regression | `fxamacker/cbor/v2` is zero-alloc for common types. Benchmem gate. |
| Config hot-reload races with running pipeline | Medium — inconsistent state | Reload acquires write lock; validates new config before swap; atomic pointer update. |
| Per-market stats flag creates config drift | Low — ops confusion | Clear documentation; `make config-validate` script checks consistency. |

---

## Success Metrics

- **Security score:** 2/5 -> **4/5** (TLS + JWT + secrets + DB hardening + audit logging)
- **Production ops score:** 3/5 -> **4.5/5** (backup, rolling updates, hot-reload, version mgmt)
- **WS payload reduction:** >= 40% with CBOR (measured vs JSON baseline)
- **Orderbook lookup latency:** map O(n) -> B-Tree O(log n) (measured < 200ns)
- **Throughput retention:** >= 100K evt/sec with all hardening enabled
- **Test count:** >= 1,500 total tests (up from 1,404)
- **Zero regressions:** All existing 1,404 tests continue to pass
- **Backup reliability:** 100% success rate over 7-day trial; restore tested

---

## Dependencies

| Dependency | Type | Status |
|------------|------|--------|
| `golang-jwt/jwt/v5` | Library | New dependency |
| `fxamacker/cbor/v2` | Library | New dependency |
| `tidwall/btree` | Library | New dependency |
| `internal/shared/codec/` | Code | Existing (JSON + Proto; extend with CBOR) |
| `internal/core/aggregation/domain/orderbook.go` | Code | Existing (map-based; migrate to B-Tree) |
| `internal/interfaces/ws/auth.go` | Code | Existing (API-key; extend with JWT) |
| `deploy/nginx/client.conf` | Config | Existing (HTTP; add TLS) |
| ADR-0020 (SOPS/Vault) | Decision | Accepted; Phase 1 in progress |
| ADR-0007 (WS Sessions) | Decision | Accepted |
| ADR-0013 (Backpressure) | Decision | Accepted |
| TimescaleDB + ClickHouse | Infra | Running (docker-compose) |
| NATS JetStream | Infra | Running |
| PRD-0003 (MM Parity) | PRD | Implemented (all M1-M5 validated) |

---

## Competitive Context

This PRD addresses the security and operational gaps identified in the competitive benchmark:

| Capability | Before PRD-0004 | After PRD-0004 | TradingView | Bookmap |
|------------|-----------------|----------------|-------------|---------|
| TLS/HTTPS | None | Full | Full | Full |
| Auth model | Static API key | JWT + API key | OAuth2 | License key |
| Wire encoding | JSON only | JSON + CBOR | Binary | Binary |
| WS compression | None | permessage-deflate | Yes | N/A |
| Backup automation | Manual | Daily automated | Managed | N/A |
| Rolling updates | Manual restart | Zero-downtime | Managed | N/A |
| Config hot-reload | Partial (proto flags) | Full | N/A | N/A |
| OB data structure | HashMap | B-Tree | N/A | B-Tree |

---

## References

- [SWOT v8 Analysis](../../.context/evidence/swot-market-raccoon-v8-2026-02-20.md)
- [PRD-0003 — MM Backend Parity](PRD-0003-mm-backend-parity.md)
- [ADR-0020 — GitOps Secrets Management](../adrs/ADR-0020-gitops-secrets-management.md)
- [ADR-0007 — Delivery WS Sessions](../adrs/ADR-0007-delivery-ws-sessions.md)
- [ADR-0013 — Backpressure Overload Policies](../adrs/ADR-0013-backpressure-overload-policies.md)
- [RFC-0015 — Backend Sub-Minute Hardening](../rfcs/RFC-0015-backend-subminute-hardening-rollout.md)
- [Architecture TRUTH-MAP](../architecture/TRUTH-MAP.md)
- [System Invariants](../architecture/system-invariants.md)
- [MarketMonkey Audit Pack](../../zip/01-marketmonkey-files/marketmonkey/docs/architecture/MARKETMONKEY-AUDIT-PACK.md)

---

## Roadmap Context

This PRD is Phase 1 of a 5-phase backend evolution roadmap:

| Phase | PRD | Focus | Status |
|-------|-----|-------|--------|
| **1** | **PRD-0004** | **Production Hardening & Wire Evolution** | **This document** |
| 2 | PRD-0005 | Intelligence Layer (cross-exchange OB, anomaly detection, smart alerts) | Planned |
| 3 | PRD-0006 | Analytics Engine (indicators, footprint, delta, replay) | Planned |
| 4 | PRD-0007 | Platform Expansion (new exchanges, plugin API, data export) | Planned |
| 5 | PRD-0008 | Multi-Region & Scale (K8s, geo-distributed, consensus) | Planned |
