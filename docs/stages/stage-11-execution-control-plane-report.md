# Stage 11 -- Execution Control Plane + Runtime Governance

**Date:** 2026-03-06
**Branch:** codex/s9-legacy-removal-cutover
**Status:** COMPLETE

---

## Executive Summary

Stage 11 introduces a runtime control plane that enables operational governance of the execution subsystem without altering domain contracts or requiring restarts. The control plane operates as a pre-flight gate in the GovernedExecutor, evaluated before static governance. It supports pause/resume, drain, emergency halt, per-strategy and per-adapter disable toggles, simulation profile switching, and runtime allowlist narrowing. All decisions are fail-closed, deterministic, and emitted as standard rejection events through the existing execution.event pipeline.

**Key metrics:**
- 860 LOC new code (332 source + 528 test)
- 227 LOC modified across 6 existing files
- 73 tests passing in execution/app (30 new control plane + 5 new governed executor integration)
- Zero regressions across full test suite
- Race-detector clean under concurrent Apply + Snapshot

---

## Architecture

### Control Plane Model

```
                    ControlDirective
                         |
                    +----v----+
                    | Control |
                    |  Plane  |  (InMemoryControlPlane, sync.RWMutex)
                    +----+----+
                         |
                    Snapshot()
                         |
          +--------------v--------------+
          |      GovernedExecutor       |
          |  [PRE-FLIGHT GATE]          |
          |  snap.IsExecutionAllowed()  |
          |                             |
          |  if denied -> reject event  |
          |  if allowed -> governance   |
          |            -> adapter       |
          |            -> execute       |
          +-----------------------------+
```

### State Machine

```
                   +--------+
            +----->| Active |<-----+
            |      +---+----+      |
         resume        |        resume
            |       pause|drain    |
            |          v   v       |
       +----+---+  +--------+  +--------+
       | Paused |  | Paused |  | Drained|
       +--------+  +---+----+  +--------+
                       |drain
                       v
                   +--------+
                   |Drained |
                   +--------+

                   +--------+
       (any) ----->| Halted |  (terminal until explicit resume)
          halt     +--------+
                       ^ resume NOT allowed from halted
```

### Control Commands (10)

| Command | Target | Effect |
|---------|--------|--------|
| pause | -- | Active -> Paused (blocks all execution) |
| resume | -- | Paused/Drained -> Active |
| drain | -- | Active/Paused -> Drained (blocks new, finish in-flight) |
| halt | -- | Any -> Halted (emergency stop) |
| disable_strategy | strategy ID | Blocks intents from that strategy |
| enable_strategy | strategy ID | Re-allows that strategy |
| disable_adapter | adapter ID | Blocks routing to that adapter |
| enable_adapter | adapter ID | Re-allows that adapter |
| set_simulation_profile | profile name | Selects simulation fill profile (empty = default) |
| update_allowlist | venues,symbols | Narrows (never widens) boot-time grant scope |

### Rejection Reason Categories

7 new reason constants added under `control_plane_` prefix, all mapping to `ReasonCategoryControlPlane`:
- `control_plane_paused`, `control_plane_drained`, `control_plane_halted`
- `control_plane_strategy_disabled`, `control_plane_adapter_disabled`
- `control_plane_venue_restricted`, `control_plane_symbol_restricted`

### Design Decisions

1. **Pre-flight, not mid-flight:** Control plane evaluates BEFORE governance, not after. This means pause/halt takes effect immediately without wasting governance cycles.

2. **Optional port:** `ControlPlane` field is nil-safe. Existing code paths (including `NewDefaultGovernedBootstrapExecutor`) work unchanged.

3. **Narrowing only:** Runtime allowlist overrides can only restrict venues/symbols beyond the boot-time grant. They cannot widen scope. This preserves the static governance invariant.

4. **Immutable snapshots:** `Snapshot()` returns a deep-copied struct. Callers cannot corrupt control plane state.

5. **Halted is sticky:** Resume from Halted is not allowed. The operator must investigate and explicitly use resume only from Paused/Drained. This prevents accidental restart after emergency stop.

---

## Files Modified

### New Files (4)

| File | LOC | Purpose |
|------|-----|---------|
| `internal/core/execution/domain/control.go` | 138 | ControlState, ControlCommand, ControlDirective, ControlSnapshot, AllowlistOverride, IsExecutionAllowed |
| `internal/core/execution/ports/control_plane.go` | 13 | ControlPlane interface (Snapshot + Apply) |
| `internal/core/execution/app/control_plane.go` | 181 | InMemoryControlPlane with sync.RWMutex, state machine, deep-copy snapshots |
| `internal/core/execution/app/control_plane_test.go` | 528 | 30 tests: state transitions, deny/allow, immutability, concurrency, reason categories |

### Modified Files (6)

| File | Delta | Change |
|------|-------|--------|
| `internal/core/execution/domain/reason.go` | +11 | 7 control_plane reasons + category constant + ReasonCategory handler |
| `internal/core/execution/app/governed_executor.go` | +15/-3 | Added controlPlane field, pre-flight gate in ExecuteAt |
| `internal/core/execution/app/governed_executor_test.go` | +150 | 5 integration tests: nil passthrough, halted/paused rejection, strategy disable, active flow-through |
| `cmd/executor/bootstrap.go` | +8/-4 | Create InMemoryControlPlane, pass to buildIntentExecutor, wire into both execution paths |
| `cmd/executor/bootstrap_test.go` | +3/-4 | Update call sites to pass control plane |
| `.dockerignore` | -1 | Whitespace cleanup (pre-existing diff) |

---

## Validation

### Test Results

```
execution/app:        73 tests PASS (race-detector clean)
execution/governance: 13 tests PASS (cached)
cmd/executor:          5 tests PASS
Full suite:            0 failures
```

### Coverage Areas

- State transitions: all 4 states, valid/invalid transitions (8 tests)
- Deny enforcement: paused/drained/halted block execution (3 tests)
- Strategy/adapter toggles: disable blocks, enable re-allows, case-insensitive (6 tests)
- Allowlist overrides: venue restriction, symbol restriction, clearing (4 tests)
- Validation: empty command, empty issuer, zero timestamp, missing target_id (4 tests)
- Immutability: snapshot mutation isolation, allowlist mutation isolation (2 tests)
- Concurrency: 100 goroutines Apply + 100 Snapshot with race detector (1 test)
- Integration: nil control plane passthrough, halted/paused/strategy-disabled rejection, active flow-through (5 tests)
- Reason category routing: all 7 reasons map to control_plane category (1 test)

---

## Remaining Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| No HTTP/gRPC endpoint to issue directives yet | Low | InMemoryControlPlane is wired and ready; Stage 12 can add `/api/v1/control` endpoint |
| Halted state requires process restart to recover | By Design | Sticky halt prevents accidental resume; future Stage can add explicit "acknowledge + resume" ceremony |
| No persistence of control plane state across restarts | Low | In-memory is correct for operational control; boot always starts Active |
| SimulationProfile stored but not yet consumed by SimulationEngine | Low | Plumbing ready; Stage 12 can add profile-to-config mapping |
| Allowlist narrowing is runtime-only, not persisted | By Design | Narrowing is an operational override, not a configuration change |

---

## Next Stage

**Stage 12 candidates** (in priority order):

1. **Control Plane HTTP API** -- Expose `/api/v1/control` endpoint for issuing directives, querying snapshots. Enables operator tooling without restart.

2. **Simulation Profile Registry** -- Map `SimulationProfile` names to `SimulationConfig` variants. Connect to control plane's `set_simulation_profile` command.

3. **Portfolio State Projection** -- Build `portfolio.state` as a derived projection from `execution.event` stream. Read-only aggregate of positions, P&L, exposure.

4. **Strategy Subsystem** -- Signal-to-intent pipeline with strategy domain types, signal evaluation, and intent emission.
