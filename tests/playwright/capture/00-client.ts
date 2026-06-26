import path from 'path';
import { test } from '@playwright/test';
import { waitForFullBoot, waitForCandles, waitForOrderbook } from '../helpers/wait';
import { waitForStack } from './helpers/wait-for-stack';

const SCREENSHOTS = path.join(process.cwd(), 'docs/assets/showcase/screenshots');

test.beforeAll(async () => {
  await waitForStack();
});

test('client: full cockpit', async ({ page }) => {
  await page.goto('/');
  await waitForFullBoot(page);
  await waitForCandles(page);
  await waitForOrderbook(page);
  await page.waitForTimeout(5_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'client-cockpit-full.png'), fullPage: false });
});

test('client: after data seeding', async ({ page }) => {
  await page.goto('/');
  await waitForFullBoot(page);
  await waitForCandles(page);
  await waitForOrderbook(page);
  await page.waitForTimeout(8_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'client-seeded.png'), fullPage: false });
});
