# M5 Modularization Evidence - Actions Cell Mutations (2026-03-05)

## Objective
Start M5 by reducing hotspot complexity in `actions.odin` without behavior regression.

## Change Summary
- Extracted cell mutation action handlers from `client/src/core/app/actions.odin` into:
  - `client/src/core/app/actions_cell_mutations.odin`
- Moved logic for:
  - `Set_Cell_Widget`
  - `Set_Cell_Stream`
  - `Add_Cell`
  - `Remove_Cell`
- Kept orchestration in `apply_ui_actions` by delegating to dedicated helpers.
- Extracted market subscription handlers from `client/src/core/app/actions.odin` into:
  - `client/src/core/app/actions_market_subscriptions.odin`
- Moved logic for:
  - `Subscribe_Market`
  - `Unsubscribe_Market`
- Consolidated stream-liveness reset logic into:
  - `client/src/core/app/actions_stream_state_helpers.odin`
- Applied helper in:
  - `Disconnect_Profile`
  - `Pick_Stream`
  - `Resync_Active_Stream`
- Extracted profile lifecycle handlers into:
  - `client/src/core/app/actions_profiles.odin`
- Moved logic for:
  - `Select_Profile`
  - `Add_Profile`
  - `Remove_Profile`
  - `Apply_Profile`
  - `Connect_Profile`
  - `Disconnect_Profile`
- Extracted stream control handlers into:
  - `client/src/core/app/actions_stream_control.odin`
- Moved logic for:
  - `Pick_Stream`
  - `Resync_Active_Stream`
- Hotspot reduction:
  - `actions.odin`: 752 -> 474 lines.

## Validation
- `make -C client check-core`: PASS
- `make -C client check-wasm-compile`: PASS
- `npx --prefix tests/playwright playwright test tests/playwright/e2e/stress.spec.ts`: PASS (3/3)
- `npx --prefix tests/playwright playwright test tests/playwright/e2e`: PASS (18/18)
- Playwright MCP cacheless validation (`:8090` + storage/cache clear + cache disabled): PASS (`hasCanvas=true`)
- `make test-short`: PASS
- `npm --prefix tests/playwright run m1:baseline` (cacheless probes): PASS

## Probe Delta vs Frozen Baseline
- Frozen source: `.context/evidence/m1-playwright-baseline-2026-03-05-frozen.json`
- Current source: `.context/evidence/m1-playwright-baseline-2026-03-05.json`
- Delta evidence: `.context/evidence/m5-probes-delta-vs-frozen-baseline-2026-03-05.md`
- Key checks:
  - `keyboard.timeframe_switches_delta`: 1 -> 1 (delta 0)
  - `click.timeframe_switches_delta`: 3 -> 3 (delta 0)
  - `click.stream_count_delta`: 0 -> 0 (delta 0)
  - `online.tape_drop_pct`: 0 -> 0 (delta 0)

## Online Gate Note
- `make -C client check-widgets-online`: PASS (latest run with `ob=50/50`).
- During intermediate runs there was transient DOM oscillation (`ob=0/0`), but it was not persistent in the final validation.

## Conclusion
M5 foi concluído com decomposição segura de `actions.odin` e sem regressão observada em gates core/wasm/playwright (stress + suíte E2E completa).
A decisão arquitetural final do milestone está registrada em `docs/adrs/ADR-0022-odin-client-action-pipeline-modularization.md`.
