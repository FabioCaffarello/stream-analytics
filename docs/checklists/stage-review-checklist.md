# Stage Review Checklist

> Gate checklist for accepting any new stage into Market Raccoon.
> Authority: T2 operational. Aligned with ADR-0036 baseline and evolution-criteria.
> Effective: 2026-03-13. Review cadence: monthly with evolution-criteria.

---

## How to use

Before merging a stage, walk through each section. Mark `[x]` or `[n/a]`.
A single unchecked item in **Critical** blocks the merge.
Items in **Standard** should be resolved or have a tracking issue.

---

## Critical — blocks merge

### Architecture & Boundaries

- [ ] `make invariants-check` passes (all INV-* green)
- [ ] No circular dependencies introduced (module, package, or import level)
- [ ] Backend 6-layer hierarchy respected: `cmd → interfaces → actors → adapters → core → shared`
- [ ] Client DAG respected: `app → layers → md_common → services → streams → ports → ui/math/util`
- [ ] No cross-context direct function calls — NATS only at runtime
- [ ] No new cross-context import not listed in `boundary-rules.md` (requires ADR if needed)
- [ ] No business logic added to `shared/`
- [ ] No `time.Now()` in `core/` (INV-DET-01)
- [ ] No protobuf imports in `core/`, `actors/`, or `interfaces/` (INV-DOM-01)

### Naming

- [ ] New types/fields use only the 14 canonical terms with correct semantics (naming-rules.md)
- [ ] `State` is prefix-qualified (N3) — no bare `State` type
- [ ] No singular/plural-only name collision (N1)
- [ ] Backend terms reused in client retain same semantics (N2)

### Tests

- [ ] `make test` passes — zero regressions
- [ ] New domain logic has unit tests
- [ ] No mocks where integration tests are expected (storage, reconciliation)

---

## Standard — resolve or track

### Product Alignment

- [ ] Stage deliverable maps to a PRD milestone, backlog item, or ADR decision
- [ ] If new capability: 8-step gate sequence followed (evolution-criteria §7)

### Contracts & Compatibility

- [ ] Wire contract changes are backward-compatible (additive only)
- [ ] If new Go module: `replace` directives present for all workspace-local deps
- [ ] Envelope/codec changes maintain `codec.Marshal/Unmarshal` contract
- [ ] Client schema version bumped only if persistence format changed

### Client Guard Rails

- [ ] Cell_Surface_View ≤ 10 fields (or ADR justifying increase)
- [ ] Data_Readiness ≤ 6 variants
- [ ] Layer_Context remains read-only; strategies remain stateless
- [ ] Per-stream store isolation maintained (DOM, Footprint, Trades on Market_Stream)
- [ ] Pure derivation constraint held — no cached health/reliability state

### Documentation Impact

- [ ] TRUTH-MAP updated if new domain surface introduced
- [ ] `subsystems.md` updated if actors added/moved
- [ ] `boundary-rules.md` updated if new allowed cross-context import
- [ ] ADR written if architectural deviation from baseline
- [ ] No T1 document modified without explicit justification

### Legacy & Drift Prevention

- [ ] No reintroduction of patterns removed in prior stages (check stage-history)
- [ ] No `fmt.Sprintf` on hot path (use FieldHasher / buffer concat)
- [ ] No new deferred items added without P-level classification
- [ ] Known violations list (ADR-0036 §5) not expanded without tracking

### Operational Risk

- [ ] Hot-path changes validated against throughput baseline (117K evt/sec)
- [ ] New actors assigned to correct binary in subsystems ownership table
- [ ] Prometheus metrics added for new observable behaviors
- [ ] If touching execution/portfolio: reconciliation tests pass

---

## Sign-off

```
Stage:    S___
Reviewer: ___
Date:     ____-__-__
Result:   [ ] ACCEPT  [ ] REJECT — reason: ___
```
