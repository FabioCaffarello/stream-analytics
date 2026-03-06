# Stage 7 Real Adapter Contract Integration (Safe Stub-to-Real Cut-In)

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/legacy-retirement-stage6.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`, `docs/contracts/event-bus.md`

---

## Goal and Context

Introduce the first real execution adapter behind the existing `IntentExecutor` boundary without changing canonical semantics:

`signal.event -> strategy.intent -> execution.event -> portfolio.state`

Stage 7 keeps bootstrap executor as default and adds an explicit opt-in real adapter safe mode.

Stage 8 follow-up extends lifecycle/reconciliation in safe pilot mode:
`docs/architecture/real-execution-lifecycle-stage8.md`.

## Real Adapter Integration

1. `cmd/executor` now selects adapter by explicit config/flag:
   - `execution.mode=bootstrap_simulated` -> deterministic bootstrap executor.
   - `execution.mode=real_adapter_safe` + `execution.adapter=binance.spot` -> real adapter behind `IntentExecutor`.
2. Real adapter implementation lives in infrastructure layer:
   - `internal/adapters/execution/binance/safe_intent_executor.go`
   - `internal/adapters/execution/binance/trade_api_client.go`
3. `internal/actors/execution/runtime` remains unchanged semantically:
   - input contract stays `strategy.intent`
   - output contract stays canonical `execution.event`
   - boundary metadata remains explicit: `execution_boundary`, `execution_adapter`, `execution_mode`

## Safe Mode and Guardrails

Stage 7 is strictly fail-closed:

1. `execution.safe_mode=true` and `execution.trade_only=true` are mandatory.
2. Real adapter mode requires:
   - explicit venue/symbol allowlists (`execution.allowed_venues`, `execution.allowed_symbols`)
   - explicit enable flag (`execution.real.enabled=true`)
   - explicit adapter (`execution.adapter=binance.spot`)
   - endpoint mode fixed to `test_order` (live placement forbidden in Stage 7)
3. Deterministic rejections are emitted as canonical `execution.event` with status `rejected`, including:
   - mode/config mismatch
   - venue/symbol/account outside allowlist
   - missing credentials
   - unsupported sizing/order constraints
   - TTL/sizing/notional/slippage outside safe limits

## Security / Credentials Boundary

1. Added minimal trade-only credentials boundary:
   - `internal/adapters/execution/credentials/provider.go`
   - `internal/adapters/execution/credentials/env_provider.go`
2. Credentials are loaded only from env var names in config (`api_key_env`, `api_secret_env`).
3. No custody/withdraw semantics were introduced.
4. Real adapter supports only signed trade API test endpoint (`/api/v3/order/test`) in Stage 7.

## Semantic Integrity

1. Strategy semantics remain isolated in strategist (`strategy.intent` generation only).
2. Execution semantics remain isolated in executor (`execution.event` lifecycle facts).
3. Portfolio remains event-derived projection from `execution.event`.
4. No venue payload is promoted to domain contract; adapter responses are translated into canonical execution events.

## What Stage 7 Does Not Do

1. No OMS or multi-venue smart routing.
2. No withdraw/custody flows.
3. No unrestricted live order flow.
4. No external account reconciliation as portfolio source of truth.

## Validation Executed

```bash
go test ./internal/shared/config ./internal/adapters/execution/... ./internal/core/execution/... ./internal/actors/execution/runtime ./internal/actors/runtime ./cmd/executor ./cmd/portfolio -count=1
go test ./internal/core/portfolio/... ./internal/actors/portfolio/runtime ./internal/actors/strategy/runtime -count=1
go test ./internal/actors/runtime ./internal/adapters/execution/... ./cmd/executor ./internal/shared/config -count=1
```
