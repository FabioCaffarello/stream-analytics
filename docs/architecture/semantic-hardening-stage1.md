# Stage 1 Architectural Hardening - Semantic Freeze Proposal

**Status:** Proposed
**Date:** 2026-03-06
**Owner:** Architecture / Runtime Platform
**Scope:** Stage 1 only (semantic freeze and boundary hardening, no executor/portfolio implementation)

---

## Goal and Context

This document delivers Stage 1 hardening for Market-Raccoon by freezing domain semantics and clarifying ownership boundaries without introducing new trading functionality.

Historical note:
- This document captures the as-is diagnosis at Stage 1 execution time.
- Current runtime state has since advanced through Stages 4-8; use the later stage reports for the live strategist/executor/portfolio posture.

The diagnosis is anchored in current codepaths:

- `cmd/{consumer,processor,server,store,signals,strategist}`
- `internal/{core,actors,adapters,interfaces,shared}`
- `proto/*` and `internal/shared/contracts/*`
- `deploy/configs/*`

---

## Deliverable A - Architectural Diagnosis (As-Is)

### 1) Existing domains and runtime responsibilities (today)

1. `consumer` is ingest-only (exchange WS -> canonical envelopes), no strategy semantics.
   - Anchor: `cmd/consumer/bootstrap.go:63-65`, `cmd/consumer/bootstrap.go:95-105`
2. `processor` owns aggregation and insights outputs, but still has optional embedded signal runtime.
   - Anchor: `cmd/processor/bootstrap.go:718-780`, `internal/shared/config/schema.go:690-700`
3. `server` should be gateway/control-plane, but still can host evidence and signal-composer runtime branches.
   - Anchor: `cmd/server/main.go:3-5` vs `cmd/server/bootstrap.go:450-512`, `cmd/server/bootstrap.go:1045-1055`
4. `signals` command emits `signal.event` using `internal/core/signal` + `internal/actors/signal/runtime`.
   - Anchor: `cmd/signals/main.go:3-4`, `cmd/signals/bootstrap.go:59-63`, `internal/core/signal/emitter.go:9`
5. `strategist` command emits `signal.composite` using `internal/core/signals` + `internal/actors/signals/runtime`.
   - Anchor: `cmd/strategist/main.go:3-4`, `cmd/strategist/bootstrap.go:67-71`, `internal/core/signals/domain/composite_signal.go:11`
6. `store` is persistence/observer pipeline and currently stores aggregation/insights streams, not execution/portfolio.
   - Anchor: `cmd/store/bootstrap.go:45-49`, `cmd/store/bootstrap.go:247-285`

### 2) Critical conceptual ambiguities (today)

1. Two parallel "signal" worlds coexist:
   - `signal.event` (`internal/core/signal`)
   - `signal.composite` (`internal/core/signals`)
   - Anchor: `proto/registry.json:197-212`, `internal/shared/contracts/signal_engine_registry.go:10`, `internal/shared/contracts/signals_registry.go:17`
2. Runtime naming mismatch:
   - `cmd/strategist` registers actor as `SubsystemSignals` in guardian.
   - ownership hashing uses `SubsystemStrategist`.
   - Anchor: `cmd/strategist/bootstrap.go:87-89`, `internal/actors/runtime/protocol.go:18-29`, `internal/shared/ownership/contract.go:14-16`, `internal/actors/signals/runtime/subsystem.go:370-376`
3. Delivery has transitional behavior:
   - rejects `signal.composite` subscriptions as legacy,
   - but still treats `signal.composite` as routable in contracts and subject translation.
   - Anchor: `internal/actors/delivery/runtime/session_commands.go:539-551`, `internal/core/delivery/domain/envelope_policy.go:28-29`, `internal/core/delivery/domain/subject.go:15-16`
4. Feature semantics diverge across bounded contexts:
   - `EvidenceFeature{key,float}`
   - `marketmodel.SignalFeature{key,float}`
   - `signals.SignalFeature{label,string}`
   - Anchor: `internal/core/evidence/domain/evidence.go:58-62`, `internal/core/marketmodel/events.go:150-153`, `internal/core/signals/domain/composite_signal.go:17-21`
5. Contract/documentation drift:
   - `proto/registry.json` includes `signal.event` and `signal.composite`,
   - `docs/contracts/subject-registry.yaml` has no `signal.*` entries.
   - Anchor: `proto/registry.json:197-212`, `docs/contracts/subject-registry.yaml` (`rg "signal\\."` => no signal rows)
6. Strategy intent does not exist as domain event; only `intent_id` metadata is injected on `signal.event`.
   - Anchor: `internal/actors/signal/runtime/subsystem.go:491`
7. Execution and portfolio domains are not implemented yet.
   - Anchor: `cmd/executor` (empty), `cmd/portfolio` (empty), `cmd/credentials-broker` (empty)

### 3) Overlap matrix (`signal`, `signals`, `strategist`, `processor`, `server`)

| Component | What it does today | Why ambiguous |
|---|---|---|
| `cmd/signals` | Emits `signal.event` from market/evidence inputs | Name is plural but runtime is singular signal engine |
| `cmd/strategist` | Composes `signal.composite` from evidence/regime | "Strategist" name implies intent/decision, but code is still signal composition |
| `cmd/processor` | Aggregation + optional embedded `signal.event` runtime | Mixes aggregation BC with optional signal BC |
| `cmd/server` | Delivery gateway + optional evidence + optional composer | Boundary says "no business logic", but wiring still hosts domain runtime |
| Delivery layer | Migrating from `signal.composite` to `signal/<kind>/...` | Legacy and target semantics coexist in policy and routing |

---

## Deliverable C - As-Is -> To-Be Mapping

| Concept | Current implementation / ownership | Current problem | Target ownership | Recommended action |
|---|---|---|---|---|
| `feature` | Duplicated across `evidence`, `marketmodel`, `signals` types | Same word, different shape (`float` vs `string`) | Canonical numeric feature in evidence/signal engine; string mapping only as transport adapter | Declare numeric feature as canonical; mark string feature as `signal.composite`-legacy representation |
| `evidence` | Produced in evidence runtime (`insights.microstructure_evidence`, `liquidity.evidence`, `insights.regime_evidence`) | Two evidence lineages mixed in naming/docs | Evidence BC (`internal/core/evidence`, `internal/actors/evidence`) | Keep both event lines short-term; designate `liquidity.evidence` as forward stream and document `insights.microstructure_evidence` as transitional |
| `signal` | `signal.event` (engine) and `signal.composite` (composer) both active | "Signal" overloaded between canonical and composed legacy | Signal Engine BC as canonical (`signal.event`) | Freeze `signal.event` as canonical signal; deprecate `signal.composite` as compatibility stream |
| `strategy intent` | No event contract; only `meta.intent_id` on signal envelope | Concept exists by name but not by boundary/event | Future Strategist BC (intent generation only) | Introduce explicit `strategy.intent` contract in next phase; do not overload `signal.event`/meta |
| `execution event` | Not implemented | Missing domain/event boundary | Future Executor BC | Reserve `execution.event` family; implement only after strategy intent contract is stable |
| `portfolio state` | Not implemented | Missing aggregate/read-model boundary | Future Portfolio BC | Reserve `portfolio.state` and projection policy from execution events |

---

## Deliverable D - Naming and Boundary Recommendations

### Direct answers

1. Should `cmd/signals` keep this name?
   - Recommendation: **rename to `cmd/signal-engine`** (or `cmd/signal`) in next refactor batch.
   - Rationale: service emits canonical `signal.event`, not "signals" aggregate.
2. Does `cmd/strategist` represent strategist today?
   - Recommendation: **not yet**. Current behavior is signal composition.
   - Rename target: `cmd/signal-composer` now; reserve `cmd/strategist` for future `strategy.intent`.
3. Is `server` hosting too much domain?
   - Recommendation: **yes**. `server` should remain gateway/control-plane only.
   - Remove embedded evidence/composer runtime from `cmd/server` after dedicated services are mandatory.
4. Should `processor` keep signal-related responsibilities?
   - Recommendation: **no** for target architecture.
   - Stage 2 update: embedded signal wiring removed; legacy `processor.signals.enabled` key is compatibility no-op.

### Recommended boundary split (target)

1. `consumer`: ingest canonical market events only.
2. `processor`: aggregation/insights transforms only.
3. `evidence` service: evidence generation only.
4. `signal-engine` service: canonical `signal.event` only.
5. `strategist` service: `strategy.intent` only (future).
6. `executor` service: execution lifecycle events only (future).
7. `portfolio` service: portfolio projections/state only (future).
8. `server`: delivery + control-plane only.

### What to do now vs later

**Now (Stage 1):**

1. Freeze semantic vocabulary (ADR-0023).
2. Mark `signal.composite` as transitional compatibility stream.
3. Freeze ownership contract: no new strategy/execution semantics inside `signal.event` metadata.
4. Keep fallback toggles but document them as migration-only.

**Next phase (Stage 2+):**

1. Remove embedded signal/composer from `processor` and `server`.
2. Rename command/runtime/module labels to align with frozen semantics.
3. Introduce proto/contracts for `strategy.intent`, `execution.event`, `portfolio.state`.
4. Implement paper-trading style executor/portfolio only after contracts are accepted.

---

## Deliverable E - Prioritized Next Steps

1. Hardening do `processor`: remove embedded signal subsystem after rollout confidence. **(Done in Stage 2)**
2. Hardening do `server`: strip evidence/composer wiring; keep only delivery/control-plane. **(Done in Stage 2)**
3. Contract phase: add `strategy.intent` proto + registry + subject matrix entries.
4. Contract phase: add `execution.event` and `portfolio.state` proto + registries.
5. Runtime phase: rebind `cmd/strategist` to strategy-intent BC and retire `signal.composite`.
6. Delivery hardening: fully remove `signal.composite` compatibility branches from routing/session policies.

---

## Stage 2 Execution Status (2026-03-06)

1. Completed: embedded signal runtime removed from `cmd/processor`.
2. Completed: embedded evidence/composer runtime removed from `cmd/server`.
3. Completed: compose profiles no longer advertise embedded fallback flags.
4. Still transitional: `signal.composite` compatibility stream and `cmd/strategist` naming.
