import path from 'path';
import { test, type Page } from '@playwright/test';
import { waitForStack } from './helpers/wait-for-stack';

const SCREENSHOTS = path.join(process.cwd(), 'docs/assets/showcase/screenshots');
const MB_URL = 'http://127.0.0.1:3001';

async function metabaseApiLogin(page: Page): Promise<string | null> {
  const probe = await page.request.get(`${MB_URL}/api/health`, { timeout: 5_000 }).catch(() => null);
  if (!probe || !probe.ok()) {
    console.warn('[capture] Metabase not healthy — skipping');
    return null;
  }

  const res = await page.request.post(`${MB_URL}/api/session`, {
    headers: { 'Content-Type': 'application/json' },
    data: JSON.stringify({ username: 'fabio.caffarello@gmail.com', password: 'Racc00n_Admin!' }),
  });

  if (!res.ok()) {
    console.warn(`[capture] Metabase login failed: ${res.status()}. Skipping.`);
    return null;
  }

  const body = await res.json() as { id: string };
  const token = body.id;

  await page.context().addCookies([{
    name: 'metabase.SESSION',
    value: token,
    domain: '127.0.0.1',
    path: '/',
  }]);
  return token;
}

async function getCollectionId(page: Page, token: string, name: string): Promise<number | null> {
  const res = await page.request.get(`${MB_URL}/api/collection`, {
    headers: { 'X-Metabase-Session': token },
  });
  if (!res.ok()) return null;
  const cols = await res.json() as Array<{ id: number | string; name: string }>;
  const match = cols.find(c => typeof c.id === 'number' && c.name.includes(name));
  return match ? (match.id as number) : null;
}

async function getDashboardId(page: Page, token: string, name: string): Promise<number | null> {
  const res = await page.request.get(`${MB_URL}/api/dashboard`, {
    headers: { 'X-Metabase-Session': token },
  });
  if (!res.ok()) return null;
  const dashes = await res.json() as Array<{ id: number; name: string }>;
  const match = dashes.find(d => d.name.includes(name));
  return match ? match.id : null;
}

test.beforeAll(async () => {
  await waitForStack();
});

test('metabase: stream analytics collection', async ({ page }) => {
  const token = await metabaseApiLogin(page);
  if (!token) return;

  // Navigate to our provisioned collection (Stream Analytics Analytics)
  const colId = await getCollectionId(page, token, 'Stream Analytics Analytics');
  if (colId) {
    await page.goto(`${MB_URL}/collection/${colId}`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    console.log(`  navigated to collection id=${colId}`);
  } else {
    await page.goto(`${MB_URL}/collection/root`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    console.warn('[capture] Collection not found, showing root');
  }
  await page.waitForTimeout(4_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'metabase-collection.png'), fullPage: false });
  console.log('  saved metabase-collection.png');
});

test('metabase: market microstructure dashboard', async ({ page }) => {
  const token = await metabaseApiLogin(page);
  if (!token) return;

  const dashId = await getDashboardId(page, token, 'Market Microstructure');
  if (!dashId) {
    console.warn('[capture] Dashboard not found');
    return;
  }

  await page.goto(`${MB_URL}/dashboard/${dashId}`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  // Wait for dashboard cards to begin rendering
  await page.waitForTimeout(8_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'metabase-dashboard.png'), fullPage: false });
  console.log(`  saved metabase-dashboard.png (id=${dashId})`);
});

test('metabase: question editor — KPI card', async ({ page }) => {
  const token = await metabaseApiLogin(page);
  if (!token) return;

  // Find the "KPI · Total Trades (24h)" card
  const res = await page.request.get(`${MB_URL}/api/card`, {
    headers: { 'X-Metabase-Session': token },
  });
  const cards = res.ok() ? (await res.json() as Array<{ id: number; name: string }>) : [];
  const kpi = cards.find(c => c.name.includes('Total Trades'));

  if (kpi) {
    await page.goto(`${MB_URL}/question/${kpi.id}`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await page.waitForTimeout(6_000);
    await page.screenshot({ path: path.join(SCREENSHOTS, 'metabase-kpi-card.png'), fullPage: false });
    console.log(`  saved metabase-kpi-card.png (card id=${kpi.id})`);
  } else {
    console.warn('[capture] KPI card not found');
  }
});
