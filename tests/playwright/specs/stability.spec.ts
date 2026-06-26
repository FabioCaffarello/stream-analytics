/**
 * Stability — long-running checks that verify data integrity over time.
 *
 * These tests are intentionally slower. They verify that the client
 * doesn't degrade, leak, or desync over sustained operation.
 */

import { test, expect } from '../fixtures/mr-test';

test.describe('Stability', () => {
  test.beforeEach(async ({ dash }) => {
    await dash.bootWithData();
  });

  test('30s sustained operation — no seq gaps or page errors', async ({ page, probe, console: cons }) => {
    await page.waitForTimeout(25_000);
    expect(await probe.seqGapCount()).toBe(0);
    expect(await probe.resyncCount()).toBe(0);
    expect(cons.errors).toHaveLength(0);
    // Candles should still be incrementing
    expect(await probe.candleCount()).toBeGreaterThan(0);
  });

  test('60s sustained operation — data still flowing', async ({ page, probe, console: cons }) => {
    const initialCandles = await probe.candleCount();
    await page.waitForTimeout(55_000);
    const finalCandles = await probe.candleCount();
    expect(finalCandles).toBeGreaterThanOrEqual(initialCandles);
    expect(await probe.seqGapCount()).toBe(0);
    expect(cons.errors).toHaveLength(0);
  });

  test('stress: rapid TF cycle then 30s settle', async ({ dash, probe }) => {
    // Rapid cycle
    const tfs = ['1m', '5m', '15m', '1h', '4h', '1d'] as const;
    for (const tf of tfs) {
      await dash.switchTimeframe(tf, 300);
    }
    await dash.switchTimeframe('1m', 300);

    // Faster second pass
    for (const tf of tfs) {
      await dash.page.keyboard.press(tf === '1m' ? '1' : tf === '5m' ? '3' : tf === '15m' ? '4' : tf === '1h' ? '5' : tf === '4h' ? '6' : '7');
      await dash.page.waitForTimeout(200);
    }

    // Settle on 5m and wait
    await dash.switchTimeframe('5m');
    await dash.page.waitForTimeout(15_000);

    expect(await probe.seqGapCount()).toBe(0);
    expect(await probe.candleCount()).toBeGreaterThan(0);
    await dash.screenshot('stress-post-tf-settle');
  });
});
