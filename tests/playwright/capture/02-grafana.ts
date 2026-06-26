import path from 'path';
import { test, type Page } from '@playwright/test';
import { waitForStack } from './helpers/wait-for-stack';

const SCREENSHOTS = path.join(process.cwd(), 'docs/assets/showcase/screenshots');
const GRAFANA = 'http://127.0.0.1:3000';
const BASIC_AUTH = `Basic ${Buffer.from('admin:admin').toString('base64')}`;

// Dashboard UIDs provisioned via Grafana provisioning
const DASHBOARDS = [
  { uid: 'stream-analytics-overview',  file: 'grafana-overview.png',   title: 'Overview' },
  { uid: 'stream-analytics-ingest',    file: 'grafana-ingestion.png',  title: 'Ingestion' },
  { uid: 'stream-analytics-delivery',  file: 'grafana-delivery.png',   title: 'Delivery' },
  { uid: 'stream-analytics-store',     file: 'grafana-storage.png',    title: 'Storage' },
  { uid: 'stream-analytics-vpvr',      file: 'grafana-analytics.png',  title: 'VPVR/Analytics' },
] as const;

async function setGrafanaAuth(page: Page): Promise<void> {
  await page.context().setExtraHTTPHeaders({ Authorization: BASIC_AUTH });
}

async function captureDashboard(page: Page, uid: string, file: string, title: string): Promise<void> {
  await setGrafanaAuth(page);
  // kiosk=tv removes nav bar; from/to ensure 1h window with real data
  const url = `${GRAFANA}/d/${uid}?kiosk=tv&from=now-1h&to=now&refresh=`;
  await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30_000 });

  // Wait for Grafana to finish loading panels (spinner disappears)
  await page.waitForFunction(() => {
    const spinners = document.querySelectorAll('[aria-label="Panel loading bar"]');
    return spinners.length === 0;
  }, { timeout: 20_000 }).catch(() => {
    // If timeout, proceed anyway — panels may still render
  });

  // Additional settle time for chart rendering
  await page.waitForTimeout(5_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, file), fullPage: false });
  console.log(`  saved ${file} (${title})`);
}

test.beforeAll(async () => {
  await waitForStack();
});

test('grafana: overview',  async ({ page }) => captureDashboard(page, DASHBOARDS[0].uid, DASHBOARDS[0].file, DASHBOARDS[0].title));
test('grafana: ingestion', async ({ page }) => captureDashboard(page, DASHBOARDS[1].uid, DASHBOARDS[1].file, DASHBOARDS[1].title));
test('grafana: delivery',  async ({ page }) => captureDashboard(page, DASHBOARDS[2].uid, DASHBOARDS[2].file, DASHBOARDS[2].title));
test('grafana: store',     async ({ page }) => captureDashboard(page, DASHBOARDS[3].uid, DASHBOARDS[3].file, DASHBOARDS[3].title));
test('grafana: vpvr',      async ({ page }) => captureDashboard(page, DASHBOARDS[4].uid, DASHBOARDS[4].file, DASHBOARDS[4].title));
