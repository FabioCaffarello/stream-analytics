# Stage 8 Real Execution Lifecycle Expansion (Safe Pilot)

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/real-adapter-integration-stage7.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`, `docs/contracts/event-bus.md`

---

## Goal and Context

Expand Stage 7 safe real-adapter integration from `test_order` validation-only into a controlled lifecycle-safe pilot while preserving canonical semantics:

`signal.event -> strategy.intent -> execution.event -> portfolio.state`

`execution.event` remains the only execution source of truth.
`portfolio.state` remains a derived projection only.

---

## Lifecycle Expansion Delivered

Stage 8 real adapter now supports explicit lifecycle mapping in safe pilot mode:

- `accepted`
- `placed`
- `partially_filled`
- `filled`
- `canceled`
- `expired`
- `failed`

`rejected` remains deterministic for boundary/guardrail failures before venue placement.

Stage 8 keeps Stage 7 compatibility mode:

- `execution.real.binance.trade_api.endpoint_mode=test_order` (validation-only; accepted path).
- new opt-in lifecycle mode: `execution.real.binance.trade_api.endpoint_mode=safe_order_lifecycle`.

---

## Deterministic Reconciliation Model

In `safe_order_lifecycle` mode, the Binance adapter now:

1. submits a real testnet order (`/api/v3/order`);
2. emits canonical `accepted`;
3. maps observed venue status to canonical lifecycle state;
4. polls order status (`/api/v3/order`) with bounded reconciliation loop;
5. emits only deterministic lifecycle deltas (dedupe by status + cumulative/leaves progression);
6. closes in terminal state (`filled`, `canceled`, `expired`, `failed`) or emits deterministic timeout failure.

Status mapping is explicit and fail-closed:

- `NEW|PENDING_NEW|PENDING_CANCEL -> placed`
- `PARTIALLY_FILLED -> partially_filled`
- `FILLED -> filled`
- `CANCELED -> canceled`
- `EXPIRED -> expired`
- `REJECTED -> failed`
- unknown status -> `failed` (`failed_unknown_venue_status`)

---

## Portfolio Projection Update

Portfolio projector remains fully event-derived and now handles cumulative partial-fill progression more faithfully:

- tracks pending order cumulative fill;
- applies fill deltas from cumulative progression (not only `last_fill_qty`);
- dedupes repeated partial snapshots without double-counting position/cash;
- keeps locked balances coherent through `placed -> partially_filled -> terminal`.

No direct exchange dependency was added to portfolio.

---

## Safe Pilot Guardrails Preserved

Stage 7 guardrails remain mandatory:

- `execution.mode=real_adapter_safe` is opt-in;
- `execution.safe_mode=true`;
- `execution.trade_only=true`;
- allowlists required (`allowed_venues`, `allowed_symbols`, optional `allowed_accounts`);
- fail-closed deterministic rejections.

Stage 8 adds lifecycle-specific controls:

- `execution.real.binance.trade_api.endpoint_mode=safe_order_lifecycle` requires:
  - `reconcile_enabled=true`;
  - bounded `reconcile_max_polls`;
  - bounded `reconcile_poll_interval`;
  - testnet base URL enforcement.

`test_order` mode remains available and disallows reconciliation polling.

---

## Security / Credentials

Credentials boundary remains unchanged and trade-only:

- env-based key/secret loading only;
- no withdraw/custody permissions;
- no custody flows introduced.

No external source of truth bypasses `execution.event`.

---

## Out of Scope (Still Not Implemented)

- OMS/full routing engine;
- custody/withdraw/account transfer capabilities;
- unrestricted live trading mode;
- portfolio account-sync as source of truth;
- multi-exchange smart routing.

---

## Validation Executed

```bash
go test ./internal/adapters/execution/binance ./internal/core/portfolio/app ./internal/actors/execution/runtime ./internal/actors/portfolio/runtime ./internal/actors/runtime ./cmd/executor ./internal/shared/config -count=1
go test ./internal/core/execution/app ./internal/shared/contracts ./cmd/portfolio -count=1
```
