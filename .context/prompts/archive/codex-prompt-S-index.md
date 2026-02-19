# Codex Prompt Orchestration — S-Waves (Production Readiness)

## Overview

5 sequential waves that take Market Raccoon from **complete application layer with in-memory stubs** to **production-grade infrastructure**. Each wave builds on the previous.

## Execution Order

| # | Prompt | Focus | Key Deliverables | Gate |
|---|--------|-------|-------------------|------|
| **S1** | `codex-prompt-S1-storage-drivers.md` | Real storage drivers | pgx pool, clickhouse-go pool, SQLExecutor interface, real writer/reader, DDL | `make test-workspace-race` |
| **S2** | `codex-prompt-S2-ack-on-commit.md` | ACK-on-commit hardening | Conformance tests, commit ordering spy, timeout→NAK, idempotent redelivery | `make test-workspace-race && make invariants-check` |
| **S3** | `codex-prompt-S3-delivery-completion.md` | Delivery subsystem | Hot snapshot, PgRangeStore, DropOldest+PriorityDrop, session bounded queue | `make test-workspace-race` |
| **S4** | `codex-prompt-S4-artifact-writers.md` | All artifact writers | Candle/Stats/Heatmap/VPVR real Timescale+ClickHouse writers, DDL | `make test-workspace-race` |
| **S5** | `codex-prompt-S5-production-hardening.md` | Production hardening | API key auth, TLS, rate limiting, soak tests (1M msgs, 50 slow clients) | `make test-workspace-race` |

## Dependency Graph

```
S1 (real storage drivers)
    ↓
S2 (ACK-on-commit hardening)
    ↓
S3 (delivery completion — needs real storage for GetRange)
    ↓
S4 (artifact writers — needs real pools from S1)
    ↓
S5 (production hardening — soak tests exercise full pipeline)
```

### Why sequential?
- **S1→S2:** ACK conformance tests need real storage to prove commit order
- **S2→S3:** GetRange needs real Timescale queries; snapshot relies on committed state
- **S3→S4:** Artifact writers use the same pool infrastructure from S1; wiring extends S3 delivery
- **S4→S5:** Soak tests in S5 exercise the full pipeline including all real writers from S4

## Pre-flight Check (before starting S1)

```bash
# Verify current state is green
make test-workspace
make test-workspace-race
make docs-check
make invariants-check

# Verify storage stubs exist and are in-memory
grep -r "IsProductionReady" internal/adapters/storage/ | grep false
grep -r "AdapterMode" internal/adapters/storage/ | grep stub
```

## Post-flight Check (after completing all 5)

```bash
make test-workspace
make test-workspace-race
make docs-check
make invariants-check

# Verify storage is production-ready
grep -r "IsProductionReady" internal/adapters/storage/ | grep true

# Verify soak tests exist
go test -list "TestSoak" ./cmd/processor/... ./internal/interfaces/ws/...

# Verify auth + rate limiting
grep -r "AuthConfig" internal/interfaces/ws/
grep -r "RateLimiter" internal/actors/delivery/runtime/
```

## Files Modified Summary

### S1 — Real Storage Drivers (~12 files)

**Modified:**
- `internal/adapters/storage/timescale/writer.go` — replace sync.Map with real pgx
- `internal/adapters/storage/clickhouse/writer.go` — replace sync.Map with real clickhouse-go
- `internal/adapters/storage/timescale/status.go` — `IsProductionReady()=true`
- `internal/shared/config/schema.go` — storage connection config
- `cmd/processor/bootstrap.go` — conditional real vs stub storage
- `cmd/server/bootstrap.go` — conditional real vs stub storage
- `go.work` — if new modules needed

**New:**
- `internal/adapters/storage/timescale/pool.go` — pgx pool wrapper
- `internal/adapters/storage/clickhouse/pool.go` — clickhouse pool wrapper
- `internal/adapters/storage/timescale/pool_test.go`
- `internal/adapters/storage/clickhouse/pool_test.go`
- `migrations/001_orderbook_tables.sql` — DDL for orderbook tables

### S2 — ACK-on-Commit Hardening (~5 files)

**New:**
- `internal/adapters/jetstream/ack_commit_conformance_test.go` — 5 conformance tests
- `internal/adapters/storage/commit_spy_test.go` — commit ordering spy

**Modified:**
- `internal/shared/config/schema.go` — `AckWait` config
- `internal/actors/aggregation/runtime/processor.go` — commit metrics
- `internal/adapters/jetstream/consumer.go` — timeout handling

### S3 — Delivery Completion (~10 files)

**New:**
- `internal/actors/delivery/runtime/session_snapshot_test.go`
- `internal/interfaces/ws/delivery_snapshot_e2e_test.go`

**Modified:**
- `internal/actors/delivery/runtime/session.go` — hot snapshot + bounded queue
- `internal/actors/delivery/runtime/router.go` — verify routing
- `internal/core/delivery/domain/backpressure_policy.go` — DropOldest + PriorityDrop
- `internal/core/delivery/domain/backpressure_policy_test.go` — extend tests
- `internal/adapters/storage/timescale/delivery_range_store.go` — real PgRangeStore

### S4 — Artifact Writers (~10 files)

**New:**
- `internal/adapters/storage/timescale/candle_writer.go`
- `internal/adapters/storage/timescale/stats_writer.go`
- `internal/adapters/storage/timescale/candle_writer_test.go`
- `internal/adapters/storage/timescale/stats_writer_test.go`
- `internal/adapters/storage/clickhouse/candle_writer.go`
- `internal/adapters/storage/clickhouse/stats_writer.go`
- `migrations/002_artifact_tables.sql`

**Modified:**
- `internal/adapters/storage/timescale/heatmap_writer.go` — rewrite from stub
- `internal/adapters/storage/timescale/volume_profile_writer.go` — rewrite from stub
- `cmd/processor/bootstrap.go` — wire all real writers

### S5 — Production Hardening (~10 files)

**New:**
- `internal/interfaces/ws/auth.go` — API key authentication
- `internal/interfaces/ws/auth_test.go`
- `internal/actors/delivery/runtime/rate_limiter.go` — token bucket
- `internal/actors/delivery/runtime/rate_limiter_test.go`
- `cmd/processor/soak_pipeline_test.go` — 1M message soak
- `internal/interfaces/ws/soak_delivery_test.go` — 50 slow clients soak

**Modified:**
- `internal/interfaces/http/server.go` — TLS support
- `internal/interfaces/ws/server.go` — auth integration
- `internal/shared/config/schema.go` — auth + rate limit config
- `internal/shared/metrics/metrics.go` — remaining metric wiring

## Total Commit Chain

```
# S1
feat(s1): add pgx connection pool with health check and graceful shutdown
feat(s1): add clickhouse-go connection pool with batch support
feat(s1): replace timescale orderbook stub with real pgx writer
feat(s1): replace clickhouse orderbook stub with real batch writer
feat(s1): wire real storage pools in processor and server bootstrap
feat(s1): add DDL migration for orderbook snapshot tables

# S2
fix(s2): harden ACK-on-commit boundary with conformance tests

# S3
feat(s3): add hot snapshot on subscribe for all delivery subjects
feat(s3): implement real Timescale GetRange query
feat(s3): add DropOldest and PriorityDrop backpressure policies
test(s3): add delivery snapshot, getrange, and backpressure E2E tests

# S4
feat(s4): add Timescale+ClickHouse candle writers
feat(s4): add Timescale+ClickHouse stats writers
feat(s4): rewrite heatmap+VPVR writers with real SQL
feat(s4): wire all artifact writers in processor bootstrap

# S5
feat(s5): add API key authentication for WS connections
feat(s5): add TLS support for HTTP/WS endpoints
feat(s5): add per-session token bucket rate limiting
feat(s5): complete metrics wiring for all artifact writers
test(s5): add full pipeline soak test (1M messages)
test(s5): add slow client delivery soak test (50 clients)
feat(s5): add funding rate extraction to Binance parser
```

## Important Constraints (Cross-Wave)

1. **go.mod hygiene** — Any new `require` must have corresponding `replace` directive. Run `make tidy` after each wave.
2. **No cross-module imports** — Use shared wire DTOs (like Prompt B pattern) when payload types cross module boundaries.
3. **Feature flags** — Storage pools gated by `cfg.Storage.Timescale.Enabled` / `cfg.Storage.ClickHouse.Enabled`. Auth gated by `cfg.WS.Auth.Enabled`.
4. **Backward compat** — Every wave must leave all existing tests passing. Log stubs remain as fallback when storage is disabled.
5. **Soak tests behind `testing.Short()`** — Must not slow CI.
6. **Metrics cardinality** — Follow `docs/observability/metrics-policy.md`.
7. **`*problem.Problem` at boundaries** — Adapters wrap `error` to `*problem.Problem` at the port boundary.
