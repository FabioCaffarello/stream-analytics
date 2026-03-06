# Stage 5 Execution/Portfolio Hardening + Transitional Cutover

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/runtime-bootstrap-stage4.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`, `docs/operations/signals-strategist-cutover.md`

---

## Goal and Context

Evolve Stage 4 bootstrap runtime (`signal.event -> strategy.intent -> execution.event -> portfolio.state`) into a hardened deterministic base for execution/projection without introducing real exchange integration.

Stage 6 legacy retirement follow-up is documented in `docs/architecture/legacy-retirement-stage6.md`.

---

## Executor Hardening

`internal/core/execution/app/bootstrap_executor.go` now applies explicit deterministic policy decisions before emit:

- Reject incomplete intent scope/identity.
- Reject non-positive sizing.
- Reject expired TTL (`expires_at_ms <= observed_at_ms`).
- Reject oversized TTL window.
- Reject oversized sizing/notional/slippage vs bootstrap limits.
- Reject unsupported bootstrap constraints (`post_only`, `reduce_only`).
- Accept valid intents and emit deterministic synthetic lifecycle:
  - `accepted`
  - `filled` (when enabled)

Rejected intents emit canonical `execution.event` with:

- `status=rejected`
- deterministic `reason` code
- stable `event_id`, `execution_seq`, and correlation/provenance fields

---

## Portfolio Projector Hardening

`internal/core/portfolio/app/bootstrap_projector.go` now projects lifecycle-aware state instead of filled-only updates:

- Tracks pending orders from `accepted` events.
- Clears pending locks on terminal non-fill events (`rejected/canceled/expired/failed`).
- Applies fills with deterministic position/cash updates.
- Maintains basic realized/unrealized PnL snapshot and exposure.
- Emits balances with explicit `available`/`locked` values.

This stays intentionally bootstrap-scoped (no external account sync, no market mark stream, no production risk engine).

---

## Transitional Cutover Progress (`signal.composite`)

Stage 5 starts controlled cutover by making canonical strategist intake explicit:

- Strategist default filter is now `signal.event.>`.
- Legacy `signal.composite` intake is opt-in via explicit filter (`signal.composite.>`).
- Legacy-derived intents remain tagged with `meta.transitional_source=signal.composite`.

This Stage 5 transitional posture is superseded by Stage 6, which retires strategist `signal.composite` intake.

Operational docs now define this as transitional legacy mode and include cutover monitoring guidance.

---

## Observability / Traceability

New metadata markers in publish path:

- `strategy.intent`: `input_semantic` (`signal.event` or `signal.composite.transitional`), plus transitional marker.
- `execution.event`: `execution_status`, deterministic `execution_reason`, `correlation_id`.
- `portfolio.state`: `source_execution_status`, `source_execution_reason`, `correlation_id`.

Stage 6 adds explicit execution boundary metadata (`execution_boundary`, `execution_adapter`, `execution_mode`).

---

## Validation Executed

```bash
go test ./internal/core/execution/... ./internal/core/portfolio/... ./internal/actors/strategy/runtime ./internal/actors/execution/runtime ./internal/actors/portfolio/runtime ./internal/actors/runtime ./cmd/strategist ./cmd/executor ./cmd/portfolio -count=1
```

---

## Intentional Limitations (Still Out of Scope)

- No real exchange connectivity.
- No credentials broker or API key handling.
- No OMS/routing layer.
- No custody/withdraw flows.
- No portfolio mark-to-market from marketdata feed.
