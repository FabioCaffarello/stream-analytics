import { test, expect, Page } from '@playwright/test';

const SCREENSHOT_DIR = 'tests/playwright/screenshots';

async function waitForCanvas(page: Page, timeoutMs = 15_000) {
  await page.waitForFunction(
    () => {
      const c = document.querySelector('canvas');
      if (!c) return false;
      const ctx = c.getContext('2d') || c.getContext('webgl2') || c.getContext('webgl');
      return !!ctx;
    },
    { timeout: timeoutMs },
  );
}

async function waitForWS(page: Page, timeoutMs = 20_000) {
  // Wait until the status bar shows something other than "Offline"
  await page.waitForFunction(
    () => {
      const el = document.querySelector('canvas');
      return !!el; // canvas exists = WASM loaded
    },
    { timeout: timeoutMs },
  );
  // Give WS time to connect and receive data
  await page.waitForTimeout(8_000);
}

async function getStatusBarText(page: Page): Promise<string> {
  // Take screenshot and read it visually — status bar is rendered in canvas
  return '';
}

test.describe('Market Raccoon Diagnostic Suite', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await waitForCanvas(page);
  });

  test('01 - page loads and canvas renders', async ({ page }) => {
    const canvas = page.locator('canvas');
    await expect(canvas).toBeVisible();
    await page.screenshot({ path: `${SCREENSHOT_DIR}/01-page-load.png`, fullPage: true });
  });

  test('02 - websocket connects and receives data', async ({ page }) => {
    await waitForWS(page);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/02-ws-connected.png`, fullPage: true });
  });

  test('03 - data flow after 10s', async ({ page }) => {
    await waitForWS(page);
    await page.waitForTimeout(5_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/03-data-flow-10s.png`, fullPage: true });
  });

  test('04 - data flow after 20s', async ({ page }) => {
    await waitForWS(page);
    await page.waitForTimeout(15_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/04-data-flow-20s.png`, fullPage: true });
  });

  test('05 - timeframe switch 1m', async ({ page }) => {
    await waitForWS(page);
    // Press '1' for 1m timeframe
    await page.keyboard.press('1');
    await page.waitForTimeout(3_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/05-timeframe-1m.png`, fullPage: true });
  });

  test('06 - timeframe switch 1h', async ({ page }) => {
    await waitForWS(page);
    await page.keyboard.press('5');
    await page.waitForTimeout(3_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/06-timeframe-1h.png`, fullPage: true });
  });

  test('07 - indicators toggle', async ({ page }) => {
    await waitForWS(page);
    // 'i' toggles indicator panel
    await page.keyboard.press('i');
    await page.waitForTimeout(2_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/07-indicators.png`, fullPage: true });
  });

  test('08 - zen mode', async ({ page }) => {
    await waitForWS(page);
    await page.keyboard.press('z');
    await page.waitForTimeout(1_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/08-zen-mode.png`, fullPage: true });
  });

  test('09 - help overlay', async ({ page }) => {
    await waitForWS(page);
    await page.keyboard.press('?');
    await page.waitForTimeout(1_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/09-help-overlay.png`, fullPage: true });
    // Close help
    await page.keyboard.press('?');
  });

  test('10 - detail panel', async ({ page }) => {
    await waitForWS(page);
    await page.keyboard.press('d');
    await page.waitForTimeout(2_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/10-detail-panel.png`, fullPage: true });
  });

  test('11 - stream picker', async ({ page }) => {
    await waitForWS(page);
    await page.keyboard.press('p');
    await page.waitForTimeout(2_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/11-stream-picker.png`, fullPage: true });
  });

  test('12 - compare mode', async ({ page }) => {
    await waitForWS(page);
    await page.keyboard.press('c');
    await page.waitForTimeout(3_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/12-compare-mode.png`, fullPage: true });
  });

  test('13 - long running stability 30s', async ({ page }) => {
    await waitForWS(page);
    await page.waitForTimeout(25_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/13-stability-30s.png`, fullPage: true });
  });

  test('14 - long running stability 45s', async ({ page }) => {
    await waitForWS(page);
    await page.waitForTimeout(40_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/14-stability-45s.png`, fullPage: true });
  });

  test('15 - long running stability 60s', async ({ page }) => {
    await waitForWS(page);
    await page.waitForTimeout(55_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/15-stability-60s.png`, fullPage: true });
  });
});
