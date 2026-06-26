import path from 'path';
import { test } from '@playwright/test';
import { waitForStack } from './helpers/wait-for-stack';

const SCREENSHOTS = path.join(process.cwd(), 'docs/assets/showcase/screenshots');

test.use({ baseURL: 'http://localhost:8222' });

test.beforeAll(async () => {
  await waitForStack();
});

test('nats: jetstream overview', async ({ page }) => {
  await page.goto('/jsz?acc=&accounts=true&streams=true');
  await page.waitForTimeout(2_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'nats-jetstream.png'), fullPage: true });
});

test('nats: connections', async ({ page }) => {
  await page.goto('/connz?limit=20');
  await page.waitForTimeout(2_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'nats-connections.png'), fullPage: true });
});
