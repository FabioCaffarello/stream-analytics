# Stage 4 Runtime Bootstrap Report

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/semantic-hardening-stage1.md`, `docs/architecture/boundary-hardening-stage2.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`

---

## Goal and Context

Implement the first deterministic runtime chain across the newly contracted bounded contexts, without any real exchange integration or external credentials.

Stage 5 hardening evolution is documented in `docs/architecture/execution-portfolio-hardening-stage5.md`.
Stage 6 legacy retirement is documented in `docs/architecture/legacy-retirement-stage6.md`.

Target flow implemented in this stage:

`signal.event -> strategy.intent -> execution.event -> portfolio.state`

This stage keeps Stage 1 semantic freeze and Stage 3 contracts intact while introducing only minimal runtime behavior.

---

## Runtime Bootstrap Flow

1. `cmd/strategist`
- Consumes `signal.event` as canonical input.
- Emits `strategy.intent` using deterministic planner logic.
- Historical Stage 4 note: transitional `signal.composite` intake existed at bootstrap time; retired in Stage 6.

2. `cmd/executor`
- Consumes `strategy.intent`.
- Emits deterministic synthetic `execution.event` transitions (`accepted` then `filled`) with stable sequencing/idempotency.
- No external calls, no venue adapters, no credentials.

3. `cmd/portfolio`
- Consumes `execution.event`.
- Applies deterministic projection to minimal `portfolio.state`.
- Emits balances/position/exposure/risk snapshot derived only from execution stream.

---

## What Each Runtime Does and Does Not Do

### Strategist bootstrap

Does:
- Plans explicit `strategy.intent` from canonical `signal.event`.
- Preserves correlation/provenance (`signal_id`, `correlation_id`, deterministic `intent_id`).

Does not:
- Call exchanges.
- Emit `execution.event`.
- Collapse strategy and execution into a single step.

### Executor bootstrap

Does:
- Converts one intent into deterministic execution lifecycle events.
- Produces replay-stable sequence and ids.

Does not:
- Route real orders.
- Integrate with Binance/Coinbase/Kraken.
- Implement production-grade execution/risk logic.

### Portfolio bootstrap

Does:
- Projects minimal portfolio state from execution events.
- Emits deterministic state snapshots with provenance back to execution seq/event id.

Does not:
- Pull market data.
- Read external account state.
- Act as full risk or custody system.

---

## Intentional Limitations (Stage 4)

- Execution lifecycle is synthetic bootstrap behavior (`accepted -> filled`) and intentionally minimal.
- Portfolio projection uses simplified accounting assumptions to validate topology, not production PnL correctness.
- Historical Stage 4 state included transitional `signal.composite`; Stage 6 removes it from strategist operational intake.
- Delivery/storage integration for the new streams remains contract-aligned but bootstrap-focused.

---

## Validation Evidence

Primary runtime/contract tests for this stage:

- `internal/actors/strategy/runtime/subsystem_test.go`
- `internal/actors/execution/runtime/subsystem_test.go`
- `internal/actors/portfolio/runtime/subsystem_test.go`
- `internal/actors/runtime/strategy_execution_portfolio_bootstrap_e2e_test.go`
- `internal/shared/contracts/strategy_execution_portfolio_registry_test.go`
- `internal/shared/contracts/fallback_coverage_test.go`

These tests verify contract encoding/decoding plus end-to-end bootstrap chaining.
