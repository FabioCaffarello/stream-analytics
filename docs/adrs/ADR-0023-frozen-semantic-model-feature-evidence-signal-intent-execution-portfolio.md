# ADR-0023 - Frozen Semantic Model (Feature, Evidence, Signal, Strategy Intent, Execution Event, Portfolio State)

**Status:** Proposed
**Date:** 2026-03-06
**Owners:** Architecture / Runtime Platform

## Context

The repository currently has strong event-driven foundations, but key domain terms are overloaded across codepaths:

1. Two signal semantics coexist: `signal.event` and `signal.composite`.
2. `cmd/strategist` currently composes signals, but does not generate strategy intent.
3. At freeze capture time, `cmd/server` and `cmd/processor` still exposed optional embedded signal/evidence behavior through flags.
4. `feature` shape differs by domain (`key/float` vs `label/string`), increasing conceptual drift.
5. `strategy intent`, `execution event`, and `portfolio state` are not first-class contracts yet.

Code evidence:

- `cmd/server/main.go:3-5` vs `cmd/server/bootstrap.go:450-512`
- `cmd/processor/bootstrap.go:718-780`
- `cmd/signals/main.go:3-4`, `cmd/strategist/main.go:3-4`
- `internal/core/evidence/domain/evidence.go:58-84`
- `internal/core/marketmodel/events.go:150-179`
- `internal/core/signals/domain/composite_signal.go:11-38`
- `proto/registry.json:197-212`

## Decision

Freeze the following semantic model and ownership boundaries:

| Concept | Definition | Responsibility | Example | Produces | Consumes | Must live in code | Must not live in code |
|---|---|---|---|---|---|---|---|
| `feature` | Atomic deterministic observation field (`name + value`) used as evidence/signal ingredient, not a business event. | Express measured facts used by downstream rules. | `spread_bps=27.4`, `imbalance=0.81` | Evidence and signal composition internals | Evidence rules, signal rules, strategist rules | Domain value objects in `internal/core/*/domain` (canonical numeric in evidence/signal engine flows) | `cmd/*` wiring, delivery command protocol, ad-hoc envelope metadata semantics |
| `evidence` | Replayable market observation event describing structural market condition with confidence, never an execution directive. | Explain what market structure was detected. | `liquidity.evidence` or `insights.regime_evidence` | Evidence runtime (`internal/actors/evidence/runtime`) | Signal engine, strategist/composer, delivery UI consumers | `internal/core/evidence/*`, `internal/actors/evidence/runtime`, `proto/{evidence,liquidity}` | `server` decision logic, execution/portfolio contexts |
| `signal` | Replayable actionable alert derived from evidence/market context, still non-execution and non-order. | Surface deterministic opportunity/risk signal for downstream decision-making. | `signal.event` with `type=liquidity_thinning` | Signal-engine runtime (`internal/actors/signal/runtime`) | Delivery/UI, future strategist intent generator | `internal/core/signal/*`, `cmd/signals`, `proto/marketmodel/v1` | Strategy intent semantics, execution routing logic, portfolio mutation logic |
| `strategy intent` | Explicit domain decision proposal to take/adjust risk, derived from signals and policy. Separate from signal emission. | Decide "what to attempt" (intent), not "what happened in market". | Future `strategy.intent` payload: intent id, side, sizing policy, constraints | Future Strategist BC (`cmd/strategist`) | Future executor | Future `internal/core/strategy/*`, `internal/actors/strategy/runtime`, `proto/strategy/*` | `signal.event` metadata hacks (`intent_id`), `server` or `processor` runtime |
| `execution event` | Immutable event describing lifecycle of intent execution (accepted/rejected/routed/filled/canceled/failed). | Represent what happened during execution. | Future `execution.event` with status transitions and venue order refs | Future Executor BC | Portfolio projector, audit consumers, delivery | Future `internal/core/execution/*`, `cmd/executor`, `proto/execution/*` | Signal/evidence bounded contexts, delivery-only gateway code |
| `portfolio state` | Deterministic projected state derived from execution events and risk/account events. | Represent resulting exposure, PnL, and limits state. | Future `portfolio.state` snapshot/projection stream | Future Portfolio BC | Delivery/read APIs, risk monitors | Future `internal/core/portfolio/*`, `cmd/portfolio`, `proto/portfolio/*` | Signal/evidence generation paths, marketdata/aggregation runtime |

Additional freeze rules:

1. `signal.event` is the canonical signal stream.
2. `signal.composite` is transitional compatibility output and must not become the semantic anchor for strategy intent.
3. `intent_id` in envelope metadata is not a strategy-intent domain contract.
4. `server` and `processor` embedded signal/evidence branches are not target ownership; Stage 2 boundary hardening removes those runtime branches.

## Consequences

- Positive:
  - Removes conceptual ambiguity before introducing executor/portfolio.
  - Prevents strategy/execution semantics from leaking into signal/evidence contexts.
  - Establishes a clean contract-first path for next phases.
- Negative:
  - Requires planned renaming/repositioning (`signals`/`strategist`) to align terminology.
  - Transitional compatibility (`signal.composite`) must be explicitly managed until retirement.

## Alternatives

- Keep current mixed semantics and defer decisions (rejected: increases legacy lock-in and naming debt).
- Treat `signal.composite` as final strategy contract (rejected: conflates composition with intent).
- Introduce executor/portfolio immediately without semantic freeze (rejected: high risk of codifying wrong boundaries).

## Evidence

- Validation gate: `make docs-check-fast`
- Authority path: `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`
- Supporting diagnosis: `docs/architecture/semantic-hardening-stage1.md`

## Changelog

- 2026-03-06: initial draft for Stage 1 semantic freeze.
- 2026-03-06: aligned with Stage 2 boundary hardening (`cmd/processor` and `cmd/server` embedded domain branches removed).
- 2026-03-06: reaffirmed in Stage 5 hardening (executor lifecycle policies + portfolio lifecycle projection + controlled `signal.composite` transitional cutover).
