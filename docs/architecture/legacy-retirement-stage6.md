# Stage 6 Legacy Retirement + Pre-Real-Execution Readiness

**Status:** Implemented
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Relates to:** `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`, `docs/architecture/execution-portfolio-hardening-stage5.md`, `docs/contracts/strategy-execution-portfolio-contracts.md`, `docs/contracts/event-bus.md`

---

## Legacy Retirement Report

### What was retired

1. Strategist intake of `signal.composite` was removed from operational runtime.
2. Strategist transitional output tagging from legacy intake (`meta.transitional_source=signal.composite`) was removed.
3. Strategist filter hardening now strips legacy/broad signal filters and enforces canonical `signal.event` intake.

### What is no longer operational path

1. `signal.composite -> strategy.intent` is no longer a supported runtime decision path.
2. Recommended and enforced strategist decision path is only:
   `signal.event -> strategy.intent`.

### Residual compatibility that remains (intentional)

1. `signal.composite` schema/converters remain available for historical replay/read compatibility in delivery and contracts tooling.
2. Delivery/runtime may still parse historical `signal.composite` envelopes, but this stream is not part of strategist execution path.
3. Legacy compatibility is explicitly marked as retired/deprecated in contracts and operations docs.

### Why this retirement was required

1. Stage 5 still left operational ambiguity by allowing dual strategist intake semantics.
2. Real execution readiness requires one canonical upstream decision semantic before adapter pluggability.
3. Removing strategist legacy intake reduces hidden behavior branches and simplifies replay/observability reasoning for:
   `signal.event -> strategy.intent -> execution.event -> portfolio.state`.

---

## Pre-Real-Execution Readiness Additions

1. Executor runtime now depends on an explicit execution port interface (`internal/core/execution/ports.IntentExecutor`) instead of a concrete bootstrap implementation.
2. Bootstrap executor publishes explicit boundary metadata (`execution_boundary`, `execution_adapter`, `execution_mode`) for future adapter insertion points.
3. This preserves current no-external-call behavior while making the venue adapter boundary explicit for future real integration stages.

---

## Out of Scope (Intentionally Unchanged)

1. No exchange integration (Binance/Coinbase/Kraken/etc.).
2. No API keys or credentials broker implementation.
3. No OMS/routing expansion, custody, withdrawals, or multi-venue real routing.
