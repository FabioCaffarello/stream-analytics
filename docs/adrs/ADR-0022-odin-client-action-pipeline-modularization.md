# ADR-0022 - Odin Client Action Pipeline Modularization

**Status:** Accepted  
**Implementation status:** Implemented (M5 scope)  
**Partial marker:** Status: Implemented (M5 scope)  
**Owner:** Client Platform  
**Last updated:** 2026-03-05  
**Date:** 2026-03-05  
**Deciders:** Client Architecture Group  
**Relates to:** `docs/prds/PRD-0006-client-evolution-mm-parity.md`, `.context/plans/odin-client-m5-legacy-exit-modularization.md`

---

## Context

The Odin client action pipeline accumulated multiple responsibilities in `client/src/core/app/actions.odin`:

1. UI action orchestration.
2. Cell topology mutation.
3. Market subscription fanout.
4. Stream-liveness/reset policy handling.

This concentration increases regression risk and slows review/ownership. M5 requires safer boundaries and phased decomposition while preserving runtime behavior and gates.

## Decision

1. Keep `actions.odin` as orchestration entrypoint (`apply_ui_actions` dispatcher).
2. Move domain-specific action logic into focused modules:
   - `actions_cell_mutations.odin` for cell topology state mutation.
   - `actions_market_subscriptions.odin` for market subscribe/unsubscribe policy.
   - `actions_profiles.odin` for profile lifecycle actions.
   - `actions_stream_control.odin` for active stream pick/resync transitions.
   - `actions_stream_state_helpers.odin` for stream liveness/reset semantics.
3. Treat each extraction as behavior-preserving refactor with mandatory gate validation (`check-core`, `check-wasm-compile`, Playwright stress).
4. Keep legacy-compatible flow during M5; remove redundant paths only after full gate stability.

## Consequences

- Positive:
  - Reduced hotspot size and clearer ownership boundaries.
  - Lower risk for future edits in cell/subscription/runtime-state concerns.
  - Incremental rollout compatible with current PREVC execution.

- Negative:
  - More files/functions to navigate for new contributors.
  - Temporary split architecture while remaining monolith pieces are still being extracted.

## Implementation Matrix

| Capability | Status | Reference |
|---|---|---|
| Cell mutation extraction (`Set_Cell_Widget/Stream`, `Add/Remove_Cell`) | Implemented | `client/src/core/app/actions_cell_mutations.odin` |
| Market subscription extraction (`Subscribe/Unsubscribe_Market`) | Implemented | `client/src/core/app/actions_market_subscriptions.odin` |
| Profile action extraction (`Select/Add/Remove/Apply/Connect/Disconnect`) | Implemented | `client/src/core/app/actions_profiles.odin` |
| Stream control extraction (`Pick_Stream`, `Resync_Active_Stream`) | Implemented | `client/src/core/app/actions_stream_control.odin` |
| Stream liveness reset consolidation | Implemented | `client/src/core/app/actions_stream_state_helpers.odin` |
| Full decomposition of remaining action branches (compare/layout/toggles) | Planned (next iteration) | `client/src/core/app/actions.odin` |

## Evidence

- `.context/evidence/m5-modularization-actions-cell-2026-03-05.md`
- `make -C client check-core`
- `make -C client check-wasm-compile`
- `npx --prefix tests/playwright playwright test tests/playwright/e2e/stress.spec.ts`

## Changelog

- 2026-03-05:
  - ADR created to formalize M5 modularization strategy.
  - First extraction wave implemented and validated by local gates.
  - M5 finalized with additional extraction waves (profiles/stream control) and validated by:
    - `npx --prefix tests/playwright playwright test tests/playwright/e2e` (18/18)
    - `.context/evidence/m5-probes-delta-vs-frozen-baseline-2026-03-05.md`.
