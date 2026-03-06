# Pre-Stage 9 Architectural Audit

**Status:** Current
**Last updated:** 2026-03-06

## Scope

This audit reviews the implemented state after Stages 1-8 and applies pre-Stage 9 hardening without expanding the functional scope. The review was anchored in the frozen semantic model and the current runtime/code paths, not only in stage summaries.

Authorities reviewed before changes:

- `docs/adrs/ADR-0023-frozen-semantic-model-feature-evidence-signal-intent-execution-portfolio.md`
- `docs/architecture/TRUTH-MAP.md`
- `docs/architecture/subsystems.md`
- `docs/contracts/strategy-execution-portfolio-contracts.md`
- `docs/contracts/event-bus.md`
- `docs/contracts/subject-registry.yaml`
- `proto/registry.json`
- stage docs for Stages 1, 2, 4, 5, 6, 7, and 8

## Executive Assessment

The target architecture is materially present and coherent:

`signal.event -> strategy.intent -> execution.event -> portfolio.state`

Dedicated strategy, execution, and portfolio bounded contexts exist in both contracts and runtime (`internal/core/strategy/*`, `internal/core/execution/*`, `internal/core/portfolio/*`, plus matching actor subsystems and entrypoints). Execution remains behind the `IntentExecutor` boundary, and portfolio state remains projector-derived from `execution.event`.

The main pre-Stage 9 issue was not missing architecture. It was architectural drift around the edges:

- overly broad subject intake in runtime/config bootstrap,
- delivery governance not fully updated to the canonical lifecycle contracts,
- observability/docs still reflecting transitional or aspirational states,
- and one broken invariant anchor in the boundedness catalog.

Those drifts were corrected in this hardening pass.

## Strengths Confirmed

- Canonical lifecycle BCs are explicit and no longer piggyback on `signal.event` metadata.
- Executor real-mode integration stays behind adapter ports (`internal/core/execution/ports/intent_executor.go`, `internal/adapters/execution/binance/safe_intent_executor.go`).
- Portfolio projection remains event-derived and deterministic (`internal/core/portfolio/app/bootstrap_projector.go`).
- Stage 6 retirement of strategist operational `signal.composite` intake remains enforced in runtime (`internal/actors/strategy/runtime/subsystem.go`).
- Config validation already contains useful fail-closed protection such as bus-capacity versus session-queue bounds (`internal/shared/config/loader.go`).

## Findings By Severity

### Critical

1. Canonical intake boundaries were weaker than the architecture required.
   - `cmd/executor/bootstrap.go` accepted broad `strategy.>` patterns instead of enforcing the `strategy.intent` family.
   - `cmd/portfolio/bootstrap.go` accepted broad `execution.>` patterns instead of enforcing the `execution.event` family.
   - `deploy/configs/executor.jsonc` and `deploy/configs/portfolio.jsonc` mirrored the same drift.
   - Risk: future event families under `strategy.*` or `execution.*` could bypass intended bounded-context isolation.
   - Action: narrowed effective filters and deploy config to the canonical contract families only.

2. Delivery envelope governance had not been fully updated to the canonical lifecycle contracts.
   - `internal/core/delivery/domain/envelope_policy.go` still treated signal ownership as `signals` and did not allow `strategy.intent`, `execution.event`, or `portfolio.state`.
   - Risk: delivery observability/governance was lagging behind the runtime truth and contract truth.
   - Action: lifecycle streams were added to the allowed policy set and signal ownership was normalized to the canonical singular BC.

### High

3. Delivery backpressure priority policy did not represent the canonical runtime lifecycle.
   - `internal/core/delivery/domain/backpressure_policy.go` prioritized legacy `signal.composite` but did not prioritize `signal.event`, `strategy.intent`, `execution.event`, or `portfolio.state`.
   - It also contained version-suffixed insight delta keys while runtime comparison uses bare event types.
   - Risk: under WS pressure, the runtime could preferentially protect the wrong streams or silently miss intended priorities.
   - Action: lifecycle priorities were added in canonical order and unreachable version-suffixed keys were corrected.

4. Server delivery observability was narrower than the documented architecture.
   - `deploy/configs/server.jsonc` delivery/JetStream filters omitted `strategy.intent.>`, `execution.event.>`, and `portfolio.state.>`.
   - Risk: the server could not observe or deliver the full canonical lifecycle chain despite docs and architecture claiming it could.
   - Action: lifecycle stream filters were added to the server config.

5. Contracts/docs overstated currently wired consumers for lifecycle streams.
   - `docs/contracts/strategy-execution-portfolio-contracts.md`, `docs/contracts/event-bus.md`, and `docs/contracts/subject-registry.yaml` listed storage as a current lifecycle consumer even though the current runtime wiring is strategy -> execution -> portfolio -> delivery.
   - Risk: governance/planning decisions for Stage 9 would be based on a false picture of persisted execution/portfolio lifecycle support.
   - Action: docs and registry entries were corrected to reflect current delivery wiring and explicitly mark storage as not yet wired.

### Medium

6. Historical stage documents read too much like current-state documents.
   - `docs/architecture/semantic-hardening-stage1.md` and `docs/architecture/boundary-hardening-stage2.md` still described transitional states without an explicit historical warning.
   - Risk: readers could misread Stage 1/2 posture as current architecture and reintroduce ambiguity.
   - Action: explicit historical notes were added.

7. The boundedness catalog had documentary drift strong enough to fail a gate.
   - `docs/contracts/boundedness-matrix.md` referenced a stale line anchor for `backend.delivery.session_outbound_queue_size`.
   - `make invariants-check` failed until the anchor was corrected to the actual default assignment in `internal/shared/config/loader.go`.
   - Action: anchor updated and invariants gate rerun successfully.

### Low / Controlled Residue

8. Legacy signal-composer/runtime packages still exist.
   - `internal/core/signals/*` and `internal/actors/signals/*` remain in the tree.
   - Current role: compatibility/replay/document history, not the canonical operational decision path.
   - Assessment: acceptable as controlled residue, but they must not regain architectural authority.

9. Ownership salt naming still carries the transitional `SubsystemStrategist` label.
   - `internal/shared/ownership/contract.go` still uses `SubsystemStrategist`.
   - `internal/actors/strategy/runtime/subsystem.go` intentionally retains that ownership salt for continuity.
   - Assessment: acceptable for shard/ownership continuity, but this is naming residue and should be treated as a migration decision, not silently normalized later.

## Axis-by-Axis Assessment

| Axis | Assessment | Status |
| --- | --- | --- |
| Architecture / BCs | Signal, strategy, execution, and portfolio are structurally separated; major drift was in intake filters, now corrected. | Stronger after hardening |
| Clean Architecture | Domain/application remain infra-free in the audited paths; `cmd/*` stay as composition roots. | Acceptable |
| DDD | Ubiquitous language now aligns better across runtime and docs; lifecycle facts vs projection remain separated. | Acceptable |
| SOLID | Executor/projector responsibilities are narrow; delivery governance had SRP/OCP drift that was corrected. | Improved |
| Patterns / modeling | Ports/adapters, projector, and runtime supervision patterns are used coherently; excess abstraction was not introduced. | Acceptable |
| Executor boundary | Binance-specific behavior stays in adapter translation behind `IntentExecutor`; domain contracts remain canonical. | Strong |
| Portfolio projector | Projection remains derived from `execution.event` with deterministic bootstrap behavior. | Strong |
| Config / governance | Canonical stream filters and boundedness truth were hardened; fail-closed posture remains. | Improved |
| Observability / audit | Lifecycle streams are now delivery-visible and alias-normalized; governance metadata is consistent with docs. | Improved |
| Contracts / registry / docs | Major drift on lifecycle consumers and ownership labeling was corrected. | Improved |
| Testability / gates | Added direct tests for canonical intake narrowing and delivery governance priorities; docs/invariants gates pass. | Improved |
| Legacy residual | Compatibility residue remains but is now clearly classified as non-authoritative. | Controlled |

## Hardening Applied In This Audit

1. Enforced canonical family intake for executor and portfolio bootstrap/config.
2. Updated delivery envelope governance to include canonical lifecycle event types.
3. Rebalanced delivery backpressure priorities around the canonical lifecycle chain.
4. Extended server delivery filters so observability sees the full canonical chain.
5. Aligned contracts/registry/docs with current runtime truth rather than aspirational storage wiring.
6. Marked historical stage documents explicitly as historical snapshots.
7. Repaired the boundedness-matrix anchor so invariant gates reflect the actual codebase.

## Remaining Risks Before Stage 9

1. Lifecycle streams are now documented truthfully as delivery-visible, but durable storage/read APIs for `strategy.intent`, `execution.event`, and `portfolio.state` are still not wired.
2. `signal.composite` remains routable in compatibility paths for replay/history; it must remain outside operational decision authority.
3. The `SubsystemStrategist` ownership salt name is controlled residue. Renaming it later is a deliberate migration, not a cleanup task.
4. `cmd/server` remains a broad composition root because it bootstraps many subsystems. That is acceptable today, but Stage 9 should avoid pushing domain policy back into it.

## Readiness Verdict

Pre-Stage 9 readiness is **yes, with controlled residue**.

The codebase is materially stronger after this pass because the remaining issues are now mostly explicit scope decisions rather than hidden architectural drift. The highest-risk mismatches before Stage 9 were around boundaries, observability truth, and documentary truth; those have been corrected without reopening the semantic model or expanding the runtime scope.
