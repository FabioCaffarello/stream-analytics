# Evolution Criteria — Months 2–12 (2026-04 → 2027-02)

> Normative document. Governs all architectural, product, documentation, and engineering
> decisions for Market Raccoon from Month 2 through Month 12.
> Authority: T1 canonical. Supersedes ad-hoc stage-level judgment.
> Effective: 2026-04-01. Review cadence: monthly, first business day.

---

## 1. Architecture

### Allowed

- Adding new bounded contexts ONLY via ADR with: domain justification, dependency graph update, binary ownership assignment, and TRUTH-MAP entry.
- Extending existing BCs with new domain types that respect the existing dependency DAG.
- Adding new actor subsystems within existing binary ownership boundaries.
- New adapters (exchanges, storage backends) that conform to existing port interfaces.
- Introducing new shared/ packages ONLY if they contain zero business logic and serve ≥2 consumers.

### Prohibited

- Circular dependencies between bounded contexts, actor subsystems, or Go modules — zero tolerance.
- Upward imports: core → actors, core → adapters, core → interfaces, actors → interfaces.
- Direct cross-context function calls — all cross-context communication goes through NATS.
- New dependencies from `delivery` or `workspace` to any other BC (isolation axiom).
- Adding business logic to `shared/` — shared is foundation only (problem, result, validation, ids, clock, envelope, codec, hash, naming).
- Creating a new Go module without `replace` directives for all workspace-local dependencies.

### Acceptance criteria for architectural changes

1. `make invariants-check` passes — all 8 INV-* rules green.
2. Dependency graph remains a DAG — no cycles introduced at module, package, or import level.
3. Binary ownership table in `subsystems.md` updated if actors are added/moved.
4. TRUTH-MAP updated with theme ownership for any new domain surface.

### Rejection criteria (drift signals)

- Any import from `core/*/domain` to a package outside `shared/`.
- Any `time.Now()` call inside `core/` (determinism invariant INV-DET-01).
- Any protobuf import inside `core/`, `actors/`, or `interfaces/` (INV-DOM-01).
- A new BC without an ADR — revert first, discuss second.

---

## 2. Boundaries

### Allowed

- Refining boundary rules in `boundary-rules.md` to add new exhaustive cross-context imports when justified by an ADR.
- Promoting a cross-cutting concern (like orderflow) with explicit tier ownership rather than a new BC.

### Prohibited

- Adding cross-context imports not listed in the exhaustive allowed set without an ADR.
- Merging two BCs without an ADR and migration plan.
- Violating the 6-layer backend hierarchy: `cmd → interfaces → actors → adapters → core → shared`.
- Violating the client DAG: `ports → services → layers → app`. No reverse imports.
- `services/` importing `layers/` or `app/`; `layers/` importing `app/`.

### Acceptance criteria

1. `boundary-rules.md` cross-context import table stays exhaustive — every allowed import is listed.
2. Client guard rails hold: Cell_Surface_View ≤ 10 fields, Data_Readiness ≤ 6 variants.
3. Layer_Context remains read-only; layer strategies remain stateless and side-effect free.
4. Per-stream store isolation maintained (DOM, Footprint, Trades on Market_Stream).

### Rejection criteria

- Any file that imports across a prohibited boundary — blocks merge.
- Cell_Surface_View exceeding 10 fields without an ADR justifying the increase.
- A layer strategy that holds mutable state or produces side effects.

---

## 3. Naming

### Rules (N1–N7 from `naming-rules.md` remain canonical)

| Rule | Summary |
|------|---------|
| N1 | Distinct responsibilities → distinct names |
| N2 | Backend terms cannot be reused with different semantics in client |
| N3 | `State` must always be prefix-qualified (`Pane_Visual_State`, not `State`) |
| N4 | `Health` = observable monitoring; `Readiness` = safety gate |
| N5 | `Summary` = rolled-up aggregation; `Snapshot` = complete point-in-time |
| N6 | `Event` = immutable append-only; `Action` = client user input |
| N7 | Cross-stack terms must have identical semantics or different names |

### Allowed

- Adding new canonical terms to the glossary (currently 14) via ADR or naming-rules.md update.
- Renaming to fix N1–N7 violations — always via a dedicated stage with zero functional changes.

### Prohibited

- Introducing a new domain type named `State`, `Health`, `Event`, `Snapshot`, `Summary`, `Session`, `Readiness`, `Signal`, `Intent`, `Execution`, `Portfolio`, `Artifact`, `Insight`, or `Workspace` without prefix qualification and N1–N7 compliance check.
- Reusing a backend canonical term in the client with different semantics.
- Naming a file `layer_*` unless it is an actual architectural layer in the client DAG.

### Rejection criteria

- A PR that introduces a new unqualified `State` or `Health` type — blocks merge.
- A PR that creates two packages with homonymous or quasi-homonymous names (like `signal/` + `signals/`).

---

## 4. Contracts

### Allowed

- Extending wire contracts (adding fields) with backward-compatible defaults.
- Adding new contract surfaces with a corresponding entry in the contract registry.
- Evolving encoding (JSON → CBOR) per PRD-0004 M2, with dual-format transition period.

### Prohibited

- Removing or renaming wire-format fields without a migration plan and version bump.
- Breaking `omitempty` semantics on existing fields.
- Adding required fields to existing payloads without client-side handling.
- Diverging backend/client field names on shared contract surfaces — names must match JSON tags exactly.

### Acceptance criteria

1. Contract reconciliation check passes — all surfaces verified against client parser.
2. New contract surfaces documented in `docs/contracts/` with field-level specification.
3. Wire-format changes tested with golden fixtures (encode → decode round-trip).
4. `omitempty` and zero-value semantics explicitly documented for new fields.

### Rejection criteria

- A wire-format field rename without migration — blocks merge.
- A new contract surface with no documentation — blocks merge.
- Golden fixture test failure on any contract change.

---

## 5. Documentation

### Governance model (4-tier, from AUTHORITY-MAP)

| Tier | Role | Examples | Mutability |
|------|------|----------|-----------|
| T1 | Canonical — governs code | ADRs, PRDs, contracts, architecture docs | Via ADR or PRD amendment |
| T2 | Operational — how to run | Testing strategy, local-dev, runbooks | Owner updates freely |
| T3 | Evolutionary — proposals | RFCs, moat.md | Superseded by ADR on acceptance |
| T4 | Historical — never governs | Stage reports, audits | Append-only, never modify |

### Allowed

- New ADRs for architectural decisions (sequential numbering from ADR-0036).
- New PRDs for product milestones (sequential from PRD-0007).
- Updating AUTHORITY-MAP and TRUTH-MAP when T1 documents are added.
- Archiving superseded T3 documents to `docs/archive/`.

### Prohibited

- Modifying T4 documents (stage reports, past audits) — append-only.
- Creating T1 documents without AUTHORITY-MAP and TRUTH-MAP entries.
- Leaving T3 proposals active after the corresponding ADR is accepted — must archive.
- Creating new `docs/roadmap-*` or `docs/*-roadmap-*` files — roadmap lives in PRDs only.

### Acceptance criteria

1. Every new ADR has: status, date, context, decision, consequences, and authority reference.
2. TRUTH-MAP covers every T1 document theme.
3. No T3 document older than 60 days without either promotion to T1 or archival.
4. AUTHORITY-MAP paths resolve to existing files.

### Rejection criteria

- A T1 document not indexed in AUTHORITY-MAP — must index before merge.
- A stale T3 document (>60 days) — must archive or promote.

---

## 6. Introducing New Capabilities

### Gate sequence (mandatory for all new capabilities)

```
1. ADR or PRD amendment  →  justification + scope
2. Contract specification →  wire format + golden fixtures
3. Domain types           →  core/*/domain, no external deps
4. Application logic      →  core/*/app, tested
5. Actor integration      →  actors/*/runtime
6. Adapter wiring         →  adapters/ or interfaces/
7. Soak validation        →  throughput + latency + memory
8. Documentation          →  TRUTH-MAP + contract docs
```

### Allowed

- Skipping step 7 (soak) for non-hot-path capabilities (e.g., admin endpoints, config changes).
- Combining steps 3–4 in a single stage for small capabilities (<200 LOC).

### Prohibited

- Implementing actor integration (step 5) before domain types are tested (step 4).
- Shipping a new wire contract without golden fixtures.
- Adding a capability that touches hot-path without soak validation.
- Adding a new exchange adapter without parser tests covering ≥95% of message types.

### Acceptance criteria

1. Every gate step produces a testable artifact.
2. New capability does not degrade existing soak benchmarks (throughput, p99 latency).
3. Binary ownership is preserved — new actors go into the correct binary.

---

## 7. Refactoring

### Allowed

- Refactoring stages that produce zero functional changes — rename, move, extract, inline.
- Splitting large files (>2000 LOC) into cohesive units.
- Unifying quasi-homonymous packages (e.g., `signal/` + `signals/` → `detection/` + `composition/`).
- Reducing `shared/contracts/` surface area (P0-1/P0-2 extraction).

### Prohibited

- Mixing refactoring with functional changes in the same stage — one concern per stage.
- Refactoring hot-path code without before/after benchmark comparison.
- Deleting public API surface without verifying zero external consumers.
- Refactoring that breaks wire-format compatibility.

### Acceptance criteria

1. All tests pass before and after — zero test delta for pure refactoring.
2. `make invariants-check` green.
3. No new `TODO` or `FIXME` introduced — refactoring resolves, not defers.
4. Hot-path refactoring includes benchmark output in stage report.

### Rejection criteria

- A "refactoring" stage that also changes behavior — split into two stages.
- Test count decreasing after refactoring without justification.

---

## 8. UI / Client Operational

### Allowed

- New widget kinds (Widget_Kind variants) with corresponding test coverage.
- New indicators and subplot analytics following existing layer strategy pattern.
- Workspace schema version bumps ONLY for persistence format changes.
- New keybindings via existing shortcut registration system.

### Prohibited

- Widget_Kind variants without tests.
- Workspace schema bump for non-persistence changes.
- Breaking the 31-node max tree constraint without ADR.
- Adding mutable state to Layer_Context or layer strategies.
- Introducing frame-budget regressions (measured via soak at steady state).
- `services/` depending on `layers/` or `app/`; `layers/` depending on `app/`.

### Acceptance criteria

1. Client test count does not decrease (baseline: 1,317 at S158).
2. Zero allocation in steady-state rendering path.
3. Native + WASM parity maintained for all new widgets.
4. 30-minute soak with full overlay stack shows no frame drops or memory growth.

### Rejection criteria

- Frame budget regression in soak — blocks merge.
- Client test count drop without equivalent coverage elsewhere.

---

## 9. Readiness / Live Trading

### Gate model (fail-closed, per PRD-0004 + execution governance)

| Gate | Requirement | Enforced by |
|------|-------------|-------------|
| G1 — Security | TLS + JWT + SOPS secrets + CORS + audit logging | PRD-0004 M1 |
| G2 — Wire stability | CBOR encoding + version handshake + permessage-deflate | PRD-0004 M2 |
| G3 — Execution safety | 5-gate FSM, explicit mode (no `bootstrap_simulated` default) | ADR execution governance |
| G4 — Portfolio reconciliation | Position projection matches exchange state within tolerance | Portfolio reconciliation tests |
| G5 — Operational baseline | Backup/restore, rolling updates, hot-reload, rate limiting | PRD-0004 M4 |
| G6 — Soak validation | 10M+ events, p99 < 100μs, zero data loss, memory stable | PRD-0004 M5 |

### Allowed

- Progressive gate completion (G1 → G6 in order).
- Simulated trading at any time (fail-closed FSM protects).
- Paper trading after G1 + G3 + G4.

### Prohibited

- Live trading with real funds before ALL 6 gates pass.
- Bypassing the execution FSM fail-closed design.
- Deploying without explicit execution mode configuration (no defaults to simulated).
- Running production without `/healthz` returning unconditional 200 OK (P1-3).
- `CheckOrigin: always true` in production WebSocket (must fix pre-deploy).

### Acceptance criteria

1. Each gate has a dated validation report in `docs/evidence/`.
2. Gate passage is irreversible — once passed, regression blocks deployment.
3. Execution FSM state machine coverage: all 10 commands × 4 states tested.

### Rejection criteria

- Any attempt to skip a gate — hard block.
- Execution mode defaulting to any value — must be explicitly configured.

---

## 10. Quality & Testing

### Quantitative baselines (Month 1 closure)

| Metric | Baseline | Direction | Tolerance |
|--------|----------|-----------|-----------|
| Backend tests | 1,666 | Must increase | Never decrease |
| Client tests | 1,317 | Must increase | Never decrease |
| Soak throughput | 117,697 evt/sec | Must hold | ≥100K evt/sec |
| Soak p99 latency | 56μs | Must hold | ≤100μs |
| Guard rails held | 9/9 (client) + 8/8 (backend) | Must hold | Zero regression |
| Circular deps | 0 | Must hold | Zero tolerance |
| `make invariants-check` | Pass | Must hold | Zero tolerance |

### Allowed

- Adding new test categories (fuzz, property-based, chaos) alongside existing suites.
- Adding new soak harnesses for new subsystems.
- Increasing invariant count as new architectural rules are added.
- Replacing tests with strictly better coverage (e.g., integration replacing unit when unit was testing mocks).

### Prohibited

- Deleting tests without replacement coverage.
- Disabling tests with `t.Skip()` without a linked issue and deadline.
- Committing code that fails `make ci`.
- Soak benchmark regression without an ADR justifying the tradeoff.
- `fmt.Sprintf` in hot-path code (zero-alloc discipline).
- `time.Now()` in `core/` (use `clock.Clock` interface).

### Acceptance criteria for new stages

1. `make ci` green (lint + test + invariants).
2. Test count ≥ previous stage test count.
3. No new `t.Skip()` without issue link.
4. Hot-path changes include benchmark comparison.
5. New domain types have ≥1 unit test per public method.
6. New actor subsystems have ≥1 integration test.
7. New exchange adapters have parser tests covering ≥95% message types.

### Rejection criteria

- `make ci` failure — hard block.
- Test count regression — hard block unless justified by test consolidation with equal coverage.
- Soak regression >10% — hard block without ADR.

---

## Appendix A — Stage Acceptance Checklist

Every stage MUST satisfy ALL of the following before closure:

```
[ ] make ci passes (lint + test + invariants)
[ ] Test count ≥ previous stage
[ ] No new circular dependencies
[ ] No upward imports (core → actors/adapters/interfaces)
[ ] No new time.Now() in core/
[ ] No fmt.Sprintf in hot-path
[ ] Binary ownership table current
[ ] TRUTH-MAP current (if T1 docs changed)
[ ] AUTHORITY-MAP current (if T1 docs added)
[ ] Wire contracts backward-compatible or migration planned
[ ] Client guard rails held (if client changes)
[ ] Naming rules N1–N7 not violated
[ ] Stage report written (T4, append-only)
```

## Appendix B — Monthly Review Protocol

On the first business day of each month:

1. **Verify baselines** — run full soak suite, compare against Month 1 baselines.
2. **Audit guard rails** — run `make invariants-check`, verify client guard rails.
3. **Review documentation** — scan for stale T3 docs (>60 days), verify AUTHORITY-MAP/TRUTH-MAP.
4. **Update this document** — if any criteria need amendment, do so via PR with rationale.
5. **Backlog triage** — re-prioritize P0/P1/P2/P3 based on current state.
6. **Gate progress** — update readiness gate status for live trading path.

---

*Last updated: 2026-03-13. Next review: 2026-04-01.*
