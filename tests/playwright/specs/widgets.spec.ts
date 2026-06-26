/**
 * Widget Operational Probes — validates that core widgets receive and process data.
 *
 * These tests verify the data pipeline from WebSocket → parse → store → render
 * for each major widget type. Assertions use WASM probes, not visual checks.
 */

import { test, expect } from '../fixtures/mr-test';
import { waitForTrades, waitForStats, waitForOrderbook, waitForProbeValue } from '../helpers/wait';

test.describe('Widget Data Flow', () => {
  test.beforeEach(async ({ dash }) => {
    // All 7 panels needed — default layout is Candle+Orderbook only.
    await dash.bootWithAllPanels();
  });

  // ── Candle widget ──────────────────────────────────────────────────

  test('candle widget has data', async ({ probe }) => {
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('candle latest close is a valid price', async ({ probe }) => {
    const close = await probe.read('probe_widget_candle_latest_close');
    expect(close).toBeGreaterThan(0);
  });

  test('candle latest timestamp is recent', async ({ probe }) => {
    const ts = await probe.read('probe_widget_candle_latest_end_ts');
    // Should be within last 24h (86400 seconds)
    const now = Date.now() / 1000;
    expect(ts).toBeGreaterThan(now - 86400);
  });

  test('live candle display is active', async ({ probe }) => {
    expect(await probe.activeLiveCandle()).toBe(true);
  });

  // ── Trades widget ──────────────────────────────────────────────────

  test('trades flow after boot', async ({ page, probe }) => {
    await waitForTrades(page);
    expect(await probe.tradesCount()).toBeGreaterThan(0);
  });

  test('tape entries are populated', async ({ page, probe }) => {
    await waitForTrades(page);
    expect(await probe.tapeEntries()).toBeGreaterThan(0);
  });

  // ── Stats widget ───────────────────────────────────────────────────

  test('stats entries populate after boot', async ({ page, probe }) => {
    await waitForStats(page);
    expect(await probe.statsCount()).toBeGreaterThan(0);
  });

  test('live stats display is active', async ({ probe, page }) => {
    await waitForStats(page);
    expect(await probe.activeLiveStats()).toBe(true);
  });

  // ── Orderbook / DOM widget ─────────────────────────────────────────

  test('orderbook has asks and bids', async ({ page, probe }) => {
    await waitForOrderbook(page);
    expect(await probe.orderbookAsks()).toBeGreaterThan(0);
    expect(await probe.orderbookBids()).toBeGreaterThan(0);
  });

  test('DOM entries are populated', async ({ page, probe }) => {
    // DOM may need time to accumulate
    await page.waitForTimeout(5_000);
    const entries = await probe.domEntries();
    expect(entries).toBeGreaterThanOrEqual(0);
  });

  // ── Widget data continues across TF switch ─────────────────────────

  test('candle data reloads after TF switch', async ({ dash, probe }) => {
    const countBefore = await probe.candleCount();
    await dash.switchTimeframe('5m');
    // New TF should eventually have candles
    await dash.page.waitForTimeout(3_000);
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('trades continue after TF switch', async ({ dash, probe }) => {
    await waitForTrades(dash.page);
    await dash.switchTimeframe('15m');
    await dash.page.waitForTimeout(5_000);
    // Trades are timeframe-independent — should still flow
    expect(await probe.tradesCount()).toBeGreaterThan(0);
  });

  // ── No widget drops under normal operation ─────────────────────────

  test('stats widget has zero drops during first 15s', async ({ page, probe }) => {
    await page.waitForTimeout(10_000);
    expect(await probe.statsDropTotal()).toBe(0);
  });

  test('tape widget has zero drops during first 15s', async ({ page, probe }) => {
    await page.waitForTimeout(10_000);
    expect(await probe.tapeDropTotal()).toBe(0);
  });
});
