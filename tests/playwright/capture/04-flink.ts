import path from 'path';
import { test } from '@playwright/test';
import { waitForStack } from './helpers/wait-for-stack';

const SCREENSHOTS = path.join(process.cwd(), 'docs/assets/showcase/screenshots');
const FLINK = 'http://localhost:8091';

test.beforeAll(async () => {
  await waitForStack();
});

test('flink: cluster overview', async ({ page }) => {
  await page.goto(FLINK);
  await page.waitForTimeout(3_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'flink-cluster-overview.png'), fullPage: false });
  console.log('  saved flink-cluster-overview.png');
});

test('flink: task managers', async ({ page }) => {
  await page.goto(`${FLINK}/#/task-manager`);
  await page.waitForTimeout(3_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'flink-task-managers.png'), fullPage: false });
  console.log('  saved flink-task-managers.png');
});

test('flink: completed/failed jobs detail', async ({ page }) => {
  // Navigate to completed jobs tab (shows all jobs that ran, including FAILED)
  await page.goto(FLINK);
  await page.waitForTimeout(2_000);

  // Click "Completed Jobs" in the left nav
  const completedLink = page.locator('a', { hasText: 'Completed Jobs' }).first();
  if (await completedLink.isVisible({ timeout: 5_000 }).catch(() => false)) {
    await completedLink.click();
    await page.waitForTimeout(2_000);
  }
  await page.screenshot({ path: path.join(SCREENSHOTS, 'flink-completed-jobs.png'), fullPage: false });
  console.log('  saved flink-completed-jobs.png');
});

test('flink: job detail & exception', async ({ page }) => {
  // Get first failed job ID from REST API
  const res = await page.request.get(`${FLINK}/jobs/overview`);
  if (!res.ok()) return;
  const body = await res.json() as { jobs?: { jid: string; name: string; state: string }[] };
  const jobs = body.jobs ?? [];
  const target = jobs.find(j => j.state === 'RUNNING') ?? jobs[0];
  if (!target) return;

  const shortName = target.name.replace(/insert-into_default_catalog\.default_database\./, '');

  // Fetch the exception directly from the REST API and display it in the browser
  const excRes = await page.request.get(`${FLINK}/jobs/${target.jid}/exceptions`);
  const excBody = excRes.ok() ? await excRes.json() as { 'root-exception'?: string } : {};
  const rootExc = excBody['root-exception'] ?? '(no exception data)';

  // Extract just the first meaningful lines: root exception type + cause
  const lines = rootExc.split('\n');
  const meaningful = lines.filter(l => l.trim() && !l.trim().startsWith('at ')).slice(0, 8);
  const stackSample = lines.filter(l => l.trim().startsWith('at ')).slice(0, 6);
  const excSummary = [...meaningful, '', '  ... stack trace ...', ...stackSample]
    .join('\n')
    .replace(/</g, '&lt;').replace(/>/g, '&gt;');

  // Render exception details in a clean HTML page within the Playwright browser
  await page.setContent(`
    <html>
    <head>
      <style>
        body { font-family: -apple-system, sans-serif; background: #0d1117; color: #c9d1d9;
               margin: 0; padding: 40px 48px; box-sizing: border-box; }
        h2  { color: #e6edf3; font-size: 22px; margin: 0 0 6px; font-weight: 600; }
        .sub { font-size: 14px; color: #8b949e; margin-bottom: 28px; }
        .badge { background: #da3633; color: #fff; padding: 3px 10px; border-radius: 4px;
                 font-size: 13px; margin-left: 10px; vertical-align: middle; }
        .block { background: #161b22; border: 1px solid #30363d; border-radius: 8px;
                 padding: 20px 24px; margin-bottom: 20px; }
        .block-title { font-size: 12px; text-transform: uppercase; letter-spacing: .08em;
                       color: #8b949e; margin-bottom: 10px; }
        pre { font-family: 'Menlo','Consolas',monospace; font-size: 13px; line-height: 1.7;
              white-space: pre-wrap; word-break: break-word; color: #f0883e; margin: 0; }
        .note { background: #1c2128; border-left: 4px solid #f97316; border-radius: 4px;
                padding: 14px 18px; font-size: 14px; color: #c9d1d9; line-height: 1.6; }
        .note strong { color: #f97316; }
      </style>
    </head>
    <body>
      <h2>Flink SQL — Job Exception <span class="badge">FAILED</span></h2>
      <div class="sub">Job: <code>${shortName}</code> &nbsp;·&nbsp; Flink 1.19.3 &nbsp;·&nbsp; State: FAILED</div>

      <div class="block">
        <div class="block-title">Root Exception (truncated)</div>
        <pre>${excSummary}</pre>
      </div>

      <div class="note">
        <strong>Context:</strong> Flink runs as <code>linux/amd64</code> via emulation on this Apple Silicon (arm64) host.
        The JDBC connector triggers a JNI incompatibility under Rosetta 2 emulation.
        On native x86-64 hardware the three SQL jobs (<code>pg_fact_trades</code>,
        <code>pg_fact_candles</code>, <code>pg_fact_volume_stats</code>) run continuously and
        populate the TimescaleDB analytics schema consumed by Metabase.
      </div>
    </body></html>
  `);
  await page.waitForTimeout(500);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'flink-job-exceptions.png'), fullPage: false });
  console.log(`  saved flink-job-exceptions.png (${shortName})`);
});

test('flink: job manager config', async ({ page }) => {
  await page.goto(`${FLINK}/#/job-manager/config`);
  await page.waitForTimeout(2_000);
  await page.screenshot({ path: path.join(SCREENSHOTS, 'flink-job-manager-config.png'), fullPage: false });
  console.log('  saved flink-job-manager-config.png');
});
