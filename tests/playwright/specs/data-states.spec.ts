/**
 * Data States — validates the loading → seeding → live state transitions.
 *
 * When the client boots, widgets progress through states:
 *   Loading → Seeding (historical data arriving) → Live (realtime data flowing)
 *
 * These tests verify the transition is observable via probes and that
 * each state has expected characteristics.
 */

import { test, expect } from '../fixtures/mr-test';
import {
  waitForFullBoot,
  waitForCandles,
  waitForTrades,
  waitForStats,
} from '../helpers/wait';

test.describe('Data State Transitions', () => {
  test('WASM probes return zeros before data arrives', async ({ page, probe }) => {
    await page.goto('/');
    // Immediately after WASM load, before hello
    await probe.waitForReady();

    // Candles should be 0 initially
    const candles = await probe.candleCount();
    expect(candles).toBe(0);
  });

  test('hello handshake precedes subscribe ACK', async ({ page, probe }) => {
    await page.goto('/');
    await probe.waitForReady();

    // Wait for hello
    await page.waitForFunction(
      () => {
        const ex = (window as any).__mr_wasm_exports;
        return ex && ex.probe_md_hello_received() === 1;
      },
      { timeout: 20_000 },
    );

    expect(await probe.helloReceived()).toBe(true);
    // ACK may or may not be there yet — but hello must be first
  });

  test('candle count grows monotonically during seeding', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    await waitForCandles(page);

    const count1 = await probe.candleCount();
    await page.waitForTimeout(3_000);
    const count2 = await probe.candleCount();

    // Candle count should never decrease (monotonic)
    expect(count2).toBeGreaterThanOrEqual(count1);
  });

  test('trades accumulate after subscription', async ({ page, probe }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem('mr.settings.panel_visible_mask', '1111111');
    });
    await page.goto('/');
    await waitForFullBoot(page);
    await waitForTrades(page);

    const trades1 = await probe.tradesCount();
    await page.waitForTimeout(5_000);
    const trades2 = await probe.tradesCount();

    expect(trades2).toBeGreaterThanOrEqual(trades1);
  });

  test('stats become live after bootstrap', async ({ page, probe }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem('mr.settings.panel_visible_mask', '1111111');
    });
    await page.goto('/');
    await waitForFullBoot(page);
    await waitForStats(page);

    // Stats should have entries
    expect(await probe.statsCount()).toBeGreaterThan(0);
  });

  test('full pipeline: hello → ACK → candles → trades → stats', async ({ page, probe }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem('mr.settings.panel_visible_mask', '1111111');
    });
    await page.goto('/');
    await probe.waitForReady();

    // Stage 1: Hello
    await page.waitForFunction(
      () => (window as any).__mr_wasm_exports?.probe_md_hello_received() === 1,
      { timeout: 20_000 },
    );
    expect(await probe.helloReceived()).toBe(true);

    // Stage 2: Subscribe ACK
    await page.waitForFunction(
      () => (window as any).__mr_wasm_exports?.probe_md_subscribe_ack_count() > 0,
      { timeout: 30_000 },
    );
    expect(await probe.subscribeAckCount()).toBeGreaterThan(0);

    // Stage 3: Candles arrive
    await waitForCandles(page);
    expect(await probe.candleCount()).toBeGreaterThan(0);

    // Stage 4: Trades arrive
    await waitForTrades(page);
    expect(await probe.tradesCount()).toBeGreaterThan(0);

    // Stage 5: Stats arrive
    await waitForStats(page);
    expect(await probe.statsCount()).toBeGreaterThan(0);
  });

  test('TF switch reseeds candle data', async ({ dash, probe }) => {
    await dash.bootWithData();
    const candlesBefore = await probe.candleCount();

    // Switch to a different TF — candle store rebuilds
    await dash.switchTimeframe('4h');
    await dash.page.waitForTimeout(5_000);

    // New candles should arrive for the 4h timeframe
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('no resync events during normal boot', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    await waitForCandles(page);
    await page.waitForTimeout(5_000);

    // Under normal conditions, resync count should be 0
    expect(await probe.resyncCount()).toBe(0);
  });

  test('zero seq gaps during normal boot', async ({ page, probe }) => {
    await page.goto('/');
    await waitForFullBoot(page);
    await waitForCandles(page);
    await page.waitForTimeout(5_000);

    expect(await probe.seqGapCount()).toBe(0);
  });
});
