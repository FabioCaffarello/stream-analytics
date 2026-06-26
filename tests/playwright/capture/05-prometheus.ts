import path from 'path';
import { test } from '@playwright/test';
import { waitForStack } from './helpers/wait-for-stack';

const SCREENSHOTS = path.join(process.cwd(), 'docs/assets/showcase/screenshots');
const PROM = 'http://localhost:9090';

test.beforeAll(async () => {
  await waitForStack();
});

test('prometheus: targets overview', async ({ page }) => {
  await page.goto(`${PROM}/targets`);
  // Expand all target groups
  await page.waitForTimeout(2_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'prometheus-targets.png'), fullPage: false });
  console.log('  saved prometheus-targets.png');
});

test('prometheus: bus event throughput', async ({ page }) => {
  // rate of bus_published_total — confirmed 84 series with real data
  const expr = encodeURIComponent('sum by (instance) (rate(bus_published_total[2m]))');
  await page.goto(`${PROM}/graph?g0.expr=${expr}&g0.tab=0&g0.range_input=1h`);
  await page.waitForTimeout(4_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'prometheus-bus-throughput.png'), fullPage: false });
  console.log('  saved prometheus-bus-throughput.png');
});

test('prometheus: canonical events pipeline', async ({ page }) => {
  // canonical_events_total: events canonicalized per service
  const expr = encodeURIComponent('sum by (instance) (rate(canonical_events_total[2m]))');
  await page.goto(`${PROM}/graph?g0.expr=${expr}&g0.tab=0&g0.range_input=1h`);
  await page.waitForTimeout(4_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'prometheus-canonical-events.png'), fullPage: false });
  console.log('  saved prometheus-canonical-events.png');
});

test('prometheus: delivery routing', async ({ page }) => {
  // WS delivery: routed events + active sessions side by side
  const expr1 = encodeURIComponent('sum(rate(delivery_router_events_routed_total[2m]))');
  const expr2 = encodeURIComponent('sum(delivery_router_sessions_active)');
  await page.goto(
    `${PROM}/graph?g0.expr=${expr1}&g0.tab=0&g0.range_input=1h&g1.expr=${expr2}&g1.tab=0&g1.range_input=1h`,
  );
  await page.waitForTimeout(4_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'prometheus-delivery.png'), fullPage: false });
  console.log('  saved prometheus-delivery.png');
});
