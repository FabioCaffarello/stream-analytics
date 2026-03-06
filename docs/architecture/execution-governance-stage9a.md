# Stage 9A Execution Governance First

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/real-execution-lifecycle-stage8.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`, `docs/contracts/event-bus.md`

---

## Goal

Introduce an explicit execution-governance layer before any Stage 9B credential-broker hardening.

This stage keeps the canonical chain unchanged:

`signal.event -> strategy.intent -> execution.event -> portfolio.state`

`execution.event` remains the only execution source of truth.
`portfolio.state` remains a derived projection only.

---

## Governance Concepts Introduced

1. `ExecutionGrant`
   - explicit capability/grant model for execution authorization;
   - captures boundary, adapter, mode, trade-only posture, scope allowlists, quantitative limits, and minimal provenance.
2. `AuthorizationDecision`
   - answers whether an intent is authorized to cross the execution boundary under the current grant.
3. `AdapterSelectionDecision`
   - answers which concrete adapter boundary may be used, or why selection was denied.
4. `CredentialRequirement` / `CredentialResolution`
   - separate the question "is the required trade-only credential available?" from both authorization and adapter selection.
5. `GovernedExecutor`
   - keeps runtime consumption at the `IntentExecutor` boundary while forcing governance decisions to happen first.

These concepts live in:

- `internal/core/execution/governance/*`
- `internal/core/execution/ports/governance.go`
- `internal/core/execution/app/static_governance.go`
- `internal/core/execution/app/governed_executor.go`

---

## Boundaries Added

Stage 9A makes three decisions explicit and independently testable:

1. `CapabilityAuthorizer`
   - validates grant existence, safe/trade posture, account/venue/symbol scope, TTL, and quantitative limits.
2. `AdapterSelector`
   - selects the concrete boundary route (`bootstrap.simulated` or `binance.spot`) without resolving secrets.
3. `CredentialResolver`
   - checks credential availability for the selected route and scope without leaking env-var semantics into the core model.

`cmd/executor` is now a composition root for these boundaries instead of a place where the governance semantics themselves live.

---

## Executor Integration

The executor runtime still consumes `strategy.intent` and still emits canonical `execution.event`.

What changed:

- `cmd/executor` now builds:
  - a static execution grant from config,
  - a selector route for the configured adapter,
  - a credential-availability resolver,
  - and a governed `IntentExecutor`.
- `internal/actors/execution/runtime` now defaults to a governed bootstrap executor even when no explicit executor is injected.
- real adapter lifecycle logic remains inside the adapter boundary, but governance denials are decided before the adapter is called.

What did not change:

- no new event family was introduced;
- no strategy semantics were moved into execution;
- no portfolio coupling to exchange APIs was introduced.

---

## Rejection Taxonomy

Stage 9A makes rejection classes explicit via reason prefixes plus `meta.execution_reason_category`:

- `governance_denied_*`
- `credentials_unavailable*`
- `adapter_selection_denied*`
- `rejected_*` for execution-policy rejection inside the executor/adapter
- `failed_*` / `venue_runtime_failed_*` for venue/runtime failure

This clarifies the difference between:

- governance denial,
- missing credentials,
- adapter selection failure,
- execution-policy rejection,
- and venue/runtime failure.

---

## What Stage 9A Does Not Do

1. No Stage 9B credentials broker.
2. No multi-adapter routing expansion.
3. No OMS, custody, or withdraw semantics.
4. No widening of real trading surface.
5. No semantic changes to `strategy.intent`, `execution.event`, or `portfolio.state`.

---

## Validation

```bash
go test ./internal/core/execution/... ./internal/adapters/execution/... ./internal/actors/execution/runtime ./cmd/executor ./internal/shared/config -count=1
```
