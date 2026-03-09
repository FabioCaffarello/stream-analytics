/**
 * mr-test — Custom Playwright test fixtures for Market Raccoon.
 *
 * Provides pre-wired helpers so every test gets:
 *   - `probe`   — WasmProbe instance
 *   - `canvas`  — CanvasDriver instance
 *   - `console` — ConsoleCollector (attached before navigation)
 *   - `dash`    — DashboardPage (keyboard-driven page object)
 *
 * Usage:
 *   import { test, expect } from '../fixtures/mr-test';
 *   test('my test', async ({ page, probe, dash }) => { ... });
 */

import { test as base, expect } from '@playwright/test';
import { WasmProbe } from '../helpers/wasm-probe';
import { ConsoleCollector } from '../helpers/console-collector';
import { CanvasDriver } from '../helpers/canvas-driver';
import { DashboardPage } from '../pages/dashboard';

type MrFixtures = {
  probe: WasmProbe;
  canvas: CanvasDriver;
  console: ConsoleCollector;
  dash: DashboardPage;
};

export const test = base.extend<MrFixtures>({
  console: async ({ page }, use) => {
    const collector = new ConsoleCollector(page);
    await use(collector);
  },

  probe: async ({ page }, use) => {
    await use(new WasmProbe(page));
  },

  canvas: async ({ page }, use) => {
    await use(new CanvasDriver(page));
  },

  dash: async ({ page }, use) => {
    await use(new DashboardPage(page));
  },
});

export { expect };
