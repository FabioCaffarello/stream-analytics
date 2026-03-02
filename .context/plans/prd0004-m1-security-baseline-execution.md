---
status: pending
progress: 0
generated: 2026-02-28
updated: 2026-02-28
title: "PRD-0004 M1 — Security Baseline Execution"
owner: "codex"
workflow: PREVC
phase: P
objective: "Fechar todos os gaps P0 de seguranca do backend: TLS, JWT auth, secrets SOPS, hardening de credenciais de banco, CORS e audit logging."
scope:
  include:
    - "TLS termination via nginx (FR-1)"
    - "JWT authentication com golang-jwt/jwt/v5 (FR-2)"
    - "Token refresh inline via WS (FR-3)"
    - "Secrets loading from env vars / SOPS (FR-4)"
    - "Database credential hardening (FR-5)"
    - "CORS origin validation (FR-6)"
    - "Audit logging para auth events (FR-18)"
  exclude:
    - "CBOR encoding (M2)"
    - "B-Tree orderbook (M3)"
    - "Backup/restore scripts (M4)"
    - "Client-side auth changes"
references:
  - "docs/prds/PRD-0004-backend-evolution-production-hardening.md"
  - "docs/adrs/ADR-0020-gitops-secrets-management.md"
  - "docs/adrs/ADR-0007-delivery-ws-sessions.md"
  - "docs/architecture/system-invariants.md"
---

# PRD-0004 M1 — Security Baseline Execution

> Fechar o gap de seguranca 2/5 → 4/5: TLS, JWT, secrets encriptados, DB hardening, CORS e audit logging.

## Current State (baseline)

| Asset | Status | File |
|---|---|---|
| API key auth (static) | Working | `internal/interfaces/ws/auth.go` |
| TLS config schema | Wired, not deployed | `internal/shared/config/schema.go:192-198`, `internal/interfaces/http/server.go:155-161` |
| WS CheckOrigin | **OPEN** (accepts all) | `internal/interfaces/ws/server.go` upgrader config |
| nginx proxy | HTTP only (8090) | `deploy/nginx/client.conf` |
| SOPS scaffold | ADR-0020 accepted, placeholders | `deploy/gitops/.sops.yaml` |
| DB credentials | Hardcoded plaintext | `deploy/configs/server.jsonc:81,93` |
| Audit logging | None | — |
| JWT | None | — |
| Auth tests | 8 tests (6 WS + 2 HTTP) | `internal/interfaces/ws/auth_test.go`, `internal/interfaces/http/auth_test.go` |

## Dependencies

| Dependency | Type | Status |
|---|---|---|
| `docs/adrs/ADR-0020-gitops-secrets-management.md` | informs | done |
| `docs/adrs/ADR-0007-delivery-ws-sessions.md` | informs | done |
| `golang-jwt/jwt/v5` | new library | pending |
| ADR for JWT auth model | blocks S2 | pending (create as part of S1) |

## Execution Slices

### S0 — Env Var Config & Secrets Foundation (P0)

**Goal:** Config loader resolves secrets from env vars; zero plaintext passwords in JSONC.

**Deliverables:**
- [ ] `internal/shared/config/loader.go` — env var substitution in string fields (pattern: `${ENV_VAR}` or `$ENV_VAR`)
- [ ] `internal/shared/config/loader_test.go` — tests for env var resolution (happy path, missing var, partial)
- [ ] `deploy/configs/server.jsonc` — replace hardcoded passwords with `${TIMESCALE_DSN}`, `${CLICKHOUSE_PASSWORD}`, `${WS_API_KEYS}`
- [ ] `deploy/configs/processor.jsonc` — same treatment
- [ ] `deploy/envs/local.env` — add all secrets (dev values, gitignored)
- [ ] `deploy/compose/docker-compose.yml` — verify `env_file` injection works for substituted config
- [ ] Validate: `make test MODULE=./internal/shared`

**Files touched:**
- `internal/shared/config/loader.go`
- `internal/shared/config/loader_test.go`
- `internal/shared/config/schema.go` (doc comments only)
- `deploy/configs/server.jsonc`
- `deploy/configs/processor.jsonc`
- `deploy/envs/local.env`

**Gate:**
```bash
make test MODULE=./internal/shared    # env var substitution tests pass
make test-workspace                   # no regressions
```

---

### S1 — TLS Termination via nginx (P0)

**Goal:** All HTTP/WS traffic encrypted. Plaintext redirected to HTTPS.

**Deliverables:**
- [ ] `scripts/ops/gen-dev-certs.sh` — generate self-signed dev cert+key (openssl, gitignored output)
- [ ] `deploy/nginx/ssl/` — directory for dev certs (gitignored)
- [ ] `deploy/nginx/client.conf` — add HTTPS listener (443→8443 mapped), HTTP→HTTPS redirect, TLS 1.2+ only, strong ciphers, HSTS header
- [ ] `deploy/compose/docker-compose.yml` — mount cert volume into nginx, expose 8443
- [ ] Security headers: add `Content-Security-Policy`, `Strict-Transport-Security`
- [ ] Validate: `curl -k https://localhost:8443/healthz` returns 200

**Files touched:**
- `deploy/nginx/client.conf`
- `deploy/compose/docker-compose.yml`
- `scripts/ops/gen-dev-certs.sh` (new)
- `.gitignore` (add `deploy/nginx/ssl/`)

**Gate:**
```bash
curl -k https://localhost:8443/healthz          # HTTPS works
curl -sS http://localhost:8090 -o /dev/null -w '%{http_code}'  # 301 redirect
make test-workspace                              # no regressions
```

**Decision:** nginx TLS termination (not Go-native). Rationale: cert rotation via `nginx -s reload`, K8s ingress parity, separates TLS concern from application.

---

### S2 — JWT Authentication (P0)

**Goal:** JWT-based auth with configurable expiry, API key fallback for backward compat.

**Deliverables:**
- [ ] Add `golang-jwt/jwt/v5` to `internal/interfaces` go.mod (or whichever module owns the ws/http packages)
- [ ] `internal/interfaces/ws/auth_jwt.go` — JWT validator: parse+validate Bearer token, extract claims (client_id, exp, iat)
- [ ] `internal/interfaces/ws/auth.go` — extend `Authenticate()`: try Bearer JWT first, fallback to API key
- [ ] `internal/interfaces/http/token_handler.go` — `POST /auth/token` endpoint: accepts API key in body → returns signed JWT with configurable expiry (default 24h)
- [ ] `internal/shared/config/schema.go` — add `JWTConfig { SigningKey string, Expiry string, Issuer string }` to WSConfig
- [ ] `internal/shared/config/loader.go` — validate JWT config (signing key required when JWT enabled)
- [ ] `cmd/server/bootstrap.go` — wire token endpoint + pass JWT config to auth
- [ ] Tests: JWT validation (valid, expired, bad signature, missing claims), token issuance, auth fallback
- [ ] Benchmark: `BenchmarkJWTValidation` — target < 50us p95

**Files touched:**
- `internal/interfaces/ws/auth.go` (modify)
- `internal/interfaces/ws/auth_jwt.go` (new)
- `internal/interfaces/ws/auth_test.go` (extend)
- `internal/interfaces/http/token_handler.go` (new)
- `internal/interfaces/http/token_handler_test.go` (new)
- `internal/shared/config/schema.go` (modify)
- `internal/shared/config/loader.go` (modify)
- `internal/shared/config/loader_test.go` (extend)
- `cmd/server/bootstrap.go` (modify)
- `deploy/configs/server.jsonc` (add jwt section)
- `deploy/envs/local.env` (add JWT_SIGNING_KEY)

**Gate:**
```bash
make test MODULE=./internal/interfaces      # JWT tests pass
make test MODULE=./internal/shared          # config validation
go test -bench BenchmarkJWT -benchmem ./internal/interfaces/ws/...  # NF-2
make test-workspace-race                     # no races
```

---

### S3 — Token Refresh via WS (P1)

**Goal:** Active sessions can refresh JWT without disconnecting.

**Deliverables:**
- [ ] `internal/actors/delivery/runtime/session.go` — handle `{"op":"refresh_token","token":"<new_jwt>"}` message
- [ ] Validate new token before accepting; reject with error if invalid/expired
- [ ] Update session's `clientID` and `expiry` in-place
- [ ] Audit log: token refresh event (clientID, old expiry, new expiry)
- [ ] Tests: refresh happy path, refresh with expired token, refresh with invalid signature

**Files touched:**
- `internal/actors/delivery/runtime/session.go` (modify)
- `internal/actors/delivery/runtime/session_test.go` (extend)

**Gate:**
```bash
make test MODULE=./internal/actors    # refresh tests pass
make test-workspace-race
```

---

### S4 — CORS Origin Validation (P1)

**Goal:** WS upgrade validates origin against explicit allowlist. Default: deny all.

**Deliverables:**
- [ ] `internal/shared/config/schema.go` — add `CORSConfig { AllowedOrigins []string }` to WSConfig
- [ ] `internal/shared/config/loader.go` — validate CORS config (warn if empty in prod mode)
- [ ] `internal/interfaces/ws/server.go` — replace `CheckOrigin: func(r) bool { return true }` with origin allowlist check
- [ ] `internal/interfaces/ws/cors.go` (new) — `CheckOriginAllowed(r *http.Request, allowed []string) bool`
- [ ] Tests: allowed origin, blocked origin, wildcard dev mode, missing Origin header
- [ ] `deploy/configs/server.jsonc` — add `ws.cors.allowed_origins: ["*"]` (dev default)

**Files touched:**
- `internal/shared/config/schema.go` (modify)
- `internal/shared/config/loader.go` (modify)
- `internal/interfaces/ws/server.go` (modify)
- `internal/interfaces/ws/cors.go` (new)
- `internal/interfaces/ws/cors_test.go` (new)
- `deploy/configs/server.jsonc` (modify)

**Gate:**
```bash
make test MODULE=./internal/interfaces      # CORS tests pass
make test MODULE=./internal/shared          # config validation
make test-workspace
```

---

### S5 — Database Credential Hardening (P0)

**Goal:** Per-service DB users with least-privilege. Strong passwords via env vars.

**Deliverables:**
- [ ] `sql/timescale/migrations/0005_m1_per_service_users.sql` — create `raccoon_reader` (SELECT only on hot tables) and `raccoon_writer` (INSERT/UPDATE on hot tables)
- [ ] `sql/clickhouse/migrations/0008_m1_per_service_users.sql` — create restricted users for server (read-only) and processor (write-only)
- [ ] `deploy/envs/local.env` — separate DSN per service: `TIMESCALE_DSN_SERVER`, `TIMESCALE_DSN_PROCESSOR`, `CLICKHOUSE_PASSWORD_SERVER`, `CLICKHOUSE_PASSWORD_PROCESSOR`
- [ ] `deploy/configs/server.jsonc` — DSN uses `${TIMESCALE_DSN_SERVER}`
- [ ] `deploy/configs/processor.jsonc` — DSN uses `${TIMESCALE_DSN_PROCESSOR}`
- [ ] Docker-compose init script for creating users on first boot
- [ ] Test: migration applies cleanly (testcontainers or manual)

**Files touched:**
- `sql/timescale/migrations/0005_m1_per_service_users.sql` (new)
- `sql/clickhouse/migrations/0008_m1_per_service_users.sql` (new)
- `deploy/envs/local.env` (modify)
- `deploy/configs/server.jsonc` (modify, already updated in S0)
- `deploy/configs/processor.jsonc` (modify, already updated in S0)
- `deploy/compose/docker-compose.yml` (init scripts for user creation)

**Gate:**
```bash
make test MODULE=./internal/adapters        # storage tests with new users
make test-workspace
```

**Depends on:** S0 (env var substitution in config loader)

---

### S6 — Audit Logging (P1)

**Goal:** All auth events logged as structured JSON for forensics and compliance.

**Deliverables:**
- [ ] `internal/interfaces/ws/audit.go` (new) — `AuditLogger` interface + `StructuredAuditLogger` implementation
- [ ] Log events: `auth_success`, `auth_failure`, `token_issued`, `token_refreshed`, `session_connected`, `session_disconnected`
- [ ] Fields: `timestamp`, `event`, `client_id`, `client_ip`, `key_id` (hashed), `reason` (on failure)
- [ ] Wire into: `auth.go` (success/failure), `token_handler.go` (issuance), `session.go` (connect/disconnect/refresh)
- [ ] Use `slog.Logger` (stdlib) with JSON handler — no new dependency
- [ ] Tests: verify log output contains expected fields

**Files touched:**
- `internal/interfaces/ws/audit.go` (new)
- `internal/interfaces/ws/audit_test.go` (new)
- `internal/interfaces/ws/auth.go` (modify — add audit calls)
- `internal/interfaces/http/token_handler.go` (modify — add audit calls)
- `internal/actors/delivery/runtime/session.go` (modify — add connect/disconnect audit)
- `cmd/server/bootstrap.go` (wire audit logger)

**Gate:**
```bash
make test MODULE=./internal/interfaces      # audit tests pass
make test MODULE=./internal/actors          # session audit tests
make test-workspace-race
```

**Depends on:** S2 (JWT auth), S3 (token refresh)

---

### S7 — SOPS Secrets Encryption (P0)

**Goal:** GitOps secrets encrypted at rest. Zero plaintext in repository.

**Deliverables:**
- [ ] Generate age keypair for local environment
- [ ] `deploy/gitops/clusters/local/raccoon-system/secrets/secrets.enc.yaml` — encrypt with SOPS
- [ ] `deploy/gitops/clusters/prod/raccoon-system/secrets/secrets.enc.yaml` — encrypt with SOPS (strong passwords)
- [ ] `.sops.yaml` — verify creation rules match file paths
- [ ] CI guard: `scripts/ci/guards/check-no-plaintext-secrets.sh` — grep for known secret patterns in tracked files
- [ ] Validate: `sops -d` produces valid K8s Secret YAML

**Files touched:**
- `deploy/gitops/clusters/local/raccoon-system/secrets/secrets.enc.yaml` (encrypt)
- `deploy/gitops/clusters/prod/raccoon-system/secrets/secrets.enc.yaml` (encrypt)
- `deploy/gitops/.sops.yaml` (verify/update)
- `scripts/ci/guards/check-no-plaintext-secrets.sh` (new)
- `Makefile` (add `secrets-check` target)

**Gate:**
```bash
make secrets-check                          # no plaintext secrets in tracked files
sops -d deploy/gitops/clusters/local/raccoon-system/secrets/secrets.enc.yaml  # decrypts
```

**Depends on:** S0 (env var config), S5 (strong passwords defined)

---

### S8 — Integration Gate & Evidence (validation)

**Goal:** Full M1 validation with all slices active. Evidence captured.

**Deliverables:**
- [ ] Smoke test: docker-compose up with TLS + JWT + env-var secrets
- [ ] Verify: `wss://localhost:8443/ws` with Bearer token
- [ ] Verify: API key fallback still works
- [ ] Verify: expired JWT returns 401
- [ ] Verify: wrong origin blocked by CORS
- [ ] Verify: audit log contains auth events
- [ ] `.context/evidence/prd0004-m1-security-baseline-gate-YYYY-MM-DD.md` — evidence artifact
- [ ] Update `docs/architecture/TRUTH-MAP.md` with new security anchors
- [ ] Update `docs/rfcs/EXECUTION-SEQUENCE.md` if needed

**Gate:**
```bash
make test-workspace                          # all module tests pass
make test-workspace-race                     # zero data races
make invariants-check                        # layer isolation preserved
make secrets-check                           # no plaintext secrets
curl -k https://localhost:8443/healthz       # TLS responds
make docs-check                              # documentation gates
```

## Execution Order (dependency chain)

```
S0 (env vars) ──┬──→ S1 (TLS) ──────────────────────────┐
                │                                         │
                ├──→ S2 (JWT) ──→ S3 (refresh) ──┐       │
                │                                 │       │
                ├──→ S4 (CORS) ──────────────────┤       │
                │                                 │       │
                └──→ S5 (DB hardening) ──┐       │       │
                                         │       │       │
                                         ├──→ S6 (audit) │
                                         │               │
                                         └──→ S7 (SOPS) ─┤
                                                          │
                                                          └──→ S8 (gate)
```

**Critical path:** S0 → S2 → S3 → S6 → S8
**Parallel tracks after S0:** S1 (TLS) | S2+S3+S6 (auth chain) | S4 (CORS) | S5+S7 (secrets chain)

## Acceptance Criteria

1. `curl -k https://localhost:8443/healthz` returns 200 (FR-1)
2. `POST /auth/token` with valid API key returns signed JWT (FR-2)
3. WS connect with `Authorization: Bearer <jwt>` succeeds (FR-2)
4. WS connect with expired JWT returns 401 (FR-2)
5. WS `refresh_token` op updates session without disconnect (FR-3)
6. Zero plaintext secrets in config files or Git history (FR-4)
7. Server uses read-only DB user; processor uses write-only (FR-5)
8. WS upgrade from non-allowed origin rejected (FR-6)
9. Auth events appear as structured JSON in server logs (FR-18)
10. `make test-workspace-race` green (NF-10)
11. `make invariants-check` green (NF-9)
12. `BenchmarkJWTValidation` < 50us p95 (NF-2)

## Risks

| Risk | Mitigation |
|---|---|
| JWT signing key rotation complexity | Use symmetric HMAC-SHA256 initially; rotate key with grace period (accept old key for 24h after rotation) |
| Env var substitution breaks existing config loading | S0 is isolated; comprehensive test coverage for edge cases |
| Self-signed certs confuse dev workflow | `gen-dev-certs.sh` script + clear docs; browsers need `--ignore-certificate-errors` |
| Per-service DB users break existing queries | Migration tested in testcontainers; grants are additive (don't remove superuser for rollback) |
| CORS blocks legitimate client in dev | Dev config allows `["*"]`; production uses explicit origins |
| Audit logging adds latency to auth path | Audit is fire-and-forget (async write to slog); no blocking on log flush |

## New Dependencies

| Dependency | Version | Justification |
|---|---|---|
| `golang-jwt/jwt/v5` | latest stable | Industry standard JWT library for Go; 11K+ GitHub stars, actively maintained |

## Estimated Test Count Delta

| Slice | New Tests | Modified Tests |
|---|---|---|
| S0 (env vars) | ~6 | ~2 |
| S1 (TLS) | ~2 | ~1 |
| S2 (JWT) | ~10 | ~4 |
| S3 (refresh) | ~4 | ~1 |
| S4 (CORS) | ~5 | ~1 |
| S5 (DB hardening) | ~3 | ~1 |
| S6 (audit) | ~6 | ~3 |
| S7 (SOPS) | ~1 (CI guard) | ~0 |
| S8 (gate) | ~0 | ~0 |
| **Total** | **~37** | **~13** |

Target: 1,404 + 37 = **~1,441 tests** after M1.
