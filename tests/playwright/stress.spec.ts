import { test, expect, Page } from '@playwright/test';

const SCREENSHOT_DIR = 'tests/playwright/screenshots';

async function waitForCanvas(page: Page, timeoutMs = 15_000) {
  await page.waitForFunction(
    () => !!document.querySelector('canvas'),
    { timeout: timeoutMs },
  );
}

async function waitForWS(page: Page) {
  await waitForCanvas(page);
  await page.waitForTimeout(8_000);
}

test.describe('Desync Stress Tests', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await waitForWS(page);
  });

  test('rapid TF switching then stability check', async ({ page }) => {
    // Rapid timeframe switching — historically triggers desync
    const tfKeys = ['1', '2', '3', '4', '5', '6', '7']; // 1m,5s,5m,15m,1h,4h,1d
    for (const k of tfKeys) {
      await page.keyboard.press(k);
      await page.waitForTimeout(500);
    }
    // Back to 1m
    await page.keyboard.press('1');
    await page.waitForTimeout(500);

    // Cycle again faster
    for (const k of tfKeys) {
      await page.keyboard.press(k);
      await page.waitForTimeout(200);
    }
    await page.keyboard.press('3'); // settle on 5m

    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-01-post-tf-cycle.png`, fullPage: true });

    // Wait 15s for data to flow and any desync to manifest
    await page.waitForTimeout(15_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-02-tf-settle-15s.png`, fullPage: true });

    // Wait another 15s
    await page.waitForTimeout(15_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-03-tf-settle-30s.png`, fullPage: true });
  });

  test('compare mode with TF switches', async ({ page }) => {
    // Enter compare mode
    await page.keyboard.press('c');
    await page.waitForTimeout(5_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-04-compare-enter.png`, fullPage: true });

    // Switch TF in compare mode
    await page.keyboard.press('3'); // 5m
    await page.waitForTimeout(3_000);
    await page.keyboard.press('5'); // 1h
    await page.waitForTimeout(3_000);
    await page.keyboard.press('1'); // 1m
    await page.waitForTimeout(10_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-05-compare-tf-switch.png`, fullPage: true });

    // Exit compare, wait for stability
    await page.keyboard.press('c');
    await page.waitForTimeout(10_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-06-compare-exit-10s.png`, fullPage: true });
  });

  test('stream picker cycle', async ({ page }) => {
    // Open stream picker and cycle
    await page.keyboard.press('p');
    await page.waitForTimeout(2_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-07-picker-open.png`, fullPage: true });

    // Navigate picker (down arrow to select different stream)
    await page.keyboard.press('ArrowDown');
    await page.waitForTimeout(500);
    await page.keyboard.press('Enter');
    await page.waitForTimeout(8_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-08-picker-switch.png`, fullPage: true });

    // Wait for full stability after stream switch
    await page.waitForTimeout(15_000);
    await page.screenshot({ path: `${SCREENSHOT_DIR}/stress-09-picker-settle.png`, fullPage: true });
  });
});
