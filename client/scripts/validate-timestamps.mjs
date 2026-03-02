// Playwright script to validate timestamp parity with MarketMonkey.
// Captures screenshots and checks canvas rendering for timestamp patterns.
import { chromium } from 'playwright';
import { writeFileSync } from 'fs';

const URL = 'http://localhost:8090';
const WAIT_FOR_DATA_MS = 12000; // wait for WS data to arrive
const SCREENSHOT_DIR = '/Volumes/OWC Express 1M2/Develop/market-raccoon/client/build';

async function main() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });

  // Collect console logs for debugging.
  const logs = [];
  page.on('console', msg => logs.push(`[${msg.type()}] ${msg.text()}`));

  console.log(`[1/5] Navigating to ${URL}...`);
  await page.goto(URL, { waitUntil: 'networkidle' });

  console.log(`[2/5] Waiting ${WAIT_FOR_DATA_MS / 1000}s for WS data...`);
  await page.waitForTimeout(WAIT_FOR_DATA_MS);

  // Take full-page screenshot.
  console.log('[3/5] Capturing full-page screenshot...');
  const fullPath = `${SCREENSHOT_DIR}/validate-timestamps-full.png`;
  await page.screenshot({ path: fullPath, fullPage: false });
  console.log(`  → ${fullPath}`);

  // Check console logs for WS connection and data.
  console.log('[4/5] Analyzing console output...');
  const wsConnected = logs.some(l => l.includes('ws_open') || l.includes('connected') || l.includes('ack'));
  const hasEvents = logs.some(l => l.includes('event') || l.includes('candle') || l.includes('trade'));
  const hasErrors = logs.filter(l => l.includes('error') || l.includes('Error') || l.includes('ERROR'));

  console.log(`  WS connected: ${wsConnected}`);
  console.log(`  Has data events: ${hasEvents}`);
  if (hasErrors.length > 0) {
    console.log(`  Errors found (${hasErrors.length}):`);
    hasErrors.slice(0, 5).forEach(e => console.log(`    ${e}`));
  }

  // Capture individual widget regions by zooming in on different areas.
  // The Odin client renders to a canvas, so we capture regions.
  console.log('[5/5] Capturing widget-region screenshots...');

  // Top-left region (candle chart with countdown timer).
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/validate-ts-candles.png`,
    clip: { x: 44, y: 52, width: 700, height: 400 },
  });
  console.log('  → candle region captured');

  // Bottom-left region (trades widget with HH:MM:SS).
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/validate-ts-trades.png`,
    clip: { x: 44, y: 600, width: 500, height: 280 },
  });
  console.log('  → trades region captured');

  // Right side (orderbook + stats — no timestamps expected).
  await page.screenshot({
    path: `${SCREENSHOT_DIR}/validate-ts-orderbook.png`,
    clip: { x: 900, y: 600, width: 490, height: 280 },
  });
  console.log('  → orderbook region captured');

  // Summary
  console.log('\n══════════════════════════════════════════');
  console.log('TIMESTAMP VALIDATION SUMMARY');
  console.log('══════════════════════════════════════════');
  console.log('Expected timestamps (MM parity):');
  console.log('  [1] Trades widget: HH:MM:SS per trade row');
  console.log('  [2] Candle chart: HH:MM x-axis labels');
  console.log('  [3] Candle chart: MM:SS countdown timer (new)');
  console.log('  [4] Stats/Orderbook/Heatmap/VPVR: no timestamps (matches MM)');
  console.log('');
  console.log('Canvas-rendered UI — visual inspection of screenshots required.');
  console.log(`Screenshots saved to: ${SCREENSHOT_DIR}/validate-ts-*.png`);

  // Write log dump for analysis.
  const logPath = `${SCREENSHOT_DIR}/validate-timestamps-console.log`;
  writeFileSync(logPath, logs.join('\n'));
  console.log(`Console log: ${logPath} (${logs.length} entries)`);

  await browser.close();

  // Exit code based on basic connectivity.
  if (!wsConnected && !hasEvents) {
    console.log('\n⚠ WARNING: No WS data detected — screenshots may show empty/offline state.');
    console.log('  Check that backend is running: make up PROCESSOR_REPLICAS=2');
    process.exit(1);
  }

  console.log('\n✓ Screenshots captured. Review visually for timestamp presence.');
  process.exit(0);
}

main().catch(err => {
  console.error('Playwright error:', err.message);
  process.exit(2);
});
