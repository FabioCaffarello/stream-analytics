# Stage S132 — Playwright Test Architecture Rewrite

**Date:** 2026-03-09
**Status:** COMPLETE

## Summary

Rewrote the entire Playwright E2E test architecture from fragile screenshot-centric scripts to a
behavior-driven, probe-verified test suite with proper fixtures, helpers, and page objects.

## Problems Solved

| Problem | Before | After |
|---------|--------|-------|
| Duplicated helpers | `waitForCanvas`/`waitForWS` copied in 8 files | Single `helpers/` module |
| Blind waits | `waitForTimeout(8_000)` everywhere | Probe-driven `waitForFullBoot` (hello + ACK) |
| No page objects | Raw `page.keyboard.press('c')` | `dash.enterCompareMode()` |
| No fixtures | Manual setup per test | `mr-test.ts` provides probe/canvas/console/dash |
| Fragile assertions | Screenshot-only (no behavioral checks) | WASM probe assertions + screenshots on failure |
| No structured reporting | Scattered screenshot dirs | HTML + JSON reporter, artifacts dir |
| No retry strategy | Tests fail once → done | 1 retry with trace + video |

## New Architecture

```
tests/playwright/
├── fixtures/
│   └── mr-test.ts           # Custom Playwright fixtures (probe, canvas, console, dash)
├── helpers/
│   ├── index.ts             # Barrel export
│   ├── wasm-probe.ts        # Typed access to window.__mr_wasm_exports
│   ├── console-collector.ts # Console/error capture
│   ├── canvas-driver.ts     # Canvas pixel inspection
│   └── wait.ts              # Probe-based wait strategies
├── pages/
│   └── dashboard.ts         # Keyboard-driven page object
├── specs/
│   ├── boot.spec.ts         # 8 tests — lifecycle validation
│   ├── timeframe.spec.ts    # 8 tests — TF switching + desync check
│   ├── compare-mode.spec.ts # 5 tests — split-pane behavior
│   ├── ui-modes.spec.ts     # 6 tests — zen/indicators/help/detail/picker
│   └── stability.spec.ts    # 3 tests — long-running integrity
├── scripts/                 # Legacy standalone scripts (retained)
├── archive/
│   └── e2e-legacy/          # Old spec files (archived, not deleted)
└── package.json             # v2.0.0 with per-suite scripts
```

## Key Design Decisions

### 1. Probe-Based Waiting (no blind timeouts)

Old pattern:
```ts
await waitForCanvas(page);
await page.waitForTimeout(8_000); // hope WS connects
```

New pattern:
```ts
await waitForFullBoot(page);
// canvas → WASM → hello handshake → subscribe ACK
```

Each step polls a WASM probe function. Hard timeout (45s) is safety net only.

### 2. DashboardPage as Behavioral Façade

All keyboard shortcuts are encapsulated:
```ts
dash.switchTimeframe('5m')    // press key + wait for probe to confirm TF index
dash.enterCompareMode()       // idempotent — checks probe first
dash.toggleZenMode()          // press 'z' + settle
```

Tests read as intent, not implementation.

### 3. WasmProbe Typed Accessor

Wraps `window.__mr_wasm_exports` with typed methods:
```ts
probe.candleCount()          // number
probe.compareMode()          // boolean
probe.seqGapCount()          // number
probe.snapshot()             // Record<string, number>
```

### 4. Canvas Strategy

Since MR renders entirely to Canvas2D:
- **Behavioral assertions** use WASM probes (data flowing, mode active, etc.)
- **Visual assertions** use `CanvasDriver.hasVisibleContent()` and pixel sampling
- **Screenshots** captured on failure only (or explicitly via `dash.screenshot()`)

### 5. Legacy Scripts Retained

The standalone `.mjs` scripts (`iq-smoke`, `m1-baseline`, etc.) serve different purposes
(metrics probing, evidence collection for stage reports) and are retained in `scripts/`.

## Test Count

| Suite | Tests |
|-------|-------|
| boot.spec.ts | 8 |
| timeframe.spec.ts | 8 |
| compare-mode.spec.ts | 5 |
| ui-modes.spec.ts | 6 |
| stability.spec.ts | 3 |
| **Total** | **30** |

## Config Changes

- `playwright.config.ts`: testDir → `specs/`, reporters (list + HTML + JSON), retry 1, trace on retry
- Workers: 1 (serial) — WASM client is single-instance
- Screenshots: only-on-failure (not every test)
- Artifacts: `tests/playwright/artifacts/`

## Running

```bash
# All specs
npx playwright test

# Single suite
npx playwright test specs/boot.spec.ts

# Via npm scripts
cd tests/playwright && npm test
cd tests/playwright && npm run test:boot
```

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Sustainable, reusable E2E base | DONE — fixtures + helpers + page object |
| Robust helpers | DONE — probe-based waits, typed accessors |
| Less fragility with UI changes | DONE — no DOM selectors, behavioral probes |
| Canvas/WASM strategy | DONE — probe + pixel sampling |
| Obsolete tests archived | DONE — `archive/e2e-legacy/` |
| Suite documented | DONE — this report |
