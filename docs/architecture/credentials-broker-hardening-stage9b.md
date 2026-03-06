# Stage 9B Credentials Broker Hardening

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/execution-governance-stage9a.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`, `docs/contracts/event-bus.md`

---

## Goal

Harden Stage 9A credential resolution without changing the canonical lifecycle:

`signal.event -> strategy.intent -> execution.event -> portfolio.state`

`execution.event` remains the only execution source of truth.
`portfolio.state` remains a derived projection only.

---

## Concepts Introduced

1. `CredentialAvailabilityStatus`
   - separates raw credential material availability from downstream authorization fitness.
2. `CredentialResolutionStatus`
   - distinguishes `not_required`, `resolved`, and `denied`.
3. `CredentialLease`
   - models `lease_id`, `issued_at`, `valid_until`, and explicit lease state.
4. `CredentialProvenance`
   - models resolver id, provider id, source type/reference, and revocation readiness.
5. `ResolvedCredential`
   - binds the issued lease to boundary, adapter, mode, trade-only scope, and request context.
6. `credentials.Broker`
   - becomes the hardened boundary used by both governance resolution and adapter-side lease acquisition.

These concepts live in:

- `internal/core/execution/governance/*`
- `internal/core/execution/ports/governance.go`
- `internal/adapters/execution/credentials/*`
- `cmd/executor/bootstrap.go`

---

## What Hardened In Stage 9B

1. Credentials are no longer modeled as simple availability booleans.
2. Governance now evaluates:
   - material availability;
   - provenance acceptance (`resolver_id`, `provider_id`);
   - trade-only and scope fitness;
   - lease state and expiry;
   - adapter/mode adequacy.
3. The env-based provider remains concrete infrastructure only.
4. Binance trade API client no longer consumes env providers directly.
   - it consumes the broker boundary plus a static credential requirement.
5. Rejection taxonomy is more explicit:
   - `credentials_unavailable_*`
   - `credentials_invalid_*`
   - `credentials_scope_denied_*`
   - `credentials_lease_*`

---

## What Stage 9B Still Does Not Do

1. No distributed broker or external control plane.
2. No automatic rotation workflow.
3. No multi-tenant credential management.
4. No custody, withdraw, or account-transfer semantics.
5. No advanced multi-adapter governance/routing.

---

## Current Env-Provider Posture

The current provider remains env-backed, but now sits behind a broker that:

- emits explicit provenance;
- mints short-lived in-process usage leases;
- stays trade-only;
- remains fail-closed when resolver/provider/scope/mode do not fit the requirement.

This keeps the implementation small while making revocation and future multi-provider growth easier.

## Runtime Alignment Note

Stage 9C keeps the credentials broker in-process with `cmd/executor`.
Local Docker Compose does not introduce a standalone `credentials-broker` service.

Operationally that means:

- `execution.mode=bootstrap_simulated` remains the default and requires no live credentials;
- `execution.mode=real_adapter_safe` still resolves credentials only through the broker boundary;
- compose/runtime wiring passes `MR_BINANCE_API_KEY` and `MR_BINANCE_API_SECRET` only to the executor container;
- missing or empty env credentials remain fail-closed through the broker/governance path.
