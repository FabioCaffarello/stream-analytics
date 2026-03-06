# Strategy / Execution / Portfolio Contracts (Stage 3)

**Status:** Active
**Owner:** Architecture / Runtime Platform
**Last updated:** 2026-03-06
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/semantic-hardening-stage1.md`, `docs/architecture/boundary-hardening-stage2.md`, `docs/architecture/real-adapter-integration-stage7.md`, `docs/architecture/real-execution-lifecycle-stage8.md`, `docs/architecture/execution-governance-stage9a.md`, `docs/architecture/credentials-broker-hardening-stage9b.md`, `docs/contracts/event-bus.md`, `docs/contracts/subject-registry.yaml`

---

## Goal

Formalize canonical contracts for the next bounded contexts without implementing runtime:

- `strategy.intent`
- `execution.event`
- `portfolio.state`

The result is contract-first readiness for the next runtime stage while preserving Stage 1 semantic freeze and Stage 2 boundary hardening.

## Contracts Introduced

| Contract | Proto | Subject pattern | Owner BC | Producer BC | Primary consumers | Status |
|---|---|---|---|---|---|---|
| `strategy.intent` | `proto/strategy/v1/intent.proto` (`strategy.v1.StrategyIntentV1`) | `strategy.intent.v1.{venue}.{instrument}` | `strategy` | `strategy` | `execution`, `delivery` | draft |
| `execution.event` | `proto/execution/v1/event.proto` (`execution.v1.ExecutionEventV1`) | `execution.event.v1.{venue}.{instrument}` | `execution` | `execution` | `portfolio`, `delivery` | draft |
| `portfolio.state` | `proto/portfolio/v1/state.proto` (`portfolio.v1.PortfolioStateV1`) | `portfolio.state.v1.{venue}.{instrument}` | `portfolio` | `portfolio` | `delivery` | draft |

## Semantic Boundaries

### `signal.event` vs `strategy.intent`

- `signal.event` is a deterministic analytical signal derived from market/evidence context.
- `strategy.intent` is an explicit decision to attempt risk action with side, sizing, constraints, and TTL.
- `signal.event` remains canonical signal stream; it must not carry execution intent semantics as metadata hacks.

### `execution.event` vs `portfolio.state`

- `execution.event` is an immutable fact from execution lifecycle transitions (`accepted` .. `failed`).
- `portfolio.state` is a deterministic projection derived from execution events.
- Execution and projection remain separate contracts to preserve replay/audit clarity.

### `signal.composite` position

- `signal.composite` is retired from strategist operational intake in Stage 6.
- Any residual handling is compatibility-only for historical replay/read paths.
- It is not the semantic anchor for strategy, execution, or portfolio contracts.

## Why This Prepares Next Stage

- Defines stable contract surfaces before executor/portfolio runtime implementation.
- Establishes explicit ownership and subject taxonomy for intent -> execution -> portfolio flow.
- Preserves deterministic replay and auditability via correlation, trace, and sequence/provenance fields.
- Avoids provider-specific coupling in contract layer.
- Current operational wiring exposes these lifecycle streams to `delivery`; durable `storage` wiring is still planned and must not be inferred from the contract alone.

## Stage 5 Runtime Hardening Notes

Runtime remains bootstrap/synthetic, but lifecycle handling is now explicit:

- `execution.event`
  - deterministic accepted/rejected/filled paths;
  - deterministic rejection reason codes (policy-visible);
  - replay-stable seq/event ids preserved.
- `portfolio.state`
  - lifecycle-aware projection from execution stream (`accepted/rejected/filled`);
  - explicit locked/available balances for pending-order bootstrap semantics;
  - basic deterministic realized/unrealized PnL snapshot.

Stage 6 canonical posture:

- canonical strategist input path is `signal.event`;
- `signal.composite` is no longer accepted as strategist runtime input.
- `execution.event` publish path exposes adapter boundary metadata (`execution_boundary`, `execution_adapter`, `execution_mode`) for future real venue integration behind ports.

## Stage 7 Safe Real Adapter Notes

- Real adapter cut-in is now available behind `IntentExecutor` with explicit mode gating:
  - default: `execution.mode=bootstrap_simulated`
  - opt-in: `execution.mode=real_adapter_safe` + `execution.adapter=binance.spot`
- Safe mode is fail-closed and constrained:
  - trade-only semantics only;
  - allowlists for venue/symbol (optional account);
  - deterministic rejection reasons for out-of-scope intents and missing credentials.
- Canonical contracts are unchanged:
  - input remains `strategy.intent`;
  - output remains canonical `execution.event`;
  - `portfolio.state` remains projection derived from `execution.event`.

## Stage 8 Lifecycle/Reconciliation Notes

- Stage 8 keeps the same contracts and boundaries while expanding safe real-adapter lifecycle representation.
- In `safe_order_lifecycle` mode, adapter-observed statuses are deterministically translated into canonical `execution.event` states:
  - `placed`, `partially_filled`, `filled`, `canceled`, `expired`, `failed`.
- Reconciliation polling is bounded and fail-closed; repeated snapshots are deduped before canonical emit.
- `portfolio.state` remains strictly projector-derived from `execution.event`; no exchange API coupling is introduced in portfolio BC.

## Stage 9A Governance Notes

- `IntentExecutor` remains the operational execution boundary consumed by runtime actors.
- Stage 9A inserts explicit governance before the adapter is called:
  - `ExecutionGrant`
  - `AuthorizationDecision`
  - `AdapterSelectionDecision`
  - `CredentialResolution`
- Governance is fail-closed:
  - no grant -> deny;
  - no credential availability -> deny;
  - out-of-scope venue/symbol/account -> deny;
  - unauthorized mode/adapter -> deny.
- Canonical contracts are unchanged:
  - `strategy.intent` is still the only execution input contract;
  - `execution.event` is still the only execution fact contract;
  - `portfolio.state` remains derived only from `execution.event`.

## Stage 9B Credentials Broker Notes

- Stage 9B hardens only the credential boundary behind the existing execution governance layer.
- The core model now distinguishes:
  - credential material availability;
  - credential resolution status;
  - credential provenance;
  - credential lease state/validity.
- The runtime still uses the same operational boundary:
  - `IntentExecutor` for execution;
  - governance before adapter invocation;
  - canonical `execution.event` after the decision.
- Real adapter wiring now uses a broker boundary instead of consuming env providers directly.
- Canonical contracts remain unchanged:
  - no new input contract was introduced;
  - no new execution or portfolio event family was introduced;
  - `execution.event` remains the only execution truth surface.
